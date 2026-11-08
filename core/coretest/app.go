// Package coretest runs oryn applications in tests.
//
// Test applications use the "test" environment: all telemetry is exported in
// memory, so it can be asserted with the oteltest packages, and logs are not
// printed on the console.
//
//	func TestGreet(t *testing.T) {
//		var spans *tracetest.InMemoryExporter
//
//		coretest.Start(t, app, fx.Populate(&spans))
//
//		// exercise the application...
//
//		spantest.HasSpan(t, spans, spantest.WithName("greet"))
//	}
package coretest

import (
	"testing"

	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/core/config"
)

// testOptions is registered at package initialization, before any test code
// runs, so the config options of the tests always win over it.
var testOptions = config.AsOptions(
	config.WithEnvironment(config.EnvTest),
	config.WithValues(map[string]any{
		"log.console.enabled": false,
		"log.set_default":     false,
	}),
)

// TestOptions are the options added to every test application: the test
// environment, no console logs, and no global slog logger. Config options of
// the tests override them.
func TestOptions() fx.Option {
	return testOptions
}

// New builds the test application, using the test context as base context.
// The options are added after the app ones, so they can replace (fx.Replace)
// or decorate (fx.Decorate) dependencies.
func New(tb testing.TB, app core.App, options ...fx.Option) *fxtest.App {
	tb.Helper()

	return fxtest.New(tb, app.WithContext(tb.Context()).Options(append([]fx.Option{TestOptions()}, options...)...)...)
}

// Start builds and starts the test application, and stops it when the test ends.
func Start(tb testing.TB, app core.App, options ...fx.Option) *fxtest.App {
	tb.Helper()

	testApp := New(tb, app, options...)
	testApp.RequireStart()
	tb.Cleanup(testApp.RequireStop)

	return testApp
}
