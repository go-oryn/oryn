package otel

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core/config"
)

// NewMeterProviderParams are the dependencies of [NewMeterProvider].
type NewMeterProviderParams struct {
	fx.In

	Lifecycle fx.Lifecycle
	Config    *config.Config
	Resource  *resource.Resource
	Memory    *sdkmetric.ManualReader
	Options   []sdkmetric.Option `group:"oryn-otel-meter-provider-options"`
}

// NewMeterProvider returns the meter provider, and sets it as the global one.
// It is shut down, flushing the pending metrics, when the application stops.
func NewMeterProvider(params NewMeterProviderParams) (*sdkmetric.MeterProvider, error) {
	options := []sdkmetric.Option{sdkmetric.WithResource(params.Resource)}

	kind, err := exporterKind(params.Config, signalMetrics)
	if err != nil {
		return nil, err
	}

	var exporter sdkmetric.Exporter

	switch kind {
	case ExporterOTLP:
		exporter, err = newOTLPMetricExporter(params.Config)
	case ExporterConsole:
		exporter, err = stdoutmetric.New()
	case ExporterMemory:
		options = append(options, sdkmetric.WithReader(params.Memory))
	}

	if err != nil {
		return nil, fmt.Errorf("cannot create %s metric exporter: %w", kind, err)
	}

	if exporter != nil {
		var readerOptions []sdkmetric.PeriodicReaderOption

		// The periodic reader reads OTEL_METRIC_EXPORT_INTERVAL by itself.
		if interval := params.Config.GetDuration("otel.metrics.interval"); interval > 0 && !envSet("OTEL_METRIC_EXPORT_INTERVAL") {
			readerOptions = append(readerOptions, sdkmetric.WithInterval(interval))
		}

		options = append(options, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter, readerOptions...)))
	}

	provider := sdkmetric.NewMeterProvider(append(options, params.Options...)...)
	otel.SetMeterProvider(provider)

	params.Lifecycle.Append(fx.StopHook(provider.Shutdown))

	return provider, nil
}

func newOTLPMetricExporter(cfg *config.Config) (sdkmetric.Exporter, error) {
	settings, err := newOTLPSettings(cfg, signalMetrics)
	if err != nil {
		return nil, err
	}

	if settings.protocol == ProtocolGRPC {
		return newGRPCMetricExporter(settings)
	}

	return newHTTPMetricExporter(settings)
}

func newGRPCMetricExporter(settings otlpSettings) (sdkmetric.Exporter, error) {
	var opts []otlpmetricgrpc.Option
	if settings.endpointURL != "" {
		opts = append(opts, otlpmetricgrpc.WithEndpointURL(settings.endpointURL))
	}

	if len(settings.headers) > 0 {
		opts = append(opts, otlpmetricgrpc.WithHeaders(settings.headers))
	}

	return otlpmetricgrpc.New(context.Background(), opts...)
}

func newHTTPMetricExporter(settings otlpSettings) (sdkmetric.Exporter, error) {
	var opts []otlpmetrichttp.Option
	if settings.endpointURL != "" {
		opts = append(opts, otlpmetrichttp.WithEndpointURL(settings.endpointURL))
	}

	if len(settings.headers) > 0 {
		opts = append(opts, otlpmetrichttp.WithHeaders(settings.headers))
	}

	return otlpmetrichttp.New(context.Background(), opts...)
}
