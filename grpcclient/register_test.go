package grpcclient_test

import (
	"context"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	testpb "google.golang.org/grpc/interop/grpc_testing"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/grpcclient"
)

// recorder records the interceptor calls, in order.
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

func unary(r *recorder, name string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		r.record(name)

		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func stream(r *recorder, name string) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		r.record(name)

		return streamer(ctx, desc, cc, method, opts...)
	}
}

func TestAsInterceptor(t *testing.T) {
	t.Parallel()

	t.Run("applied in registration order", func(t *testing.T) {
		t.Parallel()

		r := &recorder{}

		client, _, _ := startClient(t,
			fx.Supply(r),
			grpcclient.AsUnaryInterceptor(
				unary(r, "unary_first"),
				// A constructor, with injected dependencies.
				func(r *recorder) grpc.UnaryClientInterceptor { return unary(r, "unary_second") },
			),
			grpcclient.AsStreamInterceptor(
				stream(r, "stream_first"),
				func(r *recorder) grpc.StreamClientInterceptor { return stream(r, "stream_second") },
			),
		)

		_, err := unaryCall(t, client, codes.OK)
		require.NoError(t, err)

		s, err := client.StreamingOutputCall(t.Context(), &testpb.StreamingOutputCallRequest{})
		require.NoError(t, err)

		_, err = s.Recv()
		require.ErrorIs(t, err, io.EOF)

		assert.Equal(t, []string{"unary_first", "unary_second", "stream_first", "stream_second"}, r.get())
	})

	t.Run("invalid registrations", func(t *testing.T) {
		t.Parallel()

		for name, option := range map[string]fx.Option{
			"nil unary":      grpcclient.AsUnaryInterceptor(nil),
			"invalid unary":  grpcclient.AsUnaryInterceptor("invalid"),
			"invalid stream": grpcclient.AsStreamInterceptor(func() string { return "" }),
		} {
			err := startError(t, core.New(grpcclient.Module), option)
			require.ErrorContains(t, err, "cannot register interceptor", name)
		}
	})
}
