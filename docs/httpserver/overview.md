---
title: HTTP server
description: The httpserver module, an HTTP server on net/http with handlers, middlewares and handler groups registered through fx.
---

The `httpserver` module provides an HTTP server built on the standard [http.ServeMux](https://pkg.go.dev/net/http#ServeMux), instrumented with OpenTelemetry.

```shell
go get github.com/go-oryn/oryn/httpserver
```

## Quick start

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

- `httpserver.Module` provides the `*http.Server`, with the registered handlers and middlewares.
- `httpserver.Serve()` starts it with the application, and shuts it down gracefully when the application stops: in-flight requests are completed. If the server fails while running, the application stops.
- Handlers, middlewares and handler groups are registered with `AsHandler`, `AsMiddleware` and `AsHandlerGroup`, see [Routing](routing.md).
- Every request is traced, measured and logged, see [Observability](observability.md).

## Configuration

The `httpserver.*` keys set the address, the timeouts, the request logs and the paths excluded from telemetry. See [Configuration](configuration.md).
