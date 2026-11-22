# Pull Request Submission Guidelines

To ensure the quality of the codebase and maintainability of the project, please follow these guidelines before submitting a Pull Request (PR):

## 1. PR Title and Description

- **Clear Title**: Concisely describe the main content of the PR, for example:
    - Fix: Correct error messages in user login
    - Feature: Add order export functionality

- **Detailed Description**: Include the following details in the description:
    - Purpose and background of this PR.
    - Detailed explanation of the changes.
    - For bug fixes, describe the steps to reproduce the issue.
    - For new features, explain how to use them.
    - Link related issues (if any) using keywords like `Closes #123`.

## 2. Code Checks Before Submission

- **Code Style**: Ensure the code adheres to the project's coding standards (e.g., ESLint, Prettier, or GoLint).
- **Functional Testing**: Fully test new features or bug fixes to ensure no missing functionality or regressions.
- **Unit Tests**: Write unit tests for added or modified functionality and ensure all tests pass.
- **Documentation Updates**: Update documentation if the PR includes new features or API changes.
- **File Names**: Go files and directories are lowercase snake_case (`update_server_handler.go`, not
  `updateServerHandler.go`); `internal/arch` fails on any other name.

### Local checks

The Makefile runs the same checks as CI. The Go tools it needs (goimports,
golangci-lint) are installed on first use, at the versions the Makefile pins,
into `bin/tools` (ignored by git); govulncheck runs through `go run` at its
pinned version, so it only lives in the Go module cache. Nothing is installed
globally.

| Command | What it does |
|---|---|
| `make check` | Formatting check, `go vet`, golangci-lint, unit tests. Run it before pushing. |
| `make fmt` / `make fmt-check` | Rewrite / check formatting (`gofmt` and `goimports`). |
| `make vet` | `go vet ./...` |
| `make lint` | golangci-lint on the whole tree, as CI runs it: any finding fails the build. |
| `make lint-new LINT_BASE=origin/dev` | golangci-lint, reporting only lines changed since `LINT_BASE` (default `HEAD`, i.e. uncommitted work); the pre-commit hook's quick pass. |
| `make test` / `make test-race` | Unit tests, optionally with the race detector. |
| `make vulncheck` | govulncheck for reachable known vulnerabilities. |
| `make proto` / `make proto-check` | Regenerate `api/**/*.pb.go` / fail if the committed files differ. Needs protoc 21.12 (it reports `libprotoc 3.21.12`) on `PATH`; protoc-gen-go is built at the version `go.mod` requires. |
| `make linux-amd64` (and the other platform targets) | Build a release binary. `VERSION`, `CHANNEL` (`stable`, `beta`, `nightly`, `dev`) and `BUILD_TIME` (UTC, `YYYY-MM-DDTHH:MM:SSZ`, default now) set the injected build metadata; `make ldflags` prints the flags. |

The database-backed tests are skipped unless a DSN is set, as CI does:

```sh
PPANEL_TEST_MYSQL_DSN='root:mysql@tcp(127.0.0.1:3306)/ppanel?charset=utf8mb4&parseTime=true&loc=UTC&interpolateParams=true' \
PPANEL_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:5432/ppanel?sslmode=disable' \
go test ./...
```

When a change adds, removes or rewires an HTTP route, refresh the route
inventory and review its diff:

```sh
go test ./internal/transport/http/routes -run TestRegisterHandlers_routeInventory -update
```

### Git hooks

[lefthook](https://github.com/evilmartians/lefthook) runs the checks on commit. Install lefthook
1.10 or newer once (for example `brew install lefthook`, or
`go install github.com/evilmartians/lefthook/v2@latest`), then enable the hooks
in your clone:

```sh
lefthook install
```

- **pre-commit** formats the staged Go files with goimports and stages the
  result, then runs golangci-lint on the changed lines, `go vet` and the unit
  tests.
- **commit-msg** checks the message against the
  [Conventional Commits](https://www.conventionalcommits.org) rules with
  commitlint. The repository has no `package.json`: the hook runs a pinned
  commitlint through `npx`, so it needs Node.js 22.12 or newer on `PATH`.

## 3. Branch Strategy

- **Correct Branch**:
    - Develop new features based on `feature/*` branches.
    - Fix bugs based on `fix/*` branches.
    - Ensure the target branch of the PR aligns with the project's branching strategy.

- **Sync with Base Branch**: Before submitting the PR, ensure your branch is up-to-date with the target branch (`main` or `dev`).

## 4. Review Process

- **Small Commits**: Avoid submitting excessive changes in a single PR; break it into smaller logical units.

---

Thank you for your contribution!
