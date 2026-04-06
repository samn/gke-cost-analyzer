- Use `mise` to manage the toolchain for this project to ensure a consistent development environment. Pin versions.
- Install precommit checks when setting up a new environment (after installing tools with `mise`) by running `prek install`

## Environment Setup

Set up the development environment with `mise`. If `mise` is not installed:

```sh
curl -sSf https://mise.run | sh
export PATH="$HOME/.local/bin:$PATH"
```

Then trust and install the project toolchain:

```sh
mise trust
mise install
```

This installs Go (version pinned in `mise.toml`), `prek` (pre-commit runner),
and `golangci-lint`. If some tools fail to install due to network issues, retry
or install them individually.

After mise tools are installed, the Go binary is at:

```sh
# Use mise exec to run Go commands in the correct environment:
mise exec -- go version
mise exec -- go build ./...
mise exec -- go test ./...

# Or add the Go install to your PATH directly:
export PATH="$(mise where go)/bin:$PATH"
```

Install pre-commit hooks:

```sh
prek install
```
- Make sure that everything compile without warnings.
- Write tests for all functionality that you create. The tests should be robust and reliable.
- Minimize complexity wherever possible
- Use the latest versions of all dependencies and tools, this should be a modern project with no baggage.
- Fix all warnings when you see them
- Ask the user for clarifications if anything is unclear. DO NOT MAKE ASSUMPTIONS!
- Follow the spec in SPEC.md
- Save the plan you're working on as markdown in plans/

## Changelog

All user-facing changes must be documented in `CHANGELOG.md` following the
[Keep a Changelog](https://keepachangelog.com/) format. Add entries under the
`[Unreleased]` section as you make changes. Categories: Added, Changed,
Deprecated, Removed, Fixed, Security.

## Pre-commit checks and CI

Before committing, run the same checks that CI runs. CI executes
`prek run --all-files` which runs all hooks defined in
`.pre-commit-config.yaml`. These checks **must** pass before pushing:

1. **Trailing whitespace** — no trailing spaces on any line
2. **End-of-file fixer** — all files must end with a newline
3. **check-merge-conflict** — no merge conflict markers
4. **check-yaml / check-json / check-toml** — valid config files
5. **detect-private-key** — no private keys committed
6. **check-added-large-files** — no files > 1024 KB
7. **gofmt** (`gofmt -l -w`) — all Go files must be formatted
8. **go vet** (`go vet ./...`) — no vet warnings
9. **go build** (`go build ./...`) — project must compile
10. **go test** (`go test ./...`) — all tests must pass
11. **golangci-lint** (`mise exec -- golangci-lint run`) — no lint warnings

To run all checks locally (same as CI):

```sh
prek run --all-files
```

Or run individual checks manually:

```sh
gofmt -l -w .
go vet ./...
go build ./...
go test -race ./...
mise exec -- golangci-lint run
```

CI is defined in `.github/workflows/ci.yaml` and runs two jobs:
- **Lint & Build**: `prek run --all-files` (all hooks above)
- **Test**: `go test -race -v ./...` (tests with race detector)

Both jobs must pass for a PR to merge.
