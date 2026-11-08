package mcpserver

import (
	"cmp"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/fx"
)

// capabilitiesGroup is the value group of the registered capabilities.
const capabilitiesGroup = `group:"oryn-mcpserver-capabilities"`

var (
	capabilityType           = reflect.TypeFor[Capability]()
	capabilityFuncType       = reflect.TypeFor[func(*mcp.Server) error]()
	registeredCapabilityType = reflect.TypeFor[registeredCapability]()
	errNilCapability         = errors.New("nil capability")
	registrationSequence     atomic.Uint64 // orders the capabilities
	componentNameSequence    atomic.Uint64 // names each capability uniquely
)

// Capability registers tools, resources or prompts on the MCP server.
type Capability interface {
	Register(server *mcp.Server) error
}

// CapabilityFunc is a func(*mcp.Server) error [Capability].
type CapabilityFunc func(server *mcp.Server) error

// Register calls f(server).
func (f CapabilityFunc) Register(server *mcp.Server) error {
	return f(server)
}

// registeredCapability is a capability, numbered in registration order.
type registeredCapability struct {
	sequence   uint64
	capability Capability
}

// AsCapability registers capabilities, applied to the MCP server in registration order.
//
// A capability is either a value (a [Capability], or a func(*mcp.Server) error), or a constructor
// returning one, whose parameters are injected by fx.
//
//	mcpserver.AsCapability(NewUserTools, NewDocsResource)
func AsCapability(capabilities ...any) fx.Option {
	options := make([]fx.Option, 0, len(capabilities))

	for _, capability := range capabilities {
		option, err := newCapability(capability)
		if err != nil {
			return fx.Error(fmt.Errorf("cannot register mcp capability: %w", err))
		}

		options = append(options, option)
	}

	return fx.Options(options...)
}

// newCapability provides the capability under a unique name, then numbered in the capabilities
// group. A capability value is used as is; any other function is a constructor, whose first result
// must be a capability.
func newCapability(value any) (fx.Option, error) {
	if value == nil {
		return nil, errNilCapability
	}

	typ := reflect.TypeOf(value)
	name := fmt.Sprintf("oryn-mcpserver-%d", componentNameSequence.Add(1))

	var provide fx.Option

	switch {
	case isCapability(typ):
		constructor := reflect.MakeFunc(reflect.FuncOf(nil, []reflect.Type{typ}, false), func([]reflect.Value) []reflect.Value {
			return []reflect.Value{reflect.ValueOf(value)}
		}).Interface()

		provide = fx.Provide(fx.Annotated{Name: name, Target: constructor})
	case typ.Kind() == reflect.Func && typ.NumOut() > 0 && isCapability(typ.Out(0)):
		typ = typ.Out(0)
		provide = fx.Provide(fx.Annotated{Name: name, Target: value})
	default:
		return nil, fmt.Errorf("unsupported %T: expected a value or a constructor returning one", value)
	}

	sequence := registrationSequence.Add(1)

	build := reflect.MakeFunc(
		reflect.FuncOf([]reflect.Type{typ}, []reflect.Type{registeredCapabilityType}, false),
		func(args []reflect.Value) []reflect.Value {
			return []reflect.Value{reflect.ValueOf(registeredCapability{sequence: sequence, capability: toCapability(args[0])})}
		},
	).Interface()

	return fx.Options(
		provide,
		fx.Provide(fx.Annotate(build, fx.ParamTags(`name:"`+name+`"`), fx.ResultTags(capabilitiesGroup))),
	), nil
}

// sortCapabilities restores the registration order: fx shuffles the value groups.
func sortCapabilities(registered []registeredCapability) []Capability {
	slices.SortFunc(registered, func(a, b registeredCapability) int { return cmp.Compare(a.sequence, b.sequence) })

	capabilities := make([]Capability, 0, len(registered))
	for _, r := range registered {
		capabilities = append(capabilities, r.capability)
	}

	return capabilities
}

func isCapability(t reflect.Type) bool {
	return t.Implements(capabilityType) || t.ConvertibleTo(capabilityFuncType)
}

func toCapability(v reflect.Value) Capability {
	if capability, ok := v.Interface().(Capability); ok {
		return capability
	}

	// isCapability checked the conversion when registering.
	capability, _ := v.Convert(capabilityFuncType).Interface().(func(*mcp.Server) error)

	return CapabilityFunc(capability)
}
