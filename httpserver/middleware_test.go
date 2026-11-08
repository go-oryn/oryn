package httpserver_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/codes"

	"github.com/go-oryn/oryn/core/otel/oteltest/logtest"
	"github.com/go-oryn/oryn/core/otel/oteltest/spantest"
	"github.com/go-oryn/oryn/httpserver"
)

func TestRecover(t *testing.T) {
	t.Parallel()

	t.Run("panic before writing", func(t *testing.T) {
		t.Parallel()

		server, tel := startServer(t, httpserver.AsHandler("GET /", func(http.ResponseWriter, *http.Request) {
			panic("test_panic")
		}))

		status, body := do(t, server, http.MethodGet, "/")
		assert.Equal(t, http.StatusInternalServerError, status)
		assert.Equal(t, "Internal Server Error\n", body)

		logtest.HasRecord(t, tel.logs, logtest.WithBody("http handler panic"), logtest.WithLevel(slog.LevelError),
			logtest.WithAttributeValue("panic", "test_panic"), logtest.WithAttribute("stack"))
		logtest.HasRecord(t, tel.logs, logtest.WithBody("http request"), logtest.WithLevel(slog.LevelError),
			logtest.WithAttributeValue("status", "500"))
		spantest.HasSpan(t, tel.spans, spantest.WithName("GET /"), spantest.WithStatus(codes.Error))
	})

	t.Run("panic after writing", func(t *testing.T) {
		t.Parallel()

		server, _ := startServer(t,
			httpserver.AsHandler("GET /header", func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusAccepted)
				panic("test_panic")
			}),
			httpserver.AsHandler("GET /body", func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, "partial")
				panic("test_panic")
			}),
		)

		status, body := do(t, server, http.MethodGet, "/header")
		assert.Equal(t, http.StatusAccepted, status)
		assert.Empty(t, body)

		status, body = do(t, server, http.MethodGet, "/body")
		assert.Equal(t, http.StatusOK, status)
		assert.Equal(t, "partial", body)
	})

	t.Run("abort handler", func(t *testing.T) {
		t.Parallel()

		server, _ := startServer(t, httpserver.AsHandler("GET /", func(http.ResponseWriter, *http.Request) {
			panic(http.ErrAbortHandler)
		}))

		assert.PanicsWithValue(t, http.ErrAbortHandler, func() {
			server.Handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
		})
	})
}

func TestRequestLogs(t *testing.T) {
	t.Parallel()

	server, tel := startServer(t,
		httpserver.AsHandler("GET /ok", text("ok")),
		httpserver.AsHandler("GET /error", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "test_error", http.StatusServiceUnavailable)
		}),
	)

	do(t, server, http.MethodGet, "/ok")
	do(t, server, http.MethodGet, "/error")
	do(t, server, http.MethodGet, "/missing")

	logtest.HasRecord(t, tel.logs, logtest.WithBody("http request"), logtest.WithLevel(slog.LevelInfo),
		logtest.WithAttributeValue("method", "GET"),
		logtest.WithAttributeValue("route", "GET /ok"),
		logtest.WithAttributeValue("status", "200"),
		logtest.WithAttributeValue("response_size", "2"),
		logtest.WithAttribute("duration"),
	)
	logtest.HasRecord(t, tel.logs, logtest.WithBody("http request"), logtest.WithLevel(slog.LevelError),
		logtest.WithAttributeValue("route", "GET /error"), logtest.WithAttributeValue("status", "503"))
	logtest.HasRecord(t, tel.logs, logtest.WithBody("http request"), logtest.WithLevel(slog.LevelInfo),
		logtest.WithAttributeValue("path", "/missing"), logtest.WithAttributeValue("route", ""), logtest.WithAttributeValue("status", "404"))
}
