// Package mcpclienttest connects the MCP client sessions to MCP servers in memory, for tests.
package mcpclienttest

import (
	"context"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/mcpclient"
)

// Serve connects the sessions to the server name to the given MCP server, with in-memory
// transports, without network. The client instrumentation is kept. It replaces the
// mcpclient.servers.<name>.url of the configuration:
//
//	server := mcp.NewServer(&mcp.Implementation{Name: "docs"}, nil)
//	mcp.AddTool(server, &mcp.Tool{Name: "search"}, search)
//
//	coretest.Start(t, app, mcpclienttest.Serve("docs", server), fx.Populate(&factory))
func Serve(name string, server *mcp.Server) fx.Option {
	return serve(name, func() *mcp.Server { return server })
}

// ServeContainer connects the sessions to the server name to the [*mcp.Server] of the application,
// provided by the mcpserver module, with in-memory transports: a test can then call the MCP server
// of the application through its own client.
func ServeContainer(name string) fx.Option {
	return serve(name, nil)
}

// serve registers the in-memory transport of the server name. Without server func, the server
// comes from the container. The server sessions are closed when the application stops.
func serve(name string, server func() *mcp.Server) fx.Option {
	sessions := &serverSessions{}

	newTransport := func(container *mcp.Server) mcpclient.TransportFunc {
		target := container
		if server != nil {
			target = server()
		}

		return func(ctx context.Context) (mcp.Transport, error) {
			serverTransport, clientTransport := mcp.NewInMemoryTransports()

			session, err := target.Connect(context.WithoutCancel(ctx), serverTransport, nil)
			if err != nil {
				return nil, err
			}

			sessions.add(session)

			return clientTransport, nil
		}
	}

	var constructor any = newTransport
	if server != nil {
		constructor = func() mcpclient.TransportFunc { return newTransport(nil) }
	}

	return fx.Options(
		mcpclient.AsTransport(name, constructor),
		fx.Invoke(func(lc fx.Lifecycle) { lc.Append(fx.StopHook(sessions.close)) }),
	)
}

// serverSessions are the server sessions opened in memory.
type serverSessions struct {
	mu       sync.Mutex
	sessions []*mcp.ServerSession
}

func (s *serverSessions) add(session *mcp.ServerSession) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions = append(s.sessions, session)
}

func (s *serverSessions) close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, session := range s.sessions {
		_ = session.Close()
	}

	s.sessions = nil
}
