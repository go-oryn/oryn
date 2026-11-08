// Package metrictest provides test assertions on the metrics collected by a
// [sdkmetric.ManualReader].
package metrictest

import (
	"fmt"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// Filter selects metrics.
type Filter func(metricdata.Metrics) bool

// WithName selects the metrics named name.
func WithName(name string) Filter {
	return func(m metricdata.Metrics) bool {
		return m.Name == name
	}
}

// WithUnit selects the metrics with the given unit.
func WithUnit(unit string) Filter {
	return func(m metricdata.Metrics) bool {
		return m.Unit == unit
	}
}

// WithAttributeValue selects the metrics having at least one data point with the attribute kv.
func WithAttributeValue(kv attribute.KeyValue) Filter {
	return func(m metricdata.Metrics) bool {
		for _, set := range attributeSets(m.Data) {
			if value, ok := set.Value(kv.Key); ok && value == kv.Value {
				return true
			}
		}

		return false
	}
}

// Collect collects and returns all the metrics of the reader.
func Collect(tb testing.TB, reader *sdkmetric.ManualReader) []metricdata.Metrics {
	tb.Helper()

	var rm metricdata.ResourceMetrics

	if err := reader.Collect(tb.Context(), &rm); err != nil {
		tb.Fatalf("cannot collect metrics: %v", err)
	}

	var metrics []metricdata.Metrics
	for _, sm := range rm.ScopeMetrics {
		metrics = append(metrics, sm.Metrics...)
	}

	return metrics
}

// Find collects the metrics of the reader and returns the ones matching all the filters.
func Find(tb testing.TB, reader *sdkmetric.ManualReader, filters ...Filter) []metricdata.Metrics {
	tb.Helper()

	var matches []metricdata.Metrics

	for _, metric := range Collect(tb, reader) {
		if matchAll(metric, filters) {
			matches = append(matches, metric)
		}
	}

	return matches
}

// HasMetric asserts that at least one metric matches all the filters.
func HasMetric(tb testing.TB, reader *sdkmetric.ManualReader, filters ...Filter) bool {
	tb.Helper()

	if len(Find(tb, reader, filters...)) == 0 {
		tb.Errorf("expected a metric matching %d filter(s), found none\n%s", len(filters), describe(Collect(tb, reader)))

		return false
	}

	return true
}

// HasNoMetric asserts that no metric matches all the filters.
func HasNoMetric(tb testing.TB, reader *sdkmetric.ManualReader, filters ...Filter) bool {
	tb.Helper()

	if matches := Find(tb, reader, filters...); len(matches) > 0 {
		tb.Errorf("expected no metric matching %d filter(s), found %d\n%s", len(filters), len(matches), describe(matches))

		return false
	}

	return true
}

// Int64Sum returns the sum of the int64 counter (or up down counter) named name,
// over the data points having all the given attributes.
func Int64Sum(tb testing.TB, reader *sdkmetric.ManualReader, name string, attrs ...attribute.KeyValue) int64 {
	tb.Helper()

	var total int64

	for _, metric := range Find(tb, reader, WithName(name)) {
		sum, ok := metric.Data.(metricdata.Sum[int64])
		if !ok {
			tb.Fatalf("metric %q is a %T, not an int64 sum", name, metric.Data)
		}

		for _, dp := range sum.DataPoints {
			if hasAll(dp.Attributes, attrs) {
				total += dp.Value
			}
		}
	}

	return total
}

func hasAll(set attribute.Set, attrs []attribute.KeyValue) bool {
	for _, kv := range attrs {
		if value, ok := set.Value(kv.Key); !ok || value != kv.Value {
			return false
		}
	}

	return true
}

func matchAll(m metricdata.Metrics, filters []Filter) bool {
	for _, filter := range filters {
		if !filter(m) {
			return false
		}
	}

	return true
}

func attributeSets(data metricdata.Aggregation) []attribute.Set {
	switch d := data.(type) {
	case metricdata.Sum[int64]:
		return points(d.DataPoints)
	case metricdata.Sum[float64]:
		return points(d.DataPoints)
	case metricdata.Gauge[int64]:
		return points(d.DataPoints)
	case metricdata.Gauge[float64]:
		return points(d.DataPoints)
	case metricdata.Histogram[int64]:
		return histogramPoints(d.DataPoints)
	case metricdata.Histogram[float64]:
		return histogramPoints(d.DataPoints)
	case metricdata.ExponentialHistogram[int64]:
		return exponentialHistogramPoints(d.DataPoints)
	case metricdata.ExponentialHistogram[float64]:
		return exponentialHistogramPoints(d.DataPoints)
	default:
		return nil
	}
}

func points[N int64 | float64](dps []metricdata.DataPoint[N]) []attribute.Set {
	sets := make([]attribute.Set, 0, len(dps))
	for _, dp := range dps {
		sets = append(sets, dp.Attributes)
	}

	return sets
}

func histogramPoints[N int64 | float64](dps []metricdata.HistogramDataPoint[N]) []attribute.Set {
	sets := make([]attribute.Set, 0, len(dps))
	for _, dp := range dps {
		sets = append(sets, dp.Attributes)
	}

	return sets
}

func exponentialHistogramPoints[N int64 | float64](dps []metricdata.ExponentialHistogramDataPoint[N]) []attribute.Set {
	sets := make([]attribute.Set, 0, len(dps))
	for _, dp := range dps {
		sets = append(sets, dp.Attributes)
	}

	return sets
}

func describe(metrics []metricdata.Metrics) string {
	if len(metrics) == 0 {
		return "  metrics: none"
	}

	var b strings.Builder

	fmt.Fprintf(&b, "  metrics (%d):", len(metrics))

	for i, m := range metrics {
		fmt.Fprintf(&b, "\n    [%d] name=%q unit=%q type=%T", i, m.Name, m.Unit, m.Data)
	}

	return b.String()
}
