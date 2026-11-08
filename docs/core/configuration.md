---
title: Configuration
description: Configuration files, environments, environment variables and config options.
---

The configuration is a `*config.Config`, built on [viper](https://github.com/spf13/viper), and injected in any constructor.

```go
func NewClient(cfg *config.Config) *Client {
	return &Client{
		url:     cfg.GetString("client.url"),
		timeout: cfg.GetDurationOrDefault("client.timeout", 5*time.Second),
	}
}
```

Besides the viper getters (`GetString`, `GetInt`, `GetDuration`...), `GetXxxOrDefault` return a default when the key is not set, and `AppName()`, `AppVersion()`, `Env()` and `IsTestEnv()` give the application identity.

## Sources

Configuration comes from, lowest to highest precedence:

1. built-in defaults (`app.name: app`, `app.version: 0.0.0`),
2. YAML or JSON files at the root of the config file systems, in lexical order,
3. the files of the sub directory named after the environment, e.g. `prod/`,
4. environment variables prefixed with `APP_`: `httpserver.address` is read from `APP_HTTPSERVER_ADDRESS`,
5. values set with `config.WithValues`.

The environment comes from the `APP_ENV` variable (`dev`, `prod`...). Tests run in the `test` environment.

```
configs/
├── app.yaml        # loaded in every environment
├── otel.yaml
├── dev/otel.yaml   # loaded when APP_ENV=dev, on top of the root files
└── test/app.yaml   # loaded in tests
```

Embed the files from inside the directory, so they are at the root of the file system:

```go
// configs/embed.go
package configs

import "embed"

//go:embed *.yaml */*.yaml
var FS embed.FS
```

```go
core.New(core.WithConfigFS(configs.FS))
```

## Environment variable placeholders

Files can reference environment variables with `${VAR}`, or `${VAR:default}` to use a default when the variable is not set or empty:

```yaml
database:
  url: ${DATABASE_URL}
  pool_size: ${DATABASE_POOL_SIZE:10}
```

## Config options

Modules and applications register config options with `config.AsOptions`:

```go
config.AsOptions(
	config.WithFS(extraFS),
	config.WithValues(map[string]any{"log.level": "debug"}),
)
```

| Option | Description |
|---|---|
| `WithFS(fsys)` | Loads the files of a file system. Later file systems override earlier ones |
| `WithValues(values)` | Sets values, with the highest precedence. Keys can be nested maps or dotted (`"log.level"`) |
| `WithEnvironment(env)` | Sets the environment, instead of `APP_ENV` |
| `WithEnvPrefix(prefix)` | Reads `<PREFIX>_ENV` and `<PREFIX>_*` variables instead of `APP_` |

Options apply in registration order: when two set the same key, the last registered wins. This is how tests override the application configuration, see [Testing](testing.md).

## Keys

Keys are namespaced by module. The core keys, with their defaults:

```yaml
app:
  name: app                     # service name, OTEL_SERVICE_NAME wins
  version: 0.0.0                # service version

log:
  level: info                   # debug, info, warn or error
  source: false                 # add the source code location
  set_default: true             # set the logger as slog.Default
  console:
    enabled: false              # also print the logs on stdout
    format: text                # text or json

otel:
  disabled: false               # disable the SDK, OTEL_SDK_DISABLED wins
  propagators: [tracecontext, baggage]
  resource:
    attributes: {}              # extra resource attributes
  traces:
    exporter: otlp              # otlp, console, memory or none
    sampler: parentbased_always_on
    sampler_arg: 1
  metrics:
    exporter: otlp
    interval: 60s               # export interval
    runtime: true               # Go runtime metrics, false in tests
  logs:
    exporter: otlp
  exporter:
    otlp:
      protocol: http/protobuf   # http/protobuf or grpc
      endpoint: http://localhost:4318
      headers: {}

health:
  timeout: 5s                   # max duration of a check
  server:
    address: ":8081"            # address of health.Serve()
```

Every key can be set with an environment variable: `APP_` followed by the key in upper case, with `_` instead of `.`, for example `APP_LOG_LEVEL=debug`. The standard `OTEL_*` variables win over the `otel.*` keys.

Details: [OpenTelemetry](opentelemetry.md#standard-environment-variables-win), [Logging](opentelemetry.md#logging), [Health checks](health.md).

The other modules document their keys on their Configuration page:

| Keys | Page |
|---|---|
| `httpserver.*` | [HTTP server configuration](../httpserver/configuration.md) |
| `grpcserver.*` | [gRPC server configuration](../grpcserver/configuration.md) |
| `mcpserver.*` | [MCP server configuration](../mcpserver/configuration.md) |
| `sql.*` | [SQL configuration](../sql/configuration.md) |
