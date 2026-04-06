# Configuration

gao is configured via a YAML file at `~/.config/gao/config.yaml`. Run `gao init` to generate a starter config.

## Full reference

```yaml
# gao - GitHub Agent Orchestrator configuration
# https://github.com/eulercb/github-agent-orchestrator

# Root directory containing your git repos. gao auto-discovers all
# git repositories (immediate subdirectories) and extracts their
# GitHub owner/name from the origin remote URL.
# Required.
repos_dir: "~/code"

# Whether to show the Issues panel. When false, the dashboard shows
# only the Sessions panel. Toggle at runtime with the 'i' key.
# Default: true
track_issues: true

# GitHub search syntax filter for the Issues panel.
# This is global — issue results come from GitHub search, not scoped
# to a single repo. Use repo: qualifiers to narrow scope.
# Default: "is:open assignee:@me"
issue_filter: "is:open assignee:@me"

# How Claude Code sessions are spawned.
spawn:
  # Shell command to run in the worktree directory.
  # This is where you control Claude Code's permission mode.
  # Default: "claude --dangerously-skip-permissions"
  command: "claude --dangerously-skip-permissions"

  # Use git worktrees (true) or regular branch checkout (false).
  # Worktrees let multiple sessions work on the same repo simultaneously.
  # Default: true
  use_worktree: true

  # Base branch for creating worktrees (e.g., "main", "master", "develop").
  # If empty, gao derives it from origin/HEAD automatically.
  # Default: "" (auto-detect)
  base_branch: ""

# How "open worktree" (a key) navigates to a session's worktree directory.
worktree:
  # Custom shell command to open a terminal at a path.
  # Use {path} as a placeholder for the worktree directory.
  # When empty, gao auto-detects:
  #   tmux ($TMUX set)          → tmux new-window -c {path}
  #   Warp ($TERM_PROGRAM)      → open -a Warp {path}
  #   fallback                  → login shell in the worktree path (suspends TUI)
  # Examples:
  #   "kitty --directory {path}"
  #   "alacritty --working-directory {path}"
  #   "gnome-terminal --working-directory={path}"
  # Default: ""
  open_command: ""

# Bottom status bar. Can be populated by a custom script.
status_bar:
  # Shell command whose stdout becomes the status bar text.
  # Runs every 10 seconds. If empty, the built-in default is used
  # (session counts by status).
  # Default: ""
  command: ""

# Optional ccusage integration for token usage tracking.
ccusage:
  # Enable ccusage in the status bar.
  # Default: false
  enabled: false

  # Command to run. Must be on PATH or a full path.
  # Default: "ccusage"
  command: "ccusage"

# Override the directory for session state persistence.
# Default: "" (uses ~/.config/gao/sessions.yaml)
session_dir: ""
```

## Minimal config

Just point gao at your repos directory:

```yaml
repos_dir: ~/code
```

Everything else uses sensible defaults. gao will auto-discover all git repos under `~/code` that have a GitHub origin remote.

## How repo discovery works

On startup (and on every worktree scan), gao reads all immediate subdirectories of `repos_dir` and checks each for a `.git` directory. For each git repo found, it parses the `origin` remote URL to extract the GitHub owner and name. Both SSH (`git@github.com:owner/name.git`) and HTTPS (`https://github.com/owner/name`) formats are supported.

Non-git directories and repos without a GitHub remote are silently skipped.

## File locations

| File | Purpose |
|------|---------|
| `~/.config/gao/config.yaml` | User configuration |
| `~/.config/gao/sessions.yaml` | Persisted session state (managed by gao, not hand-edited) |
| `~/.config/gao/logs/*.log` | Session output logs (stdout/stderr from Claude processes) |

## Custom spawn scripts

The `spawn.command` field accepts any shell command. For example, to use a custom script that sets up environment variables and runs Claude in a specific mode:

```yaml
spawn:
  command: "/home/you/bin/run-claude.sh"
```

Your script receives no arguments — it runs inside the worktree directory with the repo already checked out on the correct branch. A simple example:

```bash
#!/usr/bin/env bash
# run-claude.sh — custom Claude Code launcher
export ANTHROPIC_MODEL="claude-sonnet-4-20250514"
exec claude --dangerously-skip-permissions
```

## Custom status bar

The `status_bar.command` is a shell command whose stdout replaces the built-in status text. It runs every 10 seconds. Example using ccusage:

```yaml
status_bar:
  command: "ccusage --format oneline 2>/dev/null || echo 'ccusage unavailable'"
```

If the command fails or returns empty, gao falls back to the built-in status (session counts).
