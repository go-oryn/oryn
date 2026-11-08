---
title: Testing
description: Test gRPC clients against in memory servers, and assert their telemetry.
---

Tests run the application with [coretest](../core/testing.md). The `grpcclienttest` package serves a `*grpc.Server` in memory, and points a named connection at it, without listening on a port and whatever the configured target.

## Fake server

`grpcclienttest.Serve` serves a test server on the connection:

```go
type fakeUsers struct {
	pb.UnimplementedUsersServer
}

func (fakeUsers) GetUser(_ context.Context, req *pb.GetUserRequest) (*pb.User, error) {
	return &pb.User{Id: req.GetId(), Name: "test_name"}, nil
}

func TestService(t *testing.T) {
	t.Parallel()

	server := grpc.NewServer()
	pb.RegisterUsersServer(server, fakeUsers{})

	var (
		service *app.Service
		spans   *tracetest.InMemoryExporter
		logs    *logtest.InMemoryExporter
	)

	coretest.Start(t, app.New(), grpcclienttest.Serve("users", server), fx.Populate(&service, &spans, &logs))

	name, err := service.UserName(t.Context(), "42")
	require.NoError(t, err)
	assert.Equal(t, "test_name", name)

	spantest.HasSpan(t, spans,
		spantest.WithName("users.v1.Users/GetUser"),
		spantest.WithKind(trace.SpanKindClient),
	)
	logtest.HasRecord(t, logs, logtest.WithBody("grpc client request"), logtest.WithAttributeValue("method", "GetUser"))
}
```

The connection is plaintext, its target is `passthrough:///users`. The server is started with the application, and stopped with it.

## Application server

`grpcclienttest.ServeApp` serves the `*grpc.Server` of the application, provided by the [grpcserver](../grpcserver/overview.md) module, on the connection. Use it to test a client against the real services, or the trace propagation:

```go
coretest.Start(t,
	core.New(
		grpcserver.Module,
		grpcserver.AsService(&pb.Users_ServiceDesc, NewUsersService),
		grpcclient.Module,
		grpcclient.AsClient("users", pb.NewUsersClient),
	),
	grpcclienttest.ServeApp("users"),
	fx.Populate(&client, &spans),
)

_, err := client.GetUser(t.Context(), &pb.GetUserRequest{Id: "42"})
require.NoError(t, err)

clientSpans := spantest.Find(spans, spantest.WithName("users.v1.Users/GetUser"), spantest.WithKind(trace.SpanKindClient))
require.Len(t, clientSpans, 1)
spantest.HasSpan(t, spans,
	spantest.WithKind(trace.SpanKindServer),
	spantest.WithParent(clientSpans[0].SpanContext.SpanID()),
)
```
