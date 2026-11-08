package log_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/core/coretest"
	"github.com/go-oryn/oryn/core/log"
	"github.com/go-oryn/oryn/core/oteltest/logtest"
)

// errorHandler is a [slog.Handler] failing on every record.
type errorHandler struct {
	slog.Handler
}

func (h errorHandler) Handle(context.Context, slog.Record) error {
	return errors.New("test_error")
}

func newBufferHandler(t *testing.T) (*bytes.Buffer, slog.Handler) {
	t.Helper()

	var buf bytes.Buffer

	return &buf, slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
}

func TestParseLevel(t *testing.T) {
	t.Parallel()

	for name, expected := range map[string]slog.Level{
		"":       slog.LevelInfo,
		"debug":  slog.LevelDebug,
		"INFO":   slog.LevelInfo,
		"WARN":   slog.LevelWarn,
		"error":  slog.LevelError,
		"info+2": slog.LevelInfo + 2,
	} {
		level, err := log.ParseLevel(name)
		require.NoError(t, err, name)
		assert.Equal(t, expected, level, name)
	}

	_, err := log.ParseLevel("verbose")
	require.ErrorContains(t, err, `invalid log level "verbose"`)
}

func TestLevelHandler(t *testing.T) {
	t.Parallel()

	buf, handler := newBufferHandler(t)
	logger := slog.New(log.NewLevelHandler(slog.LevelWarn, handler)).With("test_attr", "value").WithGroup("test_group")

	logger.Info("test_info")
	logger.Warn("test_warn", "key", "value")

	assert.NotContains(t, buf.String(), "test_info")
	assert.Contains(t, buf.String(), "msg=test_warn test_attr=value test_group.key=value")
}

func TestFanoutHandler(t *testing.T) {
	t.Parallel()

	first, firstHandler := newBufferHandler(t)
	second, secondHandler := newBufferHandler(t)

	handler := log.NewFanoutHandler(log.NewLevelHandler(slog.LevelError, firstHandler), secondHandler)
	logger := slog.New(handler).With("test_attr", "value").WithGroup("test_group")

	assert.True(t, handler.Enabled(t.Context(), slog.LevelDebug))
	assert.False(t, log.NewFanoutHandler().Enabled(t.Context(), slog.LevelError))

	logger.Info("test_info", "key", "value")
	logger.Error("test_error")

	assert.NotContains(t, first.String(), "test_info")
	assert.Contains(t, first.String(), "msg=test_error test_attr=value")
	assert.Contains(t, second.String(), "msg=test_info test_attr=value test_group.key=value")
	assert.Contains(t, second.String(), "msg=test_error")

	err := log.NewFanoutHandler(errorHandler{secondHandler}).Handle(t.Context(), slog.Record{})
	require.EqualError(t, err, "test_error")
}

func TestNewLogger(t *testing.T) {
	t.Parallel()

	t.Run("level from config", func(t *testing.T) {
		t.Parallel()

		var (
			logger *slog.Logger
			logs   *logtest.InMemoryExporter
		)

		coretest.Start(t, core.New(config.AsOptions(config.WithValues(map[string]any{"log.level": "warn"}))), fx.Populate(&logger, &logs))

		logger.Info("test_info")
		logger.Warn("test_warn")

		logtest.HasNoRecord(t, logs, logtest.WithBody("test_info"))
		logtest.HasRecord(t, logs, logtest.WithBody("test_warn"), logtest.WithLevel(slog.LevelWarn))
	})

	t.Run("extra handlers", func(t *testing.T) {
		t.Parallel()

		var logger *slog.Logger

		buf, handler := newBufferHandler(t)

		coretest.Start(t, core.New(log.AsHandler(func() slog.Handler { return handler })), fx.Populate(&logger))

		logger.Info("test_info")

		assert.Contains(t, buf.String(), "msg=test_info")
	})

	t.Run("console", func(t *testing.T) {
		t.Parallel()

		var logger *slog.Logger

		coretest.Start(t, core.New(), config.AsOptions(config.WithValues(map[string]any{
			"log.console.enabled": true,
			"log.console.format":  "json",
		})), fx.Populate(&logger))

		require.NotNil(t, logger)
	})

	t.Run("invalid config", func(t *testing.T) {
		t.Parallel()

		for name, values := range map[string]map[string]any{
			"level":          {"log.level": "verbose"},
			"console format": {"log.console.enabled": true, "log.console.format": "xml"},
		} {
			app := fx.New(core.New().Options(
				coretest.TestOptions(),
				config.AsOptions(config.WithValues(values)),
				fx.NopLogger,
				fx.Invoke(func(*slog.Logger) {}),
			)...)
			require.Error(t, app.Err(), name)
		}
	})
}
