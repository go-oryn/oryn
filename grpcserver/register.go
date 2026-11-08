package grpcserver

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
	servicesGroup           = `group:"oryn-grpcserver-services"`
	unaryInterceptorsGroup  = `group:"oryn-grpcserver-unary-interceptors"`
	streamInterceptorsGroup = `group:"oryn-grpcserver-stream-interceptors"`
)

var (
	unaryInterceptorType  = reflect.TypeFor[grpc.UnaryServerInterceptor]()
	streamInterceptorType = reflect.TypeFor[grpc.StreamServerInterceptor]()
	serviceType           = reflect.TypeFor[service]()
	unaryType             = reflect.TypeFor[unaryInterceptor]()
	streamType            = reflect.TypeFor[streamInterceptor]()
	errNilRegistration    = errors.New("nil service or interceptor")
	registrationSequence  atomic.Uint64 // orders the services and interceptors
	componentNameSequence atomic.Uint64 // names each service and interceptor uniquely
)

// service is a registered service implementation, numbered in registration order.
type service struct {
	sequence uint64
	desc     *grpc.ServiceDesc
	impl     any
}

// unaryInterceptor is a registered unary interceptor, numbered in registration order.
type unaryInterceptor struct {
	sequence    uint64
	interceptor grpc.UnaryServerInterceptor
}

// streamInterceptor is a registered stream interceptor, numbered in registration order.
type streamInterceptor struct {
	sequence    uint64
	interceptor grpc.StreamServerInterceptor
}

// AsService registers the implementation of the gRPC service described by desc, the
// generated XXX_ServiceDesc:
//
//	grpcserver.AsService(&pb.Users_ServiceDesc, NewUsersService)
//
// The implementation is either a value implementing the service interface, or a
// constructor returning one, whose parameters are injected by fx.
func AsService(desc *grpc.ServiceDesc, implementation any) fx.Option {
	if desc == nil {
		return fx.Error(errors.New("cannot register service: nil service description"))
	}

	handlerType := reflect.TypeOf(desc.HandlerType)
	if handlerType == nil || handlerType.Kind() != reflect.Pointer || handlerType.Elem().Kind() != reflect.Interface {
		return fx.Error(fmt.Errorf("cannot register service %s: invalid handler type %T", desc.ServiceName, desc.HandlerType))
	}

	serverInterface := handlerType.Elem()

	component, err := newComponent(implementation, func(t reflect.Type) bool { return t.Implements(serverInterface) })
	if err != nil {
		return fx.Error(fmt.Errorf("cannot register service %s: %w", desc.ServiceName, err))
	}

	sequence := registrationSequence.Add(1)

	build := reflect.MakeFunc(
		reflect.FuncOf([]reflect.Type{component.typ}, []reflect.Type{serviceType}, false),
		func(args []reflect.Value) []reflect.Value {
			return []reflect.Value{reflect.ValueOf(service{sequence: sequence, desc: desc, impl: args[0].Interface()})}
		},
	).Interface()

	return fx.Options(
		component.option,
		fx.Provide(fx.Annotate(build, fx.ParamTags(component.tag()), fx.ResultTags(servicesGroup))),
	)
}

// AsUnaryInterceptor registers unary interceptors, applied to every unary call in registration
// order, after the built-in ones: the first registered interceptor is the outermost one.
//
// An interceptor is either a value (a [grpc.UnaryServerInterceptor]), or a constructor returning
// one, whose parameters are injected by fx.
func AsUnaryInterceptor(interceptors ...any) fx.Option {
	return asInterceptors(interceptors, unaryInterceptorType, unaryType, unaryInterceptorsGroup,
		func(sequence uint64, v reflect.Value) any {
			interceptor, _ := v.Convert(unaryInterceptorType).Interface().(grpc.UnaryServerInterceptor)

			return unaryInterceptor{sequence: sequence, interceptor: interceptor}
		})
}

// AsStreamInterceptor registers stream interceptors, applied to every streaming call in
// registration order, after the built-in ones: the first registered interceptor is the outermost one.
//
// An interceptor is either a value (a [grpc.StreamServerInterceptor]), or a constructor returning
// one, whose parameters are injected by fx.
func AsStreamInterceptor(interceptors ...any) fx.Option {
	return asInterceptors(interceptors, streamInterceptorType, streamType, streamInterceptorsGroup,
		func(sequence uint64, v reflect.Value) any {
			interceptor, _ := v.Convert(streamInterceptorType).Interface().(grpc.StreamServerInterceptor)

			return streamInterceptor{sequence: sequence, interceptor: interceptor}
		})
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

// component is a service or an interceptor provided in the container under a unique name.
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
		return component{}, errNilRegistration
	}

	c := component{
		typ:  reflect.TypeOf(value),
		name: fmt.Sprintf("oryn-grpcserver-%d", componentNameSequence.Add(1)),
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
