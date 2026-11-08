# httpserver

[![Go Reference](https://pkg.go.dev/badge/github.com/go-oryn/oryn/httpserver.svg)](https://pkg.go.dev/github.com/go-oryn/oryn/httpserver)
[![codecov](https://codecov.io/gh/go-oryn/oryn/graph/badge.svg?flag=httpserver)](https://app.codecov.io/gh/go-oryn/oryn?flags%5B0%5D=httpserver)

> HTTP server built on the standard [http.ServeMux](https://pkg.go.dev/net/http#ServeMux), with handlers, middlewares and handler groups registered through fx, and OpenTelemetry instrumentation.

```shell
go get github.com/go-oryn/oryn/httpserver
```

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

## Documentation

| Page | Source |
|---|---|
| [HTTP server](https://go-oryn.github.io/oryn/httpserver/overview/) (configuration) | [docs/httpserver/overview.md](../docs/httpserver/overview.md) |
| [Routing](https://go-oryn.github.io/oryn/httpserver/routing/) (handlers, middlewares, groups) | [docs/httpserver/routing.md](../docs/httpserver/routing.md) |
| [Observability](https://go-oryn.github.io/oryn/httpserver/observability/) | [docs/httpserver/observability.md](../docs/httpserver/observability.md) |
| [Testing](https://go-oryn.github.io/oryn/httpserver/testing/) | [docs/httpserver/testing.md](../docs/httpserver/testing.md) |
