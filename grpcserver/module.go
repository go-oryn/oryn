// Package grpcserver provides a gRPC server, instrumented with OpenTelemetry, with the gRPC
// health checking protocol backed by the application health checks.
//
// Services and interceptors are registered with [AsService], [AsUnaryInterceptor] and
// [AsStreamInterceptor], and built by fx with their dependencies. The server is started with [Serve]:
//
//	core.New(
//		grpcserver.Module,
//		grpcserver.AsUnaryInterceptor(NewAuthInterceptor),
//		grpcserver.AsService(&pb.Users_ServiceDesc, NewUsersService),
//	).Run(grpcserver.Serve())
package grpcserver

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"sync"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"
	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	reflectionv1 "google.golang.org/grpc/reflection/grpc_reflection_v1"
	reflectionv1alpha "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/grpc/stats"

	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/core/health"
)

// ModuleName is the name of the grpcserver fx module.
const ModuleName = "grpcserver"

// DefaultAddress is the default of grpcserver.address.
const DefaultAddress = ":50051"

// DefaultExclude is the default of grpcserver.observability.exclude: the health checks and the
// server reflection.
var DefaultExclude = []string{
	healthpb.Health_Check_FullMethodName,
	healthpb.Health_Watch_FullMethodName,
	healthpb.Health_List_FullMethodName,
	reflectionv1.ServerReflection_ServerReflectionInfo_FullMethodName,
	reflectionv1alpha.ServerReflection_ServerReflectionInfo_FullMethodName,
}

// reflectionServices are the services registered by the server reflection.
var reflectionServices = []string{
	"grpc.reflection.v1.ServerReflection",
	"grpc.reflection.v1alpha.ServerReflection",
}

// Module provides the [*grpc.Server], with the registered services and interceptors, and its [*Address].
var Module = fx.Module(
	ModuleName,
	fx.Provide(NewServer, NewAddress),
)

// NewServerParams are the dependencies of [NewServer].
type NewServerParams struct {
	fx.In

	Config             *config.Config
	Logger             *slog.Logger
	Checker            *health.Checker
	TracerProvider     trace.TracerProvider
	MeterProvider      metric.MeterProvider
	Propagator         propagation.TextMapPropagator
	Services           []service           `group:"oryn-grpcserver-services"`
	UnaryInterceptors  []unaryInterceptor  `group:"oryn-grpcserver-unary-interceptors"`
	StreamInterceptors []streamInterceptor `group:"oryn-grpcserver-stream-interceptors"`
}

// NewServer returns the gRPC server. Every call goes through, from the outermost:
//
//  1. the OpenTelemetry instrumentation (spans and metrics), skipped for the excluded methods,
//  2. the call logs, unless disabled, and skipped for the excluded methods,
//  3. the panic recovery,
//  4. the registered interceptors, in registration order, and the service.
//
// The gRPC health service is registered unless grpcserver.health.enabled is false, and the
// server reflection if grpcserver.reflection.enabled is true.
func NewServer(params NewServerParams) (*grpc.Server, error) {
	cfg := params.Config

	exclude := make(map[string]bool)
	for _, method := range cfg.GetStringSliceOrDefault("grpcserver.observability.exclude", DefaultExclude) {
		exclude[method] = true
	}

	unary, stream := interceptors(params, exclude)

	options := []grpc.ServerOption{
		grpc.StatsHandler(otelgrpc.NewServerHandler(
			otelgrpc.WithTracerProvider(params.TracerProvider),
			otelgrpc.WithMeterProvider(params.MeterProvider),
			otelgrpc.WithPropagators(params.Propagator),
			otelgrpc.WithFilter(func(info *stats.RPCTagInfo) bool { return !exclude[info.FullMethodName] }),
		)),
		grpc.ChainUnaryInterceptor(unary...),
		grpc.ChainStreamInterceptor(stream...),
	}

	if size := cfg.GetInt("grpcserver.max_recv_msg_size"); size > 0 {
		options = append(options, grpc.MaxRecvMsgSize(size))
	}

	if size := cfg.GetInt("grpcserver.max_send_msg_size"); size > 0 {
		options = append(options, grpc.MaxSendMsgSize(size))
	}

	server := grpc.NewServer(options...)

	if err := registerServices(server, params); err != nil {
		return nil, err
	}

	return server, nil
}

// interceptors returns the built-in interceptors, then the registered ones in registration order.
func interceptors(params NewServerParams, exclude map[string]bool) ([]grpc.UnaryServerInterceptor, []grpc.StreamServerInterceptor) {
	var (
		unary  []grpc.UnaryServerInterceptor
		stream []grpc.StreamServerInterceptor
	)

	if params.Config.GetBoolOrDefault("grpcserver.log.enabled", true) {
		unary = append(unary, newLogUnaryInterceptor(params.Logger, exclude))
		stream = append(stream, newLogStreamInterceptor(params.Logger, exclude))
	}

	unary = append(unary, newRecoverUnaryInterceptor(params.Logger))
	stream = append(stream, newRecoverStreamInterceptor(params.Logger))

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

// registerServices registers the services in registration order, then the health service and the
// server reflection when enabled. grpc exits the process on a duplicate service: check them first.
func registerServices(server *grpc.Server, params NewServerParams) error {
	slices.SortFunc(params.Services, func(a, b service) int { return cmp.Compare(a.sequence, b.sequence) })

	registered := make(map[string]bool)

	register := func(desc *grpc.ServiceDesc, impl any) error {
		if registered[desc.ServiceName] {
			return fmt.Errorf("cannot register service %s: already registered", desc.ServiceName)
		}

		registered[desc.ServiceName] = true
		server.RegisterService(desc, impl)

		return nil
	}

	for _, s := range params.Services {
		if err := register(s.desc, s.impl); err != nil {
			return err
		}
	}

	if params.Config.GetBoolOrDefault("grpcserver.health.enabled", true) {
		if err := register(&healthpb.Health_ServiceDesc, NewHealthServer(params.Checker)); err != nil {
			return err
		}
	}

	if params.Config.GetBool("grpcserver.reflection.enabled") {
		for _, name := range reflectionServices {
			if registered[name] {
				return fmt.Errorf("cannot register the server reflection: %s already registered", name)
			}
		}

		reflection.Register(server)
	}

	return nil
}

// Address is the address of the gRPC server: grpcserver.address, then the listening address once
// started by [Serve] (useful with port 0).
type Address struct {
	mu    sync.RWMutex
	value string
}

// NewAddress returns the configured [*Address].
func NewAddress(cfg *config.Config) *Address {
	return &Address{value: cfg.GetStringOrDefault("grpcserver.address", DefaultAddress)}
}

// String returns the address.
func (a *Address) String() string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return a.value
}

func (a *Address) set(value string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.value = value
}

// Serve starts the gRPC server with the application, on grpcserver.address, and stops it
// gracefully when the application stops: in-flight calls are completed, unless the stop timeout
// expires first. Once started, the [*Address] is the listening address. If the server fails,
// the application is shut down.
func Serve() fx.Option {
	return fx.Invoke(func(lc fx.Lifecycle, shutdowner fx.Shutdowner, logger *slog.Logger, server *grpc.Server, address *Address) {
		lc.Append(fx.Hook{
			OnStart: func(ctx context.Context) error {
				listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", address.String())
				if err != nil {
					return fmt.Errorf("cannot start grpc server on %s: %w", address, err)
				}

				address.set(listener.Addr().String())

				go func() {
					// Serve returns nil once stopped.
					if err := server.Serve(listener); err != nil {
						logger.Error("grpc server stopped", "error", err)

						_ = shutdowner.Shutdown(fx.ExitCode(1))
					}
				}()

				logger.DebugContext(ctx, "grpc server started", "address", address.String())

				return nil
			},
			OnStop: func(ctx context.Context) error {
				stopped := make(chan struct{})

				go func() {
					server.GracefulStop()
					close(stopped)
				}()

				select {
				case <-stopped:
				case <-ctx.Done():
					server.Stop()
					<-stopped
				}

				return nil
			},
		})
	})
}
