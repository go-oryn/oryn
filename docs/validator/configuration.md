---
title: Configuration
description: Every validator configuration key, with its default and when to change it.
---

The `validator.*` keys configure the validator. All of them are optional.

## Reference

```yaml
validator:
  field_name_tag: json        # struct tag naming the fields in the errors
  observability:
    enabled: true             # record the failures on the span, and count them
```

| Key | Type | Default | Environment variable |
|---|---|---|---|
| `validator.field_name_tag` | string | `json` | `APP_VALIDATOR_FIELD_NAME_TAG` |
| `validator.observability.enabled` | bool | `true` | `APP_VALIDATOR_OBSERVABILITY_ENABLED` |

## Field names

The field errors name the fields after their `json` tag, so they match the request the client sent. Fields without the tag, or with `json:"-"`, use their Go name.

For forms decoded from a `form` tag, name the fields after it instead:

```yaml
validator:
  field_name_tag: form
```

```go
type SignupForm struct {
	Email string `form:"email_address" validate:"required,email"`
}
// field error: {"field": "email_address", ...}
```

## Observability

Failures are recorded on the active span and counted, see [Observability](observability.md). Disable it if validation failures are not worth measuring, for example for an internal tool:

```yaml
validator:
  observability:
    enabled: false
```

See [Configuration](../core/configuration.md) for the loading rules.
