---
title: Configuration
description: Every mcpserver configuration key, with its default and when to change it.
---

The `mcpserver.*` keys configure the MCP server and its streamable HTTP transport. All of them are optional.

## Reference

```yaml
mcpserver:
  address: ":8082"            # listen address
  path: /mcp                  # path of the MCP endpoint
  stateless: false            # no sessions: every request is independent
  json_response: false        # answer with application/json instead of SSE streams
  session_timeout: 30m        # idle sessions are closed after this duration, 0 = never
  read_header_timeout: 10s    # max duration to read the request headers
  log:
    enabled: true             # log every request
  observability:
    exclude: []               # MCP methods excluded from spans, metrics and logs
```

| Key | Type | Default | Environment variable |
|---|---|---|---|
| `mcpserver.address` | string | `:8082` | `APP_MCPSERVER_ADDRESS` |
| `mcpserver.path` | string | `/mcp` | `APP_MCPSERVER_PATH` |
| `mcpserver.stateless` | bool | `false` | `APP_MCPSERVER_STATELESS` |
| `mcpserver.json_response` | bool | `false` | `APP_MCPSERVER_JSON_RESPONSE` |
| `mcpserver.session_timeout` | duration | `30m` | `APP_MCPSERVER_SESSION_TIMEOUT` |
| `mcpserver.read_header_timeout` | duration | `10s` | `APP_MCPSERVER_READ_HEADER_TIMEOUT` |
| `mcpserver.log.enabled` | bool | `true` | `APP_MCPSERVER_LOG_ENABLED` |
| `mcpserver.observability.exclude` | list | `[]` | |

The server name and version sent to the clients are [`app.name` and `app.version`](../core/configuration.md#keys).

## Address and path

The MCP endpoint is `http://<address><path>`, `http://localhost:8082/mcp` by default:

```yaml
mcpserver:
  address: ":9000"
  path: /agents/mcp
```

```shell
claude mcp add --transport http my-app http://localhost:9000/agents/mcp
```

Once `mcpserver.Serve()` has started the server, the `*mcpserver.HTTPServer` Addr holds the real listening address, which is useful with port `0`.

## Sessions

By default, the server is stateful: a client opens a session (`Mcp-Session-Id` header), and the server keeps it until the client closes it, or until it has been idle for `session_timeout`. With `session_timeout: 0s`, idle sessions are never closed.

Stateful sessions are kept in memory: behind a load balancer, a client must reach the same instance. For several instances without sticky sessions, run stateless:

```yaml
mcpserver:
  stateless: true
```

In stateless mode, every request is independent: the server cannot send requests or notifications to the client, which is fine for tools, resources and prompts.

## Responses

By default, responses are sent as [server-sent event](https://html.spec.whatwg.org/multipage/server-sent-events.html) streams, which allow progress notifications. Some proxies buffer streams: answer with plain JSON instead:

```yaml
mcpserver:
  json_response: true
```

## Request logs

A log is written for every MCP request, `mcp request`, see [Observability](observability.md#logs). Disable it when the traces are enough:

```yaml
mcpserver:
  log:
    enabled: false
```

## Exclude methods

Exclude MCP methods from spans, metrics and request logs, for example the keep-alive pings and the discovery calls:

```yaml
mcpserver:
  observability:
    exclude:
      - ping
      - tools/list
```

Methods are the MCP method names: `initialize`, `tools/list`, `tools/call`, `resources/read`, `prompts/get`...

See [Configuration](../core/configuration.md) for the loading rules.
