// Package validator validates structs and values with the rules of their `validate` tags,
// using [go-playground/validator], and returns field errors ready to send in API responses.
//
//	core.New(
//		validator.Module,
//		validator.AsValidation("username", NewUsernameValidation),
//	)
//
//	type CreateUserRequest struct {
//		Name  string `json:"name" validate:"required,max=100"`
//		Email string `json:"email" validate:"required,email"`
//	}
//
//	func NewCreateUserHandler(v *validator.Validator) http.HandlerFunc {
//		return func(w http.ResponseWriter, r *http.Request) {
//			var request CreateUserRequest
//			...
//			if err := v.Struct(r.Context(), request); err != nil {
//				// 400 with the field errors: [{"field": "email", "tag": "email", "message": "..."}]
//			}
//		}
//	}
//
// Validation failures are recorded on the active span and counted, see the documentation.
//
// [go-playground/validator]: https://github.com/go-playground/validator
package validator

import (
	"fmt"
	"reflect"
	"strings"

	playground "github.com/go-playground/validator/v10"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core/config"
)

// ModuleName is the name of the validator fx module.
const ModuleName = "validator"

// DefaultFieldNameTag is the default of validator.field_name_tag: the struct tag naming the
// fields in the errors.
const DefaultFieldNameTag = "json"

// InstrumentationScope is the OpenTelemetry instrumentation scope of the module.
const InstrumentationScope = "github.com/go-oryn/oryn/validator"

// Module provides the [*Validator], and the underlying go-playground [*playground.Validate]
// with the registered validations, struct validations and aliases.
var Module = fx.Module(
	ModuleName,
	fx.Provide(NewValidate, NewValidator),
)

// NewValidateParams are the dependencies of [NewValidate].
type NewValidateParams struct {
	fx.In

	Config            *config.Config
	Aliases           []registeredAlias            `group:"oryn-validator-aliases"`
	Validations       []registeredValidation       `group:"oryn-validator-validations"`
	StructValidations []registeredStructValidation `group:"oryn-validator-struct-validations"`
}

// NewValidate returns the go-playground [*playground.Validate]: required structs enabled,
// fields named after their validator.field_name_tag tag (json by default) in the errors, and
// the registered aliases, validations and struct validations. Duplicate or invalid
// registrations fail the application start.
func NewValidate(params NewValidateParams) (*playground.Validate, error) {
	validate := playground.New(playground.WithRequiredStructEnabled())
	validate.RegisterTagNameFunc(fieldNameFunc(params.Config.GetStringOrDefault("validator.field_name_tag", DefaultFieldNameTag)))

	// fx shuffles the value groups: restore the registration order.
	sortBySequence(params.Aliases)
	sortBySequence(params.Validations)
	sortBySequence(params.StructValidations)

	tags := map[string]bool{}

	for _, alias := range params.Aliases {
		if tags[alias.alias] {
			return nil, fmt.Errorf("validation tag %q registered twice", alias.alias)
		}

		tags[alias.alias] = true

		if err := registerAlias(validate, alias); err != nil {
			return nil, err
		}
	}

	for _, validation := range params.Validations {
		if tags[validation.tag] {
			return nil, fmt.Errorf("validation tag %q registered twice", validation.tag)
		}

		tags[validation.tag] = true

		if err := registerValidation(validate, validation); err != nil {
			return nil, err
		}
	}

	for _, validation := range params.StructValidations {
		validate.RegisterStructValidationCtx(validation.fn, validation.types...)
	}

	return validate, nil
}

// registerValidation registers a validation, turning the panic of a restricted tag into an error.
func registerValidation(validate *playground.Validate, validation registeredValidation) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("cannot register the validation %q: %v", validation.tag, recovered)
		}
	}()

	if err := validate.RegisterValidationCtx(validation.tag, validation.fn); err != nil {
		return fmt.Errorf("cannot register the validation %q: %w", validation.tag, err)
	}

	return nil
}

// registerAlias registers an alias, turning the panic of an invalid alias into an error.
func registerAlias(validate *playground.Validate, alias registeredAlias) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("cannot register the alias %q: %v", alias.alias, recovered)
		}
	}()

	if alias.alias == "" || alias.tags == "" {
		return fmt.Errorf("cannot register the alias %q: empty alias or tags", alias.alias)
	}

	validate.RegisterAlias(alias.alias, alias.tags)

	return nil
}

// fieldNameFunc names the fields after their tag: the part before the first comma. Without
// tag, or with a "-" tag, the Go field name is used.
func fieldNameFunc(tag string) playground.TagNameFunc {
	return func(field reflect.StructField) string {
		if name, _, _ := strings.Cut(field.Tag.Get(tag), ","); name != "" && name != "-" {
			return name
		}

		return field.Name
	}
}

// NewValidatorParams are the dependencies of [NewValidator].
type NewValidatorParams struct {
	fx.In

	Config        *config.Config
	Validate      *playground.Validate
	MeterProvider metric.MeterProvider
}

// NewValidator returns the [*Validator], instrumented unless validator.observability.enabled is false.
func NewValidator(params NewValidatorParams) (*Validator, error) {
	validator := &Validator{validate: params.Validate}

	if !params.Config.GetBoolOrDefault("validator.observability.enabled", true) {
		return validator, nil
	}

	failures, err := params.MeterProvider.Meter(InstrumentationScope).Int64Counter(
		MetricFailures,
		metric.WithDescription("Number of failed field validations"),
		metric.WithUnit("{failure}"),
	)
	if err != nil {
		return nil, fmt.Errorf("cannot create the %s metric: %w", MetricFailures, err)
	}

	validator.failures = failures

	return validator, nil
}
