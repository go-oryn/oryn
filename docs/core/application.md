---
title: Application
description: How an oryn application is created, assembled from modules, and run.
---

The `core` module is the foundation of every oryn application: the [fx](https://uber-go.github.io/fx/) runtime, [configuration](configuration.md), [OpenTelemetry and logging](opentelemetry.md), and [health checks](health.md).

```shell
go get github.com/go-oryn/oryn/core
```

## Create and run an application

```go
func main() {
	core.New(
		core.WithConfigFS(configs.FS),
		httpserver.Module,
		users.Module,
	).Run(httpserver.Serve())
}
```

- `core.New(options...)` returns a `core.App` made of the core module and the given [fx options](https://pkg.go.dev/go.uber.org/fx#Option): modules, constructors (`fx.Provide`), registrations (`httpserver.AsHandler`...).
- `core.WithConfigFS(fsys)` loads the configuration files of `fsys`, see [Configuration](configuration.md).
- `Run(options...)` starts the application with the extra options, blocks until `SIGINT` or `SIGTERM`, then stops it gracefully. It exits the process if the application fails to start.

- `RunOnce(options...)` starts the application then stops it right away, for one-shot commands whose work is done by start hooks, like `sql.Migrate()`. It exits the process on failure.

Options passed to `Run` are the things that run with the application, like servers: `httpserver.Serve()`, `health.Serve()`. Modules never start anything by themselves, so tests can use the same modules without opening ports.

`core.App` is immutable: `WithOptions(options...)` and `WithContext(ctx)` return a copy, so an `App` can be shared, for example between `main` and the tests.

| Method | Description |
|---|---|
| `New(options...)` | Creates the application |
| `WithOptions(options...)` | Returns a copy with more fx options |
| `WithContext(ctx)` | Returns a copy using `ctx` as base context, injected as `context.Context` |
| `Build(options...)` | Returns the `*fx.App`, without starting it |
| `Run(options...)` | Builds, starts, and waits for a shutdown signal |
| `RunOnce(options...)` | Builds, starts, then stops right away |
| `Options(options...)` | Returns all the fx options, to build an `fx.App` yourself |

## Several entrypoints

Share one `App` between several commands, each running it with its own options. For example, with one `main` package per command in `cmd/`:

```go
// app.go: the application, shared by the commands and the tests.
var App = core.New(core.WithConfigFS(configs.FS), httpserver.Module, sql.Module, sql.AsMigrations(migrations.FS), users.Module)
```

```go
// cmd/serve/main.go: go run ./cmd/serve
func main() {
	app.App.Run(health.Serve(), httpserver.Serve())
}
```

```go
// cmd/migrate/main.go: go run ./cmd/migrate
func main() {
	app.App.RunOnce(sql.Migrate())
}
```

The [demo application](../demo/overview.md) is organized this way.

## Organize the code in modules

Group the code of a feature in a module: a `fx.Options` (or `fx.Module`) value with its constructors and registrations.

```go
// users/module.go
package users

var Module = fx.Options(
	fx.Provide(NewRepository),
	httpserver.AsHandlerGroup("/users",
		httpserver.WithHandler("GET /{id}", NewGetUserHandler),
		httpserver.WithHandler("POST /{$}", NewCreateUserHandler),
	),
	health.AsProbe(NewRepositoryProbe, health.Readiness),
)
```

Extension points always follow the same pattern: an `AsXxx` helper registers a value, or a constructor whose parameters are injected by fx. Registrations are independent from each other: their order only matters where the documentation says so (middlewares, config options), and it is then the registration order.

## Provided dependencies

The core module provides, ready to be injected in any constructor:

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
| `context.Context` | `context` | The base context, see `WithContext` |

```go
type GreetService struct {
	telemetry otel.Telemetry
}

func NewGreetService(telemetry otel.Telemetry) *GreetService {
	return &GreetService{telemetry: telemetry}
}

func (s *GreetService) Greet(ctx context.Context, name string) string {
	ctx, span := s.telemetry.Tracer().Start(ctx, "greet")
	defer span.End()

	s.telemetry.Logger().InfoContext(ctx, "greeting", "name", name)

	return "hello " + name
}
```

At `debug` [log level](opentelemetry.md#logging), the fx events (provided constructors, invoked functions, lifecycle hooks) are logged too.
