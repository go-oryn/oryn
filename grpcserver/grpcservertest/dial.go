// Package grpcservertest calls the gRPC server of an application in tests, without network.
//
//	func TestGetUser(t *testing.T) {
//		var server *grpc.Server
//
//		coretest.Start(t, app, fx.Populate(&server))
//
//		client := pb.NewUsersClient(grpcservertest.Dial(t, server))
//		user, err := client.GetUser(t.Context(), &pb.GetUserRequest{Id: "42"})
//	}
package grpcservertest

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// BufferSize is the size of the in memory connection buffer.
const BufferSize = 1024 * 1024

// Dial serves the server on an in memory listener, and returns a client connection to it.
// The connection is closed and the server stopped when the test ends.
func Dial(tb testing.TB, server *grpc.Server, options ...grpc.DialOption) *grpc.ClientConn {
	tb.Helper()

	listener := bufconn.Listen(BufferSize)

	served := make(chan struct{})

	go func() {
		defer close(served)

		_ = server.Serve(listener)
	}()

	options = append([]grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
	}, options...)

	conn, err := grpc.NewClient("passthrough:///bufconn", options...)
	if err != nil {
		tb.Fatalf("cannot create the grpc client: %v", err)
	}

	tb.Cleanup(func() {
		_ = conn.Close()

		server.Stop()
		<-served
	})

	return conn
}
