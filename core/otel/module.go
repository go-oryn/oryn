// Package otel sets up OpenTelemetry: resource, propagator, and the tracer,
// meter and logger providers with their exporters.
//
// It follows the OpenTelemetry environment variables specification: a standard
// OTEL_* variable (OTEL_SERVICE_NAME, OTEL_EXPORTER_OTLP_ENDPOINT,
// OTEL_TRACES_EXPORTER, OTEL_TRACES_SAMPLER, ...) always wins over the
// equivalent "otel.*" config key. In the test environment, all signals are
// exported in memory, see the oteltest packages.
package otel

import (
	"context"

	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/contrib/propagators/autoprop"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/core/oteltest/logtest"
)

// ModuleName is the name of the otel fx module.
const ModuleName = "otel"

// Module provides the OpenTelemetry resource, propagator, providers, and an
// application [Telemetry] (logger, tracer and meter named after app.name).
var Module = fx.Module(
	ModuleName,
	fx.Provide(
		NewResource,
		NewPropagator,
		fx.Annotate(NewTracerProvider, fx.As(fx.Self()), fx.As(new(trace.TracerProvider))),
		fx.Annotate(NewMeterProvider, fx.As(fx.Self()), fx.As(new(metric.MeterProvider))),
		fx.Annotate(NewLoggerProvider, fx.As(fx.Self()), fx.As(new(log.LoggerProvider))),
		NewTracer,
		NewMeter,
		fx.Annotate(NewTelemetry, fx.As(fx.Self()), fx.As(new(Telemetry))),
		tracetest.NewInMemoryExporter,
		logtest.NewInMemoryExporter,
		sdkmetric.NewManualReader,
	),
	fx.Invoke(StartRuntimeMetrics),
)

// NewPropagator returns the text map propagator, and sets it as the global one.
// It reads OTEL_PROPAGATORS, then the otel.propagators config key, and defaults
// to W3C trace context and baggage.
func NewPropagator(cfg *config.Config) (propagation.TextMapPropagator, error) {
	propagator := autoprop.NewTextMapPropagator()

	if names := cfg.GetStringSlice("otel.propagators"); !envSet("OTEL_PROPAGATORS") && len(names) > 0 {
		var err error

		propagator, err = autoprop.TextMapPropagator(names...)
		if err != nil {
			return nil, err
		}
	}

	otel.SetTextMapPropagator(propagator)

	return propagator, nil
}

// NewTracer returns the application tracer, named after app.name.
func NewTracer(cfg *config.Config, provider trace.TracerProvider) trace.Tracer {
	return provider.Tracer(cfg.AppName(), trace.WithInstrumentationVersion(cfg.AppVersion()))
}

// NewMeter returns the application meter, named after app.name.
func NewMeter(cfg *config.Config, provider metric.MeterProvider) metric.Meter {
	return provider.Meter(cfg.AppName(), metric.WithInstrumentationVersion(cfg.AppVersion()))
}

// StartRuntimeMetrics starts the Go runtime metrics (memory, GC, goroutines...),
// unless otel.metrics.runtime is false. They are disabled by default in the test environment.
func StartRuntimeMetrics(lc fx.Lifecycle, cfg *config.Config, provider metric.MeterProvider) {
	if !cfg.GetBoolOrDefault("otel.metrics.runtime", !cfg.IsTestEnv()) {
		return
	}

	lc.Append(fx.StartHook(func(context.Context) error {
		return runtime.Start(runtime.WithMeterProvider(provider))
	}))
}
