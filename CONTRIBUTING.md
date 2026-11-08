# Contributing

## Repository layout

Each top level directory holding a `go.mod` is a Go module, imported as `github.com/go-oryn/oryn/<module>`.
The committed `go.work` makes all the modules use each other's local code during development.

## Local development

```shell
cd core
go test -race ./...
golangci-lint run --config ../.golangci.yml ./...
golangci-lint fmt --config ../.golangci.yml ./...
```

## Commit conventions

This repository uses [Conventional Commits](https://www.conventionalcommits.org/) and [release-please](https://github.com/googleapis/release-please). Scope commits with the module name:

| Prefix | Version bump | Example |
|---|---|---|
| `fix(module): ...` | Patch | `fix(core): Flush logs on shutdown` |
| `feat(module): ...` | Minor | `feat(httpserver): Add route groups` |
| `feat(module)!: ...` | Major (*) | `feat(core)!: Rename App.Run` |

(*) While a module is pre `v1`, a breaking change bumps the minor version and a feature bumps the patch version.

## Releases

On every push to `main`, release-please opens or updates one release PR per changed module, with its changelog.
Merging a release PR tags the module (e.g. `core/v0.2.0`), which is the tag format Go expects for a module in a sub directory.

When a module depends on another one, bump the dependency once the other module is released:

```shell
cd httpserver
go get github.com/go-oryn/oryn/core@v0.2.0
go mod tidy
```

CI tests every module twice:

- **Test** (required): with `go.work`, against the current code of the other modules.
- **Release mode** (informative): without `go.work`, against their released versions. It fails while a change waits for another module to be released.

### Repository setup

- Create a GitHub App with `contents: write` and `pull-requests: write` permissions, install it on the repository, and set the `RELEASE_APP_ID` variable and the `RELEASE_APP_PRIVATE_KEY` secret. Release PRs opened with the default token would not trigger CI.
- Install the [Codecov GitHub App](https://github.com/apps/codecov) on the repository. The coverage upload authenticates with GitHub OIDC: no token to store.
- Protect `main`, requiring the `CI` check.

## Adding a module

1. Create the directory and the Go module:
   ```shell
   mkdir foo && cd foo
   go mod init github.com/go-oryn/oryn/foo
   cd .. && go work use ./foo
   ```
2. Register it in [release-please-config.json](release-please-config.json):
   ```json
   "foo": { "component": "foo", "initial-version": "0.1.0" }
   ```
3. Register its coverage flag in [codecov.yml](codecov.yml):
   ```yaml
   - name: foo
     paths:
       - foo/
   ```
4. Add the code, tests and a `README.md`, following [CLAUDE.md](CLAUDE.md).
5. Add it to the modules table of the [README](README.md).
