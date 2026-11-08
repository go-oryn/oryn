package workertest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/core/coretest"
	"github.com/go-oryn/oryn/worker"
	"github.com/go-oryn/oryn/worker/workertest"
)

// fakeTB records the fatal calls of the helpers.
type fakeTB struct {
	testing.TB

	fatal string
}

func (f *fakeTB) Fatalf(format string, args ...any) {
	f.fatal = format
}

func newPool(t *testing.T, options ...fx.Option) *worker.Pool {
	t.Helper()

	var pool *worker.Pool

	coretest.Start(t, core.New(append(options, worker.Module)...), fx.Populate(&pool))

	return pool
}

func TestRun(t *testing.T) {
	t.Parallel()

	pool := newPool(t,
		worker.AsWorker("test_ok", func(context.Context) error { return nil }),
		worker.AsWorker("test_error", func(context.Context) error { return errors.New("test_error") }),
	)

	require.NoError(t, workertest.Run(t, pool, "test_ok"))
	require.EqualError(t, workertest.Run(t, pool, "test_error"), "test_error")
}

func TestWaitForState(t *testing.T) {
	t.Parallel()

	t.Run("waits for the state", func(t *testing.T) {
		t.Parallel()

		pool := newPool(t, worker.AsWorker("test_worker", func(context.Context) error { return nil }), worker.Run())

		infos := workertest.WaitForState(t, pool, "test_worker", worker.StateStopped)
		require.Len(t, infos, 1)
		assert.Equal(t, worker.StateStopped, infos[0].State)
	})

	t.Run("fails", func(t *testing.T) {
		t.Parallel()

		pool := newPool(t, worker.AsWorker("test_worker", func(context.Context) error { return nil }))

		for name, wanted := range map[string]string{"test_missing": "not registered", "test_worker": "did not reach"} {
			tb := &fakeTB{TB: t}

			assert.Nil(t, workertest.WaitForStateWithin(tb, pool, name, worker.StateRunning, 0))
			assert.Contains(t, tb.fatal, wanted)
		}
	})
}
