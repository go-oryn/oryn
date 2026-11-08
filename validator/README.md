# validator

[![Go Reference](https://pkg.go.dev/badge/github.com/go-oryn/oryn/validator.svg)](https://pkg.go.dev/github.com/go-oryn/oryn/validator)
[![codecov](https://codecov.io/gh/go-oryn/oryn/graph/badge.svg?flag=validator)](https://app.codecov.io/gh/go-oryn/oryn?flags%5B0%5D=validator)

> Struct and value validation with [go-playground/validator](https://github.com/go-playground/validator), with custom validations registered through fx, field errors ready for API responses, and OpenTelemetry instrumentation.

```shell
go get github.com/go-oryn/oryn/validator
```

```go
type CreateUserRequest struct {
	Name  string `json:"name" validate:"required,max=100"`
	Email string `json:"email" validate:"required,email"`
}

func NewCreateUserHandler(v *validator.Validator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request CreateUserRequest
		_ = json.NewDecoder(r.Body).Decode(&request)

		if err := v.Struct(r.Context(), request); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(err) // [{"field": "email", "tag": "email", "message": "must be a valid email address"}]

			return
		}
		...
	}
}
```

## Documentation

| Page | Source |
|---|---|
| [Validator](https://go-oryn.github.io/oryn/validator/overview/) | [docs/validator/overview.md](../docs/validator/overview.md) |
| [Configuration](https://go-oryn.github.io/oryn/validator/configuration/) | [docs/validator/configuration.md](../docs/validator/configuration.md) |
| [Observability](https://go-oryn.github.io/oryn/validator/observability/) | [docs/validator/observability.md](../docs/validator/observability.md) |
| [Testing](https://go-oryn.github.io/oryn/validator/testing/) | [docs/validator/testing.md](../docs/validator/testing.md) |
