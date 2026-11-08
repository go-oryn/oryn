package grpcservertest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/go-oryn/oryn/grpcserver/grpcservertest"
)

func TestDial(t *testing.T) {
	t.Parallel()

	server := grpc.NewServer()
	healthpb.RegisterHealthServer(server, health.NewServer())

	conn := grpcservertest.Dial(t, server, grpc.WithUserAgent("test_agent"))

	resp, err := healthpb.NewHealthClient(conn).Check(t.Context(), &healthpb.HealthCheckRequest{})
	require.NoError(t, err)
	assert.Equal(t, healthpb.HealthCheckResponse_SERVING, resp.GetStatus())
}
