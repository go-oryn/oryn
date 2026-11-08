---
title: Observability
description: Traces, metrics and logs of the MCP client, following the MCP semantic conventions, and trace propagation to the servers.
---

Every MCP request goes through, from the outermost:

1. the timeout, if configured,
2. the OpenTelemetry instrumentation, following the [MCP semantic conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/mcp/),
3. the request log,
4. the transport.

With the streamable HTTP transport, the HTTP requests are also instrumented with [otelhttp](https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp): an HTTP client span per request, and the `http.client.*` metrics.

## Traces

Each MCP request gets a client span, child of the span of `ctx`:

- named `{mcp.method.name} {target}`: `tools/call search`, `resources/read docs://readme`, `prompts/get review`, or just the method without target: `tools/list`,
- with the attributes:

| Attribute | Example | When |
|---|---|---|
| `mcp.method.name` | `tools/call` | always |
| `mcp.session.id` | `4XZ…` | with a session |
| `gen_ai.operation.name` | `execute_tool` | tool calls |
| `gen_ai.tool.name` | `search` | tool calls |
| `gen_ai.prompt.name` | `review` | `prompts/get` |
| `mcp.resource.uri` | `docs://readme` | `resources/read` |
| `server.address` | `docs.example.com` | streamable HTTP |
| `server.port` | `443` | streamable HTTP |
| `error.type` | `tool_error`, `-32602`, `_OTHER` | failures |

- with an error status when the request fails: `error.type` is the JSON-RPC error code for protocol errors (unknown tool, invalid params...), `_OTHER` for other errors (timeout, connection...), and `tool_error` for a tool returning an error result.

## Propagation

The trace continues on the server, in two ways:

- the trace context (`traceparent`, `tracestate`) is put in the request `_meta`, as advised by the conventions: the MCP server span of an [mcpserver](../mcpserver/observability.md) is a child of the client span, whatever the transport,
- with streamable HTTP, the HTTP requests carry the trace context headers too.

The application propagator is used, so `OTEL_PROPAGATORS` applies.

## Metrics

| Metric | Description | Attributes |
|---|---|---|
| `mcp.client.operation.duration` | Duration of the MCP requests, in seconds | `mcp.method.name`, `gen_ai.tool.name`, `gen_ai.prompt.name`, `error.type`, `server.address`, `server.port` |

The instrumentation scope of the spans and metrics is `github.com/go-oryn/oryn/mcpclient`.

## Logs

Each request is logged as `mcp client request`, at error level for failures and tool errors, and info level otherwise, with:

| Attribute | Example |
|---|---|
| `server` | `docs`, the server name |
| `method` | `tools/call` |
| `target` | `search` |
| `session_id` | `4XZ…` |
| `duration` | `1.2ms` |
| `error_type` | `tool_error` (failures only) |
| `error` | `missing query` (failures only) |

The log is correlated with the request span. Disable request logs with `mcpclient.log.enabled: false`. The logs of the MCP SDK itself are logged at debug level.

## Exclude methods

Noisy methods can be excluded from spans, metrics and request logs:

```yaml
mcpclient:
  observability:
    exclude:
      - ping
      - notifications/initialized
```
