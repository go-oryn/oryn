# cron

[![Go Reference](https://pkg.go.dev/badge/github.com/go-oryn/oryn/cron.svg)](https://pkg.go.dev/github.com/go-oryn/oryn/cron)
[![codecov](https://codecov.io/gh/go-oryn/oryn/graph/badge.svg?flag=cron)](https://app.codecov.io/gh/go-oryn/oryn?flags%5B0%5D=cron)

> Scheduled jobs registered through fx, built on [gocron](https://github.com/go-co-op/gocron), with a trace, metrics and logs for every execution.

```shell
go get github.com/go-oryn/oryn/cron
```

```go
func main() {
	core.New(
		core.WithConfigFS(configs.FS),
		cron.Module,
		cron.AsJob("cleanup", "0 3 * * *", NewCleanupJob),
	).Run(cron.Run())
}
```

## Documentation

| Page | Source |
|---|---|
| [Cron](https://go-oryn.github.io/oryn/cron/overview/) (jobs, schedules, overlaps, timeouts, stop) | [docs/cron/overview.md](../docs/cron/overview.md) |
| [Configuration](https://go-oryn.github.io/oryn/cron/configuration/) | [docs/cron/configuration.md](../docs/cron/configuration.md) |
| [Observability](https://go-oryn.github.io/oryn/cron/observability/) | [docs/cron/observability.md](../docs/cron/observability.md) |
| [Testing](https://go-oryn.github.io/oryn/cron/testing/) | [docs/cron/testing.md](../docs/cron/testing.md) |
