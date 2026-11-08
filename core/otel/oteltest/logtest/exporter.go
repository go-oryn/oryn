// Package logtest provides an in-memory OpenTelemetry log exporter and test
// assertions on the exported log records.
package logtest

import (
	"context"
	"sync"

	sdklog "go.opentelemetry.io/otel/sdk/log"
)

var _ sdklog.Exporter = (*InMemoryExporter)(nil)

// InMemoryExporter is an [sdklog.Exporter] keeping the exported records in memory.
// Records are kept after shutdown, so they can be asserted once the application stopped.
type InMemoryExporter struct {
	mu      sync.Mutex
	records []sdklog.Record
}

// NewInMemoryExporter returns a new [InMemoryExporter].
func NewInMemoryExporter() *InMemoryExporter {
	return &InMemoryExporter{}
}

// Export stores a copy of the records.
func (e *InMemoryExporter) Export(_ context.Context, records []sdklog.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, record := range records {
		e.records = append(e.records, record.Clone())
	}

	return nil
}

// Shutdown does nothing: records stay available.
func (e *InMemoryExporter) Shutdown(context.Context) error {
	return nil
}

// ForceFlush does nothing: records are stored synchronously.
func (e *InMemoryExporter) ForceFlush(context.Context) error {
	return nil
}

// Records returns a copy of the exported records.
func (e *InMemoryExporter) Records() []sdklog.Record {
	e.mu.Lock()
	defer e.mu.Unlock()

	records := make([]sdklog.Record, len(e.records))
	copy(records, e.records)

	return records
}

// Reset removes all the exported records.
func (e *InMemoryExporter) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.records = nil
}
