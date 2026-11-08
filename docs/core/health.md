---
title: Health checks
description: Liveness, readiness and startup probes, and the health HTTP server.
---

The health checker runs probes for three kinds of check, matching the [Kubernetes probes](https://kubernetes.io/docs/concepts/configuration/liveness-readiness-startup-probes/):

| Kind | Endpoint | Question |
|---|---|---|
| `health.Liveness` | `/livez` | Is the application alive? A failure means it should be restarted |
| `health.Readiness` | `/readyz` | Can the application receive traffic? |
| `health.Startup` | `/startupz` | Has the application started? |

## Probes

A probe has a name, and checks one part of the application:

```go
type DatabaseProbe struct {
	db *sql.DB
}

func NewDatabaseProbe(db *sql.DB) *DatabaseProbe {
	return &DatabaseProbe{db: db}
}

func (p *DatabaseProbe) Name() string {
	return "database"
}

func (p *DatabaseProbe) Check(ctx context.Context) error {
	return p.db.PingContext(ctx)
}
```

Register it with `health.AsProbe`, for some kinds of check, or all of them when none is given:

```go
health.AsProbe(NewDatabaseProbe, health.Readiness, health.Startup)
```

Probes run concurrently, and the whole check must finish within `health.timeout`. A panicking probe fails, without crashing the application. The readiness check fails as soon as the application starts shutting down, so load balancers stop sending traffic before the servers stop.

## Health server

`health.Serve()` starts an HTTP server with the application, exposing the three endpoints. They answer `200` when healthy and `503` otherwise, with the probe results as JSON when the `verbose` query parameter is present:

```shell
curl 'localhost:8081/readyz?verbose'
```

```go
core.New(
	core.WithConfigFS(configs.FS),
	health.AsProbe(NewDatabaseProbe, health.Readiness),
).Run(health.Serve())
```

| Config key | Default |
|---|---|
| `health.server.address` | `:8081` |
| `health.timeout` | `5s` |

To check health in code, inject the `*health.Checker` and call `Check(ctx, kind)`.
