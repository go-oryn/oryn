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

Every module documents its keys, namespaced by module:

- `app.*`: [Application](application.md) identity, also used by [OpenTelemetry](opentelemetry.md)
- `otel.*`: [OpenTelemetry](opentelemetry.md)
- `log.*`: [Logging](opentelemetry.md#logging)
- `health.*`: [Health checks](health.md)
- `httpserver.*`: [HTTP server configuration](../httpserver/overview.md#configuration)
