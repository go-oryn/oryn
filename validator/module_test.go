package validator_test

import (
	"context"
	"testing"
	"time"

	playground "github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/core/coretest"
	"github.com/go-oryn/oryn/validator"
	"github.com/go-oryn/oryn/validator/validatortest"
)

// startError returns the error of an application failing to start.
func startError(t *testing.T, options ...fx.Option) error {
	t.Helper()

	app := fx.New(core.New(validator.Module).Options(append(options, coretest.TestOptions(), fx.NopLogger,
		fx.Invoke(func(*validator.Validator) {}))...)...)
	if err := app.Err(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	err := app.Start(ctx)
	if err == nil {
		require.NoError(t, app.Stop(ctx))
	}

	return err
}

// testUsernames is a dependency of a validation constructor.
type testUsernames struct {
	taken map[string]bool
}

type testPeriod struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

func TestModule(t *testing.T) {
	t.Parallel()

	t.Run("provides the validators", func(t *testing.T) {
		t.Parallel()

		var (
			v        *validator.Validator
			validate *playground.Validate
		)

		coretest.Start(t, core.New(validator.Module), fx.Populate(&v, &validate))

		assert.Same(t, validate, v.Validate())
	})

	t.Run("validations", func(t *testing.T) {
		t.Parallel()

		v := validatortest.New(t,
			fx.Supply(&testUsernames{taken: map[string]bool{"taken": true}}),
			// A constructor, with a dependency, returning a context validation.
			validator.AsValidation("username", func(usernames *testUsernames) playground.FuncCtx {
				return func(ctx context.Context, fl playground.FieldLevel) bool {
					return ctx.Err() == nil && !usernames.taken[fl.Field().String()]
				}
			}),
			// A value, without context.
			validator.AsValidation("even", func(fl playground.FieldLevel) bool { return fl.Field().Int()%2 == 0 }),
			validator.AsAlias("short", "min=1,max=3"),
		)

		require.NoError(t, v.Var(t.Context(), "free", "username"))
		require.Error(t, v.Var(t.Context(), "taken", "username"))
		require.NoError(t, v.Var(t.Context(), 2, "even"))
		require.Error(t, v.Var(t.Context(), 3, "even"))
		require.NoError(t, v.Var(t.Context(), "abc", "short"))

		var errs validator.ValidationErrors
		require.ErrorAs(t, v.Var(t.Context(), "abcd", "short"), &errs)
		assert.Equal(t, "short", errs[0].Tag)
	})

	t.Run("struct validations", func(t *testing.T) {
		t.Parallel()

		periodValidation := func(sl playground.StructLevel) {
			if period, _ := sl.Current().Interface().(testPeriod); period.End < period.Start {
				sl.ReportError(period.End, "end", "End", "after_start", "")
			}
		}

		ctxValidation := func(_ context.Context, sl playground.StructLevel) {
			if period, _ := sl.Current().Interface().(testPeriod); period.Start < 0 {
				sl.ReportError(period.Start, "start", "Start", "positive", "")
			}
		}

		v := validatortest.New(t,
			validator.AsStructValidation(periodValidation, testPeriod{}),
			validator.AsStructValidation(func() playground.StructLevelFuncCtx { return ctxValidation }, testPeriod{}),
		)

		require.NoError(t, v.Struct(t.Context(), testPeriod{Start: 1, End: 2}))

		var errs validator.ValidationErrors
		require.ErrorAs(t, v.Struct(t.Context(), testPeriod{Start: -1, End: -2}), &errs)
		assert.Equal(t, map[string]string{"start": "must satisfy positive"}, errs.Fields(),
			"the last registered struct validation of a type wins")

		require.NoError(t, v.Struct(t.Context(), testPeriod{Start: 2, End: 1}))
	})

	t.Run("field names", func(t *testing.T) {
		t.Parallel()

		type testNames struct {
			JSON   string `json:"json_name,omitempty" validate:"required"`
			Form   string `form:"form_name" json:"other" validate:"required"`
			NoTag  string `validate:"required"`
			Hidden string `json:"-" validate:"required"`
		}

		for _, tc := range []struct {
			tag    string
			fields []string
		}{
			{tag: "", fields: []string{"json_name", "other", "NoTag", "Hidden"}},
			{tag: "form", fields: []string{"JSON", "form_name", "NoTag", "Hidden"}},
		} {
			v := validatortest.New(t, config.AsOptions(config.WithValues(map[string]any{"validator.field_name_tag": tc.tag})))

			var errs validator.ValidationErrors
			require.ErrorAs(t, v.Struct(t.Context(), testNames{}), &errs, tc.tag)

			fields := make([]string, 0, len(errs))
			for _, err := range errs {
				fields = append(fields, err.Field)
			}

			assert.Equal(t, tc.fields, fields, tc.tag)
		}
	})

	t.Run("invalid registrations", func(t *testing.T) {
		t.Parallel()

		valid := func(playground.FieldLevel) bool { return true }
		structValid := func(playground.StructLevel) {}

		for name, tc := range map[string]struct {
			options []fx.Option
			err     string
		}{
			"nil validation": {
				options: []fx.Option{validator.AsValidation("test_tag", nil)},
				err:     `cannot register the validation "test_tag": nil validation`,
			},
			"not a validation": {
				options: []fx.Option{validator.AsValidation("test_tag", "test_string")},
				err:     `cannot register the validation "test_tag": unsupported string`,
			},
			"constructor not returning a validation": {
				options: []fx.Option{validator.AsValidation("test_tag", func() string { return "" })},
				err:     "unsupported func() string",
			},
			"empty tag": {
				options: []fx.Option{validator.AsValidation("", valid)},
				err:     `cannot register the validation ""`,
			},
			"restricted tag": {
				options: []fx.Option{validator.AsValidation("omitempty", valid)},
				err:     `cannot register the validation "omitempty"`,
			},
			"duplicate validation": {
				options: []fx.Option{validator.AsValidation("test_tag", valid), validator.AsValidation("test_tag", valid)},
				err:     `validation tag "test_tag" registered twice`,
			},
			"duplicate alias": {
				options: []fx.Option{validator.AsAlias("test_alias", "required"), validator.AsAlias("test_alias", "min=1")},
				err:     `validation tag "test_alias" registered twice`,
			},
			"alias and validation": {
				options: []fx.Option{validator.AsAlias("test_tag", "required"), validator.AsValidation("test_tag", valid)},
				err:     `validation tag "test_tag" registered twice`,
			},
			"empty alias tags": {
				options: []fx.Option{validator.AsAlias("test_alias", "")},
				err:     `cannot register the alias "test_alias": empty alias or tags`,
			},
			"restricted alias": {
				options: []fx.Option{validator.AsAlias("dive", "required")},
				err:     `cannot register the alias "dive"`,
			},
			"struct validation without type": {
				options: []fx.Option{validator.AsStructValidation(structValid)},
				err:     "cannot register the struct validation: no type given",
			},
			"not a struct validation": {
				options: []fx.Option{validator.AsStructValidation(valid, testPeriod{})},
				err:     "cannot register the struct validation: unsupported",
			},
		} {
			require.ErrorContains(t, startError(t, tc.options...), tc.err, name)
		}
	})
}
