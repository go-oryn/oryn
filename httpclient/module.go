// Package httpclient provides HTTP clients instrumented with OpenTelemetry: every request gets
// a client span, metrics and a log, and carries the trace context to the server it calls.
//
// [Module] provides an [*http.Client], and a [*Factory] to build more clients. Transport
// middlewares, adding headers or retrying for example, are registered with [AsMiddleware]:
//
//	core.New(
//		httpclient.Module,
//		httpclient.AsMiddleware(NewAuthMiddleware),
//		fx.Provide(NewWeatherClient), // func NewWeatherClient(client *http.Client) *WeatherClient
//	)
//
// Tests route the requests to an [http.Handler] in memory with the httpclienttest package.
package httpclient

import (
	"cmp"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core/config"
)

// ModuleName is the name of the httpclient fx module.
const ModuleName = "httpclient"

// Defaults of the config keys.
const (
	DefaultTimeout               = 30 * time.Second
	DefaultMaxIdleConns          = 100
	DefaultMaxIdleConnsPerHost   = 100
	DefaultIdleConnTimeout       = 90 * time.Second
	DefaultTLSHandshakeTimeout   = 10 * time.Second
	DefaultExpectContinueTimeout = 1 * time.Second
)

// Module provides the [BaseTransport], the [*Factory], and an [*http.Client] built by the factory.
var Module = fx.Module(
	ModuleName,
	fx.Provide(NewBaseTransport, NewFactory, NewClient),
)

// BaseTransport is the transport sending the requests on the network, under the instrumentation
// and the middlewares. Tests replace it to serve the requests in memory, see the httpclienttest package.
type BaseTransport struct {
	http.RoundTripper
}

// NewBaseTransport returns an [http.Transport] configured by the httpclient.transport.* keys.
func NewBaseTransport(cfg *config.Config) BaseTransport {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		transport = &http.Transport{}
	}

	// Clone the default transport: it keeps its proxy and dialer settings.
	transport = transport.Clone()
	transport.MaxIdleConns = cfg.GetIntOrDefault("httpclient.transport.max_idle_conns", DefaultMaxIdleConns)
	transport.MaxIdleConnsPerHost = cfg.GetIntOrDefault("httpclient.transport.max_idle_conns_per_host", DefaultMaxIdleConnsPerHost)
	transport.MaxConnsPerHost = cfg.GetInt("httpclient.transport.max_conns_per_host")
	transport.IdleConnTimeout = cfg.GetDurationOrDefault("httpclient.transport.idle_conn_timeout", DefaultIdleConnTimeout)
	transport.TLSHandshakeTimeout = cfg.GetDurationOrDefault("httpclient.transport.tls_handshake_timeout", DefaultTLSHandshakeTimeout)
	transport.ResponseHeaderTimeout = cfg.GetDuration("httpclient.transport.response_header_timeout")
	transport.ExpectContinueTimeout = cfg.GetDurationOrDefault("httpclient.transport.expect_continue_timeout", DefaultExpectContinueTimeout)

	return BaseTransport{RoundTripper: transport}
}

// NewFactoryParams are the dependencies of [NewFactory].
type NewFactoryParams struct {
	fx.In

	Config         *config.Config
	Logger         *slog.Logger
	Base           BaseTransport
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	Propagator     propagation.TextMapPropagator
	Middlewares    []registeredMiddleware `group:"oryn-httpclient-middlewares"`
}

// Factory builds instrumented HTTP clients, with the registered middlewares.
type Factory struct {
	base        http.RoundTripper
	logger      *slog.Logger
	log         bool
	timeout     time.Duration
	exclude     []*regexp.Regexp
	middlewares []Middleware
	otel        []otelhttp.Option
}

// NewFactory returns the [*Factory], configured by the httpclient.* keys.
func NewFactory(params NewFactoryParams) (*Factory, error) {
	cfg := params.Config

	exclude, err := excludePatterns(cfg)
	if err != nil {
		return nil, err
	}

	// fx shuffles the value groups: restore the registration order.
	slices.SortFunc(params.Middlewares, func(a, b registeredMiddleware) int { return cmp.Compare(a.sequence, b.sequence) })

	middlewares := make([]Middleware, 0, len(params.Middlewares))
	for _, middleware := range params.Middlewares {
		middlewares = append(middlewares, middleware.middleware)
	}

	return &Factory{
		base:        params.Base.RoundTripper,
		logger:      params.Logger,
		log:         cfg.GetBoolOrDefault("httpclient.log.enabled", true),
		timeout:     cfg.GetDurationOrDefault("httpclient.timeout", DefaultTimeout),
		exclude:     exclude,
		middlewares: middlewares,
		otel: []otelhttp.Option{
			otelhttp.WithTracerProvider(params.TracerProvider),
			otelhttp.WithMeterProvider(params.MeterProvider),
			otelhttp.WithPropagators(params.Propagator),
			otelhttp.WithSpanNameFormatter(spanName),
			otelhttp.WithFilter(func(r *http.Request) bool { return !excluded(exclude, r) }),
		},
	}, nil
}

// Option configures a client built by the [Factory].
type Option func(*options)

type options struct {
	transport   http.RoundTripper
	timeout     *time.Duration
	middlewares []Middleware
}

// WithTransport sets the base transport of the client, instead of the [BaseTransport].
// The instrumentation and the middlewares still wrap it.
func WithTransport(transport http.RoundTripper) Option {
	return func(o *options) {
		o.transport = transport
	}
}

// WithTimeout sets the timeout of the client, instead of httpclient.timeout. 0 means no timeout.
func WithTimeout(timeout time.Duration) Option {
	return func(o *options) {
		o.timeout = &timeout
	}
}

// WithMiddlewares adds middlewares to the client, applied after the registered ones.
func WithMiddlewares(middlewares ...Middleware) Option {
	return func(o *options) {
		o.middlewares = append(o.middlewares, middlewares...)
	}
}

// NewClient returns an instrumented client. A request goes through, from the outermost:
//
//  1. the registered middlewares, in registration order, then the ones of [WithMiddlewares],
//  2. the OpenTelemetry instrumentation: client span, metrics and trace context propagation,
//  3. the request log, unless disabled,
//  4. the base transport.
//
// The excluded URLs skip the instrumentation and the log.
func (f *Factory) NewClient(opts ...Option) *http.Client {
	o := options{transport: f.base, timeout: &f.timeout}
	for _, opt := range opts {
		opt(&o)
	}

	transport := o.transport

	if f.log {
		transport = newLogMiddleware(f.logger, f.exclude).Wrap(transport)
	}

	transport = otelhttp.NewTransport(transport, f.otel...)

	chain := append(slices.Clone(f.middlewares), o.middlewares...)
	for i := len(chain) - 1; i >= 0; i-- {
		transport = chain[i].Wrap(transport)
	}

	return &http.Client{Transport: transport, Timeout: *o.timeout}
}

// NewClient returns the default client of the [Factory].
func NewClient(factory *Factory) *http.Client {
	return factory.NewClient()
}

// spanName names the client spans after the HTTP method, as the semantic conventions specify.
func spanName(_ string, r *http.Request) string {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch,
		http.MethodDelete, http.MethodConnect, http.MethodOptions, http.MethodTrace:
		return r.Method
	default:
		return "HTTP"
	}
}

// excludePatterns compiles httpclient.observability.exclude.
func excludePatterns(cfg *config.Config) ([]*regexp.Regexp, error) {
	var patterns []*regexp.Regexp

	for _, expr := range cfg.GetStringSlice("httpclient.observability.exclude") {
		pattern, err := regexp.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("invalid httpclient.observability.exclude pattern %q: %w", expr, err)
		}

		patterns = append(patterns, pattern)
	}

	return patterns, nil
}
