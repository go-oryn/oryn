package mcpclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"reflect"
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

// InstrumentationScope is the name of the tracer and meter of the MCP client instrumentation.
const InstrumentationScope = "github.com/go-oryn/oryn/mcpclient"

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

// MetricOperationDuration is the histogram of the duration of the MCP client operations, in seconds.
const MetricOperationDuration = "mcp.client.operation.duration"

// ErrorTypeToolError is the error.type of a tool call returning a result with isError set.
const ErrorTypeToolError = "tool_error"

// operationDurationBuckets are the bucket boundaries advised by the MCP semantic conventions.
var operationDurationBuckets = []float64{0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1, 2, 5, 10, 30, 60, 120, 300}

// operation describes a sent MCP request.
type operation struct {
	method     string
	target     string
	attributes []attribute.KeyValue
}

// newOperation returns the operation of a request: its method, target (tool or prompt name,
// resource URI) and span attributes.
func newOperation(method string, req mcp.Request) operation {
	op := operation{method: method, attributes: []attribute.KeyValue{MCPMethodNameKey.String(method)}}

	switch params := req.GetParams().(type) {
	case *mcp.CallToolParams:
		op.target = params.Name
		op.attributes = append(op.attributes, GenAIOperationNameKey.String("execute_tool"), GenAIToolNameKey.String(op.target))
	case *mcp.GetPromptParams:
		op.target = params.Name
		op.attributes = append(op.attributes, GenAIPromptNameKey.String(op.target))
	case *mcp.ReadResourceParams:
		op.target = params.URI
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

// sessionID returns the ID of the session of the request, empty before it is initialized.
func sessionID(req mcp.Request) string {
	if session := req.GetSession(); !isNil(session) {
		return session.ID()
	}

	return ""
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

// instrumentation holds the tracer and the metric of the client, shared by its sessions.
type instrumentation struct {
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
	duration   metric.Float64Histogram
	exclude    map[string]bool
}

// newInstrumentation returns the instrumentation of the client.
func newInstrumentation(
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	propagator propagation.TextMapPropagator,
	exclude map[string]bool,
) (*instrumentation, error) {
	duration, err := meterProvider.Meter(InstrumentationScope).Float64Histogram(MetricOperationDuration,
		metric.WithDescription("Duration of the MCP client operations."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(operationDurationBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("cannot create the %s metric: %w", MetricOperationDuration, err)
	}

	return &instrumentation{
		tracer:     tracerProvider.Tracer(InstrumentationScope),
		propagator: propagator,
		duration:   duration,
		exclude:    exclude,
	}, nil
}

// middleware traces and measures the operations sent to a server that are not excluded, and puts
// the trace context in the request _meta, so the server span continues the trace.
func (i *instrumentation) middleware(serverAttributes []attribute.KeyValue) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if i.exclude[method] {
				return next(ctx, method, req)
			}

			op := newOperation(method, req)

			ctx, span := i.tracer.Start(ctx, op.name(),
				trace.WithSpanKind(trace.SpanKindClient),
				trace.WithAttributes(op.attributes...),
				trace.WithAttributes(serverAttributes...),
			)
			defer span.End()

			i.inject(ctx, req)

			start := time.Now()
			result, err := next(ctx, method, req)
			elapsed := time.Since(start)

			if id := sessionID(req); id != "" {
				span.SetAttributes(MCPSessionIDKey.String(id))
			}

			metricAttributes := []attribute.KeyValue{MCPMethodNameKey.String(method)}

			switch params := req.GetParams().(type) {
			case *mcp.CallToolParams:
				metricAttributes = append(metricAttributes, GenAIToolNameKey.String(params.Name))
			case *mcp.GetPromptParams:
				metricAttributes = append(metricAttributes, GenAIPromptNameKey.String(params.Name))
			}

			if errorType, description := outcome(result, err); errorType != "" {
				span.SetAttributes(semconv.ErrorTypeKey.String(errorType))
				span.SetStatus(codes.Error, description)

				if err != nil {
					span.RecordError(err)
				}

				metricAttributes = append(metricAttributes, semconv.ErrorTypeKey.String(errorType))
			}

			i.duration.Record(ctx, elapsed.Seconds(), metric.WithAttributes(append(metricAttributes, serverAttributes...)...))

			return result, err
		}
	}
}

// inject puts the trace context (traceparent, tracestate, baggage) in the request _meta.
func (i *instrumentation) inject(ctx context.Context, req mcp.Request) {
	params := req.GetParams()
	if isNil(params) {
		return
	}

	carrier := propagation.MapCarrier{}
	i.propagator.Inject(ctx, carrier)

	if len(carrier) == 0 {
		return
	}

	meta := maps.Clone(params.GetMeta())
	if meta == nil {
		meta = map[string]any{}
	}

	for key, value := range carrier {
		meta[key] = value
	}

	params.SetMeta(meta)
}

// newLogMiddleware logs the operations sent to the server name that are not excluded, at error
// level for errors and tool errors, and info level otherwise.
func newLogMiddleware(logger *slog.Logger, server string, exclude map[string]bool) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if exclude[method] {
				return next(ctx, method, req)
			}

			start := time.Now()
			result, err := next(ctx, method, req)
			elapsed := time.Since(start)

			op := newOperation(method, req)
			attributes := []slog.Attr{slog.String("server", server), slog.String("method", method)}

			if op.target != "" {
				attributes = append(attributes, slog.String("target", op.target))
			}

			if id := sessionID(req); id != "" {
				attributes = append(attributes, slog.String("session_id", id))
			}

			attributes = append(attributes, slog.Duration("duration", elapsed))
			level := slog.LevelInfo

			if errorType, description := outcome(result, err); errorType != "" {
				level = slog.LevelError
				attributes = append(attributes, slog.String("error_type", errorType), slog.String("error", description))
			}

			logger.LogAttrs(ctx, level, "mcp client request", attributes...)

			return result, err
		}
	}
}

// newTimeoutMiddleware bounds the duration of every request sent to the server.
func newTimeoutMiddleware(timeout time.Duration) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

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

// isNil reports whether the interface is nil, or holds a nil pointer.
func isNil(value any) bool {
	if value == nil {
		return true
	}

	v := reflect.ValueOf(value)

	return v.Kind() == reflect.Pointer && v.IsNil()
}
