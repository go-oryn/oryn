package validator

import (
	"context"
	"errors"
	"reflect"
	"strings"

	playground "github.com/go-playground/validator/v10"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Telemetry of the validation failures.
const (
	// MetricFailures counts the failed field validations.
	MetricFailures = "validation.failures"
	// EventFailed is the span event recorded when a validation fails.
	EventFailed = "validation failed"
	// TypeKey is the validated type: the struct type, like "users.CreateRequest", or "var".
	TypeKey = attribute.Key("validation.type")
	// TagKey is the failed validation tag, like "required".
	TagKey = attribute.Key("validation.tag")
	// FieldsKey lists the failed fields, on the span event.
	FieldsKey = attribute.Key("validation.fields")
)

// typeVar is the validation.type of [Validator.Var].
const typeVar = "var"

// Validator validates structs and values, and returns [ValidationErrors] on failure.
type Validator struct {
	validate *playground.Validate
	failures metric.Int64Counter
}

// Validate returns the underlying go-playground [*playground.Validate].
func (v *Validator) Validate() *playground.Validate {
	return v.validate
}

// Struct validates the fields of s, a struct or a pointer to a struct, with the rules of their
// `validate` tags. It returns [ValidationErrors] if a field is invalid, or another error if s
// cannot be validated, like a nil pointer. The context is given to the context-aware
// validations, and its span records the failures.
func (v *Validator) Struct(ctx context.Context, s any) error {
	return v.result(ctx, typeName(s), v.validate.StructCtx(ctx, s))
}

// Var validates a single value with the tag rules, e.g. Var(ctx, email, "required,email").
// The field of the returned [FieldError] is empty.
func (v *Validator) Var(ctx context.Context, field any, tag string) error {
	return v.result(ctx, typeVar, v.validate.VarCtx(ctx, field, tag))
}

// result converts the go-playground errors to [ValidationErrors], and records them.
func (v *Validator) result(ctx context.Context, typ string, err error) error {
	var validationErrs playground.ValidationErrors
	if !errors.As(err, &validationErrs) {
		return err
	}

	fieldErrs := make(ValidationErrors, 0, len(validationErrs))
	for _, fieldErr := range validationErrs {
		fieldErrs = append(fieldErrs, newFieldError(fieldErr))
	}

	v.record(ctx, typ, fieldErrs)

	return fieldErrs
}

// record adds a span event with the failed fields, and counts the failures per tag.
// The values are never recorded: they may be sensitive.
func (v *Validator) record(ctx context.Context, typ string, errs ValidationErrors) {
	if v.failures == nil {
		return
	}

	if span := trace.SpanFromContext(ctx); span.IsRecording() {
		fields := make([]string, 0, len(errs))
		for _, err := range errs {
			fields = append(fields, err.Field)
		}

		span.AddEvent(EventFailed, trace.WithAttributes(TypeKey.String(typ), FieldsKey.StringSlice(fields)))
	}

	for _, err := range errs {
		v.failures.Add(ctx, 1, metric.WithAttributes(TypeKey.String(typ), TagKey.String(err.Tag)))
	}
}

// typeName returns the name of the struct type of s, without pointers.
func typeName(s any) string {
	t := reflect.TypeOf(s)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t == nil {
		return "nil"
	}

	return t.String()
}

// ValidationErrors are the invalid fields of a validation. It marshals to JSON as a list of
// [FieldError], ready to send in an API response.
type ValidationErrors []FieldError

// Error lists the invalid fields and their messages.
func (e ValidationErrors) Error() string {
	parts := make([]string, 0, len(e))
	for _, err := range e {
		parts = append(parts, err.Error())
	}

	return "validation failed: " + strings.Join(parts, "; ")
}

// Fields returns the invalid fields with their message, e.g. {"email": "must be a valid email address"}.
// When a field has several errors, the first one is kept.
func (e ValidationErrors) Fields() map[string]string {
	fields := make(map[string]string, len(e))
	for _, err := range e {
		if _, ok := fields[err.Field]; !ok {
			fields[err.Field] = err.Message
		}
	}

	return fields
}

// FieldError is an invalid field.
type FieldError struct {
	// Field is the path of the field, named after the validator.field_name_tag tags (json by
	// default), without the struct name: "email", "address.city", "items[0].quantity".
	// It is empty for [Validator.Var].
	Field string `json:"field"`
	// Tag is the failed validation tag: "required", "email", "max"...
	Tag string `json:"tag"`
	// Param is the parameter of the tag, like "100" for max=100.
	Param string `json:"param,omitempty"`
	// Message describes the error, in English.
	Message string `json:"message"`
}

// Error returns "<field>: <message>", or the message alone without field.
func (e FieldError) Error() string {
	if e.Field == "" {
		return e.Message
	}

	return e.Field + ": " + e.Message
}

func newFieldError(err playground.FieldError) FieldError {
	// The namespace starts with the struct name, and is empty for Var.
	_, field, _ := strings.Cut(err.Namespace(), ".")

	return FieldError{
		Field:   field,
		Tag:     err.Tag(),
		Param:   err.Param(),
		Message: message(err),
	}
}
