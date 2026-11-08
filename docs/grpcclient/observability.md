---
title: Observability
description: Traces, metrics and logs of the gRPC client calls, and how to exclude methods.
---

Every call goes through, from the outermost:

1. the [otelgrpc](https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc) instrumentation,
2. the call log,
3. the registered [interceptors](overview.md#interceptors).

## Traces

Each call gets a client span:

- named after the full method: `users.v1.Users/GetUser`,
- with the [RPC semantic conventions](https://opentelemetry.io/docs/specs/semconv/rpc/grpc/) attributes: `rpc.system.name`, `rpc.method` and `rpc.response.status_code`,
- with an error status for server errors: `Unknown`, `DeadlineExceeded`, `Unimplemented`, `Internal`, `Unavailable` and `DataLoss`.

The span is a child of the span of the call context. The trace context is injected in the call metadata with the application propagator, so the server span, of a [grpcserver](../grpcserver/overview.md) application for example, continues the trace.

## Metrics

| Metric | Description |
|---|---|
| `rpc.client.call.duration` | Duration of the calls, with the `rpc.method` and `rpc.response.status_code` attributes |

## Logs

Each call is logged as `grpc client request`, at error level for server errors (see above) and info level otherwise, with:

| Attribute | Example |
|---|---|
| `rpc` | `/users.v1.Users/GetUser` |
| `service` | `users.v1.Users` |
| `method` | `GetUser` |
| `target` | `users:50051`, the connection target |
| `code` | `NotFound` |
| `error` | `user not found`, the error message, if any |
| `duration` | `1.2ms` |

Streaming calls are logged once the stream is created, with the code of its creation. The log is correlated with the call span. Disable call logs with `grpcclient.log.enabled: false`.

## Exclude methods

To exclude methods from spans, metrics and call logs, list their full methods:

```yaml
grpcclient:
  observability:
    exclude:
      - /grpc.health.v1.Health/Check
      - /users.v1.Users/Ping
```
