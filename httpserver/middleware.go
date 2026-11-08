package httpserver

import (
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"

	"github.com/felixge/httpsnoop"
)

// Middleware wraps a handler, to run code before and after it.
type Middleware interface {
	Wrap(next http.Handler) http.Handler
}

// MiddlewareFunc is a func(http.Handler) http.Handler [Middleware].
type MiddlewareFunc func(next http.Handler) http.Handler

// Wrap calls f(next).
func (f MiddlewareFunc) Wrap(next http.Handler) http.Handler {
	return f(next)
}

// excluded reports whether the request path matches one of the patterns.
func excluded(patterns []*regexp.Regexp, r *http.Request) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(r.URL.Path) {
			return true
		}
	}

	return false
}

// newRecoverMiddleware turns a panic into a 500 response, and logs it with its stack.
// http.ErrAbortHandler is panicked again, to abort the response as net/http expects.
func newRecoverMiddleware(logger *slog.Logger) Middleware {
	return MiddlewareFunc(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			written := false

			w = httpsnoop.Wrap(w, httpsnoop.Hooks{
				WriteHeader: func(writeHeader httpsnoop.WriteHeaderFunc) httpsnoop.WriteHeaderFunc {
					return func(code int) {
						written = true

						writeHeader(code)
					}
				},
				Write: func(write httpsnoop.WriteFunc) httpsnoop.WriteFunc {
					return func(b []byte) (int, error) {
						written = true

						return write(b)
					}
				},
			})

			defer func() { //nolint:contextcheck // logs with the request context.
				recovered := recover()
				if recovered == nil {
					return
				}

				if recovered == http.ErrAbortHandler { //nolint:errorlint,err113 // net/http compares the sentinel the same way.
					panic(recovered)
				}

				logger.ErrorContext(r.Context(), "http handler panic", "panic", recovered, "stack", string(debug.Stack()))

				if !written {
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
			}()

			next.ServeHTTP(w, r)
		})
	})
}

// newLogMiddleware logs every request not excluded, at error level for 5xx responses and info level otherwise.
func newLogMiddleware(logger *slog.Logger, exclude []*regexp.Regexp) Middleware {
	return MiddlewareFunc(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if excluded(exclude, r) {
				next.ServeHTTP(w, r)

				return
			}

			metrics := httpsnoop.CaptureMetrics(next, w, r)

			level := slog.LevelInfo
			if metrics.Code >= http.StatusInternalServerError {
				level = slog.LevelError
			}

			// The mux sets the pattern of the request when it routes it.
			logger.Log(r.Context(), level, "http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("route", r.Pattern),
				slog.Int("status", metrics.Code),
				slog.Int64("response_size", metrics.Written),
				slog.Duration("duration", metrics.Duration),
			)
		})
	})
}
