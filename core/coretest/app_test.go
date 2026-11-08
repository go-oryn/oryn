package coretest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/core/coretest"
)

// hooks records the lifecycle of an application.
type hooks struct {
	started bool
	stopped bool
}

func (h *hooks) register(lc fx.Lifecycle) {
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			h.started = true

			return nil
		},
		OnStop: func(context.Context) error {
			h.stopped = true

			return nil
		},
	})
}

func TestTestOptions(t *testing.T) {
	t.Parallel()

	t.Run("defaults", func(t *testing.T) {
		t.Parallel()

		var cfg *config.Config

		coretest.Start(t, core.New(), fx.Populate(&cfg))

		assert.True(t, cfg.IsTestEnv())
		assert.False(t, cfg.GetBool("log.console.enabled"))
		assert.False(t, cfg.GetBool("log.set_default"))
	})

	t.Run("config options of the tests win", func(t *testing.T) {
		t.Parallel()

		var cfg *config.Config

		coretest.Start(t,
			core.New(config.AsOptions(config.WithValues(map[string]any{"log.console.enabled": true}))),
			fx.Populate(&cfg),
		)

		assert.True(t, cfg.IsTestEnv())
		assert.True(t, cfg.GetBool("log.console.enabled"))
	})
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("builds without starting", func(t *testing.T) {
		t.Parallel()

		var h hooks

		app := coretest.New(t, core.New(fx.Invoke(h.register)))

		assert.False(t, h.started)

		app.RequireStart().RequireStop()

		assert.True(t, h.started)
		assert.True(t, h.stopped)
	})

	t.Run("uses the test context as base context", func(t *testing.T) {
		t.Parallel()

		var ctx context.Context

		coretest.New(t, core.New(), fx.Populate(&ctx))

		assert.Equal(t, t.Context(), ctx)
	})

	t.Run("options can replace the app dependencies", func(t *testing.T) {
		t.Parallel()

		var value string

		coretest.New(t,
			core.New(fx.Provide(func() string { return "test_app" })),
			fx.Decorate(func(string) string { return "test_value" }),
			fx.Populate(&value),
		)

		assert.Equal(t, "test_value", value)
	})
}

func TestStart(t *testing.T) {
	t.Parallel()

	var h hooks

	// Cleanups run in reverse order: this one runs after the application is stopped.
	t.Cleanup(func() {
		assert.True(t, h.stopped, "the application is stopped when the test ends")
	})

	coretest.Start(t, core.New(fx.Invoke(h.register)))

	assert.True(t, h.started)
	assert.False(t, h.stopped)
}
