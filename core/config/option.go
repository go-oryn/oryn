package config

import "io/fs"

// Options holds the parameters used by [New].
type Options struct {
	env         string
	envSet      bool
	envPrefix   string
	fileSystems []fs.FS
	values      []map[string]any
}

// Option configures [New].
type Option func(*Options)

// WithEnvironment sets the environment name. By default, it is read from the
// <PREFIX>_ENV environment variable (APP_ENV).
func WithEnvironment(env string) Option {
	return func(o *Options) {
		o.env = env
		o.envSet = true
	}
}

// WithEnvPrefix sets the prefix of the environment variables read by the
// configuration. Defaults to [DefaultEnvPrefix].
func WithEnvPrefix(prefix string) Option {
	return func(o *Options) {
		o.envPrefix = prefix
	}
}

// WithFS adds a file system to load config files from, typically an [embed.FS].
// YAML and JSON files at its root are loaded first, then the ones in the
// sub directory named after the environment. File systems are loaded in the
// order they are added.
func WithFS(fsys fs.FS) Option {
	return func(o *Options) {
		o.fileSystems = append(o.fileSystems, fsys)
	}
}

// WithValues sets values programmatically. They have the highest precedence,
// above environment variables. Keys can be nested maps or dotted keys
// (e.g. "http.server.address").
func WithValues(values map[string]any) Option {
	return func(o *Options) {
		o.values = append(o.values, values)
	}
}
