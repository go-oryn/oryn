// Package mcpservertest connects an MCP client to an MCP server in memory, for tests.
package mcpservertest

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Connect connects an MCP client to the server with in-memory transports, without network,
// and returns the client session. The sessions are closed when the test ends.
//
//	var server *mcp.Server
//
//	coretest.Start(t, app, fx.Populate(&server))
//
//	session := mcpservertest.Connect(t, server)
//	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "greet"})
func Connect(tb testing.TB, server *mcp.Server) *mcp.ClientSession {
	tb.Helper()

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	serverSession, err := server.Connect(tb.Context(), serverTransport, nil)
	if err != nil {
		tb.Fatalf("cannot connect the mcp server: %v", err)
	}

	tb.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "mcpservertest", Version: "test"}, nil)

	clientSession, err := client.Connect(tb.Context(), clientTransport, nil)
	if err != nil {
		tb.Fatalf("cannot connect the mcp client: %v", err)
	}

	tb.Cleanup(func() { _ = clientSession.Close() })

	return clientSession
}
