---
title: SQL
description: The sql module, a database/sql connection pool to PostgreSQL, MySQL or SQLite, instrumented with OpenTelemetry.
---

The `sql` module provides a standard [`*sql.DB`](https://pkg.go.dev/database/sql#DB) connection pool to PostgreSQL, MySQL or SQLite, instrumented with OpenTelemetry, with a health probe and [migrations](migrations.md).

```shell
go get github.com/go-oryn/oryn/sql
```

## Quick start

```yaml
# configs/app.yaml
sql:
  driver: postgres
  dsn: postgres://app:${DATABASE_PASSWORD}@localhost:5432/app
```

```go
func main() {
	core.New(
		core.WithConfigFS(configs.FS),
		sql.Module,
		fx.Provide(NewUserRepository),
	).Run()
}
```

The `*sql.DB` is injected like any dependency. Always pass the context, so queries are traced as children of the current span:

```go
import "database/sql"

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Find(ctx context.Context, id int64) (User, error) {
	var user User

	err := r.db.QueryRowContext(ctx, "SELECT id, name FROM users WHERE id = $1", id).Scan(&user.ID, &user.Name)

	return user, err
}
```

The oryn package is named `sql` like `database/sql`: in the files using both, import it with an alias, e.g. `orynsql "github.com/go-oryn/oryn/sql"`.

## Drivers

| `sql.driver` | Database | Driver | DSN example |
|---|---|---|---|
| `postgres` | PostgreSQL | [pgx](https://github.com/jackc/pgx) | `postgres://user:password@localhost:5432/app?sslmode=disable` |
| `mysql` | MySQL, MariaDB | [go-sql-driver/mysql](https://github.com/go-sql-driver/mysql) | `user:password@tcp(localhost:3306)/app?parseTime=true` |
| `sqlite` | SQLite | [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite), pure Go, no cgo | `app.db`, `file:app.db?_pragma=foreign_keys(1)` |

The drivers are included in the module: there is nothing else to import. Query placeholders are those of the database: `$1` for PostgreSQL, `?` for MySQL and SQLite.

## Lifecycle

- When the application starts, the database is pinged: the application fails to start if it is unreachable. Disable it with `sql.ping_on_start: false`.
- The module registers a readiness and startup [health probe](../core/health.md) named `sql`, pinging the database. It is not a liveness probe: a database outage must not restart the application.
- The pool is closed when the application stops, after the servers.

## Observability

Every query is traced and measured, see [Observability](observability.md).

## Configuration

The `sql.*` keys set the database, the pool, the ping on start and the query text in spans. See [Configuration](configuration.md).
