package mcpclient

import (
	"net/url"

	"go.opentelemetry.io/otel/attribute"
)

// ServerAttributes exports serverAttributes for tests.
func ServerAttributes(endpoint string) []attribute.KeyValue {
	u, _ := url.Parse(endpoint)

	return serverAttributes(u)
}

// SDKLogger exports sdkLogger for tests.
var SDKLogger = sdkLogger
