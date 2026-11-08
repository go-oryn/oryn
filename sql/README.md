# sql

[![Go Reference](https://pkg.go.dev/badge/github.com/go-oryn/oryn/sql.svg)](https://pkg.go.dev/github.com/go-oryn/oryn/sql)
[![codecov](https://codecov.io/gh/go-oryn/oryn/graph/badge.svg?flag=sql)](https://app.codecov.io/gh/go-oryn/oryn?flags%5B0%5D=sql)

> A `database/sql` connection pool to PostgreSQL, MySQL or SQLite, instrumented with OpenTelemetry, with a health probe and migrations.

```shell
go get github.com/go-oryn/oryn/sql
```

```yaml
sql:
  driver: postgres   # postgres, mysql or sqlite
  dsn: postgres://user:password@localhost:5432/app
```

```go
core.New(
	core.WithConfigFS(configs.FS),
	sql.Module,
	sql.AsMigrations(migrations.FS),
).Run(sql.Migrate(), httpserver.Serve())
```

## Documentation

| Page | Source |
|---|---|
| [SQL](https://go-oryn.github.io/oryn/sql/overview/) (configuration, observability) | [docs/sql/overview.md](../docs/sql/overview.md) |
| [Migrations](https://go-oryn.github.io/oryn/sql/migrations/) | [docs/sql/migrations.md](../docs/sql/migrations.md) |
| [Testing](https://go-oryn.github.io/oryn/sql/testing/) | [docs/sql/testing.md](../docs/sql/testing.md) |
