package logtest

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// maxDescribed is the maximum number of records printed in a failure message.
const maxDescribed = 10

// Filter selects log records.
type Filter func(sdklog.Record) bool

// WithLevel selects the records emitted with the given slog level.
func WithLevel(level slog.Level) Filter {
	// otelslog maps slog.LevelDebug (-4) to log.SeverityDebug (5), and so on.
	severity := log.Severity(int(level) + int(log.SeverityDebug) - int(slog.LevelDebug))

	return WithSeverity(severity)
}

// WithSeverity selects the records with the given severity.
func WithSeverity(severity log.Severity) Filter {
	return func(r sdklog.Record) bool {
		return r.Severity() == severity
	}
}

// WithBody selects the records whose body is exactly body.
func WithBody(body string) Filter {
	return func(r sdklog.Record) bool {
		return r.Body().AsString() == body
	}
}

// WithBodyContains selects the records whose body contains substr.
func WithBodyContains(substr string) Filter {
	return func(r sdklog.Record) bool {
		return strings.Contains(r.Body().AsString(), substr)
	}
}

// WithAttribute selects the records having the attribute key.
func WithAttribute(key string) Filter {
	return func(r sdklog.Record) bool {
		_, found := attributeValue(r, attribute.Key(key))

		return found
	}
}

// WithAttributeValue selects the records having the attribute key, whose value is printed as value.
func WithAttributeValue(key string, value string) Filter {
	return func(r sdklog.Record) bool {
		v, found := attributeValue(r, attribute.Key(key))

		return found && v.String() == value
	}
}

// Filter returns the exported records matching all the filters.
func (e *InMemoryExporter) Filter(filters ...Filter) []sdklog.Record {
	var matches []sdklog.Record

	for _, record := range e.Records() {
		if matchAll(record, filters) {
			matches = append(matches, record)
		}
	}

	return matches
}

// HasRecord asserts that at least one exported record matches all the filters.
func HasRecord(tb testing.TB, exporter *InMemoryExporter, filters ...Filter) bool {
	tb.Helper()

	if len(exporter.Filter(filters...)) == 0 {
		tb.Errorf("expected a log record matching %d filter(s), found none\n%s", len(filters), describe(exporter.Records()))

		return false
	}

	return true
}

// HasNoRecord asserts that no exported record matches all the filters.
func HasNoRecord(tb testing.TB, exporter *InMemoryExporter, filters ...Filter) bool {
	tb.Helper()

	if matches := exporter.Filter(filters...); len(matches) > 0 {
		tb.Errorf("expected no log record matching %d filter(s), found %d\n%s", len(filters), len(matches), describe(matches))

		return false
	}

	return true
}

// RecordCount asserts that exactly n exported records match all the filters.
func RecordCount(tb testing.TB, exporter *InMemoryExporter, n int, filters ...Filter) bool {
	tb.Helper()

	if matches := exporter.Filter(filters...); len(matches) != n {
		tb.Errorf("expected %d log record(s) matching %d filter(s), found %d\n%s", n, len(filters), len(matches), describe(exporter.Records()))

		return false
	}

	return true
}

func attributeValue(r sdklog.Record, key attribute.Key) (attribute.Value, bool) {
	var (
		value attribute.Value
		found bool
	)

	r.WalkAttributes(func(kv attribute.KeyValue) bool {
		if kv.Key == key {
			value, found = kv.Value, true

			return false
		}

		return true
	})

	return value, found
}

func matchAll(r sdklog.Record, filters []Filter) bool {
	for _, filter := range filters {
		if !filter(r) {
			return false
		}
	}

	return true
}

func describe(records []sdklog.Record) string {
	if len(records) == 0 {
		return "  records: none"
	}

	var b strings.Builder

	fmt.Fprintf(&b, "  records (%d):", len(records))

	for i, r := range records[:min(len(records), maxDescribed)] {
		fmt.Fprintf(&b, "\n    [%d] severity=%s body=%q", i, r.Severity(), r.Body().AsString())

		r.WalkAttributes(func(kv attribute.KeyValue) bool {
			fmt.Fprintf(&b, " %s=%q", kv.Key, kv.Value.String())

			return true
		})
	}

	if len(records) > maxDescribed {
		fmt.Fprintf(&b, "\n    ... and %d more", len(records)-maxDescribed)
	}

	return b.String()
}
