package mcpclienttest_test

import (
	"context"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/core/coretest"
	"github.com/go-oryn/oryn/core/otel/oteltest/spantest"
	"github.com/go-oryn/oryn/mcpclient"
	"github.com/go-oryn/oryn/mcpclient/mcpclienttest"
	"github.com/go-oryn/oryn/mcpserver"
)

// addTools adds the test_tool tool to the server.
func addTools(server *mcp.Server) error {
	mcp.AddTool(server, &mcp.Tool{Name: "test_tool"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "test_value"}}}, nil, nil
	})

	return nil
}

// callTool calls test_tool on the server name, and returns the text result.
func callTool(t *testing.T, factory *mcpclient.Factory, name string) string {
	t.Helper()

	session, err := factory.Session(t.Context(), name)
	require.NoError(t, err)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "test_tool"})
	require.NoError(t, err)

	return result.Content[0].(*mcp.TextContent).Text
}

func TestServe(t *testing.T) {
	t.Parallel()

	server := mcp.NewServer(&mcp.Implementation{Name: "test_server", Version: "test"}, nil)
	require.NoError(t, addTools(server))

	var (
		factory *mcpclient.Factory
		spans   *tracetest.InMemoryExporter
	)

	app := coretest.New(t, core.New(mcpclient.Module), mcpclienttest.Serve("test", server), fx.Populate(&factory, &spans))
	app.RequireStart()

	assert.Equal(t, "test_value", callTool(t, factory, "test"))
	spantest.HasSpan(t, spans, spantest.WithName("tools/call test_tool"), spantest.WithKind(trace.SpanKindClient))

	app.RequireStop()
	assert.Empty(t, slices.Collect(server.Sessions()), "the server sessions are closed")
}

func TestServeContainer(t *testing.T) {
	t.Parallel()

	var (
		factory *mcpclient.Factory
		spans   *tracetest.InMemoryExporter
	)

	coretest.Start(t, core.New(mcpclient.Module, mcpserver.Module, mcpserver.AsCapability(addTools)),
		mcpclienttest.ServeContainer("self"), fx.Populate(&factory, &spans))

	assert.Equal(t, "test_value", callTool(t, factory, "self"))

	client := spantest.Find(spans, spantest.WithName("tools/call test_tool"), spantest.WithKind(trace.SpanKindClient))
	require.Len(t, client, 1)
	spantest.HasSpan(t, spans,
		spantest.WithName("tools/call test_tool"),
		spantest.WithKind(trace.SpanKindServer),
		spantest.WithParent(client[0].SpanContext.SpanID()),
	)
}
