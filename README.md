# gao — GitHub Agent Orchestrator

[![Latest Release](https://img.shields.io/github/v/release/eulercb/github-agent-orchestrator?label=download&sort=semver)](https://github.com/eulercb/github-agent-orchestrator/releases/latest)
[![CI](https://github.com/eulercb/github-agent-orchestrator/actions/workflows/ci.yml/badge.svg)](https://github.com/eulercb/github-agent-orchestrator/actions/workflows/ci.yml)

A terminal dashboard for visualizing and navigating [Claude Code](https://docs.anthropic.com/en/docs/claude-code) worktrees alongside GitHub issues and pull requests.

```
+--------------------------------------------------------------------+
|  gao                                          eulercb/my-project   |
+--------------------------------------------------------------------+
|  Issues                                                            |
|  > #42  Fix login bug                                    [bug]     |
|    #43  Add dark mode                                    [feature] |
|    #44  Refactor auth module                             [refactor]|
+--------------------------------------------------------------------+
|  Sessions                                                          |
|  #42  PR #50 draft       Fix login bug                             |
|  #43  PR #51 approved    Add dark mode                             |
|  #44  --                 Refactor auth module                      |
+--------------------------------------------------------------------+
|  Sessions: 3                                                        |
|  j/k nav  tab switch  a worktree  o open  ? help  q quit           |
+--------------------------------------------------------------------+
```

## The workflow

1. **See your issues** — gao fetches open issues from your configured repos (filtered by assignee, labels, etc.)
2. **Scan worktrees** — Press `w` to discover Claude Code worktrees across your repos
3. **Navigate to a worktree** — Press `a` to open the worktree directory in a new terminal
4. **Check PR status** — The dashboard tracks PRs by branch, showing draft/open/approved/merged status
5. **Review the result** — Press `o` to open the linked PR or issue in your browser

## Requirements

| Tool | Required | Purpose |
|------|----------|---------|
| [Go](https://go.dev/) 1.24+ | Build only | Compiling gao |
| [gh](https://cli.github.com/) | Yes | GitHub API (issues, PRs) |
| [Warp](https://www.warp.dev/) | No | Optional: opens worktrees in new Warp tabs |
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

> **macOS note:** The release binaries are unsigned. macOS Gatekeeper may block the binary on first run. To allow it:
> ```bash
> xattr -d com.apple.quarantine /usr/local/bin/gao
> ```

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
| `a` | Open the selected session's worktree in a new terminal |
| `w` | Scan and import discovered worktrees |
| `o` | Open issue or PR in the browser |
| `O` | Open session's linked issue in the browser |
| `x` | Remove the selected session's worktree |
| `/` | Filter issues (GitHub search syntax) |
| `r` | Refresh issues and PRs |
| `?` | Toggle help |
| `q` | Quit |

Shortcuts were kept minimal for a clean single-layer keymap.

## Documentation

| Document | Audience |
|----------|----------|
| [Configuration](docs/configuration.md) | Users setting up gao |
| [Architecture](docs/architecture.md) | Contributors and LLM agents working on the codebase |
| [Development](docs/development.md) | Building, testing, and contributing |

## License

See [LICENSE](LICENSE).
