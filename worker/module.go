// Package worker runs long-running background workers with the application: queue consumers,
// pollers, outbox relays... Each worker runs in its own goroutines, is restarted with a backoff
// when it fails or panics, is reported by a health probe, and is instrumented with OpenTelemetry.
//
//	core.New(
//		core.WithConfigFS(configs.FS),
//		worker.Module,
//		worker.AsWorker("outbox", NewOutboxWorker),
//	).Run(worker.Run())
//
// A worker implements [Worker]: its Run method blocks until the work is done, or until its
// context is canceled when the application stops.
package worker

import (
	"context"
	"errors"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/core/health"
)

// ModuleName is the name of the worker fx module.
const ModuleName = "worker"

// InstrumentationScope is the OpenTelemetry instrumentation scope of the worker module.
const InstrumentationScope = "github.com/go-oryn/oryn/worker"

// Telemetry attribute keys.
const (
	// NameKey is the name of the worker.
	NameKey = attribute.Key("worker.name")
	// ReplicaKey is the index of the worker replica.
	ReplicaKey = attribute.Key("worker.replica")
	// ErrorTypeKey is the type of the error of a failed run: the Go error type, or "panic".
	ErrorTypeKey = attribute.Key("error.type")
)

// Metric names.
const (
	MetricRuns     = "worker.runs"
	MetricRestarts = "worker.restarts"
	MetricActive   = "worker.active"
)

// Module provides the [*Pool] running the registered workers, and registers its liveness and
// readiness [health.Probe]. The workers are started by [Run].
var Module = fx.Module(
	ModuleName,
	fx.Provide(NewPool),
	health.AsProbe(NewProbe, health.Liveness, health.Readiness),
)

// NewPoolParams are the dependencies of [NewPool].
type NewPoolParams struct {
	fx.In

	Config         *config.Config
	Logger         *slog.Logger
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	Shutdowner     fx.Shutdowner
	Registrations  []registration `group:"oryn-worker-workers"`
}

// NewPool returns the [*Pool] of the registered workers, with their settings resolved from the
// options and the worker.* config keys.
func NewPool(params NewPoolParams) (*Pool, error) {
	cfg := params.Config

	entries, byName, err := newEntries(params.Registrations, func(reg registration) (settings, error) {
		return resolveSettings(cfg, reg.name, reg.options)
	})
	if err != nil {
		return nil, err
	}

	meter := params.MeterProvider.Meter(InstrumentationScope)

	runs, err := meter.Int64Counter(MetricRuns, metric.WithDescription("Number of worker runs, by outcome"), metric.WithUnit("{run}"))
	if err != nil {
		return nil, err
	}

	restarts, err := meter.Int64Counter(MetricRestarts, metric.WithDescription("Number of worker restarts"), metric.WithUnit("{restart}"))
	if err != nil {
		return nil, err
	}

	active, err := meter.Int64UpDownCounter(MetricActive, metric.WithDescription("Number of running worker replicas"), metric.WithUnit("{replica}"))
	if err != nil {
		return nil, err
	}

	return &Pool{
		logger:         params.Logger,
		tracer:         params.TracerProvider.Tracer(InstrumentationScope),
		shutdowner:     params.Shutdowner,
		shutdownOnFail: getBool(cfg, "worker.shutdown_on_failure", true),
		entries:        entries,
		byName:         byName,
		runs:           runs,
		restarts:       restarts,
		active:         active,
	}, nil
}

// Run starts the registered workers with the application, and stops them when the application
// stops: their context is canceled, and the application waits for them to return.
func Run() fx.Option {
	return fx.Invoke(func(lc fx.Lifecycle, pool *Pool) {
		lc.Append(fx.Hook{
			OnStart: pool.Start,
			OnStop:  pool.Stop,
		})
	})
}

// Probe is a [health.Probe] failing when a worker replica has failed.
type Probe struct {
	pool *Pool
}

// NewProbe returns a [Probe].
func NewProbe(pool *Pool) *Probe {
	return &Probe{pool: pool}
}

// Name returns "worker".
func (p *Probe) Name() string {
	return ModuleName
}

// Check returns an error naming the failed worker replicas.
func (p *Probe) Check(context.Context) error {
	var failed []error

	for _, info := range p.pool.Workers() {
		if info.State == StateFailed {
			failed = append(failed, &FailedError{Name: info.Name, Replica: info.Replica, Err: info.Err})
		}
	}

	return errors.Join(failed...)
}
