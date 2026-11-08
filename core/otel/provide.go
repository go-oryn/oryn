package otel

import (
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.uber.org/fx"
)

// AsResourceOptions adds options to the resource, for example extra detectors
// (resource.WithContainer(), resource.WithHost()...).
func AsResourceOptions(options ...resource.Option) fx.Option {
	return supplyGroup(`group:"oryn-otel-resource-options"`, options)
}

// AsTracerProviderOptions adds options to the tracer provider, for example span processors.
func AsTracerProviderOptions(options ...sdktrace.TracerProviderOption) fx.Option {
	return supplyGroup(`group:"oryn-otel-tracer-provider-options"`, options)
}

// AsMeterProviderOptions adds options to the meter provider, for example readers or views.
func AsMeterProviderOptions(options ...sdkmetric.Option) fx.Option {
	return supplyGroup(`group:"oryn-otel-meter-provider-options"`, options)
}

// AsLoggerProviderOptions adds options to the logger provider, for example log processors.
func AsLoggerProviderOptions(options ...sdklog.LoggerProviderOption) fx.Option {
	return supplyGroup(`group:"oryn-otel-logger-provider-options"`, options)
}

// supplyGroup supplies each value to the value group described by tag, typed as T.
func supplyGroup[T any](tag string, values []T) fx.Option {
	fxOptions := make([]fx.Option, 0, len(values))

	for _, value := range values {
		fxOptions = append(fxOptions, fx.Supply(fx.Annotate(value, fx.As(new(T)), fx.ResultTags(tag))))
	}

	return fx.Options(fxOptions...)
}
