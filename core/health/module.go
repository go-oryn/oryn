package health

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"

	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core/config"
)

// ModuleName is the name of the health fx module.
const ModuleName = "health"

// Module provides the [*Checker], fed with the probes registered with [AsProbe].
var Module = fx.Module(
	ModuleName,
	fx.Provide(NewModuleChecker),
)

// NewModuleCheckerParams are the dependencies of [NewModuleChecker].
type NewModuleCheckerParams struct {
	fx.In

	Lifecycle       fx.Lifecycle
	Config          *config.Config
	Logger          *slog.Logger
	LivenessProbes  []Probe `group:"oryn-health-liveness"`
	ReadinessProbes []Probe `group:"oryn-health-readiness"`
	StartupProbes   []Probe `group:"oryn-health-startup"`
}

// NewModuleChecker returns the [*Checker] of the application. Its readiness
// checks fail as soon as the application starts shutting down.
func NewModuleChecker(params NewModuleCheckerParams) *Checker {
	checker := NewChecker(
		params.Logger,
		params.Config.GetDuration("health.timeout"),
		map[Kind][]Probe{
			Liveness:  params.LivenessProbes,
			Readiness: params.ReadinessProbes,
			Startup:   params.StartupProbes,
		},
	)

	params.Lifecycle.Append(fx.StopHook(checker.SetShuttingDown))

	return checker
}

// probeSequence makes the internal name of each registered probe unique.
var probeSequence atomic.Uint64

// AsProbe registers the [Probe] built by constructor for the given kinds of check,
// or for all of them if no kind is given. The probe is built once, even if used by several kinds.
func AsProbe(constructor any, kinds ...Kind) fx.Option {
	if len(kinds) == 0 {
		kinds = Kinds
	}

	name := fmt.Sprintf(`name:"oryn-health-probe-%d"`, probeSequence.Add(1))

	options := make([]fx.Option, 0, 1+len(kinds))
	options = append(options, fx.Provide(fx.Annotate(constructor, fx.As(new(Probe)), fx.ResultTags(name))))

	for _, kind := range kinds {
		options = append(options, fx.Provide(fx.Annotate(
			func(probe Probe) Probe { return probe },
			fx.ParamTags(name),
			fx.ResultTags(fmt.Sprintf(`group:"oryn-health-%s"`, kind)),
		)))
	}

	return fx.Options(options...)
}

// Serve starts the health HTTP server with the application, on health.server.address (default :8081):
//
//	GET /livez     liveness check
//	GET /readyz    readiness check
//	GET /startupz  startup check
//
// Each endpoint answers 200 when healthy and 503 otherwise. Add ?verbose to get the probe results as JSON.
func Serve() fx.Option {
	return fx.Invoke(func(lc fx.Lifecycle, cfg *config.Config, logger *slog.Logger, checker *Checker) {
		server := &http.Server{
			Addr:              cfg.GetStringOrDefault("health.server.address", ":8081"),
			Handler:           NewHandler(checker),
			ReadHeaderTimeout: DefaultTimeout,
		}

		lc.Append(fx.Hook{
			OnStart: func(ctx context.Context) error {
				listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", server.Addr)
				if err != nil {
					return fmt.Errorf("cannot start health server on %s: %w", server.Addr, err)
				}

				go func() {
					if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
						logger.Error("health server stopped", "error", err)
					}
				}()

				logger.DebugContext(ctx, "health server started", "address", listener.Addr().String())

				return nil
			},
			OnStop: server.Shutdown,
		})
	})
}
