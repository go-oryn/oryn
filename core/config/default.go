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
	if c.IsSet(key) && c.GetString(key) != "" {
		return c.GetBool(key)
	}

	return defaultValue
}

// GetIntOrDefault returns the int value of key, or defaultValue if the key is not set or zero.
func (c *Config) GetIntOrDefault(key string, defaultValue int) int {
	if value := c.GetInt(key); c.IsSet(key) && value != 0 {
		return value
	}

	return defaultValue
}

// GetFloat64OrDefault returns the float64 value of key, or defaultValue if the key is not set or zero.
func (c *Config) GetFloat64OrDefault(key string, defaultValue float64) float64 {
	if value := c.GetFloat64(key); c.IsSet(key) && value != 0 {
		return value
	}

	return defaultValue
}

// GetDurationOrDefault returns the duration value of key, or defaultValue if the key is not set or zero.
func (c *Config) GetDurationOrDefault(key string, defaultValue time.Duration) time.Duration {
	if value := c.GetDuration(key); c.IsSet(key) && value != 0 {
		return value
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
