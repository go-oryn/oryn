package worker

import "fmt"

// FailedError is reported by the [Probe] for a failed worker replica.
type FailedError struct {
	Name    string
	Replica int
	Err     error
}

func (e *FailedError) Error() string {
	return fmt.Sprintf("worker %s (replica %d) failed: %v", e.Name, e.Replica, e.Err)
}

// Unwrap returns the error of the last failed run.
func (e *FailedError) Unwrap() error {
	return e.Err
}
