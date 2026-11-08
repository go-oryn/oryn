package otel

import (
	"log/slog"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Telemetry gives application code its logger, tracer and meter in a single dependency.
type Telemetry interface {
	Logger() *slog.Logger
	Tracer() trace.Tracer
	Meter() metric.Meter
}

var _ Telemetry = (*DefaultTelemetry)(nil)

// DefaultTelemetry is the default [Telemetry] implementation.
type DefaultTelemetry struct {
	logger *slog.Logger
	tracer trace.Tracer
	meter  metric.Meter
}

// NewTelemetry returns a [DefaultTelemetry].
func NewTelemetry(logger *slog.Logger, tracer trace.Tracer, meter metric.Meter) *DefaultTelemetry {
	return &DefaultTelemetry{
		logger: logger,
		tracer: tracer,
		meter:  meter,
	}
}

// Logger returns the application logger.
func (t *DefaultTelemetry) Logger() *slog.Logger {
	return t.logger
}

// Tracer returns the application tracer.
func (t *DefaultTelemetry) Tracer() trace.Tracer {
	return t.tracer
}

// Meter returns the application meter.
func (t *DefaultTelemetry) Meter() metric.Meter {
	return t.meter
}
