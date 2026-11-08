---
title: Observability
description: Traces, metrics and logs of the cron job executions.
---

Every execution of a job, scheduled or run on demand, is traced, measured and logged with the application providers, under the `github.com/go-oryn/oryn/cron` instrumentation scope. A panic in a job is recovered, and recorded as a failure.

## Traces

Each execution gets an internal span:

- named `cron <name>`: `cron cleanup`,
- with the `cron.job.name`, `cron.job.schedule`, `cron.job.trigger` and `cron.job.outcome` attributes (see below),
- with an error status and the recorded error when the job fails.

A scheduled execution starts a new trace. An execution triggered by `Scheduler.RunNow` is a child of the span of its context. Spans created in the job with its context are children of the execution span.

| Attribute | Values |
|---|---|
| `cron.job.name` | `cleanup` |
| `cron.job.schedule` | `0 3 * * *` |
| `cron.job.trigger` | `schedule`, or `manual` for `RunNow` |
| `cron.job.outcome` | `success`, `failure` (the job returned an error) or `panic` |

## Metrics

| Metric | Type | Description |
|---|---|---|
| `cron.job.runs` | counter | Number of executions, with the `cron.job.name` and `cron.job.outcome` attributes |
| `cron.job.duration` | histogram, `s` | Duration of the executions, with the `cron.job.name` and `cron.job.outcome` attributes |

Alert on failures with `cron.job.runs{cron.job.outcome!="success"}`, and on a job that stopped running with the absence of increase of `cron.job.runs`.

## Logs

| Message | Level | Attributes |
|---|---|---|
| `cron job started` | debug | `job`, `trigger` |
| `cron job succeeded` | info | `job`, `trigger`, `duration` |
| `cron job failed` | error | `job`, `trigger`, `duration`, `error` |
| `cron job panic` | error | `job`, `panic`, `stack` |

The logs are correlated with the execution span. Disable the execution logs with `cron.log.enabled: false`; `cron job panic` is always logged.

When the scheduler starts, `cron scheduler started` is logged at debug level, with the number of jobs.
