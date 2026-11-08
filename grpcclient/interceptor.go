package grpcclient

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// newLogUnaryInterceptor logs every unary call not excluded.
func newLogUnaryInterceptor(logger *slog.Logger, exclude map[string]bool) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if exclude[method] {
			return invoker(ctx, method, req, reply, cc, opts...)
		}

		start := time.Now()
		err := invoker(ctx, method, req, reply, cc, opts...)
		logCall(ctx, logger, cc.Target(), method, err, time.Since(start))

		return err
	}
}

// newLogStreamInterceptor logs every streaming call not excluded, when its stream is created.
// The stream messages are not logged: the span of the call covers the whole stream.
func newLogStreamInterceptor(logger *slog.Logger, exclude map[string]bool) grpc.StreamClientInterceptor {
	return func(
		ctx context.Context,
		desc *grpc.StreamDesc,
		cc *grpc.ClientConn,
		method string,
		streamer grpc.Streamer,
		opts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		if exclude[method] {
			return streamer(ctx, desc, cc, method, opts...)
		}

		start := time.Now()
		stream, err := streamer(ctx, desc, cc, method, opts...)
		logCall(ctx, logger, cc.Target(), method, err, time.Since(start))

		return stream, err
	}
}

// logCall logs a call at error level for server errors, and info level otherwise.
func logCall(ctx context.Context, logger *slog.Logger, target string, fullMethod string, err error, duration time.Duration) {
	code := status.Code(err)

	level := slog.LevelInfo
	if isServerError(code) {
		level = slog.LevelError
	}

	service, method, _ := strings.Cut(strings.TrimPrefix(fullMethod, "/"), "/")

	attrs := []slog.Attr{
		slog.String("rpc", fullMethod),
		slog.String("service", service),
		slog.String("method", method),
		slog.String("target", target),
		slog.String("code", code.String()),
		slog.Duration("duration", duration),
	}

	if err != nil {
		attrs = append(attrs, slog.String("error", status.Convert(err).Message()))
	}

	logger.LogAttrs(ctx, level, "grpc client request", attrs...)
}

// isServerError reports whether the code is a server error, as defined by the OpenTelemetry
// semantic conventions for gRPC.
func isServerError(code codes.Code) bool {
	switch code { //nolint:exhaustive // The other codes are client errors or success.
	case codes.Unknown, codes.DeadlineExceeded, codes.Unimplemented, codes.Internal, codes.Unavailable, codes.DataLoss:
		return true
	default:
		return false
	}
}
