package httpclienttest_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/core/coretest"
	"github.com/go-oryn/oryn/core/otel/oteltest/spantest"
	"github.com/go-oryn/oryn/httpclient"
	"github.com/go-oryn/oryn/httpclient/httpclienttest"
)

func TestServe(t *testing.T) {
	t.Parallel()

	var (
		client *http.Client
		spans  *tracetest.InMemoryExporter
	)

	coretest.Start(t, core.New(httpclient.Module),
		httpclienttest.Serve(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "test_body")
		})),
		fx.Populate(&client, &spans),
	)

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://api.example.com/", nil)
	require.NoError(t, err)

	response, err := client.Do(request)
	require.NoError(t, err)

	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)

	assert.Equal(t, "test_body", string(body))
	spantest.HasSpan(t, spans, spantest.WithName("GET"))
}

func TestTransport(t *testing.T) {
	t.Parallel()

	t.Run("serves the request in memory", func(t *testing.T) {
		t.Parallel()

		var server *http.Request

		transport := httpclienttest.Transport(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			server = r

			body, _ := io.ReadAll(r.Body)
			w.Header().Set("X-Test", "test_value")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write(body)
		}))

		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://api.example.com/users?q=1", strings.NewReader("test_body"))
		require.NoError(t, err)

		response, err := (&http.Client{Transport: transport}).Do(request)
		require.NoError(t, err)

		defer response.Body.Close()

		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)

		assert.Equal(t, http.StatusCreated, response.StatusCode)
		assert.Equal(t, "test_value", response.Header.Get("X-Test"))
		assert.Equal(t, "test_body", string(body))
		assert.Same(t, request, response.Request)

		assert.Equal(t, "/users?q=1", server.RequestURI)
		assert.Equal(t, "api.example.com", server.Host)
		assert.Equal(t, httpclienttest.RemoteAddr, server.RemoteAddr)
	})

	t.Run("request without body nor host", func(t *testing.T) {
		t.Parallel()

		var server *http.Request

		transport := httpclienttest.Transport(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			server = r
		}))

		request := &http.Request{Method: http.MethodGet, URL: mustParse(t, "http://localhost/"), Header: http.Header{}}
		request = request.WithContext(t.Context())

		response, err := transport.RoundTrip(request)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())

		assert.Equal(t, http.NoBody, server.Body)
		assert.Equal(t, "localhost", server.Host)
	})

	t.Run("canceled context", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost/", nil)
		require.NoError(t, err)

		_, err = httpclienttest.Transport(http.NotFoundHandler()).RoundTrip(request) //nolint:bodyclose // no response on error.
		require.ErrorIs(t, err, context.Canceled)
	})
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()

	u, err := url.Parse(raw)
	require.NoError(t, err)

	return u
}
