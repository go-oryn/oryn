package otel_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/go-oryn/oryn/core/config"
	"github.com/go-oryn/oryn/core/otel"
)

func newTestConfig(t *testing.T, env string, values map[string]any) *config.Config {
	t.Helper()

	cfg, err := config.New(config.WithEnvironment(env), config.WithValues(values))
	require.NoError(t, err)

	return cfg
}

func TestExporterKind(t *testing.T) {
	t.Run("defaults to otlp", func(t *testing.T) {
		kind, err := otel.ExporterKind(newTestConfig(t, "prod", nil), "traces")
		require.NoError(t, err)
		assert.Equal(t, otel.ExporterOTLP, kind)
	})

	t.Run("from config", func(t *testing.T) {
		kind, err := otel.ExporterKind(newTestConfig(t, "dev", map[string]any{"otel.traces.exporter": "Console"}), "traces")
		require.NoError(t, err)
		assert.Equal(t, otel.ExporterConsole, kind)
	})

	t.Run("environment variable wins over config", func(t *testing.T) {
		t.Setenv("OTEL_METRICS_EXPORTER", "none")

		kind, err := otel.ExporterKind(newTestConfig(t, "dev", map[string]any{"otel.metrics.exporter": "console"}), "metrics")
		require.NoError(t, err)
		assert.Equal(t, otel.ExporterNone, kind)
	})

	t.Run("disabled sdk", func(t *testing.T) {
		kind, err := otel.ExporterKind(newTestConfig(t, "dev", map[string]any{"otel.disabled": true}), "logs")
		require.NoError(t, err)
		assert.Equal(t, otel.ExporterNone, kind)

		t.Setenv("OTEL_SDK_DISABLED", "false")

		kind, err = otel.ExporterKind(newTestConfig(t, "dev", map[string]any{"otel.disabled": true}), "logs")
		require.NoError(t, err)
		assert.Equal(t, otel.ExporterOTLP, kind)
	})

	t.Run("test environment always exports in memory", func(t *testing.T) {
		t.Setenv("OTEL_TRACES_EXPORTER", "otlp")

		kind, err := otel.ExporterKind(newTestConfig(t, config.EnvTest, nil), "traces")
		require.NoError(t, err)
		assert.Equal(t, otel.ExporterMemory, kind)
	})

	t.Run("unsupported", func(t *testing.T) {
		_, err := otel.ExporterKind(newTestConfig(t, "dev", map[string]any{"otel.logs.exporter": "zipkin"}), "logs")
		require.ErrorContains(t, err, `unsupported logs exporter "zipkin"`)
	})
}

func TestOTLPSettings(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		protocol, endpoint, headers, err := otel.OTLPSettings(newTestConfig(t, "dev", nil), "traces")
		require.NoError(t, err)

		assert.Equal(t, otel.ProtocolHTTPProtobuf, protocol)
		assert.Empty(t, endpoint)
		assert.Empty(t, headers)
	})

	t.Run("http from config", func(t *testing.T) {
		cfg := newTestConfig(t, "dev", map[string]any{
			"otel.exporter.otlp.endpoint": "https://collector:4318/base/",
			"otel.exporter.otlp.headers":  map[string]any{"authorization": "Bearer token"},
		})

		protocol, endpoint, headers, err := otel.OTLPSettings(cfg, "metrics")
		require.NoError(t, err)

		assert.Equal(t, otel.ProtocolHTTPProtobuf, protocol)
		assert.Equal(t, "https://collector:4318/base/v1/metrics", endpoint)
		assert.Equal(t, map[string]string{"authorization": "Bearer token"}, headers)
	})

	t.Run("grpc from config", func(t *testing.T) {
		cfg := newTestConfig(t, "dev", map[string]any{
			"otel.exporter.otlp.protocol": "grpc",
			"otel.exporter.otlp.endpoint": "http://collector:4317",
		})

		protocol, endpoint, _, err := otel.OTLPSettings(cfg, "logs")
		require.NoError(t, err)

		assert.Equal(t, otel.ProtocolGRPC, protocol)
		assert.Equal(t, "http://collector:4317", endpoint)
	})

	t.Run("environment variables win over config", func(t *testing.T) {
		t.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "grpc")
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://from-env:4317")
		t.Setenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "key=value")

		cfg := newTestConfig(t, "dev", map[string]any{
			"otel.exporter.otlp.protocol": "http/protobuf",
			"otel.exporter.otlp.endpoint": "http://from-config:4318",
			"otel.exporter.otlp.headers":  map[string]any{"key": "config"},
		})

		protocol, endpoint, headers, err := otel.OTLPSettings(cfg, "traces")
		require.NoError(t, err)

		assert.Equal(t, otel.ProtocolGRPC, protocol)
		assert.Empty(t, endpoint, "the exporter reads the endpoint from the environment")
		assert.Empty(t, headers, "the exporter reads the headers from the environment")
	})

	t.Run("invalid protocol", func(t *testing.T) {
		_, _, _, err := otel.OTLPSettings(newTestConfig(t, "dev", map[string]any{"otel.exporter.otlp.protocol": "http/json"}), "traces")
		require.ErrorContains(t, err, `unsupported OTLP protocol "http/json"`)
	})

	t.Run("invalid endpoint", func(t *testing.T) {
		_, _, _, err := otel.OTLPSettings(newTestConfig(t, "dev", map[string]any{"otel.exporter.otlp.endpoint": "collector:4318"}), "traces")
		require.ErrorContains(t, err, "invalid otel.exporter.otlp.endpoint")
	})
}

func TestNewSampler(t *testing.T) {
	t.Run("not configured", func(t *testing.T) {
		sampler, err := otel.NewSampler(newTestConfig(t, "dev", nil))
		require.NoError(t, err)
		assert.Nil(t, sampler)
	})

	t.Run("from config", func(t *testing.T) {
		for name, description := range map[string]string{
			"always_on":                "AlwaysOnSampler",
			"always_off":               "AlwaysOffSampler",
			"traceidratio":             "TraceIDRatioBased{0.25}",
			"parentbased_always_on":    "ParentBased{root:AlwaysOnSampler",
			"parentbased_always_off":   "ParentBased{root:AlwaysOffSampler",
			"parentbased_traceidratio": "ParentBased{root:TraceIDRatioBased{0.25}",
		} {
			cfg := newTestConfig(t, "dev", map[string]any{"otel.traces.sampler": name, "otel.traces.sampler_arg": 0.25})

			sampler, err := otel.NewSampler(cfg)
			require.NoError(t, err)
			assert.Contains(t, sampler.Description(), description, name)
		}
	})

	t.Run("environment variable wins over config", func(t *testing.T) {
		t.Setenv("OTEL_TRACES_SAMPLER", "always_off")

		sampler, err := otel.NewSampler(newTestConfig(t, "dev", map[string]any{"otel.traces.sampler": "always_on"}))
		require.NoError(t, err)
		assert.Nil(t, sampler, "the SDK reads the sampler from the environment")
	})

	t.Run("unsupported", func(t *testing.T) {
		_, err := otel.NewSampler(newTestConfig(t, "dev", map[string]any{"otel.traces.sampler": "jaeger_remote"}))
		require.ErrorContains(t, err, `unsupported otel.traces.sampler "jaeger_remote"`)
	})
}
