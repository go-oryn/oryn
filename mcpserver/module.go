// Package mcpserver provides a [Model Context Protocol] server, built on the official
// [MCP Go SDK], instrumented with OpenTelemetry.
//
// Tools, resources and prompts are registered with [AsCapability], and built by fx with
// their dependencies. The streamable HTTP transport is started with [Serve]:
//
//	core.New(
//		mcpserver.Module,
//		mcpserver.AsCapability(NewGreetTool),
//	).Run(mcpserver.Serve())
//
// with:
//
//	type GreetInput struct {
//		Name string `json:"name" jsonschema:"the name to greet"`
//	}
//
//	func NewGreetTool() mcpserver.CapabilityFunc {
//		return func(server *mcp.Server) error {
//			mcp.AddTool(server, &mcp.Tool{Name: "greet", Description: "Greets someone"},
//				func(ctx context.Context, req *mcp.CallToolRequest, in GreetInput) (*mcp.CallToolResult, any, error) {
//					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "hello " + in.Name}}}, nil, nil
//				})
//
//			return nil
//		}
//	}
//
// [Model Context Protocol]: https://modelcontextprotocol.io
// [MCP Go SDK]: https://github.com/modelcontextprotocol/go-sdk
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core/config"
)

// ModuleName is the name of the mcpserver fx module.
const ModuleName = "mcpserver"

// Defaults of the config keys.
const (
	DefaultAddress           = ":8082"
	DefaultPath              = "/mcp"
	DefaultSessionTimeout    = 30 * time.Minute
	DefaultReadHeaderTimeout = 10 * time.Second
)

// Module provides the [*mcp.Server], with the registered capabilities, and the [*HTTPServer]
// serving it with the streamable HTTP transport.
var Module = fx.Module(
	ModuleName,
	fx.Provide(NewServer, NewHTTPServer),
)

// NewServerParams are the dependencies of [NewServer].
type NewServerParams struct {
	fx.In

	Config         *config.Config
	Logger         *slog.Logger
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	Propagator     propagation.TextMapPropagator
	Capabilities   []registeredCapability `group:"oryn-mcpserver-capabilities"`
}

// NewServer returns the MCP server, named after app.name and app.version, with the registered
// capabilities. Every request goes through, from the outermost:
//
//  1. the OpenTelemetry instrumentation (spans and metrics), skipped for the excluded methods,
//  2. the request logs, unless disabled, and skipped for the excluded methods,
//  3. the panic recovery,
//  4. the tool, resource or prompt handler.
func NewServer(params NewServerParams) (*mcp.Server, error) {
	cfg := params.Config

	server := mcp.NewServer(
		&mcp.Implementation{Name: cfg.GetString("app.name"), Version: cfg.GetString("app.version")},
		&mcp.ServerOptions{Logger: sdkLogger(params.Logger), Capabilities: &mcp.ServerCapabilities{}},
	)

	exclude := excludedMethods(cfg.GetStringSlice("mcpserver.observability.exclude"))

	instrumentation, err := newOTelMiddleware(params.TracerProvider, params.MeterProvider, params.Propagator, exclude)
	if err != nil {
		return nil, err
	}

	middlewares := []mcp.Middleware{instrumentation}

	if cfg.GetBoolOrDefault("mcpserver.log.enabled", true) {
		middlewares = append(middlewares, newLogMiddleware(params.Logger, exclude))
	}

	server.AddReceivingMiddleware(append(middlewares, newRecoverMiddleware(params.Logger))...)

	for _, capability := range sortCapabilities(params.Capabilities) {
		if err := capability.Register(server); err != nil {
			return nil, fmt.Errorf("cannot register mcp capability: %w", err)
		}
	}

	return server, nil
}

// HTTPServer is the HTTP server of the streamable HTTP transport. It is a dedicated type, so it
// does not collide with the [*http.Server] of the httpserver module in the container.
type HTTPServer struct {
	*http.Server
}

// NewHTTPServerParams are the dependencies of [NewHTTPServer].
type NewHTTPServerParams struct {
	fx.In

	Config         *config.Config
	Logger         *slog.Logger
	Server         *mcp.Server
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	Propagator     propagation.TextMapPropagator
}

// NewHTTPServer returns the HTTP server of the streamable HTTP transport, serving the MCP server on
// mcpserver.path, instrumented with otelhttp.
func NewHTTPServer(params NewHTTPServerParams) *HTTPServer {
	cfg := params.Config

	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return params.Server },
		&mcp.StreamableHTTPOptions{
			Stateless:      cfg.GetBool("mcpserver.stateless"),
			JSONResponse:   cfg.GetBool("mcpserver.json_response"),
			SessionTimeout: cfg.GetDurationOrDefault("mcpserver.session_timeout", DefaultSessionTimeout),
			Logger:         sdkLogger(params.Logger),
		},
	)

	path := cfg.GetStringOrDefault("mcpserver.path", DefaultPath)

	mux := http.NewServeMux()
	mux.Handle(path, otelhttp.NewHandler(withSpanHeaders(params.Propagator, handler), ModuleName,
		otelhttp.WithTracerProvider(params.TracerProvider),
		otelhttp.WithMeterProvider(params.MeterProvider),
		otelhttp.WithPropagators(params.Propagator),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string { return r.Method + " " + path }),
	))

	return &HTTPServer{Server: &http.Server{
		Addr:              cfg.GetStringOrDefault("mcpserver.address", DefaultAddress),
		Handler:           mux,
		ReadHeaderTimeout: cfg.GetDurationOrDefault("mcpserver.read_header_timeout", DefaultReadHeaderTimeout),
		ErrorLog:          slog.NewLogLogger(params.Logger.Handler(), slog.LevelError),
	}}
}

// Serve starts the streamable HTTP transport with the application, on mcpserver.address, and shuts
// it down gracefully when the application stops. Once started, the server Addr is the listening
// address (useful with port 0). If the server fails, the application is shut down.
func Serve() fx.Option {
	return fx.Invoke(func(lc fx.Lifecycle, shutdowner fx.Shutdowner, logger *slog.Logger, server *HTTPServer) {
		lc.Append(fx.Hook{
			OnStart: func(ctx context.Context) error {
				listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", server.Addr)
				if err != nil {
					return fmt.Errorf("cannot start mcp server on %s: %w", server.Addr, err)
				}

				server.Addr = listener.Addr().String()

				go func() {
					if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
						logger.Error("mcp server stopped", "error", err)

						_ = shutdowner.Shutdown(fx.ExitCode(1))
					}
				}()

				logger.DebugContext(ctx, "mcp server started", "address", server.Addr)

				return nil
			},
			OnStop: server.Shutdown,
		})
	})
}

// sdkLogger returns the logger of the MCP SDK: its info logs (sessions connecting...) are logged
// at debug level, so they don't flood the application logs.
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

// withSpanHeaders injects the trace context of the HTTP server span in the request headers. The MCP
// SDK runs the handlers in the context of the session, not of the request: the MCP spans get their
// parent from these headers instead.
func withSpanHeaders(propagator propagation.TextMapPropagator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		propagator.Inject(r.Context(), propagation.HeaderCarrier(r.Header))

		next.ServeHTTP(w, r)
	})
}
