package httpserver

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"

	"go.uber.org/fx"
)

// Value groups of the registrations.
const (
	middlewaresGroup = `group:"oryn-httpserver-middlewares"`
	routesGroup      = `group:"oryn-httpserver-routes"`
)

var (
	handlerType              = reflect.TypeFor[http.Handler]()
	handlerFuncType          = reflect.TypeFor[func(http.ResponseWriter, *http.Request)]()
	middlewareType           = reflect.TypeFor[Middleware]()
	middlewareFuncType       = reflect.TypeFor[func(http.Handler) http.Handler]()
	registeredMiddlewareType = reflect.TypeFor[registeredMiddleware]()
	routeType                = reflect.TypeFor[route]()
	errNilRegistration       = errors.New("nil handler or middleware")
	errGroupPatternWithHost  = errors.New("the patterns of a handler group must not have a host")
	registrationSequence     atomic.Uint64 // orders the routes and global middlewares
	componentNameSequence    atomic.Uint64 // names each handler and middleware uniquely
)

// registeredMiddleware is a global middleware, numbered in registration order.
type registeredMiddleware struct {
	sequence   uint64
	middleware Middleware
}

// route is a registered handler with its own middlewares (group ones first), numbered in registration order.
type route struct {
	sequence    uint64
	pattern     string
	handler     http.Handler
	middlewares []Middleware
}

// AsMiddleware registers global middlewares, applied to every handler in registration order:
// the first registered middleware is the outermost one.
//
// A middleware is either a value (a [Middleware], or a func(http.Handler) http.Handler), or a
// constructor returning one, whose parameters are injected by fx.
func AsMiddleware(middlewares ...any) fx.Option {
	options := make([]fx.Option, 0, len(middlewares))

	for _, middleware := range middlewares {
		component, err := newComponent(middleware, isMiddleware)
		if err != nil {
			return fx.Error(fmt.Errorf("cannot register middleware: %w", err))
		}

		sequence := registrationSequence.Add(1)

		build := reflect.MakeFunc(
			reflect.FuncOf([]reflect.Type{component.typ}, []reflect.Type{registeredMiddlewareType}, false),
			func(args []reflect.Value) []reflect.Value {
				return []reflect.Value{reflect.ValueOf(registeredMiddleware{
					sequence:   sequence,
					middleware: toMiddleware(args[0]),
				})}
			},
		).Interface()

		options = append(options,
			component.option,
			fx.Provide(fx.Annotate(build, fx.ParamTags(component.tag()), fx.ResultTags(middlewaresGroup))),
		)
	}

	return fx.Options(options...)
}

// AsHandler registers a handler for the [http.ServeMux] pattern (e.g. "GET /users/{id}"),
// with optional middlewares applied to this handler only, after the global ones.
//
// The handler is either a value (an [http.Handler], or a func(http.ResponseWriter, *http.Request)),
// or a constructor returning one, whose parameters are injected by fx. Middlewares are
// described in [AsMiddleware].
func AsHandler(pattern string, handler any, middlewares ...any) fx.Option {
	return newGroup().handler(pattern, handler, middlewares).option()
}

// AsHandlerGroup registers a group of handlers sharing a path prefix (e.g. "/api/v1") and middlewares.
// The patterns of the group handlers are relative to the prefix: "GET /users" in the group
// "/api" serves "GET /api/users".
//
//	httpserver.AsHandlerGroup("/api",
//		httpserver.WithMiddlewares(NewAuthMiddleware),
//		httpserver.WithHandler("GET /users", NewListUsersHandler),
//		httpserver.WithHandler("POST /users", NewCreateUserHandler, NewAuditMiddleware),
//		httpserver.WithGroup("/admin",
//			httpserver.WithMiddlewares(NewAdminMiddleware),
//			httpserver.WithHandler("GET /stats", NewStatsHandler),
//		),
//	)
func AsHandlerGroup(prefix string, options ...GroupOption) fx.Option {
	return newGroup().group(prefix, options).option()
}

// GroupOption configures a handler group, see [AsHandlerGroup].
type GroupOption func(*group)

// WithMiddlewares adds middlewares to all the handlers of the group, including its sub groups,
// wherever the option is placed. They are applied after the global and parent group middlewares.
func WithMiddlewares(middlewares ...any) GroupOption {
	return func(g *group) {
		g.middlewares = append(g.middlewares, middlewares...)
	}
}

// WithHandler adds a handler to the group, with optional middlewares applied to this handler only,
// after the group middlewares. See [AsHandler].
func WithHandler(pattern string, handler any, middlewares ...any) GroupOption {
	return func(g *group) {
		g.entries = append(g.entries, func(g *group) *group { return g.handler(pattern, handler, middlewares) })
	}
}

// WithGroup adds a sub group, whose prefix is relative to the group one, and whose handlers
// also get the group middlewares. See [AsHandlerGroup].
func WithGroup(prefix string, options ...GroupOption) GroupOption {
	return func(g *group) {
		g.entries = append(g.entries, func(g *group) *group { return g.group(prefix, options) })
	}
}

// group collects the fx options of a handler group.
type group struct {
	prefix      string
	middlewares []any
	entries     []func(*group) *group

	// Resolved components of the middlewares, inherited from the parent groups.
	inherited []component
	options   *[]fx.Option
	err       *error
}

// newGroup returns the root group, without prefix nor middlewares.
func newGroup() *group {
	return &group{options: new([]fx.Option), err: new(error)}
}

// option returns the fx options of the group, or the first registration error.
func (g *group) option() fx.Option {
	if *g.err != nil {
		return fx.Error(*g.err)
	}

	return fx.Options(*g.options...)
}

func (g *group) fail(err error) *group {
	if *g.err == nil {
		*g.err = err
	}

	return g
}

// group registers a sub group: its middlewares, then its handlers and sub groups, in order.
func (g *group) group(prefix string, options []GroupOption) *group {
	if prefix != "" && !strings.HasPrefix(prefix, "/") {
		return g.fail(fmt.Errorf("invalid handler group prefix %q: it must start with /", prefix))
	}

	sub := &group{
		prefix:    g.prefix + strings.TrimSuffix(prefix, "/"),
		inherited: append([]component(nil), g.inherited...),
		options:   g.options,
		err:       g.err,
	}

	for _, option := range options {
		option(sub)
	}

	for _, middleware := range sub.middlewares {
		component, err := newComponent(middleware, isMiddleware)
		if err != nil {
			return g.fail(fmt.Errorf("cannot register middleware of the handler group %q: %w", sub.prefix, err))
		}

		*g.options = append(*g.options, component.option)
		sub.inherited = append(sub.inherited, component)
	}

	for _, entry := range sub.entries {
		entry(sub)
	}

	return g
}

// handler registers a route: the handler, its own middlewares, and the route built from them.
func (g *group) handler(pattern string, handler any, middlewares []any) *group {
	fullPattern, err := joinPattern(g.prefix, pattern)
	if err != nil {
		return g.fail(err)
	}

	handlerComponent, err := newComponent(handler, isHandler)
	if err != nil {
		return g.fail(fmt.Errorf("cannot register handler %q: %w", fullPattern, err))
	}

	components := append([]component{handlerComponent}, g.inherited...)
	*g.options = append(*g.options, handlerComponent.option)

	for _, middleware := range middlewares {
		component, err := newComponent(middleware, isMiddleware)
		if err != nil {
			return g.fail(fmt.Errorf("cannot register middleware of the handler %q: %w", fullPattern, err))
		}

		*g.options = append(*g.options, component.option)
		components = append(components, component)
	}

	types := make([]reflect.Type, 0, len(components))
	tags := make([]string, 0, len(components))

	for _, component := range components {
		types = append(types, component.typ)
		tags = append(tags, component.tag())
	}

	sequence := registrationSequence.Add(1)

	build := reflect.MakeFunc(
		reflect.FuncOf(types, []reflect.Type{routeType}, false),
		func(args []reflect.Value) []reflect.Value {
			r := route{sequence: sequence, pattern: fullPattern, handler: toHandler(args[0])}

			for _, arg := range args[1:] {
				r.middlewares = append(r.middlewares, toMiddleware(arg))
			}

			return []reflect.Value{reflect.ValueOf(r)}
		},
	).Interface()

	*g.options = append(*g.options, fx.Provide(fx.Annotate(build, fx.ParamTags(tags...), fx.ResultTags(routesGroup))))

	return g
}

// joinPattern prefixes the path of the pattern "[METHOD ][HOST]/PATH".
func joinPattern(prefix string, pattern string) (string, error) {
	if prefix == "" {
		return pattern, nil
	}

	method, path, found := strings.Cut(strings.TrimSpace(pattern), " ")
	if !found {
		method, path = "", method
	}

	path = strings.TrimLeft(path, " \t")

	if !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("invalid pattern %q in the handler group %q: %w", pattern, prefix, errGroupPatternWithHost)
	}

	if method == "" {
		return prefix + path, nil
	}

	return method + " " + prefix + path, nil
}

// component is a handler or a middleware provided in the container under a unique name.
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
		name: fmt.Sprintf("oryn-httpserver-%d", componentNameSequence.Add(1)),
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

func isHandler(t reflect.Type) bool {
	return t.Implements(handlerType) || t.ConvertibleTo(handlerFuncType)
}

func isMiddleware(t reflect.Type) bool {
	return t.Implements(middlewareType) || t.ConvertibleTo(middlewareFuncType)
}

func toHandler(v reflect.Value) http.Handler {
	if handler, ok := v.Interface().(http.Handler); ok {
		return handler
	}

	// isHandler checked the conversion when registering.
	handler, _ := v.Convert(handlerFuncType).Interface().(func(http.ResponseWriter, *http.Request))

	return http.HandlerFunc(handler)
}

func toMiddleware(v reflect.Value) Middleware {
	if middleware, ok := v.Interface().(Middleware); ok {
		return middleware
	}

	// isMiddleware checked the conversion when registering.
	middleware, _ := v.Convert(middlewareFuncType).Interface().(func(http.Handler) http.Handler)

	return MiddlewareFunc(middleware)
}
