// Package spantest provides test assertions on the spans recorded by a
// [tracetest.InMemoryExporter].
package spantest

import (
	"fmt"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// maxDescribed is the maximum number of spans printed in a failure message.
const maxDescribed = 10

// Filter selects spans.
type Filter func(tracetest.SpanStub) bool

// WithName selects the spans named name.
func WithName(name string) Filter {
	return func(s tracetest.SpanStub) bool {
		return s.Name == name
	}
}

// WithNameContains selects the spans whose name contains substr.
func WithNameContains(substr string) Filter {
	return func(s tracetest.SpanStub) bool {
		return strings.Contains(s.Name, substr)
	}
}

// WithKind selects the spans of the given kind.
func WithKind(kind trace.SpanKind) Filter {
	return func(s tracetest.SpanStub) bool {
		return s.SpanKind == kind
	}
}

// WithStatus selects the spans with the given status code.
func WithStatus(code codes.Code) Filter {
	return func(s tracetest.SpanStub) bool {
		return s.Status.Code == code
	}
}

// WithAttribute selects the spans having the attribute key.
func WithAttribute(key attribute.Key) Filter {
	return func(s tracetest.SpanStub) bool {
		for _, attr := range s.Attributes {
			if attr.Key == key {
				return true
			}
		}

		return false
	}
}

// WithAttributeValue selects the spans having the attribute kv.
func WithAttributeValue(kv attribute.KeyValue) Filter {
	return func(s tracetest.SpanStub) bool {
		for _, attr := range s.Attributes {
			if attr == kv {
				return true
			}
		}

		return false
	}
}

// WithParent selects the spans whose parent has the given span ID.
func WithParent(spanID trace.SpanID) Filter {
	return func(s tracetest.SpanStub) bool {
		return s.Parent.SpanID() == spanID
	}
}

// Find returns the spans of the exporter matching all the filters.
func Find(exporter *tracetest.InMemoryExporter, filters ...Filter) tracetest.SpanStubs {
	var matches tracetest.SpanStubs

	for _, span := range exporter.GetSpans() {
		if matchAll(span, filters) {
			matches = append(matches, span)
		}
	}

	return matches
}

// HasSpan asserts that at least one span matches all the filters.
func HasSpan(tb testing.TB, exporter *tracetest.InMemoryExporter, filters ...Filter) bool {
	tb.Helper()

	if len(Find(exporter, filters...)) == 0 {
		tb.Errorf("expected a span matching %d filter(s), found none\n%s", len(filters), describe(exporter.GetSpans()))

		return false
	}

	return true
}

// HasNoSpan asserts that no span matches all the filters.
func HasNoSpan(tb testing.TB, exporter *tracetest.InMemoryExporter, filters ...Filter) bool {
	tb.Helper()

	if matches := Find(exporter, filters...); len(matches) > 0 {
		tb.Errorf("expected no span matching %d filter(s), found %d\n%s", len(filters), len(matches), describe(matches))

		return false
	}

	return true
}

// SpanCount asserts that exactly n spans match all the filters.
func SpanCount(tb testing.TB, exporter *tracetest.InMemoryExporter, n int, filters ...Filter) bool {
	tb.Helper()

	if matches := Find(exporter, filters...); len(matches) != n {
		tb.Errorf("expected %d span(s) matching %d filter(s), found %d\n%s", n, len(filters), len(matches), describe(exporter.GetSpans()))

		return false
	}

	return true
}

func matchAll(s tracetest.SpanStub, filters []Filter) bool {
	for _, filter := range filters {
		if !filter(s) {
			return false
		}
	}

	return true
}

func describe(spans tracetest.SpanStubs) string {
	if len(spans) == 0 {
		return "  spans: none"
	}

	var b strings.Builder

	fmt.Fprintf(&b, "  spans (%d):", len(spans))

	for i, s := range spans[:min(len(spans), maxDescribed)] {
		fmt.Fprintf(&b, "\n    [%d] name=%q kind=%s status=%s", i, s.Name, s.SpanKind, s.Status.Code)

		for _, attr := range s.Attributes {
			fmt.Fprintf(&b, " %s=%s", attr.Key, attr.Value.String())
		}
	}

	if len(spans) > maxDescribed {
		fmt.Fprintf(&b, "\n    ... and %d more", len(spans)-maxDescribed)
	}

	return b.String()
}
