---
title: Demo application
description: A complete oryn application managing tasks over HTTP, gRPC and MCP, with one domain module, a database, telemetry and in-memory tests.
---

The [go-oryn/demo](https://github.com/go-oryn/demo) repository is a complete oryn application: it manages tasks over an HTTP API, a gRPC API, and MCP tools for AI agents. The three APIs are thin layers over **one** `tasks` module, so a task created over gRPC is listed over HTTP or by an agent.

```
            HTTP :8080          gRPC :50051          MCP :8082
                │                    │                    │
          ┌─────▼─────┐        ┌─────▼─────┐        ┌─────▼─────┐
          │   http    │        │   grpc    │        │    mcp    │   internal/: one package per API
          └─────┬─────┘        └─────┬─────┘        └─────┬─────┘
                └────────────────────┼────────────────────┘
                              ┌──────▼──────┐
                              │    tasks    │   Service: rules and telemetry
                              │             │   Store: the Repository, SQL
                              └──────┬──────┘
                              SQLite / PostgreSQL
```

## Run it

```shell
git clone https://github.com/go-oryn/demo.git && cd demo
go run ./cmd/migrate   # create the SQLite database
go run ./cmd/serve     # HTTP, gRPC, MCP and health servers
```

```shell
curl -X POST localhost:8080/api/tasks -d '{"title": "Try oryn"}'
grpcurl -plaintext localhost:50051 tasks.v1.TasksService/ListTasks
claude mcp add --transport http oryn-demo http://localhost:8082/mcp
```

## Layout

| Path | Content |
|---|---|
| `app.go` | The application: the oryn modules and the demo modules, shared by the commands and the tests |
| `cmd/serve`, `cmd/migrate` | The commands, see [Entrypoints](entrypoints.md) |
| `internal/tasks/` | The domain module: `Service`, and its `Store` implemented by the `Repository`, see [Telemetry](telemetry.md) |
| `internal/http/`, `internal/grpc/`, `internal/mcp/` | The APIs on the `tasks.Service`, see [APIs](apis.md) |
| `proto/` | The protobuf definition of the gRPC API, and its generated code |
| `migrations/` | The SQL migrations, embedded |
| `configs/` | The configuration, per environment |
| `internal/demotest/` | Starts the application in tests, with in-memory API clients, see [Testing](testing.md) |
| `compose.yaml` | PostgreSQL and a Grafana observability stack |

## What it shows

| Feature | Where |
|---|---|
| One domain module reused by three APIs | `internal/tasks/module.go`, `app.go` |
| Several commands on one application | `cmd/`, [Entrypoints](entrypoints.md) |
| HTTP handlers, a handler group and middlewares | `internal/http/module.go` |
| A gRPC service, with health and reflection | `internal/grpc/service.go` |
| Typed MCP tools | `internal/mcp/tools.go` |
| Custom spans, metrics and logs, correlated with the API and SQL spans | `internal/tasks/service.go` |
| In-memory tests of the three APIs, asserting telemetry, with `fx.Replace` | `*_test.go`, `internal/demotest/` |
