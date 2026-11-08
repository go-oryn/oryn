package worker

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"
	"time"

	"go.uber.org/fx"
)

// workersGroup is the value group of the registered workers.
const workersGroup = `group:"oryn-worker-workers"`

var (
	workerType            = reflect.TypeFor[Worker]()
	workerFuncType        = reflect.TypeFor[func(context.Context) error]()
	registrationType      = reflect.TypeFor[registration]()
	errNilWorker          = errors.New("nil worker")
	registrationSequence  atomic.Uint64 // orders the workers
	componentNameSequence atomic.Uint64 // names each worker component uniquely
)

// Worker is a long-running background task. Run must block until the work is done, or until
// ctx is canceled when the application stops; it should then return promptly.
//
// A Run returning an error (or panicking) is restarted, following the restart policy. A Run
// returning nil is done, and not restarted.
type Worker interface {
	Run(ctx context.Context) error
}

// WorkerFunc is a function implementing [Worker].
type WorkerFunc func(ctx context.Context) error //nolint:revive // Named like http.HandlerFunc, worker.Func would be unclear.

// Run calls f.
func (f WorkerFunc) Run(ctx context.Context) error {
	return f(ctx)
}

// RestartPolicy controls how a failed worker run is restarted.
//
// A failed replica is restarted after a backoff, doubling from InitialBackoff up to MaxBackoff.
// After MaxRestarts consecutive failures, the replica is failed and not restarted anymore.
// The consecutive failures are reset when a run lasted at least ResetAfter.
type RestartPolicy struct {
	// Enabled restarts the failed runs. When false, a failed run fails the replica.
	Enabled bool
	// MaxRestarts is the maximum number of consecutive restarts. 0 means unlimited.
	MaxRestarts int
	// InitialBackoff is the delay before the first restart.
	InitialBackoff time.Duration
	// MaxBackoff is the maximum delay between restarts.
	MaxBackoff time.Duration
	// ResetAfter is the run duration after which the consecutive failures are reset. 0 never resets them.
	ResetAfter time.Duration
}

// Option configures a registered worker, see [AsWorker].
// The worker.workers.<name>.* config keys override them.
type Option func(*workerOptions)

// workerOptions are the options set in code. Nil fields are not set.
type workerOptions struct {
	replicas *int
	restart  *RestartPolicy
}

// WithReplicas runs n concurrent replicas of the worker, each with its own Run call.
func WithReplicas(n int) Option {
	return func(o *workerOptions) {
		o.replicas = &n
	}
}

// WithRestart sets the restart policy of the worker, instead of the worker.restart.* config.
func WithRestart(policy RestartPolicy) Option {
	return func(o *workerOptions) {
		o.restart = &policy
	}
}

// registration is a registered worker, numbered in registration order.
type registration struct {
	sequence uint64
	name     string
	worker   Worker
	options  workerOptions
}

// AsWorker registers a worker under a unique name, used in the config keys, logs and telemetry.
// Workers are started by [Run].
//
// The worker is either a value (a [Worker], or a func(context.Context) error), or a constructor
// returning one, whose parameters are injected by fx.
//
//	worker.AsWorker("outbox", NewOutboxWorker, worker.WithReplicas(2))
func AsWorker(name string, worker any, options ...Option) fx.Option {
	if name == "" {
		return fx.Error(errors.New("cannot register worker: empty name"))
	}

	component, err := newComponent(worker)
	if err != nil {
		return fx.Error(fmt.Errorf("cannot register worker %q: %w", name, err))
	}

	var opts workerOptions
	for _, option := range options {
		option(&opts)
	}

	sequence := registrationSequence.Add(1)

	build := reflect.MakeFunc(
		reflect.FuncOf([]reflect.Type{component.typ}, []reflect.Type{registrationType}, false),
		func(args []reflect.Value) []reflect.Value {
			return []reflect.Value{reflect.ValueOf(registration{
				sequence: sequence,
				name:     name,
				worker:   toWorker(args[0]),
				options:  opts,
			})}
		},
	).Interface()

	return fx.Options(
		component.option,
		fx.Provide(fx.Annotate(build, fx.ParamTags(`name:"`+component.name+`"`), fx.ResultTags(workersGroup))),
	)
}

// component is a worker provided in the container under a unique name.
type component struct {
	option fx.Option
	typ    reflect.Type
	name   string
}

// newComponent provides value under a unique name. A worker value is used as is; any other
// function is a constructor, whose first result must be a worker.
func newComponent(value any) (component, error) {
	if value == nil {
		return component{}, errNilWorker
	}

	c := component{
		typ:  reflect.TypeOf(value),
		name: fmt.Sprintf("oryn-worker-%d", componentNameSequence.Add(1)),
	}

	switch {
	case isWorker(c.typ):
		constructor := reflect.MakeFunc(reflect.FuncOf(nil, []reflect.Type{c.typ}, false), func([]reflect.Value) []reflect.Value {
			return []reflect.Value{reflect.ValueOf(value)}
		}).Interface()

		c.option = fx.Provide(fx.Annotated{Name: c.name, Target: constructor})
	case c.typ.Kind() == reflect.Func && c.typ.NumOut() > 0 && isWorker(c.typ.Out(0)):
		c.typ = c.typ.Out(0)
		c.option = fx.Provide(fx.Annotated{Name: c.name, Target: value})
	default:
		return component{}, fmt.Errorf("unsupported %T: expected a worker or a constructor returning one", value)
	}

	return c, nil
}

func isWorker(t reflect.Type) bool {
	return t.Implements(workerType) || t.ConvertibleTo(workerFuncType)
}

func toWorker(v reflect.Value) Worker {
	if v.Type().Implements(workerType) {
		return v.Interface().(Worker) //nolint:forcetypeassert // Checked by Implements.
	}

	return WorkerFunc(v.Convert(workerFuncType).Interface().(func(context.Context) error)) //nolint:forcetypeassert // Converted.
}
