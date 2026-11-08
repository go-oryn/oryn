package crontest_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/core/coretest"
	"github.com/go-oryn/oryn/cron"
	"github.com/go-oryn/oryn/cron/crontest"
)

func TestRunJob(t *testing.T) {
	t.Parallel()

	var (
		runs      atomic.Int64
		scheduler *cron.Scheduler
	)

	job := func(context.Context) error {
		runs.Add(1)

		return nil
	}

	coretest.Start(t, core.New(cron.Module, cron.AsJob("test_job", "0 0 1 1 *", job)), fx.Populate(&scheduler))

	require.NoError(t, crontest.RunJob(t, scheduler, "test_job"))
	assert.Equal(t, int64(1), runs.Load())
}

func TestWithSchedule(t *testing.T) {
	t.Parallel()

	var runs atomic.Int64

	job := func(context.Context) error {
		runs.Add(1)

		return nil
	}

	coretest.Start(t,
		core.New(cron.Module, cron.AsJob("test_job", "0 0 1 1 *", job)),
		crontest.WithSchedule("test_job", "@every 10ms"),
		cron.Run(),
	)

	assert.Eventually(t, func() bool { return runs.Load() >= 2 }, 5*time.Second, 5*time.Millisecond)
}
