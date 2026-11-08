package validator

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync/atomic"

	playground "github.com/go-playground/validator/v10"
	"go.uber.org/fx"
)

// Value groups of the registrations.
const (
	aliasesGroup           = `group:"oryn-validator-aliases"`
	validationsGroup       = `group:"oryn-validator-validations"`
	structValidationsGroup = `group:"oryn-validator-struct-validations"`
)

var (
	funcCtxType                  = reflect.TypeFor[playground.FuncCtx]()
	funcType                     = reflect.TypeFor[playground.Func]()
	structLevelFuncCtxType       = reflect.TypeFor[playground.StructLevelFuncCtx]()
	structLevelFuncType          = reflect.TypeFor[playground.StructLevelFunc]()
	registeredValidationType     = reflect.TypeFor[registeredValidation]()
	registeredStructValidationTy = reflect.TypeFor[registeredStructValidation]()
	errNilRegistration           = errors.New("nil validation")
	registrationSequence         atomic.Uint64 // orders the registrations
	componentNameSequence        atomic.Uint64 // names each validation uniquely
)

type registeredAlias struct {
	sequence uint64
	alias    string
	tags     string
}

type registeredValidation struct {
	sequence uint64
	tag      string
	fn       playground.FuncCtx
}

type registeredStructValidation struct {
	sequence uint64
	fn       playground.StructLevelFuncCtx
	types    []any
}

func (r registeredAlias) order() uint64            { return r.sequence }
func (r registeredValidation) order() uint64       { return r.sequence }
func (r registeredStructValidation) order() uint64 { return r.sequence }

func sortBySequence[T interface{ order() uint64 }](registrations []T) {
	slices.SortFunc(registrations, func(a, b T) int { return cmp.Compare(a.order(), b.order()) })
}

// AsAlias registers an alias for a list of validation tags, e.g. AsAlias("iscolor", "hexcolor|rgb|rgba").
func AsAlias(alias string, tags string) fx.Option {
	return fx.Supply(fx.Annotate(
		registeredAlias{sequence: registrationSequence.Add(1), alias: alias, tags: tags},
		fx.ResultTags(aliasesGroup),
	))
}

// AsValidation registers a field validation for the tag, used as `validate:"<tag>"` or
// `validate:"<tag>=<param>"`.
//
// The validation is either a value (a func(context.Context, validator.FieldLevel) bool, or a
// func(validator.FieldLevel) bool, of go-playground/validator), or a constructor returning one,
// whose parameters are injected by fx: a validation can use a repository or a client. The
// context version receives the context given to [Validator.Struct] or [Validator.Var].
func AsValidation(tag string, validation any) fx.Option {
	component, err := newComponent(validation, isValidation)
	if err != nil {
		return fx.Error(fmt.Errorf("cannot register the validation %q: %w", tag, err))
	}

	sequence := registrationSequence.Add(1)

	build := reflect.MakeFunc(
		reflect.FuncOf([]reflect.Type{component.typ}, []reflect.Type{registeredValidationType}, false),
		func(args []reflect.Value) []reflect.Value {
			return []reflect.Value{reflect.ValueOf(registeredValidation{
				sequence: sequence,
				tag:      tag,
				fn:       toValidation(args[0]),
			})}
		},
	).Interface()

	return fx.Options(
		component.option,
		fx.Provide(fx.Annotate(build, fx.ParamTags(component.tag()), fx.ResultTags(validationsGroup))),
	)
}

// AsStructValidation registers a struct level validation for the types of the given values,
// to validate fields together, e.g. AsStructValidation(NewPeriodValidation, Period{}).
//
// The validation is either a value (a func(context.Context, validator.StructLevel), or a
// func(validator.StructLevel), of go-playground/validator), or a constructor returning one,
// whose parameters are injected by fx. It reports errors with StructLevel.ReportError.
func AsStructValidation(validation any, types ...any) fx.Option {
	if len(types) == 0 {
		return fx.Error(errors.New("cannot register the struct validation: no type given"))
	}

	component, err := newComponent(validation, isStructValidation)
	if err != nil {
		return fx.Error(fmt.Errorf("cannot register the struct validation: %w", err))
	}

	sequence := registrationSequence.Add(1)

	build := reflect.MakeFunc(
		reflect.FuncOf([]reflect.Type{component.typ}, []reflect.Type{registeredStructValidationTy}, false),
		func(args []reflect.Value) []reflect.Value {
			return []reflect.Value{reflect.ValueOf(registeredStructValidation{
				sequence: sequence,
				fn:       toStructValidation(args[0]),
				types:    types,
			})}
		},
	).Interface()

	return fx.Options(
		component.option,
		fx.Provide(fx.Annotate(build, fx.ParamTags(component.tag()), fx.ResultTags(structValidationsGroup))),
	)
}

// component is a validation provided in the container under a unique name.
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
		name: fmt.Sprintf("oryn-validator-%d", componentNameSequence.Add(1)),
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
		return component{}, fmt.Errorf("unsupported %T: expected a validation function or a constructor returning one", value)
	}

	return c, nil
}

func isValidation(t reflect.Type) bool {
	return t.ConvertibleTo(funcCtxType) || t.ConvertibleTo(funcType)
}

func isStructValidation(t reflect.Type) bool {
	return t.ConvertibleTo(structLevelFuncCtxType) || t.ConvertibleTo(structLevelFuncType)
}

func toValidation(v reflect.Value) playground.FuncCtx {
	if v.Type().ConvertibleTo(funcCtxType) {
		fn, _ := v.Convert(funcCtxType).Interface().(playground.FuncCtx)

		return fn
	}

	fn, _ := v.Convert(funcType).Interface().(playground.Func)

	return playground.FuncCtx(func(_ context.Context, fl playground.FieldLevel) bool { return fn(fl) })
}

func toStructValidation(v reflect.Value) playground.StructLevelFuncCtx {
	if v.Type().ConvertibleTo(structLevelFuncCtxType) {
		fn, _ := v.Convert(structLevelFuncCtxType).Interface().(playground.StructLevelFuncCtx)

		return fn
	}

	fn, _ := v.Convert(structLevelFuncType).Interface().(playground.StructLevelFunc)

	return playground.StructLevelFuncCtx(func(_ context.Context, sl playground.StructLevel) { fn(sl) })
}
