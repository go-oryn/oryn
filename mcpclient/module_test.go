package mcpclient_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/core/coretest"
	"github.com/go-oryn/oryn/core/otel/oteltest/logtest"
	"github.com/go-oryn/oryn/core/otel/oteltest/metrictest"
	"github.com/go-oryn/oryn/core/otel/oteltest/spantest"
	"github.com/go-oryn/oryn/mcpclient"
	"github.com/go-oryn/oryn/mcpclient/mcpclienttest"
	"github.com/go-oryn/oryn/mcpserver"
)

// greetInput is the input of the greet tool.
type greetInput struct {
	Name string `json:"name"`
}

// addTools adds the test tools, resource and prompt to the server.
func addTools(server *mcp.Server) error {
	mcp.AddTool(server, &mcp.Tool{Name: "greet"}, func(_ context.Context, req *mcp.CallToolRequest, in greetInput) (*mcp.CallToolResult, any, error) {
		text := "hello " + in.Name
		if extra := req.GetExtra(); extra != nil && extra.Header != nil && extra.Header.Get("X-Test") != "" {
			text += " " + extra.Header.Get("X-Test")
		}

		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{Name: "fail"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		return nil, nil, errors.New("test_tool_error")
	})

	mcp.AddTool(server, &mcp.Tool{Name: "slow"}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-time.After(5 * time.Second):
			return &mcp.CallToolResult{}, nil, nil
		}
	})

	server.AddResource(&mcp.Resource{URI: "test://resource", Name: "resource"},
		func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: req.Params.URI, Text: "test_value"}}}, nil
		})

	server.AddPrompt(&mcp.Prompt{Name: "review"}, func(context.Context, *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{Messages: []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: "test_prompt"}}}}, nil
	})

	return nil
}

// newServer returns a standalone MCP server with the test tools.
func newServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "test_server", Version: "test"}, nil)
	_ = addTools(server)

	return server
}

// telemetry is the in-memory telemetry of a test application.
type telemetry struct {
	spans   *tracetest.InMemoryExporter
	logs    *logtest.InMemoryExporter
	metrics *sdkmetric.ManualReader
}

// start starts an application with the mcpclient module, the config values and the options,
// and returns its factory and telemetry.
func start(t *testing.T, values map[string]any, options ...fx.Option) (*mcpclient.Factory, telemetry) {
	t.Helper()

	var (
		factory *mcpclient.Factory
		tel     telemetry
	)

	coretest.Start(t, core.New(mcpclient.Module, config.AsOptions(config.WithValues(values))),
		append(options, fx.Populate(&factory, &tel.spans, &tel.logs, &tel.metrics))...)

	return factory, tel
}

// session returns the session to the server name.
func session(t *testing.T, factory *mcpclient.Factory, name string) *mcp.ClientSession {
	t.Helper()

	s, err := factory.Session(t.Context(), name)
	require.NoError(t, err)

	return s
}

func TestFactory(t *testing.T) {
	t.Parallel()

	t.Run("calls a tool in memory", func(t *testing.T) {
		t.Parallel()

		factory, tel := start(t, nil, mcpclienttest.Serve("Docs", newServer()))

		s := session(t, factory, "docs")
		assert.Same(t, s, session(t, factory, " DOCS "), "the session is reused")

		result, err := s.CallTool(t.Context(), &mcp.CallToolParams{
			Meta:      mcp.Meta{"test_key": "test_value"},
			Name:      "greet",
			Arguments: greetInput{Name: "oryn"},
		})
		require.NoError(t, err)
		require.False(t, result.IsError)
		assert.Equal(t, "hello oryn", result.Content[0].(*mcp.TextContent).Text)

		spantest.HasSpan(t, tel.spans,
			spantest.WithName("tools/call greet"),
			spantest.WithKind(trace.SpanKindClient),
			spantest.WithStatus(codes.Unset),
			spantest.WithAttributeValue(mcpclient.MCPMethodNameKey.String("tools/call")),
			spantest.WithAttributeValue(mcpclient.GenAIOperationNameKey.String("execute_tool")),
			spantest.WithAttributeValue(mcpclient.GenAIToolNameKey.String("greet")),
		)
		spantest.HasSpan(t, tel.spans, spantest.WithName("server/discover"), spantest.WithKind(trace.SpanKindClient))
		metrictest.HasMetric(t, tel.metrics,
			metrictest.WithName(mcpclient.MetricOperationDuration),
			metrictest.WithUnit("s"),
			metrictest.WithAttributeValue(mcpclient.GenAIToolNameKey.String("greet")),
		)
		logtest.HasRecord(t, tel.logs,
			logtest.WithBody("mcp client request"),
			logtest.WithLevel(slog.LevelInfo),
			logtest.WithAttributeValue("server", "docs"),
			logtest.WithAttributeValue("method", "tools/call"),
			logtest.WithAttributeValue("target", "greet"),
		)
	})

	t.Run("resources and prompts", func(t *testing.T) {
		t.Parallel()

		factory, tel := start(t, nil, mcpclienttest.Serve("docs", newServer()))
		s := session(t, factory, "docs")

		resource, err := s.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: "test://resource"})
		require.NoError(t, err)
		assert.Equal(t, "test_value", resource.Contents[0].Text)

		_, err = s.GetPrompt(t.Context(), &mcp.GetPromptParams{Name: "review"})
		require.NoError(t, err)

		_, err = s.ListTools(t.Context(), nil)
		require.NoError(t, err)

		spantest.HasSpan(t, tel.spans,
			spantest.WithName("resources/read test://resource"),
			spantest.WithAttributeValue(mcpclient.MCPResourceURIKey.String("test://resource")),
		)
		spantest.HasSpan(t, tel.spans,
			spantest.WithName("prompts/get review"),
			spantest.WithAttributeValue(mcpclient.GenAIPromptNameKey.String("review")),
		)
		spantest.HasSpan(t, tel.spans, spantest.WithName("tools/list"))
		metrictest.HasMetric(t, tel.metrics,
			metrictest.WithName(mcpclient.MetricOperationDuration),
			metrictest.WithAttributeValue(mcpclient.GenAIPromptNameKey.String("review")),
		)
	})

	t.Run("tool error", func(t *testing.T) {
		t.Parallel()

		factory, tel := start(t, nil, mcpclienttest.Serve("docs", newServer()))

		result, err := session(t, factory, "docs").CallTool(t.Context(), &mcp.CallToolParams{Name: "fail"})
		require.NoError(t, err)
		assert.True(t, result.IsError)

		spantest.HasSpan(t, tel.spans,
			spantest.WithName("tools/call fail"),
			spantest.WithStatus(codes.Error),
			spantest.WithAttributeValue(semconv.ErrorTypeKey.String(mcpclient.ErrorTypeToolError)),
		)
		metrictest.HasMetric(t, tel.metrics,
			metrictest.WithName(mcpclient.MetricOperationDuration),
			metrictest.WithAttributeValue(semconv.ErrorTypeKey.String(mcpclient.ErrorTypeToolError)),
		)
		logtest.HasRecord(t, tel.logs,
			logtest.WithBody("mcp client request"),
			logtest.WithLevel(slog.LevelError),
			logtest.WithAttributeValue("error_type", mcpclient.ErrorTypeToolError),
			logtest.WithAttributeValue("error", "test_tool_error"),
		)
	})

	t.Run("protocol error", func(t *testing.T) {
		t.Parallel()

		factory, tel := start(t, nil, mcpclienttest.Serve("docs", newServer()))

		_, err := session(t, factory, "docs").CallTool(t.Context(), &mcp.CallToolParams{Name: "missing"})
		require.Error(t, err)

		spans := spantest.Find(tel.spans, spantest.WithName("tools/call missing"), spantest.WithStatus(codes.Error))
		require.Len(t, spans, 1)
		assert.Contains(t, spans[0].Attributes, semconv.ErrorTypeKey.String("-32602"))
		logtest.HasRecord(t, tel.logs, logtest.WithBody("mcp client request"), logtest.WithLevel(slog.LevelError),
			logtest.WithAttributeValue("error_type", "-32602"))
	})

	t.Run("continues the trace on the server", func(t *testing.T) {
		t.Parallel()

		factory, tel := start(t, nil,
			mcpserver.Module,
			mcpserver.AsCapability(addTools),
			mcpclienttest.ServeContainer("self"),
		)

		_, err := session(t, factory, "self").CallTool(t.Context(), &mcp.CallToolParams{Name: "greet", Arguments: greetInput{Name: "oryn"}})
		require.NoError(t, err)

		client := spantest.Find(tel.spans, spantest.WithName("tools/call greet"), spantest.WithKind(trace.SpanKindClient))
		require.Len(t, client, 1)
		spantest.HasSpan(t, tel.spans,
			spantest.WithName("tools/call greet"),
			spantest.WithKind(trace.SpanKindServer),
			spantest.WithParent(client[0].SpanContext.SpanID()),
		)
	})

	t.Run("excluded methods", func(t *testing.T) {
		t.Parallel()

		factory, tel := start(t, map[string]any{"mcpclient.observability.exclude": []string{"tools/list"}},
			mcpclienttest.Serve("docs", newServer()))

		_, err := session(t, factory, "docs").ListTools(t.Context(), nil)
		require.NoError(t, err)

		spantest.HasNoSpan(t, tel.spans, spantest.WithName("tools/list"))
		logtest.HasNoRecord(t, tel.logs, logtest.WithAttributeValue("method", "tools/list"))
		spantest.HasSpan(t, tel.spans, spantest.WithName("server/discover"))
	})

	t.Run("logs disabled", func(t *testing.T) {
		t.Parallel()

		factory, tel := start(t, map[string]any{"mcpclient.log.enabled": false}, mcpclienttest.Serve("docs", newServer()))

		_, err := session(t, factory, "docs").ListTools(t.Context(), nil)
		require.NoError(t, err)

		spantest.HasSpan(t, tel.spans, spantest.WithName("tools/list"))
		logtest.HasNoRecord(t, tel.logs, logtest.WithBody("mcp client request"))
	})

	t.Run("timeout", func(t *testing.T) {
		t.Parallel()

		factory, tel := start(t, map[string]any{"mcpclient.servers.docs.timeout": "50ms"}, mcpclienttest.Serve("docs", newServer()))

		_, err := session(t, factory, "docs").CallTool(t.Context(), &mcp.CallToolParams{Name: "slow"})
		require.ErrorIs(t, err, context.DeadlineExceeded)

		spantest.HasSpan(t, tel.spans, spantest.WithName("tools/call slow"), spantest.WithStatus(codes.Error))
	})

	t.Run("closed when the application stops", func(t *testing.T) {
		t.Parallel()

		var factory *mcpclient.Factory

		app := coretest.New(t, core.New(mcpclient.Module), mcpclienttest.Serve("docs", newServer()), fx.Populate(&factory))
		app.RequireStart()

		s := session(t, factory, "docs")

		app.RequireStop()

		_, err := factory.Session(t.Context(), "docs")
		require.ErrorIs(t, err, mcpclient.ErrClosed)

		_, err = s.ListTools(t.Context(), nil)
		require.Error(t, err, "the session is closed")
	})

	t.Run("errors", func(t *testing.T) {
		t.Parallel()

		factory, _ := start(t, map[string]any{
			"mcpclient.servers.invalid.url": "not a url",
			"mcpclient.servers.down.url":    "http://127.0.0.1:1/mcp",
		}, mcpclient.AsTransport("broken", func(context.Context) (mcp.Transport, error) {
			return nil, errors.New("test_transport_error")
		}))

		for name, expected := range map[string]string{
			"unknown": "cannot connect to the unknown mcp server: unknown server: missing mcpclient.servers.unknown.url",
			"invalid": `invalid mcpclient.servers.invalid.url "not a url"`,
			"broken":  "test_transport_error",
			"down":    "cannot connect to the down mcp server",
		} {
			_, err := factory.Session(t.Context(), name)
			require.ErrorContains(t, err, expected, name)
		}
	})
}

func TestAsTransport(t *testing.T) {
	t.Parallel()

	t.Run("constructor with dependencies", func(t *testing.T) {
		t.Parallel()

		server := newServer()

		factory, _ := start(t, map[string]any{"test.greeting": "test_value"},
			mcpclient.AsTransport("docs", func(cfg *config.Config) mcpclient.TransportFunc {
				assert.Equal(t, "test_value", cfg.GetString("test.greeting"))

				return func(ctx context.Context) (mcp.Transport, error) {
					serverTransport, clientTransport := mcp.NewInMemoryTransports()
					if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
						return nil, err
					}

					return clientTransport, nil
				}
			}),
		)

		_, err := session(t, factory, "docs").ListTools(t.Context(), nil)
		require.NoError(t, err)
	})

	t.Run("invalid", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct {
			server    string
			transport any
			err       string
		}{
			"nil":         {server: "docs", transport: nil, err: "nil transport"},
			"empty name":  {server: " ", transport: mcpclient.TransportFunc(nil), err: "empty server name"},
			"unsupported": {server: "docs", transport: "test_value", err: "unsupported string"},
		} {
			app := fx.New(core.New(mcpclient.Module, mcpclient.AsTransport(tc.server, tc.transport)).Options(coretest.TestOptions(), fx.NopLogger)...)
			require.ErrorContains(t, app.Err(), "cannot register mcp transport: "+tc.err, name)
		}
	})
}

func TestStreamableHTTP(t *testing.T) {
	t.Parallel()

	// The server application, listening on a random port.
	var (
		httpServer  *mcpserver.HTTPServer
		serverSpans *tracetest.InMemoryExporter
	)

	coretest.Start(t,
		core.New(mcpserver.Module, mcpserver.AsCapability(addTools),
			config.AsOptions(config.WithValues(map[string]any{"mcpserver.address": "127.0.0.1:0"}))),
		mcpserver.Serve(),
		fx.Populate(&httpServer, &serverSpans),
	)

	// The client application, configured with its URL and a header.
	factory, tel := start(t, map[string]any{
		"mcpclient.servers.remote.url":            "http://" + httpServer.Addr + "/mcp",
		"mcpclient.servers.remote.headers.x-test": "test_header",
	})

	result, err := session(t, factory, "remote").CallTool(t.Context(), &mcp.CallToolParams{Name: "greet", Arguments: greetInput{Name: "oryn"}})
	require.NoError(t, err)
	assert.Equal(t, "hello oryn test_header", result.Content[0].(*mcp.TextContent).Text, "the configured header is sent")

	client := spantest.Find(tel.spans,
		spantest.WithName("tools/call greet"),
		spantest.WithKind(trace.SpanKindClient),
		spantest.WithAttributeValue(semconv.ServerAddress("127.0.0.1")),
		spantest.WithAttribute(semconv.ServerPortKey),
		spantest.WithAttribute(mcpclient.MCPSessionIDKey),
	)
	require.Len(t, client, 1)

	// The HTTP request is traced under the MCP span, and the server continues the trace.
	spantest.HasSpan(t, tel.spans, spantest.WithKind(trace.SpanKindClient), spantest.WithParent(client[0].SpanContext.SpanID()),
		spantest.WithAttribute(semconv.HTTPRequestMethodKey))
	spantest.HasSpan(t, serverSpans,
		spantest.WithName("tools/call greet"),
		spantest.WithKind(trace.SpanKindServer),
		spantest.WithParent(client[0].SpanContext.SpanID()),
	)
	metrictest.HasMetric(t, tel.metrics,
		metrictest.WithName(mcpclient.MetricOperationDuration),
		metrictest.WithAttributeValue(attribute.String("server.address", "127.0.0.1")),
	)
}

func TestServerAttributes(t *testing.T) {
	t.Parallel()

	for endpoint, port := range map[string]int{"http://example.com/mcp": 80, "https://example.com/mcp": 443} {
		attributes := mcpclient.ServerAttributes(endpoint)
		assert.Contains(t, attributes, semconv.ServerPort(port), endpoint)
	}

	assert.Len(t, mcpclient.ServerAttributes("unknown://example.com/mcp"), 1, "no port")
}

func TestSDKLogger(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logger := mcpclient.SDKLogger(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))).
		WithGroup("test_group").With("test_key", "test_value")

	assert.True(t, logger.Enabled(t.Context(), slog.LevelInfo))
	logger.Info("test_info")
	logger.Warn("test_warn")

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 2)
	assert.Contains(t, lines[0], "level=DEBUG msg=test_info test_group.test_key=test_value")
	assert.Contains(t, lines[1], "level=WARN msg=test_warn")
}
