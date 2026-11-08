package grpcclient

import (
	"errors"
	"fmt"
	"reflect"
	"sync/atomic"

	"go.uber.org/fx"
	"google.golang.org/grpc"
)

// Value groups of the registrations.
const (
	unaryInterceptorsGroup  = `group:"oryn-grpcclient-unary-interceptors"`
	streamInterceptorsGroup = `group:"oryn-grpcclient-stream-interceptors"`
	dialOptionsGroup        = `group:"oryn-grpcclient-dial-options"`
)

var (
	unaryInterceptorType  = reflect.TypeFor[grpc.UnaryClientInterceptor]()
	streamInterceptorType = reflect.TypeFor[grpc.StreamClientInterceptor]()
	unaryType             = reflect.TypeFor[unaryInterceptor]()
	streamType            = reflect.TypeFor[streamInterceptor]()
	errNilInterceptor     = errors.New("nil interceptor")
	registrationSequence  atomic.Uint64 // orders the interceptors and dial options
	componentNameSequence atomic.Uint64 // names each interceptor uniquely
)

// unaryInterceptor is a registered unary interceptor, numbered in registration order.
type unaryInterceptor struct {
	sequence    uint64
	interceptor grpc.UnaryClientInterceptor
}

// streamInterceptor is a registered stream interceptor, numbered in registration order.
type streamInterceptor struct {
	sequence    uint64
	interceptor grpc.StreamClientInterceptor
}

// dialOptions are registered dial options of a connection, numbered in registration order.
type dialOptions struct {
	sequence uint64
	name     string
	options  []grpc.DialOption
}

// AsUnaryInterceptor registers unary interceptors, applied to every unary call of every
// connection, in registration order, after the built-in ones: the first registered interceptor
// is the outermost one.
//
// An interceptor is either a value (a [grpc.UnaryClientInterceptor]), or a constructor returning
// one, whose parameters are injected by fx.
func AsUnaryInterceptor(interceptors ...any) fx.Option {
	return asInterceptors(interceptors, unaryInterceptorType, unaryType, unaryInterceptorsGroup,
		func(sequence uint64, v reflect.Value) any {
			interceptor, _ := v.Convert(unaryInterceptorType).Interface().(grpc.UnaryClientInterceptor)

			return unaryInterceptor{sequence: sequence, interceptor: interceptor}
		})
}

// AsStreamInterceptor registers stream interceptors, applied to every streaming call of every
// connection, in registration order, after the built-in ones: the first registered interceptor
// is the outermost one.
//
// An interceptor is either a value (a [grpc.StreamClientInterceptor]), or a constructor returning
// one, whose parameters are injected by fx.
func AsStreamInterceptor(interceptors ...any) fx.Option {
	return asInterceptors(interceptors, streamInterceptorType, streamType, streamInterceptorsGroup,
		func(sequence uint64, v reflect.Value) any {
			interceptor, _ := v.Convert(streamInterceptorType).Interface().(grpc.StreamClientInterceptor)

			return streamInterceptor{sequence: sequence, interceptor: interceptor}
		})
}

// AsDialOptions registers dial options of the connection named name, applied after the ones built
// from its configuration, in registration order. For example, per-call credentials:
//
//	grpcclient.AsDialOptions("users", grpc.WithPerRPCCredentials(credentials))
func AsDialOptions(name string, options ...grpc.DialOption) fx.Option {
	registration := dialOptions{sequence: registrationSequence.Add(1), name: name, options: options}

	return fx.Supply(fx.Annotate(registration, fx.ResultTags(dialOptionsGroup)))
}

// asInterceptors provides each interceptor, then its registration built by wrap, in the group.
func asInterceptors(
	interceptors []any,
	interceptorType reflect.Type,
	registrationType reflect.Type,
	group string,
	wrap func(sequence uint64, v reflect.Value) any,
) fx.Option {
	options := make([]fx.Option, 0, 2*len(interceptors))

	for _, interceptor := range interceptors {
		component, err := newComponent(interceptor, func(t reflect.Type) bool { return t.ConvertibleTo(interceptorType) })
		if err != nil {
			return fx.Error(fmt.Errorf("cannot register interceptor: %w", err))
		}

		sequence := registrationSequence.Add(1)

		build := reflect.MakeFunc(
			reflect.FuncOf([]reflect.Type{component.typ}, []reflect.Type{registrationType}, false),
			func(args []reflect.Value) []reflect.Value {
				return []reflect.Value{reflect.ValueOf(wrap(sequence, args[0]))}
			},
		).Interface()

		options = append(options,
			component.option,
			fx.Provide(fx.Annotate(build, fx.ParamTags(component.tag()), fx.ResultTags(group))),
		)
	}

	return fx.Options(options...)
}

// component is an interceptor provided in the container under a unique name.
type component struct {
	option fx.Option
	typ    reflect.Type
	name   string
}

func (c component) tag() string {
	return `name:"` + c.name + `"`
}

// newComponent provides value under a unique name. A value accepted by isValid is used as is;
// any other function is a constructor, whose first result must be accepted by isValid.
func newComponent(value any, isValid func(reflect.Type) bool) (component, error) {
	if value == nil {
		return component{}, errNilInterceptor
	}

	c := component{
		typ:  reflect.TypeOf(value),
		name: fmt.Sprintf("oryn-grpcclient-%d", componentNameSequence.Add(1)),
	}

	switch {
	case isValid(c.typ):
		constructor := reflect.MakeFunc(reflect.FuncOf(nil, []reflect.Type{c.typ}, false), func([]reflect.Value) []reflect.Value {
			return []reflect.Value{reflect.ValueOf(value)}
		}).Interface()

		c.option = fx.Provide(fx.Annotated{Name: c.name, Target: constructor})
	case c.typ.Kind() == reflect.Func && c.typ.NumOut() > 0 && isValid(c.typ.Out(0)):
		c.typ = c.typ.Out(0)
		c.option = fx.Provide(fx.Annotated{Name: c.name, Target: value})
	default:
		return component{}, fmt.Errorf("unsupported %T: expected a value or a constructor returning one", value)
	}

	return c, nil
}
