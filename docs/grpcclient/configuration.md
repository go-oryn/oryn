---
title: Configuration
description: Every grpcclient configuration key, with its default and when to change it.
---

The `grpcclient.*` keys configure the gRPC client connections. Only the `target` of the used connections is required.

## Reference

```yaml
grpcclient:
  connections:
    <name>:
      target: "users:50051"   # required, gRPC target
      insecure: false         # plaintext instead of TLS with the system root certificates
      authority: ""           # :authority header, and TLS server name, "" = from the target
      service_config: ""      # default service config, as JSON
  log:
    enabled: true             # log every call
  observability:
    exclude: []               # full methods excluded from spans, metrics and logs
```

| Key | Type | Default | Environment variable |
|---|---|---|---|
| `grpcclient.connections.<name>.target` | string | required | `APP_GRPCCLIENT_CONNECTIONS_<NAME>_TARGET` |
| `grpcclient.connections.<name>.insecure` | bool | `false` | `APP_GRPCCLIENT_CONNECTIONS_<NAME>_INSECURE` |
| `grpcclient.connections.<name>.authority` | string | `""` (from the target) | `APP_GRPCCLIENT_CONNECTIONS_<NAME>_AUTHORITY` |
| `grpcclient.connections.<name>.service_config` | string | `""` | `APP_GRPCCLIENT_CONNECTIONS_<NAME>_SERVICE_CONFIG` |
| `grpcclient.log.enabled` | bool | `true` | `APP_GRPCCLIENT_LOG_ENABLED` |
| `grpcclient.observability.exclude` | list | empty | |

## Target

The target uses the [gRPC name syntax](https://github.com/grpc/grpc/blob/master/doc/naming.md). Without a scheme, it is resolved with DNS:

```yaml
grpcclient:
  connections:
    users:
      target: users:50051                       # DNS
    orders:
      target: dns:///orders.internal:443        # DNS, explicit
    local:
      target: unix:///var/run/local.sock        # Unix socket
```

A missing target fails the creation of the connection, and so the application start when the connection is used by `AsClient`.

## Transport security

Connections use TLS with the system root certificates by default. Use plaintext for servers without TLS, such as services in the same cluster behind a mesh, or in development:

```yaml
# configs/dev/grpcclient.yaml (APP_ENV=dev)
grpcclient:
  connections:
    users:
      insecure: true
```

For client certificates or custom root certificates, set the credentials with [`AsDialOptions`](overview.md#dial-options).

## Authority

The `:authority` header, and the TLS server name, default to the target host. Override them when calling through a proxy, or when the certificate does not match the target:

```yaml
grpcclient:
  connections:
    users:
      target: 10.0.0.12:443
      authority: users.example.com
```

## Service config

The [service config](https://github.com/grpc/grpc/blob/master/doc/service_config.md) sets the load balancing, the retries and the timeouts of a connection, as JSON. It is used unless the name resolver provides one:

```yaml
grpcclient:
  connections:
    users:
      target: dns:///users.internal:50051
      service_config: |
        {
          "loadBalancingConfig": [{"round_robin": {}}],
          "methodConfig": [{
            "name": [{"service": "users.v1.Users"}],
            "timeout": "2s",
            "retryPolicy": {
              "maxAttempts": 3,
              "initialBackoff": "0.1s",
              "maxBackoff": "1s",
              "backoffMultiplier": 2,
              "retryableStatusCodes": ["UNAVAILABLE"]
            }
          }]
        }
```

An invalid service config fails the creation of the connection.

## Call logs

A log is written for every call, `grpc client request`, see [Observability](observability.md#logs). Disable it when the traces are enough:

```yaml
grpcclient:
  log:
    enabled: false
```

## Exclude methods

No method is excluded by default. Excluded methods get no span, no metric and no call log:

```yaml
grpcclient:
  observability:
    exclude:
      - /grpc.health.v1.Health/Check
      - /users.v1.Users/Ping
```

Methods are full gRPC method names: `/<package>.<Service>/<Method>`. The trace context of the caller is still propagated for excluded methods.

See [Configuration](../core/configuration.md) for the loading rules.
