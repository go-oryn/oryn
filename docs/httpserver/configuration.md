---
title: Configuration
description: Every httpserver configuration key, with its default and when to change it.
---

The `httpserver.*` keys configure the HTTP server. All of them are optional.

## Reference

```yaml
httpserver:
  address: ":8080"            # listen address
  read_header_timeout: 10s    # max duration to read the request headers
  read_timeout: 0s            # max duration to read the whole request, 0 = none
  write_timeout: 0s           # max duration to write the response, 0 = none
  idle_timeout: 2m            # max duration of an idle keep-alive connection
  log:
    enabled: true             # log every request
  observability:
    exclude: []               # path regular expressions excluded from spans, metrics and logs
```

| Key | Type | Default | Environment variable |
|---|---|---|---|
| `httpserver.address` | string | `:8080` | `APP_HTTPSERVER_ADDRESS` |
| `httpserver.read_header_timeout` | duration | `10s` | `APP_HTTPSERVER_READ_HEADER_TIMEOUT` |
| `httpserver.read_timeout` | duration | `0s` (none) | `APP_HTTPSERVER_READ_TIMEOUT` |
| `httpserver.write_timeout` | duration | `0s` (none) | `APP_HTTPSERVER_WRITE_TIMEOUT` |
| `httpserver.idle_timeout` | duration | `2m` | `APP_HTTPSERVER_IDLE_TIMEOUT` |
| `httpserver.log.enabled` | bool | `true` | `APP_HTTPSERVER_LOG_ENABLED` |
| `httpserver.observability.exclude` | list | `[]` | |

Durations use the Go syntax: `500ms`, `30s`, `2m`. For the keys with a default, `0s` means the default.

## Address

```yaml
httpserver:
  address: ":8080"          # all interfaces
  # address: "127.0.0.1:8080"  # local only
  # address: "127.0.0.1:0"     # a random free port, for tests
```

Once `httpserver.Serve()` has started the server, `server.Addr` holds the real listening address, which is useful with port `0`.

## Timeouts

The defaults protect against slow clients sending headers (`read_header_timeout`), and close idle keep-alive connections (`idle_timeout`). `read_timeout` and `write_timeout` are disabled, so long uploads, downloads and streaming responses work.

For an API with small requests and responses, set them to bound every request:

```yaml
httpserver:
  read_timeout: 30s
  write_timeout: 30s
```

`write_timeout` includes the handler time: a handler slower than it gets its connection closed.

## Request logs

A log is written for every request, `http request`, see [Observability](observability.md#logs). Disable it when the traces are enough, or to reduce the volume:

```yaml
httpserver:
  log:
    enabled: false
```

## Exclude paths

Health checks and metric scrapes produce a lot of uninteresting telemetry. Exclude their paths with Go [regular expressions](https://pkg.go.dev/regexp/syntax), matched against the request path:

```yaml
httpserver:
  observability:
    exclude:
      - "^/livez$"
      - "^/readyz$"
      - "^/static/"
```

Excluded requests are still served: they only get no span, no metrics and no request log. An invalid expression fails the application start.

## Per environment

```yaml
# configs/app.yaml
httpserver:
  address: ":8080"

# configs/dev/httpserver.yaml (APP_ENV=dev)
httpserver:
  address: "127.0.0.1:8080"
  log:
    enabled: true
```

See [Configuration](../core/configuration.md) for the loading rules.
