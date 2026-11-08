---
title: Configuration
description: Every worker configuration key, with its default and when to change it.
---

The `worker.*` keys configure the restart policy of the workers, globally and per worker. All of them are optional.

## Reference

```yaml
worker:
  shutdown_on_failure: true   # shut the application down when a worker fails
  restart:
    enabled: true             # restart the failed runs
    max_restarts: 5           # consecutive restarts before failing, 0 = unlimited
    backoff:
      initial: 1s             # delay before the first restart
      max: 30s                # max delay between restarts
    reset_after: 1m           # run duration resetting the consecutive restarts, 0 = never
  workers:
    <name>:                   # per worker, overrides the options set in code
      enabled: true           # run the worker
      replicas: 1             # concurrent replicas
      restart:                # same keys as worker.restart, defaulting to them
        enabled: true
        max_restarts: 5
        backoff:
          initial: 1s
          max: 30s
        reset_after: 1m
```

| Key | Type | Default | Environment variable |
|---|---|---|---|
| `worker.shutdown_on_failure` | bool | `true` | `APP_WORKER_SHUTDOWN_ON_FAILURE` |
| `worker.restart.enabled` | bool | `true` | `APP_WORKER_RESTART_ENABLED` |
| `worker.restart.max_restarts` | int | `5` (`0` = unlimited) | `APP_WORKER_RESTART_MAX_RESTARTS` |
| `worker.restart.backoff.initial` | duration | `1s` | `APP_WORKER_RESTART_BACKOFF_INITIAL` |
| `worker.restart.backoff.max` | duration | `30s` | `APP_WORKER_RESTART_BACKOFF_MAX` |
| `worker.restart.reset_after` | duration | `1m` (`0s` = never) | `APP_WORKER_RESTART_RESET_AFTER` |
| `worker.workers.<name>.enabled` | bool | `true` | `APP_WORKER_WORKERS_<NAME>_ENABLED` |
| `worker.workers.<name>.replicas` | int | `1`, or `WithReplicas` | `APP_WORKER_WORKERS_<NAME>_REPLICAS` |
| `worker.workers.<name>.restart.*` | | `worker.restart.*`, or `WithRestart` | `APP_WORKER_WORKERS_<NAME>_RESTART_*` |

Durations use the Go syntax: `500ms`, `30s`, `2m`. An explicit zero is a value: `max_restarts: 0` means unlimited restarts, it does not fall back to the default.

For a worker, the settings are resolved from the highest precedence:

1. the `worker.workers.<name>.*` keys,
2. the `WithReplicas` and `WithRestart` options set in code,
3. the `worker.restart.*` keys,
4. the defaults.

The application fails to start with less than 1 replica, a negative `max_restarts` or a negative duration. A `backoff.max` lower than `backoff.initial` is raised to it.

## Restarts

The backoff doubles at each consecutive restart: with the defaults, `1s`, `2s`, `4s`, `8s`, `16s`, then the worker fails. For a worker that must never give up, like a queue consumer waiting for its broker, allow unlimited restarts:

```yaml
worker:
  restart:
    max_restarts: 0
    backoff:
      max: 1m
```

To fail on the first error instead, and let the orchestrator restart the application:

```yaml
worker:
  restart:
    enabled: false
```

## Failures

By default, a failed worker shuts the application down with exit code 1. To keep the application running, and only report the failure with the [health probe](overview.md#health) and the logs:

```yaml
worker:
  shutdown_on_failure: false
```

The readiness probe then fails, so the instance stops receiving traffic, and the liveness probe lets the orchestrator restart it.

## Per worker

The keys under `worker.workers.<name>` apply to the worker registered with that name, and override the options set in code:

```yaml
worker:
  workers:
    orders-consumer:
      replicas: 8
      restart:
        max_restarts: 0
    outbox:
      enabled: false
```

A disabled worker is not started, its state is `disabled`, and it is healthy.

## Per environment

```yaml
# configs/app.yaml
worker:
  workers:
    orders-consumer:
      replicas: 8

# configs/dev/worker.yaml (APP_ENV=dev)
worker:
  workers:
    orders-consumer:
      replicas: 1
    outbox:
      enabled: false
```

See [Configuration](../core/configuration.md) for the loading rules.
