package grpcserver

import (
	"context"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// newRecoverUnaryInterceptor turns a panic into an Internal error, and logs it with its stack.
func newRecoverUnaryInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = recoverPanic(ctx, logger, info.FullMethod, recovered)
			}
		}()

		return handler(ctx, req)
	}
}

// newRecoverStreamInterceptor turns a panic into an Internal error, and logs it with its stack.
func newRecoverStreamInterceptor(logger *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = recoverPanic(stream.Context(), logger, info.FullMethod, recovered)
			}
		}()

		return handler(srv, stream)
	}
}

func recoverPanic(ctx context.Context, logger *slog.Logger, fullMethod string, recovered any) error {
	logger.ErrorContext(ctx, "grpc handler panic", "rpc", fullMethod, "panic", recovered, "stack", string(debug.Stack()))

	return status.Error(codes.Internal, codes.Internal.String())
}

// newLogUnaryInterceptor logs every unary call not excluded.
func newLogUnaryInterceptor(logger *slog.Logger, exclude map[string]bool) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if exclude[info.FullMethod] {
			return handler(ctx, req)
		}

		start := time.Now()
		resp, err := handler(ctx, req)
		logCall(ctx, logger, info.FullMethod, err, time.Since(start))

		return resp, err
	}
}

// newLogStreamInterceptor logs every streaming call not excluded.
func newLogStreamInterceptor(logger *slog.Logger, exclude map[string]bool) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if exclude[info.FullMethod] {
			return handler(srv, stream)
		}

		start := time.Now()
		err := handler(srv, stream)
		logCall(stream.Context(), logger, info.FullMethod, err, time.Since(start))

		return err
	}
}

// logCall logs a call at error level for server errors, and info level otherwise.
func logCall(ctx context.Context, logger *slog.Logger, fullMethod string, err error, duration time.Duration) {
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
		slog.String("code", code.String()),
		slog.Duration("duration", duration),
	}

	if err != nil {
		attrs = append(attrs, slog.String("error", status.Convert(err).Message()))
	}

	logger.LogAttrs(ctx, level, "grpc request", attrs...)
}

// isServerError reports whether the code is a server error, as defined by the
// OpenTelemetry semantic conventions for gRPC servers.
func isServerError(code codes.Code) bool {
	switch code { //nolint:exhaustive // The other codes are client errors or success.
	case codes.Unknown, codes.DeadlineExceeded, codes.Unimplemented, codes.Internal, codes.Unavailable, codes.DataLoss:
		return true
	default:
		return false
	}
}
