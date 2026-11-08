# grpcclient

[![Go Reference](https://pkg.go.dev/badge/github.com/go-oryn/oryn/grpcclient.svg)](https://pkg.go.dev/github.com/go-oryn/oryn/grpcclient)
[![codecov](https://codecov.io/gh/go-oryn/oryn/graph/badge.svg?flag=grpcclient)](https://app.codecov.io/gh/go-oryn/oryn?flags%5B0%5D=grpcclient)

> gRPC client connections configured by name, with generated clients and interceptors registered through fx, and OpenTelemetry instrumentation.

```shell
go get github.com/go-oryn/oryn/grpcclient
```

```go
func main() {
	core.New(
		core.WithConfigFS(configs.FS),
		grpcclient.Module,
		grpcclient.AsClient("users", pb.NewUsersClient),
		fx.Provide(NewService),
	).Run()
}
```

```yaml
grpcclient:
  connections:
    users:
      target: users:50051
      insecure: true
```

## Documentation

| Page | Source |
|---|---|
| [gRPC client](https://go-oryn.github.io/oryn/grpcclient/overview/) (connections, clients, interceptors) | [docs/grpcclient/overview.md](../docs/grpcclient/overview.md) |
| [Configuration](https://go-oryn.github.io/oryn/grpcclient/configuration/) | [docs/grpcclient/configuration.md](../docs/grpcclient/configuration.md) |
| [Observability](https://go-oryn.github.io/oryn/grpcclient/observability/) | [docs/grpcclient/observability.md](../docs/grpcclient/observability.md) |
| [Testing](https://go-oryn.github.io/oryn/grpcclient/testing/) | [docs/grpcclient/testing.md](../docs/grpcclient/testing.md) |
