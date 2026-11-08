// Package crontest helps testing cron jobs, without waiting for their schedule.
//
//	var scheduler *cron.Scheduler
//
//	coretest.Start(t, app, fx.Populate(&scheduler))
//
//	require.NoError(t, crontest.RunJob(t, scheduler, "cleanup"))
package crontest

import (
	"testing"

	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/cron"
)

// RunJob runs the job named name now, synchronously, with [cron.Scheduler.RunNow], and returns
// its error. The execution is traced, measured and logged like a scheduled one.
func RunJob(tb testing.TB, scheduler *cron.Scheduler, name string) error {
	tb.Helper()

	return scheduler.RunNow(tb.Context(), name)
}

// WithSchedule overrides the schedule of the job named name, for example with "@every 10ms" to
// test its scheduled runs quickly, with cron.Run().
func WithSchedule(name string, schedule string) fx.Option {
	return config.AsOptions(config.WithValues(map[string]any{
		"cron.jobs." + name + ".schedule": schedule,
	}))
}
