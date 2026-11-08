---
title: gRPC client
description: The grpcclient module, gRPC client connections configured by name, with generated clients and interceptors registered through fx.
---

The `grpcclient` module provides [gRPC](https://grpc.io/docs/languages/go/) client connections, declared by name in the configuration and instrumented with OpenTelemetry.

```shell
go get github.com/go-oryn/oryn/grpcclient
```

## Quick start

```yaml
# configs/grpcclient.yaml
grpcclient:
  connections:
    users:
      target: users:50051
      insecure: true
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

type Service struct {
	users pb.UsersClient
}

func NewService(users pb.UsersClient) *Service {
	return &Service{users: users}
}
```

- `grpcclient.Module` provides the `*grpcclient.Factory`, which creates the connections declared under `grpcclient.connections.<name>`.
- `grpcclient.AsClient("users", pb.NewUsersClient)` provides a `pb.UsersClient` on the `users` connection.
- Every call is traced, measured and logged, and the trace context is propagated to the server, see [Observability](observability.md).

## Connections

A connection is declared by its name, with at least a `target`, in the [gRPC name syntax](https://github.com/grpc/grpc/blob/master/doc/naming.md): `users:50051`, `dns:///users.internal:443`... It uses TLS with the system root certificates, unless `insecure` is `true`. See [Configuration](configuration.md).

The `*grpcclient.Factory` creates each connection once, on first use, and shares it. Creating a connection does not connect: gRPC connects on the first call, and reconnects as needed. All the connections are closed when the application stops.

```go
conn, err := factory.Conn("users")
if err != nil {
	return err // grpcclient.connections.users.target is missing, or the connection settings are invalid
}

client := pb.NewUsersClient(conn)
```

## Clients

`AsClient` provides the client returned by a generated `pb.NewXxxClient` constructor, on a named connection. The application fails to start if the connection cannot be created.

As fx identifies values by type, a client type can be provided only once. To call the same service on two connections, build the clients from the factory, and name them:

```go
fx.Provide(
	fx.Annotate(func(f *grpcclient.Factory) (pb.UsersClient, error) {
		conn, err := f.Conn("users-eu")
		if err != nil {
			return nil, err
		}

		return pb.NewUsersClient(conn), nil
	}, fx.ResultTags(`name:"users-eu"`)),
)
```

## Interceptors

Interceptors run code around every call of every connection: authentication, metadata, retries... Register them with `AsUnaryInterceptor` and `AsStreamInterceptor`, as values or constructors whose parameters are injected by fx:

```go
func NewAuthInterceptor(tokens *TokenSource) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+tokens.Token())

		return invoker(ctx, method, req, reply, cc, opts...)
	}
}
```

```go
grpcclient.AsUnaryInterceptor(NewAuthInterceptor)
grpcclient.AsStreamInterceptor(NewAuthStreamInterceptor)
```

Interceptors apply in registration order: the first registered interceptor is the outermost one. They run after the built-in call log.

## Dial options

`AsDialOptions` adds [dial options](https://pkg.go.dev/google.golang.org/grpc#DialOption) to one connection, for what the configuration does not cover: custom TLS credentials, keepalive, a custom dialer...

```go
grpcclient.AsDialOptions("users",
	grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
	grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: 30 * time.Second}),
)
```

They are applied after the configured settings, so they override them.

## Configuration

The `grpcclient.*` keys declare the connections, the call logs and the methods excluded from telemetry. See [Configuration](configuration.md).
