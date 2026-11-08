---
title: APIs
description: The HTTP, gRPC and MCP APIs of the demo, three thin layers registered on the same tasks service, and how to call them.
---

Each API is a package of `internal/` with its own fx module: `internal/http` (package `httpapi`), `internal/grpc` (`grpcapi`) and `internal/mcp` (`mcpapi`), named so they do not collide with the `net/http`, gRPC and MCP SDK packages they import. It depends on the `*tasks.Service`, maps the requests to it, and maps the `tasks.ErrNotFound` and `tasks.ErrInvalidTitle` errors to its protocol:

| Error | HTTP | gRPC | MCP |
|---|---|---|---|
| `tasks.ErrInvalidTitle` | `400` | `INVALID_ARGUMENT` | tool error |
| `tasks.ErrNotFound` | `404` | `NOT_FOUND` | tool error |
| other | `500` | `INTERNAL` | tool error |

## HTTP

```go
// internal/http/module.go
var Module = fx.Module("httpapi",
	httpserver.AsMiddleware(TraceIDMiddleware),
	httpserver.AsHandlerGroup("/api",
		httpserver.WithMiddlewares(JSONMiddleware),
		httpserver.WithHandler("GET /tasks", NewListHandler),
		httpserver.WithHandler("POST /tasks", NewCreateHandler),
		httpserver.WithHandler("GET /tasks/{id}", NewGetHandler),
		httpserver.WithHandler("POST /tasks/{id}/complete", NewCompleteHandler),
		httpserver.WithHandler("DELETE /tasks/{id}", NewDeleteHandler),
	),
)

// internal/http/handlers.go
func NewCreateHandler(service *tasks.Service, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request createRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody(`expected a JSON body like {"title": "..."}`))
			return
		}

		task, err := service.Create(r.Context(), request.Title)
		...
	}
}
```

```shell
curl -X POST localhost:8080/api/tasks -d '{"title": "Try oryn"}'
curl localhost:8080/api/tasks
curl -X POST localhost:8080/api/tasks/<id>/complete
curl -X DELETE localhost:8080/api/tasks/<id>
```

The `TraceIDMiddleware` adds an `X-Trace-Id` header to every response: the ID of the trace of the request. See [HTTP server](../httpserver/overview.md).

## gRPC

The service is defined in `proto/tasks/v1/tasks.proto`. The generated code is committed; `go generate ./proto` regenerates it with [protoc](https://protobuf.dev/installation/) and the plugins declared as `tool` dependencies in `go.mod`.

```go
// internal/grpc/service.go
var Module = fx.Module("grpcapi",
	grpcserver.AsService(&tasksv1.TasksService_ServiceDesc, NewTasksService),
)

type TasksService struct {
	tasksv1.UnimplementedTasksServiceServer
	service *tasks.Service
}

func (s *TasksService) CreateTask(ctx context.Context, request *tasksv1.CreateTaskRequest) (*tasksv1.Task, error) {
	task, err := s.service.Create(ctx, request.GetTitle())
	if err != nil {
		return nil, grpcError(err)
	}

	return toProto(task), nil
}
```

Reflection is enabled in `configs/app.yaml` (`grpcserver.reflection.enabled`), so [grpcurl](https://github.com/fullstorydev/grpcurl) needs no proto file:

```shell
grpcurl -plaintext localhost:50051 list
grpcurl -plaintext -d '{"title": "Try oryn"}' localhost:50051 tasks.v1.TasksService/CreateTask
grpcurl -plaintext localhost:50051 tasks.v1.TasksService/ListTasks
grpcurl -plaintext localhost:50051 grpc.health.v1.Health/Check
```

See [gRPC server](../grpcserver/overview.md).

## MCP

The tools are typed: their input and output JSON schemas are inferred from the Go types.

```go
// internal/mcp/tools.go
var Module = fx.Module("mcpapi",
	mcpserver.AsCapability(NewTools),
)

type CreateInput struct {
	Title string `json:"title" jsonschema:"the title of the task"`
}

func (t *Tools) Register(server *mcp.Server) error {
	mcp.AddTool(server, &mcp.Tool{Name: "create_task", Description: "Create a task."},
		func(ctx context.Context, _ *mcp.CallToolRequest, input CreateInput) (*mcp.CallToolResult, tasks.Task, error) {
			task, err := t.service.Create(ctx, input.Title)
			return nil, task, err
		})
	// list_tasks, complete_task, delete_task...
	return nil
}
```

Connect an agent, then ask it to manage your tasks:

```shell
claude mcp add --transport http oryn-demo http://localhost:8082/mcp
```

Or explore the tools with the [MCP Inspector](https://github.com/modelcontextprotocol/inspector): `npx @modelcontextprotocol/inspector`, then connect to `http://localhost:8082/mcp` with the streamable HTTP transport. See [MCP server](../mcpserver/overview.md).
