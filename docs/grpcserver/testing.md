---
title: Testing
description: Test gRPC services in memory, and assert their telemetry.
---

Tests run the application with [coretest](../core/testing.md), and call the `*grpc.Server` with `grpcservertest.Dial`, which serves it on an in memory connection, without listening on a port:

```go
func TestGetUser(t *testing.T) {
	t.Parallel()

	var (
		server *grpc.Server
		spans  *tracetest.InMemoryExporter
		logs   *logtest.InMemoryExporter
	)

	coretest.Start(t, core.New(grpcserver.Module, users.Module), fx.Populate(&server, &spans, &logs))

	client := pb.NewUsersClient(grpcservertest.Dial(t, server))

	user, err := client.GetUser(t.Context(), &pb.GetUserRequest{Id: "42"})
	require.NoError(t, err)
	assert.Equal(t, "42", user.GetId())

	spantest.HasSpan(t, spans,
		spantest.WithName("users.v1.Users/GetUser"),
		spantest.WithAttributeValue(semconv.RPCResponseStatusCode("OK")),
	)
	logtest.HasRecord(t, logs, logtest.WithBody("grpc request"), logtest.WithAttributeValue("method", "GetUser"))
}
```

The call goes through the whole server: instrumentation, interceptors and service. The connection is closed and the server stopped when the test ends.

## Over the network

To test with a real connection, set `grpcserver.address` to `127.0.0.1:0` and add `grpcserver.Serve()`: once started, the `*grpcserver.Address` is the listening address.

```go
var address *grpcserver.Address

coretest.Start(t, app,
	config.AsOptions(config.WithValues(map[string]any{"grpcserver.address": "127.0.0.1:0"})),
	grpcserver.Serve(),
	fx.Populate(&address),
)

conn, err := grpc.NewClient(address.String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
```
