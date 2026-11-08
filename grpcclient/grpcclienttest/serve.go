// Package grpcclienttest connects the gRPC connections of an application to in-memory servers
// in tests: the client code and its instrumentation run as in production, without network.
//
//	func TestGetUserName(t *testing.T) {
//		// A fake of the users API.
//		server := grpc.NewServer()
//		pb.RegisterUsersServer(server, &fakeUsers{})
//
//		var service *Service
//
//		coretest.Start(t, app, grpcclienttest.Serve("users", server), fx.Populate(&service))
//
//		name, err := service.UserName(t.Context(), "42")
//	}
package grpcclienttest

import (
	"context"
	"net"

	"go.uber.org/fx"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/grpcclient"
)

// BufferSize is the size of the in-memory connection buffer.
const BufferSize = 1024 * 1024

// Serve makes the connection named name call server, in memory. The connection is configured as
// plaintext, whatever its configuration. The server is served when the application starts, and
// stopped when it stops.
func Serve(name string, server *grpc.Server) fx.Option {
	return serve(name, fx.Supply(fx.Annotate(server, fx.ResultTags(`name:"`+serverName(name)+`"`))))
}

// ServeApp makes the connection named name call the [*grpc.Server] of the application itself,
// provided by the grpcserver module, in memory: the client and server instrumentation are both
// exercised, and the server spans are children of the client spans.
func ServeApp(name string) fx.Option {
	return serve(name, fx.Provide(fx.Annotate(
		func(server *grpc.Server) *grpc.Server { return server },
		fx.ResultTags(`name:"`+serverName(name)+`"`),
	)))
}

func serverName(name string) string {
	return "oryn-grpcclienttest-" + name
}

// serve configures the connection to dial an in-memory listener, and serves on it the server
// provided by provide.
func serve(name string, provide fx.Option) fx.Option {
	listener := bufconn.Listen(BufferSize)
	key := "grpcclient.connections." + name

	return fx.Options(
		provide,
		config.AsOptions(config.WithValues(map[string]any{
			key + ".target":   "passthrough:///" + name,
			key + ".insecure": true,
		})),
		grpcclient.AsDialOptions(name, grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		})),
		fx.Invoke(fx.Annotate(func(lc fx.Lifecycle, server *grpc.Server) {
			served := make(chan struct{})

			lc.Append(fx.Hook{
				OnStart: func(context.Context) error {
					go func() {
						defer close(served)

						_ = server.Serve(listener)
					}()

					return nil
				},
				OnStop: func(context.Context) error {
					server.Stop()
					<-served

					return nil
				},
			})
		}, fx.ParamTags(``, `name:"`+serverName(name)+`"`))),
	)
}
