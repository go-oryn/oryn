// Package validatortest provides a configured [validator.Validator] for unit tests, without
// starting an application.
package validatortest

import (
	"testing"

	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/core/coretest"
	"github.com/go-oryn/oryn/validator"
)

// New returns a [*validator.Validator] built like in the application, with the given
// registrations (validator.AsValidation...) and config options. It runs a minimal application
// in the test environment, stopped at the end of the test.
//
//	v := validatortest.New(t, validator.AsValidation("username", NewUsernameValidation))
func New(tb testing.TB, options ...fx.Option) *validator.Validator {
	tb.Helper()

	var v *validator.Validator

	coretest.Start(tb, core.New(validator.Module), append(options, fx.NopLogger, fx.Populate(&v))...)

	return v
}
