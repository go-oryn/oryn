# httpserver

[![Go Reference](https://pkg.go.dev/badge/github.com/go-oryn/oryn/httpserver.svg)](https://pkg.go.dev/github.com/go-oryn/oryn/httpserver)
[![codecov](https://codecov.io/gh/go-oryn/oryn/graph/badge.svg?flag=httpserver)](https://app.codecov.io/gh/go-oryn/oryn?flags%5B0%5D=httpserver)

> HTTP server built on the standard [http.ServeMux](https://pkg.go.dev/net/http#ServeMux), with handlers, middlewares and handler groups registered through fx, and OpenTelemetry instrumentation.

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

`httpserver.Module` provides the `*http.Server`, and `httpserver.Serve()` starts it with the application.

## Handlers

`AsHandler` registers a handler for a [ServeMux pattern](https://pkg.go.dev/net/http#hdr-Patterns): `[METHOD ][HOST]/[PATH]`, with `{name}` wildcards read with `r.PathValue("name")`.

A handler is either:

- a value: an `http.Handler`, or a `func(http.ResponseWriter, *http.Request)`,
- or a constructor returning one, whose parameters are injected by fx.

```go
type GetUserHandler struct {
	repository *UserRepository
}

func NewGetUserHandler(repository *UserRepository) *GetUserHandler {
	return &GetUserHandler{repository: repository}
}

func (h *GetUserHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	user, err := h.repository.Find(r.Context(), r.PathValue("id"))
	// ...
}

var Module = fx.Options(
	httpserver.AsHandler("GET /users/{id}", NewGetUserHandler),
	httpserver.AsHandler("GET /ping", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "pong")
	}),
)
```

## Middlewares

A middleware is either:

- a value: a `func(http.Handler) http.Handler` (the usual Go middleware signature, so existing middlewares work as is), or an `httpserver.Middleware` (an interface with `Wrap(next http.Handler) http.Handler`),
- or a constructor returning one, whose parameters are injected by fx.

`AsMiddleware` registers global middlewares, applied to every handler. Middlewares can also be given to a single handler, after its pattern and handler:

```go
func NewAuthMiddleware(verifier *TokenVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !verifier.Verify(r) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

var Module = fx.Options(
	httpserver.AsMiddleware(NewAuthMiddleware),
	httpserver.AsHandler("DELETE /users/{id}", NewDeleteUserHandler, NewAuditMiddleware),
)
```

Middlewares run in registration order, the first one being the outermost: global middlewares, then group middlewares (outer groups first), then handler middlewares. They run once the request is routed: `r.Pattern` and `r.PathValue()` are available.

## Handler groups

`AsHandlerGroup` registers handlers sharing a path prefix and middlewares. Groups can be nested with `WithGroup`.

```go
httpserver.AsHandlerGroup("/api/v1",
	httpserver.WithMiddlewares(NewAuthMiddleware),
	httpserver.WithHandler("GET /users", NewListUsersHandler),
	httpserver.WithHandler("POST /users", NewCreateUserHandler, NewAuditMiddleware),
	httpserver.WithGroup("/admin",
		httpserver.WithMiddlewares(NewAdminMiddleware),
		httpserver.WithHandler("GET /stats", NewStatsHandler),
	),
)
```

| Request | Middlewares, in order |
|---|---|
| `GET /api/v1/users` | global, auth |
| `POST /api/v1/users` | global, auth, audit |
| `GET /api/v1/admin/stats` | global, auth, admin |

The group patterns are relative to the prefix, and cannot have a host. `WithMiddlewares` applies to all the handlers of the group, wherever it is placed. A group middleware is built once, and shared by the group handlers.

## Observability

Every request goes through, from the outermost:

1. the [otelhttp](https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp) instrumentation: a server span named after the route (`GET /users/{id}`), with the `http.route` attribute, and the HTTP server metrics,
2. the request log: `http request`, with `method`, `path`, `route`, `status`, `response_size` and `duration`, at error level for 5xx responses and info level otherwise,
3. the panic recovery: a panic is logged with its stack, and answered with a 500 if nothing was written yet,
4. the routing, the middlewares and the handler.

Logs are correlated with the request span. Paths matching `httpserver.observability.exclude` (e.g. health checks) get neither spans, metrics nor request logs.

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

## Testing

Tests run the application with [coretest](../core/coretest), and serve requests with the `*http.Server` handler, without listening on a port:

```go
func TestGetUser(t *testing.T) {
	var (
		server *http.Server
		spans  *tracetest.InMemoryExporter
	)

	coretest.Start(t, core.New(httpserver.Module, users.Module), fx.Populate(&server, &spans))

	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/users/42", nil))

	assert.Equal(t, http.StatusOK, recorder.Code)
	spantest.HasSpan(t, spans, spantest.WithName("GET /users/{id}"))
}
```

To test over the network, set `httpserver.address` to `127.0.0.1:0` and add `httpserver.Serve()`: once started, `server.Addr` is the listening address.
