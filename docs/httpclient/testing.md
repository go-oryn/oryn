---
title: Testing
description: Serve the requests of the HTTP clients in memory, and assert their telemetry.
---

The `httpclienttest` package routes the requests of the clients to an `http.Handler`, in memory: tests need no network nor fake server, and the requests still go through the middlewares, the instrumentation and the request log.

## Serve

`httpclienttest.Serve` replaces the base transport of the application: every client of the factory sends its requests to the handler.

```go
func TestTemperature(t *testing.T) {
	t.Parallel()

	var (
		weather *WeatherClient
		spans   *tracetest.InMemoryExporter
		logs    *logtest.InMemoryExporter
	)

	coretest.Start(t, core.New(httpclient.Module, fx.Provide(NewWeatherClient)),
		httpclienttest.Serve(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/cities/paris", r.URL.Path)

			fmt.Fprint(w, `{"celsius": 21}`)
		})),
		fx.Populate(&weather, &spans, &logs),
	)

	temperature, err := weather.Temperature(t.Context(), "paris")
	require.NoError(t, err)
	assert.Equal(t, 21, temperature.Celsius)

	spantest.HasSpan(t, spans,
		spantest.WithName("GET"),
		spantest.WithAttributeValue(semconv.HTTPResponseStatusCode(http.StatusOK)),
	)
	logtest.HasRecord(t, logs, logtest.WithBody("http client request"), logtest.WithAttributeValue("status", "200"))
}
```

The handler gets a server request, like from a real server: `r.Host`, `r.RequestURI` and `r.RemoteAddr` (`httpclienttest.RemoteAddr`) are set. Route the requests of several APIs with an `http.ServeMux`, using host patterns: `mux.Handle("weather.example.com/", ...)`.

A client built with `httpclient.WithTransport` keeps its own transport: `Serve` does not apply to it.

## Transport

`httpclienttest.Transport` returns the in-memory `http.RoundTripper`, for a single client:

```go
client := factory.NewClient(httpclient.WithTransport(httpclienttest.Transport(handler)))
```

## Calling the application server

To test a client against the HTTP server of the application, serve its requests with the server handler. The trace context is propagated, so the server span is a child of the client span:

```go
var (
	factory *httpclient.Factory
	server  *http.Server
	spans   *tracetest.InMemoryExporter
)

coretest.Start(t, core.New(httpclient.Module, httpserver.Module, users.Module), fx.Populate(&factory, &server, &spans))

client := factory.NewClient(httpclient.WithTransport(httpclienttest.Transport(server.Handler)))
```

## Errors

Test the transport errors with a failing transport:

```go
client := factory.NewClient(httpclient.WithTransport(httpclient.RoundTripperFunc(func(*http.Request) (*http.Response, error) {
	return nil, errors.New("connection refused")
})))
```

A canceled request context fails the request with the context error, like with a real transport.
