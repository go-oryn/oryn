---
title: Observability
description: Traces, metrics and logs of the HTTP server, and how to exclude paths.
---

Every request goes through, from the outermost:

1. the [otelhttp](https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp) instrumentation,
2. the request log,
3. the panic recovery,
4. the routing, the [middlewares](routing.md#middlewares) and the handler.

## Traces

Each request gets a server span:

- named after the route: `GET /users/{id}`, or just the method when no route matches,
- with the [HTTP semantic conventions](https://opentelemetry.io/docs/specs/semconv/http/http-spans/) attributes: `http.route`, `http.request.method`, `http.response.status_code`, `url.path`, `client.address`, body sizes...
- with an error status for `5xx` responses.

The incoming trace context (`traceparent`, `baggage`...) is extracted with the application propagator, so the span continues the trace of the caller. Spans created in handlers with `r.Context()` are children of the request span.

## Metrics

The HTTP server metrics of the semantic conventions, with the `http.route`, `http.request.method` and `http.response.status_code` attributes:

| Metric | Description |
|---|---|
| `http.server.request.duration` | Duration of the requests |
| `http.server.request.body.size` | Size of the request bodies |
| `http.server.response.body.size` | Size of the response bodies |

## Logs

Each request is logged as `http request`, at error level for `5xx` responses and info level otherwise, with:

| Attribute | Example |
|---|---|
| `method` | `GET` |
| `path` | `/users/42` |
| `route` | `GET /users/{id}` |
| `status` | `200` |
| `response_size` | `512` |
| `duration` | `1.2ms` |

The log is correlated with the request span. Disable request logs with `httpserver.log.enabled: false`.

## Panics

A panic in a handler or a middleware is logged at error level, with its stack, and answered with a `500` if nothing was written yet. The span and the request log record the `500`. A `panic(http.ErrAbortHandler)` aborts the response, as with the standard server.

## Exclude paths

Requests to health checks or other noisy endpoints can be excluded from spans, metrics and request logs, with Go regular expressions matched against the request path:

```yaml
httpserver:
  observability:
    exclude:
      - "^/livez$"
      - "^/readyz$"
```
