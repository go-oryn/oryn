package validator_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/core/coretest"
	"github.com/go-oryn/oryn/core/otel/oteltest/metrictest"
	"github.com/go-oryn/oryn/core/otel/oteltest/spantest"
	"github.com/go-oryn/oryn/validator"
	"github.com/go-oryn/oryn/validator/validatortest"
)

type testAddress struct {
	City string `json:"city" validate:"required"`
}

type testItem struct {
	Quantity int `json:"quantity" validate:"gte=1"`
}

type testRequest struct {
	Name     string      `json:"name"               validate:"required,max=5"`
	Email    string      `json:"email,omitempty"    validate:"omitempty,email"`
	Password string      `json:"password"           validate:"omitempty,min=8"`
	Confirm  string      `json:"confirm"            validate:"omitempty,eqfield=Password"`
	Address  testAddress `json:"address"`
	Items    []testItem  `json:"items"              validate:"max=2,dive"`
}

func TestValidator(t *testing.T) {
	t.Parallel()

	t.Run("valid struct", func(t *testing.T) {
		t.Parallel()

		v := validatortest.New(t)

		require.NoError(t, v.Struct(t.Context(), testRequest{Name: "test", Address: testAddress{City: "test_city"}}))
		require.NoError(t, v.Struct(t.Context(), &testRequest{Name: "test", Address: testAddress{City: "test_city"}}))
	})

	t.Run("field errors", func(t *testing.T) {
		t.Parallel()

		v := validatortest.New(t)

		err := v.Struct(t.Context(), testRequest{
			Name:     "too_long",
			Email:    "invalid",
			Password: "secret",
			Confirm:  "other",
			Items:    []testItem{{Quantity: 1}, {Quantity: 0}, {Quantity: 1}},
		})

		var errs validator.ValidationErrors
		require.ErrorAs(t, err, &errs)
		assert.Equal(t, validator.ValidationErrors{
			{Field: "name", Tag: "max", Param: "5", Message: "must be at most 5 characters long"},
			{Field: "email", Tag: "email", Message: "must be a valid email address"},
			{Field: "password", Tag: "min", Param: "8", Message: "must be at least 8 characters long"},
			{Field: "confirm", Tag: "eqfield", Param: "Password", Message: "must be equal to Password"},
			{Field: "address.city", Tag: "required", Message: "is required"},
			{Field: "items", Tag: "max", Param: "2", Message: "must be at most 2 items long"},
		}, errs)

		assert.Equal(t, "validation failed: name: must be at most 5 characters long; email: must be a valid email address; "+
			"password: must be at least 8 characters long; confirm: must be equal to Password; address.city: is required; "+
			"items: must be at most 2 items long", err.Error())
	})

	t.Run("nested collections", func(t *testing.T) {
		t.Parallel()

		var errs validator.ValidationErrors
		require.ErrorAs(t, validatortest.New(t).Struct(t.Context(), testRequest{
			Name:    "test",
			Address: testAddress{City: "test_city"},
			Items:   []testItem{{Quantity: 1}, {Quantity: 0}},
		}), &errs)
		assert.Equal(t, map[string]string{"items[1].quantity": "must be greater than or equal to 1"}, errs.Fields())
	})

	t.Run("json", func(t *testing.T) {
		t.Parallel()

		errs := validator.ValidationErrors{
			{Field: "name", Tag: "required", Message: "is required"},
			{Field: "name", Tag: "max", Param: "5", Message: "must be at most 5"},
		}

		data, err := json.Marshal(errs)
		require.NoError(t, err)
		assert.JSONEq(t, `[
			{"field": "name", "tag": "required", "message": "is required"},
			{"field": "name", "tag": "max", "param": "5", "message": "must be at most 5"}
		]`, string(data))
		assert.Equal(t, map[string]string{"name": "is required"}, errs.Fields(), "the first error of a field is kept")
	})

	t.Run("var", func(t *testing.T) {
		t.Parallel()

		v := validatortest.New(t)

		require.NoError(t, v.Var(t.Context(), "test@example.com", "required,email"))

		var errs validator.ValidationErrors
		require.ErrorAs(t, v.Var(t.Context(), "", "required,email"), &errs)
		assert.Equal(t, validator.ValidationErrors{{Tag: "required", Message: "is required"}}, errs)
		assert.Equal(t, "validation failed: is required", errs.Error())
	})

	t.Run("invalid input", func(t *testing.T) {
		t.Parallel()

		v := validatortest.New(t)

		err := v.Struct(t.Context(), nil)
		require.Error(t, err)
		require.NotErrorAs(t, err, new(validator.ValidationErrors))

		err = v.Struct(t.Context(), (*testRequest)(nil))
		require.Error(t, err)
		require.NotErrorAs(t, err, new(validator.ValidationErrors))
	})

	t.Run("messages", func(t *testing.T) {
		t.Parallel()

		v := validatortest.New(t)

		for _, tc := range []struct {
			value   any
			tag     string
			message string
		}{
			{"invalid", "url", "must be a valid URL"},
			{"invalid", "uuid", "must be a valid UUID"},
			{"c", "oneof=a b", "must be one of: a, b"},
			{"ab", "len=3", "must be exactly 3 characters long"},
			{[]int{1}, "min=2", "must be at least 2 items long"},
			{3, "max=2", "must be at most 2"},
			{1, "gt=1", "must be greater than 1"},
			{1, "lt=1", "must be less than 1"},
			{2, "lte=1", "must be less than or equal to 1"},
			{2, "eq=1", "must be equal to 1"},
			{1, "ne=1", "must not be equal to 1"},
			{"a1", "alpha", "must contain only letters"},
			{"a-", "alphanum", "must contain only letters and digits"},
			{"a", "numeric", "must be a number"},
			{"invalid", "datetime=2006-01-02", "must be a date time with the format 2006-01-02"},
			{"invalid", "hexcolor", "must satisfy hexcolor"},
			{"invalid", "startswith=x", "must satisfy startswith=x"},
		} {
			var errs validator.ValidationErrors
			require.ErrorAs(t, v.Var(t.Context(), tc.value, tc.tag), &errs, tc.tag)
			assert.Equal(t, tc.message, errs[0].Message, tc.tag)
		}

		type testFields struct {
			A string `json:"a"`
			B string `json:"b" validate:"nefield=A"`
		}

		var errs validator.ValidationErrors
		require.ErrorAs(t, v.Struct(t.Context(), testFields{A: "x", B: "x"}), &errs)
		assert.Equal(t, "must not be equal to A", errs[0].Message)
	})
}

func TestObservability(t *testing.T) {
	t.Parallel()

	t.Run("records the failures", func(t *testing.T) {
		t.Parallel()

		var (
			v       *validator.Validator
			tracer  trace.TracerProvider
			spans   *tracetest.InMemoryExporter
			metrics *sdkmetric.ManualReader
		)

		coretest.Start(t, core.New(validator.Module), fx.Populate(&v, &tracer, &spans, &metrics))

		ctx, span := tracer.Tracer("test").Start(t.Context(), "test_span")
		require.Error(t, v.Struct(ctx, testRequest{Name: "too_long"}))
		require.Error(t, v.Var(ctx, "", "required"))
		require.NoError(t, v.Var(ctx, "test", "required"))
		span.End()

		stubs := spantest.Find(spans, spantest.WithName("test_span"))
		require.Len(t, stubs, 1)
		require.Len(t, stubs[0].Events, 2)

		event := stubs[0].Events[0]
		assert.Equal(t, validator.EventFailed, event.Name)
		assert.Contains(t, event.Attributes, validator.TypeKey.String("validator_test.testRequest"))
		assert.Contains(t, event.Attributes, validator.FieldsKey.StringSlice([]string{"name", "address.city"}))
		assert.Contains(t, stubs[0].Events[1].Attributes, validator.TypeKey.String("var"))

		assert.Equal(t, int64(1), metrictest.Int64Sum(t, metrics, validator.MetricFailures,
			validator.TypeKey.String("validator_test.testRequest"), validator.TagKey.String("max")))
		assert.Equal(t, int64(1), metrictest.Int64Sum(t, metrics, validator.MetricFailures,
			validator.TypeKey.String("validator_test.testRequest"), validator.TagKey.String("required")))
		assert.Equal(t, int64(1), metrictest.Int64Sum(t, metrics, validator.MetricFailures,
			validator.TypeKey.String("var"), validator.TagKey.String("required")))
	})

	t.Run("without span", func(t *testing.T) {
		t.Parallel()

		var (
			v       *validator.Validator
			metrics *sdkmetric.ManualReader
		)

		coretest.Start(t, core.New(validator.Module), fx.Populate(&v, &metrics))

		require.Error(t, v.Var(t.Context(), "", "required"))
		assert.Equal(t, int64(1), metrictest.Int64Sum(t, metrics, validator.MetricFailures))
	})

	t.Run("disabled", func(t *testing.T) {
		t.Parallel()

		var (
			v       *validator.Validator
			tracer  trace.TracerProvider
			spans   *tracetest.InMemoryExporter
			metrics *sdkmetric.ManualReader
		)

		coretest.Start(t, core.New(validator.Module),
			config.AsOptions(config.WithValues(map[string]any{"validator.observability.enabled": false})),
			fx.Populate(&v, &tracer, &spans, &metrics))

		ctx, span := tracer.Tracer("test").Start(t.Context(), "test_span")
		require.Error(t, v.Var(ctx, "", "required"))
		span.End()

		stubs := spantest.Find(spans, spantest.WithName("test_span"))
		require.Len(t, stubs, 1)
		assert.Empty(t, stubs[0].Events)
		metrictest.HasNoMetric(t, metrics, metrictest.WithName(validator.MetricFailures))
	})
}
