package worker

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"
)

// State is the state of a worker replica.
type State string

// States of a worker replica.
const (
	// StatePending means the replica has not been started yet.
	StatePending State = "pending"
	// StateRunning means the replica is running.
	StateRunning State = "running"
	// StateRestarting means the last run failed, and the replica waits for its restart.
	StateRestarting State = "restarting"
	// StateStopped means the replica is done: its run returned nil, or the application stopped.
	StateStopped State = "stopped"
	// StateFailed means the replica failed and is not restarted anymore.
	StateFailed State = "failed"
	// StateDisabled means the worker is disabled with worker.workers.<name>.enabled.
	StateDisabled State = "disabled"
)

// Info describes a worker replica.
type Info struct {
	// Name is the registered name of the worker.
	Name string
	// Replica is the index of the replica, from 0.
	Replica int
	// State is the current state.
	State State
	// Restarts is the total number of restarts.
	Restarts int
	// Err is the error of the last failed run, if any.
	Err error
}

// ErrPanic is wrapped by the error of a run that panicked.
var ErrPanic = errors.New("worker panic")

// errorTypePanic is the error.type of a run that panicked.
const errorTypePanic = "panic"

// replica is the running state of one replica of a worker.
type replica struct {
	worker   *entry
	index    int
	mu       sync.Mutex
	state    State
	restarts int
	err      error
}

// entry is a registered worker with its resolved settings.
type entry struct {
	name     string
	worker   Worker
	settings settings
	replicas []*replica
}

// Pool runs the registered workers. It is provided by [Module], and started by [Run].
type Pool struct {
	logger         *slog.Logger
	tracer         trace.Tracer
	shutdowner     fx.Shutdowner
	shutdownOnFail bool
	entries        []*entry
	byName         map[string]*entry

	runs     metric.Int64Counter
	restarts metric.Int64Counter
	active   metric.Int64UpDownCounter

	mu      sync.Mutex
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	started bool
}

// Workers returns the replicas of all the workers, in registration order.
func (p *Pool) Workers() []Info {
	var infos []Info

	for _, e := range p.entries {
		for _, r := range e.replicas {
			infos = append(infos, r.info())
		}
	}

	return infos
}

// Worker returns the replicas of the named worker, or nil if it is not registered.
func (p *Pool) Worker(name string) []Info {
	e, ok := p.byName[name]
	if !ok {
		return nil
	}

	infos := make([]Info, 0, len(e.replicas))
	for _, r := range e.replicas {
		infos = append(infos, r.info())
	}

	return infos
}

// RunOnce runs the named worker once, synchronously, in the calling goroutine: one run of its
// first replica, instrumented like in [Run], without restart. It returns the error of the run,
// wrapping [ErrPanic] if it panicked. It is meant for tests, see the workertest package.
func (p *Pool) RunOnce(ctx context.Context, name string) error {
	e, ok := p.byName[name]
	if !ok {
		return fmt.Errorf("unknown worker %q", name)
	}

	r := &replica{worker: e, index: 0, state: StatePending}

	return p.run(ctx, r)
}

// Start starts the replicas of the enabled workers, in goroutines.
// Their context is canceled by [Pool.Stop]. A pool can be started once.
func (p *Pool) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.started {
		return errors.New("worker pool already started")
	}

	p.started = true

	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	p.cancel = cancel

	for _, e := range p.entries {
		if !e.settings.enabled {
			p.logger.InfoContext(ctx, "worker disabled", "worker", e.name)

			continue
		}

		for _, r := range e.replicas {
			p.wg.Go(func() { p.supervise(runCtx, r) })
		}
	}

	return nil
}

// Stop cancels the context of the workers, and waits for them to return, until ctx is done.
func (p *Pool) Stop(ctx context.Context) error {
	p.mu.Lock()
	cancel := p.cancel
	p.mu.Unlock()

	if cancel == nil {
		return nil
	}

	cancel()

	done := make(chan struct{})

	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("workers did not stop in time: %w", ctx.Err())
	}
}

// supervise runs a replica until it is done, failed, or ctx is canceled, restarting its failed runs.
func (p *Pool) supervise(ctx context.Context, r *replica) {
	policy := r.worker.settings.restart
	consecutive := 0

	for {
		started := time.Now()
		err := p.run(ctx, r)

		if ctx.Err() != nil || err == nil {
			r.set(StateStopped, err)

			return
		}

		if policy.ResetAfter > 0 && time.Since(started) >= policy.ResetAfter {
			consecutive = 0
		}

		if !policy.Enabled || (policy.MaxRestarts > 0 && consecutive >= policy.MaxRestarts) {
			p.fail(ctx, r, err)

			return
		}

		consecutive++
		backoff := backoffDelay(policy, consecutive)

		r.restart(err)
		p.restarts.Add(ctx, 1, metric.WithAttributes(attribute.String(string(NameKey), r.worker.name)))
		p.logger.WarnContext(ctx, "worker restarting", append(r.logAttrs(),
			"error", err, "restart", consecutive, "backoff", backoff.String())...)

		if !wait(ctx, backoff) {
			r.set(StateStopped, err)

			return
		}
	}
}

// wait waits for the delay, and reports false if ctx is canceled first.
func wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// fail marks the replica failed, and shuts the application down if configured.
func (p *Pool) fail(ctx context.Context, r *replica, err error) {
	r.set(StateFailed, err)
	p.logger.ErrorContext(ctx, "worker failed", append(r.logAttrs(), "error", err)...)

	if p.shutdownOnFail {
		_ = p.shutdowner.Shutdown(fx.ExitCode(1))
	}
}

// run runs the worker once, in its own root span, recovering panics.
func (p *Pool) run(ctx context.Context, r *replica) (err error) {
	attrs := []attribute.KeyValue{NameKey.String(r.worker.name), ReplicaKey.Int(r.index)}

	ctx, span := p.tracer.Start(ctx, "worker "+r.worker.name,
		trace.WithNewRoot(),
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attrs...),
	)
	defer span.End()

	nameAttr := metric.WithAttributes(attribute.String(string(NameKey), r.worker.name))

	r.set(StateRunning, nil)
	p.active.Add(ctx, 1, nameAttr)
	p.logger.InfoContext(ctx, "worker started", r.logAttrs()...)

	defer func() {
		errorType := ""

		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%w: %v", ErrPanic, recovered)
			errorType = errorTypePanic

			p.logger.ErrorContext(ctx, "worker panic", append(r.logAttrs(), "panic", fmt.Sprint(recovered), "stack", string(debug.Stack()))...)
		}

		p.active.Add(ctx, -1, nameAttr)

		runAttrs := []attribute.KeyValue{attribute.String(string(NameKey), r.worker.name)}

		switch {
		case err != nil && ctx.Err() != nil:
			// Stopped with the application: canceled, not failed.
			p.logger.InfoContext(ctx, "worker stopped", r.logAttrs()...)
		case err != nil:
			if errorType == "" {
				errorType = fmt.Sprintf("%T", err)
			}

			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			span.SetAttributes(ErrorTypeKey.String(errorType))
			runAttrs = append(runAttrs, ErrorTypeKey.String(errorType))
		default:
			p.logger.InfoContext(ctx, "worker stopped", r.logAttrs()...)
		}

		p.runs.Add(ctx, 1, metric.WithAttributes(runAttrs...))
	}()

	return r.worker.worker.Run(ctx)
}

// backoffDelay returns the delay before the restart n (from 1): the initial backoff, doubled
// at each consecutive restart, up to the max backoff.
func backoffDelay(policy RestartPolicy, n int) time.Duration {
	delay := policy.InitialBackoff

	for range n - 1 {
		if delay >= policy.MaxBackoff/2 {
			return policy.MaxBackoff
		}

		delay *= 2
	}

	return min(delay, policy.MaxBackoff)
}

func (r *replica) set(state State, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.state = state
	if err != nil {
		r.err = err
	}
}

func (r *replica) restart(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.state = StateRestarting
	r.restarts++
	r.err = err
}

func (r *replica) info() Info {
	r.mu.Lock()
	defer r.mu.Unlock()

	return Info{Name: r.worker.name, Replica: r.index, State: r.state, Restarts: r.restarts, Err: r.err}
}

func (r *replica) logAttrs() []any {
	return []any{"worker", r.worker.name, "replica", r.index}
}

// newEntries sorts the registrations, and resolves their settings.
func newEntries(registrations []registration, resolve func(registration) (settings, error)) ([]*entry, map[string]*entry, error) {
	// fx shuffles the value groups: restore the registration order.
	slices.SortFunc(registrations, func(a, b registration) int { return cmp.Compare(a.sequence, b.sequence) })

	entries := make([]*entry, 0, len(registrations))
	byName := make(map[string]*entry, len(registrations))

	for _, reg := range registrations {
		if _, ok := byName[reg.name]; ok {
			return nil, nil, fmt.Errorf("worker %q registered twice", reg.name)
		}

		s, err := resolve(reg)
		if err != nil {
			return nil, nil, err
		}

		e := &entry{name: reg.name, worker: reg.worker, settings: s}

		state := StatePending
		if !s.enabled {
			state = StateDisabled
		}

		for i := range s.replicas {
			e.replicas = append(e.replicas, &replica{worker: e, index: i, state: state})
		}

		entries = append(entries, e)
		byName[reg.name] = e
	}

	return entries, byName, nil
}
