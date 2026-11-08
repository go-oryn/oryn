package httpserver_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/core/coretest"
	"github.com/go-oryn/oryn/core/otel/oteltest/logtest"
	"github.com/go-oryn/oryn/core/otel/oteltest/metrictest"
	"github.com/go-oryn/oryn/core/otel/oteltest/spantest"
	"github.com/go-oryn/oryn/httpserver"
)

// telemetry holds the in memory telemetry of a test application.
type telemetry struct {
	spans   *tracetest.InMemoryExporter
	logs    *logtest.InMemoryExporter
	metrics *sdkmetric.ManualReader
}

// startServer starts a test application with the httpserver module and the options,
// and returns its server and telemetry.
func startServer(t *testing.T, options ...fx.Option) (*http.Server, telemetry) {
	t.Helper()

	var (
		server *http.Server
		tel    telemetry
	)

	coretest.Start(t, core.New(httpserver.Module), append(options, fx.Populate(&server, &tel.spans, &tel.logs, &tel.metrics))...)

	return server, tel
}

// newServerError returns the error of an application failing to build the server.
func newServerError(t *testing.T, options ...fx.Option) error {
	t.Helper()

	app := fx.New(core.New(httpserver.Module).Options(append(options,
		coretest.TestOptions(),
		fx.NopLogger,
		fx.Invoke(func(*http.Server) {}),
	)...)...)

	return app.Err()
}

// do serves the request, and returns the response status and body.
func do(t *testing.T, server *http.Server, method string, target string) (int, string) {
	t.Helper()

	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), method, target, nil))

	return recorder.Code, recorder.Body.String()
}

// withValues sets config values.
func withValues(values map[string]any) fx.Option {
	return config.AsOptions(config.WithValues(values))
}

// text returns a handler writing body.
func text(body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, body)
	}
}

func TestNewServer(t *testing.T) {
	t.Parallel()

	t.Run("defaults", func(t *testing.T) {
		t.Parallel()

		server, _ := startServer(t)

		assert.Equal(t, httpserver.DefaultAddress, server.Addr)
		assert.Equal(t, httpserver.DefaultReadHeaderTimeout, server.ReadHeaderTimeout)
		assert.Equal(t, httpserver.DefaultIdleTimeout, server.IdleTimeout)
		assert.Zero(t, server.ReadTimeout)
		assert.Zero(t, server.WriteTimeout)

		status, _ := do(t, server, http.MethodGet, "/missing")
		assert.Equal(t, http.StatusNotFound, status)
	})

	t.Run("from config", func(t *testing.T) {
		t.Parallel()

		server, _ := startServer(t, withValues(map[string]any{
			"httpserver.address":             ":9090",
			"httpserver.read_header_timeout": "1s",
			"httpserver.read_timeout":        "2s",
			"httpserver.write_timeout":       "3s",
			"httpserver.idle_timeout":        "4s",
		}))

		assert.Equal(t, ":9090", server.Addr)
		assert.Equal(t, time.Second, server.ReadHeaderTimeout)
		assert.Equal(t, 2*time.Second, server.ReadTimeout)
		assert.Equal(t, 3*time.Second, server.WriteTimeout)
		assert.Equal(t, 4*time.Second, server.IdleTimeout)
	})

	t.Run("invalid exclude pattern", func(t *testing.T) {
		t.Parallel()

		err := newServerError(t, withValues(map[string]any{"httpserver.observability.exclude": []string{"("}}))
		require.ErrorContains(t, err, `invalid httpserver.observability.exclude pattern "("`)
	})
}

func TestTelemetry(t *testing.T) {
	t.Parallel()

	t.Run("spans, metrics and logs", func(t *testing.T) {
		t.Parallel()

		server, tel := startServer(t, httpserver.AsHandler("GET /users/{id}", text("test_user")))

		status, body := do(t, server, http.MethodGet, "/users/42")
		assert.Equal(t, http.StatusOK, status)
		assert.Equal(t, "test_user", body)

		spantest.HasSpan(t, tel.spans,
			spantest.WithName("GET /users/{id}"),
			spantest.WithAttributeValue(semconv.HTTPRoute("/users/{id}")),
			spantest.WithAttributeValue(semconv.HTTPResponseStatusCode(http.StatusOK)),
		)
		metrictest.HasMetric(t, tel.metrics,
			metrictest.WithName("http.server.request.duration"),
			metrictest.WithAttributeValue(attribute.String("http.route", "/users/{id}")),
		)
		logtest.HasRecord(t, tel.logs,
			logtest.WithBody("http request"),
			logtest.WithAttributeValue("route", "GET /users/{id}"),
			logtest.WithAttributeValue("path", "/users/42"),
			logtest.WithAttributeValue("status", "200"),
		)
	})

	t.Run("route of patterns with a host", func(t *testing.T) {
		t.Parallel()

		// httptest requests target example.com.
		server, tel := startServer(t, httpserver.AsHandler("example.com/hosted", text("hosted")))

		_, body := do(t, server, http.MethodGet, "/hosted")
		assert.Equal(t, "hosted", body)

		spantest.HasSpan(t, tel.spans, spantest.WithAttributeValue(semconv.HTTPRoute("/hosted")))
	})

	t.Run("excluded paths", func(t *testing.T) {
		t.Parallel()

		server, tel := startServer(t,
			withValues(map[string]any{"httpserver.observability.exclude": []string{"^/livez$"}}),
			httpserver.AsHandler("GET /livez", text("ok")),
		)

		status, _ := do(t, server, http.MethodGet, "/livez")
		assert.Equal(t, http.StatusOK, status)

		spantest.HasNoSpan(t, tel.spans)
		logtest.HasNoRecord(t, tel.logs, logtest.WithBody("http request"))
	})

	t.Run("request logs disabled", func(t *testing.T) {
		t.Parallel()

		server, tel := startServer(t,
			withValues(map[string]any{"httpserver.log.enabled": false}),
			httpserver.AsHandler("GET /", text("ok")),
		)

		do(t, server, http.MethodGet, "/")

		spantest.HasSpan(t, tel.spans, spantest.WithName("GET /"))
		logtest.HasNoRecord(t, tel.logs, logtest.WithBody("http request"))
	})
}

func TestServe(t *testing.T) {
	t.Parallel()

	t.Run("serves until the application stops", func(t *testing.T) {
		t.Parallel()

		var server *http.Server

		app := coretest.New(t,
			core.New(httpserver.Module, httpserver.AsHandler("GET /", text("test_body"))),
			withValues(map[string]any{"httpserver.address": "127.0.0.1:0"}),
			httpserver.Serve(),
			fx.Populate(&server),
		)
		app.RequireStart()

		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+server.Addr+"/", nil)
		require.NoError(t, err)

		response, err := http.DefaultClient.Do(request)
		require.NoError(t, err)

		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		assert.Equal(t, "test_body", string(body))

		app.RequireStop()

		response, err = http.DefaultClient.Do(request)
		if response != nil {
			_ = response.Body.Close()
		}

		require.Error(t, err, "the server is stopped")
	})

	t.Run("address in use", func(t *testing.T) {
		t.Parallel()

		listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
		require.NoError(t, err)
		t.Cleanup(func() { _ = listener.Close() })

		app := fx.New(core.New(httpserver.Module).Options(
			coretest.TestOptions(),
			fx.NopLogger,
			withValues(map[string]any{"httpserver.address": listener.Addr().String()}),
			httpserver.Serve(),
		)...)
		require.NoError(t, app.Err())

		ctx, cancel := context.WithTimeout(t.Context(), fx.DefaultTimeout)
		defer cancel()

		require.ErrorContains(t, app.Start(ctx), "cannot start http server on "+listener.Addr().String())
	})
}
