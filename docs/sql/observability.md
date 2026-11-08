---
title: Observability
description: The traces and metrics of the sql module, and the migration span.
---

The pool is instrumented with [otelsql](https://github.com/XSAM/otelsql), with the providers of the application.

## Traces

A client span per query, named after the operation (`SELECT`, `INSERT`...), with `db.system.name`, `db.operation.name` and the query text in `db.query.text`. Transactions get `sql.conn.begin_tx`, `sql.tx.commit`... spans.

The query text holds the SQL with its placeholders, not the arguments. Disable it with `sql.observability.query: false`.

## Metrics

| Metric | Description |
|---|---|
| `db.client.operation.duration` | Query duration |
| `db.sql.connection.open`, `db.sql.connection.max_open`, `db.sql.connection.wait`... | Pool statistics |

## Migrations

`sql.Migrate()` traces the migration queries under one `sql.migrate` span, with the number of applied migrations in `sql.migrations.applied`, and logs `database migrated` with `applied=N`. See [Migrations](migrations.md).

## Your own spans

The query spans are children of the span in the context: pass the request context to the `...Context` methods (`QueryContext`, `ExecContext`...), and wrap a business operation in a span of your own to group its queries:

```go
func (s *Service) Create(ctx context.Context, title string) (Task, error) {
	ctx, span := s.telemetry.Tracer().Start(ctx, "tasks.create")
	defer span.End()

	// The INSERT span is a child of tasks.create.
	_, err := s.db.ExecContext(ctx, "INSERT INTO tasks (id, title) VALUES ($1, $2)", id, title)
	...
}
```
