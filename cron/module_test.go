package cron_test

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/core/coretest"
	"github.com/go-oryn/oryn/core/otel/oteltest/logtest"
	"github.com/go-oryn/oryn/core/otel/oteltest/metrictest"
	"github.com/go-oryn/oryn/core/otel/oteltest/spantest"
	"github.com/go-oryn/oryn/cron"
)

// never is a schedule that does not run during a test: once a year.
const never = "0 0 1 1 *"

// newApp returns an application with the cron module, the config values and the options.
func newApp(values map[string]any, options ...fx.Option) core.App {
	return core.New(append(options, cron.Module, config.AsOptions(config.WithValues(values)))...)
}

// telemetry is the in-memory telemetry of a test application.
type telemetry struct {
	scheduler *cron.Scheduler
	spans     *tracetest.InMemoryExporter
	logs      *logtest.InMemoryExporter
	metrics   *sdkmetric.ManualReader
}

// start starts the application, and returns its scheduler and telemetry.
func start(t *testing.T, app core.App, options ...fx.Option) *telemetry {
	t.Helper()

	var tel telemetry

	coretest.Start(t, app, append(options, fx.Populate(&tel.scheduler, &tel.spans, &tel.logs, &tel.metrics))...)

	return &tel
}

// counter is a job counting its runs.
type counter struct {
	runs atomic.Int64
}

func (c *counter) Run(context.Context) error {
	c.runs.Add(1)

	return nil
}

func TestRunNow(t *testing.T) {
	t.Parallel()

	t.Run("instrumented execution", func(t *testing.T) {
		t.Parallel()

		job := &counter{}
		tel := start(t, newApp(nil, cron.AsJob("test_job", never, job)))

		require.NoError(t, tel.scheduler.RunNow(t.Context(), "test_job"))
		assert.Equal(t, int64(1), job.runs.Load())

		spantest.HasSpan(t, tel.spans,
			spantest.WithName("cron test_job"),
			spantest.WithKind(trace.SpanKindInternal),
			spantest.WithStatus(codes.Unset),
			spantest.WithAttributeValue(cron.JobNameKey.String("test_job")),
			spantest.WithAttributeValue(cron.JobScheduleKey.String(never)),
			spantest.WithAttributeValue(cron.JobTriggerKey.String(cron.TriggerManual)),
			spantest.WithAttributeValue(cron.JobOutcomeKey.String(cron.OutcomeSuccess)),
		)
		assert.Equal(t, int64(1), metrictest.Int64Sum(t, tel.metrics, cron.MetricJobRuns,
			cron.JobNameKey.String("test_job"), cron.JobOutcomeKey.String(cron.OutcomeSuccess)))
		metrictest.HasMetric(t, tel.metrics, metrictest.WithName(cron.MetricJobDuration), metrictest.WithUnit("s"))
		logtest.HasRecord(t, tel.logs,
			logtest.WithBody("cron job succeeded"),
			logtest.WithLevel(slog.LevelInfo),
			logtest.WithAttributeValue("job", "test_job"),
			logtest.WithAttributeValue("trigger", cron.TriggerManual),
			logtest.WithAttribute("duration"),
		)
	})

	t.Run("child of the caller span", func(t *testing.T) {
		t.Parallel()

		var tracerProvider trace.TracerProvider

		tel := start(t, newApp(nil, cron.AsJob("test_job", never, &counter{})), fx.Populate(&tracerProvider))

		ctx, parent := tracerProvider.Tracer("test").Start(t.Context(), "parent")
		require.NoError(t, tel.scheduler.RunNow(ctx, "test_job"))
		parent.End()

		spantest.HasSpan(t, tel.spans, spantest.WithName("cron test_job"), spantest.WithParent(parent.SpanContext().SpanID()))
	})

	t.Run("span in the job context", func(t *testing.T) {
		t.Parallel()

		var spanName string

		job := cron.JobFunc(func(ctx context.Context) error {
			// The span of the execution is in the context.
			spanName = trace.SpanFromContext(ctx).SpanContext().SpanID().String()

			return nil
		})

		tel := start(t, newApp(nil, cron.AsJob("test_job", never, job)))
		require.NoError(t, tel.scheduler.RunNow(t.Context(), "test_job"))

		spans := spantest.Find(tel.spans, spantest.WithName("cron test_job"))
		require.Len(t, spans, 1)
		assert.Equal(t, spans[0].SpanContext.SpanID().String(), spanName)
	})

	t.Run("failure", func(t *testing.T) {
		t.Parallel()

		errJob := errors.New("test_error")
		tel := start(t, newApp(nil, cron.AsJob("test_job", never, func(context.Context) error { return errJob })))

		require.ErrorIs(t, tel.scheduler.RunNow(t.Context(), "test_job"), errJob)

		spantest.HasSpan(t, tel.spans,
			spantest.WithName("cron test_job"),
			spantest.WithStatus(codes.Error),
			spantest.WithAttributeValue(cron.JobOutcomeKey.String(cron.OutcomeFailure)),
		)
		assert.Equal(t, int64(1), metrictest.Int64Sum(t, tel.metrics, cron.MetricJobRuns, cron.JobOutcomeKey.String(cron.OutcomeFailure)))
		logtest.HasRecord(t, tel.logs,
			logtest.WithBody("cron job failed"),
			logtest.WithLevel(slog.LevelError),
			logtest.WithAttributeValue("exception.message", "test_error"),
		)
	})

	t.Run("panic", func(t *testing.T) {
		t.Parallel()

		tel := start(t, newApp(nil, cron.AsJob("test_job", never, func(context.Context) error { panic("test_panic") })))

		require.ErrorContains(t, tel.scheduler.RunNow(t.Context(), "test_job"), `cron job "test_job" panicked: test_panic`)

		spantest.HasSpan(t, tel.spans,
			spantest.WithName("cron test_job"),
			spantest.WithStatus(codes.Error),
			spantest.WithAttributeValue(cron.JobOutcomeKey.String(cron.OutcomePanic)),
		)
		assert.Equal(t, int64(1), metrictest.Int64Sum(t, tel.metrics, cron.MetricJobRuns, cron.JobOutcomeKey.String(cron.OutcomePanic)))
		logtest.HasRecord(t, tel.logs, logtest.WithBody("cron job panic"), logtest.WithAttribute("stack"))
		logtest.HasRecord(t, tel.logs, logtest.WithBody("cron job failed"))
	})

	t.Run("unknown job", func(t *testing.T) {
		t.Parallel()

		tel := start(t, newApp(nil))

		require.ErrorIs(t, tel.scheduler.RunNow(t.Context(), "missing"), cron.ErrJobNotFound)
	})

	t.Run("logs disabled", func(t *testing.T) {
		t.Parallel()

		tel := start(t, newApp(map[string]any{"cron.log.enabled": false}, cron.AsJob("test_job", never, &counter{})))

		require.NoError(t, tel.scheduler.RunNow(t.Context(), "test_job"))

		logtest.HasNoRecord(t, tel.logs, logtest.WithBodyContains("cron job"))
		spantest.HasSpan(t, tel.spans, spantest.WithName("cron test_job"))
	})
}

func TestAsJob(t *testing.T) {
	t.Parallel()

	t.Run("constructor with dependencies", func(t *testing.T) {
		t.Parallel()

		var got string

		newJob := func(cfg *config.Config) cron.Job {
			return cron.JobFunc(func(context.Context) error {
				got = cfg.GetString("test_string")

				return nil
			})
		}

		tel := start(t, newApp(map[string]any{"test_string": "test_value"}, cron.AsJob("test_job", never, newJob)))

		require.NoError(t, tel.scheduler.RunNow(t.Context(), "test_job"))
		assert.Equal(t, "test_value", got)
	})

	t.Run("registration order", func(t *testing.T) {
		t.Parallel()

		names := []string{"job_a", "job_b", "job_c", "job_d", "job_e"}
		options := make([]fx.Option, 0, len(names))

		for _, name := range names {
			options = append(options, cron.AsJob(name, never, &counter{}))
		}

		tel := start(t, newApp(nil, options...))

		jobs := tel.scheduler.Jobs()
		got := make([]string, 0, len(jobs))

		for _, info := range jobs {
			got = append(got, info.Name)
		}

		assert.Equal(t, names, got)
	})

	t.Run("errors", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct {
			option fx.Option
			values map[string]any
			err    string
		}{
			"empty name":       {option: cron.AsJob(" ", never, &counter{}), err: "cannot register cron job: empty name"},
			"nil job":          {option: cron.AsJob("test_job", never, nil), err: `cannot register cron job "test_job": nil job`},
			"unsupported job":  {option: cron.AsJob("test_job", never, "test_string"), err: "unsupported string"},
			"invalid cron":     {option: cron.AsJob("test_job", "invalid", &counter{}), err: `invalid schedule "invalid" of cron job "test_job"`},
			"invalid every":    {option: cron.AsJob("test_job", "@every soon", &counter{}), err: `invalid schedule "@every soon"`},
			"negative every":   {option: cron.AsJob("test_job", "@every -1s", &counter{}), err: "the interval must be positive"},
			"invalid override": {option: cron.AsJob("test_job", never, &counter{}), values: map[string]any{"cron.jobs.test_job.schedule": "invalid"}, err: `invalid schedule "invalid"`},
			"invalid location": {option: cron.AsJob("test_job", never, &counter{}), values: map[string]any{"cron.location": "Mars/Base"}, err: "invalid cron.location"},
			"duplicate name": {
				option: fx.Options(cron.AsJob("test_job", never, &counter{}), cron.AsJob("test_job", never, &counter{})),
				err:    `duplicate cron job "test_job"`,
			},
		} {
			app := newApp(tc.values, tc.option).Build(coretest.TestOptions(), fx.NopLogger, fx.Invoke(func(*cron.Scheduler) {}))
			require.ErrorContains(t, app.Err(), tc.err, name)
		}
	})
}

func TestRun(t *testing.T) {
	t.Parallel()

	t.Run("scheduled executions", func(t *testing.T) {
		t.Parallel()

		job := &counter{}
		tel := start(t, newApp(nil, cron.AsJob("test_job", "@every 10ms", job)), cron.Run())

		require.Eventually(t, func() bool { return job.runs.Load() >= 2 }, 5*time.Second, 5*time.Millisecond)

		// A scheduled execution is a trace of its own.
		spans := spantest.Find(tel.spans, spantest.WithName("cron test_job"),
			spantest.WithAttributeValue(cron.JobTriggerKey.String(cron.TriggerSchedule)))
		require.NotEmpty(t, spans)
		assert.False(t, spans[0].Parent.IsValid())
	})

	t.Run("cron expression with seconds", func(t *testing.T) {
		t.Parallel()

		tel := start(t, newApp(nil, cron.AsJob("test_job", "*/1 * * * * *", &counter{})), cron.Run())

		info := tel.scheduler.Jobs()[0]
		assert.Equal(t, "*/1 * * * * *", info.Schedule)
		assert.True(t, info.Enabled)
		assert.WithinDuration(t, time.Now(), info.NextRun, 2*time.Second)
	})

	t.Run("schedule override", func(t *testing.T) {
		t.Parallel()

		job := &counter{}
		start(t, newApp(map[string]any{"cron.jobs.test_job.schedule": "@every 10ms"}, cron.AsJob("test_job", never, job)), cron.Run())

		require.Eventually(t, func() bool { return job.runs.Load() >= 1 }, 5*time.Second, 5*time.Millisecond)
	})

	t.Run("disabled job", func(t *testing.T) {
		t.Parallel()

		job := &counter{}
		tel := start(t, newApp(map[string]any{"cron.jobs.test_job.enabled": false}, cron.AsJob("test_job", "@every 1ms", job)), cron.Run())

		info := tel.scheduler.Jobs()[0]
		assert.False(t, info.Enabled)
		assert.True(t, info.NextRun.IsZero())

		time.Sleep(20 * time.Millisecond)
		assert.Equal(t, int64(0), job.runs.Load(), "a disabled job is not scheduled")

		// It can still be run on demand.
		require.NoError(t, tel.scheduler.RunNow(t.Context(), "test_job"))
		assert.Equal(t, int64(1), job.runs.Load())
	})

	t.Run("location", func(t *testing.T) {
		t.Parallel()

		tel := start(t, newApp(map[string]any{"cron.location": "Asia/Tokyo"}, cron.AsJob("test_job", "0 9 * * *", &counter{})), cron.Run())

		next := tel.scheduler.Jobs()[0].NextRun
		tokyo, err := time.LoadLocation("Asia/Tokyo")
		require.NoError(t, err)
		assert.Equal(t, 9, next.In(tokyo).Hour())
	})

	t.Run("not started", func(t *testing.T) {
		t.Parallel()

		job := &counter{}
		tel := start(t, newApp(nil, cron.AsJob("test_job", "@every 1ms", job)))

		time.Sleep(20 * time.Millisecond)
		assert.Equal(t, int64(0), job.runs.Load(), "jobs are only scheduled with cron.Run()")
		require.NoError(t, tel.scheduler.RunNow(t.Context(), "test_job"))
	})
}

// overlapping is a job recording its maximum number of concurrent runs.
type overlapping struct {
	active, max, runs atomic.Int64
}

func (o *overlapping) Run(context.Context) error {
	active := o.active.Add(1)
	defer o.active.Add(-1)

	for {
		current := o.max.Load()
		if active <= current || o.max.CompareAndSwap(current, active) {
			break
		}
	}

	time.Sleep(30 * time.Millisecond)
	o.runs.Add(1)

	return nil
}

func TestSingleton(t *testing.T) {
	t.Parallel()

	t.Run("no overlap by default", func(t *testing.T) {
		t.Parallel()

		job := &overlapping{}
		start(t, newApp(nil, cron.AsJob("test_job", "@every 5ms", job)), cron.Run())

		require.Eventually(t, func() bool { return job.runs.Load() >= 3 }, 5*time.Second, 5*time.Millisecond)
		assert.Equal(t, int64(1), job.max.Load())
	})

	for name, tc := range map[string]struct {
		values  map[string]any
		options []cron.JobOption
	}{
		"option":     {options: []cron.JobOption{cron.WithSingleton(false)}},
		"global key": {values: map[string]any{"cron.singleton": false}},
		"job key":    {values: map[string]any{"cron.jobs.test_job.singleton": false}, options: []cron.JobOption{cron.WithSingleton(true)}},
	} {
		t.Run("overlap with "+name, func(t *testing.T) {
			t.Parallel()

			job := &overlapping{}
			start(t, newApp(tc.values, cron.AsJob("test_job", "@every 5ms", job, tc.options...)), cron.Run())

			require.Eventually(t, func() bool { return job.max.Load() >= 2 }, 5*time.Second, 5*time.Millisecond)
		})
	}
}

// waiting is a job waiting for its context to be done.
func waiting(ctx context.Context) error {
	<-ctx.Done()

	return ctx.Err()
}

func TestTimeout(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		values  map[string]any
		options []cron.JobOption
	}{
		"option":     {options: []cron.JobOption{cron.WithTimeout(10 * time.Millisecond)}},
		"global key": {values: map[string]any{"cron.timeout": "10ms"}},
		"job key":    {values: map[string]any{"cron.jobs.test_job.timeout": "10ms"}, options: []cron.JobOption{cron.WithTimeout(time.Hour)}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tel := start(t, newApp(tc.values, cron.AsJob("test_job", never, waiting, tc.options...)))

			require.ErrorIs(t, tel.scheduler.RunNow(t.Context(), "test_job"), context.DeadlineExceeded)
		})
	}

	t.Run("job key 0s removes the global timeout", func(t *testing.T) {
		t.Parallel()

		values := map[string]any{"cron.timeout": "1ms", "cron.jobs.test_job.timeout": "0s"}
		job := func(ctx context.Context) error {
			if _, ok := ctx.Deadline(); ok {
				return errors.New("unexpected deadline")
			}

			return nil
		}

		tel := start(t, newApp(values, cron.AsJob("test_job", never, job)))

		require.NoError(t, tel.scheduler.RunNow(t.Context(), "test_job"))
	})
}

func TestStop(t *testing.T) {
	t.Parallel()

	t.Run("waits for the running jobs", func(t *testing.T) {
		t.Parallel()

		var (
			started  = make(chan struct{})
			finished atomic.Bool
		)

		job := func(context.Context) error {
			select {
			case started <- struct{}{}:
			default:
			}

			time.Sleep(50 * time.Millisecond)
			finished.Store(true)

			return nil
		}

		app := coretest.New(t, newApp(nil, cron.AsJob("test_job", "@every 5ms", job)), cron.Run())
		app.RequireStart()

		<-started
		app.RequireStop()

		assert.True(t, finished.Load(), "the running job finished before the application stopped")
	})

	t.Run("cancels the jobs when the stop times out", func(t *testing.T) {
		t.Parallel()

		var (
			started  = make(chan struct{}, 1)
			canceled atomic.Bool
		)

		job := func(ctx context.Context) error {
			select {
			case started <- struct{}{}:
			default:
			}

			<-ctx.Done()
			canceled.Store(true)

			return ctx.Err()
		}

		app := coretest.New(t, newApp(nil, cron.AsJob("test_job", "@every 5ms", job)), cron.Run())
		app.RequireStart()

		<-started

		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()

		// fx gives up on the stop hook once the stop context is done.
		require.ErrorIs(t, app.Stop(ctx), context.DeadlineExceeded)
		assert.Eventually(t, canceled.Load, 5*time.Second, 5*time.Millisecond)
	})
}

func TestEnvironmentVariables(t *testing.T) {
	t.Setenv("APP_CRON_JOBS_TEST_JOB_ENABLED", "false")

	tel := start(t, newApp(nil, cron.AsJob("test_job", never, &counter{})))

	assert.False(t, tel.scheduler.Jobs()[0].Enabled)
}
