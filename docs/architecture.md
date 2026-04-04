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
├── process/
│   └── process.go              Background process management (start, monitor, kill, log capture)
├── claude/
│   ├── session.go              Session lifecycle, state machine, spawn logic
│   └── session_test.go         Unit tests for status detection helpers
├── statusbar/
│   ├── statusbar.go            Pluggable status bar (shell command or fallback func)
│   └── statusbar_test.go       Status bar provider tests
└── tui/
    ├── model.go                Bubble Tea model, Init/Update, all tea.Cmd producers
    ├── view.go                 All View() rendering (dashboard, help, confirm)
    ├── keymap.go               Key bindings
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
       │    └── process
       └── styles
```

No circular dependencies. Each `internal/` package has a single responsibility. The `tui` package is the integration layer that wires everything together.

## Data flow

### Startup

```
main.go
  → config.Load()            reads ~/.config/gao/config.yaml
  → github.NewClient()       stateless, wraps gh CLI
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
   - Generates session name: `gao-{owner}-{repoName}-{issueNumber}`
   - Generates branch name: `claude/issue-{issueNumber}`
   - Sets up the git worktree (or branch checkout) synchronously
   - Starts the Claude process in the background via `process.StartBackground()`
   - Output is captured to a log file in `~/.config/gao/logs/`
   - Persists session (with PID and log path) to `sessions.yaml`
3. UI switches to Sessions panel

### Session status detection

`Manager.RefreshStatuses()` runs every 10 seconds:

```
For each tracked session:
  1. Is the PID still running? (signal 0 check)
     NO  → StatusDone (or StatusStopped if previously stopped)
     YES → Read last 5 lines of log file
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

The result is cached in `model.prCache` (map from `repo:branch` composite key to `*PullRequest`). The cache is refreshed every 10 seconds alongside the tick.

### Attach flow

When the user presses `a` on a session:

- **Warp available**: Runs `warp-cli open-tab -- sh -c 'cd <worktree> && <spawn_command>'` (non-blocking, TUI stays running)
- **No Warp**: Uses `tea.ExecProcess` to suspend the TUI, runs an interactive Claude session in the worktree directory, resumes TUI on exit

## State persistence

| File | Format | Managed by |
|------|--------|------------|
| `~/.config/gao/config.yaml` | YAML | User (hand-edited) |
| `~/.config/gao/sessions.yaml` | YAML | `claude.Manager` (read/write on spawn, remove, load) |
| `~/.config/gao/logs/<session>.log` | Text | `process.StartBackground` (stdout/stderr capture) |

The `sessions.yaml` file is the source of truth for tracked sessions. It stores:
- Session ID, issue number/title, repo, branch, PID
- Log file path, worktree path, creation time, last known status, last activity snippet

## External tool interfaces

All external tools are called via `exec.Command`. There are no Go libraries wrapping these — the CLIs are the contract.

| Tool | Interface | Error handling |
|------|-----------|----------------|
| `gh` | JSON output (`--json` flag) parsed into Go structs; `browse --url` for opening URLs in the browser | Stderr from exit errors surfaced to user |
| `git` | Direct exec for fetch, worktree, checkout operations | Combined output surfaced in error messages |
| `pgrep` | Not used (replaced by direct PID monitoring via signal 0) | N/A |
| `warp-cli` | `exec.LookPath` for detection, `open-tab` subcommand | Falls back to interactive attach via `tea.ExecProcess` |

## Design decisions

**Why shell out to `gh` instead of using the GitHub API directly?**
The user already has `gh` authenticated. No need to manage OAuth tokens or PATs. The JSON output mode makes parsing straightforward.

**Why direct process management instead of tmux?**
Spawning Claude Code as a background process with log file capture removes an external dependency, simplifies the architecture, and improves portability. Status detection reads from log files instead of tmux pane capture. The user can attach to a session by launching an interactive Claude instance in the worktree directory.

**Why a flat keymap instead of modes/menus?**
The user explicitly wanted minimal keyboard shortcuts. A single-layer keymap with obvious keys (`s` spawn, `a` attach, `o` open) is faster than nested menus.

**Why YAML for config?**
It's human-friendly, supports comments, and is the de facto standard for CLI tool configs in this ecosystem (gh, docker-compose, etc.).

## Adding a new feature — checklist

1. If it touches config: add fields to `config.Config` struct with `yaml` tags and update `DefaultConfig()`
2. If it adds a key: add to `KeyMap` in `tui/keymap.go`, handle in `model.handleKey()`, add to help bar in `view.go`
3. If it adds an async operation: define a message type, create a `tea.Cmd` producer, handle the message in `Update()`
4. If it wraps an external tool: create a new package under `internal/`, keep it stateless if possible
5. Add tests for any non-trivial logic (helpers, parsing, state transitions)
6. Run `make all` (lint + test + build) before pushing
