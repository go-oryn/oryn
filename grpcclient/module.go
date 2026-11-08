// Package grpcclient provides gRPC client connections, configured by name and instrumented with
// OpenTelemetry.
//
// Connections are declared in the configuration, under grpcclient.connections.<name>, and created
// by the [*Factory]. [AsClient] provides a generated client on a named connection:
//
//	core.New(
//		core.WithConfigFS(configs.FS),
//		grpcclient.Module,
//		grpcclient.AsClient("users", pb.NewUsersClient),
//		fx.Provide(NewService), // NewService(client pb.UsersClient) *Service
//	)
//
// With the configuration:
//
//	grpcclient:
//	  connections:
//	    users:
//	      target: users:50051
//	      insecure: true
package grpcclient

import (
	"cmp"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/stats"

	"github.com/go-oryn/oryn/core/config"
)

// ModuleName is the name of the grpcclient fx module.
const ModuleName = "grpcclient"

// ErrClosed is returned by [Factory.Conn] once the application is stopped.
var ErrClosed = errors.New("grpcclient: factory closed")

// Module provides the [*Factory] creating the named connections. They are closed when the
// application stops.
var Module = fx.Module(
	ModuleName,
	fx.Provide(NewFactory),
)

// NewFactoryParams are the dependencies of [NewFactory].
type NewFactoryParams struct {
	fx.In

	Lifecycle          fx.Lifecycle
	Config             *config.Config
	Logger             *slog.Logger
	TracerProvider     trace.TracerProvider
	MeterProvider      metric.MeterProvider
	Propagator         propagation.TextMapPropagator
	UnaryInterceptors  []unaryInterceptor  `group:"oryn-grpcclient-unary-interceptors"`
	StreamInterceptors []streamInterceptor `group:"oryn-grpcclient-stream-interceptors"`
	DialOptions        []dialOptions       `group:"oryn-grpcclient-dial-options"`
}

// Factory creates the connections declared under grpcclient.connections.<name>. Each connection
// is created once, on first use, and shared. They are all closed when the application stops.
type Factory struct {
	config      *config.Config
	options     []grpc.DialOption
	dialOptions map[string][]grpc.DialOption

	mu     sync.Mutex
	conns  map[string]*grpc.ClientConn
	closed bool
}

// NewFactory returns the [*Factory]. Every connection it creates goes through, from the outermost:
//
//  1. the OpenTelemetry instrumentation (spans and metrics, trace context propagation), skipped
//     for the methods of grpcclient.observability.exclude,
//  2. the call logs, unless grpcclient.log.enabled is false, skipped for the excluded methods,
//  3. the registered interceptors, in registration order.
func NewFactory(params NewFactoryParams) *Factory {
	cfg := params.Config

	exclude := make(map[string]bool)
	for _, method := range cfg.GetStringSlice("grpcclient.observability.exclude") {
		exclude[method] = true
	}

	unary, stream := interceptors(params, exclude)

	factory := &Factory{
		config: cfg,
		options: []grpc.DialOption{
			grpc.WithStatsHandler(otelgrpc.NewClientHandler(
				otelgrpc.WithTracerProvider(params.TracerProvider),
				otelgrpc.WithMeterProvider(params.MeterProvider),
				otelgrpc.WithPropagators(params.Propagator),
				otelgrpc.WithFilter(func(info *stats.RPCTagInfo) bool { return !exclude[info.FullMethodName] }),
			)),
			grpc.WithChainUnaryInterceptor(unary...),
			grpc.WithChainStreamInterceptor(stream...),
		},
		dialOptions: make(map[string][]grpc.DialOption),
		conns:       make(map[string]*grpc.ClientConn),
	}

	slices.SortFunc(params.DialOptions, func(a, b dialOptions) int { return cmp.Compare(a.sequence, b.sequence) })

	for _, options := range params.DialOptions {
		factory.dialOptions[options.name] = append(factory.dialOptions[options.name], options.options...)
	}

	params.Lifecycle.Append(fx.StopHook(factory.Close))

	return factory
}

// interceptors returns the built-in interceptors, then the registered ones in registration order.
func interceptors(params NewFactoryParams, exclude map[string]bool) ([]grpc.UnaryClientInterceptor, []grpc.StreamClientInterceptor) {
	var (
		unary  []grpc.UnaryClientInterceptor
		stream []grpc.StreamClientInterceptor
	)

	if params.Config.GetBoolOrDefault("grpcclient.log.enabled", true) {
		unary = append(unary, newLogUnaryInterceptor(params.Logger, exclude))
		stream = append(stream, newLogStreamInterceptor(params.Logger, exclude))
	}

	// fx shuffles the value groups: restore the registration order.
	slices.SortFunc(params.UnaryInterceptors, func(a, b unaryInterceptor) int { return cmp.Compare(a.sequence, b.sequence) })
	slices.SortFunc(params.StreamInterceptors, func(a, b streamInterceptor) int { return cmp.Compare(a.sequence, b.sequence) })

	for _, interceptor := range params.UnaryInterceptors {
		unary = append(unary, interceptor.interceptor)
	}

	for _, interceptor := range params.StreamInterceptors {
		stream = append(stream, interceptor.interceptor)
	}

	return unary, stream
}

// Conn returns the connection named name, configured under grpcclient.connections.<name>.
// It is created on the first call, then shared. Creating a connection does not connect: the
// connection is established on the first call, and re-established when lost.
func (f *Factory) Conn(name string) (*grpc.ClientConn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return nil, ErrClosed
	}

	if conn, ok := f.conns[name]; ok {
		return conn, nil
	}

	conn, err := f.newConn(name)
	if err != nil {
		return nil, err
	}

	f.conns[name] = conn

	return conn, nil
}

// newConn creates the connection from its configuration.
func (f *Factory) newConn(name string) (*grpc.ClientConn, error) {
	key := "grpcclient.connections." + name

	target := f.config.GetString(key + ".target")
	if target == "" {
		return nil, fmt.Errorf("cannot create grpc connection %q: missing %s.target", name, key)
	}

	options := slices.Clone(f.options)

	if f.config.GetBool(key + ".insecure") {
		options = append(options, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		// TLS, with the system root certificates.
		options = append(options, grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, "")))
	}

	if authority := f.config.GetString(key + ".authority"); authority != "" {
		options = append(options, grpc.WithAuthority(authority))
	}

	if serviceConfig := f.config.GetString(key + ".service_config"); serviceConfig != "" {
		options = append(options, grpc.WithDefaultServiceConfig(serviceConfig))
	}

	options = append(options, f.dialOptions[name]...)

	conn, err := grpc.NewClient(target, options...)
	if err != nil {
		return nil, fmt.Errorf("cannot create grpc connection %q: %w", name, err)
	}

	return conn, nil
}

// Close closes all the connections. It is called when the application stops: [Factory.Conn]
// then returns [ErrClosed].
func (f *Factory) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.closed = true

	var errs []error

	for name, conn := range f.conns {
		if err := conn.Close(); err != nil {
			errs = append(errs, fmt.Errorf("cannot close grpc connection %q: %w", name, err))
		}
	}

	clear(f.conns)

	return errors.Join(errs...)
}

// AsClient provides a generated gRPC client, built with its constructor on the connection named
// name. The client type is inferred from the constructor:
//
//	grpcclient.AsClient("users", pb.NewUsersClient) // provides a pb.UsersClient
//
// The client is created when first needed by fx, which fails if the connection is not configured.
// To use two clients of the same type on different connections, provide them with
// [Factory.Conn] and named values instead.
func AsClient[T any](name string, constructor func(grpc.ClientConnInterface) T) fx.Option {
	return fx.Provide(func(factory *Factory) (T, error) {
		conn, err := factory.Conn(name)
		if err != nil {
			var zero T

			return zero, err
		}

		return constructor(conn), nil
	})
}
