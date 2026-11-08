package config_test

import (
	"errors"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/go-oryn/oryn/core/config"
)

func newTestFS(t *testing.T) fstest.MapFS {
	t.Helper()

	return fstest.MapFS{
		"app.yaml": {Data: []byte(`
app:
  name: test_app
  version: 1.0.0
test_string: base
test_nested:
  first: base
  second: base
`)},
		"extra.json":      {Data: []byte(`{"test_json": "json"}`)},
		"ignored.txt":     {Data: []byte(`test_string: ignored`)},
		"prod/app.yaml":   {Data: []byte("test_string: prod\ntest_nested:\n  first: prod\n")},
		"prod/nested/a.y": {Data: []byte(`ignored`)},
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("defaults", func(t *testing.T) {
		t.Parallel()

		cfg, err := config.New(config.WithEnvironment(""))
		require.NoError(t, err)

		assert.Equal(t, config.DefaultAppName, cfg.AppName())
		assert.Equal(t, config.DefaultAppVersion, cfg.AppVersion())
		assert.Empty(t, cfg.Env())
		assert.False(t, cfg.IsTestEnv())
	})

	t.Run("loads root files", func(t *testing.T) {
		t.Parallel()

		cfg, err := config.New(config.WithEnvironment(""), config.WithFS(newTestFS(t)))
		require.NoError(t, err)

		assert.Equal(t, "test_app", cfg.AppName())
		assert.Equal(t, "1.0.0", cfg.AppVersion())
		assert.Equal(t, "base", cfg.GetString("test_string"))
		assert.Equal(t, "json", cfg.GetString("test_json"))
	})

	t.Run("environment files override root files", func(t *testing.T) {
		t.Parallel()

		cfg, err := config.New(config.WithEnvironment("PROD"), config.WithFS(newTestFS(t)))
		require.NoError(t, err)

		assert.Equal(t, "prod", cfg.Env())
		assert.Equal(t, "prod", cfg.GetString("test_string"))
		assert.Equal(t, "prod", cfg.GetString("test_nested.first"))
		assert.Equal(t, "base", cfg.GetString("test_nested.second"))
	})

	t.Run("missing environment directory", func(t *testing.T) {
		t.Parallel()

		cfg, err := config.New(config.WithEnvironment("staging"), config.WithFS(newTestFS(t)))
		require.NoError(t, err)

		assert.Equal(t, "base", cfg.GetString("test_string"))
	})

	t.Run("later file systems override earlier ones", func(t *testing.T) {
		t.Parallel()

		cfg, err := config.New(
			config.WithEnvironment(""),
			config.WithFS(newTestFS(t)),
			config.WithFS(fstest.MapFS{"other.yaml": {Data: []byte("test_string: other")}}),
		)
		require.NoError(t, err)

		assert.Equal(t, "other", cfg.GetString("test_string"))
	})

	t.Run("values have the highest precedence and keep siblings", func(t *testing.T) {
		t.Parallel()

		cfg, err := config.New(
			config.WithEnvironment(""),
			config.WithFS(newTestFS(t)),
			config.WithValues(map[string]any{
				"test_string": "value",
				"test_nested": map[string]any{"first": "value"},
			}),
		)
		require.NoError(t, err)

		assert.Equal(t, "value", cfg.GetString("test_string"))
		assert.Equal(t, "value", cfg.GetString("test_nested.first"))
		assert.Equal(t, "base", cfg.GetString("test_nested.second"))
	})

	t.Run("dotted keys in values", func(t *testing.T) {
		t.Parallel()

		cfg, err := config.New(config.WithEnvironment(""), config.WithValues(map[string]any{"test_nested.first": "dotted"}))
		require.NoError(t, err)

		assert.Equal(t, "dotted", cfg.GetString("test_nested.first"))
	})

	t.Run("maps with dotted keys are kept whole", func(t *testing.T) {
		t.Parallel()

		cfg, err := config.New(
			config.WithEnvironment(""),
			config.WithFS(fstest.MapFS{"app.yaml": {Data: []byte("test_file_map:\n  test.key: file\n")}}),
			config.WithValues(map[string]any{"test_values_map": map[string]any{"test.key": "value"}}),
		)
		require.NoError(t, err)

		assert.Equal(t, map[string]string{"test.key": "file"}, cfg.GetStringMapString("test_file_map"))
		assert.Equal(t, map[string]string{"test.key": "value"}, cfg.GetStringMapString("test_values_map"))
	})

	t.Run("invalid file", func(t *testing.T) {
		t.Parallel()

		_, err := config.New(config.WithEnvironment(""), config.WithFS(fstest.MapFS{"app.yaml": {Data: []byte("test: [")}}))
		require.ErrorContains(t, err, `cannot parse config file "app.yaml"`)
	})

	t.Run("invalid environment file", func(t *testing.T) {
		t.Parallel()

		_, err := config.New(config.WithEnvironment("prod"), config.WithFS(fstest.MapFS{"prod/app.yaml": {Data: []byte("test: [")}}))
		require.ErrorContains(t, err, `cannot parse config file "prod/app.yaml"`)
	})

	t.Run("environment directory is a file", func(t *testing.T) {
		t.Parallel()

		_, err := config.New(config.WithEnvironment("prod"), config.WithFS(fstest.MapFS{"prod": {Data: []byte("test_string: prod")}}))
		require.ErrorContains(t, err, `cannot read config directory "prod"`)
	})

	t.Run("unreadable file", func(t *testing.T) {
		t.Parallel()

		_, err := config.New(config.WithEnvironment(""), config.WithFS(unreadableFS{fstest.MapFS{"app.yaml": {}}}))
		require.ErrorContains(t, err, `cannot read config file "app.yaml": test_error`)
	})
}

// unreadableFS is a file system listing its files, but failing to read them.
type unreadableFS struct {
	fstest.MapFS
}

func (unreadableFS) ReadFile(string) ([]byte, error) {
	return nil, errors.New("test_error")
}

func TestNewWithEnvironmentVariables(t *testing.T) {
	t.Run("placeholders", func(t *testing.T) {
		t.Setenv("TEST_PLACEHOLDER", "from_env")

		cfg, err := config.New(config.WithEnvironment(""), config.WithFS(fstest.MapFS{"app.yaml": {Data: []byte(`
test_set: ${TEST_PLACEHOLDER}
test_set_with_default: ${TEST_PLACEHOLDER:default}
test_unset: ${TEST_UNSET_PLACEHOLDER}
test_unset_with_default: ${TEST_UNSET_PLACEHOLDER:default}
test_bool: ${TEST_UNSET_PLACEHOLDER:true}
`)}}))
		require.NoError(t, err)

		assert.Equal(t, "from_env", cfg.GetString("test_set"))
		assert.Equal(t, "from_env", cfg.GetString("test_set_with_default"))
		assert.Empty(t, cfg.GetString("test_unset"))
		assert.Equal(t, "default", cfg.GetString("test_unset_with_default"))
		assert.True(t, cfg.GetBool("test_bool"))
	})

	t.Run("environment variables override files", func(t *testing.T) {
		t.Setenv("APP_TEST_NESTED_FIRST", "from_env")

		cfg, err := config.New(config.WithEnvironment(""), config.WithFS(newTestFS(t)))
		require.NoError(t, err)

		assert.Equal(t, "from_env", cfg.GetString("test_nested.first"))
	})

	t.Run("values override environment variables", func(t *testing.T) {
		t.Setenv("APP_TEST_STRING", "from_env")

		cfg, err := config.New(config.WithEnvironment(""), config.WithValues(map[string]any{"test_string": "value"}))
		require.NoError(t, err)

		assert.Equal(t, "value", cfg.GetString("test_string"))
	})

	t.Run("custom prefix", func(t *testing.T) {
		t.Setenv("MY_ENV", "prod")
		t.Setenv("MY_TEST_STRING", "from_env")

		cfg, err := config.New(config.WithEnvPrefix("MY"), config.WithFS(newTestFS(t)))
		require.NoError(t, err)

		assert.Equal(t, "prod", cfg.Env())
		assert.Equal(t, "from_env", cfg.GetString("test_string"))
	})

	t.Run("environment from APP_ENV", func(t *testing.T) {
		t.Setenv("APP_ENV", "Test")

		cfg, err := config.New()
		require.NoError(t, err)

		assert.Equal(t, config.EnvTest, cfg.Env())
		assert.True(t, cfg.IsTestEnv())
	})
}

func TestGetOrDefault(t *testing.T) {
	t.Parallel()

	cfg, err := config.New(config.WithEnvironment(""), config.WithValues(map[string]any{
		"test_string":   "value",
		"test_empty":    "",
		"test_bool":     false,
		"test_int":      7,
		"test_float":    1.5,
		"test_duration": "2s",
		"test_slice":    []string{"a", "b"},
		"test_map":      map[string]any{"key": "value"},
	}))
	require.NoError(t, err)

	assert.Equal(t, "value", cfg.GetStringOrDefault("test_string", "default"))
	assert.Equal(t, "default", cfg.GetStringOrDefault("test_empty", "default"))
	assert.Equal(t, "default", cfg.GetStringOrDefault("test_missing", "default"))
	assert.False(t, cfg.GetBoolOrDefault("test_bool", true))
	assert.True(t, cfg.GetBoolOrDefault("test_missing", true))
	assert.Equal(t, 7, cfg.GetIntOrDefault("test_int", 3))
	assert.Equal(t, 3, cfg.GetIntOrDefault("test_missing", 3))
	assert.InDelta(t, 1.5, cfg.GetFloat64OrDefault("test_float", 3), 0)
	assert.InDelta(t, 3.0, cfg.GetFloat64OrDefault("test_missing", 3), 0)
	assert.Equal(t, 2*time.Second, cfg.GetDurationOrDefault("test_duration", time.Minute))
	assert.Equal(t, time.Minute, cfg.GetDurationOrDefault("test_missing", time.Minute))
	assert.Equal(t, []string{"a", "b"}, cfg.GetStringSliceOrDefault("test_slice", []string{"c"}))
	assert.Equal(t, []string{"c"}, cfg.GetStringSliceOrDefault("test_missing", []string{"c"}))
	assert.Equal(t, map[string]string{"key": "value"}, cfg.GetStringMapStringOrDefault("test_map", nil))
	assert.Equal(t, map[string]string{"k": "v"}, cfg.GetStringMapStringOrDefault("test_missing", map[string]string{"k": "v"}))
}

func TestModule(t *testing.T) {
	t.Parallel()

	var cfg *config.Config

	app := fxtest.New(
		t,
		config.Module,
		config.AsOptions(config.WithEnvironment("prod"), config.WithFS(newTestFS(t))),
		fx.Populate(&cfg),
	)
	app.RequireStart().RequireStop()

	assert.Equal(t, "prod", cfg.Env())
	assert.Equal(t, "prod", cfg.GetString("test_string"))
}

func TestModuleOptionsOrder(t *testing.T) {
	t.Parallel()

	first := config.AsOptions(config.WithValues(map[string]any{"test_string": "first"}), config.WithEnvironment("first"))
	second := config.AsOptions(config.WithValues(map[string]any{"test_string": "second"}), config.WithEnvironment("second"))

	// fx shuffles value groups: build several times to catch an order dependency.
	for range 20 {
		var cfg *config.Config

		fxtest.New(t, config.Module, second, first, fx.Populate(&cfg)).RequireStart().RequireStop()

		assert.Equal(t, "second", cfg.GetString("test_string"), "the last registered option wins")
		assert.Equal(t, "second", cfg.Env())
	}
}
