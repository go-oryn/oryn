# Oryn

[![CI](https://github.com/go-oryn/oryn/actions/workflows/ci.yml/badge.svg)](https://github.com/go-oryn/oryn/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/go-oryn/oryn/graph/badge.svg)](https://codecov.io/gh/go-oryn/oryn)
[![Go Reference](https://pkg.go.dev/badge/github.com/go-oryn/oryn/core.svg)](https://pkg.go.dev/github.com/go-oryn/oryn/core)

> A modular Go framework built on [fx](https://uber-go.github.io/fx/), OpenTelemetry native.

Oryn applications are assembled from modules glued together by fx dependency injection. Every module is instrumented with OpenTelemetry out of the box, configured with the standard `OTEL_*` environment variables, and testable with in-memory telemetry.

```go
func main() {
	core.New(
		core.WithConfigFS(configs.FS),
		httpserver.Module,
		api.Module,
	).Run(httpserver.Serve())
}
```

## Modules

Each module is a Go module of this repository, versioned and released independently.

| Module | Description | Status |
|---|---|---|
| [core](core) | Runtime, configuration, OpenTelemetry, logging, health checks, test helpers | In progress |
| [httpserver](httpserver) | HTTP server on `net/http`, with handlers, middlewares and handler groups registered through fx | In progress |
| httpclient | Instrumented HTTP client | Planned |
| grpcserver | gRPC server, with services and interceptors registered through fx | Planned |
| grpcclient | Instrumented gRPC client connections | Planned |
| worker, cron, validator, sql, mcpserver | | Later |

```shell
go get github.com/go-oryn/oryn/core
```

Go 1.26 or later is required.

## Related repositories

- [go-oryn/template](https://github.com/go-oryn/template): application template.
- [go-oryn/demo](https://github.com/go-oryn/demo): demo application using all the modules.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE)
