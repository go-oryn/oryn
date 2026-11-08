// Package log provides the application [*slog.Logger].
//
// Records are sent to the OpenTelemetry logger provider (through the otelslog
// bridge, so they are correlated with the active span), and optionally printed
// on the console. Config keys:
//
//	log.level            minimum level: debug, info, warn or error (default: info)
//	log.source           add the source code location (default: false)
//	log.console.enabled  also print records on stdout (default: false)
//	log.console.format   text or json (default: text)
//	log.set_default      set the logger as slog.Default (default: true)
package log

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/log"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core/config"
)

// ModuleName is the name of the log fx module.
const ModuleName = "log"

// handlersGroup is the fx value group collecting the extra log handlers.
const handlersGroup = `group:"oryn-log-handlers"`

// Module provides the application [*slog.Logger].
var Module = fx.Module(
	ModuleName,
	fx.Provide(NewLogger),
)

// NewLoggerParams are the dependencies of [NewLogger].
type NewLoggerParams struct {
	fx.In

	Config   *config.Config
	Provider log.LoggerProvider
	Handlers []slog.Handler `group:"oryn-log-handlers"`
}

// NewLogger returns the application logger.
func NewLogger(params NewLoggerParams) (*slog.Logger, error) {
	cfg := params.Config

	level, err := ParseLevel(cfg.GetString("log.level"))
	if err != nil {
		return nil, err
	}

	source := cfg.GetBool("log.source")

	handlers := []slog.Handler{
		otelslog.NewHandler(
			cfg.AppName(),
			otelslog.WithLoggerProvider(params.Provider),
			otelslog.WithVersion(cfg.AppVersion()),
			otelslog.WithSource(source),
		),
	}

	if cfg.GetBool("log.console.enabled") {
		console, err := newConsoleHandler(os.Stdout, cfg.GetString("log.console.format"), source)
		if err != nil {
			return nil, err
		}

		handlers = append(handlers, console)
	}

	handlers = append(handlers, params.Handlers...)

	logger := slog.New(NewLevelHandler(level, NewFanoutHandler(handlers...)))

	if cfg.GetBoolOrDefault("log.set_default", true) {
		slog.SetDefault(logger)
	}

	return logger, nil
}

// AsHandler registers an extra [slog.Handler], built by constructor, that receives all the application records.
func AsHandler(constructor any) fx.Option {
	return fx.Provide(fx.Annotate(constructor, fx.As(new(slog.Handler)), fx.ResultTags(handlersGroup)))
}

// ParseLevel parses a level name (debug, info, warn, error, case insensitive,
// with an optional offset like "info+2"). An empty name is [slog.LevelInfo].
func ParseLevel(name string) (slog.Level, error) {
	var level slog.Level

	if strings.TrimSpace(name) == "" {
		return slog.LevelInfo, nil
	}

	if err := level.UnmarshalText([]byte(strings.TrimSpace(name))); err != nil {
		return level, fmt.Errorf("invalid log level %q: %w", name, err)
	}

	return level, nil
}

func newConsoleHandler(w io.Writer, format string, source bool) (slog.Handler, error) {
	// The level is filtered by the LevelHandler wrapping all handlers.
	options := &slog.HandlerOptions{AddSource: source, Level: slog.Level(-128)}

	switch strings.ToLower(format) {
	case "", "text":
		return slog.NewTextHandler(w, options), nil
	case "json":
		return slog.NewJSONHandler(w, options), nil
	default:
		return nil, fmt.Errorf("invalid log.console.format %q, expected text or json", format)
	}
}
