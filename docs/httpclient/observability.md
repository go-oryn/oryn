---
title: Observability
description: Traces, metrics and logs of the HTTP clients, trace context propagation, and how to exclude URLs.
---

Every request goes through, from the outermost:

1. the registered [middlewares](overview.md#middlewares),
2. the [otelhttp](https://pkg.go.dev/go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp) instrumentation,
3. the request log,
4. the base transport.

The instrumentation uses the tracer provider, meter provider and propagator of the application, never the OpenTelemetry globals.

## Traces

Each request gets a client span:

- named after the method, `GET`, or `HTTP` for a non-standard method, following the [HTTP semantic conventions](https://opentelemetry.io/docs/specs/semconv/http/http-spans/#http-client),
- with the `http.request.method`, `url.full`, `server.address`, `server.port` and `http.response.status_code` attributes; `url.full` keeps the query, without the user info and the values of known signature parameters,
- with an error status for transport errors and `5xx` responses.

The span is a child of the span of the request context: build the requests with `http.NewRequestWithContext`. The span ends when the response body is closed or fully read.

## Propagation

The trace context is injected in the request headers with the application propagator, `traceparent` and `tracestate` by default. A server instrumented with OpenTelemetry, like the [httpserver](../httpserver/overview.md) module, continues the trace: its server span is a child of the client span.

## Metrics

| Metric | Description |
|---|---|
| `http.client.request.duration` | Duration of the requests, with the `http.request.method`, `server.address`, `server.port` and `http.response.status_code` attributes |
| `http.client.request.body.size` | Size of the request bodies, with the same attributes |

## Logs

Each request is logged as `http client request`, at error level for transport errors and `5xx` responses, and info level otherwise, with:

| Attribute | Example |
|---|---|
| `method` | `GET` |
| `url` | `https://weather.example.com/cities/paris`, without the user info, query and fragment |
| `status` | `200`, unless the request failed |
| `error` | `dial tcp: connection refused`, if the request failed |
| `duration` | `1.2ms`, until the response headers are received |

The query is left out of the log as it often holds secrets, like API keys: avoid secrets in URLs, the span `url.full` attribute keeps it. The log is correlated with the client span. Disable request logs with `httpclient.log.enabled: false`.

## Exclude URLs

To exclude requests from spans, metrics and logs, list URL regular expressions:

```yaml
httpclient:
  observability:
    exclude:
      - "^https://status\\.example\\.com/"
```

See [Configuration](configuration.md#exclude-urls).
