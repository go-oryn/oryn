package validatortest_test

import (
	"testing"

	playground "github.com/go-playground/validator/v10"
	"github.com/stretchr/testify/require"

	"github.com/go-oryn/oryn/validator"
	"github.com/go-oryn/oryn/validator/validatortest"
)

func TestNew(t *testing.T) {
	t.Parallel()

	v := validatortest.New(t, validator.AsValidation("test_tag", func(fl playground.FieldLevel) bool {
		return fl.Field().String() == "test_value"
	}))

	require.NoError(t, v.Var(t.Context(), "test_value", "test_tag"))
	require.Error(t, v.Var(t.Context(), "other", "test_tag"))
}
