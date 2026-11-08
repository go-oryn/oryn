---
title: For agents
description: How coding agents can read this documentation, and the rules to follow when writing oryn code.
---

This documentation is written for coding agents first: every page is a plain Markdown file, and the website is built from these files.

## Read the documentation

| Source | Use it when |
|---|---|
| [`llms.txt`](https://go-oryn.github.io/oryn/llms.txt) | You need the list of pages, with a description of each |
| [`llms-full.txt`](https://go-oryn.github.io/oryn/llms-full.txt) | You want the whole documentation in one file |
| [`llms-small.txt`](https://go-oryn.github.io/oryn/llms-small.txt) | You want a shorter version of it |
| [`docs/`](https://github.com/go-oryn/oryn/tree/main/docs) in the repository | You work in a checkout of the repository, or need one page |
| [pkg.go.dev](https://pkg.go.dev/github.com/go-oryn/oryn/core) | You need the API reference of a package |

To make a project built with oryn aware of it, add this to its `AGENTS.md` or `CLAUDE.md`:

```markdown
This project uses the oryn Go framework. Read https://go-oryn.github.io/oryn/llms.txt
before writing code, and follow the conventions of the oryn documentation.
```

## Rules for writing oryn code

- **Assemble the application with modules.** Start from `core.New(...)`, add the modules you need (`httpserver.Module`...), and group the application code in your own `fx.Options` modules.
- **Register, do not wire.** Use the `AsXxx` helpers to add handlers, services, tools, middlewares, probes, log handlers, config options or OpenTelemetry options: `httpserver.AsHandler`, `grpcserver.AsService`, `mcpserver.AsCapability`, `worker.AsWorker`, `cron.AsJob`, `validator.AsValidation`, `grpcclient.AsClient`, `httpclient.AsMiddleware`, `health.AsProbe`, `log.AsHandler`, `config.AsOptions`...
- **Inject dependencies in constructors.** A handler, middleware or probe can be given as a constructor: its parameters (`*config.Config`, `*slog.Logger`, `otel.Telemetry`, your services...) are injected by fx.
- **Start servers explicitly.** Modules never start anything by themselves: pass `httpserver.Serve()`, `grpcserver.Serve()`, `mcpserver.Serve()`, `health.Serve()`, `worker.Run()` or `cron.Run()` to `Run`, and one-shot work like `sql.Migrate()` to `RunOnce`.
- **Keep the APIs thin.** Put the business rules and their telemetry in a domain module, and expose it through HTTP handlers, gRPC services or MCP tools that only map requests and errors, like the [demo](demo/overview.md).
- **Configure with keys, not code.** Configuration keys are namespaced by module (`httpserver.address`, `log.level`...) and documented on each module page. Put them in YAML files, per environment.
- **Call other services with the client modules.** Use the `*http.Client` of `httpclient`, the connections of `grpcclient` and the sessions of `mcpclient`: they are instrumented and propagate the trace context.
- **Do not configure OpenTelemetry globally.** Use the providers from the container, and let the standard `OTEL_*` variables configure the exporters.
- **Log with the context.** Use the `...Context` methods of `*slog.Logger` (`InfoContext`...) so logs are correlated with the active span.
- **Test with coretest.** Start the application with `coretest.Start`, call it in memory (`httptest`, `grpcservertest.Dial`, `mcpservertest.Connect`), serve the services it calls in memory (`httpclienttest.Serve`, `grpcclienttest.Serve`, `mcpclienttest.Serve`), run jobs and workers on demand (`crontest.RunJob`, `workertest.Run`), and assert telemetry with the `oteltest` packages. Tests run in memory: no network, no collector.

## Check your work

```shell
go build ./...
go test -race ./...
```

A test asserting the span or the log of the code you added (see [Testing](core/testing.md)) proves both the behaviour and the instrumentation.
