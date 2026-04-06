# Configuration

gao is configured via a YAML file at `~/.config/gao/config.yaml`. Run `gao init` to generate a starter config.

## Full reference

```yaml
# gao - GitHub Agent Orchestrator configuration
# https://github.com/eulercb/github-agent-orchestrator

# Repositories to track. The TUI currently loads issues from the first entry.
repos:
  - owner: eulercb
    name: my-project
    # Override the local clone path for this specific repo.
    # If unset, falls back to repos_dir/<owner>/<name>, then ~/<name>.
    local_path: "/home/you/work/my-project"
    filters:
      assignee: "@me"          # GitHub username or "@me"
      labels:                  # Only show issues with ALL of these labels
        - bug
        - priority/high
      state: open              # "open", "closed", or "all"

  - owner: eulercb
    name: another-repo
    filters:
      assignee: "@me"
      state: open

# Root directory where repos are cloned. Used as <repos_dir>/<owner>/<name>
# when a repo doesn't have an explicit local_path.
# Default: "" (falls back to ~/<name>)
repos_dir: ""

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

# How to attach to a running session.
attach:
  # Force Warp tab behavior on/off. When unset (null), gao auto-detects
  # Warp by checking if warp-cli is on PATH.
  # Default: null (auto-detect)
  use_warp: null

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

If you just want to get started with a single repo:

```yaml
repos:
  - owner: your-username
    name: your-repo
    filters:
      assignee: "@me"
      state: open
```

Everything else uses sensible defaults.

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
