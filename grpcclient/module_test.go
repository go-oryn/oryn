package grpcclient_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/core/coretest"
	"github.com/go-oryn/oryn/core/otel/oteltest/logtest"
	"github.com/go-oryn/oryn/core/otel/oteltest/metrictest"
	"github.com/go-oryn/oryn/core/otel/oteltest/spantest"
	"github.com/go-oryn/oryn/grpcclient"
	"github.com/go-oryn/oryn/grpcclient/grpcclienttest"
	"github.com/go-oryn/oryn/grpcserver"
)

// testService implements the grpc_testing TestService, and records the incoming metadata.
type testService struct {
	testpb.UnimplementedTestServiceServer

	mu       sync.Mutex
	metadata metadata.MD
}

func (s *testService) UnaryCall(ctx context.Context, req *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
	s.mu.Lock()
	s.metadata, _ = metadata.FromIncomingContext(ctx)
	s.mu.Unlock()

	if code := req.GetResponseStatus().GetCode(); code != 0 {
		return nil, status.Error(codes.Code(code), req.GetResponseStatus().GetMessage()) //nolint:gosec // Small codes.
	}

	return &testpb.SimpleResponse{Payload: &testpb.Payload{Body: []byte("test_" + string(req.GetPayload().GetBody()))}}, nil
}

func (s *testService) StreamingOutputCall(
	req *testpb.StreamingOutputCallRequest,
	stream grpc.ServerStreamingServer[testpb.StreamingOutputCallResponse],
) error {
	for range req.GetResponseParameters() {
		if err := stream.Send(&testpb.StreamingOutputCallResponse{Payload: req.GetPayload()}); err != nil {
			return err
		}
	}

	return nil
}

func (s *testService) get(key string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.metadata.Get(key)
}

// newServer returns a plain gRPC server, without instrumentation, serving the test service.
func newServer() (*grpc.Server, *testService) {
	service := &testService{}
	server := grpc.NewServer()
	testpb.RegisterTestServiceServer(server, service)

	return server, service
}

// telemetry is the in-memory telemetry of a test application.
type telemetry struct {
	spans   *tracetest.InMemoryExporter
	logs    *logtest.InMemoryExporter
	metrics *sdkmetric.ManualReader
}

// startClient starts an application calling the in-memory test server on the "test" connection,
// and returns its client.
func startClient(t *testing.T, options ...fx.Option) (testpb.TestServiceClient, *testService, telemetry) {
	t.Helper()

	server, service := newServer()

	var (
		client testpb.TestServiceClient
		tel    telemetry
	)

	coretest.Start(t, core.New(grpcclient.Module, grpcclient.AsClient("test", testpb.NewTestServiceClient)),
		append(options,
			grpcclienttest.Serve("test", server),
			fx.Populate(&client, &tel.spans, &tel.logs, &tel.metrics),
		)...,
	)

	return client, service, tel
}

func withValues(values map[string]any) fx.Option {
	return config.AsOptions(config.WithValues(values))
}

func unaryCall(t *testing.T, client testpb.TestServiceClient, code codes.Code) (*testpb.SimpleResponse, error) {
	t.Helper()

	return client.UnaryCall(t.Context(), &testpb.SimpleRequest{
		Payload:        &testpb.Payload{Body: []byte("body")},
		ResponseStatus: &testpb.EchoStatus{Code: int32(code), Message: "test_error"}, //nolint:gosec // Small codes.
	})
}

// startError returns the error of an application failing to build or start.
func startError(t *testing.T, app core.App, options ...fx.Option) error {
	t.Helper()

	fxApp := fx.New(app.Options(append(options, coretest.TestOptions(), fx.NopLogger)...)...)
	if err := fxApp.Err(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	err := fxApp.Start(ctx)
	if err == nil {
		require.NoError(t, fxApp.Stop(ctx))
	}

	return err
}

func TestFactory(t *testing.T) {
	t.Parallel()

	t.Run("calls are traced, measured and logged", func(t *testing.T) {
		t.Parallel()

		client, _, tel := startClient(t)

		response, err := unaryCall(t, client, codes.OK)
		require.NoError(t, err)
		assert.Equal(t, "test_body", string(response.GetPayload().GetBody()))

		spantest.HasSpan(t, tel.spans,
			spantest.WithName("grpc.testing.TestService/UnaryCall"),
			spantest.WithKind(trace.SpanKindClient),
			spantest.WithAttributeValue(semconv.RPCResponseStatusCode("OK")),
		)
		metrictest.HasMetric(t, tel.metrics, metrictest.WithName("rpc.client.call.duration"))
		logtest.HasRecord(t, tel.logs,
			logtest.WithBody("grpc client request"),
			logtest.WithLevel(slog.LevelInfo),
			logtest.WithAttributeValue("rpc", "/grpc.testing.TestService/UnaryCall"),
			logtest.WithAttributeValue("service", "grpc.testing.TestService"),
			logtest.WithAttributeValue("method", "UnaryCall"),
			logtest.WithAttributeValue("target", "passthrough:///test"),
			logtest.WithAttributeValue("code", "OK"),
		)
	})

	t.Run("errors are logged by severity", func(t *testing.T) {
		t.Parallel()

		client, _, tel := startClient(t)

		_, err := unaryCall(t, client, codes.NotFound)
		assert.Equal(t, codes.NotFound, status.Code(err))

		_, err = unaryCall(t, client, codes.Internal)
		assert.Equal(t, codes.Internal, status.Code(err))

		logtest.HasRecord(t, tel.logs, logtest.WithBody("grpc client request"), logtest.WithLevel(slog.LevelInfo),
			logtest.WithAttributeValue("code", "NotFound"), logtest.WithAttributeValue("error", "test_error"))
		logtest.HasRecord(t, tel.logs, logtest.WithBody("grpc client request"), logtest.WithLevel(slog.LevelError),
			logtest.WithAttributeValue("code", "Internal"))
	})

	t.Run("streaming calls", func(t *testing.T) {
		t.Parallel()

		client, _, tel := startClient(t)

		stream, err := client.StreamingOutputCall(t.Context(), &testpb.StreamingOutputCallRequest{
			Payload:            &testpb.Payload{Body: []byte("body")},
			ResponseParameters: []*testpb.ResponseParameters{{}, {}},
		})
		require.NoError(t, err)

		received := 0

		for {
			_, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				break
			}

			require.NoError(t, err)

			received++
		}

		assert.Equal(t, 2, received)
		spantest.HasSpan(t, tel.spans, spantest.WithName("grpc.testing.TestService/StreamingOutputCall"))
		logtest.HasRecord(t, tel.logs, logtest.WithBody("grpc client request"), logtest.WithAttributeValue("method", "StreamingOutputCall"))
	})

	t.Run("trace context propagated to the server", func(t *testing.T) {
		t.Parallel()

		var (
			client testpb.TestServiceClient
			spans  *tracetest.InMemoryExporter
		)

		coretest.Start(t, core.New(
			grpcserver.Module,
			grpcserver.AsService(&testpb.TestService_ServiceDesc, &testService{}),
			grpcclient.Module,
			grpcclient.AsClient("self", testpb.NewTestServiceClient),
		), grpcclienttest.ServeApp("self"), fx.Populate(&client, &spans))

		_, err := unaryCall(t, client, codes.OK)
		require.NoError(t, err)

		clientSpans := spantest.Find(spans, spantest.WithName("grpc.testing.TestService/UnaryCall"), spantest.WithKind(trace.SpanKindClient))
		require.Len(t, clientSpans, 1)
		spantest.HasSpan(t, spans,
			spantest.WithName("grpc.testing.TestService/UnaryCall"),
			spantest.WithKind(trace.SpanKindServer),
			spantest.WithParent(clientSpans[0].SpanContext.SpanID()),
		)
	})

	t.Run("connections are shared, and closed on stop", func(t *testing.T) {
		t.Parallel()

		var factory *grpcclient.Factory

		app := coretest.New(t, core.New(grpcclient.Module), withValues(map[string]any{
			"grpcclient.connections.test.target":   "localhost:1",
			"grpcclient.connections.test.insecure": true,
		}), fx.Populate(&factory))
		app.RequireStart()

		conn, err := factory.Conn("test")
		require.NoError(t, err)

		same, err := factory.Conn("test")
		require.NoError(t, err)
		assert.Same(t, conn, same)

		app.RequireStop()

		assert.Equal(t, connectivity.Shutdown, conn.GetState())

		_, err = factory.Conn("test")
		require.ErrorIs(t, err, grpcclient.ErrClosed)
	})

	t.Run("connection settings", func(t *testing.T) {
		t.Parallel()

		client, service, _ := startClient(t,
			withValues(map[string]any{
				"grpcclient.connections.test.authority":      "test_authority",
				"grpcclient.connections.test.service_config": `{"loadBalancingConfig": [{"round_robin": {}}]}`,
			}),
			grpcclient.AsDialOptions("test", grpc.WithUserAgent("test_agent")),
		)

		_, err := unaryCall(t, client, codes.OK)
		require.NoError(t, err)

		assert.Equal(t, []string{"test_authority"}, service.get(":authority"))
		require.Len(t, service.get("user-agent"), 1)
		assert.Contains(t, service.get("user-agent")[0], "test_agent")
	})

	t.Run("unreachable server", func(t *testing.T) {
		t.Parallel()

		var (
			factory *grpcclient.Factory
			logs    *logtest.InMemoryExporter
		)

		coretest.Start(t, core.New(grpcclient.Module), withValues(map[string]any{
			// Without insecure: TLS with the system root certificates.
			"grpcclient.connections.test.target": "127.0.0.1:1",
		}), fx.Populate(&factory, &logs))

		conn, err := factory.Conn("test")
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()

		_, err = testpb.NewTestServiceClient(conn).EmptyCall(ctx, &testpb.Empty{})
		require.Error(t, err)

		logtest.HasRecord(t, logs, logtest.WithBody("grpc client request"), logtest.WithLevel(slog.LevelError))
	})

	t.Run("log disabled", func(t *testing.T) {
		t.Parallel()

		client, _, tel := startClient(t, withValues(map[string]any{"grpcclient.log.enabled": false}))

		_, err := unaryCall(t, client, codes.OK)
		require.NoError(t, err)

		spantest.HasSpan(t, tel.spans, spantest.WithName("grpc.testing.TestService/UnaryCall"))
		logtest.HasNoRecord(t, tel.logs, logtest.WithBody("grpc client request"))
	})

	t.Run("excluded methods", func(t *testing.T) {
		t.Parallel()

		client, _, tel := startClient(t, withValues(map[string]any{
			"grpcclient.observability.exclude": []string{
				"/grpc.testing.TestService/UnaryCall",
				"/grpc.testing.TestService/StreamingOutputCall",
			},
		}))

		_, err := unaryCall(t, client, codes.OK)
		require.NoError(t, err)

		stream, err := client.StreamingOutputCall(t.Context(), &testpb.StreamingOutputCallRequest{})
		require.NoError(t, err)

		_, err = stream.Recv()
		require.ErrorIs(t, err, io.EOF)

		spantest.HasNoSpan(t, tel.spans, spantest.WithName("grpc.testing.TestService/UnaryCall"))
		spantest.HasNoSpan(t, tel.spans, spantest.WithName("grpc.testing.TestService/StreamingOutputCall"))
		logtest.HasNoRecord(t, tel.logs, logtest.WithBody("grpc client request"))
	})

	t.Run("errors", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct {
			values map[string]any
			err    string
		}{
			"missing target": {
				err: `cannot create grpc connection "test": missing grpcclient.connections.test.target`,
			},
			"invalid service config": {
				values: map[string]any{
					"grpcclient.connections.test.target":         "localhost:1",
					"grpcclient.connections.test.service_config": "{invalid",
				},
				err: `cannot create grpc connection "test"`,
			},
		} {
			err := startError(t, core.New(grpcclient.Module, grpcclient.AsClient("test", testpb.NewTestServiceClient)),
				withValues(tc.values), fx.Invoke(func(testpb.TestServiceClient) {}))
			require.ErrorContains(t, err, tc.err, name)
		}
	})
}
