// Package cron runs jobs on a schedule, built on [gocron], instrumented with OpenTelemetry.
//
//	core.New(
//		core.WithConfigFS(configs.FS),
//		cron.Module,
//		cron.AsJob("cleanup", "0 3 * * *", NewCleanupJob),
//	).Run(cron.Run())
//
// Every execution gets its own trace, metrics and logs. Tests run a job on demand with
// [Scheduler.RunNow], without waiting for its schedule.
//
// [gocron]: https://github.com/go-co-op/gocron
package cron

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/go-co-op/gocron/v2"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core/config"
)

// ModuleName is the name of the cron fx module.
const ModuleName = "cron"

// DefaultLocation is the default of cron.location.
const DefaultLocation = "UTC"

// maxStopTimeout is the gocron stop timeout: the application stop context is the real bound.
const maxStopTimeout = 24 * time.Hour

// Module provides the [*Scheduler], with the jobs registered with [AsJob]. Start it with [Run].
var Module = fx.Module(
	ModuleName,
	fx.Provide(NewScheduler),
)

// NewSchedulerParams are the dependencies of [NewScheduler].
type NewSchedulerParams struct {
	fx.In

	//nolint:containedctx // The base context of the application, supplied by the fx container.
	Context        context.Context
	Lifecycle      fx.Lifecycle
	Config         *config.Config
	Logger         *slog.Logger
	TracerProvider trace.TracerProvider
	MeterProvider  metric.MeterProvider
	Jobs           []registeredJob `group:"oryn-cron-jobs"`
}

// NewScheduler returns the [*Scheduler] of the registered jobs, configured by the cron.* keys.
// It fails on an invalid schedule or location, or on duplicate job names.
func NewScheduler(params NewSchedulerParams) (*Scheduler, error) {
	cfg := params.Config

	location, err := time.LoadLocation(cfg.GetStringOrDefault("cron.location", DefaultLocation))
	if err != nil {
		return nil, fmt.Errorf("invalid cron.location: %w", err)
	}

	scheduler, err := gocron.NewScheduler(
		gocron.WithLocation(location),
		// The application stop context bounds the wait for the running jobs, see Run.
		gocron.WithStopTimeout(maxStopTimeout),
	)
	if err != nil {
		return nil, fmt.Errorf("cannot create the cron scheduler: %w", err)
	}

	s, err := newScheduler(params, scheduler)
	if err != nil {
		_ = scheduler.ShutdownWithContext(context.Background())

		return nil, err
	}

	params.Lifecycle.Append(fx.StopHook(s.stop))

	return s, nil
}

func newScheduler(params NewSchedulerParams, scheduler gocron.Scheduler) (*Scheduler, error) {
	cfg := params.Config
	tracer := params.TracerProvider.Tracer(InstrumentationScope)
	meter := params.MeterProvider.Meter(InstrumentationScope)

	runs, err := meter.Int64Counter(MetricJobRuns, metric.WithDescription("Number of cron job executions"))
	if err != nil {
		return nil, err
	}

	duration, err := meter.Float64Histogram(MetricJobDuration,
		metric.WithDescription("Duration of cron job executions"), metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.WithoutCancel(params.Context))

	s := &Scheduler{
		scheduler:  scheduler,
		byName:     make(map[string]*job, len(params.Jobs)),
		logger:     params.Logger,
		logEnabled: cfg.GetBoolOrDefault("cron.log.enabled", true),
		tracer:     tracer,
		runs:       runs,
		duration:   duration,
		ctx:        ctx,
		cancel:     cancel,
	}

	// fx shuffles the value groups: restore the registration order.
	registered := slices.Clone(params.Jobs)
	slices.SortFunc(registered, func(a, b registeredJob) int { return cmp.Compare(a.sequence, b.sequence) })

	for _, r := range registered {
		if _, ok := s.byName[r.name]; ok {
			cancel()

			return nil, fmt.Errorf("duplicate cron job %q", r.name)
		}

		j := newJob(cfg, r)

		if j.enabled {
			if err := s.schedule(j); err != nil {
				cancel()

				return nil, err
			}
		}

		s.jobs = append(s.jobs, j)
		s.byName[j.name] = j
	}

	return s, nil
}

// newJob applies the config overrides, cron.jobs.<name>.*, then the global keys.
func newJob(cfg *config.Config, r registeredJob) *job {
	prefix := "cron.jobs." + r.name + "."

	singleton := cfg.GetBoolOrDefault("cron.singleton", true)
	if r.options.singleton != nil {
		singleton = *r.options.singleton
	}

	timeout := cfg.GetDuration("cron.timeout")
	if r.options.timeout > 0 {
		timeout = r.options.timeout
	}

	return &job{
		name:      r.name,
		schedule:  strings.TrimSpace(cfg.GetStringOrDefault(prefix+"schedule", r.schedule)),
		enabled:   cfg.GetBoolOrDefault(prefix+"enabled", true),
		singleton: cfg.GetBoolOrDefault(prefix+"singleton", singleton),
		timeout:   cfg.GetDurationOrDefault(prefix+"timeout", timeout),
		run:       r.job,
	}
}

// schedule adds the job to the gocron scheduler.
func (s *Scheduler) schedule(j *job) error {
	definition, err := definitionOf(j.schedule)
	if err != nil {
		return fmt.Errorf("invalid schedule %q of cron job %q: %w", j.schedule, j.name, err)
	}

	options := []gocron.JobOption{gocron.WithName(j.name)}
	if j.singleton {
		options = append(options, gocron.WithSingletonMode(gocron.LimitModeReschedule))
	}

	// The task takes no context: gocron would cancel it as soon as the scheduler stops, while
	// the running jobs are given the application stop timeout to finish.
	scheduled, err := s.scheduler.NewJob(definition, gocron.NewTask(func() {
		_ = s.execute(s.ctx, j, TriggerSchedule)
	}), options...)
	if err != nil {
		return fmt.Errorf("invalid schedule %q of cron job %q: %w", j.schedule, j.name, err)
	}

	j.scheduled = scheduled

	return nil
}

// definitionOf parses a schedule: @every <duration>, or a cron expression with optional seconds.
func definitionOf(schedule string) (gocron.JobDefinition, error) {
	if every, ok := strings.CutPrefix(schedule, "@every "); ok {
		interval, err := time.ParseDuration(strings.TrimSpace(every))
		if err != nil {
			return nil, err
		}

		if interval <= 0 {
			return nil, errors.New("the interval must be positive")
		}

		return gocron.DurationJob(interval), nil
	}

	return gocron.CronJob(schedule, true), nil
}

// Run starts the scheduler with the application. When the application stops, no new run is
// started, and the running jobs are given the stop timeout to finish, after which their context
// is canceled.
func Run() fx.Option {
	return fx.Invoke(func(lc fx.Lifecycle, logger *slog.Logger, scheduler *Scheduler) {
		lc.Append(fx.StartHook(func(ctx context.Context) {
			scheduler.scheduler.Start()

			logger.DebugContext(ctx, "cron scheduler started", "jobs", len(scheduler.jobs))
		}))
	})
}

// stop shuts the scheduler down, waiting for the running jobs until ctx is done.
func (s *Scheduler) stop(ctx context.Context) error {
	defer s.cancel()

	done := make(chan error, 1)

	go func() {
		// Wait for the running jobs past ctx: they are canceled below if ctx is done first.
		done <- s.scheduler.ShutdownWithContext(context.WithoutCancel(ctx))
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		// Cancel the running jobs, and give up waiting for them.
		s.cancel()

		return fmt.Errorf("cron jobs still running: %w", ctx.Err())
	}
}
