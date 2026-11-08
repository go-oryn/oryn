package mcpserver_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/go-oryn/oryn/core/otel/oteltest/logtest"
	"github.com/go-oryn/oryn/core/otel/oteltest/metrictest"
	"github.com/go-oryn/oryn/core/otel/oteltest/spantest"
	"github.com/go-oryn/oryn/mcpserver"
)

func TestObservability(t *testing.T) {
	t.Parallel()

	t.Run("tool call", func(t *testing.T) {
		t.Parallel()

		session, tel := startServer(t)

		_, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "greet", Arguments: map[string]any{"name": "oryn"}})
		require.NoError(t, err)

		spantest.HasSpan(t, tel.spans,
			spantest.WithName("tools/call greet"),
			spantest.WithKind(trace.SpanKindServer),
			spantest.WithAttributeValue(mcpserver.MCPMethodNameKey.String("tools/call")),
			spantest.WithAttributeValue(mcpserver.GenAIOperationNameKey.String("execute_tool")),
			spantest.WithAttributeValue(mcpserver.GenAIToolNameKey.String("greet")),
			spantest.WithStatus(codes.Unset),
		)
		spantest.HasSpan(t, tel.spans, spantest.WithName("server/discover"))
		spantest.HasNoSpan(t, tel.spans, spantest.WithAttribute(semconv.ErrorTypeKey))

		metrictest.HasMetric(t, tel.metrics,
			metrictest.WithName(mcpserver.MetricOperationDuration),
			metrictest.WithUnit("s"),
			metrictest.WithAttributeValue(mcpserver.GenAIToolNameKey.String("greet")),
		)

		logtest.HasRecord(t, tel.logs,
			logtest.WithBody("mcp request"),
			logtest.WithLevel(slog.LevelInfo),
			logtest.WithAttributeValue("method", "tools/call"),
			logtest.WithAttributeValue("target", "greet"),
		)
	})

	t.Run("resource and prompt", func(t *testing.T) {
		t.Parallel()

		session, tel := startServer(t)

		_, err := session.ReadResource(t.Context(), &mcp.ReadResourceParams{URI: "docs://readme"})
		require.NoError(t, err)

		_, err = session.GetPrompt(t.Context(), &mcp.GetPromptParams{Name: "review"})
		require.NoError(t, err)

		spantest.HasSpan(t, tel.spans,
			spantest.WithName("resources/read docs://readme"),
			spantest.WithAttributeValue(mcpserver.MCPResourceURIKey.String("docs://readme")),
		)
		spantest.HasSpan(t, tel.spans,
			spantest.WithName("prompts/get review"),
			spantest.WithAttributeValue(mcpserver.GenAIPromptNameKey.String("review")),
		)
		metrictest.HasMetric(t, tel.metrics,
			metrictest.WithName(mcpserver.MetricOperationDuration),
			metrictest.WithAttributeValue(mcpserver.GenAIPromptNameKey.String("review")),
		)
	})

	t.Run("tool error", func(t *testing.T) {
		t.Parallel()

		session, tel := startServer(t)

		result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "greet", Arguments: map[string]any{}})
		require.NoError(t, err)
		assert.True(t, result.IsError)

		spantest.HasSpan(t, tel.spans,
			spantest.WithName("tools/call greet"),
			spantest.WithStatus(codes.Error),
			spantest.WithAttributeValue(semconv.ErrorTypeKey.String(mcpserver.ErrorTypeToolError)),
		)
		metrictest.HasMetric(t, tel.metrics,
			metrictest.WithName(mcpserver.MetricOperationDuration),
			metrictest.WithAttributeValue(semconv.ErrorTypeKey.String(mcpserver.ErrorTypeToolError)),
		)
		logtest.HasRecord(t, tel.logs,
			logtest.WithBody("mcp request"),
			logtest.WithLevel(slog.LevelError),
			logtest.WithAttributeValue("error_type", mcpserver.ErrorTypeToolError),
			logtest.WithAttributeValue("error", "missing name"),
		)
	})

	t.Run("tool error without text", func(t *testing.T) {
		t.Parallel()

		session, tel := startServer(t, mcpserver.AsCapability(func(server *mcp.Server) error {
			server.AddTool(&mcp.Tool{Name: "image_error", InputSchema: map[string]any{"type": "object"}},
				func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
					return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.ImageContent{Data: []byte("test"), MIMEType: "image/png"}}}, nil
				})

			return nil
		}))

		_, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "image_error"})
		require.NoError(t, err)

		logtest.HasRecord(t, tel.logs, logtest.WithBody("mcp request"), logtest.WithAttributeValue("error", "tool error"))
	})

	t.Run("protocol error", func(t *testing.T) {
		t.Parallel()

		session, tel := startServer(t)

		_, err := session.GetPrompt(t.Context(), &mcp.GetPromptParams{Name: "unknown"})
		require.Error(t, err)

		spantest.HasSpan(t, tel.spans,
			spantest.WithName("prompts/get unknown"),
			spantest.WithStatus(codes.Error),
			spantest.WithAttribute(semconv.ErrorTypeKey),
		)
		logtest.HasRecord(t, tel.logs, logtest.WithBody("mcp request"), logtest.WithLevel(slog.LevelError))
	})

	t.Run("panic", func(t *testing.T) {
		t.Parallel()

		session, tel := startServer(t)

		_, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "panic"})
		require.ErrorContains(t, err, "test_panic")

		spantest.HasSpan(t, tel.spans,
			spantest.WithName("tools/call panic"),
			spantest.WithStatus(codes.Error),
			spantest.WithAttributeValue(semconv.ErrorTypeOther),
		)
		logtest.HasRecord(t, tel.logs, logtest.WithBody("mcp handler panic"), logtest.WithAttribute("stack"))
	})

	t.Run("trace context from _meta", func(t *testing.T) {
		t.Parallel()

		session, tel := startServer(t)

		traceID := "0af7651916cd43dd8448eb211c80319c"
		params := &mcp.CallToolParams{Name: "greet", Arguments: map[string]any{"name": "oryn"}}
		params.SetMeta(map[string]any{"traceparent": "00-" + traceID + "-b7ad6b7169203331-01", "ignored": 1})

		_, err := session.CallTool(t.Context(), params)
		require.NoError(t, err)

		spans := spantest.Find(tel.spans, spantest.WithName("tools/call greet"))
		require.Len(t, spans, 1)
		assert.Equal(t, traceID, spans[0].SpanContext.TraceID().String())
		assert.Equal(t, "b7ad6b7169203331", spans[0].Parent.SpanID().String())
	})

	t.Run("excluded methods", func(t *testing.T) {
		t.Parallel()

		session, tel := startServer(t, withValues(map[string]any{"mcpserver.observability.exclude": []string{"tools/list"}}))

		_, err := session.ListTools(t.Context(), nil)
		require.NoError(t, err)

		spantest.HasNoSpan(t, tel.spans, spantest.WithName("tools/list"))
		logtest.HasNoRecord(t, tel.logs, logtest.WithAttributeValue("method", "tools/list"))
		spantest.HasSpan(t, tel.spans, spantest.WithName("server/discover"))
	})

	t.Run("logs disabled", func(t *testing.T) {
		t.Parallel()

		session, tel := startServer(t, withValues(map[string]any{"mcpserver.log.enabled": false}))

		_, err := session.ListTools(t.Context(), nil)
		require.NoError(t, err)

		spantest.HasSpan(t, tel.spans, spantest.WithName("tools/list"))
		logtest.HasNoRecord(t, tel.logs, logtest.WithBody("mcp request"))
	})
}
