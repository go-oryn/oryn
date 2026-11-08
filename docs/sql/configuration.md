---
title: Configuration
description: Every sql configuration key, with its default and when to change it.
---

The `sql.*` keys configure the database connection pool. `sql.driver` and `sql.dsn` are required.

## Reference

```yaml
sql:
  driver: postgres            # postgres, mysql or sqlite (required)
  dsn: postgres://app:${DATABASE_PASSWORD}@localhost:5432/app   # data source name (required)
  max_open_conns: 0           # max open connections, 0 = unlimited
  max_idle_conns: 2           # max idle connections kept in the pool, 0 = none
  conn_max_lifetime: 0s       # max lifetime of a connection, 0 = unlimited
  conn_max_idle_time: 0s      # max idle time of a connection, 0 = unlimited
  ping_on_start: true         # fail the application start if the database is unreachable
  observability:
    query: true               # record the query text in the spans
```

| Key | Type | Default | Environment variable |
|---|---|---|---|
| `sql.driver` | string | required | `APP_SQL_DRIVER` |
| `sql.dsn` | string | required | `APP_SQL_DSN` |
| `sql.max_open_conns` | int | `0` (unlimited) | `APP_SQL_MAX_OPEN_CONNS` |
| `sql.max_idle_conns` | int | `2` (`0` = none) | `APP_SQL_MAX_IDLE_CONNS` |
| `sql.conn_max_lifetime` | duration | `0s` (unlimited) | `APP_SQL_CONN_MAX_LIFETIME` |
| `sql.conn_max_idle_time` | duration | `0s` (unlimited) | `APP_SQL_CONN_MAX_IDLE_TIME` |
| `sql.ping_on_start` | bool | `true` | `APP_SQL_PING_ON_START` |
| `sql.observability.query` | bool | `true` | `APP_SQL_OBSERVABILITY_QUERY` |

## Driver and DSN

| `sql.driver` | DSN example |
|---|---|
| `postgres` | `postgres://app:secret@localhost:5432/app?sslmode=disable` |
| `mysql` | `app:secret@tcp(localhost:3306)/app?parseTime=true` |
| `sqlite` | `app.db`, `file:app.db?_pragma=foreign_keys(1)` |

See [Drivers](overview.md#drivers) for the DSN formats.

Keep the password out of the files, with a [placeholder](../core/configuration.md#environment-variable-placeholders) or the whole DSN in an environment variable:

```yaml
sql:
  dsn: postgres://app:${DATABASE_PASSWORD}@db:5432/app
```

```shell
APP_SQL_DSN="postgres://app:secret@db:5432/app" go run ./cmd/serve
```

A placeholder can have a default: `${DATABASE_PASSWORD:local}`.

## Pool

The defaults are those of [database/sql](https://pkg.go.dev/database/sql#DB.SetMaxOpenConns). In production, bound the pool to protect the database, and recycle the connections so they follow failovers and load balancer changes:

```yaml
sql:
  max_open_conns: 20
  max_idle_conns: 10
  conn_max_lifetime: 30m
  conn_max_idle_time: 5m
```

Size `max_open_conns` with the number of instances: their sum must stay below the database connection limit. The pool usage is measured, see [Observability](observability.md#metrics).

## Ping on start

By default, the application pings the database when it starts, and fails to start if it is unreachable: a misconfigured DSN is found at deployment, not at the first request. To start anyway, for example when the database may come up later, disable it:

```yaml
sql:
  ping_on_start: false
```

The readiness [health probe](../core/health.md) still reports the database state.

## Query text

The query spans record the SQL text in `db.query.text`, with its placeholders, never the arguments. If the queries themselves are sensitive, do not record them:

```yaml
sql:
  observability:
    query: false
```

## Per environment

```yaml
# configs/app.yaml: local development, SQLite
sql:
  driver: sqlite
  dsn: app.db

# configs/prod/sql.yaml (APP_ENV=prod): PostgreSQL
sql:
  driver: postgres
  dsn: postgres://app:${DATABASE_PASSWORD}@db:5432/app
  max_open_conns: 20
  conn_max_lifetime: 30m
```

See [Configuration](../core/configuration.md) for the loading rules.
