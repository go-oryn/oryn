# mcpclient

[![Go Reference](https://pkg.go.dev/badge/github.com/go-oryn/oryn/mcpclient.svg)](https://pkg.go.dev/github.com/go-oryn/oryn/mcpclient)
[![codecov](https://codecov.io/gh/go-oryn/oryn/graph/badge.svg?flag=mcpclient)](https://app.codecov.io/gh/go-oryn/oryn?flags%5B0%5D=mcpclient)

> [Model Context Protocol](https://modelcontextprotocol.io) client sessions to the configured MCP servers, built on the official [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk), with OpenTelemetry instrumentation and trace propagation to the servers.

```shell
go get github.com/go-oryn/oryn/mcpclient
```

```yaml
mcpclient:
  servers:
    docs:
      url: https://docs.example.com/mcp
      headers:
        Authorization: Bearer ${DOCS_TOKEN}
```

```go
session, err := factory.Session(ctx, "docs") // *mcpclient.Factory, provided by mcpclient.Module
if err != nil {
	return err
}

result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "search", Arguments: map[string]any{"query": "oryn"}})
```

## Documentation

| Page | Source |
|---|---|
| [MCP client](https://go-oryn.github.io/oryn/mcpclient/overview/) (sessions, transports) | [docs/mcpclient/overview.md](../docs/mcpclient/overview.md) |
| [Configuration](https://go-oryn.github.io/oryn/mcpclient/configuration/) | [docs/mcpclient/configuration.md](../docs/mcpclient/configuration.md) |
| [Observability](https://go-oryn.github.io/oryn/mcpclient/observability/) | [docs/mcpclient/observability.md](../docs/mcpclient/observability.md) |
| [Testing](https://go-oryn.github.io/oryn/mcpclient/testing/) | [docs/mcpclient/testing.md](../docs/mcpclient/testing.md) |
