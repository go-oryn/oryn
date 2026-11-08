---
title: Validator
description: The validator module, struct and value validation with go-playground/validator, custom validations registered through fx, and field errors ready for API responses.
---

The `validator` module validates structs and values with the rules of their `validate` tags, using [go-playground/validator](https://github.com/go-playground/validator), and returns field errors ready to send in an API response. Validation failures are recorded on the active span and counted.

```shell
go get github.com/go-oryn/oryn/validator
```

## Quick start

```go
func main() {
	core.New(
		core.WithConfigFS(configs.FS),
		httpserver.Module,
		validator.Module,
		httpserver.AsHandler("POST /users", NewCreateUserHandler),
	).Run(httpserver.Serve())
}
```

- `validator.Module` provides the `*validator.Validator`, and the underlying go-playground `*validator.Validate`.
- Custom validations, struct validations and aliases are registered with `AsValidation`, `AsStructValidation` and `AsAlias`, see [Custom validations](#custom-validations).
- Failures are recorded on the span of the context, see [Observability](observability.md).

## Validate a request

Describe the rules with the [go-playground tags](https://pkg.go.dev/github.com/go-playground/validator/v10#readme-baked-in-validations), then call `Struct` with the request context:

```go
type CreateUserRequest struct {
	Name    string   `json:"name"    validate:"required,max=100"`
	Email   string   `json:"email"   validate:"required,email"`
	Role    string   `json:"role"    validate:"oneof=admin member"`
	Address Address  `json:"address"`
	Tags    []string `json:"tags"    validate:"max=5,dive,min=1"`
}

type Address struct {
	City string `json:"city" validate:"required"`
}

func NewCreateUserHandler(v *validator.Validator, users *UserService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var request CreateUserRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		if err := v.Struct(r.Context(), request); err != nil {
			var errs validator.ValidationErrors
			if errors.As(err, &errs) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"errors": errs})
				return
			}

			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		...
	}
}
```

```json
{
  "errors": [
    {"field": "email", "tag": "email", "message": "must be a valid email address"},
    {"field": "address.city", "tag": "required", "message": "is required"},
    {"field": "tags", "tag": "max", "param": "5", "message": "must be at most 5 items long"}
  ]
}
```

- `Struct(ctx, s)` validates a struct, or a pointer to one: nested structs are validated too. Required structs are enabled: `validate:"required"` on a struct field fails on its zero value.
- `Var(ctx, value, tags)` validates a single value: `v.Var(ctx, email, "required,email")`. Its field errors have no field.
- Both return `validator.ValidationErrors` when a field is invalid, and another error when the input cannot be validated (a nil pointer, a value that is not a struct): answer `500` to those.

`ValidationErrors` is a list of `FieldError`:

| Field | Description |
|---|---|
| `Field` | Path of the field, named after its `json` tag, without the struct name: `email`, `address.city`, `tags[2]` |
| `Tag` | Failed tag: `required`, `email`, `max`... |
| `Param` | Parameter of the tag, like `100` for `max=100` |
| `Message` | English message: `is required`, `must be at most 100 characters long`... |

`errs.Fields()` returns a map of field to message, convenient for forms. Unknown tags get a generic message, `must satisfy <tag>`.

## Custom validations

### Field validations

`AsValidation` registers a tag. The validation is a go-playground `Func` or `FuncCtx`, or a constructor returning one, whose dependencies are injected by fx. Import go-playground with an alias, `playground "github.com/go-playground/validator/v10"`, in the files declaring validations:

```go
// A value: no dependency.
validator.AsValidation("slug", func(fl playground.FieldLevel) bool {
	return slugPattern.MatchString(fl.Field().String())
})

// A constructor: the validation uses a repository, and the context of the validation.
func NewUniqueEmailValidation(users *UserRepository) playground.FuncCtx {
	return func(ctx context.Context, fl playground.FieldLevel) bool {
		exists, err := users.EmailExists(ctx, fl.Field().String())
		return err == nil && !exists
	}
}

validator.AsValidation("unique_email", NewUniqueEmailValidation)
```

```go
type CreateUserRequest struct {
	Email string `json:"email" validate:"required,email,unique_email"`
	Slug  string `json:"slug"  validate:"required,slug"`
}
```

### Struct validations

A struct validation, a `StructLevelFunc` or `StructLevelFuncCtx` or a constructor returning one, checks fields together. It reports its errors with `ReportError(value, fieldName, structFieldName, tag, param)`:

```go
func validatePeriod(sl playground.StructLevel) {
	period := sl.Current().Interface().(Period)
	if period.End.Before(period.Start) {
		sl.ReportError(period.End, "end", "End", "after_start", "")
	}
}

validator.AsStructValidation(validatePeriod, Period{})
```

It runs after the field validations of the struct. A type has one struct validation: the last registered one wins.

### Aliases

An alias names a list of tags:

```go
validator.AsAlias("username", "required,min=3,max=30,alphanum")
```

A tag registered twice, by a validation or an alias, or a reserved tag (`omitempty`, `dive`...), fails the application start.

## Configuration

The `validator.*` keys set the tag naming the fields, and the observability. See [Configuration](configuration.md).
