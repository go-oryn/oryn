// Package health checks the health of the application with probes, for the
// liveness, readiness and startup checks of an orchestrator like Kubernetes.
//
// Modules and applications register probes with [AsProbe]. The checks can be
// served over HTTP with [Serve]. Config keys:
//
//	health.timeout          maximum duration of a check (default: 5s)
//	health.server.address   address of the health HTTP server (default: :8081)
package health

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultTimeout is the default maximum duration of a check.
const DefaultTimeout = 5 * time.Second

// Kind is a kind of check.
type Kind string

const (
	// Liveness checks whether the application is alive. A failure means it should be restarted.
	Liveness Kind = "liveness"
	// Readiness checks whether the application can receive traffic.
	Readiness Kind = "readiness"
	// Startup checks whether the application has started.
	Startup Kind = "startup"
)

// Kinds lists all the kinds of check.
var Kinds = []Kind{Liveness, Readiness, Startup}

// Status is the result of a check.
type Status string

const (
	// Healthy means all the probes succeeded.
	Healthy Status = "healthy"
	// Unhealthy means at least one probe failed.
	Unhealthy Status = "unhealthy"
)

// Probe checks one part of the application, for example a database connection.
type Probe interface {
	// Name identifies the probe in the check results.
	Name() string
	// Check returns an error if the checked part is not healthy.
	Check(ctx context.Context) error
}

// ProbeResult is the result of a single probe.
type ProbeResult struct {
	Status Status `json:"status"`
	Error  string `json:"error,omitempty"`
}

// Result is the result of a check.
type Result struct {
	Status Status                 `json:"status"`
	Probes map[string]ProbeResult `json:"probes"`
}

// Healthy reports whether all the probes succeeded.
func (r Result) Healthy() bool {
	return r.Status == Healthy
}

// Checker runs the probes registered for each kind of check.
type Checker struct {
	logger       *slog.Logger
	timeout      time.Duration
	probes       map[Kind][]Probe
	shuttingDown atomic.Bool
}

// NewChecker returns a [Checker]. A zero timeout means [DefaultTimeout].
func NewChecker(logger *slog.Logger, timeout time.Duration, probes map[Kind][]Probe) *Checker {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	return &Checker{
		logger:  logger,
		timeout: timeout,
		probes:  probes,
	}
}

// Check runs, concurrently, the probes registered for kind. A panicking probe is unhealthy.
// Once [Checker.SetShuttingDown] is called, readiness checks fail so the application stops receiving traffic.
func (c *Checker) Check(ctx context.Context, kind Kind) Result {
	probes := c.probes[kind]

	result := Result{
		Status: Healthy,
		Probes: make(map[string]ProbeResult, len(probes)+1),
	}

	if kind == Readiness && c.shuttingDown.Load() {
		result.Status = Unhealthy
		result.Probes["shutdown"] = ProbeResult{Status: Unhealthy, Error: "application is shutting down"}
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)

	for _, probe := range probes {
		wg.Go(func() {
			err := runProbe(ctx, probe)

			mu.Lock()
			defer mu.Unlock()

			if err == nil {
				result.Probes[probe.Name()] = ProbeResult{Status: Healthy}

				return
			}

			c.logger.WarnContext(ctx, "health probe failed", "probe", probe.Name(), "kind", kind, "error", err)

			result.Status = Unhealthy
			result.Probes[probe.Name()] = ProbeResult{Status: Unhealthy, Error: err.Error()}
		})
	}

	wg.Wait()

	return result
}

// SetShuttingDown makes the readiness checks fail from now on.
func (c *Checker) SetShuttingDown() {
	c.shuttingDown.Store(true)
}

// runProbe runs the probe, turning a panic into an error.
func runProbe(ctx context.Context, probe Probe) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("probe panicked: %v", recovered)
		}
	}()

	return probe.Check(ctx)
}
