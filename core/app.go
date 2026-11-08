// Package core is the entry point of an oryn application.
//
// It bundles the config, OpenTelemetry, logging and health modules, and runs
// the application with fx:
//
//	func main() {
//		core.New(
//			core.WithConfigFS(configs.FS),
//			api.Module,
//		).Run()
//	}
package core

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"

	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"

	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/core/health"
	"github.com/go-oryn/oryn/core/log"
	"github.com/go-oryn/oryn/core/otel"
)

// ModuleName is the name of the core fx module.
const ModuleName = "core"

// Module provides the config, OpenTelemetry, logger and health checker.
var Module = fx.Options(
	fx.Module(
		ModuleName,
		config.Module,
		otel.Module,
		log.Module,
		health.Module,
	),
	fx.WithLogger(NewFxLogger),
)

// App describes an oryn application: the core module, a base context, and the
// fx options of the application modules. App is immutable: its methods return copies.
type App struct {
	//nolint:containedctx // The base context is supplied to the fx container.
	ctx     context.Context
	options []fx.Option
}

// New returns an [App] made of the core [Module] and the given options.
func New(options ...fx.Option) App {
	return App{
		ctx:     context.Background(),
		options: options,
	}
}

// WithContext returns a copy of the app using ctx as the base context.
// The base context is available in the fx container as a [context.Context].
func (a App) WithContext(ctx context.Context) App {
	a.ctx = ctx

	return a
}

// WithOptions returns a copy of the app with additional fx options.
func (a App) WithOptions(options ...fx.Option) App {
	a.options = append(a.options[:len(a.options):len(a.options)], options...)

	return a
}

// Options returns all the fx options of the app: the core module, the base
// context, the app options, and the given options.
func (a App) Options(options ...fx.Option) []fx.Option {
	all := make([]fx.Option, 0, len(a.options)+len(options)+2)
	all = append(all, Module, fx.Supply(fx.Annotate(a.ctx, fx.As(new(context.Context)))))
	all = append(all, a.options...)

	return append(all, options...)
}

// Build returns the [*fx.App], with the given additional options.
func (a App) Build(options ...fx.Option) *fx.App {
	return fx.New(a.Options(options...)...)
}

// Run builds and starts the application, then blocks until it receives a
// shutdown signal (SIGINT, SIGTERM) and stops it. It exits the process on failure.
func (a App) Run(options ...fx.Option) {
	a.Build(options...).Run()
}

// RunOnce builds and starts the application, then stops it right away: for one-shot
// commands, like applying database migrations, whose work is done by start hooks.
// It exits the process on failure.
func (a App) RunOnce(options ...fx.Option) {
	if err := a.runOnce(options...); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// runOnce starts then stops the application, returning the first error.
func (a App) runOnce(options ...fx.Option) error {
	app := a.Build(options...)
	if err := app.Err(); err != nil {
		return err
	}

	startCtx, cancelStart := context.WithTimeout(a.ctx, app.StartTimeout())
	defer cancelStart()

	if err := app.Start(startCtx); err != nil {
		return err
	}

	stopCtx, cancelStop := context.WithTimeout(context.WithoutCancel(a.ctx), app.StopTimeout())
	defer cancelStop()

	return app.Stop(stopCtx)
}

// WithConfigFS loads the config files of fsys, see [config.WithFS].
func WithConfigFS(fsys fs.FS) fx.Option {
	return config.AsOptions(config.WithFS(fsys))
}

// NewFxLogger logs the fx events (provides, invokes, lifecycle hooks) with
// the application logger, at debug level.
func NewFxLogger(logger *slog.Logger) fxevent.Logger {
	fxLogger := &fxevent.SlogLogger{Logger: logger}
	fxLogger.UseLogLevel(slog.LevelDebug)

	return fxLogger
}
