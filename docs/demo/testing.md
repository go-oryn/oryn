---
title: Testing
description: How the demo tests the tasks service and its HTTP, gRPC and MCP APIs in memory, asserting responses and telemetry.
---

The demo tests the whole application in memory: a SQLite database per test, API clients connected without network, and the telemetry exported in memory.

```shell
go test ./...
```

## The demotest helper

`demotest.Start` starts the application with [coretest](../core/testing.md), and connects a client to each API:

```go
// demotest/demotest.go
func Start(t *testing.T, options ...fx.Option) *App {
	app := &App{t: t}

	var (
		grpcServer *grpc.Server
		mcpServer  *mcp.Server
	)

	coretest.Start(t, demo.App,
		append(options,
			config.AsOptions(config.WithValues(map[string]any{
				"sql.driver": "sqlite",
				"sql.dsn":    filepath.Join(t.TempDir(), "test.db"),
			})),
			sql.Migrate(),
			fx.Populate(&app.HTTPServer, &grpcServer, &mcpServer, &app.Spans, &app.Logs, &app.Metrics),
		)...,
	)

	app.GRPC = tasksv1.NewTasksServiceClient(grpcservertest.Dial(t, grpcServer)) // in memory, with bufconn
	app.MCP = mcpservertest.Connect(t, mcpServer)                                // in-memory MCP transport

	return app
}
```

## The service

```go
func TestService(t *testing.T) {
	t.Parallel()

	var service *tasks.Service

	app := demotest.Start(t, fx.Populate(&service))

	task, err := service.Create(t.Context(), "test_title")
	require.NoError(t, err)

	create := spantest.Find(app.Spans, spantest.WithName("tasks.create"), spantest.WithAttributeValue(tasks.TaskIDKey.String(task.ID)))
	require.Len(t, create, 1)
	spantest.HasSpan(t, app.Spans, spantest.WithName("INSERT"), spantest.WithParent(create[0].SpanContext.SpanID()))
	logtest.HasRecord(t, app.Logs, logtest.WithBody("task created"), logtest.WithAttributeValue("task_id", task.ID))
	assert.Equal(t, int64(1), metrictest.Int64Sum(t, app.Metrics, "tasks.created"))
}
```

## The APIs

Each API test calls the API, then checks that the `tasks.create` span is a child of the server span.

```go
// httpapi/handlers_test.go
response := app.HTTP(http.MethodPost, "/api/tasks", `{"title": "test_title"}`)
require.Equal(t, http.StatusCreated, response.Code)

server := spantest.Find(app.Spans, spantest.WithName("POST /api/tasks"))
spantest.HasSpan(t, app.Spans, spantest.WithName("tasks.create"), spantest.WithParent(server[0].SpanContext.SpanID()))
```

```go
// grpcapi/service_test.go
task, err := app.GRPC.CreateTask(t.Context(), &tasksv1.CreateTaskRequest{Title: "test_title"})
require.NoError(t, err)

_, err = app.GRPC.GetTask(t.Context(), &tasksv1.GetTaskRequest{Id: "missing"})
assert.Equal(t, codes.NotFound, status.Code(err))

server := spantest.Find(app.Spans, spantest.WithName("tasks.v1.TasksService/CreateTask"))
```

```go
// mcpapi/tools_test.go
result, err := app.MCP.CallTool(t.Context(), &mcp.CallToolParams{
	Name:      "create_task",
	Arguments: mcpapi.CreateInput{Title: "test_title"},
})
require.NoError(t, err)
assert.False(t, result.IsError)

server := spantest.Find(app.Spans, spantest.WithName("tools/call create_task"))
```

The requests go through the whole servers, with their instrumentation, middlewares and interceptors: the same code as in production, without ports. See the testing pages of [httpserver](../httpserver/testing.md), [grpcserver](../grpcserver/testing.md) and [mcpserver](../mcpserver/testing.md).
