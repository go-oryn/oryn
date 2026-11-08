package worker

import (
	"fmt"
	"time"

	"github.com/go-oryn/oryn/core/config"
)

// Defaults of the worker.* config keys.
const (
	DefaultReplicas       = 1
	DefaultMaxRestarts    = 5
	DefaultInitialBackoff = time.Second
	DefaultMaxBackoff     = 30 * time.Second
	DefaultResetAfter     = time.Minute
)

// DefaultRestartPolicy is the restart policy when nothing is configured.
var DefaultRestartPolicy = RestartPolicy{
	Enabled:        true,
	MaxRestarts:    DefaultMaxRestarts,
	InitialBackoff: DefaultInitialBackoff,
	MaxBackoff:     DefaultMaxBackoff,
	ResetAfter:     DefaultResetAfter,
}

// settings are the resolved settings of a worker.
type settings struct {
	enabled  bool
	replicas int
	restart  RestartPolicy
}

// resolveSettings resolves the settings of a worker, from the highest to the lowest precedence:
// the worker.workers.<name>.* keys, the options set in code, the worker.* keys, then the defaults.
func resolveSettings(cfg *config.Config, name string, options workerOptions) (settings, error) {
	prefix := "worker.workers." + name + "."

	restart := readRestart(cfg, "worker.restart.", DefaultRestartPolicy)
	if options.restart != nil {
		restart = *options.restart
	}

	replicas := DefaultReplicas
	if options.replicas != nil {
		replicas = *options.replicas
	}

	s := settings{
		enabled:  getBool(cfg, prefix+"enabled", true),
		replicas: getInt(cfg, prefix+"replicas", replicas),
		restart:  readRestart(cfg, prefix+"restart.", restart),
	}

	switch {
	case s.replicas < 1:
		return s, fmt.Errorf("invalid replicas %d for worker %q: expected at least 1", s.replicas, name)
	case s.restart.MaxRestarts < 0:
		return s, fmt.Errorf("invalid max_restarts %d for worker %q: expected 0 (unlimited) or more", s.restart.MaxRestarts, name)
	case s.restart.InitialBackoff < 0 || s.restart.MaxBackoff < 0 || s.restart.ResetAfter < 0:
		return s, fmt.Errorf("invalid restart durations for worker %q: expected positive durations", name)
	}

	if s.restart.MaxBackoff < s.restart.InitialBackoff {
		s.restart.MaxBackoff = s.restart.InitialBackoff
	}

	return s, nil
}

// readRestart overrides the fields of policy set under prefix.
func readRestart(cfg *config.Config, prefix string, policy RestartPolicy) RestartPolicy {
	return RestartPolicy{
		Enabled:        getBool(cfg, prefix+"enabled", policy.Enabled),
		MaxRestarts:    getInt(cfg, prefix+"max_restarts", policy.MaxRestarts),
		InitialBackoff: getDuration(cfg, prefix+"backoff.initial", policy.InitialBackoff),
		MaxBackoff:     getDuration(cfg, prefix+"backoff.max", policy.MaxBackoff),
		ResetAfter:     getDuration(cfg, prefix+"reset_after", policy.ResetAfter),
	}
}

// isSet reports whether key is set to a non-empty value: an explicit zero is a value.
func isSet(cfg *config.Config, key string) bool {
	return cfg.IsSet(key) && cfg.GetString(key) != ""
}

func getBool(cfg *config.Config, key string, defaultValue bool) bool {
	if isSet(cfg, key) {
		return cfg.GetBool(key)
	}

	return defaultValue
}

func getInt(cfg *config.Config, key string, defaultValue int) int {
	if isSet(cfg, key) {
		return cfg.GetInt(key)
	}

	return defaultValue
}

func getDuration(cfg *config.Config, key string, defaultValue time.Duration) time.Duration {
	if isSet(cfg, key) {
		return cfg.GetDuration(key)
	}

	return defaultValue
}
