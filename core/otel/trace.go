package otel

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core/config"
)

// NewTracerProviderParams are the dependencies of [NewTracerProvider].
type NewTracerProviderParams struct {
	fx.In

	Lifecycle fx.Lifecycle
	Config    *config.Config
	Resource  *resource.Resource
	Memory    *tracetest.InMemoryExporter
	Options   []sdktrace.TracerProviderOption `group:"oryn-otel-tracer-provider-options"`
}

// NewTracerProvider returns the tracer provider, and sets it as the global one.
// It is shut down, flushing the pending spans, when the application stops.
func NewTracerProvider(params NewTracerProviderParams) (*sdktrace.TracerProvider, error) {
	options := []sdktrace.TracerProviderOption{sdktrace.WithResource(params.Resource)}

	sampler, err := newSampler(params.Config)
	if err != nil {
		return nil, err
	}

	if sampler != nil {
		options = append(options, sdktrace.WithSampler(sampler))
	}

	kind, err := exporterKind(params.Config, signalTraces)
	if err != nil {
		return nil, err
	}

	var exporter sdktrace.SpanExporter

	switch kind {
	case ExporterOTLP:
		exporter, err = newOTLPSpanExporter(params.Config)
	case ExporterConsole:
		exporter, err = stdouttrace.New()
	case ExporterMemory:
		exporter = params.Memory
	}

	if err != nil {
		return nil, fmt.Errorf("cannot create %s span exporter: %w", kind, err)
	}

	switch {
	case kind == ExporterOTLP:
		options = append(options, sdktrace.WithBatcher(exporter))
	case exporter != nil:
		options = append(options, sdktrace.WithSyncer(exporter))
	}

	provider := sdktrace.NewTracerProvider(append(options, params.Options...)...)
	otel.SetTracerProvider(provider)

	params.Lifecycle.Append(fx.StopHook(provider.Shutdown))

	return provider, nil
}

func newOTLPSpanExporter(cfg *config.Config) (sdktrace.SpanExporter, error) {
	settings, err := newOTLPSettings(cfg, signalTraces)
	if err != nil {
		return nil, err
	}

	if settings.protocol == ProtocolGRPC {
		return newGRPCSpanExporter(settings)
	}

	return newHTTPSpanExporter(settings)
}

func newGRPCSpanExporter(settings otlpSettings) (sdktrace.SpanExporter, error) {
	var opts []otlptracegrpc.Option
	if settings.endpointURL != "" {
		opts = append(opts, otlptracegrpc.WithEndpointURL(settings.endpointURL))
	}

	if len(settings.headers) > 0 {
		opts = append(opts, otlptracegrpc.WithHeaders(settings.headers))
	}

	return otlptracegrpc.New(context.Background(), opts...)
}

func newHTTPSpanExporter(settings otlpSettings) (sdktrace.SpanExporter, error) {
	var opts []otlptracehttp.Option
	if settings.endpointURL != "" {
		opts = append(opts, otlptracehttp.WithEndpointURL(settings.endpointURL))
	}

	if len(settings.headers) > 0 {
		opts = append(opts, otlptracehttp.WithHeaders(settings.headers))
	}

	return otlptracehttp.New(context.Background(), opts...)
}

// newSampler returns the sampler configured with otel.traces.sampler and
// otel.traces.sampler_arg, or nil to let the SDK apply OTEL_TRACES_SAMPLER
// (default: parentbased_always_on).
func newSampler(cfg *config.Config) (sdktrace.Sampler, error) {
	name := strings.ToLower(cfg.GetString("otel.traces.sampler"))
	if envSet("OTEL_TRACES_SAMPLER") || name == "" {
		return nil, nil //nolint:nilnil // nil means "use the SDK default".
	}

	ratio := cfg.GetFloat64OrDefault("otel.traces.sampler_arg", 1)

	switch name {
	case "always_on":
		return sdktrace.AlwaysSample(), nil
	case "always_off":
		return sdktrace.NeverSample(), nil
	case "traceidratio":
		return sdktrace.TraceIDRatioBased(ratio), nil
	case "parentbased_always_on":
		return sdktrace.ParentBased(sdktrace.AlwaysSample()), nil
	case "parentbased_always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample()), nil
	case "parentbased_traceidratio":
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio)), nil
	default:
		return nil, fmt.Errorf("unsupported otel.traces.sampler %q", name)
	}
}
