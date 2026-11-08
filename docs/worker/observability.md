---
title: Observability
description: Traces, metrics and logs of the worker runs.
---

The module uses the tracer, meter and logger providers of the application, with the `github.com/go-oryn/oryn/worker` instrumentation scope.

## Traces

Each run of a worker replica gets its own root span, of kind internal:

- named `worker <name>`: `worker outbox`,
- with the `worker.name` and `worker.replica` attributes,
- with an error status, the recorded error and the `error.type` attribute for a failed run: the Go type of the error (`*net.OpError`), or `panic`.

A run stopped by the application stopping is not an error. The context passed to `Run` carries the span: the spans created by the worker are its children, and its logs are correlated with it.

For a worker processing items in a loop, the span of a run can last for the whole life of the application. Create a span per item in the worker, as a child of the run span or as a new root, linked to the message for a queue consumer.

## Metrics

| Metric | Type | Unit | Attributes | Description |
|---|---|---|---|---|
| `worker.runs` | counter | `{run}` | `worker.name`, `error.type` (failed runs) | Number of worker runs, by outcome |
| `worker.restarts` | counter | `{restart}` | `worker.name` | Number of worker restarts |
| `worker.active` | up-down counter | `{replica}` | `worker.name` | Number of running worker replicas |

## Logs

| Log | Level | Attributes |
|---|---|---|
| `worker started` | info | `worker`, `replica` |
| `worker stopped` | info | `worker`, `replica` |
| `worker restarting` | warn | `worker`, `replica`, the error, `restart` (consecutive restart number), `backoff` |
| `worker failed` | error | `worker`, `replica`, the error |
| `worker panic` | error | `worker`, `replica`, `panic`, `stack` |
| `worker disabled` | info | `worker` |

The run logs are correlated with the run span.
