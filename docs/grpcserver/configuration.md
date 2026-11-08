---
title: Configuration
description: Every grpcserver configuration key, with its default and when to change it.
---

The `grpcserver.*` keys configure the gRPC server. All of them are optional.

## Reference

```yaml
grpcserver:
  address: ":50051"           # listen address
  max_recv_msg_size: 0        # max received message size in bytes, 0 = gRPC default (4 MiB)
  max_send_msg_size: 0        # max sent message size in bytes, 0 = gRPC default (no limit)
  health:
    enabled: true             # register the grpc.health.v1 Health service
  reflection:
    enabled: false            # register the server reflection
  log:
    enabled: true             # log every call
  observability:
    exclude:                  # full methods excluded from spans, metrics and logs
      - /grpc.health.v1.Health/Check
      - /grpc.health.v1.Health/Watch
      - /grpc.health.v1.Health/List
      - /grpc.reflection.v1.ServerReflection/ServerReflectionInfo
      - /grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo
```

| Key | Type | Default | Environment variable |
|---|---|---|---|
| `grpcserver.address` | string | `:50051` | `APP_GRPCSERVER_ADDRESS` |
| `grpcserver.max_recv_msg_size` | int | `0` (4 MiB) | `APP_GRPCSERVER_MAX_RECV_MSG_SIZE` |
| `grpcserver.max_send_msg_size` | int | `0` (no limit) | `APP_GRPCSERVER_MAX_SEND_MSG_SIZE` |
| `grpcserver.health.enabled` | bool | `true` | `APP_GRPCSERVER_HEALTH_ENABLED` |
| `grpcserver.reflection.enabled` | bool | `false` | `APP_GRPCSERVER_REFLECTION_ENABLED` |
| `grpcserver.log.enabled` | bool | `true` | `APP_GRPCSERVER_LOG_ENABLED` |
| `grpcserver.observability.exclude` | list | health and reflection methods | |

## Address

```yaml
grpcserver:
  address: ":50051"           # all interfaces
  # address: "127.0.0.1:0"    # a random free port, for tests
```

Once `grpcserver.Serve()` has started the server, the provided `*grpcserver.Address` holds the real listening address.

## Message sizes

gRPC rejects received messages larger than 4 MiB with `RESOURCE_EXHAUSTED`. Raise the limit for services exchanging large messages:

```yaml
grpcserver:
  max_recv_msg_size: 16777216   # 16 MiB
```

## Health

The [gRPC health service](https://github.com/grpc/grpc/blob/master/doc/health-checking.md) is registered by default, and answers with the application [health checks](../core/health.md): the `""` and `readiness` services run the readiness probes, `liveness` and `startup` the others. Kubernetes can use it directly:

```yaml
# Kubernetes container spec
readinessProbe:
  grpc:
    port: 50051
```

Disable it if the application registers its own `grpc.health.v1.Health` service:

```yaml
grpcserver:
  health:
    enabled: false
```

## Reflection

[Server reflection](https://github.com/grpc/grpc/blob/master/doc/server-reflection.md) lets tools like [grpcurl](https://github.com/fullstorydev/grpcurl) list and call the services without the proto files. It exposes the API description: enable it in development, or for internal services.

```yaml
# configs/dev/grpcserver.yaml (APP_ENV=dev)
grpcserver:
  reflection:
    enabled: true
```

```shell
grpcurl -plaintext localhost:50051 list
```

## Call logs

A log is written for every call, `grpc request`, see [Observability](observability.md#logs). Disable it when the traces are enough:

```yaml
grpcserver:
  log:
    enabled: false
```

## Exclude methods

The health checks and the reflection are excluded from spans, metrics and call logs by default. The list replaces the default one, so keep its entries when adding yours:

```yaml
grpcserver:
  observability:
    exclude:
      - /grpc.health.v1.Health/Check
      - /grpc.health.v1.Health/Watch
      - /grpc.health.v1.Health/List
      - /grpc.reflection.v1.ServerReflection/ServerReflectionInfo
      - /grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo
      - /users.v1.Users/Ping
```

Methods are full gRPC method names: `/<package>.<Service>/<Method>`. An empty list keeps the default.

See [Configuration](../core/configuration.md) for the loading rules.
