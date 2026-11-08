---
title: Testing
description: Run cron jobs on demand in tests, and assert their telemetry.
---

Tests run the application with [coretest](../core/testing.md), without `cron.Run()`, and run a job immediately with `crontest.RunJob`, without waiting for its schedule. It returns the error of the job:

```go
func TestCleanupJob(t *testing.T) {
	t.Parallel()

	var (
		scheduler *cron.Scheduler
		spans     *tracetest.InMemoryExporter
		metrics   *sdkmetric.ManualReader
	)

	coretest.Start(t, core.New(cron.Module, cleanup.Module), fx.Populate(&scheduler, &spans, &metrics))

	require.NoError(t, crontest.RunJob(t, scheduler, "cleanup"))

	spantest.HasSpan(t, spans,
		spantest.WithName("cron cleanup"),
		spantest.WithAttributeValue(cron.JobOutcomeKey.String(cron.OutcomeSuccess)),
	)
	assert.Equal(t, int64(1), metrictest.Int64Sum(t, metrics, cron.MetricJobRuns,
		cron.JobNameKey.String("cleanup"), cron.JobOutcomeKey.String(cron.OutcomeSuccess)))
}
```

The execution goes through the whole module: configuration, timeout, panic recovery and instrumentation. A disabled job can be run too.

## On a schedule

To test the scheduling itself, start the scheduler with `cron.Run()`, and give the job a short interval with `crontest.WithSchedule`, which overrides `cron.jobs.<name>.schedule`:

```go
coretest.Start(t, app, crontest.WithSchedule("cleanup", "@every 10ms"), cron.Run())

assert.Eventually(t, func() bool { return job.Runs() >= 2 }, time.Second, 5*time.Millisecond)
```

When the test ends, the application stops and waits for the running jobs.

## Replace a dependency

Replace a dependency of the job with `fx.Replace` or `fx.Decorate`, see [Testing](../core/testing.md).
