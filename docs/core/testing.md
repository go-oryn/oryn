---
title: Testing
description: Run the application in tests with coretest, and assert traces, metrics and logs in memory.
---

`coretest` runs an oryn application in tests, in the `test` environment:

- all telemetry is exported in memory, whatever the configuration,
- logs are not printed on the console,
- the logger is not set as `slog.Default()`, so parallel tests do not interfere.

## Start the application

```go
func TestGreet(t *testing.T) {
	t.Parallel()

	var greeter *Greeter

	coretest.Start(t, core.New(greet.Module), fx.Populate(&greeter))

	assert.Equal(t, "hello oryn", greeter.Greet(t.Context(), "oryn"))
}
```

| Function | Description |
|---|---|
| `coretest.Start(t, app, options...)` | Builds and starts the application, and stops it when the test ends |
| `coretest.New(t, app, options...)` | Builds the application without starting it |
| `coretest.TestOptions()` | The options added to every test application, to build an `fx.App` yourself |

The options come after the application ones: get dependencies with `fx.Populate`, replace them with `fx.Replace` or `fx.Decorate`, and override the configuration with `config.AsOptions`:

```go
coretest.Start(t, app,
	config.AsOptions(config.WithValues(map[string]any{"log.level": "debug"})),
	fx.Decorate(func() Clock { return fakeClock }),
	fx.Populate(&service),
)
```

The application uses the test context as base context: it is cancelled when the test ends.

## Assert telemetry

The in-memory exporters are in the container. Assert on them with the `otel/oteltest` packages:

```go
func TestCheckout(t *testing.T) {
	t.Parallel()

	var (
		service *CheckoutService
		spans   *tracetest.InMemoryExporter
		logs    *logtest.InMemoryExporter
		metrics *sdkmetric.ManualReader
	)

	coretest.Start(t, app, fx.Populate(&service, &spans, &logs, &metrics))

	require.NoError(t, service.Checkout(t.Context(), cart))

	spantest.HasSpan(t, spans, spantest.WithName("checkout"))
	logtest.HasRecord(t, logs, logtest.WithBody("checked out"), logtest.WithLevel(slog.LevelInfo))
	assert.Equal(t, int64(1), metrictest.Int64Sum(t, metrics, "checkout.orders"))
}
```

| Package | Assertions | Filters |
|---|---|---|
| `spantest` | `HasSpan`, `HasNoSpan`, `SpanCount`, `Find` | `WithName`, `WithNameContains`, `WithKind`, `WithStatus`, `WithAttribute`, `WithAttributeValue`, `WithParent` |
| `logtest` | `HasRecord`, `HasNoRecord`, `RecordCount` | `WithBody`, `WithBodyContains`, `WithLevel`, `WithSeverity`, `WithAttribute`, `WithAttributeValue` |
| `metrictest` | `HasMetric`, `HasNoMetric`, `Int64Sum`, `Collect`, `Find` | `WithName`, `WithUnit`, `WithAttributeValue` |

When an assertion fails, the message lists what was recorded, to see what went wrong without debugging.

## Test failures to start

To test that an application fails to start, build it with `fx.New` and check the error:

```go
app := fx.New(core.New(myModule).Options(coretest.TestOptions(), fx.NopLogger)...)
require.ErrorContains(t, app.Err(), "invalid configuration")
```
