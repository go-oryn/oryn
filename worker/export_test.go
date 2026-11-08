package worker

import "time"

// BackoffDelay exports backoffDelay for tests.
func BackoffDelay(policy RestartPolicy, n int) time.Duration {
	return backoffDelay(policy, n)
}
