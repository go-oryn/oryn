---
title: Observability
description: Traces, metrics and logs of the gRPC server, and how to exclude methods.
---

Every call goes through, from the outermost:

1. the [otelgrpc](https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc) instrumentation,
2. the call log,
3. the panic recovery,
4. the registered [interceptors](overview.md#interceptors) and the service.

## Traces

Each call gets a server span:

- named after the full method: `users.v1.Users/GetUser`,
- with the [RPC semantic conventions](https://opentelemetry.io/docs/specs/semconv/rpc/grpc/) attributes: `rpc.system.name`, `rpc.method` and `rpc.response.status_code`,
- with an error status for server errors: `Unknown`, `DeadlineExceeded`, `Unimplemented`, `Internal`, `Unavailable` and `DataLoss`.

The incoming trace context is extracted from the call metadata with the application propagator, so the span continues the trace of the caller. Spans created in services with the call context are children of the call span.

## Metrics

| Metric | Description |
|---|---|
| `rpc.server.call.duration` | Duration of the calls, with the `rpc.method` and `rpc.response.status_code` attributes |

## Logs

Each call is logged as `grpc request`, at error level for server errors (see above) and info level otherwise, with:

| Attribute | Example |
|---|---|
| `rpc` | `/users.v1.Users/GetUser` |
| `service` | `users.v1.Users` |
| `method` | `GetUser` |
| `code` | `NotFound` |
| `error` | `user not found`, the error message, if any |
| `duration` | `1.2ms` |

The log is correlated with the call span. Disable call logs with `grpcserver.log.enabled: false`.

## Panics

A panic in a service or an interceptor is logged as `grpc handler panic` at error level, with the `rpc`, `panic` and `stack` attributes, and answered with an `Internal` error. The span and the call log record the `Internal` code.

## Exclude methods

The health checks and the server reflection are excluded from spans, metrics and call logs by default. To exclude other methods, list their full methods; the list replaces the default one:

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
