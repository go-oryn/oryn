---
title: MCP client
description: The mcpclient module, Model Context Protocol client sessions to the configured MCP servers, instrumented with OpenTelemetry.
---

The `mcpclient` module opens [Model Context Protocol](https://modelcontextprotocol.io) (MCP) client sessions to the MCP servers of the configuration, built on the official [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk), instrumented with OpenTelemetry. It lets your application call the tools, read the resources and get the prompts of other MCP servers.

```shell
go get github.com/go-oryn/oryn/mcpclient
```

## Quick start

Name the servers in the configuration:

```yaml
mcpclient:
  servers:
    docs:
      url: https://docs.example.com/mcp
      headers:
        Authorization: Bearer ${DOCS_TOKEN}
```

Add the module, and get the sessions from the `*mcpclient.Factory`:

```go
func main() {
	core.New(
		core.WithConfigFS(configs.FS),
		mcpclient.Module,
		fx.Provide(NewSearch),
	).Run()
}

type Search struct {
	factory *mcpclient.Factory
}

func NewSearch(factory *mcpclient.Factory) *Search {
	return &Search{factory: factory}
}

func (s *Search) Search(ctx context.Context, query string) (*mcp.CallToolResult, error) {
	session, err := s.factory.Session(ctx, "docs")
	if err != nil {
		return nil, err
	}

	return session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "search",
		Arguments: map[string]any{"query": query},
	})
}
```

- `mcpclient.Module` provides the `*mcpclient.Factory`.
- Every request is traced, measured and logged, and the trace continues on the server, see [Observability](observability.md).
- Servers are reached over the [streamable HTTP transport](https://modelcontextprotocol.io/specification/latest/basic/transports#streamable-http), see [Configuration](configuration.md).

## Sessions

`factory.Session(ctx, name)` returns the `*mcp.ClientSession` to the server `name`, with the [MCP Go SDK](https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp#ClientSession) API: `CallTool`, `ListTools`, `ReadResource`, `GetPrompt`...

- The session is opened on the first call, then reused: call `Session` every time you need it, it is cheap and safe for concurrent use.
- Server names are case-insensitive.
- The session is not tied to `ctx`: it outlives the request that opened it. `ctx` still traces the connection.
- If the connection fails, the error is returned and the next call retries.
- All sessions are closed when the application stops. After that, `Session` returns `mcpclient.ErrClosed`.

The client sends `app.name` and `app.version` as its name and version.

## Custom transports

A server can be reached through another transport than streamable HTTP, for example a local server started as a subprocess over stdio. Register a `mcpclient.TransportFunc` for its name with `AsTransport`: it is called each time the session is opened, and wins over `mcpclient.servers.<name>.url`. It is either a value, or a constructor whose parameters are injected by fx:

```go
mcpclient.AsTransport("git", func(ctx context.Context) (mcp.Transport, error) {
	return &mcp.CommandTransport{Command: exec.Command("mcp-server-git")}, nil
})
```

The requests are traced, measured and logged like the others. [mcpclienttest](testing.md) uses it to serve servers in memory.

## Configuration

The `mcpclient.*` keys set the URL, headers and timeout of each server, the request logs and the methods excluded from telemetry. See [Configuration](configuration.md).
