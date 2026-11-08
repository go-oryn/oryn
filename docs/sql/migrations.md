---
title: Migrations
description: Apply SQL migrations when the application starts, with goose.
---

The `sql` module applies migrations with [goose](https://pressly.github.io/goose/): SQL files, numbered, applied in order, each once.

## Write migrations

Put the migration files in a directory, and embed it:

```
migrations/
├── 00001_create_users.sql
├── 00002_add_users_email.sql
└── embed.go
```

```sql
-- migrations/00001_create_users.sql

-- +goose Up
CREATE TABLE users (
    id   BIGSERIAL PRIMARY KEY,
    name TEXT NOT NULL
);

-- +goose Down
DROP TABLE users;
```

```go
// migrations/embed.go
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
```

See the [goose annotations](https://pressly.github.io/goose/documentation/annotations/) for statements spanning several lines, or running outside a transaction.

## Apply them on start

Register the migrations with `sql.AsMigrations`, and pass `sql.Migrate()` to `Run`, **before** the servers:

```go
func main() {
	core.New(
		core.WithConfigFS(configs.FS),
		sql.Module,
		sql.AsMigrations(migrations.FS),
		httpserver.Module,
		// ...
	).Run(sql.Migrate(), httpserver.Serve())
}
```

When the application starts, the pending migrations are applied, in order, before the servers start: requests never hit an outdated schema. Applied migrations are recorded in the `goose_db_version` table. The application fails to start if a migration fails, and the `database migrated` log tells how many were applied. The migration queries are traced under a single `sql.migrate` span, with a `sql.migrations.applied` attribute.

- Register one migrations file system, with the files at its root.
- With several instances of the application, migrations may run concurrently: prefer a dedicated command, see below.

## Apply them with a command

`RunOnce` starts the application then stops it: a command applying the migrations, for a deployment job or an init container, is one line.

```go
// cmd/migrate/main.go
func main() {
	app.App.RunOnce(sql.Migrate())
}
```

```shell
go run ./cmd/migrate
```

Only the database pool is built: the servers and handlers of the application are not. See [Several entrypoints](../core/application.md#several-entrypoints), and the [demo](../demo/entrypoints.md).
