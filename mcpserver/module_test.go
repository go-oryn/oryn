package mcpserver_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/core/coretest"
	"github.com/go-oryn/oryn/core/otel/oteltest/logtest"
	"github.com/go-oryn/oryn/core/otel/oteltest/spantest"
	"github.com/go-oryn/oryn/mcpserver"
	"github.com/go-oryn/oryn/mcpserver/mcpservertest"
)

// telemetry holds the in memory telemetry of a test application.
type telemetry struct {
	spans   *tracetest.InMemoryExporter
	logs    *logtest.InMemoryExporter
	metrics *sdkmetric.ManualReader
}

// startServer starts a test application with the mcpserver module, the test capabilities and
// the options, and returns a client session connected to its server, and its telemetry.
func startServer(t *testing.T, options ...fx.Option) (*mcp.ClientSession, telemetry) {
	t.Helper()

	var (
		server *mcp.Server
		tel    telemetry
	)

	app := core.New(mcpserver.Module, mcpserver.AsCapability(newGreetCapability, newPanicCapability, newDocsCapability))
	coretest.Start(t, app, append(options, fx.Populate(&server, &tel.spans, &tel.logs, &tel.metrics))...)

	return mcpservertest.Connect(t, server), tel
}

// newServerError returns the error of an application failing to build the MCP server.
func newServerError(t *testing.T, options ...fx.Option) error {
	t.Helper()

	app := fx.New(core.New(mcpserver.Module).Options(append(options,
		coretest.TestOptions(),
		fx.NopLogger,
		fx.Invoke(func(*mcp.Server) {}),
	)...)...)

	return app.Err()
}

// withValues sets config values.
func withValues(values map[string]any) fx.Option {
	return config.AsOptions(config.WithValues(values))
}

// greetInput is the input of the greet tool.
type greetInput struct {
	Name string `json:"name,omitempty" jsonschema:"the name to greet"`
}

// newGreetCapability returns a capability adding the typed greet tool, with its greeting from the config.
func newGreetCapability(cfg *config.Config) mcpserver.Capability {
	greeting := cfg.GetStringOrDefault("test.greeting", "hello")

	return mcpserver.CapabilityFunc(func(server *mcp.Server) error {
		mcp.AddTool(server, &mcp.Tool{Name: "greet", Description: "Greets someone."},
			func(_ context.Context, _ *mcp.CallToolRequest, in greetInput) (*mcp.CallToolResult, any, error) {
				if in.Name == "" {
					return nil, nil, errors.New("missing name")
				}

				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: greeting + " " + in.Name}}}, nil, nil
			})

		return nil
	})
}

// newPanicCapability returns a capability adding a tool that panics.
func newPanicCapability() func(*mcp.Server) error {
	return func(server *mcp.Server) error {
		server.AddTool(&mcp.Tool{Name: "panic", InputSchema: map[string]any{"type": "object"}},
			func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				panic("test_panic")
			})

		return nil
	}
}

// docsCapability adds a resource and a prompt.
type docsCapability struct{}

func newDocsCapability() *docsCapability {
	return &docsCapability{}
}

func (docsCapability) Register(server *mcp.Server) error {
	server.AddResource(&mcp.Resource{URI: "docs://readme", Name: "readme", MIMEType: "text/plain"},
		func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: req.Params.URI, Text: "test_readme"}}}, nil
		})

	server.AddPrompt(&mcp.Prompt{Name: "review"},
		func(context.Context, *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			return &mcp.GetPromptResult{Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: "test_prompt"}}}}, nil
		})

	return nil
}

// text returns the text of the first content of a tool result.
func text(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()

	require.NotEmpty(t, result.Content)

	content, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)

	return content.Text
}

func TestNewServer(t *testing.T) {
	t.Parallel()

	t.Run("capabilities", func(t *testing.T) {
		t.Parallel()

		session, _ := startServer(t, withValues(map[string]any{"app.name": "test_app", "test.greeting": "hi"}))

		assert.Equal(t, "test_app", session.InitializeResult().ServerInfo.Name)

		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "greet", Arguments: map[string]any{"name": "oryn"}})
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.Equal(t, "hi oryn", text(t, result))

		resource, err := session.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: "docs://readme"})
		require.NoError(t, err)
		assert.Equal(t, "test_readme", resource.Contents[0].Text)

		prompt, err := session.GetPrompt(t.Context(), &mcp.GetPromptParams{Name: "review"})
		require.NoError(t, err)
		assert.Len(t, prompt.Messages, 1)
	})

	t.Run("registration order", func(t *testing.T) {
		t.Parallel()

		describe := func(description string) mcpserver.CapabilityFunc {
			return func(server *mcp.Server) error {
				server.AddTool(&mcp.Tool{Name: "test_tool", Description: description, InputSchema: map[string]any{"type": "object"}},
					func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
						return &mcp.CallToolResult{}, nil
					})

				return nil
			}
		}

		capabilities := make([]any, 0, 10)
		for _, description := range []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "last"} {
			capabilities = append(capabilities, describe(description))
		}

		session, _ := startServer(t, mcpserver.AsCapability(capabilities...))

		tools, err := session.ListTools(t.Context(), nil)
		require.NoError(t, err)

		for _, tool := range tools.Tools {
			if tool.Name == "test_tool" {
				assert.Equal(t, "last", tool.Description, "the last registered capability wins")
			}
		}
	})

	t.Run("constructor with error", func(t *testing.T) {
		t.Parallel()

		var server *mcp.Server

		coretest.Start(t, core.New(mcpserver.Module, mcpserver.AsCapability(func() (mcpserver.Capability, error) {
			return mcpserver.CapabilityFunc(func(*mcp.Server) error { return nil }), nil
		})), fx.Populate(&server))

		assert.NotNil(t, server)
	})

	t.Run("registration errors", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct {
			capability any
			err        string
		}{
			"nil":             {capability: nil, err: "cannot register mcp capability: nil capability"},
			"unsupported":     {capability: "test_value", err: "unsupported string"},
			"bad constructor": {capability: func() string { return "" }, err: "unsupported func() string"},
			"failing": {
				capability: func(*mcp.Server) error { return errors.New("test_error") },
				err:        "cannot register mcp capability: test_error",
			},
			"failing constructor": {
				capability: func() (mcpserver.Capability, error) { return nil, errors.New("test_error") },
				err:        "test_error",
			},
		} {
			err := newServerError(t, mcpserver.AsCapability(tc.capability))
			require.ErrorContains(t, err, tc.err, name)
		}
	})
}

// serve starts a test application serving the MCP server over HTTP on a random port, and returns
// a client session connected to it, and the telemetry.
func serve(t *testing.T, values map[string]any) (*mcp.ClientSession, telemetry) {
	t.Helper()

	var (
		server *mcpserver.HTTPServer
		tel    telemetry
	)

	values["mcpserver.address"] = "127.0.0.1:0"

	app := core.New(mcpserver.Module, mcpserver.AsCapability(newGreetCapability))
	coretest.Start(t, app, withValues(values), mcpserver.Serve(), fx.Populate(&server, &tel.spans, &tel.logs, &tel.metrics))

	client := mcp.NewClient(&mcp.Implementation{Name: "test_client", Version: "test"}, nil)

	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: "http://" + server.Addr + path(values), DisableStandaloneSSE: true}, nil)
	require.NoError(t, err)

	t.Cleanup(func() { _ = session.Close() })

	return session, tel
}

// path returns the MCP endpoint path of the config values.
func path(values map[string]any) string {
	if custom, ok := values["mcpserver.path"].(string); ok {
		return custom
	}

	return mcpserver.DefaultPath
}

func TestServe(t *testing.T) {
	t.Parallel()

	for name, values := range map[string]map[string]any{
		"default":   {},
		"stateless": {"mcpserver.stateless": true, "mcpserver.json_response": true, "mcpserver.path": "/test_path"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			session, tel := serve(t, values)

			result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "greet", Arguments: map[string]any{"name": "oryn"}})
			require.NoError(t, err)
			assert.Equal(t, "hello oryn", text(t, result))

			// The MCP span is a child of the HTTP server span.
			call := spantest.Find(tel.spans, spantest.WithName("tools/call greet"))
			require.Len(t, call, 1)
			spantest.HasSpan(t, tel.spans,
				spantest.WithName("POST "+path(values)),
				spantest.WithKind(trace.SpanKindServer),
				func(span tracetest.SpanStub) bool { return span.SpanContext.SpanID() == call[0].Parent.SpanID() },
			)
		})
	}

	t.Run("session id", func(t *testing.T) {
		t.Parallel()

		session, tel := serve(t, map[string]any{})

		_, err := session.ListTools(t.Context(), nil)
		require.NoError(t, err)

		if session.ID() != "" {
			spantest.HasSpan(t, tel.spans, spantest.WithName("tools/list"), spantest.WithAttributeValue(mcpserver.MCPSessionIDKey.String(session.ID())))
		}
	})

	t.Run("cannot listen", func(t *testing.T) {
		t.Parallel()

		app := coretest.New(t, core.New(mcpserver.Module), withValues(map[string]any{"mcpserver.address": "256.0.0.1:0"}), mcpserver.Serve())

		require.ErrorContains(t, app.Start(t.Context()), "cannot start mcp server on 256.0.0.1:0")
	})
}

func TestSDKLogs(t *testing.T) {
	t.Parallel()

	t.Run("logged at debug level", func(t *testing.T) {
		t.Parallel()

		_, tel := startServer(t, withValues(map[string]any{"log.level": "debug"}))

		logtest.HasRecord(t, tel.logs, logtest.WithBody("server session connected"), logtest.WithLevel(slog.LevelDebug))
		logtest.HasNoRecord(t, tel.logs, logtest.WithBody("server session connected"), logtest.WithLevel(slog.LevelInfo))
	})

	t.Run("hidden at info level", func(t *testing.T) {
		t.Parallel()

		_, tel := startServer(t)

		logtest.HasNoRecord(t, tel.logs, logtest.WithBody("server session connected"))
	})

	t.Run("warnings kept", func(t *testing.T) {
		t.Parallel()

		var (
			logger *slog.Logger
			logs   *logtest.InMemoryExporter
		)

		coretest.Start(t, core.New(), fx.Populate(&logger, &logs))

		mcpserver.SDKLogger(logger).With("test_key", "test_value").WithGroup("test_group").Warn("test_warning")

		logtest.HasRecord(t, logs, logtest.WithBody("test_warning"), logtest.WithLevel(slog.LevelWarn))
	})
}
