---
title: Testing
description: Test validation rules and custom validations in unit tests, and validation failures through the application.
---

## Unit tests

`validatortest.New` returns a `*validator.Validator` built like in the application, with your registrations, without writing an application:

```go
func TestCreateUserRequest(t *testing.T) {
	t.Parallel()

	v := validatortest.New(t, validator.AsAlias("username", "required,min=3,max=30,alphanum"))

	err := v.Struct(t.Context(), CreateUserRequest{Name: "a", Email: "invalid"})

	var errs validator.ValidationErrors
	require.ErrorAs(t, err, &errs)
	assert.Equal(t, map[string]string{
		"name":  "must be at least 3 characters long",
		"email": "must be a valid email address",
	}, errs.Fields())
}
```

A custom validation with dependencies gets them from the options, like in the application. Replace a real dependency with a fake:

```go
v := validatortest.New(t,
	fx.Supply(fakeUserRepository{existing: "taken@example.com"}),
	validator.AsValidation("unique_email", NewUniqueEmailValidation),
)

require.Error(t, v.Var(t.Context(), "taken@example.com", "unique_email"))
require.NoError(t, v.Var(t.Context(), "free@example.com", "unique_email"))
```

## Through the application

Test the handler with [coretest](../core/testing.md): the `400` response, and the `validation failed` event on the request span.

```go
func TestCreateUserInvalid(t *testing.T) {
	t.Parallel()

	var (
		server  *http.Server
		spans   *tracetest.InMemoryExporter
		metrics *sdkmetric.ManualReader
	)

	coretest.Start(t, core.New(httpserver.Module, validator.Module, users.Module), fx.Populate(&server, &spans, &metrics))

	recorder := httptest.NewRecorder()
	server.Handler.ServeHTTP(recorder, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/users",
		strings.NewReader(`{"name": "test", "email": "invalid"}`)))

	assert.Equal(t, http.StatusBadRequest, recorder.Code)

	request := spantest.Find(spans, spantest.WithName("POST /users"))
	require.Len(t, request, 1)
	assert.Equal(t, validator.EventFailed, request[0].Events[0].Name)

	assert.Equal(t, int64(1), metrictest.Int64Sum(t, metrics, validator.MetricFailures, validator.TagKey.String("email")))
}
```
