package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

// InstrumentationScope is the name of the tracer and meter of the MCP server instrumentation.
const InstrumentationScope = "github.com/go-oryn/oryn/mcpserver"

// Attribute keys of the [MCP semantic conventions].
//
// [MCP semantic conventions]: https://opentelemetry.io/docs/specs/semconv/gen-ai/mcp/
const (
	MCPMethodNameKey      = attribute.Key("mcp.method.name")
	MCPSessionIDKey       = attribute.Key("mcp.session.id")
	MCPResourceURIKey     = attribute.Key("mcp.resource.uri")
	GenAIOperationNameKey = attribute.Key("gen_ai.operation.name")
	GenAIToolNameKey      = attribute.Key("gen_ai.tool.name")
	GenAIPromptNameKey    = attribute.Key("gen_ai.prompt.name")
)

// MetricOperationDuration is the histogram of the duration of the MCP server operations, in seconds.
const MetricOperationDuration = "mcp.server.operation.duration"

// ErrorTypeToolError is the error.type of a tool call returning a result with isError set.
const ErrorTypeToolError = "tool_error"

// operationDurationBuckets are the bucket boundaries advised by the MCP semantic conventions.
var operationDurationBuckets = []float64{0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1, 2, 5, 10, 30, 60, 120, 300}

// operation describes a received MCP request.
type operation struct {
	method     string
	target     string
	sessionID  string
	attributes []attribute.KeyValue
}

// newOperation returns the operation of a request: its method, target (tool or prompt name,
// resource URI) and span attributes.
func newOperation(method string, req mcp.Request) operation {
	op := operation{method: method, attributes: []attribute.KeyValue{MCPMethodNameKey.String(method)}}

	if session := req.GetSession(); !isNil(session) {
		op.sessionID = session.ID()
	}

	if op.sessionID != "" {
		op.attributes = append(op.attributes, MCPSessionIDKey.String(op.sessionID))
	}

	switch r := req.(type) {
	case *mcp.CallToolRequest:
		op.target = r.Params.Name
		op.attributes = append(op.attributes, GenAIOperationNameKey.String("execute_tool"), GenAIToolNameKey.String(op.target))
	case *mcp.GetPromptRequest:
		op.target = r.Params.Name
		op.attributes = append(op.attributes, GenAIPromptNameKey.String(op.target))
	case *mcp.ReadResourceRequest:
		op.target = r.Params.URI
		op.attributes = append(op.attributes, MCPResourceURIKey.String(op.target))
	}

	return op
}

// name returns the span name: "{mcp.method.name} {target}", or just the method without target.
func (op operation) name() string {
	if op.target == "" {
		return op.method
	}

	return op.method + " " + op.target
}

// outcome returns the error.type and description of a failed operation, or "" when it succeeded:
// the JSON-RPC error code (or _OTHER) for a protocol error, tool_error for a tool error result.
func outcome(result mcp.Result, err error) (errorType string, description string) {
	if err != nil {
		var rpcErr *jsonrpc.Error
		if errors.As(err, &rpcErr) {
			return strconv.FormatInt(rpcErr.Code, 10), err.Error()
		}

		return semconv.ErrorTypeOther.Value.AsString(), err.Error()
	}

	if toolResult, ok := result.(*mcp.CallToolResult); ok && toolResult != nil && toolResult.IsError {
		for _, content := range toolResult.Content {
			if text, ok := content.(*mcp.TextContent); ok {
				return ErrorTypeToolError, text.Text
			}
		}

		return ErrorTypeToolError, "tool error"
	}

	return "", ""
}

// newOTelMiddleware traces and measures the operations that are not excluded. The trace context
// is extracted from the request _meta when present, so the span continues the trace of the client.
func newOTelMiddleware(
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	propagator propagation.TextMapPropagator,
	exclude map[string]bool,
) (mcp.Middleware, error) {
	tracer := tracerProvider.Tracer(InstrumentationScope)

	duration, err := meterProvider.Meter(InstrumentationScope).Float64Histogram(MetricOperationDuration,
		metric.WithDescription("Duration of the MCP server operations."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(operationDurationBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("cannot create the %s metric: %w", MetricOperationDuration, err)
	}

	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if exclude[method] {
				return next(ctx, method, req)
			}

			op := newOperation(method, req)
			ctx = parentContext(ctx, propagator, req)

			ctx, span := tracer.Start(ctx, op.name(), trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(op.attributes...))
			defer span.End()

			start := time.Now()
			result, err := next(ctx, method, req)
			elapsed := time.Since(start)

			metricAttributes := []attribute.KeyValue{MCPMethodNameKey.String(method)}
			if req, ok := req.(*mcp.CallToolRequest); ok {
				metricAttributes = append(metricAttributes, GenAIToolNameKey.String(req.Params.Name))
			}

			if req, ok := req.(*mcp.GetPromptRequest); ok {
				metricAttributes = append(metricAttributes, GenAIPromptNameKey.String(req.Params.Name))
			}

			if errorType, description := outcome(result, err); errorType != "" {
				span.SetAttributes(semconv.ErrorTypeKey.String(errorType))
				span.SetStatus(codes.Error, description)

				if err != nil {
					span.RecordError(err)
				}

				metricAttributes = append(metricAttributes, semconv.ErrorTypeKey.String(errorType))
			}

			duration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(metricAttributes...))

			return result, err
		}
	}, nil
}

// newLogMiddleware logs the operations that are not excluded, at error level for errors and
// tool errors, and info level otherwise.
func newLogMiddleware(logger *slog.Logger, exclude map[string]bool) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if exclude[method] {
				return next(ctx, method, req)
			}

			start := time.Now()
			result, err := next(ctx, method, req)
			elapsed := time.Since(start)

			op := newOperation(method, req)
			attributes := []slog.Attr{slog.String("method", method)}

			if op.target != "" {
				attributes = append(attributes, slog.String("target", op.target))
			}

			if op.sessionID != "" {
				attributes = append(attributes, slog.String("session_id", op.sessionID))
			}

			attributes = append(attributes, slog.Duration("duration", elapsed))
			level := slog.LevelInfo

			if errorType, description := outcome(result, err); errorType != "" {
				level = slog.LevelError
				attributes = append(attributes, slog.String("error_type", errorType), slog.String("error", description))
			}

			logger.LogAttrs(ctx, level, "mcp request", attributes...)

			return result, err
		}
	}
}

// newRecoverMiddleware turns a panic of a handler into an error, and logs it with its stack.
func newRecoverMiddleware(logger *slog.Logger) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (result mcp.Result, err error) {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.ErrorContext(ctx, "mcp handler panic", "method", method, "panic", recovered, "stack", string(debug.Stack()))

					result, err = nil, fmt.Errorf("mcp handler panic: %v", recovered)
				}
			}()

			return next(ctx, method, req)
		}
	}
}

// excludedMethods returns the set of the MCP methods excluded from the observability.
func excludedMethods(methods []string) map[string]bool {
	exclude := make(map[string]bool, len(methods))
	for _, method := range methods {
		exclude[method] = true
	}

	return exclude
}

// parentContext returns the context holding the parent of the operation span. The handlers run in
// the context of the session, whose span (if any) is not the parent: it is replaced with the trace
// context of the HTTP request headers (the HTTP server span), then of the request _meta, where MCP
// clients put their trace context.
func parentContext(ctx context.Context, propagator propagation.TextMapPropagator, req mcp.Request) context.Context {
	ctx = trace.ContextWithSpanContext(ctx, trace.SpanContext{})

	if extra := req.GetExtra(); extra != nil && extra.Header != nil {
		ctx = propagator.Extract(ctx, propagation.HeaderCarrier(extra.Header))
	}

	return propagator.Extract(ctx, metaCarrier(req))
}

// metaCarrier returns the string values of the request _meta, where MCP clients put the trace
// context (traceparent, tracestate, baggage).
func metaCarrier(req mcp.Request) propagation.MapCarrier {
	carrier := propagation.MapCarrier{}

	params := req.GetParams()
	if isNil(params) {
		return carrier
	}

	for key, value := range params.GetMeta() {
		if text, ok := value.(string); ok {
			carrier[key] = text
		}
	}

	return carrier
}

// isNil reports whether the interface is nil, or holds a nil pointer.
func isNil(value any) bool {
	if value == nil {
		return true
	}

	v := reflect.ValueOf(value)

	return v.Kind() == reflect.Pointer && v.IsNil()
}
