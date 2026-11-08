package grpcclienttest_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"google.golang.org/grpc"
	testpb "google.golang.org/grpc/interop/grpc_testing"

	"github.com/go-oryn/oryn/core"
	"github.com/go-oryn/oryn/core/coretest"
	"github.com/go-oryn/oryn/grpcclient"
	"github.com/go-oryn/oryn/grpcclient/grpcclienttest"
	"github.com/go-oryn/oryn/grpcserver"
)

type testService struct {
	testpb.UnimplementedTestServiceServer
}

func (testService) UnaryCall(context.Context, *testpb.SimpleRequest) (*testpb.SimpleResponse, error) {
	return &testpb.SimpleResponse{Payload: &testpb.Payload{Body: []byte("test_response")}}, nil
}

func call(t *testing.T, client testpb.TestServiceClient) string {
	t.Helper()

	response, err := client.UnaryCall(t.Context(), &testpb.SimpleRequest{})
	require.NoError(t, err)

	return string(response.GetPayload().GetBody())
}

func TestServe(t *testing.T) {
	t.Parallel()

	server := grpc.NewServer()
	testpb.RegisterTestServiceServer(server, testService{})

	var client testpb.TestServiceClient

	coretest.Start(t,
		core.New(grpcclient.Module, grpcclient.AsClient("test", testpb.NewTestServiceClient)),
		grpcclienttest.Serve("test", server),
		fx.Populate(&client),
	)

	assert.Equal(t, "test_response", call(t, client))
}

func TestServeApp(t *testing.T) {
	t.Parallel()

	var client testpb.TestServiceClient

	coretest.Start(t,
		core.New(
			grpcserver.Module,
			grpcserver.AsService(&testpb.TestService_ServiceDesc, testService{}),
			grpcclient.Module,
			grpcclient.AsClient("self", testpb.NewTestServiceClient),
		),
		grpcclienttest.ServeApp("self"),
		fx.Populate(&client),
	)

	assert.Equal(t, "test_response", call(t, client))
}
