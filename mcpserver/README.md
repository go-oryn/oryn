# mcpserver

[![Go Reference](https://pkg.go.dev/badge/github.com/go-oryn/oryn/mcpserver.svg)](https://pkg.go.dev/github.com/go-oryn/oryn/mcpserver)
[![codecov](https://codecov.io/gh/go-oryn/oryn/graph/badge.svg?flag=mcpserver)](https://app.codecov.io/gh/go-oryn/oryn?flags%5B0%5D=mcpserver)

> [Model Context Protocol](https://modelcontextprotocol.io) server built on the official [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk), with tools, resources and prompts registered through fx, served over the streamable HTTP transport, and OpenTelemetry instrumentation.

```shell
go get github.com/go-oryn/oryn/mcpserver
```

```go
type GreetInput struct {
	Name string `json:"name" jsonschema:"the name to greet"`
}

func main() {
	core.New(
		core.WithConfigFS(configs.FS),
		mcpserver.Module,
		mcpserver.AsCapability(func(server *mcp.Server) error {
			mcp.AddTool(server, &mcp.Tool{Name: "greet", Description: "Greets someone"},
				func(ctx context.Context, req *mcp.CallToolRequest, in GreetInput) (*mcp.CallToolResult, any, error) {
					return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "hello " + in.Name}}}, nil, nil
				})

			return nil
		}),
	).Run(mcpserver.Serve())
}
```

## Documentation

| Page | Source |
|---|---|
| [MCP server](https://go-oryn.github.io/oryn/mcpserver/overview/) (capabilities, configuration) | [docs/mcpserver/overview.md](../docs/mcpserver/overview.md) |
| [Observability](https://go-oryn.github.io/oryn/mcpserver/observability/) | [docs/mcpserver/observability.md](../docs/mcpserver/observability.md) |
| [Testing](https://go-oryn.github.io/oryn/mcpserver/testing/) | [docs/mcpserver/testing.md](../docs/mcpserver/testing.md) |
