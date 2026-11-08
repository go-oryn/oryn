---
title: Entrypoints
description: How the demo shares one application between several commands in cmd/, one serving HTTP, gRPC and MCP together, one applying the migrations.
---

The demo defines its application once, in `app.go`, and runs it from one `main` package per command in `cmd/`. Each command passes the options it needs to `Run` or `RunOnce`.

## The application

```go
// app.go
package demo

var App = core.New(
	core.WithConfigFS(configs.FS),
	httpserver.Module,
	grpcserver.Module,
	mcpserver.Module,
	sql.Module,
	sql.AsMigrations(migrations.FS),
	// The tasks domain, and the three APIs on it.
	tasks.Module,
	httpapi.Module,
	grpcapi.Module,
	mcpapi.Module,
)
```

Modules only provide and register things: nothing listens on a port and nothing touches the database until a command asks for it.

## serve: all the servers in one process

```go
// cmd/serve/main.go
func main() {
	demo.App.Run(
		health.Serve(),
		httpserver.Serve(),
		grpcserver.Serve(),
		mcpserver.Serve(),
	)
}
```

```shell
go run ./cmd/serve
```

| Server | Address | Config key |
|---|---|---|
| HTTP API | `:8080` | `httpserver.address` |
| gRPC API | `:50051` | `grpcserver.address` |
| MCP | `:8082/mcp` | `mcpserver.address`, `mcpserver.path` |
| Health checks | `:8081` | `health.server.address` |

They start together, share the same `tasks.Service` and database pool, and stop gracefully together on `SIGINT` or `SIGTERM`. If one fails, the whole application stops. To run one API alone, write a command passing only its `Serve()`.

## migrate: a one-shot command

```go
// cmd/migrate/main.go
func main() {
	demo.App.RunOnce(sql.Migrate())
}
```

```shell
go run ./cmd/migrate
```

`RunOnce` starts the application, which applies the pending migrations, then stops it. Only the database pool is built: fx builds what the invoked options need, so the servers and handlers are left out. Run it before deploying a new version, as a job or an init container.

## Why not cobra?

A `main` package per command is enough: each one is a few lines, `go run ./cmd/<name>` and `go build ./cmd/...` work out of the box, and there is no flag parsing to maintain, the configuration coming from the [config files and environment](../core/configuration.md). Add a CLI library when a command needs arguments.

## Build

```shell
go build -o bin/ ./cmd/...
bin/migrate && bin/serve
```
