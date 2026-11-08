---
title: Testing
description: Test MCP tools, resources and prompts in memory, with a real MCP client, and assert their telemetry.
---

Tests run the application with [coretest](../core/testing.md), and connect an MCP client to the `*mcp.Server` with `mcpservertest.Connect`, through in-memory transports, without network:

```go
func TestGreet(t *testing.T) {
	t.Parallel()

	var (
		server *mcp.Server
		spans  *tracetest.InMemoryExporter
		logs   *logtest.InMemoryExporter
	)

	coretest.Start(t, core.New(mcpserver.Module, mcpserver.AsCapability(NewGreetTool)), fx.Populate(&server, &spans, &logs))

	session := mcpservertest.Connect(t, server)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "greet",
		Arguments: map[string]any{"name": "oryn"},
	})
	require.NoError(t, err)
	assert.False(t, result.IsError)

	spantest.HasSpan(t, spans,
		spantest.WithName("tools/call greet"),
		spantest.WithAttributeValue(mcpserver.GenAIToolNameKey.String("greet")),
	)
	logtest.HasRecord(t, logs, logtest.WithBody("mcp request"), logtest.WithAttributeValue("target", "greet"))
}
```

The request goes through the whole server: instrumentation, logs, recovery and handler. The sessions are closed when the test ends.

## Over the network

To test the streamable HTTP transport, set `mcpserver.address` to `127.0.0.1:0` and add `mcpserver.Serve()`: once started, the `*mcpserver.HTTPServer` Addr is the listening address.

```go
var server *mcpserver.HTTPServer

coretest.Start(t, app,
	config.AsOptions(config.WithValues(map[string]any{"mcpserver.address": "127.0.0.1:0"})),
	mcpserver.Serve(),
	fx.Populate(&server),
)

client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "test"}, nil)
session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: "http://" + server.Addr + "/mcp"}, nil)
```
