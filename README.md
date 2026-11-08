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

## Documentation

**https://go-oryn.github.io/oryn/**: getting started, guides and configuration reference.

The documentation is written for humans and agents: the website is built from the Markdown files of [docs/](docs), and published for agents as [llms.txt](https://go-oryn.github.io/oryn/llms.txt) and [llms-full.txt](https://go-oryn.github.io/oryn/llms-full.txt).

## Modules

Each module is a Go module of this repository, versioned and released independently.

| Module | Description | Status | Coverage |
|---|---|---|---|
| [core](core) | Runtime, configuration, OpenTelemetry, logging, health checks, test helpers | In progress | [![core coverage](https://codecov.io/gh/go-oryn/oryn/graph/badge.svg?flag=core)](https://codecov.io/gh/go-oryn/oryn?flags%5B0%5D=core) |
| [httpserver](httpserver) | HTTP server on `net/http`, with handlers, middlewares and handler groups registered through fx | In progress | [![httpserver coverage](https://codecov.io/gh/go-oryn/oryn/graph/badge.svg?flag=httpserver)](https://codecov.io/gh/go-oryn/oryn?flags%5B0%5D=httpserver) |
| [grpcserver](grpcserver) | gRPC server, with services and interceptors registered through fx, health and reflection | In progress | [![grpcserver coverage](https://codecov.io/gh/go-oryn/oryn/graph/badge.svg?flag=grpcserver)](https://codecov.io/gh/go-oryn/oryn?flags%5B0%5D=grpcserver) |
| [mcpserver](mcpserver) | Model Context Protocol server, with tools, resources and prompts registered through fx | In progress | [![mcpserver coverage](https://codecov.io/gh/go-oryn/oryn/graph/badge.svg?flag=mcpserver)](https://codecov.io/gh/go-oryn/oryn?flags%5B0%5D=mcpserver) |
| [sql](sql) | `database/sql` pool to PostgreSQL, MySQL or SQLite, with migrations | In progress | [![sql coverage](https://codecov.io/gh/go-oryn/oryn/graph/badge.svg?flag=sql)](https://codecov.io/gh/go-oryn/oryn?flags%5B0%5D=sql) |
| httpclient | Instrumented HTTP client | Planned |  |
| grpcclient | Instrumented gRPC client connections | Planned |  |
| worker, cron, validator | | Later |  |

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
