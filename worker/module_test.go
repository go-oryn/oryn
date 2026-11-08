package worker_test

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/core/coretest"
	"github.com/go-oryn/oryn/core/health"
	"github.com/go-oryn/oryn/core/otel/oteltest/logtest"
	"github.com/go-oryn/oryn/core/otel/oteltest/metrictest"
	"github.com/go-oryn/oryn/core/otel/oteltest/spantest"
	"github.com/go-oryn/oryn/worker"
	"github.com/go-oryn/oryn/worker/workertest"
)

// fastRestart restarts the failed runs right away.
var fastRestart = map[string]any{
	"worker.restart.backoff.initial": "1ms",
	"worker.restart.backoff.max":     "2ms",
}

// newApp returns an application with the worker module, the config values and the options.
func newApp(values map[string]any, options ...fx.Option) core.App {
	return core.New(append(options, worker.Module, config.AsOptions(config.WithValues(values)))...)
}

// blocking returns a worker blocking until its context is canceled, signaling each run on started.
func blocking(started chan<- struct{}) worker.WorkerFunc {
	return func(ctx context.Context) error {
		started <- struct{}{}
		<-ctx.Done()

		return ctx.Err()
	}
}

// failing returns a worker failing its first n runs, then blocking.
func failing(n int32, runs *atomic.Int32) worker.WorkerFunc {
	return func(ctx context.Context) error {
		if runs.Add(1) <= n {
			return errors.New("test_error")
		}

		<-ctx.Done()

		return nil
	}
}

// telemetry holds the in-memory telemetry of a test application.
type telemetry struct {
	pool    *worker.Pool
	checker *health.Checker
	spans   *tracetest.InMemoryExporter
	logs    *logtest.InMemoryExporter
	metrics *sdkmetric.ManualReader
}

func (tel *telemetry) populate() fx.Option {
	return fx.Populate(&tel.pool, &tel.checker, &tel.spans, &tel.logs, &tel.metrics)
}

func receive(t *testing.T, ch <-chan struct{}, n int) {
	t.Helper()

	for range n {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			require.FailNow(t, "worker not started")
		}
	}
}

func TestRun(t *testing.T) {
	t.Parallel()

	t.Run("runs the workers until the application stops", func(t *testing.T) {
		t.Parallel()

		var tel telemetry

		started := make(chan struct{}, 1)
		app := coretest.New(t, newApp(nil, worker.AsWorker("test_worker", blocking(started))), worker.Run(), tel.populate())
		app.RequireStart()
		receive(t, started, 1)

		infos := tel.pool.Worker("test_worker")
		require.Len(t, infos, 1)
		assert.Equal(t, worker.StateRunning, infos[0].State)
		assert.Equal(t, int64(1), metrictest.Int64Sum(t, tel.metrics, worker.MetricActive))

		logtest.HasRecord(t, tel.logs, logtest.WithBody("worker started"), logtest.WithAttributeValue("worker", "test_worker"))

		app.RequireStop()

		assert.Equal(t, worker.StateStopped, tel.pool.Worker("test_worker")[0].State)
	})

	t.Run("replicas", func(t *testing.T) {
		t.Parallel()

		var tel telemetry

		started := make(chan struct{}, 3)
		coretest.Start(t, newApp(nil, worker.AsWorker("test_worker", blocking(started), worker.WithReplicas(3))), worker.Run(), tel.populate())
		receive(t, started, 3)

		infos := workertest.WaitForState(t, tel.pool, "test_worker", worker.StateRunning)
		require.Len(t, infos, 3)

		for i, info := range infos {
			assert.Equal(t, i, info.Replica)
		}
	})

	t.Run("workers and constructors", func(t *testing.T) {
		t.Parallel()

		var tel telemetry

		started := make(chan struct{}, 4)
		run := blocking(started)

		coretest.Start(t, newApp(map[string]any{"test.value": "test_value"},
			worker.AsWorker("test_func", (func(context.Context) error)(run)),
			worker.AsWorker("test_worker_func", run),
			worker.AsWorker("test_struct", &testWorker{run: run}),
			worker.AsWorker("test_constructor", func(cfg *config.Config) worker.Worker {
				assert.Equal(t, "test_value", cfg.GetString("test.value"))

				return run
			}),
		), worker.Run(), tel.populate())
		receive(t, started, 4)

		names := make([]string, 0, 4)
		for _, info := range tel.pool.Workers() {
			names = append(names, info.Name)
		}

		assert.Equal(t, []string{"test_func", "test_worker_func", "test_struct", "test_constructor"}, names)
	})

	t.Run("a completed worker is not restarted", func(t *testing.T) {
		t.Parallel()

		var (
			tel  telemetry
			runs atomic.Int32
		)

		coretest.Start(t, newApp(fastRestart, worker.AsWorker("test_worker", func(context.Context) error {
			runs.Add(1)

			return nil
		})), worker.Run(), tel.populate())

		workertest.WaitForState(t, tel.pool, "test_worker", worker.StateStopped)
		time.Sleep(20 * time.Millisecond)
		assert.Equal(t, int32(1), runs.Load())
		assert.True(t, tel.checker.Check(t.Context(), health.Liveness).Healthy())
	})
}

func TestRestart(t *testing.T) {
	t.Parallel()

	t.Run("restarts the failed runs", func(t *testing.T) {
		t.Parallel()

		var (
			tel  telemetry
			runs atomic.Int32
		)

		coretest.Start(t, newApp(fastRestart, worker.AsWorker("test_worker", failing(2, &runs))), worker.Run(), tel.populate())

		require.Eventually(t, func() bool { return runs.Load() == 3 }, 5*time.Second, time.Millisecond)
		infos := workertest.WaitForState(t, tel.pool, "test_worker", worker.StateRunning)
		assert.Equal(t, 2, infos[0].Restarts)
		require.EqualError(t, infos[0].Err, "test_error")

		spantest.SpanCount(t, tel.spans, 2,
			spantest.WithName("worker test_worker"),
			spantest.WithStatus(codes.Error),
			spantest.WithAttributeValue(worker.ErrorTypeKey.String("*errors.errorString")),
		)
		logtest.RecordCount(t, tel.logs, 2, logtest.WithBody("worker restarting"), logtest.WithLevel(slog.LevelWarn),
			logtest.WithAttributeValue("exception.message", "test_error"))
		assert.Equal(t, int64(2), metrictest.Int64Sum(t, tel.metrics, worker.MetricRestarts, worker.NameKey.String("test_worker")))
		assert.Equal(t, int64(2), metrictest.Int64Sum(t, tel.metrics, worker.MetricRuns, worker.ErrorTypeKey.String("*errors.errorString")))
		assert.True(t, tel.checker.Check(t.Context(), health.Liveness).Healthy())
	})

	t.Run("recovers the panics", func(t *testing.T) {
		t.Parallel()

		var (
			tel  telemetry
			runs atomic.Int32
		)

		coretest.Start(t, newApp(fastRestart, worker.AsWorker("test_worker", func(ctx context.Context) error {
			if runs.Add(1) == 1 {
				panic("test_panic")
			}

			<-ctx.Done()

			return nil
		})), worker.Run(), tel.populate())

		require.Eventually(t, func() bool { return runs.Load() == 2 }, 5*time.Second, time.Millisecond)

		infos := workertest.WaitForState(t, tel.pool, "test_worker", worker.StateRunning)
		require.ErrorIs(t, infos[0].Err, worker.ErrPanic)
		spantest.HasSpan(t, tel.spans, spantest.WithName("worker test_worker"), spantest.WithAttributeValue(worker.ErrorTypeKey.String("panic")))
		logtest.HasRecord(t, tel.logs, logtest.WithBody("worker panic"), logtest.WithLevel(slog.LevelError),
			logtest.WithAttributeValue("panic", "test_panic"), logtest.WithAttribute("stack"))
	})

	t.Run("fails after the max restarts, and shuts the application down", func(t *testing.T) {
		t.Parallel()

		var (
			tel  telemetry
			runs atomic.Int32
		)

		values := map[string]any{"worker.restart.max_restarts": 2}
		for k, v := range fastRestart {
			values[k] = v
		}

		app := coretest.Start(t, newApp(values, worker.AsWorker("test_worker", failing(100, &runs))), worker.Run(), tel.populate())

		select {
		case signal := <-app.Wait():
			assert.Equal(t, 1, signal.ExitCode)
		case <-time.After(5 * time.Second):
			require.FailNow(t, "the application was not shut down")
		}

		infos := workertest.WaitForState(t, tel.pool, "test_worker", worker.StateFailed)
		assert.Equal(t, int32(3), runs.Load(), "1 run and 2 restarts")
		assert.Equal(t, 2, infos[0].Restarts)
		logtest.HasRecord(t, tel.logs, logtest.WithBody("worker failed"), logtest.WithLevel(slog.LevelError))

		result := tel.checker.Check(t.Context(), health.Liveness)
		assert.False(t, result.Healthy())
		assert.Contains(t, result.Probes["worker"].Error, "worker test_worker (replica 0) failed: test_error")
	})

	t.Run("fails without restart, and keeps the application running", func(t *testing.T) {
		t.Parallel()

		var (
			tel  telemetry
			runs atomic.Int32
		)

		app := coretest.Start(t, newApp(map[string]any{
			"worker.restart.enabled":     false,
			"worker.shutdown_on_failure": false,
		}, worker.AsWorker("test_worker", failing(100, &runs))), worker.Run(), tel.populate())

		infos := workertest.WaitForState(t, tel.pool, "test_worker", worker.StateFailed)
		assert.Equal(t, 0, infos[0].Restarts)

		var failed *worker.FailedError

		err := tel.checker.Check(t.Context(), health.Readiness)
		assert.False(t, err.Healthy())

		probeErr := (&worker.Probe{}).Name()
		assert.Equal(t, "worker", probeErr)
		require.ErrorAs(t, worker.NewProbe(tel.pool).Check(t.Context()), &failed)
		assert.Equal(t, "test_worker", failed.Name)
		require.EqualError(t, errors.Unwrap(failed), "test_error")

		select {
		case <-app.Wait():
			assert.Fail(t, "the application was shut down")
		case <-time.After(20 * time.Millisecond):
		}
	})

	t.Run("resets the consecutive failures after a long run", func(t *testing.T) {
		t.Parallel()

		var (
			tel  telemetry
			runs atomic.Int32
		)

		values := map[string]any{"worker.restart.max_restarts": 1, "worker.restart.reset_after": "1ms"}
		for k, v := range fastRestart {
			values[k] = v
		}

		coretest.Start(t, newApp(values, worker.AsWorker("test_worker", func(ctx context.Context) error {
			if runs.Add(1) <= 4 {
				time.Sleep(2 * time.Millisecond)

				return errors.New("test_error")
			}

			<-ctx.Done()

			return nil
		})), worker.Run(), tel.populate())

		require.Eventually(t, func() bool { return runs.Load() == 5 }, 5*time.Second, time.Millisecond)
		infos := workertest.WaitForState(t, tel.pool, "test_worker", worker.StateRunning)
		assert.Equal(t, 4, infos[0].Restarts)
	})

	t.Run("stops during the backoff", func(t *testing.T) {
		t.Parallel()

		var (
			tel  telemetry
			runs atomic.Int32
		)

		app := coretest.New(t, newApp(map[string]any{"worker.restart.backoff.initial": "1h"},
			worker.AsWorker("test_worker", failing(100, &runs))), worker.Run(), tel.populate())
		app.RequireStart()

		workertest.WaitForState(t, tel.pool, "test_worker", worker.StateRestarting)
		app.RequireStop()

		assert.Equal(t, worker.StateStopped, tel.pool.Worker("test_worker")[0].State)
		assert.Equal(t, int32(1), runs.Load())
	})
}

func TestConfig(t *testing.T) {
	t.Parallel()

	t.Run("worker config overrides the options", func(t *testing.T) {
		t.Parallel()

		var (
			tel  telemetry
			runs atomic.Int32
		)

		started := make(chan struct{}, 2)

		coretest.Start(t, newApp(map[string]any{
			"worker.workers.test_disabled.enabled":                false,
			"worker.workers.test_worker.replicas":                 2,
			"worker.workers.test_failing.restart.enabled":         false,
			"worker.workers.test_failing.restart.backoff.initial": "0s",
			"worker.workers.test_failing.restart.max_restarts":    0,
			"worker.workers.test_failing.restart.reset_after":     "0s",
			"worker.workers.test_failing.restart.backoff.max":     "0s",
			"worker.shutdown_on_failure":                          false,
		},
			worker.AsWorker("test_disabled", func(context.Context) error { panic("disabled") }),
			worker.AsWorker("test_worker", blocking(started), worker.WithReplicas(5)),
			worker.AsWorker("test_failing", failing(100, &runs), worker.WithRestart(worker.DefaultRestartPolicy)),
		), worker.Run(), tel.populate())
		receive(t, started, 2)

		assert.Len(t, workertest.WaitForState(t, tel.pool, "test_worker", worker.StateRunning), 2)
		assert.Equal(t, worker.StateDisabled, tel.pool.Worker("test_disabled")[0].State)
		workertest.WaitForState(t, tel.pool, "test_failing", worker.StateFailed)
		assert.Equal(t, int32(1), runs.Load())
		logtest.HasRecord(t, tel.logs, logtest.WithBody("worker disabled"), logtest.WithAttributeValue("worker", "test_disabled"))
	})

	t.Run("restart option", func(t *testing.T) {
		t.Parallel()

		var (
			tel  telemetry
			runs atomic.Int32
		)

		coretest.Start(t, newApp(map[string]any{"worker.shutdown_on_failure": false},
			worker.AsWorker("test_worker", failing(100, &runs), worker.WithRestart(worker.RestartPolicy{
				Enabled:        true,
				MaxRestarts:    1,
				InitialBackoff: time.Millisecond,
			})),
		), worker.Run(), tel.populate())

		workertest.WaitForState(t, tel.pool, "test_worker", worker.StateFailed)
		assert.Equal(t, int32(2), runs.Load())
	})

	t.Run("the run context carries the span", func(t *testing.T) {
		t.Parallel()

		var tel telemetry

		coretest.Start(t, newApp(nil, worker.AsWorker("test_worker", func(logger *slog.Logger) worker.WorkerFunc {
			return func(ctx context.Context) error {
				assert.True(t, trace.SpanContextFromContext(ctx).IsValid())
				logger.InfoContext(ctx, "test_log")

				return nil
			}
		})), worker.Run(), tel.populate())

		workertest.WaitForState(t, tel.pool, "test_worker", worker.StateStopped)

		spantest.HasSpan(t, tel.spans,
			spantest.WithName("worker test_worker"),
			spantest.WithKind(trace.SpanKindInternal),
			spantest.WithStatus(codes.Unset),
			spantest.WithAttributeValue(worker.NameKey.String("test_worker")),
			spantest.WithAttributeValue(worker.ReplicaKey.Int(0)),
		)
		logtest.HasRecord(t, tel.logs, logtest.WithBody("worker stopped"), logtest.WithAttributeValue("worker", "test_worker"))
		assert.Equal(t, int64(1), metrictest.Int64Sum(t, tel.metrics, worker.MetricRuns))
		assert.Equal(t, int64(0), metrictest.Int64Sum(t, tel.metrics, worker.MetricActive))

		spans := spantest.Find(tel.spans, spantest.WithName("worker test_worker"))
		require.Len(t, spans, 1)

		records := tel.logs.Filter(logtest.WithBody("test_log"))
		require.Len(t, records, 1)
		assert.Equal(t, spans[0].SpanContext.TraceID(), records[0].TraceID())
		assert.Equal(t, spans[0].SpanContext.SpanID(), records[0].SpanID())
	})
}

func TestPool(t *testing.T) {
	t.Parallel()

	t.Run("start and stop", func(t *testing.T) {
		t.Parallel()

		var pool *worker.Pool

		release := make(chan struct{})
		t.Cleanup(func() { close(release) })

		coretest.Start(t, newApp(nil, worker.AsWorker("test_stubborn", func(context.Context) error {
			<-release

			return nil
		})), fx.Populate(&pool))

		require.NoError(t, pool.Stop(t.Context()), "not started")
		require.NoError(t, pool.Start(t.Context()))
		require.EqualError(t, pool.Start(t.Context()), "worker pool already started")

		workertest.WaitForState(t, pool, "test_stubborn", worker.StateRunning)

		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
		defer cancel()

		require.ErrorContains(t, pool.Stop(ctx), "workers did not stop in time")
	})

	t.Run("unknown worker", func(t *testing.T) {
		t.Parallel()

		var pool *worker.Pool

		coretest.Start(t, newApp(nil), fx.Populate(&pool))

		assert.Nil(t, pool.Worker("test_missing"))
		assert.Empty(t, pool.Workers())
		require.EqualError(t, pool.RunOnce(t.Context(), "test_missing"), `unknown worker "test_missing"`)
	})
}

func TestRegistrationErrors(t *testing.T) {
	t.Parallel()

	noop := worker.WorkerFunc(func(context.Context) error { return nil })

	for name, tc := range map[string]struct {
		options []fx.Option
		values  map[string]any
		err     string
	}{
		"empty name":      {options: []fx.Option{worker.AsWorker("", noop)}, err: "cannot register worker: empty name"},
		"nil worker":      {options: []fx.Option{worker.AsWorker("test_worker", nil)}, err: `cannot register worker "test_worker": nil worker`},
		"unsupported":     {options: []fx.Option{worker.AsWorker("test_worker", 42)}, err: "unsupported int"},
		"duplicate":       {options: []fx.Option{worker.AsWorker("test_worker", noop), worker.AsWorker("test_worker", noop)}, err: `worker "test_worker" registered twice`},
		"invalid replica": {options: []fx.Option{worker.AsWorker("test_worker", noop, worker.WithReplicas(0))}, err: "invalid replicas 0"},
		"invalid max": {
			options: []fx.Option{worker.AsWorker("test_worker", noop)},
			values:  map[string]any{"worker.restart.max_restarts": -1},
			err:     "invalid max_restarts -1",
		},
		"invalid backoff": {
			options: []fx.Option{worker.AsWorker("test_worker", noop)},
			values:  map[string]any{"worker.workers.test_worker.restart.backoff.initial": "-1s"},
			err:     "invalid restart durations",
		},
		"constructor error": {
			options: []fx.Option{worker.AsWorker("test_worker", func() (worker.Worker, error) { return nil, errors.New("test_error") })},
			err:     "test_error",
		},
	} {
		app := fx.New(newApp(tc.values, tc.options...).Options(coretest.TestOptions(), fx.NopLogger, fx.Invoke(func(*worker.Pool) {}))...)
		require.ErrorContains(t, app.Err(), tc.err, name)
	}
}

func TestBackoffDelay(t *testing.T) {
	t.Parallel()

	policy := worker.RestartPolicy{InitialBackoff: time.Second, MaxBackoff: 5 * time.Second}

	for n, expected := range map[int]time.Duration{1: time.Second, 2: 2 * time.Second, 3: 4 * time.Second, 4: 5 * time.Second, 50: 5 * time.Second} {
		assert.Equal(t, expected, worker.BackoffDelay(policy, n), n)
	}

	assert.Equal(t, time.Duration(0), worker.BackoffDelay(worker.RestartPolicy{}, 3))
}

func TestFailedErrorAttributes(t *testing.T) {
	t.Parallel()

	err := &worker.FailedError{Name: "test_worker", Replica: 1, Err: errors.New("test_error")}
	assert.Equal(t, "worker test_worker (replica 1) failed: test_error", err.Error())
	assert.Equal(t, attribute.Key("worker.name"), worker.NameKey)
}

type testWorker struct {
	run worker.WorkerFunc
}

func (w *testWorker) Run(ctx context.Context) error {
	return w.run(ctx)
}
