# Architecture

This document is intended for contributors and LLM agents working on the gao codebase. It explains the structure, data flow, and design decisions to help you make changes confidently.

## Project structure

```
cmd/gao/main.go                 CLI entry point and subcommand routing
internal/
├── config/
│   ├── config.go               YAML config loading, saving, defaults
│   └── config_test.go          Config round-trip and default tests
├── github/
│   └── client.go               gh CLI wrapper (issues, PRs, browser open)
├── tmux/
│   └── tmux.go                 tmux session management (create, list, attach, capture, kill)
├── claude/
│   ├── session.go              Session lifecycle, state machine, spawn logic
│   └── session_test.go         Unit tests for status detection helpers
├── statusbar/
│   ├── statusbar.go            Pluggable status bar (shell command or fallback func)
│   └── statusbar_test.go       Status bar provider tests
└── tui/
    ├── model.go                Bubble Tea model, Init/Update, all tea.Cmd producers
    ├── view.go                 All View() rendering (dashboard, help, confirm)
    ├── keymap.go               Key bindings (kept minimal to avoid tmux conflicts)
    └── styles/
        └── styles.go           Lipgloss color palette and component styles
```

## Package dependency graph

```
cmd/gao
  └── tui
       ├── config
       ├── github
       ├── claude
       │    ├── config
       │    └── tmux
       └── styles
```

No circular dependencies. Each `internal/` package has a single responsibility. The `tui` package is the integration layer that wires everything together.

## Data flow

### Startup

```
main.go
  → config.Load()            reads ~/.config/gao/config.yaml
  → github.NewClient()       stateless, wraps gh CLI
  → tmux.NewClient()         stateless, wraps tmux CLI
  → claude.NewManager()      loads sessions.yaml, holds session state
  → tui.NewModel()           wires everything into the Bubble Tea model
  → tea.NewProgram().Run()   enters the TUI event loop
```

### Event loop

The Bubble Tea model (`tui/model.go`) follows the standard `Init → Update → View` cycle:

- **Init**: Fires three commands in parallel — `fetchIssues`, `refreshStatuses`, `tickCmd`
- **Update**: Dispatches on message type (key press, window resize, data loaded, tick)
- **View**: Pure rendering based on model state

Key message types:

| Message | Trigger | Effect |
|---------|---------|--------|
| `issuesLoadedMsg` | `fetchIssues` completes | Populates issue list, fires `fetchPRs` |
| `prsLoadedMsg` | `fetchPRs` completes | Populates PR cache (repo:branch → PR) |
| `statusRefreshMsg` | Manual refresh or after attach | Calls `Manager.RefreshStatuses()` |
| `sessionSpawnedMsg` | `SpawnSession` completes | Adds session, switches to Sessions panel |
| `tickMsg` | Every 10 seconds | Refreshes statuses and PR cache |

### Spawn flow

When the user presses `s` on an issue:

1. `model.spawnSession()` validates selection, checks for duplicate sessions
2. `claude.Manager.SpawnSession()`:
   - Generates session name: `gao-{repoName}-{issueNumber}`
   - Generates branch name: `claude/issue-{issueNumber}`
   - Creates a detached tmux session (`tmux new-session -d`)
   - Sends the spawn command via `tmux send-keys` (worktree setup + claude command)
   - Persists session to `sessions.yaml`
3. UI switches to Sessions panel

### Session status detection

`Manager.RefreshStatuses()` runs every 10 seconds:

```
For each tracked session:
  1. tmux session exists?
     NO  → StatusStopped
     YES → Is "claude" process running? (pgrep -P {panePid} -f claude)
           NO  → StatusDone
           YES → Capture last 5 lines of pane output
                 Contains waiting indicators? → StatusWaiting
                 Otherwise                    → StatusRunning
```

Waiting indicators (checked against the last line, case-insensitive):
- `"waiting for your"`, `"what would you like"`, `"claude >"`, `"> "`, `"? "`
- Line ends with `?`

**Known limitation**: This is heuristic-based. See [#1](https://github.com/eulercb/github-agent-orchestrator/issues/1) for planned improvements.

### PR matching

PRs are linked to sessions by branch name:

```
For each session with a branch:
  gh pr list --repo {repo} --head {branch} --limit 1
```

The result is cached in `model.prCache` (map from branch name to `*PullRequest`). The cache is refreshed every 10 seconds alongside the tick.

### Attach flow

When the user presses `a` on a session:

- **Warp available**: Runs `warp-cli open-tab -- tmux attach-session -t {session}` (non-blocking, TUI stays running)
- **No Warp**: Uses `tea.ExecProcess` to suspend the TUI, runs `tmux attach-session`, resumes TUI on detach

## State persistence

| File | Format | Managed by |
|------|--------|------------|
| `~/.config/gao/config.yaml` | YAML | User (hand-edited) |
| `~/.config/gao/sessions.yaml` | YAML | `claude.Manager` (read/write on spawn, remove, load) |

The `sessions.yaml` file is the source of truth for tracked sessions. It stores:
- Session ID, issue number/title, repo, branch, tmux session name
- Worktree path, creation time, last known status, last activity snippet

## External tool interfaces

All external tools are called via `exec.Command`. There are no Go libraries wrapping these — the CLIs are the contract.

| Tool | Interface | Error handling |
|------|-----------|----------------|
| `gh` | JSON output (`--json` flag) parsed into Go structs | Stderr from exit errors surfaced to user |
| `tmux` | Format strings (`-F`) for structured output | "no server running" / "no sessions" treated as empty, not error |
| `pgrep` | Exit code only (0 = found, non-zero = not found) | Silent failure returns false |
| `warp-cli` | `exec.LookPath` for detection, `open-tab` subcommand | Falls back to tmux attach |
| `open`/`xdg-open` | URL as argument | Tries `open` first (macOS), then `xdg-open` (Linux) |

## Design decisions

**Why shell out to `gh` instead of using the GitHub API directly?**
The user already has `gh` authenticated. No need to manage OAuth tokens or PATs. The JSON output mode makes parsing straightforward.

**Why tmux instead of managing processes directly?**
tmux provides session persistence, detach/attach, and pane capture for free. It's the standard tool for this use case and the user's existing workflow already relies on it.

**Why a flat keymap instead of modes/menus?**
The user explicitly wanted minimal keyboard shortcuts to avoid conflicts with tmux. A single-layer keymap with obvious keys (`s` spawn, `a` attach, `o` open) is faster than nested menus.

**Why YAML for config?**
It's human-friendly, supports comments, and is the de facto standard for CLI tool configs in this ecosystem (gh, docker-compose, etc.).

## Adding a new feature — checklist

1. If it touches config: add fields to `config.Config` struct with `yaml` tags and update `DefaultConfig()`
2. If it adds a key: add to `KeyMap` in `tui/keymap.go`, handle in `model.handleKey()`, add to help bar in `view.go`
3. If it adds an async operation: define a message type, create a `tea.Cmd` producer, handle the message in `Update()`
4. If it wraps an external tool: create a new package under `internal/`, keep it stateless if possible
5. Add tests for any non-trivial logic (helpers, parsing, state transitions)
6. Run `make all` (lint + test + build) before pushing
