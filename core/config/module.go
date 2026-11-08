package config

import (
	"cmp"
	"slices"
	"sync/atomic"

	"go.uber.org/fx"
)

// ModuleName is the name of the config fx module.
const ModuleName = "config"

// optionsGroup is the fx value group collecting the config options.
const optionsGroup = `group:"oryn-config-options"`

// Module provides the [*Config], built from all the options registered with [AsOptions].
var Module = fx.Module(
	ModuleName,
	fx.Provide(ProvideConfig),
)

// RegisteredOption is a config [Option] registered with [AsOptions].
type RegisteredOption struct {
	sequence uint64
	option   Option
}

// optionSequence numbers the options in the order they are registered.
var optionSequence atomic.Uint64

// ProvideConfigParams are the dependencies of [ProvideConfig].
type ProvideConfigParams struct {
	fx.In

	Options []RegisteredOption `group:"oryn-config-options"`
}

// ProvideConfig builds the [*Config] from the registered options, applied in
// the order they were registered: when two options set the same thing, the
// last registered one wins.
func ProvideConfig(params ProvideConfigParams) (*Config, error) {
	registered := slices.SortedFunc(slices.Values(params.Options), func(a, b RegisteredOption) int {
		return cmp.Compare(a.sequence, b.sequence)
	})

	options := make([]Option, 0, len(registered))
	for _, option := range registered {
		options = append(options, option.option)
	}

	return New(options...)
}

// AsOptions registers config options. Any module or application can add options,
// for example to load its own config files with [WithFS].
//
// fx does not keep the order of value groups, so options are numbered when
// AsOptions is called: options registered later win over earlier ones.
func AsOptions(options ...Option) fx.Option {
	fxOptions := make([]fx.Option, 0, len(options))

	for _, option := range options {
		registered := RegisteredOption{sequence: optionSequence.Add(1), option: option}

		fxOptions = append(fxOptions, fx.Supply(fx.Annotate(registered, fx.ResultTags(optionsGroup))))
	}

	return fx.Options(fxOptions...)
}
