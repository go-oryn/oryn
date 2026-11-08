---
title: Telemetry
description: The traces, metrics and logs of the demo, from the HTTP, gRPC and MCP servers down to the SQL queries, and how to see them on stdout or in Grafana.
---

Every layer of the demo produces telemetry, correlated in the same traces: the API servers and the database pool by the oryn modules, with no code, and the business operations by the `tasks.Service`.

## The tasks service

The service gets the application tracer, meter and logger from the container, as an `otel.Telemetry`:

```go
// tasks/service.go
func NewService(repository *Repository, telemetry otel.Telemetry) (*Service, error) {
	created, err := telemetry.Meter().Int64Counter("tasks.created", metric.WithDescription("Number of created tasks"))
	...
}

func (s *Service) Create(ctx context.Context, title string) (Task, error) {
	ctx, span := s.telemetry.Tracer().Start(ctx, "tasks.create")
	defer span.End()

	task := Task{ID: newID(), Title: strings.TrimSpace(title), CreatedAt: time.Now().UTC()}
	if task.Title == "" {
		return Task{}, s.fail(span, ErrInvalidTitle)
	}

	span.SetAttributes(TaskIDKey.String(task.ID))

	if err := s.repository.Insert(ctx, task); err != nil {
		return Task{}, s.fail(span, err)
	}

	s.created.Add(ctx, 1)
	s.telemetry.Logger().InfoContext(ctx, "task created", "task_id", task.ID)

	return task, nil
}
```

- Each operation has a span: `tasks.list`, `tasks.get`, `tasks.create`, `tasks.complete`, `tasks.delete`, with the `task.id` attribute.
- Unexpected errors set the span status to `Error`. Expected ones (not found, invalid title) are only recorded as a `tasks.error` attribute: they are not failures of the service.
- The context carries the span to the repository, so the SQL spans are its children, and to the logger, so the logs are correlated with it.

## Traces

The same `tasks.create` span, whatever the API:

```
POST /api/tasks                         httpserver
└── tasks.create                        tasks.Service
    └── INSERT                          sql

tasks.v1.TasksService/CreateTask        grpcserver
└── tasks.create
    └── INSERT

POST /mcp                               mcpserver (HTTP transport)
└── tools/call create_task              mcpserver
    └── tasks.create
        └── INSERT
```

## Metrics

| Metric | From |
|---|---|
| `http.server.request.duration` | [httpserver](../httpserver/observability.md) |
| `rpc.server.call.duration` | [grpcserver](../grpcserver/observability.md) |
| `mcp.server.operation.duration` | [mcpserver](../mcpserver/observability.md) |
| `db.client.operation.duration`, `db.sql.connection.*` | [sql](../sql/observability.md) |
| `tasks.created`, `tasks.completed` | `tasks.Service` |

## Logs

One log per request (`http request`, `grpc request`, `mcp request`), and one per change (`task created`, `task completed`, `task deleted`), all with the trace and span IDs of the request.

## See them

The default configuration only prints the logs. Choose another environment with `APP_ENV`:

| Command | Telemetry |
|---|---|
| `go run ./cmd/serve` | Readable logs on the console |
| `APP_ENV=stdout go run ./cmd/serve` | Traces, metrics and logs printed on stdout, as JSON |
| `APP_ENV=compose go run ./cmd/serve` | OTLP to the Grafana stack of `compose.yaml`, with PostgreSQL |

To explore them in Grafana, start [PostgreSQL and grafana/otel-lgtm](https://github.com/grafana/docker-otel-lgtm) (an OpenTelemetry collector, Tempo, Prometheus and Loki):

```shell
docker compose up -d
APP_ENV=compose go run ./cmd/migrate
APP_ENV=compose go run ./cmd/serve
```

Open Grafana on [localhost:3000](http://localhost:3000): the traces are in Tempo, the metrics in Prometheus, the logs in Loki. The `X-Trace-Id` header of an HTTP response gives the trace to look for.

The standard `OTEL_*` variables work too, for example `OTEL_EXPORTER_OTLP_ENDPOINT` to send the telemetry elsewhere. See [OpenTelemetry](../core/opentelemetry.md).
