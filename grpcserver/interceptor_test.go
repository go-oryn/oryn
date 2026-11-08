package grpcserver_test

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	grpccodes "google.golang.org/grpc/codes"
	testpb "google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/status"

	"github.com/go-oryn/oryn/core/otel/oteltest/logtest"
	"github.com/go-oryn/oryn/core/otel/oteltest/spantest"
)

func TestRecoverInterceptor(t *testing.T) {
	t.Parallel()

	conn, tel := startServer(t)

	_, err := unaryCall(t, conn, "panic")
	assert.Equal(t, grpccodes.Internal, status.Code(err))

	_, err = streamCall(t, conn, "panic", 1)
	assert.Equal(t, grpccodes.Internal, status.Code(err))

	for _, method := range []string{"UnaryCall", "StreamingOutputCall"} {
		rpc := "/grpc.testing.TestService/" + method

		logtest.HasRecord(t, tel.logs,
			logtest.WithBody("grpc handler panic"),
			logtest.WithLevel(slog.LevelError),
			logtest.WithAttributeValue("rpc", rpc),
			logtest.WithAttributeValue("panic", "test_panic"),
			logtest.WithAttribute("stack"),
		)
		logtest.HasRecord(t, tel.logs,
			logtest.WithBody("grpc request"),
			logtest.WithLevel(slog.LevelError),
			logtest.WithAttributeValue("rpc", rpc),
			logtest.WithAttributeValue("code", "Internal"),
		)
		spantest.HasSpan(t, tel.spans,
			spantest.WithName("grpc.testing.TestService/"+method),
			spantest.WithStatus(codes.Error),
		)
	}
}

func TestLogInterceptor(t *testing.T) {
	t.Parallel()

	call := func(t *testing.T, client testpb.TestServiceClient, code grpccodes.Code) {
		t.Helper()

		_, err := client.UnaryCall(t.Context(), &testpb.SimpleRequest{
			ResponseStatus: &testpb.EchoStatus{Code: int32(code), Message: "test_message"}, //nolint:gosec // Test codes are small.
		})
		require.Equal(t, code, status.Code(err))
	}

	t.Run("levels", func(t *testing.T) {
		t.Parallel()

		conn, tel := startServer(t)
		client := testpb.NewTestServiceClient(conn)

		call(t, client, grpccodes.NotFound)
		call(t, client, grpccodes.Unavailable)

		logtest.HasRecord(t, tel.logs,
			logtest.WithBody("grpc request"),
			logtest.WithLevel(slog.LevelInfo),
			logtest.WithAttributeValue("code", "NotFound"),
			logtest.WithAttributeValue("error", "test_message"),
		)
		logtest.HasRecord(t, tel.logs,
			logtest.WithBody("grpc request"),
			logtest.WithLevel(slog.LevelError),
			logtest.WithAttributeValue("code", "Unavailable"),
		)
		spantest.HasSpan(t, tel.spans,
			spantest.WithName("grpc.testing.TestService/UnaryCall"),
			spantest.WithAttributeValue(semconv.RPCResponseStatusCode("NOT_FOUND")),
		)
	})

	t.Run("disabled", func(t *testing.T) {
		t.Parallel()

		conn, tel := startServer(t, withValues(map[string]any{"grpcserver.log.enabled": false}))

		_, err := unaryCall(t, conn, "value")
		require.NoError(t, err)
		_, err = unaryCall(t, conn, "panic")
		require.Error(t, err)

		logtest.HasNoRecord(t, tel.logs, logtest.WithBody("grpc request"))
		logtest.HasRecord(t, tel.logs, logtest.WithBody("grpc handler panic"))
		spantest.HasSpan(t, tel.spans, spantest.WithName("grpc.testing.TestService/UnaryCall"))
	})
}
