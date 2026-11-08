package grpcserver_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"google.golang.org/grpc"
	testpb "google.golang.org/grpc/interop/grpc_testing"

	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/grpcserver"
)

// recorder records the calls of the interceptors.
type recorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *recorder) record(call string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls = append(r.calls, call)
}

func (r *recorder) get() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.calls...)
}

func unary(r *recorder, name string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		r.record(name)

		return handler(ctx, req)
	}
}

func stream(r *recorder, name string) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		r.record(name)

		return handler(srv, ss)
	}
}

func TestAsService(t *testing.T) {
	t.Parallel()

	t.Run("value", func(t *testing.T) {
		t.Parallel()

		var server *grpc.Server

		conn, _ := startServer(t, fx.Populate(&server))
		_, err := unaryCall(t, conn, "value")
		require.NoError(t, err)

		assert.Contains(t, server.GetServiceInfo(), "grpc.testing.TestService")
	})

	t.Run("constructor with dependencies", func(t *testing.T) {
		t.Parallel()

		newService := func(cfg *config.Config) testpb.TestServiceServer {
			return &testService{prefix: cfg.GetString("test.prefix")}
		}

		conn, _ := startApp(t,
			grpcserver.AsService(&testpb.TestService_ServiceDesc, newService),
			withValues(map[string]any{"test.prefix": "configured_"}),
		)

		resp, err := unaryCall(t, conn, "value")
		require.NoError(t, err)
		assert.Equal(t, "configured_value", string(resp.GetPayload().GetBody()))
	})

	t.Run("errors", func(t *testing.T) {
		t.Parallel()

		invalidDesc := testpb.TestService_ServiceDesc
		invalidDesc.HandlerType = "invalid"

		for name, tc := range map[string]struct {
			option fx.Option
			err    string
		}{
			"nil description":      {option: grpcserver.AsService(nil, newTestService), err: "nil service description"},
			"invalid handler type": {option: grpcserver.AsService(&invalidDesc, newTestService), err: "invalid handler type string"},
			"nil implementation":   {option: grpcserver.AsService(&testpb.TestService_ServiceDesc, nil), err: "nil service or interceptor"},
			"wrong implementation": {option: grpcserver.AsService(&testpb.TestService_ServiceDesc, "invalid"), err: "unsupported string"},
			"wrong constructor":    {option: grpcserver.AsService(&testpb.TestService_ServiceDesc, func() string { return "" }), err: "unsupported func() string"},
		} {
			require.ErrorContains(t, newServerError(t, tc.option), tc.err, name)
		}
	})
}

func TestAsInterceptor(t *testing.T) {
	t.Parallel()

	t.Run("applied in registration order", func(t *testing.T) {
		t.Parallel()

		rec := &recorder{}

		conn, _ := startServer(t,
			fx.Supply(rec),
			grpcserver.AsUnaryInterceptor(unary(rec, "unary_1"), func(r *recorder, _ *slog.Logger) grpc.UnaryServerInterceptor {
				return unary(r, "unary_2")
			}),
			grpcserver.AsUnaryInterceptor(unary(rec, "unary_3")),
			grpcserver.AsStreamInterceptor(stream(rec, "stream_1"), func(r *recorder) grpc.StreamServerInterceptor {
				return stream(r, "stream_2")
			}),
			grpcserver.AsStreamInterceptor(stream(rec, "stream_3")),
		)

		_, err := unaryCall(t, conn, "value")
		require.NoError(t, err)
		assert.Equal(t, []string{"unary_1", "unary_2", "unary_3"}, rec.get())

		_, err = streamCall(t, conn, "value", 1)
		require.NoError(t, err)
		assert.Equal(t, []string{"unary_1", "unary_2", "unary_3", "stream_1", "stream_2", "stream_3"}, rec.get())
	})

	t.Run("errors", func(t *testing.T) {
		t.Parallel()

		for name, option := range map[string]fx.Option{
			"nil unary":       grpcserver.AsUnaryInterceptor(nil),
			"invalid unary":   grpcserver.AsUnaryInterceptor("invalid"),
			"invalid stream":  grpcserver.AsStreamInterceptor(func(io.Reader) string { return "" }),
			"stream as unary": grpcserver.AsUnaryInterceptor(stream(&recorder{}, "stream")),
			"unary as stream": grpcserver.AsStreamInterceptor(unary(&recorder{}, "unary")),
			"nil stream":      grpcserver.AsStreamInterceptor(nil),
		} {
			require.ErrorContains(t, newServerError(t, option), "cannot register interceptor", name)
		}
	})
}
