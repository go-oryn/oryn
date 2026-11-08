package cron

import (
	"context"
	"time"
)

// Job is a unit of work run on a schedule. The context carries the span of the execution, and is
// canceled when the job times out or when the application stops.
type Job interface {
	Run(ctx context.Context) error
}

// JobFunc adapts a function to a [Job].
type JobFunc func(ctx context.Context) error

// Run calls f(ctx).
func (f JobFunc) Run(ctx context.Context) error {
	return f(ctx)
}

// JobOption configures a job registered with [AsJob]. The cron.jobs.<name>.* config keys win over
// the options.
type JobOption func(*jobOptions)

type jobOptions struct {
	timeout   time.Duration
	singleton *bool
}

// WithTimeout cancels the context of the job after timeout. 0 means no timeout.
func WithTimeout(timeout time.Duration) JobOption {
	return func(o *jobOptions) {
		o.timeout = timeout
	}
}

// WithSingleton sets whether the scheduled runs of the job can overlap: with true, the default
// set by cron.singleton, a run due while the previous one is still running is skipped.
func WithSingleton(singleton bool) JobOption {
	return func(o *jobOptions) {
		o.singleton = &singleton
	}
}
