// Package workertest tests the workers of an application, in memory.
//
// Run a worker once, synchronously, with the dependencies and the in-memory telemetry of
// the test application:
//
//	func TestOutbox(t *testing.T) {
//		var pool *worker.Pool
//
//		coretest.Start(t, app, fx.Populate(&pool))
//
//		require.NoError(t, workertest.Run(t, pool, "outbox"))
//	}
//
// Or start the workers with the application, and wait for them to reach a state:
//
//	coretest.Start(t, app, worker.Run(), fx.Populate(&pool))
//	workertest.WaitForState(t, pool, "outbox", worker.StateStopped)
package workertest

import (
	"testing"
	"time"

	"github.com/go-oryn/oryn/worker"
)

// DefaultTimeout is the timeout of [WaitForState].
const DefaultTimeout = 5 * time.Second

// Run runs the named worker once, synchronously, with the test context, and returns its error.
// The run is instrumented like with [worker.Run], but not restarted. See [worker.Pool.RunOnce].
func Run(tb testing.TB, pool *worker.Pool, name string) error {
	tb.Helper()

	return pool.RunOnce(tb.Context(), name)
}

// WaitForState waits until all the replicas of the named worker are in the state, and fails
// the test after [DefaultTimeout]. It returns the replicas.
func WaitForState(tb testing.TB, pool *worker.Pool, name string, state worker.State) []worker.Info {
	tb.Helper()

	return WaitForStateWithin(tb, pool, name, state, DefaultTimeout)
}

// WaitForStateWithin is [WaitForState] with a timeout.
func WaitForStateWithin(tb testing.TB, pool *worker.Pool, name string, state worker.State, timeout time.Duration) []worker.Info {
	tb.Helper()

	deadline := time.Now().Add(timeout)

	for {
		infos := pool.Worker(name)
		if len(infos) == 0 {
			tb.Fatalf("worker %q is not registered", name)

			return nil
		}

		if allIn(infos, state) {
			return infos
		}

		if time.Now().After(deadline) {
			tb.Fatalf("worker %q did not reach the state %q within %s: %+v", name, state, timeout, infos)

			return nil
		}

		time.Sleep(5 * time.Millisecond)
	}
}

func allIn(infos []worker.Info, state worker.State) bool {
	for _, info := range infos {
		if info.State != state {
			return false
		}
	}

	return true
}
