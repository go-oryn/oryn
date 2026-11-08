---
title: Routing
description: Register handlers, middlewares and handler groups on the HTTP server.
---

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
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)

		return
	}

	_ = json.NewEncoder(w).Encode(user)
}

var Module = fx.Options(
	fx.Provide(NewUserRepository),
	httpserver.AsHandler("GET /users/{id}", NewGetUserHandler),
	httpserver.AsHandler("GET /ping", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "pong")
	}),
)
```

Useful patterns:

| Pattern | Matches |
|---|---|
| `GET /users/{id}` | `GET` (and `HEAD`) `/users/42` |
| `POST /users` | `POST /users` only |
| `/files/{path...}` | Any method, `/files/a/b/c`, with `path` = `a/b/c` |
| `GET /{$}` | `GET /` only, not every path |
| `GET /static/` | `GET` on every path under `/static/` |

The application fails to start if a pattern is invalid, or conflicts with another one.

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

Middlewares run in registration order, the first one being the outermost:

1. global middlewares,
2. group middlewares, outer groups first,
3. handler middlewares.

They run once the request is routed: `r.Pattern` and `r.PathValue()` are available. Requests matching no route (`404`, `405`) do not go through them.

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

| Option | Description |
|---|---|
| `WithHandler(pattern, handler, middlewares...)` | Adds a handler, with its own middlewares |
| `WithMiddlewares(middlewares...)` | Adds middlewares to all the handlers of the group and its sub groups, wherever it is placed |
| `WithGroup(prefix, options...)` | Adds a sub group, whose prefix is relative to the group one |

- The group patterns are relative to the prefix: `GET /users` in the group `/api/v1` serves `GET /api/v1/users`. They cannot have a host.
- A group middleware is built once, and shared by the group handlers.
