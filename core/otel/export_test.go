package otel

import "github.com/go-oryn/oryn/core/config"

// Exports internals for the otel_test package.
var (
	ExporterKind = exporterKind
	NewSampler   = newSampler
)

// OTLPSettings returns the resolved OTLP protocol, endpoint URL and headers of a signal.
func OTLPSettings(cfg *config.Config, signal string) (string, string, map[string]string, error) {
	settings, err := newOTLPSettings(cfg, signal)

	return settings.protocol, settings.endpointURL, settings.headers, err
}
