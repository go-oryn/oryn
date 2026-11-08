---
title: Testing
description: Run workers in tests, synchronously or with the application, and assert their telemetry.
---

Tests run the application with [coretest](../core/testing.md), and the `workertest` package runs the workers in memory.

## Run a worker once

`workertest.Run` runs a worker once, synchronously, in the test goroutine, with the dependencies and the in-memory telemetry of the test application. The run is instrumented like with `worker.Run()`, but not restarted:

```go
func TestOutboxWorker(t *testing.T) {
	t.Parallel()

	var (
		pool  *worker.Pool
		spans *tracetest.InMemoryExporter
	)

	coretest.Start(t, core.New(worker.Module, outbox.Module), fx.Populate(&pool, &spans))

	require.NoError(t, workertest.Run(t, pool, "outbox"))
	spantest.HasSpan(t, spans, spantest.WithName("worker outbox"), spantest.WithStatus(codes.Unset))
}
```

The context of the run is the test context, canceled when the test ends: a worker looping until its context is canceled must be run with the application instead, or stop by itself once its work is done in the test, for example when its queue is empty.

## Run with the application

Add `worker.Run()` to start the workers with the test application, like in production, and wait for them to reach a state with `workertest.WaitForState`:

```go
func TestOutboxWorkerRestarts(t *testing.T) {
	t.Parallel()

	var (
		pool *worker.Pool
		logs *logtest.InMemoryExporter
	)

	coretest.Start(t, core.New(worker.Module, outbox.Module),
		config.AsOptions(config.WithValues(map[string]any{
			"worker.restart.backoff.initial": "1ms",
			"worker.shutdown_on_failure":     false,
		})),
		worker.Run(),
		fx.Populate(&pool, &logs),
	)

	infos := workertest.WaitForState(t, pool, "outbox", worker.StateFailed)
	assert.Equal(t, 5, infos[0].Restarts)
	logtest.HasRecord(t, logs, logtest.WithBody("worker failed"))
}
```

`WaitForState` waits until all the replicas of the worker are in the state, and fails the test after 5 seconds; `WaitForStateWithin` sets another timeout. The workers are stopped when the test application stops, at the end of the test.

Use a small backoff in tests, and `worker.shutdown_on_failure: false` to keep the application running when a worker is expected to fail.

The in-memory span exporter is reset when the application stops: assert the spans of a running worker before the application stops, for example once it reached the `stopped` state.

## Replace the dependencies

Replace the dependencies of a worker with fakes with `fx.Replace` or `fx.Decorate`:

```go
coretest.Start(t, core.New(worker.Module, outbox.Module),
	fx.Replace(fx.Annotate(fakePublisher, fx.As(new(outbox.Publisher)))),
	fx.Populate(&pool),
)
```
