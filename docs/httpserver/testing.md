---
title: Testing
description: Test HTTP handlers in memory, and assert their telemetry.
---

Tests run the application with [coretest](../core/testing.md), and serve requests with the handler of the `*http.Server`, without listening on a port:

```go
func TestGetUser(t *testing.T) {
	t.Parallel()

	var (
		server *http.Server
		spans  *tracetest.InMemoryExporter
		logs   *logtest.InMemoryExporter
	)

	coretest.Start(t, core.New(httpserver.Module, users.Module), fx.Populate(&server, &spans, &logs))

	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/users/42", nil))

	assert.Equal(t, http.StatusOK, recorder.Code)
	spantest.HasSpan(t, spans,
		spantest.WithName("GET /users/{id}"),
		spantest.WithAttributeValue(semconv.HTTPResponseStatusCode(http.StatusOK)),
	)
	logtest.HasRecord(t, logs, logtest.WithBody("http request"), logtest.WithAttributeValue("route", "GET /users/{id}"))
}
```

The request goes through the whole server: instrumentation, middlewares and handler.

## Over the network

To test with a real HTTP client, set `httpserver.address` to `127.0.0.1:0` and add `httpserver.Serve()`: once started, `server.Addr` is the listening address.

```go
coretest.Start(t, app,
	config.AsOptions(config.WithValues(map[string]any{"httpserver.address": "127.0.0.1:0"})),
	httpserver.Serve(),
	fx.Populate(&server),
)

response, err := http.Get("http://" + server.Addr + "/users/42")
```
