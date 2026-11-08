---
title: Configuration
description: Every httpclient configuration key, with its default and when to change it.
---

The `httpclient.*` keys configure the clients of the factory. All of them are optional.

## Reference

```yaml
httpclient:
  timeout: 30s                    # max duration of a request, response body included, 0 = none
  transport:
    max_idle_conns: 100           # max idle connections, all hosts, 0 = unlimited
    max_idle_conns_per_host: 100  # max idle connections per host
    max_conns_per_host: 0         # max connections per host, 0 = unlimited
    idle_conn_timeout: 90s        # max duration of an idle connection, 0 = none
    tls_handshake_timeout: 10s    # max duration of the TLS handshake, 0 = none
    response_header_timeout: 0s   # max duration to receive the response headers, 0 = none
    expect_continue_timeout: 1s   # max duration to wait for a 100-continue, 0 = send the body at once
  log:
    enabled: true                 # log every request
  observability:
    exclude: []                   # URL regular expressions excluded from spans, metrics and logs
```

| Key | Type | Default | Environment variable |
|---|---|---|---|
| `httpclient.timeout` | duration | `30s` | `APP_HTTPCLIENT_TIMEOUT` |
| `httpclient.transport.max_idle_conns` | int | `100` | `APP_HTTPCLIENT_TRANSPORT_MAX_IDLE_CONNS` |
| `httpclient.transport.max_idle_conns_per_host` | int | `100` | `APP_HTTPCLIENT_TRANSPORT_MAX_IDLE_CONNS_PER_HOST` |
| `httpclient.transport.max_conns_per_host` | int | `0` (unlimited) | `APP_HTTPCLIENT_TRANSPORT_MAX_CONNS_PER_HOST` |
| `httpclient.transport.idle_conn_timeout` | duration | `90s` | `APP_HTTPCLIENT_TRANSPORT_IDLE_CONN_TIMEOUT` |
| `httpclient.transport.tls_handshake_timeout` | duration | `10s` | `APP_HTTPCLIENT_TRANSPORT_TLS_HANDSHAKE_TIMEOUT` |
| `httpclient.transport.response_header_timeout` | duration | `0s` (none) | `APP_HTTPCLIENT_TRANSPORT_RESPONSE_HEADER_TIMEOUT` |
| `httpclient.transport.expect_continue_timeout` | duration | `1s` | `APP_HTTPCLIENT_TRANSPORT_EXPECT_CONTINUE_TIMEOUT` |
| `httpclient.log.enabled` | bool | `true` | `APP_HTTPCLIENT_LOG_ENABLED` |
| `httpclient.observability.exclude` | list | `[]` | |

Durations use the Go syntax: `500ms`, `30s`, `2m`. An explicit `0` is kept: `timeout: 0s` disables the timeout, it does not restore the default. The transport keeps the other settings of `http.DefaultTransport`, like the `HTTP_PROXY` and `HTTPS_PROXY` environment variables.

## Timeout

`httpclient.timeout` bounds the whole request: connection, redirects, and reading the response body. It applies to every client of the factory, unless built with `httpclient.WithTimeout`. For a per-request deadline, use the request context instead:

```go
ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
defer cancel()

request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
```

Disable it for streaming responses or long downloads, and bound the waiting for the server with `response_header_timeout`:

```yaml
httpclient:
  timeout: 0s
  transport:
    response_header_timeout: 10s
```

## Connection pool

The clients of the factory share the transport, and so its connections. The default keeps up to 100 idle connections per host, instead of 2 for `http.DefaultTransport`, so an application calling the same API concurrently reuses its connections. To protect a fragile API, limit the connections to it:

```yaml
httpclient:
  transport:
    max_conns_per_host: 20
```

Requests over the limit wait for a free connection.

## Request logs

A log is written for every request, `http client request`, see [Observability](observability.md#logs). Disable it when the traces are enough, or to reduce the volume:

```yaml
httpclient:
  log:
    enabled: false
```

## Exclude URLs

Exclude the calls to some URLs, like the polling of a status endpoint, with Go [regular expressions](https://pkg.go.dev/regexp/syntax), matched against the request URL without its query: `https://status.example.com/health`.

```yaml
httpclient:
  observability:
    exclude:
      - "^https://status\\.example\\.com/"
      - "/health$"
```

Excluded requests are still sent: they only get no span, no metrics and no request log. The trace context is not sent with them. An invalid expression fails the application start.

## Per environment

```yaml
# configs/app.yaml
httpclient:
  timeout: 10s

# configs/dev/httpclient.yaml (APP_ENV=dev)
httpclient:
  timeout: 0s
```

See [Configuration](../core/configuration.md) for the loading rules.
