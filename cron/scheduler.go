package cron

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/go-co-op/gocron/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// InstrumentationScope is the name of the tracer and meter of the cron instrumentation.
const InstrumentationScope = "github.com/go-oryn/oryn/cron"

// Metric names.
const (
	// MetricJobRuns counts the job executions, with their outcome.
	MetricJobRuns = "cron.job.runs"
	// MetricJobDuration is the duration of the job executions, in seconds.
	MetricJobDuration = "cron.job.duration"
)

// Span and metric attribute keys.
const (
	// JobNameKey is the name of the job.
	JobNameKey = attribute.Key("cron.job.name")
	// JobScheduleKey is the schedule of the job.
	JobScheduleKey = attribute.Key("cron.job.schedule")
	// JobTriggerKey is what started the execution: [TriggerSchedule] or [TriggerManual].
	JobTriggerKey = attribute.Key("cron.job.trigger")
	// JobOutcomeKey is the outcome of the execution: [OutcomeSuccess], [OutcomeFailure] or [OutcomePanic].
	JobOutcomeKey = attribute.Key("cron.job.outcome")
)

// Values of [JobTriggerKey].
const (
	TriggerSchedule = "schedule"
	TriggerManual   = "manual"
)

// Values of [JobOutcomeKey].
const (
	OutcomeSuccess = "success"
	OutcomeFailure = "failure"
	OutcomePanic   = "panic"
)

// ErrJobNotFound is returned by [Scheduler.RunNow] for an unknown job name.
var ErrJobNotFound = errors.New("cron job not found")

// JobInfo describes a registered job.
type JobInfo struct {
	// Name is the job name.
	Name string
	// Schedule is the effective schedule, after the config overrides.
	Schedule string
	// Enabled is false when the job is disabled with cron.jobs.<name>.enabled: it is not
	// scheduled, but can still be run with [Scheduler.RunNow].
	Enabled bool
	// NextRun is the time of the next scheduled run, zero when unknown: the scheduler is not
	// started, or the job is disabled.
	NextRun time.Time
}

// job is a registered job, with its effective settings.
type job struct {
	name      string
	schedule  string
	enabled   bool
	singleton bool
	timeout   time.Duration
	run       Job
	scheduled gocron.Job
}

// Scheduler runs the registered jobs on their schedule, once started by [Run], and on demand
// with [Scheduler.RunNow]. Every execution is traced, measured and logged.
type Scheduler struct {
	scheduler  gocron.Scheduler
	jobs       []*job
	byName     map[string]*job
	logger     *slog.Logger
	logEnabled bool
	tracer     trace.Tracer
	runs       metric.Int64Counter
	duration   metric.Float64Histogram

	// The context of the scheduled executions: canceled when the application stop times out.
	//nolint:containedctx // Scheduled executions have no caller context.
	ctx    context.Context
	cancel context.CancelFunc
}

// RunNow runs the job named name, synchronously, with the same instrumentation as a scheduled
// run, and returns its error. The execution span is a child of the span of ctx, if any. A
// disabled job can be run, and the scheduler does not need to be started.
func (s *Scheduler) RunNow(ctx context.Context, name string) error {
	j, ok := s.byName[name]
	if !ok {
		return fmt.Errorf("%w: %q", ErrJobNotFound, name)
	}

	return s.execute(ctx, j, TriggerManual)
}

// Jobs describes the registered jobs, in registration order.
func (s *Scheduler) Jobs() []JobInfo {
	infos := make([]JobInfo, 0, len(s.jobs))

	for _, j := range s.jobs {
		info := JobInfo{Name: j.name, Schedule: j.schedule, Enabled: j.enabled}

		if j.scheduled != nil {
			if next, err := j.scheduled.NextRun(); err == nil {
				info.NextRun = next
			}
		}

		infos = append(infos, info)
	}

	return infos
}

// execute runs a job: in its own span, with its timeout, recovering panics, and records the
// metrics and logs of the execution.
func (s *Scheduler) execute(ctx context.Context, j *job, trigger string) (err error) {
	options := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(JobNameKey.String(j.name), JobScheduleKey.String(j.schedule), JobTriggerKey.String(trigger)),
	}

	if trigger == TriggerSchedule {
		// A scheduled execution is a trace of its own.
		options = append(options, trace.WithNewRoot())
	}

	ctx, span := s.tracer.Start(ctx, "cron "+j.name, options...)
	defer span.End()

	if j.timeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, j.timeout)
		defer cancel()
	}

	s.log(ctx, slog.LevelDebug, "cron job started", "job", j.name, "trigger", trigger)

	start := time.Now()
	outcome := OutcomeSuccess

	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				outcome = OutcomePanic
				err = fmt.Errorf("cron job %q panicked: %v", j.name, recovered)

				s.logger.ErrorContext(ctx, "cron job panic", "job", j.name, "panic", fmt.Sprint(recovered), "stack", string(debug.Stack()))
			}
		}()

		err = j.run.Run(ctx)
	}()

	duration := time.Since(start)

	if err != nil && outcome == OutcomeSuccess {
		outcome = OutcomeFailure
	}

	attributes := metric.WithAttributes(JobNameKey.String(j.name), JobOutcomeKey.String(outcome))
	s.runs.Add(ctx, 1, attributes)
	s.duration.Record(ctx, duration.Seconds(), attributes)

	span.SetAttributes(JobOutcomeKey.String(outcome))

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		s.log(ctx, slog.LevelError, "cron job failed", "job", j.name, "trigger", trigger, "duration", duration, "error", err)

		return err
	}

	s.log(ctx, slog.LevelInfo, "cron job succeeded", "job", j.name, "trigger", trigger, "duration", duration)

	return nil
}

func (s *Scheduler) log(ctx context.Context, level slog.Level, msg string, args ...any) {
	if s.logEnabled {
		s.logger.Log(ctx, level, msg, args...)
	}
}
