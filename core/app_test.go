package core_test

import (
	"context"
	"log/slog"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/core/coretest"
	"github.com/go-oryn/oryn/core/otel/oteltest/logtest"
)

type ctxKey struct{}

func TestApp(t *testing.T) {
	t.Parallel()

	t.Run("provides the core dependencies", func(t *testing.T) {
		t.Parallel()

		var cfg *config.Config

		coretest.Start(t, core.New(core.WithConfigFS(fstest.MapFS{
			"app.yaml":      {Data: []byte("app:\n  name: test_app\n")},
			"test/app.yaml": {Data: []byte("app:\n  version: 1.0.0-test\n")},
		})), fx.Populate(&cfg))

		assert.Equal(t, config.EnvTest, cfg.Env())
		assert.Equal(t, "test_app", cfg.AppName())
		assert.Equal(t, "1.0.0-test", cfg.AppVersion())
	})

	t.Run("supplies the base context", func(t *testing.T) {
		t.Parallel()

		var ctx context.Context

		app := core.New().WithContext(context.WithValue(t.Context(), ctxKey{}, "test_value"))

		testApp := fx.New(app.Options(coretest.TestOptions(), fx.NopLogger, fx.Populate(&ctx))...)
		require.NoError(t, testApp.Err())

		assert.Equal(t, "test_value", ctx.Value(ctxKey{}))
	})

	t.Run("is immutable", func(t *testing.T) {
		t.Parallel()

		base := core.New(fx.Supply(1))
		first := base.WithOptions(fx.Supply("first"))
		second := base.WithOptions(fx.Supply(true))

		assert.Len(t, base.Options(), 3)
		assert.Len(t, first.Options(), 4)
		assert.Len(t, second.Options(), 4)

		var value string

		coretest.Start(t, first, fx.Populate(&value))

		assert.Equal(t, "first", value)
	})

	t.Run("logs fx events at debug level", func(t *testing.T) {
		t.Parallel()

		var logs *logtest.InMemoryExporter

		coretest.Start(t, core.New(config.AsOptions(config.WithValues(map[string]any{"log.level": "debug"}))), fx.Populate(&logs))

		logtest.HasRecord(t, logs, logtest.WithBody("started"), logtest.WithLevel(slog.LevelDebug))
	})

	t.Run("build failure", func(t *testing.T) {
		t.Parallel()

		app := core.New(fx.Invoke(func(string) {})).Build(coretest.TestOptions(), fx.NopLogger)

		require.ErrorContains(t, app.Err(), "missing type: string")
	})
	t.Run("runs until shutdown", func(t *testing.T) {
		t.Parallel()

		var stopped bool

		core.New().Run(
			coretest.TestOptions(),
			fx.NopLogger,
			fx.Invoke(func(lc fx.Lifecycle, shutdowner fx.Shutdowner) {
				lc.Append(fx.Hook{
					OnStart: func(context.Context) error { return shutdowner.Shutdown() },
					OnStop: func(context.Context) error {
						stopped = true

						return nil
					},
				})
			}),
		)

		assert.True(t, stopped)
	})
}
