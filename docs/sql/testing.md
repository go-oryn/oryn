---
title: Testing
description: Test database code with SQLite, migrations, and the query spans.
---

Tests can run against a real database, or against SQLite, which needs no server.

## SQLite in a temporary file

Use a SQLite database in the test temporary directory: each test gets its own, deleted after the test.

```go
func TestUserRepository(t *testing.T) {
	t.Parallel()

	var (
		repository *UserRepository
		spans      *tracetest.InMemoryExporter
	)

	coretest.Start(t,
		core.New(sql.Module, sql.AsMigrations(migrations.FS), fx.Provide(NewUserRepository)),
		config.AsOptions(config.WithValues(map[string]any{
			"sql.driver": "sqlite",
			"sql.dsn":    filepath.Join(t.TempDir(), "test.db"),
		})),
		sql.Migrate(),
		fx.Populate(&repository, &spans),
	)

	user, err := repository.Create(t.Context(), "test_user")
	require.NoError(t, err)

	spantest.HasSpan(t, spans, spantest.WithName("INSERT"))
}
```

- `sql.Migrate()` applies the migrations before the test runs, like in production.
- Prefer a file over `:memory:`: an in-memory SQLite database exists per connection, and the pool opens several.
- SQL must then work with both databases: placeholders (`?` works in SQLite and MySQL, `$1` in SQLite and PostgreSQL) and types differ. When they do not, test against the real database.

## A real database

Set `sql.driver` and `sql.dsn` to a database started for the tests, for example with [Testcontainers](https://golang.testcontainers.org/) or a CI service, and skip the tests when it is not available:

```go
dsn := os.Getenv("TEST_POSTGRES_DSN")
if dsn == "" {
	t.Skip("TEST_POSTGRES_DSN is not set")
}
```
