package cron

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"

	"go.uber.org/fx"
)

// jobsGroup is the value group of the registered jobs.
const jobsGroup = `group:"oryn-cron-jobs"`

var (
	jobType               = reflect.TypeFor[Job]()
	jobFuncType           = reflect.TypeFor[func(context.Context) error]()
	registeredJobType     = reflect.TypeFor[registeredJob]()
	errNilJob             = errors.New("nil job")
	registrationSequence  atomic.Uint64 // orders the jobs
	componentNameSequence atomic.Uint64 // names each job uniquely
)

// registeredJob is a job registered with AsJob, numbered in registration order.
type registeredJob struct {
	sequence uint64
	name     string
	schedule string
	job      Job
	options  jobOptions
}

// AsJob registers a job named name, run on schedule:
//
//	cron.AsJob("cleanup", "0 3 * * *", NewCleanupJob)       // every day at 03:00
//	cron.AsJob("report", "*/30 * * * * *", NewReportJob)    // every 30 seconds
//	cron.AsJob("sync", "@every 5m", NewSyncJob, cron.WithTimeout(time.Minute))
//
// The schedule is a cron expression, with an optional seconds field, a descriptor (@hourly,
// @daily...), or @every followed by a Go duration. The cron.jobs.<name>.schedule config key wins
// over it. The name identifies the job in the config, the telemetry and [Scheduler.RunNow]: it
// must be unique.
//
// The job is either a value (a [Job], or a func(context.Context) error), or a constructor
// returning one, whose parameters are injected by fx.
func AsJob(name string, schedule string, job any, options ...JobOption) fx.Option {
	if strings.TrimSpace(name) == "" {
		return fx.Error(errors.New("cannot register cron job: empty name"))
	}

	component, err := newComponent(job)
	if err != nil {
		return fx.Error(fmt.Errorf("cannot register cron job %q: %w", name, err))
	}

	var opts jobOptions
	for _, option := range options {
		option(&opts)
	}

	sequence := registrationSequence.Add(1)

	build := reflect.MakeFunc(
		reflect.FuncOf([]reflect.Type{component.typ}, []reflect.Type{registeredJobType}, false),
		func(args []reflect.Value) []reflect.Value {
			return []reflect.Value{reflect.ValueOf(registeredJob{
				sequence: sequence,
				name:     name,
				schedule: schedule,
				job:      toJob(args[0]),
				options:  opts,
			})}
		},
	).Interface()

	return fx.Options(
		component.option,
		fx.Provide(fx.Annotate(build, fx.ParamTags(component.tag()), fx.ResultTags(jobsGroup))),
	)
}

// component is a job provided in the container under a unique name.
type component struct {
	option fx.Option
	typ    reflect.Type
	name   string
}

func (c component) tag() string {
	return `name:"` + c.name + `"`
}

// newComponent provides the job under a unique name. A job value is used as is; any other
// function is a constructor, whose first result must be a job.
func newComponent(value any) (component, error) {
	if value == nil {
		return component{}, errNilJob
	}

	c := component{
		typ:  reflect.TypeOf(value),
		name: fmt.Sprintf("oryn-cron-%d", componentNameSequence.Add(1)),
	}

	switch {
	case isJob(c.typ):
		constructor := reflect.MakeFunc(reflect.FuncOf(nil, []reflect.Type{c.typ}, false), func([]reflect.Value) []reflect.Value {
			return []reflect.Value{reflect.ValueOf(value)}
		}).Interface()

		c.option = fx.Provide(fx.Annotated{Name: c.name, Target: constructor})
	case c.typ.Kind() == reflect.Func && c.typ.NumOut() > 0 && isJob(c.typ.Out(0)):
		c.typ = c.typ.Out(0)
		c.option = fx.Provide(fx.Annotated{Name: c.name, Target: value})
	default:
		return component{}, fmt.Errorf("unsupported %T: expected a job or a constructor returning one", value)
	}

	return c, nil
}

func isJob(t reflect.Type) bool {
	return t.Implements(jobType) || t.ConvertibleTo(jobFuncType)
}

func toJob(v reflect.Value) Job {
	if job, ok := v.Interface().(Job); ok {
		return job
	}

	// isJob checked the conversion when registering.
	f, _ := v.Convert(jobFuncType).Interface().(func(context.Context) error)

	return JobFunc(f)
}
