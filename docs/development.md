# Development

## Prerequisites

- Go 1.24+
- [golangci-lint](https://golangci-lint.run/welcome/install/) (for linting)

## Commands

```bash
make build        # Build binary to ./build/gao
make install      # Install to $GOPATH/bin
make test         # Run tests with race detector
make test-cover   # Run tests with coverage report (opens coverage.html)
make lint         # Run golangci-lint
make fmt          # Format code (gofmt + goimports)
make all          # lint + test + build
make clean        # Remove build artifacts
```

## Project layout

```
cmd/gao/           CLI entry point
internal/config/   Configuration (YAML loading, defaults)
internal/github/   GitHub integration (gh CLI wrapper)
internal/process/  Background process management
internal/claude/   Claude Code session lifecycle
internal/statusbar/ Customizable status bar
internal/tui/      Bubble Tea terminal UI
docs/              Documentation
```

See [architecture.md](architecture.md) for detailed package descriptions and data flow.

## Running locally

```bash
# Build and run
go run ./cmd/gao

# Or build first
make build
./build/gao

# Create a test config
./build/gao init
```

For development without a real GitHub repo, you can create a config pointing to any public repo:

```yaml
repos:
  - owner: charmbracelet
    name: bubbletea
    filters:
      state: open
```

## Testing

```bash
# All tests
make test

# Specific package
go test -v ./internal/config/...
go test -v ./internal/claude/...

# With coverage
make test-cover
open coverage.html
```

Tests avoid external dependencies (no `gh` or network calls in unit tests). The `config` tests use `t.TempDir()` and `t.Setenv("XDG_CONFIG_HOME", ...)` for isolation. The `claude` tests cover helper functions (status parsing). The `statusbar` tests use simple shell commands (`echo`, `false`).

## Linting

The project uses [golangci-lint](https://golangci-lint.run/) with these linters enabled:

- **errcheck**, **govet**, **staticcheck** — correctness and simplification checks
- **ineffassign**, **unused**, **unconvert**, **unparam** — dead code and simplification
- **gocritic**, **revive** — style and best practices
- **gosec** — security (G204 excluded since we intentionally shell out)
- **gofmt**, **goimports**, **misspell**, **whitespace** — formatting
- **bodyclose**, **noctx** — HTTP hygiene

```bash
make lint
```

## CI

GitHub Actions runs on every push to `main` and every PR:

- **test** job: `go build ./...` + `go test -race ./...`
- **lint** job: `golangci-lint` via the official action

See [`.github/workflows/ci.yml`](../.github/workflows/ci.yml).

## Code conventions

- **Package comments**: Every package has a doc comment on its first file.
- **Error wrapping**: Use `fmt.Errorf("context: %w", err)` to preserve the error chain.
- **No global state**: All state flows through structs (`Manager`, `Client`, `Model`). No `init()` functions.
- **External tools**: Wrap in a dedicated package under `internal/`. Keep the wrapper stateless when possible. Parse structured output (JSON, tab-delimited) rather than free-form text.
- **TUI pattern**: Follow the Bubble Tea convention — messages are types, commands are functions returning `tea.Msg`, rendering is pure. No side effects in `View()`.
