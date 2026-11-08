# core

[![Go Reference](https://pkg.go.dev/badge/github.com/go-oryn/oryn/core.svg)](https://pkg.go.dev/github.com/go-oryn/oryn/core)
[![codecov](https://codecov.io/gh/go-oryn/oryn/graph/badge.svg?flag=core)](https://app.codecov.io/gh/go-oryn/oryn?flags%5B0%5D=core)

> The foundation of every oryn application: [fx](https://uber-go.github.io/fx/) runtime, configuration, OpenTelemetry, logging and health checks.

```shell
go get github.com/go-oryn/oryn/core
```

## Quick start

```go
// configs/embed.go
package configs

import "embed"

// FS holds the config files. Embedding from inside the directory puts them at the root of FS.
//
//go:embed *.yaml */*.yaml
var FS embed.FS
```

```go
// main.go
package main

import (
	"github.com/go-oryn/oryn/core"

	"example.com/app/configs"
)

func main() {
	core.New(
		core.WithConfigFS(configs.FS),
		// your modules...
	).Run()
}
```

`core.New` returns an immutable `core.App`. `Run` starts it and blocks until `SIGINT` or `SIGTERM`, then stops it gracefully.

The container provides:

| Type | Package | What it is |
|---|---|---|
| `*config.Config` | `core/config` | The configuration |
| `*slog.Logger` | `log/slog` | The application logger, sent to OpenTelemetry |
| `otel.Telemetry` | `core/otel` | The logger, tracer and meter, in one dependency |
| `trace.Tracer`, `metric.Meter` | OpenTelemetry API | Named after `app.name` |
| `trace.TracerProvider`, `metric.MeterProvider`, `log.LoggerProvider` | OpenTelemetry API | For instrumentation libraries |
| `propagation.TextMapPropagator` | OpenTelemetry API | Context propagation |
| `*resource.Resource` | OpenTelemetry SDK | The resource describing the application |
| `*health.Checker` | `core/health` | Runs the health probes |
| `context.Context` | `context` | The base context, see `App.WithContext` |

## Configuration

Configuration comes from, lowest to highest precedence:

1. built-in defaults,
2. YAML or JSON files at the root of the config file systems,
3. the files of the sub directory named after the environment (`APP_ENV`), e.g. `prod/`,
4. environment variables prefixed with `APP_`: `http.server.address` is read from `APP_HTTP_SERVER_ADDRESS`,
5. values set with `config.WithValues`.

Files can reference environment variables with `${VAR}` or `${VAR:default}`.

```
configs/
├── app.yaml        # loaded in every environment
├── otel.yaml
├── dev/otel.yaml   # loaded when APP_ENV=dev, on top of the root files
└── test/app.yaml   # loaded in tests
```

Modules and applications register config options with `config.AsOptions`. Options apply in registration order: when two set the same key, the last registered wins.

## OpenTelemetry

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

- Exporters are `otlp`, `console`, `memory` or `none`. The protocol is `grpc` or `http/protobuf`.
- Signal specific variables (`OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`...) and the SDK tuning variables (`OTEL_BSP_*`, `OTEL_BLRP_*`...) work as usual.
- `otel.exporter.otlp.endpoint` is a base URL: like `OTEL_EXPORTER_OTLP_ENDPOINT`, `/v1/<signal>` is appended for HTTP.
- The resource attributes come, lowest to highest precedence, from the SDK, the config, `otel.AsResourceOptions`, then the environment variables.
- Go runtime metrics (memory, GC, goroutines...) are collected by default.

```yaml
# configs/dev/otel.yaml: print everything locally
otel:
  traces:
    exporter: console
  metrics:
    exporter: none
  logs:
    exporter: console
```

Extend the SDK with `otel.AsResourceOptions`, `otel.AsTracerProviderOptions`, `otel.AsMeterProviderOptions` and `otel.AsLoggerProviderOptions`.

## Logging

The `*slog.Logger` sends records to the OpenTelemetry logger provider through the [otelslog](https://pkg.go.dev/go.opentelemetry.io/contrib/bridges/otelslog) bridge, so logs are correlated with the active span when you use the `...Context` methods.

| Config key | Default | Description |
|---|---|---|
| `log.level` | `info` | `debug`, `info`, `warn` or `error` |
| `log.source` | `false` | Add the source code location |
| `log.console.enabled` | `false` | Also print records on stdout |
| `log.console.format` | `text` | `text` or `json` |
| `log.set_default` | `true` | Set the logger as `slog.Default()` |

At `debug` level, the fx events (provided constructors, lifecycle hooks...) are logged too. Add handlers with `log.AsHandler`.

## Health checks

Register probes with `health.AsProbe`, for all kinds of check or for some of them:

```go
type DatabaseProbe struct{ db *sql.DB }

func (p *DatabaseProbe) Name() string                    { return "database" }
func (p *DatabaseProbe) Check(ctx context.Context) error { return p.db.PingContext(ctx) }

health.AsProbe(NewDatabaseProbe, health.Readiness, health.Startup)
```

`health.Serve()` starts an HTTP server exposing `/livez`, `/readyz` and `/startupz`: `200` when healthy, `503` otherwise, and the probe results as JSON with `?verbose`. The readiness check fails as soon as the application starts shutting down.

| Config key | Default |
|---|---|
| `health.server.address` | `:8081` |
| `health.timeout` | `5s` |

## Testing

`coretest` runs the application in the `test` environment, where all telemetry is exported in memory. Assert on it with the `otel/oteltest` packages (`logtest`, `spantest`, `metrictest`):

```go
func TestGreet(t *testing.T) {
	var (
		spans *tracetest.InMemoryExporter
		logs  *logtest.InMemoryExporter
		meter *sdkmetric.ManualReader
	)

	coretest.Start(t, app, fx.Populate(&spans, &logs, &meter))

	// exercise the application...

	spantest.HasSpan(t, spans, spantest.WithName("greet"))
	logtest.HasRecord(t, logs, logtest.WithBody("greeted"), logtest.WithLevel(slog.LevelInfo))
	metrictest.HasMetric(t, meter, metrictest.WithName("greet.count"))
}
```

Test options override the application ones: use `config.AsOptions(config.WithValues(...))` for config, and `fx.Replace` or `fx.Decorate` for dependencies.
