package config

import "time"

// GetStringOrDefault returns the string value of key, or defaultValue if the key is not set or empty.
func (c *Config) GetStringOrDefault(key string, defaultValue string) string {
	if value := c.GetString(key); c.IsSet(key) && value != "" {
		return value
	}

	return defaultValue
}

// GetBoolOrDefault returns the bool value of key, or defaultValue if the key is not set.
func (c *Config) GetBoolOrDefault(key string, defaultValue bool) bool {
	if c.isSet(key) {
		return c.GetBool(key)
	}

	return defaultValue
}

// GetIntOrDefault returns the int value of key, or defaultValue if the key is not set or empty.
// An explicit zero is returned as is: 0 can mean "none" or "unlimited".
func (c *Config) GetIntOrDefault(key string, defaultValue int) int {
	if c.isSet(key) {
		return c.GetInt(key)
	}

	return defaultValue
}

// GetFloat64OrDefault returns the float64 value of key, or defaultValue if the key is not set or empty.
// An explicit zero is returned as is: 0 can mean "none" or "unlimited".
func (c *Config) GetFloat64OrDefault(key string, defaultValue float64) float64 {
	if c.isSet(key) {
		return c.GetFloat64(key)
	}

	return defaultValue
}

// GetDurationOrDefault returns the duration value of key, or defaultValue if the key is not set or empty.
// An explicit zero is returned as is: 0 can mean "none" or "unlimited".
func (c *Config) GetDurationOrDefault(key string, defaultValue time.Duration) time.Duration {
	if c.isSet(key) {
		return c.GetDuration(key)
	}

	return defaultValue
}

// GetStringSliceOrDefault returns the string slice value of key, or defaultValue if the key is not set or empty.
func (c *Config) GetStringSliceOrDefault(key string, defaultValue []string) []string {
	if value := c.GetStringSlice(key); c.IsSet(key) && len(value) > 0 {
		return value
	}

	return defaultValue
}

// GetStringMapStringOrDefault returns the string map value of key, or defaultValue if the key is not set or empty.
func (c *Config) GetStringMapStringOrDefault(key string, defaultValue map[string]string) map[string]string {
	if value := c.GetStringMapString(key); c.IsSet(key) && len(value) > 0 {
		return value
	}

	return defaultValue
}

// isSet reports whether key is set to a non-empty value. An empty value, like an
// environment variable placeholder expanding to nothing, is not set.
func (c *Config) isSet(key string) bool {
	return c.IsSet(key) && c.GetString(key) != ""
}
