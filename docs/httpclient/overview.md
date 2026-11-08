---
title: HTTP client
description: The httpclient module, HTTP clients instrumented with OpenTelemetry, with transport middlewares registered through fx.
---

The `httpclient` module provides standard `*http.Client`s, instrumented with OpenTelemetry: every request gets a client span, metrics and a log, and carries the trace context to the server it calls.

```shell
go get github.com/go-oryn/oryn/httpclient
```

## Quick start

```go
type WeatherClient struct {
	client *http.Client
}

func NewWeatherClient(client *http.Client) *WeatherClient {
	return &WeatherClient{client: client}
}

func (c *WeatherClient) Temperature(ctx context.Context, city string) (*Temperature, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://weather.example.com/cities/"+url.PathEscape(city), nil)
	if err != nil {
		return nil, err
	}

	response, err := c.client.Do(request)
	...
}
```

```go
core.New(
	httpclient.Module,
	fx.Provide(NewWeatherClient),
)
```

- `httpclient.Module` provides the default `*http.Client`, and the `*httpclient.Factory` to build more clients.
- Build the requests with the context of the caller, `http.NewRequestWithContext`: the client span is then a child of the current span, and the request is canceled with it.
- Every request is traced, measured and logged, see [Observability](observability.md).
- The client has a 30 seconds timeout by default, see [Configuration](configuration.md).

## Factory

The `*httpclient.Factory` builds clients with their own settings. They share the registered middlewares and the instrumentation:

```go
func NewWeatherClient(factory *httpclient.Factory) *WeatherClient {
	return &WeatherClient{client: factory.NewClient(
		httpclient.WithTimeout(5*time.Second),
		httpclient.WithMiddlewares(NewAPIKeyMiddleware("weather")),
	)}
}
```

| Option | Description |
|---|---|
| `WithTimeout(d)` | The client timeout, instead of `httpclient.timeout`. `0` means none. |
| `WithMiddlewares(m...)` | Middlewares of this client only, applied after the registered ones. |
| `WithTransport(rt)` | The base transport, instead of the configured one: a custom TLS configuration, a proxy... The middlewares and the instrumentation still wrap it. |

## Middlewares

Middlewares run code around every request: authentication headers, retries, rate limiting... A middleware implements `httpclient.Middleware`, or is a `func(http.RoundTripper) http.RoundTripper`. `httpclient.RoundTripperFunc` keeps them short:

```go
func NewAuthMiddleware(tokens *TokenSource) httpclient.Middleware {
	return httpclient.MiddlewareFunc(func(next http.RoundTripper) http.RoundTripper {
		return httpclient.RoundTripperFunc(func(r *http.Request) (*http.Response, error) {
			token, err := tokens.Token(r.Context())
			if err != nil {
				return nil, err
			}

			// A transport must not modify the request it receives: clone it.
			r = r.Clone(r.Context())
			r.Header.Set("Authorization", "Bearer "+token)

			return next.RoundTrip(r)
		})
	})
}
```

Register them with `AsMiddleware`, as values or constructors whose parameters are injected by fx:

```go
httpclient.AsMiddleware(NewAuthMiddleware)
```

Registered middlewares apply to every client of the factory, in registration order: the first registered middleware is the outermost one. A request goes through, from the outermost:

1. the registered middlewares, then the `WithMiddlewares` ones,
2. the OpenTelemetry instrumentation,
3. the request log,
4. the base transport.

As the middlewares wrap the instrumentation, a middleware retrying a request produces one client span per attempt.

## Configuration

The `httpclient.*` keys set the timeout, the connection pool, the request logs and the URLs excluded from telemetry. See [Configuration](configuration.md).
