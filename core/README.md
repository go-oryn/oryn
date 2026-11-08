# core

[![Go Reference](https://pkg.go.dev/badge/github.com/go-oryn/oryn/core.svg)](https://pkg.go.dev/github.com/go-oryn/oryn/core)
[![codecov](https://codecov.io/gh/go-oryn/oryn/graph/badge.svg?flag=core)](https://app.codecov.io/gh/go-oryn/oryn?flags%5B0%5D=core)

> The foundation of every oryn application: [fx](https://uber-go.github.io/fx/) runtime, configuration, OpenTelemetry, logging, health checks and test helpers.

```shell
go get github.com/go-oryn/oryn/core
```

```go
func main() {
	core.New(
		core.WithConfigFS(configs.FS),
		// your modules...
	).Run()
}
```

## Documentation

| Page | Source |
|---|---|
| [Application](https://go-oryn.github.io/oryn/core/application/) | [docs/core/application.md](../docs/core/application.md) |
| [Configuration](https://go-oryn.github.io/oryn/core/configuration/) | [docs/core/configuration.md](../docs/core/configuration.md) |
| [OpenTelemetry and logging](https://go-oryn.github.io/oryn/core/opentelemetry/) | [docs/core/opentelemetry.md](../docs/core/opentelemetry.md) |
| [Health checks](https://go-oryn.github.io/oryn/core/health/) | [docs/core/health.md](../docs/core/health.md) |
| [Testing](https://go-oryn.github.io/oryn/core/testing/) | [docs/core/testing.md](../docs/core/testing.md) |
