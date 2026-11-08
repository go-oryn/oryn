package oteltest_test

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/go-oryn/oryn/core/otel/oteltest/logtest"
	"github.com/go-oryn/oryn/core/otel/oteltest/metrictest"
	"github.com/go-oryn/oryn/core/otel/oteltest/spantest"
)

// recordingTB records the failures instead of failing the test.
type recordingTB struct {
	testing.TB

	failures []string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Errorf(format string, args ...any) {
	r.failures = append(r.failures, fmt.Sprintf(format, args...))
}

// Fatalf records the failure, and lets the helper continue.
func (r *recordingTB) Fatalf(format string, args ...any) {
	r.Errorf(format, args...)
}

// testProducer produces one metric per aggregation type, each with a data point having the test_key attribute.
type testProducer struct{}

func (testProducer) Produce(context.Context) ([]metricdata.ScopeMetrics, error) {
	attrs := attribute.NewSet(attribute.String("test_key", "test_value"))

	return []metricdata.ScopeMetrics{{Metrics: []metricdata.Metrics{
		{Name: "int64_sum", Data: metricdata.Sum[int64]{DataPoints: []metricdata.DataPoint[int64]{{Attributes: attrs}}}},
		{Name: "float64_sum", Data: metricdata.Sum[float64]{DataPoints: []metricdata.DataPoint[float64]{{Attributes: attrs}}}},
		{Name: "int64_gauge", Data: metricdata.Gauge[int64]{DataPoints: []metricdata.DataPoint[int64]{{Attributes: attrs}}}},
		{Name: "float64_gauge", Data: metricdata.Gauge[float64]{DataPoints: []metricdata.DataPoint[float64]{{Attributes: attrs}}}},
		{Name: "int64_histogram", Data: metricdata.Histogram[int64]{DataPoints: []metricdata.HistogramDataPoint[int64]{{Attributes: attrs}}}},
		{Name: "float64_histogram", Data: metricdata.Histogram[float64]{DataPoints: []metricdata.HistogramDataPoint[float64]{{Attributes: attrs}}}},
		{Name: "int64_exponential_histogram", Data: metricdata.ExponentialHistogram[int64]{DataPoints: []metricdata.ExponentialHistogramDataPoint[int64]{{Attributes: attrs}}}},
		{Name: "float64_exponential_histogram", Data: metricdata.ExponentialHistogram[float64]{DataPoints: []metricdata.ExponentialHistogramDataPoint[float64]{{Attributes: attrs}}}},
		{Name: "summary", Data: metricdata.Summary{DataPoints: []metricdata.SummaryDataPoint{{Attributes: attrs}}}},
	}}}, nil
}

func TestLogtest(t *testing.T) {
	t.Parallel()

	exporter := logtest.NewInMemoryExporter()
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
	logger := slog.New(otelslog.NewHandler("test", otelslog.WithLoggerProvider(provider)))

	logger.Info("test_info", "test_key", "test_value", "test_int", 1)
	logger.Warn("test_warn")
	require.NoError(t, provider.Shutdown(t.Context()))

	assert.Len(t, exporter.Records(), 2, "records are kept after shutdown")
	assert.True(t, logtest.HasRecord(t, exporter, logtest.WithBody("test_info"), logtest.WithLevel(slog.LevelInfo)))
	assert.True(t, logtest.HasRecord(t, exporter, logtest.WithBodyContains("warn"), logtest.WithLevel(slog.LevelWarn)))
	assert.True(t, logtest.HasRecord(t, exporter, logtest.WithAttribute("test_key"), logtest.WithAttributeValue("test_int", "1")))
	assert.True(t, logtest.HasNoRecord(t, exporter, logtest.WithLevel(slog.LevelError)))
	assert.True(t, logtest.RecordCount(t, exporter, 2))

	rec := &recordingTB{TB: t}
	assert.False(t, logtest.HasRecord(rec, exporter, logtest.WithBody("missing")))
	assert.False(t, logtest.HasNoRecord(rec, exporter, logtest.WithBody("test_info")))
	assert.False(t, logtest.RecordCount(rec, exporter, 1))
	require.Len(t, rec.failures, 3)
	assert.Contains(t, rec.failures[0], `body="test_info" test_key="test_value"`)

	exporter.Reset()
	assert.Empty(t, exporter.Records())
}

func TestSpantest(t *testing.T) {
	t.Parallel()

	exporter := tracetest.NewInMemoryExporter()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter)).Tracer("test")

	ctx, parent := tracer.Start(t.Context(), "test_parent", trace.WithSpanKind(trace.SpanKindServer))
	_, child := tracer.Start(ctx, "test_child", trace.WithAttributes(attribute.String("test_key", "test_value")))
	child.SetStatus(codes.Error, "test_error")
	child.End()
	parent.End()

	assert.True(t, spantest.HasSpan(t, exporter, spantest.WithName("test_parent"), spantest.WithKind(trace.SpanKindServer)))
	assert.True(t, spantest.HasSpan(t, exporter,
		spantest.WithNameContains("child"),
		spantest.WithStatus(codes.Error),
		spantest.WithAttribute("test_key"),
		spantest.WithAttributeValue(attribute.String("test_key", "test_value")),
		spantest.WithParent(parent.SpanContext().SpanID()),
	))
	assert.True(t, spantest.HasNoSpan(t, exporter, spantest.WithName("missing")))
	assert.True(t, spantest.SpanCount(t, exporter, 2))
	assert.Len(t, spantest.Find(exporter, spantest.WithKind(trace.SpanKindInternal)), 1)

	rec := &recordingTB{TB: t}
	assert.False(t, spantest.HasSpan(rec, exporter, spantest.WithName("missing")))
	assert.False(t, spantest.HasNoSpan(rec, exporter, spantest.WithName("test_child")))
	assert.False(t, spantest.SpanCount(rec, exporter, 3))
	require.Len(t, rec.failures, 3)
	assert.Contains(t, rec.failures[0], `name="test_child" kind=internal status=Error test_key=test_value`)
}

func TestMetrictest(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")

	counter, err := meter.Int64Counter("test_counter", metric.WithUnit("{call}"))
	require.NoError(t, err)

	histogram, err := meter.Float64Histogram("test_histogram")
	require.NoError(t, err)

	ctx := context.Background()
	counter.Add(ctx, 1, metric.WithAttributes(attribute.String("test_key", "a")))
	counter.Add(ctx, 2, metric.WithAttributes(attribute.String("test_key", "b")))
	histogram.Record(ctx, 1.5, metric.WithAttributes(attribute.Bool("test_bool", true)))

	assert.True(t, metrictest.HasMetric(t, reader, metrictest.WithName("test_counter"), metrictest.WithUnit("{call}")))
	assert.True(t, metrictest.HasMetric(t, reader, metrictest.WithAttributeValue(attribute.Bool("test_bool", true))))
	assert.True(t, metrictest.HasNoMetric(t, reader, metrictest.WithName("missing")))
	assert.Equal(t, int64(3), metrictest.Int64Sum(t, reader, "test_counter"))
	assert.Equal(t, int64(2), metrictest.Int64Sum(t, reader, "test_counter", attribute.String("test_key", "b")))
	assert.Len(t, metrictest.Collect(t, reader), 2)

	rec := &recordingTB{TB: t}
	assert.False(t, metrictest.HasMetric(rec, reader, metrictest.WithName("missing")))
	assert.False(t, metrictest.HasNoMetric(rec, reader, metrictest.WithName("test_histogram")))
	require.Len(t, rec.failures, 2)
	assert.Contains(t, rec.failures[0], `name="test_counter" unit="{call}"`)
}

func TestDescribe(t *testing.T) {
	t.Parallel()

	t.Run("logs", func(t *testing.T) {
		t.Parallel()

		exporter := logtest.NewInMemoryExporter()
		provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
		logger := slog.New(otelslog.NewHandler("test", otelslog.WithLoggerProvider(provider)))

		rec := &recordingTB{TB: t}
		logtest.HasRecord(rec, exporter, logtest.WithBody("missing"))

		for range 11 {
			logger.Info("test_info")
		}

		logtest.HasRecord(rec, exporter, logtest.WithBody("missing"))

		require.Len(t, rec.failures, 2)
		assert.Contains(t, rec.failures[0], "records: none")
		assert.Contains(t, rec.failures[1], "... and 1 more")
	})

	t.Run("spans", func(t *testing.T) {
		t.Parallel()

		exporter := tracetest.NewInMemoryExporter()
		tracer := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter)).Tracer("test")

		rec := &recordingTB{TB: t}
		spantest.HasSpan(rec, exporter, spantest.WithName("missing"))

		for range 11 {
			_, span := tracer.Start(t.Context(), "test_span", trace.WithAttributes(attribute.String("test_key", "test_value")))
			span.End()
		}

		spantest.HasSpan(rec, exporter, spantest.WithName("missing"))

		require.Len(t, rec.failures, 2)
		assert.Contains(t, rec.failures[0], "spans: none")
		assert.Contains(t, rec.failures[1], "... and 1 more")

		assert.True(t, spantest.HasNoSpan(t, exporter, spantest.WithAttribute("missing")))
		assert.True(t, spantest.HasNoSpan(t, exporter, spantest.WithAttributeValue(attribute.String("test_key", "other"))))
	})

	t.Run("metrics", func(t *testing.T) {
		t.Parallel()

		reader := sdkmetric.NewManualReader()
		sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

		rec := &recordingTB{TB: t}
		metrictest.HasMetric(rec, reader, metrictest.WithName("missing"))

		require.Len(t, rec.failures, 1)
		assert.Contains(t, rec.failures[0], "metrics: none")
	})
}

func TestMetrictestAggregations(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader(sdkmetric.WithProducer(testProducer{}))
	sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	value := attribute.String("test_key", "test_value")

	for _, name := range []string{
		"int64_sum", "float64_sum", "int64_gauge", "float64_gauge", "int64_histogram",
		"float64_histogram", "int64_exponential_histogram", "float64_exponential_histogram",
	} {
		assert.True(t, metrictest.HasMetric(t, reader, metrictest.WithName(name), metrictest.WithAttributeValue(value)), name)
	}

	assert.True(t, metrictest.HasNoMetric(t, reader, metrictest.WithName("summary"), metrictest.WithAttributeValue(value)),
		"unsupported aggregations have no attributes")

	rec := &recordingTB{TB: t}
	metrictest.Int64Sum(rec, reader, "float64_sum")

	require.Len(t, rec.failures, 1)
	assert.Contains(t, rec.failures[0], `metric "float64_sum" is a metricdata.Sum[float64], not an int64 sum`)
}

func TestMetrictestCollectFailure(t *testing.T) {
	t.Parallel()

	// A reader not registered with a meter provider cannot collect.
	rec := &recordingTB{TB: t}
	assert.Empty(t, metrictest.Collect(rec, sdkmetric.NewManualReader()))

	require.Len(t, rec.failures, 1)
	assert.Contains(t, rec.failures[0], "cannot collect metrics")
}
