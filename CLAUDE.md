# Oryn

Go framework built on uber/fx, OpenTelemetry native. One Go module per top level directory (`github.com/go-oryn/oryn/<module>`), released independently with release-please. See [CONTRIBUTING.md](CONTRIBUTING.md).

## Module conventions

- Every module exposes `var Module = fx.Module(ModuleName, ...)`.
- Extension points are fx value groups, registered with `AsXxx(...)` helpers (`config.AsOptions`, `health.AsProbe`, `log.AsHandler`...). Group names are prefixed with `oryn-`.
- fx shuffles value groups: never rely on their order. When order matters, number the values at registration (see `config.AsOptions`).
- Things that run with the application (servers, workers) are started by a `Serve()` / `Run...()` option invoking lifecycle hooks, never by `Module` itself.
- Instrumentation libraries get the providers (`trace.TracerProvider`, `metric.MeterProvider`, `propagation.TextMapPropagator`) from the container, never the globals, and use their own instrumentation scope.
- A standard `OTEL_*` environment variable always wins over the equivalent `otel.*` config key.
- Config keys are namespaced by module (`httpserver.address`), documented in the module README.
- No business or company specific code.

## Go testing guidelines

- Use black box tests, in the `_test` package (e.g. `package config_test`). To test internals, export them from an `export_test.go` file.
- Name test files after the source file they test.
- Group related cases under one `Test` function, with `t.Run` sub tests.
- Call `t.Parallel()` in every test and sub test that does not mutate shared state, followed by an empty line. Tests using `t.Setenv` must not be parallel.
- Use `t.Context()` instead of creating contexts.
- Use testify: `require` for errors that stop the test, `assert` for value checks.
- Extract shared setup into helpers calling `t.Helper()`, in the main `_test.go` file of the package.
- Prefer `fstest.MapFS` for config fixtures; use `testdata/` for larger ones.
- Use simple, descriptive fixture names (`test_string`, `test_value`).
- Run applications with `coretest.Start`, and assert telemetry with the `otel/oteltest` packages.

## Documentation

- Add Go doc comments to all exported identifiers, and a package comment in the main file of each package.
- The user documentation lives in `docs/` (plain Markdown, rendered by the Starlight site in `website/`). Keep it in sync with the code, in particular the config keys tables. Module READMEs stay short and link to it.
- Docs pages have a `title` and `description` frontmatter, no MDX. Link pages with relative `.md` links. Register new pages in the `website/astro.config.mjs` sidebar.

## Checks

```shell
go test -race ./...
golangci-lint run --config ../.golangci.yml ./...
```
