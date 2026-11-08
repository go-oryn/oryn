---
title: Configuration
description: Every cron configuration key, with its default and when to change it.
---

The `cron.*` keys configure the scheduler and override the registered jobs. All of them are optional.

## Reference

```yaml
cron:
  location: UTC               # time zone of the cron expressions
  singleton: true             # skip a run while the previous one of the job is still running
  timeout: 0s                 # max duration of an execution, 0 = none
  log:
    enabled: true             # log every execution
  jobs:
    <name>:                   # the name given to cron.AsJob
      schedule: "0 3 * * *"   # replaces the registered schedule
      enabled: true           # false: never scheduled, still runnable with RunNow
      singleton: true         # cron.singleton for this job
      timeout: 0s             # cron.timeout for this job, 0s = none
```

| Key | Type | Default | Environment variable |
|---|---|---|---|
| `cron.location` | string | `UTC` | `APP_CRON_LOCATION` |
| `cron.singleton` | bool | `true` | `APP_CRON_SINGLETON` |
| `cron.timeout` | duration | `0s` (none) | `APP_CRON_TIMEOUT` |
| `cron.log.enabled` | bool | `true` | `APP_CRON_LOG_ENABLED` |
| `cron.jobs.<name>.schedule` | string | the registered schedule | `APP_CRON_JOBS_<NAME>_SCHEDULE` |
| `cron.jobs.<name>.enabled` | bool | `true` | `APP_CRON_JOBS_<NAME>_ENABLED` |
| `cron.jobs.<name>.singleton` | bool | `WithSingleton`, then `cron.singleton` | `APP_CRON_JOBS_<NAME>_SINGLETON` |
| `cron.jobs.<name>.timeout` | duration | `WithTimeout`, then `cron.timeout` | `APP_CRON_JOBS_<NAME>_TIMEOUT` |

Durations use the Go syntax: `500ms`, `30s`, `2m`. In environment variables, the job name is upper cased: `APP_CRON_JOBS_CLEANUP_SCHEDULE`. Use names made of letters, digits and underscores, so that they can be set from the environment.

For each job, a `cron.jobs.<name>.*` key wins over the job option, which wins over the global key.

## Time zone

Cron expressions are evaluated in `cron.location`, an [IANA time zone](https://en.wikipedia.org/wiki/List_of_tz_database_time_zones) name, `UTC` by default. `Local` is the time zone of the host:

```yaml
cron:
  location: Europe/Paris
```

Prefer UTC for jobs scheduled at night: with a time zone observing daylight saving time, the hours around the change are skipped or repeated once a year. An unknown time zone fails the application start. `@every` intervals do not depend on the time zone.

## Schedules

Change the schedule of a job without a new build, for example per environment, or disable it:

```yaml
cron:
  jobs:
    cleanup:
      schedule: "@every 1m"
    report:
      enabled: false
```

A disabled job is not scheduled, but stays listed by `Scheduler.Jobs`, and can still be run with `Scheduler.RunNow`. An invalid schedule fails the application start. See [Schedules](overview.md#schedules) for the syntax.

## Overlaps

A run is skipped while the previous one of the same job is still running. Allow overlapping runs for every job, or a single one:

```yaml
cron:
  singleton: false
  jobs:
    cleanup:
      singleton: true
```

## Timeouts

Jobs have no timeout by default. Set one for every job, and override it per job:

```yaml
cron:
  timeout: 5m
  jobs:
    report:
      timeout: 30m
```

The job context is canceled once the timeout expires. A `0s` job key removes the timeout for that job, whatever the job option and the global key.

## Execution logs

Every execution is logged, see [Observability](observability.md#logs). Disable the logs when the traces are enough, or for jobs running every few seconds:

```yaml
cron:
  log:
    enabled: false
```

Panics are logged even when the execution logs are disabled.

## Per environment

```yaml
# configs/app.yaml
cron:
  jobs:
    cleanup:
      schedule: "0 3 * * *"

# configs/dev/cron.yaml (APP_ENV=dev)
cron:
  jobs:
    cleanup:
      schedule: "@every 1m"
```

See [Configuration](../core/configuration.md) for the loading rules.
