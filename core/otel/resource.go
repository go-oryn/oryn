package otel

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.uber.org/fx"

	"github.com/go-oryn/oryn/core/config"
)

// NewResourceParams are the dependencies of [NewResource].
type NewResourceParams struct {
	fx.In

	Config  *config.Config
	Options []resource.Option `group:"oryn-otel-resource-options"`
}

// NewResource returns the resource describing the application. From lowest to
// highest precedence, attributes come from:
//
//  1. the SDK and the Go runtime,
//  2. the config: app.name, app.version, the environment, and otel.resource.attributes,
//  3. the options registered with [AsResourceOptions],
//  4. the OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES environment variables.
func NewResource(params NewResourceParams) (*resource.Resource, error) {
	cfg := params.Config

	attrs := []attribute.KeyValue{
		semconv.ServiceName(cfg.AppName()),
		semconv.ServiceVersion(cfg.AppVersion()),
	}

	if env := cfg.Env(); env != "" {
		attrs = append(attrs, semconv.DeploymentEnvironmentNameKey.String(env))
	}

	for key, value := range cfg.GetStringMapString("otel.resource.attributes") {
		attrs = append(attrs, attribute.String(key, value))
	}

	options := make([]resource.Option, 0, len(params.Options)+6)
	options = append(options,
		resource.WithTelemetrySDK(),
		resource.WithProcessRuntimeName(),
		resource.WithProcessRuntimeVersion(),
		resource.WithAttributes(attrs...),
	)
	options = append(options, params.Options...)
	options = append(options, resource.WithFromEnv())

	res, err := resource.New(context.Background(), options...)
	if err != nil && !errors.Is(err, resource.ErrPartialResource) {
		return nil, err
	}

	return res, nil
}
