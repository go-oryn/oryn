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

```yaml
httpserver:
  # Listen address.
  address: ":8080"                # default ":8080"
  # Server timeouts. 0 disables a timeout.
  read_header_timeout: 10s        # default 10s
  read_timeout: 0s                # default 0s
  write_timeout: 0s               # default 0s
  idle_timeout: 2m                # default 2m
  log:
    # Log every request.
    enabled: true                 # default true
  observability:
    # Go regular expressions matched against the request path, excluded from spans, metrics and request logs.
    exclude:                      # default []
      - "^/livez$"
      - "^/readyz$"
```

Like every key, they can be set with environment variables: `APP_HTTPSERVER_ADDRESS=:9090`.
