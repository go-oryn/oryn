package otel_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/core/coretest"
	"github.com/go-oryn/oryn/core/otel"
	"github.com/go-oryn/oryn/core/otel/oteltest/logtest"
	"github.com/go-oryn/oryn/core/otel/oteltest/metrictest"
	"github.com/go-oryn/oryn/core/otel/oteltest/spantest"
)

func newTestApp(t *testing.T, values map[string]any) core.App {
	t.Helper()

	return core.New(config.AsOptions(config.WithValues(values)))
}

func resourceValue(t *testing.T, res *resource.Resource, key attribute.Key) string {
	t.Helper()

	value, ok := res.Set().Value(key)
	require.True(t, ok, "missing resource attribute %s", key)

	return value.String()
}

func TestTelemetry(t *testing.T) {
	t.Parallel()

	var (
		telemetry otel.Telemetry
		spans     *tracetest.InMemoryExporter
		logs      *logtest.InMemoryExporter
		metrics   *sdkmetric.ManualReader
	)

	coretest.Start(t, newTestApp(t, map[string]any{"app.name": "test_app"}), fx.Populate(&telemetry, &spans, &logs, &metrics))

	ctx, span := telemetry.Tracer().Start(t.Context(), "test_span")
	telemetry.Logger().InfoContext(ctx, "test_log", "test_key", "test_value")
	telemetry.Logger().DebugContext(ctx, "test_debug_log")

	counter, err := telemetry.Meter().Int64Counter("test_counter")
	require.NoError(t, err)
	counter.Add(ctx, 2, metric.WithAttributes(attribute.String("test_key", "test_value")))

	span.End()

	spantest.HasSpan(t, spans, spantest.WithName("test_span"))
	logtest.HasRecord(t, logs, logtest.WithBody("test_log"), logtest.WithLevel(slog.LevelInfo), logtest.WithAttributeValue("test_key", "test_value"))
	logtest.HasNoRecord(t, logs, logtest.WithBody("test_debug_log"))
	assert.Equal(t, int64(2), metrictest.Int64Sum(t, metrics, "test_counter", attribute.String("test_key", "test_value")))

	records := logs.Filter(logtest.WithBody("test_log"))
	require.Len(t, records, 1)
	assert.Equal(t, span.SpanContext().TraceID(), records[0].TraceID(), "logs are correlated with the active span")
	assert.Equal(t, "test_app", records[0].InstrumentationScope().Name)

	metrictest.HasNoMetric(t, metrics, metrictest.WithName("go.memory.used"))
}

func TestRuntimeMetrics(t *testing.T) {
	t.Parallel()

	var metrics *sdkmetric.ManualReader

	coretest.Start(t, newTestApp(t, map[string]any{"otel.metrics.runtime": true}), fx.Populate(&metrics))

	metrictest.HasMetric(t, metrics, metrictest.WithName("go.memory.used"))
}

func TestResource(t *testing.T) {
	t.Run("from config", func(t *testing.T) {
		var res *resource.Resource

		coretest.Start(t, newTestApp(t, map[string]any{
			"app.name":                 "test_app",
			"app.version":              "1.2.3",
			"otel.resource.attributes": map[string]any{"test.key": "test_value"},
		}), fx.Populate(&res))

		assert.Equal(t, "test_app", resourceValue(t, res, semconv.ServiceNameKey))
		assert.Equal(t, "1.2.3", resourceValue(t, res, semconv.ServiceVersionKey))
		assert.Equal(t, config.EnvTest, resourceValue(t, res, semconv.DeploymentEnvironmentNameKey))
		assert.Equal(t, "test_value", resourceValue(t, res, "test.key"))
		assert.Equal(t, "go", resourceValue(t, res, semconv.TelemetrySDKLanguageKey))
	})

	t.Run("environment variables win", func(t *testing.T) {
		t.Setenv("OTEL_SERVICE_NAME", "env_app")
		t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "test.key=env_value")

		var res *resource.Resource

		coretest.Start(t,
			newTestApp(t, map[string]any{
				"app.name":                 "test_app",
				"otel.resource.attributes": map[string]any{"test.key": "test_value"},
			}),
			otel.AsResourceOptions(resource.WithAttributes(semconv.ServiceName("option_app"))),
			fx.Populate(&res),
		)

		assert.Equal(t, "env_app", resourceValue(t, res, semconv.ServiceNameKey))
		assert.Equal(t, "env_value", resourceValue(t, res, "test.key"))
	})

	t.Run("options win over config", func(t *testing.T) {
		var res *resource.Resource

		coretest.Start(t,
			newTestApp(t, map[string]any{"app.name": "test_app"}),
			otel.AsResourceOptions(resource.WithAttributes(semconv.ServiceName("option_app"))),
			fx.Populate(&res),
		)

		assert.Equal(t, "option_app", resourceValue(t, res, semconv.ServiceNameKey))
	})
}

func TestPropagator(t *testing.T) {
	t.Run("defaults to trace context and baggage", func(t *testing.T) {
		var propagator propagation.TextMapPropagator

		coretest.Start(t, newTestApp(t, nil), fx.Populate(&propagator))

		assert.ElementsMatch(t, []string{"traceparent", "tracestate", "baggage"}, propagator.Fields())
	})

	t.Run("from config", func(t *testing.T) {
		var propagator propagation.TextMapPropagator

		coretest.Start(t, newTestApp(t, map[string]any{"otel.propagators": []string{"b3"}}), fx.Populate(&propagator))

		assert.Equal(t, []string{"b3"}, propagator.Fields())
	})

	t.Run("invalid", func(t *testing.T) {
		app := fx.New(newTestApp(t, map[string]any{"otel.propagators": []string{"unknown"}}).Options(
			coretest.TestOptions(),
			fx.NopLogger,
			fx.Invoke(func(propagation.TextMapPropagator) {}),
		)...)
		require.ErrorContains(t, app.Err(), "unknown propagator: unknown")
	})
}

func TestExporters(t *testing.T) {
	t.Parallel()

	// The meter provider exports on shutdown, even when nothing was recorded.
	collector := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(collector.Close)

	// Exporters are only created outside the test environment, where everything is exported in memory.
	for name, values := range map[string]map[string]any{
		"console": {
			"otel.traces.exporter":  "console",
			"otel.metrics.exporter": "console",
			"otel.logs.exporter":    "console",
		},
		"none": {
			"otel.disabled": true,
		},
		"otlp http": {
			"otel.exporter.otlp.endpoint": collector.URL,
			"otel.metrics.interval":       "1h",
		},
		"otlp grpc": {
			"otel.exporter.otlp.protocol": "grpc",
			"otel.exporter.otlp.endpoint": "http://localhost:4317",
			"otel.exporter.otlp.headers":  map[string]any{"key": "value"},
			"otel.metrics.exporter":       "none",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			app := fx.New(
				core.New(config.AsOptions(
					config.WithEnvironment("dev"),
					config.WithValues(values),
					config.WithValues(map[string]any{"otel.metrics.runtime": false, "log.set_default": false}),
				)).Options(fx.NopLogger)...,
			)
			require.NoError(t, app.Err())

			ctx, cancel := context.WithTimeout(t.Context(), fx.DefaultTimeout)
			defer cancel()

			require.NoError(t, app.Start(ctx))
			require.NoError(t, app.Stop(ctx))
		})
	}
}
