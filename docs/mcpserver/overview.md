---
title: MCP server
description: The mcpserver module, a Model Context Protocol server with tools, resources and prompts registered through fx, served over streamable HTTP.
---

The `mcpserver` module provides a [Model Context Protocol](https://modelcontextprotocol.io) (MCP) server, built on the official [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk), instrumented with OpenTelemetry. It lets AI agents call the tools, read the resources and get the prompts of your application.

```shell
go get github.com/go-oryn/oryn/mcpserver
```

## Quick start

```go
func main() {
	core.New(
		core.WithConfigFS(configs.FS),
		mcpserver.Module,
		mcpserver.AsCapability(NewGreetTool),
	).Run(mcpserver.Serve())
}
```

- `mcpserver.Module` provides the `*mcp.Server`, with the registered capabilities, and the `*mcpserver.HTTPServer` serving it.
- `mcpserver.AsCapability` registers tools, resources and prompts, see [Capabilities](#capabilities).
- `mcpserver.Serve()` starts the [streamable HTTP transport](https://modelcontextprotocol.io/specification/latest/basic/transports#streamable-http) with the application, on `:8082/mcp`, and shuts it down gracefully when the application stops. If the server fails while running, the application stops.
- Every request is traced, measured and logged, see [Observability](observability.md).

The `*mcpserver.HTTPServer` wraps an `*http.Server`: it is a dedicated type, so the MCP server can run next to the [HTTP server](../httpserver/overview.md) in the same application.

## Capabilities

A capability registers tools, resources or prompts on the `*mcp.Server`, with the [MCP Go SDK](https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp) API. It is either:

- a value: a `mcpserver.Capability` (with a `Register(*mcp.Server) error` method), or a `func(*mcp.Server) error`,
- or a constructor returning one, whose parameters are injected by fx.

Capabilities are registered in registration order. If one fails, the application fails to start.

### Tools

`mcp.AddTool` adds a typed tool: the input and output JSON schemas are inferred from the Go types, and the input is validated and decoded before the handler is called.

```go
type GreetInput struct {
	Name string `json:"name" jsonschema:"the name to greet"`
}

type GreetOutput struct {
	Greeting string `json:"greeting"`
}

// GreetTool greets with the greeting of the config.
type GreetTool struct {
	greeting string
}

func NewGreetTool(cfg *config.Config) *GreetTool {
	return &GreetTool{greeting: cfg.GetStringOrDefault("greet.greeting", "hello")}
}

func (t *GreetTool) Register(server *mcp.Server) error {
	mcp.AddTool(server, &mcp.Tool{Name: "greet", Description: "Greets someone"}, t.greet)

	return nil
}

func (t *GreetTool) greet(ctx context.Context, req *mcp.CallToolRequest, in GreetInput) (*mcp.CallToolResult, GreetOutput, error) {
	if in.Name == "" {
		// Returned to the agent as a tool error result.
		return nil, GreetOutput{}, errors.New("missing name")
	}

	return nil, GreetOutput{Greeting: t.greeting + " " + in.Name}, nil
}
```

```go
mcpserver.AsCapability(NewGreetTool)
```

An error returned by a typed tool handler is sent to the agent as a tool error result (`isError: true`), so it can correct its call.

### Resources and prompts

```go
func NewDocs(repository *docs.Repository) mcpserver.CapabilityFunc {
	return func(server *mcp.Server) error {
		server.AddResource(&mcp.Resource{URI: "docs://readme", Name: "readme", MIMEType: "text/markdown"},
			func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{
					{URI: req.Params.URI, MIMEType: "text/markdown", Text: repository.Readme(ctx)},
				}}, nil
			})

		server.AddPrompt(&mcp.Prompt{Name: "review", Description: "Reviews a document"},
			func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
				return &mcp.GetPromptResult{Messages: []*mcp.PromptMessage{
					{Role: "user", Content: &mcp.TextContent{Text: "Review the readme."}},
				}}, nil
			})

		return nil
	}
}
```

A panic in a handler is recovered, logged with its stack, and answered with an error.

## Connect an agent

Point an MCP client to the endpoint, for example with Claude Code:

```shell
claude mcp add --transport http my-app http://localhost:8082/mcp
```

Or inspect the server with the [MCP Inspector](https://github.com/modelcontextprotocol/inspector):

```shell
npx @modelcontextprotocol/inspector
```

## Configuration

```yaml
mcpserver:
  # Listen address.
  address: ":8082"                # default ":8082"
  # Path of the MCP endpoint.
  path: /mcp                      # default /mcp
  # Stateless mode: no session, every request is independent.
  stateless: false                # default false
  # Answer with application/json instead of server-sent event streams.
  json_response: false            # default false
  # Idle sessions are closed after this duration. 0 never closes them.
  session_timeout: 30m            # default 30m
  read_header_timeout: 10s        # default 10s
  log:
    # Log every request.
    enabled: true                 # default true
  observability:
    # MCP methods excluded from spans, metrics and request logs.
    exclude:                      # default []
      - ping
```

Like every key, they can be set with environment variables: `APP_MCPSERVER_ADDRESS=:9092`.

The server name and version sent to the clients are `app.name` and `app.version`.
