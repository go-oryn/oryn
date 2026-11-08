---
title: Observability
description: Traces, metrics and logs of the MCP server, following the MCP semantic conventions, and how to exclude methods.
---

Every MCP request goes through, from the outermost:

1. the OpenTelemetry instrumentation, following the [MCP semantic conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/mcp/),
2. the request log,
3. the panic recovery,
4. the tool, resource or prompt handler.

With the streamable HTTP transport, the HTTP requests are also instrumented with [otelhttp](https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp): a `POST /mcp` server span, and the `http.server.*` metrics.

## Traces

Each MCP request gets a server span, child of the `POST /mcp` HTTP span:

- named `{mcp.method.name} {target}`: `tools/call greet`, `resources/read docs://readme`, `prompts/get review`, or just the method without target: `tools/list`,
- with the attributes:

| Attribute | Example | When |
|---|---|---|
| `mcp.method.name` | `tools/call` | always |
| `mcp.session.id` | `4XZ…` | with a session |
| `gen_ai.operation.name` | `execute_tool` | tool calls |
| `gen_ai.tool.name` | `greet` | tool calls |
| `gen_ai.prompt.name` | `review` | `prompts/get` |
| `mcp.resource.uri` | `docs://readme` | `resources/read` |
| `error.type` | `tool_error`, `-32602`, `_OTHER` | failures |

- with an error status when the request fails: `error.type` is the JSON-RPC error code for protocol errors (unknown tool, invalid params...), `_OTHER` for other errors (a panic...), and `tool_error` for a tool returning an error result.

When the client sends a trace context in the request `_meta` (`traceparent`, `tracestate`), as advised by the conventions, the span continues the trace of the client instead. Spans created in handlers with `ctx` are children of the MCP span.

## Metrics

| Metric | Description | Attributes |
|---|---|---|
| `mcp.server.operation.duration` | Duration of the MCP requests, in seconds | `mcp.method.name`, `gen_ai.tool.name`, `gen_ai.prompt.name`, `error.type` |

The instrumentation scope of the spans and metrics is `github.com/go-oryn/oryn/mcpserver`.

## Logs

Each request is logged as `mcp request`, at error level for failures and tool errors, and info level otherwise, with:

| Attribute | Example |
|---|---|
| `method` | `tools/call` |
| `target` | `greet` |
| `session_id` | `4XZ…` |
| `duration` | `1.2ms` |
| `error_type` | `tool_error` (failures only) |
| `error` | `missing name` (failures only) |

The log is correlated with the request span. Disable request logs with `mcpserver.log.enabled: false`. The logs of the MCP SDK itself (sessions connecting...) are logged at debug level.

## Exclude methods

Noisy methods can be excluded from spans, metrics and request logs:

```yaml
mcpserver:
  observability:
    exclude:
      - ping
      - notifications/initialized
```
