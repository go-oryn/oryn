package httpclient_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/core/coretest"
	"github.com/go-oryn/oryn/core/otel/oteltest/logtest"
	"github.com/go-oryn/oryn/core/otel/oteltest/metrictest"
	"github.com/go-oryn/oryn/core/otel/oteltest/spantest"
	"github.com/go-oryn/oryn/httpclient"
	"github.com/go-oryn/oryn/httpclient/httpclienttest"
	"github.com/go-oryn/oryn/httpserver"
)

// newApp returns an application with the httpclient module, the config values and the options.
func newApp(values map[string]any, options ...fx.Option) core.App {
	return core.New(append(options, httpclient.Module, config.AsOptions(config.WithValues(values)))...)
}

// startError returns the error of an application failing to build.
func startError(t *testing.T, app core.App) error {
	t.Helper()

	return fx.New(app.Options(coretest.TestOptions(), fx.NopLogger, fx.Invoke(func(*http.Client) {}))...).Err()
}

// get sends a GET request with the client, and returns the response status and body.
func get(t *testing.T, client *http.Client, url string) (int, string) {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	require.NoError(t, err)

	response, err := client.Do(request)
	require.NoError(t, err)

	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	return response.StatusCode, string(body)
}

// statusHandler answers with the status, and records the traceparent header it received.
func statusHandler(status int, traceparent *string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if traceparent != nil {
			*traceparent = r.Header.Get("Traceparent")
		}

		w.WriteHeader(status)
		_, _ = io.WriteString(w, "test_body")
	})
}

// errorTransport fails every request.
var errorTransport = httpclient.RoundTripperFunc(func(*http.Request) (*http.Response, error) {
	return nil, errors.New("test_error")
})

func TestClient(t *testing.T) {
	t.Parallel()

	t.Run("requests are instrumented", func(t *testing.T) {
		t.Parallel()

		var (
			client      *http.Client
			spans       *tracetest.InMemoryExporter
			logs        *logtest.InMemoryExporter
			metrics     *sdkmetric.ManualReader
			traceparent string
		)

		coretest.Start(t, newApp(nil),
			httpclienttest.Serve(statusHandler(http.StatusOK, &traceparent)),
			fx.Populate(&client, &spans, &logs, &metrics),
		)

		status, body := get(t, client, "https://api.example.com/users?token=test_secret")
		assert.Equal(t, http.StatusOK, status)
		assert.Equal(t, "test_body", body)

		spantest.HasSpan(t, spans,
			spantest.WithName("GET"),
			spantest.WithKind(trace.SpanKindClient),
			spantest.WithAttributeValue(semconv.HTTPRequestMethodGet),
			spantest.WithAttributeValue(semconv.ServerAddress("api.example.com")),
			spantest.WithAttributeValue(semconv.HTTPResponseStatusCode(http.StatusOK)),
		)
		metrictest.HasMetric(t, metrics, metrictest.WithName("http.client.request.duration"))

		// The trace context is sent to the server.
		client0 := spantest.Find(spans, spantest.WithName("GET"))
		require.Len(t, client0, 1)
		assert.Contains(t, traceparent, client0[0].SpanContext.TraceID().String())

		// The log is correlated with the client span, without the query.
		logtest.HasRecord(t, logs,
			logtest.WithBody("http client request"),
			logtest.WithLevel(slog.LevelInfo),
			logtest.WithAttributeValue("method", "GET"),
			logtest.WithAttributeValue("url", "https://api.example.com/users"),
			logtest.WithAttributeValue("status", "200"),
		)
		logtest.HasNoRecord(t, logs, logtest.WithBodyContains("test_secret"))
	})

	t.Run("server errors", func(t *testing.T) {
		t.Parallel()

		var (
			client *http.Client
			spans  *tracetest.InMemoryExporter
			logs   *logtest.InMemoryExporter
		)

		coretest.Start(t, newApp(nil), httpclienttest.Serve(statusHandler(http.StatusBadGateway, nil)), fx.Populate(&client, &spans, &logs))

		status, _ := get(t, client, "https://api.example.com/")
		assert.Equal(t, http.StatusBadGateway, status)

		spantest.HasSpan(t, spans, spantest.WithName("GET"), spantest.WithStatus(codes.Error))
		logtest.HasRecord(t, logs, logtest.WithBody("http client request"), logtest.WithLevel(slog.LevelError),
			logtest.WithAttributeValue("status", "502"))
	})

	t.Run("transport errors", func(t *testing.T) {
		t.Parallel()

		var (
			factory *httpclient.Factory
			spans   *tracetest.InMemoryExporter
			logs    *logtest.InMemoryExporter
		)

		coretest.Start(t, newApp(nil), fx.Populate(&factory, &spans, &logs))

		client := factory.NewClient(httpclient.WithTransport(errorTransport))

		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://api.example.com/", strings.NewReader("test_body"))
		require.NoError(t, err)

		_, err = client.Do(request) //nolint:bodyclose // no response on error.
		require.ErrorContains(t, err, "test_error")

		spantest.HasSpan(t, spans, spantest.WithName("POST"), spantest.WithStatus(codes.Error))
		logtest.HasRecord(t, logs, logtest.WithBody("http client request"), logtest.WithLevel(slog.LevelError),
			logtest.WithAttributeValue("error", "test_error"))
	})

	t.Run("trace propagation to a server", func(t *testing.T) {
		t.Parallel()

		var (
			factory *httpclient.Factory
			server  *http.Server
			spans   *tracetest.InMemoryExporter
		)

		coretest.Start(t,
			newApp(nil, httpserver.Module, httpserver.AsHandler("GET /hello", statusHandler(http.StatusOK, nil))),
			fx.Populate(&factory, &server, &spans),
		)

		// The client calls the application server, in memory.
		client := factory.NewClient(httpclient.WithTransport(httpclienttest.Transport(server.Handler)))
		get(t, client, "http://localhost/hello")

		clientSpans := spantest.Find(spans, spantest.WithName("GET"), spantest.WithKind(trace.SpanKindClient))
		require.Len(t, clientSpans, 1)
		spantest.HasSpan(t, spans,
			spantest.WithName("GET /hello"),
			spantest.WithKind(trace.SpanKindServer),
			spantest.WithParent(clientSpans[0].SpanContext.SpanID()),
		)
	})

	t.Run("excluded urls", func(t *testing.T) {
		t.Parallel()

		var (
			client  *http.Client
			spans   *tracetest.InMemoryExporter
			logs    *logtest.InMemoryExporter
			metrics *sdkmetric.ManualReader
		)

		values := map[string]any{"httpclient.observability.exclude": []string{`^https://health\.example\.com/`}}

		coretest.Start(t, newApp(values), httpclienttest.Serve(statusHandler(http.StatusOK, nil)), fx.Populate(&client, &spans, &logs, &metrics))

		status, _ := get(t, client, "https://health.example.com/livez")
		assert.Equal(t, http.StatusOK, status)

		spantest.HasNoSpan(t, spans, spantest.WithName("GET"))
		logtest.HasNoRecord(t, logs, logtest.WithBody("http client request"))
		metrictest.HasNoMetric(t, metrics, metrictest.WithName("http.client.request.duration"))

		get(t, client, "https://api.example.com/")
		spantest.HasSpan(t, spans, spantest.WithName("GET"))
	})

	t.Run("log disabled", func(t *testing.T) {
		t.Parallel()

		var (
			client *http.Client
			spans  *tracetest.InMemoryExporter
			logs   *logtest.InMemoryExporter
		)

		values := map[string]any{"httpclient.log.enabled": false}

		coretest.Start(t, newApp(values), httpclienttest.Serve(statusHandler(http.StatusOK, nil)), fx.Populate(&client, &spans, &logs))

		get(t, client, "https://api.example.com/")

		spantest.HasSpan(t, spans, spantest.WithName("GET"))
		logtest.HasNoRecord(t, logs, logtest.WithBody("http client request"))
	})

	t.Run("span names", func(t *testing.T) {
		t.Parallel()

		var (
			client *http.Client
			spans  *tracetest.InMemoryExporter
		)

		coretest.Start(t, newApp(nil), httpclienttest.Serve(statusHandler(http.StatusOK, nil)), fx.Populate(&client, &spans))

		for _, method := range []string{http.MethodPut, "PURGE"} {
			request, err := http.NewRequestWithContext(t.Context(), method, "https://api.example.com/", nil)
			require.NoError(t, err)

			response, err := client.Do(request)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
		}

		spantest.HasSpan(t, spans, spantest.WithName("PUT"))
		spantest.HasSpan(t, spans, spantest.WithName("HTTP"))
	})

	t.Run("invalid exclude pattern", func(t *testing.T) {
		t.Parallel()

		err := startError(t, newApp(map[string]any{"httpclient.observability.exclude": []string{"("}}))
		require.ErrorContains(t, err, `invalid httpclient.observability.exclude pattern "("`)
	})
}

func TestTimeout(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		values  map[string]any
		options []httpclient.Option
		want    time.Duration
	}{
		"default":       {want: httpclient.DefaultTimeout},
		"configured":    {values: map[string]any{"httpclient.timeout": "5s"}, want: 5 * time.Second},
		"explicit zero": {values: map[string]any{"httpclient.timeout": "0s"}, want: 0},
		"option":        {options: []httpclient.Option{httpclient.WithTimeout(time.Second)}, want: time.Second},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var factory *httpclient.Factory

			coretest.Start(t, newApp(tc.values), fx.Populate(&factory))

			assert.Equal(t, tc.want, factory.NewClient(tc.options...).Timeout)
		})
	}
}

func TestBaseTransport(t *testing.T) {
	t.Parallel()

	t.Run("defaults", func(t *testing.T) {
		t.Parallel()

		var base httpclient.BaseTransport

		coretest.Start(t, newApp(nil), fx.Populate(&base))

		transport, ok := base.RoundTripper.(*http.Transport)
		require.True(t, ok)
		assert.Equal(t, httpclient.DefaultMaxIdleConns, transport.MaxIdleConns)
		assert.Equal(t, httpclient.DefaultMaxIdleConnsPerHost, transport.MaxIdleConnsPerHost)
		assert.Equal(t, 0, transport.MaxConnsPerHost)
		assert.Equal(t, httpclient.DefaultIdleConnTimeout, transport.IdleConnTimeout)
		assert.Equal(t, httpclient.DefaultTLSHandshakeTimeout, transport.TLSHandshakeTimeout)
		assert.Equal(t, time.Duration(0), transport.ResponseHeaderTimeout)
		assert.Equal(t, httpclient.DefaultExpectContinueTimeout, transport.ExpectContinueTimeout)
		assert.NotNil(t, transport.Proxy, "the default transport proxy settings are kept")
	})

	t.Run("configured", func(t *testing.T) {
		t.Parallel()

		var base httpclient.BaseTransport

		coretest.Start(t, newApp(map[string]any{
			"httpclient.transport.max_idle_conns":          10,
			"httpclient.transport.max_idle_conns_per_host": 5,
			"httpclient.transport.max_conns_per_host":      20,
			"httpclient.transport.idle_conn_timeout":       "1m",
			"httpclient.transport.tls_handshake_timeout":   "2s",
			"httpclient.transport.response_header_timeout": "3s",
			"httpclient.transport.expect_continue_timeout": "0s",
		}), fx.Populate(&base))

		transport, ok := base.RoundTripper.(*http.Transport)
		require.True(t, ok)
		assert.Equal(t, 10, transport.MaxIdleConns)
		assert.Equal(t, 5, transport.MaxIdleConnsPerHost)
		assert.Equal(t, 20, transport.MaxConnsPerHost)
		assert.Equal(t, time.Minute, transport.IdleConnTimeout)
		assert.Equal(t, 2*time.Second, transport.TLSHandshakeTimeout)
		assert.Equal(t, 3*time.Second, transport.ResponseHeaderTimeout)
		assert.Equal(t, time.Duration(0), transport.ExpectContinueTimeout)
	})
}

func TestFactory(t *testing.T) {
	t.Parallel()

	var (
		factory *httpclient.Factory
		spans   *tracetest.InMemoryExporter
	)

	coretest.Start(t, newApp(nil), fx.Populate(&factory, &spans))

	// A client with its own base transport is instrumented too.
	client := factory.NewClient(httpclient.WithTransport(httpclienttest.Transport(statusHandler(http.StatusAccepted, nil))))

	status, _ := get(t, client, "https://api.example.com/")
	assert.Equal(t, http.StatusAccepted, status)
	spantest.HasSpan(t, spans, spantest.WithName("GET"), spantest.WithAttributeValue(semconv.HTTPResponseStatusCode(http.StatusAccepted)))

	// The context cancellation is honored.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.example.com/", nil)
	require.NoError(t, err)

	_, err = client.Do(request) //nolint:bodyclose // no response on error.
	require.ErrorIs(t, err, context.Canceled)
}
