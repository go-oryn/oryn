package httpclient

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sync/atomic"

	"go.uber.org/fx"
)

// middlewaresGroup is the value group of the registered middlewares.
const middlewaresGroup = `group:"oryn-httpclient-middlewares"`

var (
	middlewareType           = reflect.TypeFor[Middleware]()
	middlewareFuncType       = reflect.TypeFor[func(http.RoundTripper) http.RoundTripper]()
	registeredMiddlewareType = reflect.TypeFor[registeredMiddleware]()
	errNilMiddleware         = errors.New("nil middleware")
	registrationSequence     atomic.Uint64 // orders the middlewares
	componentNameSequence    atomic.Uint64 // names each middleware uniquely
)

// registeredMiddleware is a middleware, numbered in registration order.
type registeredMiddleware struct {
	sequence   uint64
	middleware Middleware
}

// AsMiddleware registers transport middlewares, applied to every client of the [Factory] in
// registration order: the first registered middleware is the outermost one.
//
// A middleware is either a value (a [Middleware], or a func(http.RoundTripper) http.RoundTripper),
// or a constructor returning one, whose parameters are injected by fx.
func AsMiddleware(middlewares ...any) fx.Option {
	options := make([]fx.Option, 0, 2*len(middlewares))

	for _, middleware := range middlewares {
		name := fmt.Sprintf("oryn-httpclient-%d", componentNameSequence.Add(1))

		typ, option, err := newComponent(middleware, name)
		if err != nil {
			return fx.Error(fmt.Errorf("cannot register http client middleware: %w", err))
		}

		sequence := registrationSequence.Add(1)

		build := reflect.MakeFunc(
			reflect.FuncOf([]reflect.Type{typ}, []reflect.Type{registeredMiddlewareType}, false),
			func(args []reflect.Value) []reflect.Value {
				return []reflect.Value{reflect.ValueOf(registeredMiddleware{sequence: sequence, middleware: toMiddleware(args[0])})}
			},
		).Interface()

		options = append(options,
			option,
			fx.Provide(fx.Annotate(build, fx.ParamTags(`name:"`+name+`"`), fx.ResultTags(middlewaresGroup))),
		)
	}

	return fx.Options(options...)
}

// newComponent provides the middleware under the name: a middleware value as is, or a
// constructor whose first result is a middleware.
func newComponent(value any, name string) (reflect.Type, fx.Option, error) {
	if value == nil {
		return nil, nil, errNilMiddleware
	}

	typ := reflect.TypeOf(value)

	switch {
	case isMiddleware(typ):
		constructor := reflect.MakeFunc(reflect.FuncOf(nil, []reflect.Type{typ}, false), func([]reflect.Value) []reflect.Value {
			return []reflect.Value{reflect.ValueOf(value)}
		}).Interface()

		return typ, fx.Provide(fx.Annotated{Name: name, Target: constructor}), nil
	case typ.Kind() == reflect.Func && typ.NumOut() > 0 && isMiddleware(typ.Out(0)):
		return typ.Out(0), fx.Provide(fx.Annotated{Name: name, Target: value}), nil
	default:
		return nil, nil, fmt.Errorf("unsupported %T: expected a middleware or a constructor returning one", value)
	}
}

func isMiddleware(t reflect.Type) bool {
	return t.Implements(middlewareType) || t.ConvertibleTo(middlewareFuncType)
}

func toMiddleware(v reflect.Value) Middleware {
	if middleware, ok := v.Interface().(Middleware); ok {
		return middleware
	}

	// isMiddleware checked the conversion when registering.
	middleware, _ := v.Convert(middlewareFuncType).Interface().(func(http.RoundTripper) http.RoundTripper)

	return MiddlewareFunc(middleware)
}
