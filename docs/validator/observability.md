---
title: Observability
description: How the validator records the validation failures on the active span and counts them.
---

Validations are fast and frequent: the validator creates no span. When a validation fails, it records the failure on the span of the context given to `Struct` or `Var`, usually the span of the HTTP request, and counts it.

The values of the fields are never recorded: they may be sensitive.

## Traces

A failed validation adds a `validation failed` event to the active span:

| Attribute | Description |
|---|---|
| `validation.type` | Validated type, like `users.CreateUserRequest`, or `var` for `Var` |
| `validation.fields` | Invalid fields, like `["email", "address.city"]` |

```
POST /users                                     server span
  event: validation failed
    validation.type   = users.CreateUserRequest
    validation.fields = [email address.city]
```

Without a recording span in the context, nothing is added.

## Metrics

| Metric | Type | Description |
|---|---|---|
| `validation.failures` | counter | Number of failed field validations |

Attributes: `validation.type` and `validation.tag`, the failed tag (`required`, `email`...). A request with two invalid fields counts two failures.

```promql
# Most frequent failures, per type and tag
sum by (validation_type, validation_tag) (rate(validation_failures_total[5m]))
```

The metrics are emitted with the instrumentation scope `github.com/go-oryn/oryn/validator`, with the meter provider of the application.

## Disable

```yaml
validator:
  observability:
    enabled: false
```
