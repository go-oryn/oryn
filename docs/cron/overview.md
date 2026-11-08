---
title: Cron
description: The cron module, scheduled jobs registered through fx, run with the application and instrumented with OpenTelemetry.
---

The `cron` module runs jobs on a schedule, with the application. It is built on [gocron](https://github.com/go-co-op/gocron), and every execution is traced, measured and logged.

```shell
go get github.com/go-oryn/oryn/cron
```

## Quick start

```go
func main() {
	core.New(
		core.WithConfigFS(configs.FS),
		cron.Module,
		cron.AsJob("cleanup", "0 3 * * *", NewCleanupJob),
	).Run(cron.Run())
}
```

- `cron.Module` provides the `*cron.Scheduler`, with the registered jobs.
- `cron.Run()` starts the scheduler with the application, and stops it gracefully when the application stops, see [Stop](#stop). Without it, the jobs only run on demand.
- Every execution is traced, measured and logged, see [Observability](observability.md).

## Jobs

A job implements `cron.Job`:

```go
type Job interface {
	Run(ctx context.Context) error
}
```

Register it with a unique name and a schedule. The job is either a value, a plain `func(context.Context) error`, or a constructor whose parameters are injected by fx:

```go
type CleanupJob struct {
	repository *Repository
}

func NewCleanupJob(repository *Repository) *CleanupJob {
	return &CleanupJob{repository: repository}
}

func (j *CleanupJob) Run(ctx context.Context) error {
	return j.repository.DeleteExpired(ctx)
}
```

```go
cron.AsJob("cleanup", "0 3 * * *", NewCleanupJob)
cron.AsJob("heartbeat", "@every 30s", func(ctx context.Context) error { ... })
```

The context of a job carries the span of the execution, and is canceled when the job times out, or when the application stop times out. A returned error or a panic marks the execution as failed: it is recorded, and the job runs again at its next schedule.

The application fails to start if a name is empty or registered twice, if a job is not a `cron.Job` or a constructor returning one, or if a schedule is invalid.

## Schedules

| Schedule | Runs |
|---|---|
| `0 3 * * *` | a cron expression, with 5 fields: minute, hour, day of month, month, day of week |
| `*/10 * * * * *` | a cron expression, with 6 fields, the first one being the second |
| `@hourly`, `@daily`, `@weekly`, `@monthly`, `@yearly` | a predefined schedule |
| `@every 1m30s` | at a fixed interval, any positive Go duration, the first run one interval after the scheduler starts |

Cron expressions are evaluated in the `cron.location` time zone, UTC by default. The configuration can override the schedule of a job, or disable it:

```yaml
cron:
  location: Europe/Paris
  jobs:
    cleanup:
      schedule: "0 4 * * *"
    heartbeat:
      enabled: false
```

See [Configuration](configuration.md).

## Overlaps

By default, a job does not overlap itself: if it is still running when it is due again, that run is skipped, and the job runs at the following schedule. To allow concurrent runs of a job:

```go
cron.AsJob("poll", "@every 10s", NewPollJob, cron.WithSingleton(false))
```

or for every job, with `cron.singleton: false`.

## Timeouts

Jobs have no timeout by default. Bound an execution with `cron.WithTimeout`, `cron.timeout` for every job, or `cron.jobs.<name>.timeout`:

```go
cron.AsJob("report", "@hourly", NewReportJob, cron.WithTimeout(10*time.Minute))
```

Once the timeout expires, the job context is canceled: the job should return. Its error, usually `context.DeadlineExceeded`, marks the execution as failed.

## Run on demand

The `*cron.Scheduler` runs a job immediately with `RunNow`, whatever its schedule, even if it is disabled or the scheduler is not started. It returns the error of the job, or `cron.ErrJobNotFound`. The execution is instrumented like a scheduled one, as a child of the span of the given context, for example from an admin endpoint:

```go
func (h *AdminHandler) Run(w http.ResponseWriter, r *http.Request) {
	if err := h.scheduler.RunNow(r.Context(), r.PathValue("job")); err != nil {
		...
	}
}
```

`Jobs` lists the jobs in registration order, with their name, schedule, whether they are enabled, and their next run once the scheduler is started.

## Stop

When the application stops, the scheduler stops scheduling jobs, and waits for the running ones to finish. If the stop timeout expires first, the context of the running jobs is canceled, and the application stop fails with `context.DeadlineExceeded`.

## Configuration

The `cron.*` keys set the time zone, the overlaps, the timeouts and the execution logs, and override the schedule of each job. See [Configuration](configuration.md).
