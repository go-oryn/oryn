package mcpservertest_test

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-oryn/oryn/mcpserver/mcpservertest"
)

func TestConnect(t *testing.T) {
	t.Parallel()

	server := mcp.NewServer(&mcp.Implementation{Name: "test_server", Version: "test"}, nil)
	server.AddTool(&mcp.Tool{Name: "test_tool", InputSchema: map[string]any{"type": "object"}},
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "test_value"}}}, nil
		})

	session := mcpservertest.Connect(t, server)

	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "test_tool"})
	require.NoError(t, err)
	assert.Equal(t, &mcp.TextContent{Text: "test_value"}, result.Content[0])
}
