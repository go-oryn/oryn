# grpcserver

[![Go Reference](https://pkg.go.dev/badge/github.com/go-oryn/oryn/grpcserver.svg)](https://pkg.go.dev/github.com/go-oryn/oryn/grpcserver)
[![codecov](https://codecov.io/gh/go-oryn/oryn/graph/badge.svg?flag=grpcserver)](https://app.codecov.io/gh/go-oryn/oryn?flags%5B0%5D=grpcserver)

> gRPC server with services and interceptors registered through fx, the gRPC health checking protocol backed by the application health checks, and OpenTelemetry instrumentation.

```shell
go get github.com/go-oryn/oryn/grpcserver
```

```go
func main() {
	core.New(
		core.WithConfigFS(configs.FS),
		grpcserver.Module,
		grpcserver.AsService(&pb.Users_ServiceDesc, NewUsersService),
	).Run(grpcserver.Serve())
}
```

## Documentation

| Page | Source |
|---|---|
| [gRPC server](https://go-oryn.github.io/oryn/grpcserver/overview/) (services, interceptors, health, configuration) | [docs/grpcserver/overview.md](../docs/grpcserver/overview.md) |
| [Observability](https://go-oryn.github.io/oryn/grpcserver/observability/) | [docs/grpcserver/observability.md](../docs/grpcserver/observability.md) |
| [Testing](https://go-oryn.github.io/oryn/grpcserver/testing/) | [docs/grpcserver/testing.md](../docs/grpcserver/testing.md) |
