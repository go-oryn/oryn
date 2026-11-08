---
title: gRPC server
description: The grpcserver module, a gRPC server with services and interceptors registered through fx, and the gRPC health checking protocol.
---

The `grpcserver` module provides a [gRPC](https://grpc.io/docs/languages/go/) server, instrumented with OpenTelemetry, with the gRPC health checking protocol backed by the [application health checks](../core/health.md).

```shell
go get github.com/go-oryn/oryn/grpcserver
```

## Quick start

```go
func main() {
	core.New(
		core.WithConfigFS(configs.FS),
		grpcserver.Module,
		grpcserver.AsService(&pb.Users_ServiceDesc, NewUsersService),
	).Run(grpcserver.Serve())
}
```

- `grpcserver.Module` provides the `*grpc.Server`, with the registered services and interceptors, and its `*grpcserver.Address`.
- `grpcserver.Serve()` starts it with the application, and stops it gracefully when the application stops: in-flight calls are completed, unless the stop timeout expires first. If the server fails while running, the application stops.
- Every call is traced, measured and logged, see [Observability](observability.md).

## Services

Register the implementation of a service with its generated service description, `XXX_ServiceDesc`. The implementation is either a value, or a constructor whose parameters are injected by fx:

```go
type UsersService struct {
	pb.UnimplementedUsersServer

	repository *Repository
}

func NewUsersService(repository *Repository) *UsersService {
	return &UsersService{repository: repository}
}

func (s *UsersService) GetUser(ctx context.Context, req *pb.GetUserRequest) (*pb.User, error) {
	user, err := s.repository.Get(ctx, req.GetId())
	if errors.Is(err, ErrNotFound) {
		return nil, status.Error(codes.NotFound, "user not found")
	}
	...
}
```

```go
grpcserver.AsService(&pb.Users_ServiceDesc, NewUsersService)
```

The application fails to start if the implementation does not implement the service, or if a service is registered twice.

## Interceptors

Interceptors run code around every call: authentication, metadata, validation... Register them with `AsUnaryInterceptor` and `AsStreamInterceptor`, as values or constructors:

```go
func NewAuthInterceptor(auth *Authenticator) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if err := auth.Check(ctx); err != nil {
			return nil, status.Error(codes.Unauthenticated, err.Error())
		}

		return handler(ctx, req)
	}
}
```

```go
grpcserver.AsUnaryInterceptor(NewAuthInterceptor)
grpcserver.AsStreamInterceptor(NewAuthStreamInterceptor)
```

Interceptors apply to every service, in registration order: the first registered interceptor is the outermost one. They run after the built-in ones: the call log, then the panic recovery. A panic in a service or an interceptor is logged with its stack, and answered with an `Internal` error.

## Health

The server implements the [gRPC health checking protocol](https://github.com/grpc/grpc/blob/master/doc/health-checking.md), so load balancers and Kubernetes [gRPC probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#define-a-grpc-liveness-probe) can check it. The checks run the probes of the [health module](../core/health.md):

| Service name | Check |
|---|---|
| `""` (the server) | readiness |
| `liveness` | liveness |
| `readiness` | readiness |
| `startup` | startup |

Other service names answer `NotFound`. `Watch` is not implemented. As the readiness check fails once the application stops, so does the gRPC readiness check.

```shell
grpcurl -plaintext -d '{"service": "liveness"}' localhost:50051 grpc.health.v1.Health/Check
```

Disable it with `grpcserver.health.enabled: false`, for example to register your own health service.

## Reflection

The [server reflection](https://github.com/grpc/grpc/blob/master/doc/server-reflection.md) lets clients like [grpcurl](https://github.com/fullstorydev/grpcurl) list and call the services without their proto files. It is disabled by default:

```yaml
grpcserver:
  reflection:
    enabled: true
```

## Address

The `*grpcserver.Address` is `grpcserver.address`, then the listening address once the server is started, useful with port `0`:

```go
var address *grpcserver.Address

coretest.Start(t, app, config.AsOptions(config.WithValues(map[string]any{"grpcserver.address": "127.0.0.1:0"})),
	grpcserver.Serve(), fx.Populate(&address))

conn, err := grpc.NewClient(address.String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
```

## Configuration

The `grpcserver.*` keys set the address, the message sizes, the health service, the reflection, the call logs and the methods excluded from telemetry. See [Configuration](configuration.md).
