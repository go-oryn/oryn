---
title: Demo application
description: A complete oryn application, a tasks API with a database, migrations, health checks and a local observability stack.
---

The [go-oryn/demo](https://github.com/go-oryn/demo) repository is a small but complete oryn application: a tasks API, using the [core](core/application.md), [httpserver](httpserver/overview.md) and [sql](sql/overview.md) modules. Use it to see oryn running, or as a starting point.

## What it shows

| Feature | Where |
|---|---|
| Application assembled from modules | `app.go` |
| Migrations applied, then health and HTTP servers started | `main.go` |
| Handler group with a middleware, and a global middleware | `tasks/module.go`, `tasks/middleware.go` |
| Handlers and a repository built by fx, with their dependencies | `tasks/` |
| SQL queries traced, migrations embedded | `tasks/repository.go`, `migrations/` |
| Custom span attributes, metric and logs | `tasks/repository.go` |
| Configuration per environment | `configs/` |
| Tests asserting responses, spans and logs, in memory | `app_test.go` |

The queries use `$1` placeholders, so the demo runs on SQLite and PostgreSQL.

## Run it

```shell
git clone https://github.com/go-oryn/demo.git
cd demo
go run .
```

It starts with a SQLite database file, and readable logs on the console:

```shell
curl -X POST localhost:8080/api/tasks -d '{"title": "Try oryn"}'
curl localhost:8080/api/tasks
curl localhost:8081/readyz?verbose
```

Every response has an `X-Trace-Id` header: the ID of the trace of the request.

## See the telemetry

To print the traces, metrics and logs on stdout, without any setup:

```shell
APP_ENV=stdout go run .
```

To explore them in Grafana, start PostgreSQL and the [grafana/otel-lgtm](https://github.com/grafana/docker-otel-lgtm) all-in-one observability stack (an OpenTelemetry collector, Tempo, Prometheus and Loki), then run the demo against them:

```shell
docker compose up -d
APP_ENV=compose go run .
```

Open Grafana on [localhost:3000](http://localhost:3000), and explore the traces in Tempo, the metrics in Prometheus, and the logs in Loki. A `POST /api/tasks` trace shows the server span, the `INSERT` query span, and the logs correlated with them.

## Test it

```shell
go test ./...
```

The tests run the whole application in memory, with a SQLite database per test, and assert the HTTP responses together with the spans and logs they produce.
