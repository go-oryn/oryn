# worker

[![Go Reference](https://pkg.go.dev/badge/github.com/go-oryn/oryn/worker.svg)](https://pkg.go.dev/github.com/go-oryn/oryn/worker)
[![codecov](https://codecov.io/gh/go-oryn/oryn/graph/badge.svg?flag=worker)](https://app.codecov.io/gh/go-oryn/oryn?flags%5B0%5D=worker)

> Long-running background workers registered through fx, run with the application, restarted with a backoff when they fail or panic, reported by a health probe, and instrumented with OpenTelemetry.

```shell
go get github.com/go-oryn/oryn/worker
```

```go
func main() {
	core.New(
		core.WithConfigFS(configs.FS),
		worker.Module,
		worker.AsWorker("ticker", func(ctx context.Context) error {
			for {
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(time.Second):
					slog.InfoContext(ctx, "tick")
				}
			}
		}),
	).Run(worker.Run())
}
```

## Documentation

| Page | Source |
|---|---|
| [Worker](https://go-oryn.github.io/oryn/worker/overview/) | [docs/worker/overview.md](../docs/worker/overview.md) |
| [Configuration](https://go-oryn.github.io/oryn/worker/configuration/) | [docs/worker/configuration.md](../docs/worker/configuration.md) |
| [Observability](https://go-oryn.github.io/oryn/worker/observability/) | [docs/worker/observability.md](../docs/worker/observability.md) |
| [Testing](https://go-oryn.github.io/oryn/worker/testing/) | [docs/worker/testing.md](../docs/worker/testing.md) |
