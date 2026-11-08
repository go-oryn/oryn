---
title: Worker
description: The worker module, long-running background workers run with the application, restarted when they fail, and reported by a health probe.
---

The `worker` module runs long-running background workers with the application: queue consumers, pollers, outbox relays... Each worker runs in its own goroutines, is restarted with a backoff when it fails or panics, is reported by a [health probe](../core/health.md), and is instrumented with OpenTelemetry.

```shell
go get github.com/go-oryn/oryn/worker
```

## Quick start

```go
func main() {
	core.New(
		core.WithConfigFS(configs.FS),
		worker.Module,
		worker.AsWorker("outbox", NewOutboxWorker),
	).Run(worker.Run())
}
```

- `worker.Module` provides the `*worker.Pool` of the registered workers, and registers its liveness and readiness probe.
- `worker.Run()` starts the workers with the application, and stops them when the application stops.
- Every run is traced, measured and logged, see [Observability](observability.md).

## Workers

A worker implements `worker.Worker`:

```go
type Worker interface {
	Run(ctx context.Context) error
}
```

`Run` blocks until the work is done, or until `ctx` is canceled when the application stops; it should then return promptly. A typical worker loops until its context is canceled:

```go
type OutboxWorker struct {
	repository *Repository
	publisher  *Publisher
}

func NewOutboxWorker(repository *Repository, publisher *Publisher) *OutboxWorker {
	return &OutboxWorker{repository: repository, publisher: publisher}
}

func (w *OutboxWorker) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := w.relay(ctx); err != nil {
				return err // restarted with a backoff
			}
		}
	}
}
```

Register it under a unique name, used in the config keys, the logs and the telemetry. The worker is either a value (a `worker.Worker`, a `worker.WorkerFunc` or a `func(context.Context) error`), or a constructor returning one, whose parameters are injected by fx:

```go
worker.AsWorker("outbox", NewOutboxWorker)
worker.AsWorker("cleanup", func(ctx context.Context) error { ... })
```

The application fails to start if the worker is not a worker or a constructor returning one, or if a name is registered twice.

## Run outcomes

| `Run`... | Then the replica is |
|---|---|
| returns `nil` | `stopped`, and not restarted: the work is done |
| returns an error, or panics | `restarting`, then run again after a backoff, see [Restarts](#restarts) |
| returns after its context was canceled | `stopped`: the application is stopping |

A panic is recovered, logged with its stack, and handled like an error wrapping `worker.ErrPanic`.

## Replicas

`worker.WithReplicas(n)` runs `n` concurrent replicas of the worker, each with its own `Run` call, its own restarts and its own state. Use it for queue consumers:

```go
worker.AsWorker("orders-consumer", NewOrdersConsumer, worker.WithReplicas(4))
```

## Restarts

A failed run is restarted after a backoff, starting at `1s` and doubling up to `30s`. After `5` consecutive restarts, the replica is `failed`: it is not restarted anymore, an error is logged, the [probe](#health) fails, and the application shuts down with exit code 1, so that its orchestrator restarts it.

A run lasting at least `1m` resets the consecutive restarts: a worker failing once a day is not failed after 5 days.

Change the policy in the [configuration](configuration.md#restarts), or in code with `worker.WithRestart`:

```go
worker.AsWorker("outbox", NewOutboxWorker, worker.WithRestart(worker.RestartPolicy{
	Enabled:        true,
	MaxRestarts:    0, // unlimited
	InitialBackoff: 100 * time.Millisecond,
	MaxBackoff:     time.Minute,
	ResetAfter:     time.Minute,
}))
```

The `worker.workers.<name>.*` config keys override the options set in code.

## Stop

When the application stops, the context of the workers is canceled, and the application waits for them to return, until the stop timeout of the application (`fx.StopTimeout`, 15s by default). A worker in its restart backoff is stopped right away.

## Health

The module registers a liveness and readiness probe, named `worker`, failing when a worker replica is `failed`:

```
worker test_worker (replica 0) failed: connection refused
```

A `stopped` or `disabled` worker is healthy. See [Health](../core/health.md).

## Pool

The `*worker.Pool` reports the state of the workers, for example to expose them on an admin endpoint:

```go
for _, info := range pool.Workers() {
	fmt.Println(info.Name, info.Replica, info.State, info.Restarts, info.Err)
}
```

The states are `pending`, `running`, `restarting`, `stopped`, `failed` and `disabled`.
