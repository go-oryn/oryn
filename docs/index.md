---
title: oryn
description: A modular Go framework built on fx, OpenTelemetry native, documented for humans and agents.
hero:
  tagline: A modular Go framework built on fx. OpenTelemetry native. Documented for humans and agents.
  image:
    file: ./assets/oryn-mark.png
    alt: oryn logo
  actions:
    - text: Get started
      link: /oryn/getting-started/
      icon: right-arrow
    - text: View on GitHub
      link: https://github.com/go-oryn/oryn
      icon: external
      variant: minimal
---

## What is oryn?

oryn is a Go framework to build services, such as HTTP, gRPC or MCP APIs, from small modules glued together by [fx](https://uber-go.github.io/fx/) dependency injection.

```go
func main() {
	core.New(
		core.WithConfigFS(configs.FS),
		httpserver.Module,
		httpserver.AsHandler("GET /hello/{name}", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "hello %s", r.PathValue("name"))
		}),
	).Run(httpserver.Serve())
}
```

## Modular

An application is the [core](core/application.md) module plus the modules it needs. Each module is a Go module of the repository, versioned and released on its own:

| Module | What it does |
|---|---|
| [core](core/application.md) | Runtime, configuration, OpenTelemetry, logging, health checks, test helpers |
| [httpserver](httpserver/overview.md) | HTTP server on `net/http`, with handlers, middlewares and handler groups |
| [grpcserver](grpcserver/overview.md) | gRPC server, with services, interceptors, health and reflection |
| [mcpserver](mcpserver/overview.md) | Model Context Protocol server, exposing tools, resources and prompts to AI agents |
| [sql](sql/overview.md) | `database/sql` pool to PostgreSQL, MySQL or SQLite, with migrations |
| httpclient, grpcclient | Planned |

Modules are extended the same way everywhere: you register your own handlers, middlewares, probes or options with `AsXxx` helpers, and fx builds them with their dependencies. There is no global state to set up and no registration order to get right.

To see them working together, run the [demo application](demo/overview.md): one tasks module served over HTTP, gRPC and MCP, in one process.

## OpenTelemetry instrumented

Traces, metrics and logs are built in, not bolted on:

- every module is instrumented: an HTTP request, a gRPC call, an MCP tool call or a SQL query gets a span, metrics and a correlated log, with no code,
- the standard `OTEL_*` environment variables work as usual, and always win over the config, so oryn fits any OpenTelemetry deployment,
- in tests, all telemetry is exported in memory, and asserted with the `oteltest` helpers.

See [OpenTelemetry](core/opentelemetry.md).

## Made for humans and agents

This documentation is written as plain Markdown files in the [`docs/`](https://github.com/go-oryn/oryn/tree/main/docs) directory of the repository: the website you are reading is built from them.

- **Humans** read this website.
- **Agents** (Claude Code, Cursor, Codex...) read the same content as Markdown: the `docs/` files, or [`llms.txt`](https://go-oryn.github.io/oryn/llms.txt) and [`llms-full.txt`](https://go-oryn.github.io/oryn/llms-full.txt) on this website.

The framework itself is designed to be easy for agents to use: one way to do each thing, explicit registrations, typed configuration keys documented in one place, and fast in-memory tests to check the result. See [For agents](agents.md).
