---
title: OpenTelemetry
description: Traces, metrics and logs, the slog logger, exporters, sampling, and how the OTEL_* environment variables apply.
---

oryn sets up the OpenTelemetry SDK for the three signals: traces, metrics and logs. The modules use it to instrument what they do, and your code uses it through the injected `otel.Telemetry`, `trace.Tracer`, `metric.Meter` and `*slog.Logger`.

```go
func NewCheckoutService(telemetry otel.Telemetry) (*CheckoutService, error) {
	orders, err := telemetry.Meter().Int64Counter("checkout.orders")
	if err != nil {
		return nil, err
	}

	return &CheckoutService{telemetry: telemetry, orders: orders}, nil
}

func (s *CheckoutService) Checkout(ctx context.Context, cart Cart) error {
	ctx, span := s.telemetry.Tracer().Start(ctx, "checkout")
	defer span.End()

	s.orders.Add(ctx, 1)
	s.telemetry.Logger().InfoContext(ctx, "checked out", "items", len(cart.Items))

	return nil
}
```

The application tracer, meter and logger are named after `app.name`. Instrumentation libraries get the providers (`trace.TracerProvider`, `metric.MeterProvider`, `log.LoggerProvider`) and the `propagation.TextMapPropagator` from the container, and use their own instrumentation scope. oryn also sets them as the OpenTelemetry globals, for third party libraries that only use those.

## Logging

The application logger is a standard `*slog.Logger`. It sends records to the OpenTelemetry logger provider through the [otelslog](https://pkg.go.dev/go.opentelemetry.io/contrib/bridges/otelslog) bridge, so logs are exported with the traces and metrics.

```go
func NewGreeter(logger *slog.Logger) *Greeter {
	return &Greeter{logger: logger}
}

func (g *Greeter) Greet(ctx context.Context, name string) {
	g.logger.InfoContext(ctx, "greeting", "name", name)
}
```

Use the `...Context` methods (`InfoContext`, `ErrorContext`...): the record then gets the trace and span IDs of the active span, so logs and traces are linked in your observability backend.

| Config key | Default | Description |
|---|---|---|
| `log.level` | `info` | `debug`, `info`, `warn` or `error`, with an optional offset like `info+2` |
| `log.source` | `false` | Add the source code location |
| `log.console.enabled` | `false` | Also print records on stdout |
| `log.console.format` | `text` | `text` or `json` |
| `log.set_default` | `true` | Set the logger as `slog.Default()` |

At `debug` level, the fx events (provided constructors, invoked functions, lifecycle hooks) are logged too.

Register an extra `slog.Handler`, receiving all the records at or above `log.level`, with `log.AsHandler`:

```go
log.AsHandler(func() slog.Handler {
	return slog.NewJSONHandler(os.Stderr, nil)
})
```

## Standard environment variables win

oryn follows the [OpenTelemetry environment variables specification](https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/). **A standard `OTEL_*` variable always wins over the equivalent config key**, so an application deployed with the usual OpenTelemetry settings (Kubernetes operator, collector sidecar...) needs no config change.

| Config key | Environment variable | Default |
|---|---|---|
| `app.name` | `OTEL_SERVICE_NAME` | `app` |
| `app.version` | | `0.0.0` |
| `otel.resource.attributes` (map) | `OTEL_RESOURCE_ATTRIBUTES` | |
| `otel.disabled` | `OTEL_SDK_DISABLED` | `false` |
| `otel.propagators` (list) | `OTEL_PROPAGATORS` | `tracecontext,baggage` |
| `otel.traces.exporter` | `OTEL_TRACES_EXPORTER` | `otlp` |
| `otel.traces.sampler` | `OTEL_TRACES_SAMPLER` | `parentbased_always_on` |
| `otel.traces.sampler_arg` | `OTEL_TRACES_SAMPLER_ARG` | `1` |
| `otel.metrics.exporter` | `OTEL_METRICS_EXPORTER` | `otlp` |
| `otel.metrics.interval` | `OTEL_METRIC_EXPORT_INTERVAL` | `60s` |
| `otel.metrics.runtime` | | `true` (`false` in tests) |
| `otel.logs.exporter` | `OTEL_LOGS_EXPORTER` | `otlp` |
| `otel.exporter.otlp.protocol` | `OTEL_EXPORTER_OTLP_PROTOCOL` | `http/protobuf` |
| `otel.exporter.otlp.endpoint` | `OTEL_EXPORTER_OTLP_ENDPOINT` | `http://localhost:4318` |
| `otel.exporter.otlp.headers` (map) | `OTEL_EXPORTER_OTLP_HEADERS` | |

- Exporters are `otlp`, `console`, `memory` or `none`. The OTLP protocol is `grpc` or `http/protobuf`.
- Signal specific variables (`OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`...) and the SDK tuning variables (`OTEL_BSP_*`, `OTEL_BLRP_*`...) work as usual.
- `otel.exporter.otlp.endpoint` is a base URL: like `OTEL_EXPORTER_OTLP_ENDPOINT`, `/v1/<signal>` is appended for HTTP.
- Samplers are `always_on`, `always_off`, `traceidratio`, `parentbased_always_on`, `parentbased_always_off` and `parentbased_traceidratio`, with `sampler_arg` as ratio.
- Propagators are those of [autoprop](https://pkg.go.dev/go.opentelemetry.io/contrib/propagators/autoprop): `tracecontext`, `baggage`, `b3`, `b3multi`, `jaeger`, `xray`, `ottrace`.

## Local development

Print the telemetry on the console in the `dev` environment:

```yaml
# configs/dev/otel.yaml
otel:
  traces:
    exporter: console
  metrics:
    exporter: none
  logs:
    exporter: console
```

Or send it to a local collector or backend with OTLP, for example the [OpenTelemetry Collector](https://opentelemetry.io/docs/collector/) or [Jaeger](https://www.jaegertracing.io/), listening on `localhost:4318`.

## Resource

The resource describes the application. Its attributes come, lowest to highest precedence, from:

1. the SDK and the Go runtime,
2. the config: `service.name` (`app.name`), `service.version` (`app.version`), `deployment.environment.name` (the environment), and `otel.resource.attributes`,
3. the options registered with `otel.AsResourceOptions`, for example extra detectors,
4. the `OTEL_SERVICE_NAME` and `OTEL_RESOURCE_ATTRIBUTES` environment variables.

## Runtime metrics

Go runtime metrics (memory, garbage collection, goroutines...) are collected by default, except in tests. Disable them with `otel.metrics.runtime: false`.

## Extend the SDK

Register options for the SDK resource and providers:

```go
otel.AsResourceOptions(resource.WithHost(), resource.WithContainer())
otel.AsTracerProviderOptions(sdktrace.WithSpanProcessor(myProcessor))
otel.AsMeterProviderOptions(sdkmetric.WithView(myView))
otel.AsLoggerProviderOptions(sdklog.WithProcessor(myProcessor))
```

## Tests

In the `test` environment, all signals are exported in memory, whatever the configuration, and can be asserted with the `oteltest` packages. See [Testing](testing.md).
