# gao — GitHub Agent Orchestrator

[![Latest Release](https://img.shields.io/github/v/release/eulercb/github-agent-orchestrator?label=download&sort=semver)](https://github.com/eulercb/github-agent-orchestrator/releases/latest)
[![CI](https://github.com/eulercb/github-agent-orchestrator/actions/workflows/ci.yml/badge.svg)](https://github.com/eulercb/github-agent-orchestrator/actions/workflows/ci.yml)

A terminal dashboard for spawning and managing [Claude Code](https://docs.anthropic.com/en/docs/claude-code) sessions alongside GitHub issues and pull requests.

```
┌──────────────────────────────────────────────────────────────────────┐
│  gao                                            eulercb/my-project  │
├──────────────────────────────────────────────────────────────────────┤
│  Issues                                                              │
│  ● #42  Fix login bug                              [bug]             │
│    #43  Add dark mode                               [feature]        │
│    #44  Refactor auth module                        [refactor]       │
├──────────────────────────────────────────────────────────────────────┤
│  Sessions                                                            │
│  #42  claude/issue-42   ⚡ working   PR #50 draft                    │
│  #43  claude/issue-43   ✓  done      PR #51 ✓ approved               │
│  #44  claude/issue-44   ⏳ waiting   —                               │
├──────────────────────────────────────────────────────────────────────┤
│  Sessions: 3  ⚡ 1 working  ⏳ 1 waiting  ✓ 1 done                  │
│  ↑↓ navigate  tab switch  s spawn  a attach  o open  ? help  q quit │
└──────────────────────────────────────────────────────────────────────┘
```

## The workflow

1. **See your issues** — gao fetches open issues from your configured repos (filtered by assignee, labels, etc.)
2. **Spawn an agent** — Press `s` on an issue to create a Claude Code session in a tmux + git worktree
3. **Monitor progress** — The dashboard auto-refreshes session statuses (working / waiting / done) and tracks PRs by branch
4. **Jump into a session** — Press `a` to attach (opens a Warp tab if available, or suspends the TUI and runs `tmux attach`)
5. **Review the result** — Press `o` to open the linked PR in your browser

## Requirements

| Tool | Required | Purpose |
|------|----------|---------|
| [Go](https://go.dev/) 1.24+ | Build only | Compiling gao |
| [gh](https://cli.github.com/) | Yes | GitHub API (issues, PRs) |
| [tmux](https://github.com/tmux/tmux) | Yes | Background sessions |
| [Claude Code](https://docs.anthropic.com/en/docs/claude-code) | Yes | The AI agent |
| [Warp](https://www.warp.dev/) | No | Optional: opens sessions in new Warp tabs |
| [ccusage](https://github.com/ryoppippi/ccusage) | No | Optional: token usage tracking in the status bar |

## Install

### Download a release binary

Pre-built binaries for Linux and macOS (amd64/arm64) are available on the [releases page](https://github.com/eulercb/github-agent-orchestrator/releases/latest).

```bash
# Example: download the latest release for your platform
version="$(curl -fsSL -o /dev/null -w '%{url_effective}' https://github.com/eulercb/github-agent-orchestrator/releases/latest | sed 's#.*/tag/v##')"
arch="$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/')"
curl -fLo gao.tar.gz "https://github.com/eulercb/github-agent-orchestrator/releases/download/v${version}/gao_${version}_$(uname -s | tr '[:upper:]' '[:lower:]')_${arch}.tar.gz"
tar xzf gao.tar.gz
sudo mv gao /usr/local/bin/
```

### Install with Go

```bash
go install github.com/eulercb/github-agent-orchestrator/cmd/gao@latest
```

### Build from source

```bash
git clone https://github.com/eulercb/github-agent-orchestrator.git
cd github-agent-orchestrator
make build          # binary at ./build/gao
make install        # installs to $GOPATH/bin
```

## Quick start

```bash
# 1. Create a default config
gao init

# 2. Edit it with your repo
$EDITOR ~/.config/gao/config.yaml

# 3. Launch the dashboard
gao
```

## Keyboard shortcuts

| Key | Action |
|-----|--------|
| `↑`/`k` `↓`/`j` | Navigate |
| `Tab` | Switch between Issues and Sessions panels |
| `s` | Spawn a Claude Code session for the selected issue |
| `a` | Attach to the selected session |
| `o` | Open issue or PR in the browser |
| `x` | Kill the selected session |
| `r` | Refresh issues and statuses |
| `?` | Toggle help |
| `q` | Quit |

Shortcuts were kept minimal to avoid conflicts with tmux key bindings.

## Documentation

| Document | Audience |
|----------|----------|
| [Configuration](docs/configuration.md) | Users setting up gao |
| [Architecture](docs/architecture.md) | Contributors and LLM agents working on the codebase |
| [Development](docs/development.md) | Building, testing, and contributing |

## License

See [LICENSE](LICENSE).
