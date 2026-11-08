package grpcserver_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"github.com/go-oryn/oryn/core/health"
)

// failingProbe always fails.
type failingProbe struct{}

func (failingProbe) Name() string { return "test_probe" }

func (failingProbe) Check(context.Context) error { return errors.New("test_error") }

func TestHealthServer(t *testing.T) {
	t.Parallel()

	t.Run("check", func(t *testing.T) {
		t.Parallel()

		conn, _ := startServer(t, health.AsProbe(func() health.Probe { return failingProbe{} }, health.Readiness))
		client := healthpb.NewHealthClient(conn)

		for service, expected := range map[string]healthpb.HealthCheckResponse_ServingStatus{
			"":          healthpb.HealthCheckResponse_NOT_SERVING,
			"readiness": healthpb.HealthCheckResponse_NOT_SERVING,
			"liveness":  healthpb.HealthCheckResponse_SERVING,
			"startup":   healthpb.HealthCheckResponse_SERVING,
		} {
			resp, err := client.Check(t.Context(), &healthpb.HealthCheckRequest{Service: service})
			require.NoError(t, err, service)
			assert.Equal(t, expected, resp.GetStatus(), service)
		}

		_, err := client.Check(t.Context(), &healthpb.HealthCheckRequest{Service: "unknown"})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("list", func(t *testing.T) {
		t.Parallel()

		conn, _ := startServer(t, health.AsProbe(func() health.Probe { return failingProbe{} }, health.Liveness))

		resp, err := healthpb.NewHealthClient(conn).List(t.Context(), &healthpb.HealthListRequest{})
		require.NoError(t, err)

		statuses := make(map[string]healthpb.HealthCheckResponse_ServingStatus)
		for name, s := range resp.GetStatuses() {
			statuses[name] = s.GetStatus()
		}

		assert.Equal(t, map[string]healthpb.HealthCheckResponse_ServingStatus{
			"":          healthpb.HealthCheckResponse_SERVING,
			"liveness":  healthpb.HealthCheckResponse_NOT_SERVING,
			"readiness": healthpb.HealthCheckResponse_SERVING,
			"startup":   healthpb.HealthCheckResponse_SERVING,
		}, statuses)
	})

	t.Run("watch not implemented", func(t *testing.T) {
		t.Parallel()

		conn, _ := startServer(t)

		stream, err := healthpb.NewHealthClient(conn).Watch(t.Context(), &healthpb.HealthCheckRequest{})
		require.NoError(t, err)

		_, err = stream.Recv()
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})

	t.Run("disabled", func(t *testing.T) {
		t.Parallel()

		conn, _ := startServer(t, withValues(map[string]any{"grpcserver.health.enabled": false}))

		_, err := healthpb.NewHealthClient(conn).Check(t.Context(), &healthpb.HealthCheckRequest{})
		assert.Equal(t, codes.Unimplemented, status.Code(err))
	})
}
