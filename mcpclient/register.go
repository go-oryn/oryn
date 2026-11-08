package mcpclient

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/fx"
)

// transportsGroup is the value group of the registered transports.
const transportsGroup = `group:"oryn-mcpclient-transports"`

var (
	transportFuncType     = reflect.TypeFor[func(context.Context) (mcp.Transport, error)]()
	namedTransportType    = reflect.TypeFor[namedTransport]()
	errNilTransport       = errors.New("nil transport")
	errEmptyName          = errors.New("empty server name")
	componentNameSequence atomic.Uint64 // names each transport uniquely
)

// TransportFunc returns a new transport to an MCP server, called once per session.
type TransportFunc func(ctx context.Context) (mcp.Transport, error)

// namedTransport is a transport registered for a server name.
type namedTransport struct {
	name      string
	transport TransportFunc
}

// AsTransport registers the transport of the server name, used instead of its streamable HTTP
// mcpclient.servers.<name>.url. Use it for other transports, like a local command over stdio:
//
//	mcpclient.AsTransport("git", func(context.Context) (mcp.Transport, error) {
//		return &mcp.CommandTransport{Command: exec.Command("mcp-server-git")}, nil
//	})
//
// The transport is either a value (a [TransportFunc], or a func(context.Context) (mcp.Transport, error)),
// or a constructor returning one, whose parameters are injected by fx. The instrumentation applies
// to every transport.
func AsTransport(name string, transport any) fx.Option {
	option, err := newTransport(name, transport)
	if err != nil {
		return fx.Error(fmt.Errorf("cannot register mcp transport: %w", err))
	}

	return option
}

// newTransport provides the transport under a unique name, then named in the transports group.
func newTransport(name string, value any) (fx.Option, error) {
	name = normalize(name)
	if name == "" {
		return nil, errEmptyName
	}

	if value == nil {
		return nil, errNilTransport
	}

	typ := reflect.TypeOf(value)
	component := fmt.Sprintf("oryn-mcpclient-%d", componentNameSequence.Add(1))

	var provide fx.Option

	switch {
	case typ.ConvertibleTo(transportFuncType):
		constructor := reflect.MakeFunc(reflect.FuncOf(nil, []reflect.Type{typ}, false), func([]reflect.Value) []reflect.Value {
			return []reflect.Value{reflect.ValueOf(value)}
		}).Interface()

		provide = fx.Provide(fx.Annotated{Name: component, Target: constructor})
	case typ.Kind() == reflect.Func && typ.NumOut() > 0 && typ.Out(0).ConvertibleTo(transportFuncType):
		typ = typ.Out(0)
		provide = fx.Provide(fx.Annotated{Name: component, Target: value})
	default:
		return nil, fmt.Errorf("unsupported %T: expected a TransportFunc or a constructor returning one", value)
	}

	build := reflect.MakeFunc(
		reflect.FuncOf([]reflect.Type{typ}, []reflect.Type{namedTransportType}, false),
		func(args []reflect.Value) []reflect.Value {
			fn, _ := args[0].Convert(transportFuncType).Interface().(func(context.Context) (mcp.Transport, error))

			return []reflect.Value{reflect.ValueOf(namedTransport{name: name, transport: fn})}
		},
	).Interface()

	return fx.Options(
		provide,
		fx.Provide(fx.Annotate(build, fx.ParamTags(`name:"`+component+`"`), fx.ResultTags(transportsGroup))),
	), nil
}

// normalize returns the server name as used in the config keys: lower case, like viper keys.
func normalize(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
