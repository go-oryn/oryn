// Package config loads the application configuration.
//
// Configuration is read, from lowest to highest precedence, from:
//
//  1. built-in defaults (app.name, app.version),
//  2. YAML or JSON files at the root of the provided file systems,
//  3. the same files in the sub directory named after the environment (e.g. "prod/"),
//  4. environment variables, with the configured prefix (APP_ by default):
//     the key "http.server.address" is read from APP_HTTP_SERVER_ADDRESS,
//  5. values set programmatically with [WithValues].
//
// Config files can reference environment variables with ${VAR} or ${VAR:default}.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/viper"
)

const (
	// DefaultEnvPrefix is the default prefix of the environment variables read by the configuration.
	DefaultEnvPrefix = "APP"
	// DefaultAppName is the application name used when app.name is not configured.
	DefaultAppName = "app"
	// DefaultAppVersion is the application version used when app.version is not configured.
	DefaultAppVersion = "0.0.0"
	// EnvTest is the name of the test environment.
	EnvTest = "test"
)

// placeholderPattern matches ${VAR} and ${VAR:default} placeholders in config files.
var placeholderPattern = regexp.MustCompile(`\$\{([^:}]+)(?::([^}]*))?\}`)

// Config is the application configuration. It embeds a [viper.Viper], so all
// viper accessors (GetString, UnmarshalKey...) are available.
type Config struct {
	*viper.Viper
	env string
}

// New creates a [Config] from the given options.
func New(opts ...Option) (*Config, error) {
	options := Options{
		envPrefix: DefaultEnvPrefix,
	}

	for _, opt := range opts {
		opt(&options)
	}

	env := options.env
	if !options.envSet {
		env = os.Getenv(options.envPrefix + "_ENV")
	}

	env = strings.ToLower(strings.TrimSpace(env))

	v := viper.New()
	v.SetDefault("app.name", DefaultAppName)
	v.SetDefault("app.version", DefaultAppVersion)
	v.SetEnvPrefix(options.envPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()

	for _, fsys := range options.fileSystems {
		if err := mergeDir(v, fsys, "."); err != nil {
			return nil, err
		}

		if env != "" {
			if err := mergeDir(v, fsys, env); err != nil {
				return nil, err
			}
		}
	}

	for _, values := range options.values {
		for key, value := range flatten("", values) {
			v.Set(key, value)
		}
	}

	return &Config{
		Viper: v,
		env:   env,
	}, nil
}

// Env returns the environment name, in lower case (e.g. "dev", "prod", "test").
func (c *Config) Env() string {
	return c.env
}

// IsTestEnv reports whether the configuration is loaded for the test environment.
func (c *Config) IsTestEnv() bool {
	return c.env == EnvTest
}

// AppName returns the application name (app.name).
func (c *Config) AppName() string {
	return c.GetStringOrDefault("app.name", DefaultAppName)
}

// AppVersion returns the application version (app.version).
func (c *Config) AppVersion() string {
	return c.GetStringOrDefault("app.version", DefaultAppVersion)
}

// mergeDir merges every YAML and JSON file of dir into v, in lexical order.
// A missing dir is not an error.
func mergeDir(v *viper.Viper, fsys fs.FS, dir string) error {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}

		return fmt.Errorf("cannot read config directory %q: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		format := formatOf(entry.Name())
		if format == "" {
			continue
		}

		if err := mergeFile(v, fsys, path.Join(dir, entry.Name()), format); err != nil {
			return err
		}
	}

	return nil
}

// mergeFile resolves the placeholders of a single file and merges it into v.
func mergeFile(v *viper.Viper, fsys fs.FS, file string, format string) error {
	data, err := fs.ReadFile(fsys, file)
	if err != nil {
		return fmt.Errorf("cannot read config file %q: %w", file, err)
	}

	v.SetConfigType(format)

	if err := v.MergeConfig(strings.NewReader(resolvePlaceholders(string(data)))); err != nil {
		return fmt.Errorf("cannot parse config file %q: %w", file, err)
	}

	return nil
}

// resolvePlaceholders replaces ${VAR} and ${VAR:default} with the value of the environment variable VAR,
// or with default (or an empty string) when VAR is not set or empty.
func resolvePlaceholders(data string) string {
	return placeholderPattern.ReplaceAllStringFunc(data, func(match string) string {
		groups := placeholderPattern.FindStringSubmatch(match)

		if value := os.Getenv(groups[1]); value != "" {
			return value
		}

		return groups[2]
	})
}

// formatOf returns the viper config type of a file name, or an empty string if not supported.
func formatOf(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".yaml", ".yml":
		return "yaml"
	case ".json":
		return "json"
	default:
		return ""
	}
}

// flatten turns nested maps into dotted keys, so {"a": {"b": 1}} becomes {"a.b": 1}.
// Setting leaves one by one keeps the sibling keys coming from files and environment.
// A map having dotted keys (e.g. {"service.name": "x"}) is kept as a single value,
// as its keys cannot be expressed as a path.
func flatten(prefix string, values map[string]any) map[string]any {
	flat := make(map[string]any, len(values))

	for _, key := range slices.Sorted(maps.Keys(values)) {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		if nested, ok := values[key].(map[string]any); ok && len(nested) > 0 && !hasDottedKey(nested) {
			maps.Copy(flat, flatten(fullKey, nested))

			continue
		}

		flat[fullKey] = values[key]
	}

	return flat
}

func hasDottedKey(values map[string]any) bool {
	for key := range values {
		if strings.Contains(key, ".") {
			return true
		}
	}

	return false
}
