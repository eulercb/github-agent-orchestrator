# Workflow notes for Claude Code sessions

Lessons from the initial build session. This serves as memory for future sessions.

## Development workflow

### Before pushing any commit

Always run the full check locally:

```bash
make all    # or: golangci-lint run ./... && go test -race -count=1 ./... && go build ./...
```

CI failures from lint are common because the linter config is strict (14 linters + gocritic with diagnostic/style/performance tags). Running locally catches everything CI would catch.

### PR review process

This project has Copilot auto-reviews enabled. Expect 10-20 review comments per push. The pattern:

1. **Push triggers Copilot review** with code comments
2. **Address all comments in a single commit** - read them all first, batch fixes
3. **Reply to each comment** confirming the fix (or explaining why it's deferred)
4. **Copilot reviews again** on each new push - typically fewer comments each round
5. **Performance/architecture comments** can be deferred to issues with a reply explaining the tradeoff

Copilot often catches real bugs (truncation panics, key collisions, unhandled errors) mixed with style suggestions. Take code correctness comments seriously. Doc drift (docs not matching code) is also commonly flagged after refactors.

### What Copilot consistently flags

- UTF-8 safety: byte slicing on strings that could contain multi-byte chars
- Negative index / width panics on edge cases (empty lists, narrow terminals)
- Shell injection: unquoted values interpolated into `sh -c` commands
- Error handling: ignored errors, missing `%w` in `fmt.Errorf`
- Doc/code drift: docs describing old behavior after code changes
- Unused fields or config that's documented but not wired up

### Deferred work pattern

When a review comment is valid but too large for the current PR:

1. Acknowledge the comment in a reply
2. Reference a specific issue number (verify the issue actually covers the concern)
3. If it doesn't, update the issue with the specific detail

## Code patterns specific to this project

### Adding a new TUI keybinding

1. Add the key to `KeyMap` in `internal/tui/keymap.go`
2. Handle it in `model.handleKey()` in `internal/tui/model.go` - gate it to the correct panel (`PanelIssues` or `PanelSessions`)
3. Add it to the help bar in `renderHelpBar()` in `internal/tui/view.go`
4. Add it to the help screen in `viewHelp()` in `internal/tui/view.go`

### Adding a new async operation

1. Define a message type (e.g., `type fooCompletedMsg struct { ... }`)
2. Create a `tea.Cmd` producer method on `*Model` (e.g., `func (m *Model) doFoo() tea.Cmd`)
3. Handle the message in `Update()` switch
4. Never do I/O in `Update()` directly - always return a `tea.Cmd`

### Wrapping a new external CLI

1. Create a new package under `internal/`
2. Use `exec.CommandContext(context.Background(), ...)` for all commands
3. Parse structured output (JSON with `--json` flag, tab-delimited with `-F`)
4. Return Go types, don't expose raw CLI output
5. Wrap errors with `fmt.Errorf("context: %w", err)` and include stderr for exit errors

### Shell command safety

When building shell commands:
- Use `shellQuoteSession()` (in `internal/tui/model.go`) for all user-controlled or config-controlled values interpolated into `sh -c` strings
- The `resolveAttachCommand()` in `model.go` demonstrates this pattern

## Session management tips

### Long sessions with many review rounds

Context can fill up across multiple review rounds. Key facts to preserve:
- The lint config uses golangci-lint **v2** format
- CI uses `golangci-lint-action@v7` (not v6)
- Go version is **1.24** across go.mod, CI, and docs
- The `tea.Model` interface requires **value receivers** on `Init`/`Update`/`View`

### When CI fails but lint passes locally

Check these first:
1. golangci-lint version mismatch (CI vs local)
2. Go version mismatch (go.mod vs CI `setup-go`)
3. Action version mismatch (e.g., lint-action@v6 vs @v7)
