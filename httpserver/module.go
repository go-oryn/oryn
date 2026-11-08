// Package httpserver provides an HTTP server built on [http.ServeMux], instrumented with OpenTelemetry.
//
// Handlers, middlewares and handler groups are registered with [AsHandler], [AsMiddleware] and
// [AsHandlerGroup], and built by fx with their dependencies. The server is started with [Serve]:
//
//	core.New(
//		httpserver.Module,
//		httpserver.AsMiddleware(NewAuthMiddleware),
//		httpserver.AsHandler("GET /users/{id}", NewGetUserHandler),
//	).Run(httpserver.Serve())
package httpserver

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core/config"
)

// ModuleName is the name of the httpserver fx module.
const ModuleName = "httpserver"

// Defaults of the config keys.
const (
	DefaultAddress           = ":8080"
	DefaultReadHeaderTimeout = 10 * time.Second
	DefaultIdleTimeout       = 2 * time.Minute
)

// Module provides the [*http.Server], with the registered handlers and middlewares.
var Module = fx.Module(
	ModuleName,
	fx.Provide(NewServer),
)

// NewServerParams are the dependencies of [NewServer].
type NewServerParams struct {
	fx.In

	Config         *config.Config
	Logger         *slog.Logger
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	Propagator     propagation.TextMapPropagator
	Middlewares    []registeredMiddleware `group:"oryn-httpserver-middlewares"`
	Routes         []route                `group:"oryn-httpserver-routes"`
}

// NewServer returns the HTTP server. Every request goes through, from the outermost:
//
//  1. the OpenTelemetry instrumentation (spans and metrics), skipped for the excluded paths,
//  2. the request logs, unless disabled, and skipped for the excluded paths,
//  3. the panic recovery,
//  4. the routing, then the global, group and handler middlewares, and the handler.
func NewServer(params NewServerParams) (*http.Server, error) {
	cfg := params.Config

	exclude, err := excludePatterns(cfg)
	if err != nil {
		return nil, err
	}

	mux, err := newMux(params.Middlewares, params.Routes)
	if err != nil {
		return nil, err
	}

	var handler http.Handler = mux

	handler = newRecoverMiddleware(params.Logger).Wrap(handler)

	if cfg.GetBoolOrDefault("httpserver.log.enabled", true) {
		handler = newLogMiddleware(params.Logger, exclude).Wrap(handler)
	}

	handler = otelhttp.NewHandler(handler, ModuleName,
		otelhttp.WithTracerProvider(params.TracerProvider),
		otelhttp.WithMeterProvider(params.MeterProvider),
		otelhttp.WithPropagators(params.Propagator),
		otelhttp.WithFilter(func(r *http.Request) bool { return !excluded(exclude, r) }),
	)

	return &http.Server{
		Addr:              cfg.GetStringOrDefault("httpserver.address", DefaultAddress),
		Handler:           handler,
		ReadHeaderTimeout: cfg.GetDurationOrDefault("httpserver.read_header_timeout", DefaultReadHeaderTimeout),
		ReadTimeout:       cfg.GetDuration("httpserver.read_timeout"),
		WriteTimeout:      cfg.GetDuration("httpserver.write_timeout"),
		IdleTimeout:       cfg.GetDurationOrDefault("httpserver.idle_timeout", DefaultIdleTimeout),
		ErrorLog:          slog.NewLogLogger(params.Logger.Handler(), slog.LevelError),
	}, nil
}

// Serve starts the HTTP server with the application, on httpserver.address, and shuts it down
// gracefully when the application stops. Once started, the server Addr is the listening address
// (useful with port 0). If the server fails, the application is shut down.
func Serve() fx.Option {
	return fx.Invoke(func(lc fx.Lifecycle, shutdowner fx.Shutdowner, logger *slog.Logger, server *http.Server) {
		lc.Append(fx.Hook{
			OnStart: func(ctx context.Context) error {
				listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", server.Addr)
				if err != nil {
					return fmt.Errorf("cannot start http server on %s: %w", server.Addr, err)
				}

				server.Addr = listener.Addr().String()

				go func() {
					if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
						logger.Error("http server stopped", "error", err)

						_ = shutdowner.Shutdown(fx.ExitCode(1))
					}
				}()

				logger.DebugContext(ctx, "http server started", "address", server.Addr)

				return nil
			},
			OnStop: server.Shutdown,
		})
	})
}

// newMux registers the routes, in registration order, with the global middlewares then their own.
func newMux(middlewares []registeredMiddleware, routes []route) (*http.ServeMux, error) {
	// fx shuffles the value groups: restore the registration order.
	slices.SortFunc(middlewares, func(a, b registeredMiddleware) int { return cmp.Compare(a.sequence, b.sequence) })
	slices.SortFunc(routes, func(a, b route) int { return cmp.Compare(a.sequence, b.sequence) })

	global := make([]Middleware, 0, len(middlewares))
	for _, middleware := range middlewares {
		global = append(global, middleware.middleware)
	}

	mux := http.NewServeMux()

	for _, r := range routes {
		chain := append(slices.Clone(global), r.middlewares...)

		handler := r.handler
		for i := len(chain) - 1; i >= 0; i-- {
			handler = chain[i].Wrap(handler)
		}

		handler = withRouteAttribute(r.pattern, handler)

		if err := handle(mux, r.pattern, handler); err != nil {
			return nil, err
		}
	}

	return mux, nil
}

// withRouteAttribute sets the http.route attribute of the server span, which otelhttp only knows
// once the request is routed: it sets it on the metrics, but not on the span.
func withRouteAttribute(pattern string, next http.Handler) http.Handler {
	_, path, found := strings.Cut(pattern, " ")
	if !found {
		path = pattern
	}

	// Remove the host: invalid patterns without a path are rejected when registered.
	if i := strings.Index(path, "/"); i > 0 {
		path = path[i:]
	}

	routeAttribute := semconv.HTTPRoute(path)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		trace.SpanFromContext(r.Context()).SetAttributes(routeAttribute)

		next.ServeHTTP(w, r)
	})
}

// handle registers the handler, turning the panics of invalid or conflicting patterns into errors.
func handle(mux *http.ServeMux, pattern string, handler http.Handler) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("cannot register handler %q: %v", pattern, recovered)
		}
	}()

	mux.Handle(pattern, handler)

	return nil
}

// excludePatterns compiles httpserver.observability.exclude.
func excludePatterns(cfg *config.Config) ([]*regexp.Regexp, error) {
	var patterns []*regexp.Regexp

	for _, expr := range cfg.GetStringSlice("httpserver.observability.exclude") {
		pattern, err := regexp.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("invalid httpserver.observability.exclude pattern %q: %w", expr, err)
		}

		patterns = append(patterns, pattern)
	}

	return patterns, nil
}
