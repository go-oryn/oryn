package httpserver_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/httpserver"
)

// structHandler is an http.Handler writing its body.
type structHandler struct {
	body string
}

func (h *structHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	_, _ = io.WriteString(w, h.body)
}

// newStructHandler is a handler constructor with a dependency.
func newStructHandler(cfg *config.Config) *structHandler {
	return &structHandler{body: cfg.AppName()}
}

// trace returns a middleware writing name before calling the next handler.
func trace(name string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, name+">")

			next.ServeHTTP(w, r)
		})
	}
}

// structMiddleware is a [httpserver.Middleware].
type structMiddleware struct {
	name string
}

func (m structMiddleware) Wrap(next http.Handler) http.Handler {
	return trace(m.name)(next)
}

func TestAsHandler(t *testing.T) {
	t.Parallel()

	t.Run("values and constructors", func(t *testing.T) {
		t.Parallel()

		server, _ := startServer(t,
			withValues(map[string]any{"app.name": "test_app"}),
			httpserver.AsHandler("GET /func", text("func")),
			httpserver.AsHandler("GET /handler-func", http.HandlerFunc(text("handler_func"))),
			httpserver.AsHandler("GET /handler", &structHandler{body: "handler"}),
			httpserver.AsHandler("GET /constructor", newStructHandler),
			httpserver.AsHandler("GET /func-constructor", func() http.HandlerFunc { return text("func_constructor") }),
		)

		for path, expected := range map[string]string{
			"/func":             "func",
			"/handler-func":     "handler_func",
			"/handler":          "handler",
			"/constructor":      "test_app",
			"/func-constructor": "func_constructor",
		} {
			status, body := do(t, server, http.MethodGet, path)
			assert.Equal(t, http.StatusOK, status, path)
			assert.Equal(t, expected, body, path)
		}
	})

	t.Run("patterns", func(t *testing.T) {
		t.Parallel()

		server, _ := startServer(t,
			httpserver.AsHandler("GET /users/{id}", func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, "user "+r.PathValue("id")) //nolint:gosec // test handler.
			}),
			httpserver.AsHandler("/any", text("any")),
		)

		_, body := do(t, server, http.MethodGet, "/users/42")
		assert.Equal(t, "user 42", body)

		status, _ := do(t, server, http.MethodPost, "/users/42")
		assert.Equal(t, http.StatusMethodNotAllowed, status)

		_, body = do(t, server, http.MethodDelete, "/any")
		assert.Equal(t, "any", body)
	})

	t.Run("middlewares", func(t *testing.T) {
		t.Parallel()

		server, _ := startServer(t,
			httpserver.AsMiddleware(trace("global1"), structMiddleware{name: "global2"}),
			httpserver.AsMiddleware(func() httpserver.Middleware { return httpserver.MiddlewareFunc(trace("global3")) }),
			httpserver.AsHandler("GET /", text("handler"), trace("handler1"), func() structMiddleware { return structMiddleware{name: "handler2"} }),
			httpserver.AsHandler("GET /other", text("other")),
		)

		_, body := do(t, server, http.MethodGet, "/")
		assert.Equal(t, "global1>global2>global3>handler1>handler2>handler", body)

		_, body = do(t, server, http.MethodGet, "/other")
		assert.Equal(t, "global1>global2>global3>other", body, "handler middlewares only apply to their handler")
	})

	t.Run("middlewares see the route", func(t *testing.T) {
		t.Parallel()

		server, _ := startServer(t,
			httpserver.AsMiddleware(func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.WriteString(w, r.Pattern+" "+r.PathValue("id")+">") //nolint:gosec // test handler.

					next.ServeHTTP(w, r)
				})
			}),
			httpserver.AsHandler("GET /users/{id}", text("handler")),
		)

		_, body := do(t, server, http.MethodGet, "/users/42")
		assert.Equal(t, "GET /users/{id} 42>handler", body)
	})

	t.Run("constructor error", func(t *testing.T) {
		t.Parallel()

		err := newServerError(t, httpserver.AsHandler("GET /", func() (http.HandlerFunc, error) {
			return nil, errors.New("test_error")
		}))
		require.ErrorContains(t, err, "test_error")
	})

	t.Run("invalid registrations", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct {
			registration func() error
			err          string
		}{
			"nil handler": {
				registration: func() error { return newServerError(t, httpserver.AsHandler("GET /", nil)) },
				err:          `cannot register handler "GET /": nil handler or middleware`,
			},
			"unsupported handler": {
				registration: func() error { return newServerError(t, httpserver.AsHandler("GET /", "test_string")) },
				err:          "unsupported string: expected a value or a constructor returning one",
			},
			"constructor not returning a handler": {
				registration: func() error { return newServerError(t, httpserver.AsHandler("GET /", func() string { return "" })) },
				err:          "unsupported func() string",
			},
			"unsupported handler middleware": {
				registration: func() error { return newServerError(t, httpserver.AsHandler("GET /", text(""), 42)) },
				err:          `cannot register middleware of the handler "GET /": unsupported int`,
			},
			"unsupported global middleware": {
				registration: func() error { return newServerError(t, httpserver.AsMiddleware(text(""))) },
				err:          "cannot register middleware: unsupported func(http.ResponseWriter, *http.Request)",
			},
			"invalid pattern": {
				registration: func() error { return newServerError(t, httpserver.AsHandler("GET users", text(""))) },
				err:          `cannot register handler "GET users"`,
			},
			"conflicting patterns": {
				registration: func() error {
					return newServerError(t, httpserver.AsHandler("GET /", text("")), httpserver.AsHandler("GET /", text("")))
				},
				err: `cannot register handler "GET /"`,
			},
		} {
			err := tc.registration()
			require.Error(t, err, name)
			assert.Contains(t, err.Error(), tc.err, name)
		}
	})
}

func TestAsHandlerGroup(t *testing.T) {
	t.Parallel()

	t.Run("prefixes and middlewares", func(t *testing.T) {
		t.Parallel()

		server, _ := startServer(t,
			httpserver.AsMiddleware(trace("global")),
			httpserver.AsHandlerGroup("/api/",
				httpserver.WithHandler("GET /users", text("users"), trace("handler")),
				httpserver.WithHandler("/any", text("any")),
				httpserver.WithGroup("/admin",
					httpserver.WithHandler("GET /stats", text("stats")),
					httpserver.WithMiddlewares(trace("admin")),
				),
				// Applies to all the group handlers, wherever it is placed.
				httpserver.WithMiddlewares(trace("api1"), trace("api2")),
			),
			httpserver.AsHandlerGroup("/",
				httpserver.WithHandler("GET /root", text("root")),
			),
		)

		for path, expected := range map[string]string{
			"/api/users":       "global>api1>api2>handler>users",
			"/api/any":         "global>api1>api2>any",
			"/api/admin/stats": "global>api1>api2>admin>stats",
			"/root":            "global>root",
		} {
			status, body := do(t, server, http.MethodGet, path)
			assert.Equal(t, http.StatusOK, status, path)
			assert.Equal(t, expected, body, path)
		}

		status, _ := do(t, server, http.MethodGet, "/users")
		assert.Equal(t, http.StatusNotFound, status)
	})

	t.Run("group middlewares are built once", func(t *testing.T) {
		t.Parallel()

		var built atomic.Int64

		server, _ := startServer(t,
			httpserver.AsHandlerGroup("/api",
				httpserver.WithMiddlewares(func() func(http.Handler) http.Handler {
					built.Add(1)

					return trace("api")
				}),
				httpserver.WithHandler("GET /a", text("a")),
				httpserver.WithHandler("GET /b", text("b")),
				httpserver.WithGroup("/sub", httpserver.WithHandler("GET /c", text("c"))),
			),
		)

		for _, path := range []string{"/api/a", "/api/b", "/api/sub/c"} {
			_, body := do(t, server, http.MethodGet, path)
			assert.True(t, strings.HasPrefix(body, "api>"), path)
		}

		assert.Equal(t, int64(1), built.Load())
	})

	t.Run("invalid registrations", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct {
			options []httpserver.GroupOption
			prefix  string
			err     string
		}{
			"prefix without slash": {
				prefix: "api",
				err:    `invalid handler group prefix "api": it must start with /`,
			},
			"sub group prefix without slash": {
				prefix:  "/api",
				options: []httpserver.GroupOption{httpserver.WithGroup("v1")},
				err:     `invalid handler group prefix "v1"`,
			},
			"pattern with host": {
				prefix:  "/api",
				options: []httpserver.GroupOption{httpserver.WithHandler("GET example.com/users", text(""))},
				err:     `invalid pattern "GET example.com/users" in the handler group "/api"`,
			},
			"invalid handler": {
				prefix:  "/api",
				options: []httpserver.GroupOption{httpserver.WithHandler("GET /users", 42)},
				err:     `cannot register handler "GET /api/users": unsupported int`,
			},
			"invalid handler middleware": {
				prefix:  "/api",
				options: []httpserver.GroupOption{httpserver.WithHandler("GET /users", text(""), 42)},
				err:     `cannot register middleware of the handler "GET /api/users"`,
			},
			"invalid group middleware": {
				prefix:  "/api",
				options: []httpserver.GroupOption{httpserver.WithMiddlewares(42)},
				err:     `cannot register middleware of the handler group "/api": unsupported int`,
			},
			"first error wins": {
				prefix: "/api",
				options: []httpserver.GroupOption{
					httpserver.WithHandler("GET /a", 1),
					httpserver.WithHandler("GET /b", 2),
				},
				err: `cannot register handler "GET /api/a"`,
			},
		} {
			err := newServerError(t, httpserver.AsHandlerGroup(tc.prefix, tc.options...))
			require.ErrorContains(t, err, tc.err, name)
		}
	})
}
