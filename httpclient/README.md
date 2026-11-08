# httpclient

[![Go Reference](https://pkg.go.dev/badge/github.com/go-oryn/oryn/httpclient.svg)](https://pkg.go.dev/github.com/go-oryn/oryn/httpclient)
[![codecov](https://codecov.io/gh/go-oryn/oryn/graph/badge.svg?flag=httpclient)](https://app.codecov.io/gh/go-oryn/oryn?flags%5B0%5D=httpclient)

> HTTP clients instrumented with OpenTelemetry, with transport middlewares registered through fx, and in-memory testing.

```shell
go get github.com/go-oryn/oryn/httpclient
```

```go
func NewWeatherClient(client *http.Client) *WeatherClient {
	return &WeatherClient{client: client}
}

func main() {
	core.New(
		core.WithConfigFS(configs.FS),
		httpclient.Module,
		fx.Provide(NewWeatherClient),
	).Run()
}
```

## Documentation

| Page | Source |
|---|---|
| [HTTP client](https://go-oryn.github.io/oryn/httpclient/overview/) (factory, middlewares) | [docs/httpclient/overview.md](../docs/httpclient/overview.md) |
| [Configuration](https://go-oryn.github.io/oryn/httpclient/configuration/) | [docs/httpclient/configuration.md](../docs/httpclient/configuration.md) |
| [Observability](https://go-oryn.github.io/oryn/httpclient/observability/) | [docs/httpclient/observability.md](../docs/httpclient/observability.md) |
| [Testing](https://go-oryn.github.io/oryn/httpclient/testing/) | [docs/httpclient/testing.md](../docs/httpclient/testing.md) |
