package grpcserver_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	reflectionpb "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/grpc/status"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/core/coretest"
	"github.com/go-oryn/oryn/core/otel/oteltest/logtest"
	"github.com/go-oryn/oryn/core/otel/oteltest/metrictest"
	"github.com/go-oryn/oryn/core/otel/oteltest/spantest"
	"github.com/go-oryn/oryn/grpcserver"
	"github.com/go-oryn/oryn/grpcserver/grpcservertest"
)

// testService implements the grpc_testing TestService. A "panic" payload panics, and a
// response status is returned as an error.
type testService struct {
	testpb.UnimplementedTestServiceServer

	prefix string
}

func newTestService() *testService {
	return &testService{prefix: "test_"}
}

func (s *testService) UnaryCall(_ context.Context, req *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
	if err := s.handle(req.GetPayload(), req.GetResponseStatus()); err != nil {
		return nil, err
	}

	return &testpb.SimpleResponse{Payload: &testpb.Payload{Body: []byte(s.prefix + string(req.GetPayload().GetBody()))}}, nil
}

func (s *testService) StreamingOutputCall(
	req *testpb.StreamingOutputCallRequest,
	stream grpc.ServerStreamingServer[testpb.StreamingOutputCallResponse],
) error {
	if err := s.handle(req.GetPayload(), req.GetResponseStatus()); err != nil {
		return err
	}

	for range req.GetResponseParameters() {
		if err := stream.Send(&testpb.StreamingOutputCallResponse{Payload: req.GetPayload()}); err != nil {
			return err
		}
	}

	return nil
}

// FullDuplexCall echoes the requests until the client closes the stream.
func (s *testService) FullDuplexCall(stream grpc.BidiStreamingServer[testpb.StreamingOutputCallRequest, testpb.StreamingOutputCallResponse]) error {
	for {
		req, err := stream.Recv()
		if err != nil {
			return nil //nolint:nilerr // The client closed the stream.
		}

		if err := stream.Send(&testpb.StreamingOutputCallResponse{Payload: req.GetPayload()}); err != nil {
			return err
		}
	}
}

func (s *testService) handle(payload *testpb.Payload, echo *testpb.EchoStatus) error {
	if string(payload.GetBody()) == "panic" {
		panic("test_panic")
	}

	if echo.GetCode() != 0 {
		return status.Error(codes.Code(echo.GetCode()), echo.GetMessage()) //nolint:gosec // Test codes are small.
	}

	return nil
}

// telemetry holds the in memory telemetry of a test application.
type telemetry struct {
	spans   *tracetest.InMemoryExporter
	logs    *logtest.InMemoryExporter
	metrics *sdkmetric.ManualReader
}

// startServer starts a test application with the grpcserver module, the test service and the
// options, and returns a client connection to its server, and its telemetry.
func startServer(t *testing.T, options ...fx.Option) (*grpc.ClientConn, telemetry) {
	t.Helper()

	return startApp(t, append(options, grpcserver.AsService(&testpb.TestService_ServiceDesc, newTestService))...)
}

// startApp starts a test application with the grpcserver module and the options, and returns a
// client connection to its server, and its telemetry.
func startApp(t *testing.T, options ...fx.Option) (*grpc.ClientConn, telemetry) {
	t.Helper()

	var (
		server *grpc.Server
		tel    telemetry
	)

	coretest.Start(t, core.New(grpcserver.Module), append(options, fx.Populate(&server, &tel.spans, &tel.logs, &tel.metrics))...)

	return grpcservertest.Dial(t, server), tel
}

// newServerError returns the error of an application failing to build the server.
func newServerError(t *testing.T, options ...fx.Option) error {
	t.Helper()

	app := fx.New(core.New(grpcserver.Module).Options(append(options,
		coretest.TestOptions(),
		fx.NopLogger,
		fx.Invoke(func(*grpc.Server) {}),
	)...)...)

	return app.Err()
}

// withValues sets config values.
func withValues(values map[string]any) fx.Option {
	return config.AsOptions(config.WithValues(values))
}

func unaryCall(t *testing.T, conn *grpc.ClientConn, body string) (*testpb.SimpleResponse, error) {
	t.Helper()

	return testpb.NewTestServiceClient(conn).UnaryCall(t.Context(), &testpb.SimpleRequest{Payload: &testpb.Payload{Body: []byte(body)}})
}

// streamCall runs a server streaming call returning n responses, and returns the number of
// responses received and the call error.
func streamCall(t *testing.T, conn *grpc.ClientConn, body string, n int) (int, error) {
	t.Helper()

	parameters := make([]*testpb.ResponseParameters, n)
	for i := range parameters {
		parameters[i] = &testpb.ResponseParameters{}
	}

	stream, err := testpb.NewTestServiceClient(conn).StreamingOutputCall(t.Context(), &testpb.StreamingOutputCallRequest{
		ResponseParameters: parameters,
		Payload:            &testpb.Payload{Body: []byte(body)},
	})
	require.NoError(t, err)

	received := 0

	for {
		_, err := stream.Recv()
		if err == io.EOF { //nolint:errorlint // gRPC returns io.EOF as is.
			return received, nil
		}

		if err != nil {
			return received, err
		}

		received++
	}
}

func TestNewServer(t *testing.T) {
	t.Parallel()

	t.Run("unary calls are traced, measured and logged", func(t *testing.T) {
		t.Parallel()

		conn, tel := startServer(t)

		resp, err := unaryCall(t, conn, "value")
		require.NoError(t, err)
		assert.Equal(t, "test_value", string(resp.GetPayload().GetBody()))

		spantest.HasSpan(t, tel.spans,
			spantest.WithName("grpc.testing.TestService/UnaryCall"),
			spantest.WithAttributeValue(semconv.RPCMethod("grpc.testing.TestService/UnaryCall")),
			spantest.WithAttributeValue(semconv.RPCSystemNameGRPC),
		)
		metrictest.HasMetric(t, tel.metrics, metrictest.WithName("rpc.server.call.duration"))
		logtest.HasRecord(t, tel.logs,
			logtest.WithBody("grpc request"),
			logtest.WithAttributeValue("rpc", "/grpc.testing.TestService/UnaryCall"),
			logtest.WithAttributeValue("service", "grpc.testing.TestService"),
			logtest.WithAttributeValue("method", "UnaryCall"),
			logtest.WithAttributeValue("code", "OK"),
			logtest.WithAttribute("duration"),
		)
	})

	t.Run("streaming calls are traced and logged", func(t *testing.T) {
		t.Parallel()

		conn, tel := startServer(t)

		received, err := streamCall(t, conn, "value", 3)
		require.NoError(t, err)
		assert.Equal(t, 3, received)

		spantest.HasSpan(t, tel.spans, spantest.WithName("grpc.testing.TestService/StreamingOutputCall"))
		logtest.HasRecord(t, tel.logs,
			logtest.WithBody("grpc request"),
			logtest.WithAttributeValue("method", "StreamingOutputCall"),
			logtest.WithAttributeValue("code", "OK"),
		)
	})

	t.Run("message sizes", func(t *testing.T) {
		t.Parallel()

		conn, _ := startServer(t, withValues(map[string]any{
			"grpcserver.max_recv_msg_size": 128,
			"grpcserver.max_send_msg_size": 96,
		}))

		_, err := unaryCall(t, conn, "small")
		require.NoError(t, err)

		_, err = unaryCall(t, conn, string(make([]byte, 200)))
		assert.Equal(t, codes.ResourceExhausted, status.Code(err))

		// The response gets the prefix: received but too large to send.
		_, err = unaryCall(t, conn, string(make([]byte, 90)))
		assert.Equal(t, codes.ResourceExhausted, status.Code(err))
	})

	t.Run("reflection", func(t *testing.T) {
		t.Parallel()

		list := func(conn *grpc.ClientConn) ([]string, error) {
			stream, err := reflectionpb.NewServerReflectionClient(conn).ServerReflectionInfo(t.Context())
			require.NoError(t, err)

			// On failure, Send returns io.EOF: the status comes from Recv.
			_ = stream.Send(&reflectionpb.ServerReflectionRequest{
				MessageRequest: &reflectionpb.ServerReflectionRequest_ListServices{},
			})

			resp, err := stream.Recv()
			if err != nil {
				return nil, err
			}

			var names []string
			for _, s := range resp.GetListServicesResponse().GetService() {
				names = append(names, s.GetName())
			}

			return names, nil
		}

		disabled, _ := startServer(t)
		_, err := list(disabled)
		assert.Equal(t, codes.Unimplemented, status.Code(err))

		enabled, _ := startServer(t, withValues(map[string]any{"grpcserver.reflection.enabled": true}))
		names, err := list(enabled)
		require.NoError(t, err)
		assert.Contains(t, names, "grpc.testing.TestService")
		assert.Contains(t, names, "grpc.health.v1.Health")
	})

	t.Run("excluded methods", func(t *testing.T) {
		t.Parallel()

		// By default, the health checks are excluded.
		conn, tel := startServer(t)

		_, err := healthpb.NewHealthClient(conn).Check(t.Context(), &healthpb.HealthCheckRequest{})
		require.NoError(t, err)

		spantest.HasNoSpan(t, tel.spans, spantest.WithNameContains("Health"))
		logtest.HasNoRecord(t, tel.logs, logtest.WithAttributeValue("service", "grpc.health.v1.Health"))

		// A configured list replaces the default one.
		conn, tel = startServer(t, withValues(map[string]any{
			"grpcserver.observability.exclude": []string{"/grpc.testing.TestService/UnaryCall", "/grpc.testing.TestService/StreamingOutputCall"},
		}))

		_, err = unaryCall(t, conn, "value")
		require.NoError(t, err)
		_, err = streamCall(t, conn, "value", 1)
		require.NoError(t, err)
		_, err = healthpb.NewHealthClient(conn).Check(t.Context(), &healthpb.HealthCheckRequest{})
		require.NoError(t, err)

		spantest.HasNoSpan(t, tel.spans, spantest.WithNameContains("TestService"))
		logtest.HasNoRecord(t, tel.logs, logtest.WithAttributeValue("service", "grpc.testing.TestService"))
		spantest.HasSpan(t, tel.spans, spantest.WithName("grpc.health.v1.Health/Check"))
	})

	t.Run("registration errors", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct {
			options []fx.Option
			err     string
		}{
			"duplicate service": {
				options: []fx.Option{
					grpcserver.AsService(&testpb.TestService_ServiceDesc, newTestService),
					grpcserver.AsService(&testpb.TestService_ServiceDesc, newTestService()),
				},
				err: "cannot register service grpc.testing.TestService: already registered",
			},
			"health service": {
				options: []fx.Option{grpcserver.AsService(&healthpb.Health_ServiceDesc, &healthpb.UnimplementedHealthServer{})},
				err:     "cannot register service grpc.health.v1.Health: already registered",
			},
			"reflection service": {
				options: []fx.Option{
					withValues(map[string]any{"grpcserver.reflection.enabled": true}),
					grpcserver.AsService(&reflectionpb.ServerReflection_ServiceDesc, &reflectionpb.UnimplementedServerReflectionServer{}),
				},
				err: "cannot register the server reflection: grpc.reflection.v1.ServerReflection already registered",
			},
		} {
			require.ErrorContains(t, newServerError(t, tc.options...), tc.err, name)
		}
	})

	t.Run("own health service", func(t *testing.T) {
		t.Parallel()

		var server *grpc.Server

		coretest.Start(t, core.New(grpcserver.Module, grpcserver.AsService(&healthpb.Health_ServiceDesc, &healthpb.UnimplementedHealthServer{})),
			withValues(map[string]any{"grpcserver.health.enabled": false}),
			fx.Populate(&server),
		)

		_, err := healthpb.NewHealthClient(grpcservertest.Dial(t, server)).Check(t.Context(), &healthpb.HealthCheckRequest{})
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})
}

func TestServe(t *testing.T) {
	t.Parallel()

	// start starts a test application serving the test service on a random port.
	start := func(t *testing.T, options ...fx.Option) (*grpc.ClientConn, *grpcserver.Address, *fxtest.App) {
		t.Helper()

		var address *grpcserver.Address

		app := coretest.New(t, core.New(grpcserver.Module, grpcserver.AsService(&testpb.TestService_ServiceDesc, newTestService)),
			append(options,
				withValues(map[string]any{"grpcserver.address": "127.0.0.1:0"}),
				grpcserver.Serve(),
				fx.Populate(&address),
			)...,
		)
		app.RequireStart()

		conn, err := grpc.NewClient(address.String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
		require.NoError(t, err)
		t.Cleanup(func() { _ = conn.Close() })

		return conn, address, app
	}

	t.Run("serves over the network", func(t *testing.T) {
		t.Parallel()

		conn, address, app := start(t)

		assert.NotEqual(t, "127.0.0.1:0", address.String())

		resp, err := unaryCall(t, conn, "value")
		require.NoError(t, err)
		assert.Equal(t, "test_value", string(resp.GetPayload().GetBody()))

		require.NoError(t, app.Stop(t.Context()))

		_, err = unaryCall(t, conn, "value")
		assert.Equal(t, codes.Unavailable, status.Code(err))
	})

	t.Run("stops in-flight calls when the stop times out", func(t *testing.T) {
		t.Parallel()

		conn, _, app := start(t)

		// A bidi stream stays open until the client closes it.
		stream, err := testpb.NewTestServiceClient(conn).FullDuplexCall(t.Context())
		require.NoError(t, err)
		require.NoError(t, stream.Send(&testpb.StreamingOutputCallRequest{}))
		_, err = stream.Recv()
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()

		// The stop hook forces the stop once the context is done, which fx reports.
		require.ErrorIs(t, app.Stop(ctx), context.DeadlineExceeded)

		_, err = stream.Recv()
		assert.Error(t, err)
	})

	t.Run("listen error", func(t *testing.T) {
		t.Parallel()

		app := coretest.New(t, core.New(grpcserver.Module),
			withValues(map[string]any{"grpcserver.address": "invalid_address"}),
			grpcserver.Serve(),
		)

		require.ErrorContains(t, app.Start(t.Context()), "cannot start grpc server on invalid_address")
	})

	t.Run("server failure shuts the application down", func(t *testing.T) {
		t.Parallel()

		var logs *logtest.InMemoryExporter

		// Stopping the server before it serves makes it fail.
		app := coretest.New(t, core.New(grpcserver.Module),
			withValues(map[string]any{"grpcserver.address": "127.0.0.1:0"}),
			fx.Invoke(func(lc fx.Lifecycle, server *grpc.Server) { lc.Append(fx.StartHook(server.Stop)) }),
			grpcserver.Serve(),
			fx.Populate(&logs),
		)
		app.RequireStart()
		t.Cleanup(app.RequireStop)

		select {
		case signal := <-app.Wait():
			assert.Equal(t, 1, signal.ExitCode)
		case <-time.After(5 * time.Second):
			t.Fatal("the application was not shut down")
		}

		logtest.HasRecord(t, logs, logtest.WithBody("grpc server stopped"))
	})
}

func TestAddress(t *testing.T) {
	t.Parallel()

	var address *grpcserver.Address

	coretest.Start(t, core.New(grpcserver.Module), fx.Populate(&address))

	assert.Equal(t, grpcserver.DefaultAddress, address.String())
}
