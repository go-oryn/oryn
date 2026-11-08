---
title: Configuration
description: Every mcpclient configuration key, with its default and when to change it.
---

The `mcpclient.*` keys configure the MCP servers to connect to, and the client telemetry.

## Reference

```yaml
mcpclient:
  servers:
    <name>:                   # the server name, given to factory.Session
      url: ""                 # streamable HTTP endpoint, required
      headers: {}             # HTTP headers sent with every request
      timeout: 0s             # max duration of a request, 0 = no timeout
  log:
    enabled: true             # log every request
  observability:
    exclude: []               # MCP methods excluded from spans, metrics and logs
```

| Key | Type | Default | Environment variable |
|---|---|---|---|
| `mcpclient.servers.<name>.url` | string | required | `APP_MCPCLIENT_SERVERS_<NAME>_URL` |
| `mcpclient.servers.<name>.headers` | map | `{}` | |
| `mcpclient.servers.<name>.timeout` | duration | `0s` | `APP_MCPCLIENT_SERVERS_<NAME>_TIMEOUT` |
| `mcpclient.log.enabled` | bool | `true` | `APP_MCPCLIENT_LOG_ENABLED` |
| `mcpclient.observability.exclude` | list | `[]` | |

The client name and version sent to the servers are [`app.name` and `app.version`](../core/configuration.md#keys).

## Servers

Each entry of `mcpclient.servers` is a server, opened with `factory.Session(ctx, "<name>")`. Its `url` is the [streamable HTTP](https://modelcontextprotocol.io/specification/latest/basic/transports#streamable-http) endpoint of the server:

```yaml
mcpclient:
  servers:
    docs:
      url: https://docs.example.com/mcp
    tasks:
      url: http://tasks:8082/mcp
```

`Session` fails for a name without `url`, unless a [custom transport](overview.md#custom-transports) is registered for it, and for an invalid URL. Names are case-insensitive.

## Headers

Headers are sent with every HTTP request to the server, typically for authentication. Use `${VAR}` placeholders to keep secrets out of the files:

```yaml
mcpclient:
  servers:
    docs:
      url: https://docs.example.com/mcp
      headers:
        Authorization: Bearer ${DOCS_TOKEN}
        X-Tenant: ${TENANT:default}
```

Header names are case-insensitive, like in HTTP.

## Timeout

By default, a request lasts as long as its context. Set `timeout` to bound every request to the server, from the call to the response:

```yaml
mcpclient:
  servers:
    docs:
      url: https://docs.example.com/mcp
      timeout: 10s
```

A request over the timeout fails with `context.DeadlineExceeded`. The timeout applies to each request, not to the session, which stays open.

## Request logs

A log is written for every MCP request, `mcp client request`, see [Observability](observability.md#logs). Disable it when the traces are enough:

```yaml
mcpclient:
  log:
    enabled: false
```

## Exclude methods

Exclude MCP methods from spans, metrics and request logs, for example the keep-alive pings and the discovery calls:

```yaml
mcpclient:
  observability:
    exclude:
      - ping
      - tools/list
```

Methods are the MCP method names: `server/discover`, `tools/list`, `tools/call`, `resources/read`, `prompts/get`...

See [Configuration](../core/configuration.md) for the loading rules.
