package core

import "go.uber.org/fx"

// RunOnceErr exports runOnce for tests.
func (a App) RunOnceErr(options ...fx.Option) error {
	return a.runOnce(options...)
}
