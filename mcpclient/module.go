// Package mcpclient provides [Model Context Protocol] client sessions to the MCP servers of the
// configuration, built on the official [MCP Go SDK], instrumented with OpenTelemetry.
//
// Servers are named in the configuration:
//
//	mcpclient:
//	  servers:
//	    docs:
//	      url: https://docs.example.com/mcp
//	      headers:
//	        Authorization: Bearer ${DOCS_TOKEN}
//
// and their sessions are opened, then reused, with the [*Factory]:
//
//	func NewSearch(factory *mcpclient.Factory) *Search {
//		return &Search{factory: factory}
//	}
//
//	func (s *Search) Search(ctx context.Context, query string) (*mcp.CallToolResult, error) {
//		session, err := s.factory.Session(ctx, "docs")
//		if err != nil {
//			return nil, err
//		}
//
//		return session.CallTool(ctx, &mcp.CallToolParams{Name: "search", Arguments: map[string]any{"query": query}})
//	}
//
// [Model Context Protocol]: https://modelcontextprotocol.io
// [MCP Go SDK]: https://github.com/modelcontextprotocol/go-sdk
package mcpclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core/config"
)

// ModuleName is the name of the mcpclient fx module.
const ModuleName = "mcpclient"

// Module provides the [*Factory] of the MCP client sessions.
var Module = fx.Module(
	ModuleName,
	fx.Provide(NewFactory),
)

// ErrClosed is returned by [Factory.Session] once the application is stopped.
var ErrClosed = errors.New("mcp client factory closed")

// NewFactoryParams are the dependencies of [NewFactory].
type NewFactoryParams struct {
	fx.In

	Lifecycle      fx.Lifecycle
	Config         *config.Config
	Logger         *slog.Logger
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	Propagator     propagation.TextMapPropagator
	Transports     []namedTransport `group:"oryn-mcpclient-transports"`
}

// Factory opens the client sessions to the MCP servers, and keeps them open for reuse. The
// sessions are closed when the application stops.
type Factory struct {
	config          *config.Config
	logger          *slog.Logger
	tracerProvider  trace.TracerProvider
	meterProvider   metric.MeterProvider
	propagator      propagation.TextMapPropagator
	instrumentation *instrumentation
	transports      map[string]TransportFunc

	mu       sync.Mutex
	sessions map[string]*mcp.ClientSession
	closed   bool
}

// NewFactory returns the [*Factory] of the MCP client sessions, closing them when the application stops.
func NewFactory(params NewFactoryParams) (*Factory, error) {
	instrumentation, err := newInstrumentation(params.TracerProvider, params.MeterProvider, params.Propagator,
		excludedMethods(params.Config.GetStringSlice("mcpclient.observability.exclude")))
	if err != nil {
		return nil, err
	}

	transports := make(map[string]TransportFunc, len(params.Transports))
	for _, transport := range params.Transports {
		transports[transport.name] = transport.transport
	}

	factory := &Factory{
		config:          params.Config,
		logger:          params.Logger,
		tracerProvider:  params.TracerProvider,
		meterProvider:   params.MeterProvider,
		propagator:      params.Propagator,
		instrumentation: instrumentation,
		transports:      transports,
		sessions:        map[string]*mcp.ClientSession{},
	}

	params.Lifecycle.Append(fx.StopHook(factory.close))

	return factory, nil
}

// Session returns the client session to the server name: opened on the first call, then reused.
// The server is either registered with [AsTransport], or configured with mcpclient.servers.<name>.url.
func (f *Factory) Session(ctx context.Context, name string) (*mcp.ClientSession, error) {
	name = normalize(name)

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.closed {
		return nil, ErrClosed
	}

	if session, ok := f.sessions[name]; ok {
		return session, nil
	}

	session, err := f.connect(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to the %s mcp server: %w", name, err)
	}

	f.sessions[name] = session

	return session, nil
}

// connect opens a session to the server name, with an instrumented client.
func (f *Factory) connect(ctx context.Context, name string) (*mcp.ClientSession, error) {
	transport, serverAttributes, err := f.transport(ctx, name)
	if err != nil {
		return nil, err
	}

	client := mcp.NewClient(
		&mcp.Implementation{Name: f.config.GetString("app.name"), Version: f.config.GetString("app.version")},
		&mcp.ClientOptions{Logger: sdkLogger(f.logger)},
	)

	var middlewares []mcp.Middleware

	if timeout := f.config.GetDuration(key(name, "timeout")); timeout > 0 {
		middlewares = append(middlewares, newTimeoutMiddleware(timeout))
	}

	middlewares = append(middlewares, f.instrumentation.middleware(serverAttributes))

	if f.config.GetBoolOrDefault("mcpclient.log.enabled", true) {
		middlewares = append(middlewares, newLogMiddleware(f.logger, name, f.instrumentation.exclude))
	}

	client.AddSendingMiddleware(middlewares...)

	// The session outlives the request opening it: it is not canceled with it.
	return client.Connect(context.WithoutCancel(ctx), transport, nil)
}

// transport returns the transport to the server name, and the attributes describing the server.
func (f *Factory) transport(ctx context.Context, name string) (mcp.Transport, []attribute.KeyValue, error) {
	if transport, ok := f.transports[name]; ok {
		t, err := transport(ctx)

		return t, nil, err
	}

	endpoint := f.config.GetString(key(name, "url"))
	if endpoint == "" {
		return nil, nil, fmt.Errorf("unknown server: missing mcpclient.servers.%s.url", name)
	}

	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return nil, nil, fmt.Errorf("invalid mcpclient.servers.%s.url %q", name, endpoint)
	}

	base := http.DefaultTransport
	if headers := f.config.GetStringMapString(key(name, "headers")); len(headers) > 0 {
		base = headerTransport{headers: headers, next: base}
	}

	httpClient := &http.Client{Transport: otelhttp.NewTransport(base,
		otelhttp.WithTracerProvider(f.tracerProvider),
		otelhttp.WithMeterProvider(f.meterProvider),
		otelhttp.WithPropagators(f.propagator),
	)}

	return &mcp.StreamableClientTransport{Endpoint: endpoint, HTTPClient: httpClient}, serverAttributes(u), nil
}

// close closes the open sessions.
func (f *Factory) close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.closed = true

	errs := make([]error, 0, len(f.sessions))
	for _, session := range f.sessions {
		errs = append(errs, session.Close())
	}

	f.sessions = nil

	return errors.Join(errs...)
}

// key returns the config key of a server setting.
func key(name string, setting string) string {
	return "mcpclient.servers." + name + "." + setting
}

// serverAttributes returns the server.address and server.port of the endpoint.
func serverAttributes(u *url.URL) []attribute.KeyValue {
	attributes := []attribute.KeyValue{semconv.ServerAddress(u.Hostname())}

	port := u.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443"}[u.Scheme]
	}

	if value, err := strconv.Atoi(port); err == nil {
		attributes = append(attributes, semconv.ServerPort(value))
	}

	return attributes
}

// headerTransport adds the configured headers to the requests.
type headerTransport struct {
	headers map[string]string
	next    http.RoundTripper
}

func (t headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())

	for name, value := range t.headers {
		r.Header.Set(name, value)
	}

	return t.next.RoundTrip(r)
}

// sdkLogger returns the logger of the MCP SDK: its info logs are logged at debug level, so they
// don't flood the application logs.
func sdkLogger(logger *slog.Logger) *slog.Logger {
	return slog.New(debugHandler{logger.Handler()})
}

// lowered returns the debug level for the levels below warn.
func lowered(level slog.Level) slog.Level {
	if level < slog.LevelWarn {
		return slog.LevelDebug
	}

	return level
}

// debugHandler logs the records below warn level at debug level.
type debugHandler struct {
	slog.Handler
}

func (h debugHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.Handler.Enabled(ctx, lowered(level))
}

func (h debugHandler) Handle(ctx context.Context, record slog.Record) error {
	record.Level = lowered(record.Level)

	return h.Handler.Handle(ctx, record)
}

func (h debugHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return debugHandler{h.Handler.WithAttrs(attrs)}
}

func (h debugHandler) WithGroup(name string) slog.Handler {
	return debugHandler{h.Handler.WithGroup(name)}
}
