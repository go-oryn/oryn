package grpcserver

import (
	"context"

	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"github.com/go-oryn/oryn/core/health"
)

// HealthServer implements the [gRPC health checking protocol] with the application health [*health.Checker].
//
// The service names are the kinds of check: "liveness", "readiness" and "startup". The empty
// service name, the overall health of the server, is the readiness check. Watch is not implemented.
//
// [gRPC health checking protocol]: https://github.com/grpc/grpc/blob/master/doc/health-checking.md
type HealthServer struct {
	healthpb.UnimplementedHealthServer

	checker *health.Checker
}

// NewHealthServer returns a [HealthServer] running the checks of checker.
func NewHealthServer(checker *health.Checker) *HealthServer {
	return &HealthServer{checker: checker}
}

// healthServices maps the service names to the kinds of check.
var healthServices = map[string]health.Kind{
	"":                       health.Readiness,
	string(health.Liveness):  health.Liveness,
	string(health.Readiness): health.Readiness,
	string(health.Startup):   health.Startup,
}

// Check runs the check of the requested service, or answers NotFound for an unknown service.
func (s *HealthServer) Check(ctx context.Context, req *healthpb.HealthCheckRequest) (*healthpb.HealthCheckResponse, error) {
	kind, ok := healthServices[req.GetService()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "unknown health service %q", req.GetService())
	}

	return s.check(ctx, kind), nil
}

// List runs the checks of all the services.
func (s *HealthServer) List(ctx context.Context, _ *healthpb.HealthListRequest) (*healthpb.HealthListResponse, error) {
	statuses := make(map[string]*healthpb.HealthCheckResponse, len(healthServices))
	for name, kind := range healthServices {
		statuses[name] = s.check(ctx, kind)
	}

	return &healthpb.HealthListResponse{Statuses: statuses}, nil
}

func (s *HealthServer) check(ctx context.Context, kind health.Kind) *healthpb.HealthCheckResponse {
	if s.checker.Check(ctx, kind).Healthy() {
		return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_SERVING}
	}

	return &healthpb.HealthCheckResponse{Status: healthpb.HealthCheckResponse_NOT_SERVING}
}
