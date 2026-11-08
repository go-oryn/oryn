package otel

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/go-oryn/oryn/core/config"
)

// Exporter kinds, set per signal with OTEL_<SIGNAL>_EXPORTER or otel.<signal>.exporter.
const (
	ExporterOTLP    = "otlp"
	ExporterConsole = "console"
	ExporterMemory  = "memory"
	ExporterNone    = "none"
)

// OTLP protocols, set with OTEL_EXPORTER_OTLP_PROTOCOL or otel.exporter.otlp.protocol.
const (
	ProtocolGRPC         = "grpc"
	ProtocolHTTPProtobuf = "http/protobuf"
)

// Signals.
const (
	signalTraces  = "traces"
	signalMetrics = "metrics"
	signalLogs    = "logs"
)

// exporterKind returns the exporter kind for the signal.
// The test environment always exports in memory. Otherwise, from highest to lowest precedence:
// OTEL_SDK_DISABLED or otel.disabled (none), OTEL_<SIGNAL>_EXPORTER, otel.<signal>.exporter, then otlp.
func exporterKind(cfg *config.Config, signal string) (string, error) {
	if cfg.IsTestEnv() {
		return ExporterMemory, nil
	}

	if sdkDisabled(cfg) {
		return ExporterNone, nil
	}

	kind := os.Getenv("OTEL_" + strings.ToUpper(signal) + "_EXPORTER")
	if kind == "" {
		kind = cfg.GetStringOrDefault("otel."+signal+".exporter", ExporterOTLP)
	}

	kind = strings.ToLower(strings.TrimSpace(kind))

	switch kind {
	case ExporterOTLP, ExporterConsole, ExporterMemory, ExporterNone:
		return kind, nil
	default:
		return "", fmt.Errorf("unsupported %s exporter %q, expected one of otlp, console, memory or none", signal, kind)
	}
}

// sdkDisabled reports whether the SDK is disabled with OTEL_SDK_DISABLED, or otel.disabled when the variable is not set.
func sdkDisabled(cfg *config.Config) bool {
	if value, ok := os.LookupEnv("OTEL_SDK_DISABLED"); ok && value != "" {
		return strings.EqualFold(value, "true")
	}

	return cfg.GetBool("otel.disabled")
}

// otlpSettings holds the OTLP exporter settings coming from the config.
// Empty values mean "not configured": the exporter then uses its environment variables or defaults.
type otlpSettings struct {
	protocol    string
	endpointURL string
	headers     map[string]string
}

// newOTLPSettings resolves the OTLP settings of a signal. A setting is only
// taken from the config when neither its generic nor its signal specific
// environment variable is set.
func newOTLPSettings(cfg *config.Config, signal string) (otlpSettings, error) {
	upper := strings.ToUpper(signal)

	settings := otlpSettings{
		protocol: firstNonEmpty(
			os.Getenv("OTEL_EXPORTER_OTLP_"+upper+"_PROTOCOL"),
			os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL"),
			cfg.GetString("otel.exporter.otlp.protocol"),
			ProtocolHTTPProtobuf,
		),
	}

	if settings.protocol != ProtocolGRPC && settings.protocol != ProtocolHTTPProtobuf {
		return settings, fmt.Errorf("unsupported OTLP protocol %q, expected grpc or http/protobuf", settings.protocol)
	}

	if !envSet("OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_"+upper+"_ENDPOINT") {
		if endpoint := cfg.GetString("otel.exporter.otlp.endpoint"); endpoint != "" {
			endpointURL, err := signalURL(endpoint, signal, settings.protocol)
			if err != nil {
				return settings, err
			}

			settings.endpointURL = endpointURL
		}
	}

	if !envSet("OTEL_EXPORTER_OTLP_HEADERS", "OTEL_EXPORTER_OTLP_"+upper+"_HEADERS") {
		settings.headers = cfg.GetStringMapString("otel.exporter.otlp.headers")
	}

	return settings, nil
}

// signalURL turns a base endpoint (e.g. http://collector:4318) into the URL of the
// signal. Like OTEL_EXPORTER_OTLP_ENDPOINT, HTTP gets /v1/<signal> appended.
func signalURL(endpoint string, signal string, protocol string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid otel.exporter.otlp.endpoint %q: expected a URL like http://collector:4318", endpoint)
	}

	if protocol == ProtocolHTTPProtobuf {
		u.Path = strings.TrimSuffix(u.Path, "/") + "/v1/" + signal
	}

	return u.String(), nil
}

// envSet reports whether at least one of the environment variables is set to a non-empty value.
func envSet(names ...string) bool {
	for _, name := range names {
		if os.Getenv(name) != "" {
			return true
		}
	}

	return false
}

// firstNonEmpty returns the first non-empty value, trimmed and lower cased, or the last one.
func firstNonEmpty(values ...string) string {
	var value string

	for _, value = range values {
		if value = strings.ToLower(strings.TrimSpace(value)); value != "" {
			break
		}
	}

	return value
}
