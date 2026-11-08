---
title: Testing
description: Test MCP clients against in-memory MCP servers, without network, and assert their telemetry.
---

Tests run the application with [coretest](../core/testing.md), and serve the MCP servers in memory with `mcpclienttest`, without network. The client is the real one: instrumentation, logs and timeout included.

## Serve a server

`mcpclienttest.Serve(name, server)` makes the server `name` resolve to an in-memory `*mcp.Server`, built with the [MCP Go SDK](https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp):

```go
func TestSearch(t *testing.T) {
	t.Parallel()

	server := mcp.NewServer(&mcp.Implementation{Name: "docs", Version: "test"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "search"},
		func(ctx context.Context, req *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "test_result"}}}, nil, nil
		})

	var (
		search *Search
		spans  *tracetest.InMemoryExporter
	)

	coretest.Start(t, core.New(mcpclient.Module, fx.Provide(NewSearch)),
		mcpclienttest.Serve("docs", server),
		fx.Populate(&search, &spans),
	)

	result, err := search.Search(t.Context(), "oryn")
	require.NoError(t, err)
	assert.Equal(t, "test_result", result.Content[0].(*mcp.TextContent).Text)

	spantest.HasSpan(t, spans,
		spantest.WithName("tools/call search"),
		spantest.WithKind(trace.SpanKindClient),
		spantest.WithAttributeValue(mcpclient.GenAIToolNameKey.String("search")),
	)
}
```

No `mcpclient.servers.docs.url` is needed: the in-memory server wins over the configuration. Its sessions are closed when the application stops.

## Serve the application server

`mcpclienttest.ServeContainer(name)` serves the `*mcp.Server` of the application, provided by [mcpserver](../mcpserver/overview.md), to test an application calling itself, or the client and server instrumentation together:

```go
coretest.Start(t, core.New(mcpclient.Module, mcpserver.Module, mcpserver.AsCapability(NewSearchTool)),
	mcpclienttest.ServeContainer("self"),
	fx.Populate(&factory, &spans),
)

session, err := factory.Session(t.Context(), "self")
require.NoError(t, err)

_, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: "search", Arguments: map[string]any{"query": "oryn"}})
require.NoError(t, err)

client := spantest.Find(spans, spantest.WithName("tools/call search"), spantest.WithKind(trace.SpanKindClient))
spantest.HasSpan(t, spans,
	spantest.WithName("tools/call search"),
	spantest.WithKind(trace.SpanKindServer),
	spantest.WithParent(client[0].SpanContext.SpanID()),
)
```

## Over the network

To test the streamable HTTP transport, start an MCP server on `127.0.0.1:0` with [mcpserver](../mcpserver/testing.md#over-the-network), and point the server URL to it:

```go
var server *mcpserver.HTTPServer

coretest.Start(t, serverApp,
	config.AsOptions(config.WithValues(map[string]any{"mcpserver.address": "127.0.0.1:0"})),
	mcpserver.Serve(),
	fx.Populate(&server),
)

coretest.Start(t, clientApp,
	config.AsOptions(config.WithValues(map[string]any{"mcpclient.servers.docs.url": "http://" + server.Addr + "/mcp"})),
	fx.Populate(&factory),
)
```
