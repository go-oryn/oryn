---
title: Getting started
description: Create a first oryn application, with configuration, an HTTP endpoint, telemetry and a test.
---

This guide builds a small HTTP service: it answers `GET /hello/{name}`, and produces traces, metrics and logs.

Go 1.26 or later is required.

## Install

```shell
go mod init example.com/hello
go get github.com/go-oryn/oryn/core github.com/go-oryn/oryn/httpserver
```

## Configuration

Configuration files live in a `configs` directory, embedded in the binary:

```
hello/
├── configs/
│   ├── app.yaml
│   ├── embed.go
│   └── dev/
│       └── otel.yaml
├── main.go
└── main_test.go
```

```yaml
# configs/app.yaml
app:
  name: hello
  version: 0.1.0
log:
  console:
    enabled: true
```

```yaml
# configs/dev/otel.yaml: loaded when APP_ENV=dev, prints the spans on the console
otel:
  traces:
    exporter: console
  metrics:
    exporter: none
  logs:
    exporter: none
```

```go
// configs/embed.go
package configs

import "embed"

//go:embed *.yaml */*.yaml
var FS embed.FS
```

See [Configuration](core/configuration.md) for the loading rules.

## Application

```go
// main.go
package main

import (
	"fmt"
	"net/http"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/httpserver"

	"example.com/hello/configs"
)

// Module holds the application handlers. Tests reuse it.
var Module = httpserver.AsHandler("GET /hello/{name}", func(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "hello %s", r.PathValue("name"))
})

func main() {
	core.New(
		core.WithConfigFS(configs.FS),
		httpserver.Module,
		Module,
	).Run(httpserver.Serve())
}
```

- `core.New` creates the application, with the core module: configuration, OpenTelemetry, logging and health checks.
- `httpserver.Module` provides the HTTP server, and `httpserver.AsHandler` registers a handler on it.
- `Run` starts the application with the HTTP server, and stops it gracefully on `SIGINT` or `SIGTERM`.

Run it:

```shell
APP_ENV=dev go run .
curl localhost:8080/hello/oryn
```

The console shows the request log, and the `GET /hello/{name}` span. Without `APP_ENV=dev`, telemetry is sent with OTLP to `localhost:4318`, or wherever `OTEL_EXPORTER_OTLP_ENDPOINT` points.

## Test

Tests run the application in the `test` environment, where telemetry is exported in memory, and serve requests without a network:

```go
// main_test.go
package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/core/coretest"
	"github.com/go-oryn/oryn/core/otel/oteltest/spantest"
	"github.com/go-oryn/oryn/httpserver"

	"example.com/hello/configs"
)

func TestHello(t *testing.T) {
	t.Parallel()

	var (
		server *http.Server
		spans  *tracetest.InMemoryExporter
	)

	app := core.New(core.WithConfigFS(configs.FS), httpserver.Module, Module)
	coretest.Start(t, app, fx.Populate(&server, &spans))

	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/hello/oryn", nil))

	assert.Equal(t, "hello oryn", recorder.Body.String())
	spantest.HasSpan(t, spans, spantest.WithName("GET /hello/{name}"))
}
```

```shell
go test ./...
```

## Next steps

- [Application](core/application.md): how an oryn application is assembled.
- [HTTP server](httpserver/overview.md): handlers, middlewares and handler groups.
- [OpenTelemetry](core/opentelemetry.md): exporters, sampling and the `OTEL_*` variables.
