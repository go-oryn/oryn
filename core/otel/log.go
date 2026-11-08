package otel

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/core/otel/oteltest/logtest"
)

// NewLoggerProviderParams are the dependencies of [NewLoggerProvider].
type NewLoggerProviderParams struct {
	fx.In

	Lifecycle fx.Lifecycle
	Config    *config.Config
	Resource  *resource.Resource
	Memory    *logtest.InMemoryExporter
	Options   []sdklog.LoggerProviderOption `group:"oryn-otel-logger-provider-options"`
}

// NewLoggerProvider returns the logger provider, and sets it as the global one.
// It is shut down, flushing the pending log records, when the application stops.
func NewLoggerProvider(params NewLoggerProviderParams) (*sdklog.LoggerProvider, error) {
	options := []sdklog.LoggerProviderOption{sdklog.WithResource(params.Resource)}

	kind, err := exporterKind(params.Config, signalLogs)
	if err != nil {
		return nil, err
	}

	var exporter sdklog.Exporter

	switch kind {
	case ExporterOTLP:
		exporter, err = newOTLPLogExporter(params.Config)
	case ExporterConsole:
		exporter, err = stdoutlog.New()
	case ExporterMemory:
		exporter = params.Memory
	}

	if err != nil {
		return nil, fmt.Errorf("cannot create %s log exporter: %w", kind, err)
	}

	switch {
	case kind == ExporterOTLP:
		options = append(options, sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)))
	case exporter != nil:
		options = append(options, sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
	}

	provider := sdklog.NewLoggerProvider(append(options, params.Options...)...)
	otel.SetLoggerProvider(provider)

	params.Lifecycle.Append(fx.StopHook(provider.Shutdown))

	return provider, nil
}

func newOTLPLogExporter(cfg *config.Config) (sdklog.Exporter, error) {
	settings, err := newOTLPSettings(cfg, signalLogs)
	if err != nil {
		return nil, err
	}

	if settings.protocol == ProtocolGRPC {
		return newGRPCLogExporter(settings)
	}

	return newHTTPLogExporter(settings)
}

func newGRPCLogExporter(settings otlpSettings) (sdklog.Exporter, error) {
	var opts []otlploggrpc.Option
	if settings.endpointURL != "" {
		opts = append(opts, otlploggrpc.WithEndpointURL(settings.endpointURL))
	}

	if len(settings.headers) > 0 {
		opts = append(opts, otlploggrpc.WithHeaders(settings.headers))
	}

	return otlploggrpc.New(context.Background(), opts...)
}

func newHTTPLogExporter(settings otlpSettings) (sdklog.Exporter, error) {
	var opts []otlploghttp.Option
	if settings.endpointURL != "" {
		opts = append(opts, otlploghttp.WithEndpointURL(settings.endpointURL))
	}

	if len(settings.headers) > 0 {
		opts = append(opts, otlploghttp.WithHeaders(settings.headers))
	}

	return otlploghttp.New(context.Background(), opts...)
}
