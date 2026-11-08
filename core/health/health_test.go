package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/core/coretest"
	"github.com/go-oryn/oryn/core/health"
)

// testProbe returns its error, or panics if panicValue is set.
type testProbe struct {
	name       string
	err        error
	panicValue any
	wait       bool
}

func (p *testProbe) Name() string {
	return p.name
}

func (p *testProbe) Check(ctx context.Context) error {
	if p.panicValue != nil {
		panic(p.panicValue)
	}

	if p.wait {
		<-ctx.Done()

		return ctx.Err()
	}

	return p.err
}

func newTestChecker(t *testing.T, probes ...health.Probe) *health.Checker {
	t.Helper()

	return health.NewChecker(slog.New(slog.DiscardHandler), 50*time.Millisecond, map[health.Kind][]health.Probe{
		health.Readiness: probes,
	})
}

func freeAddress(t *testing.T) string {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	address := listener.Addr().String()
	require.NoError(t, listener.Close())

	return address
}

func TestChecker(t *testing.T) {
	t.Parallel()

	t.Run("healthy", func(t *testing.T) {
		t.Parallel()

		result := newTestChecker(t, &testProbe{name: "first"}, &testProbe{name: "second"}).Check(t.Context(), health.Readiness)

		assert.True(t, result.Healthy())
		assert.Equal(t, map[string]health.ProbeResult{
			"first":  {Status: health.Healthy},
			"second": {Status: health.Healthy},
		}, result.Probes)
	})

	t.Run("no probes", func(t *testing.T) {
		t.Parallel()

		result := newTestChecker(t).Check(t.Context(), health.Liveness)

		assert.True(t, result.Healthy())
		assert.Empty(t, result.Probes)
	})

	t.Run("failing, panicking and slow probes", func(t *testing.T) {
		t.Parallel()

		result := newTestChecker(t,
			&testProbe{name: "ok"},
			&testProbe{name: "failing", err: errors.New("test_error")},
			&testProbe{name: "panicking", panicValue: "test_panic"},
			&testProbe{name: "slow", wait: true},
		).Check(t.Context(), health.Readiness)

		assert.False(t, result.Healthy())
		assert.Equal(t, map[string]health.ProbeResult{
			"ok":        {Status: health.Healthy},
			"failing":   {Status: health.Unhealthy, Error: "test_error"},
			"panicking": {Status: health.Unhealthy, Error: "probe panicked: test_panic"},
			"slow":      {Status: health.Unhealthy, Error: "context deadline exceeded"},
		}, result.Probes)
	})

	t.Run("readiness fails when shutting down", func(t *testing.T) {
		t.Parallel()

		checker := newTestChecker(t, &testProbe{name: "ok"})
		checker.SetShuttingDown()

		assert.False(t, checker.Check(t.Context(), health.Readiness).Healthy())
		assert.True(t, checker.Check(t.Context(), health.Liveness).Healthy())
	})
}

func TestHandler(t *testing.T) {
	t.Parallel()

	checker := health.NewChecker(slog.New(slog.DiscardHandler), 0, map[health.Kind][]health.Probe{
		health.Liveness:  {&testProbe{name: "ok"}},
		health.Readiness: {&testProbe{name: "failing", err: errors.New("test_error")}},
	})
	handler := health.NewHandler(checker)

	for path, expected := range map[string]int{
		"/livez":    http.StatusOK,
		"/readyz":   http.StatusServiceUnavailable,
		"/startupz": http.StatusOK,
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))

		assert.Equal(t, expected, rec.Code, path)
		assert.Empty(t, rec.Body.String(), path)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz?verbose", nil))

	var result health.Result
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))

	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Equal(t, health.Unhealthy, result.Status)
	assert.Equal(t, health.ProbeResult{Status: health.Unhealthy, Error: "test_error"}, result.Probes["failing"])
}

func TestModule(t *testing.T) {
	t.Parallel()

	var checker *health.Checker

	newOK := func() *testProbe { return &testProbe{name: "ok"} }
	newFailing := func() *testProbe { return &testProbe{name: "failing", err: errors.New("test_error")} }

	coretest.Start(t, core.New(
		health.AsProbe(newOK),
		health.AsProbe(newFailing, health.Readiness, health.Startup),
	), fx.Populate(&checker))

	assert.Equal(t, []string{"ok"}, probeNames(checker.Check(t.Context(), health.Liveness)))
	assert.ElementsMatch(t, []string{"ok", "failing"}, probeNames(checker.Check(t.Context(), health.Readiness)))
	assert.ElementsMatch(t, []string{"ok", "failing"}, probeNames(checker.Check(t.Context(), health.Startup)))
}

func TestServe(t *testing.T) {
	t.Parallel()

	address := freeAddress(t)

	coretest.Start(t,
		core.New(health.Serve()),
		config.AsOptions(config.WithValues(map[string]any{"health.server.address": address})),
	)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, fmt.Sprintf("http://%s/livez", address), nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestServeAddressInUse(t *testing.T) {
	t.Parallel()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	app := coretest.New(t,
		core.New(health.Serve()),
		config.AsOptions(config.WithValues(map[string]any{"health.server.address": listener.Addr().String()})),
	)

	require.ErrorContains(t, app.Start(t.Context()), "cannot start health server")
}

func probeNames(result health.Result) []string {
	names := make([]string, 0, len(result.Probes))
	for name := range result.Probes {
		names = append(names, name)
	}

	return names
}
