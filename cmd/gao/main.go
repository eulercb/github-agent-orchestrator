// gao (GitHub Agent Orchestrator) is a CLI for managing Claude Code agent
// sessions alongside GitHub issues and pull requests.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eulercb/github-agent-orchestrator/internal/claude"
	"github.com/eulercb/github-agent-orchestrator/internal/config"
	"github.com/eulercb/github-agent-orchestrator/internal/github"
	"github.com/eulercb/github-agent-orchestrator/internal/tmux"
	"github.com/eulercb/github-agent-orchestrator/internal/tui"
)

// version is set at build time via -ldflags "-X main.version=...".
// Falls back to "dev" for local builds without ldflags.
var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version", "-v":
			fmt.Printf("gao v%s\n", version)
			return
		case "init":
			runInit()
			return
		case "help", "--help", "-h":
			printUsage()
			return
		}
	}

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "gao: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if len(cfg.Repos) == 0 {
		p, pathErr := config.Path()
		if pathErr != nil {
			fmt.Fprintf(os.Stderr, "No repos configured and config path unavailable: %v\n", pathErr)
		} else {
			fmt.Printf("No repos configured. Add a repo to %s:\n\n", p)
			fmt.Println("repos:")
			fmt.Println("  - owner: your-github-username")
			fmt.Println("    name: your-repo-name")
			fmt.Println("    filters:")
			fmt.Println("      assignee: '@me'")
			fmt.Println("      state: open")
			fmt.Println()
		}
		return nil
	}

	ghClient := github.NewClient()
	tmuxClient := tmux.NewClient()

	sessMgr, err := claude.NewManager(&cfg, tmuxClient)
	if err != nil {
		return fmt.Errorf("init session manager: %w", err)
	}

	model := tui.NewModel(&cfg, ghClient, sessMgr)
	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	return nil
}

func runInit() {
	cfgPath, err := config.Path()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gao: %v\n", err)
		os.Exit(1)
	}

	if _, err := os.Stat(cfgPath); err == nil {
		fmt.Printf("Config already exists at %s\n", cfgPath)
		return
	} else if !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "gao: stat config %s: %v\n", cfgPath, err)
		os.Exit(1)
	}

	scanner := bufio.NewScanner(os.Stdin)
	cfg := config.DefaultConfig()

	// Try to detect the current repo from git remote.
	detectedOwner, detectedName := detectGitRepo()

	owner := prompt(scanner, "Repository owner", detectedOwner)
	name := prompt(scanner, "Repository name", detectedName)

	if owner != "" && name != "" {
		assignee := prompt(scanner, "Issue assignee filter (blank for all, @me for yourself)", "@me")
		state := prompt(scanner, "Issue state filter (open, closed, all)", "open")

		repo := config.RepoConfig{
			Owner: owner,
			Name:  name,
			Filters: config.IssueFilters{
				Assignee: assignee,
				State:    state,
			},
		}
		cfg.Repos = []config.RepoConfig{repo}
	}

	if err := config.Save(&cfg); err != nil {
		fmt.Fprintf(os.Stderr, "gao: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nConfig created at %s\n", cfgPath)
	if len(cfg.Repos) > 0 {
		fmt.Println("Run 'gao' to start the dashboard.")
	} else {
		fmt.Println("Add a repo to the config, then run 'gao' to start.")
	}
}

// prompt asks the user for input with an optional default value.
func prompt(scanner *bufio.Scanner, label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("%s: ", label)
	}
	if scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input != "" {
			return input
		}
	}
	return defaultVal
}

// detectGitRepo tries to detect the GitHub owner/name from the current
// directory's git remote.
func detectGitRepo() (owner, name string) {
	cmd := exec.CommandContext(context.Background(), "gh", "repo", "view", "--json", "owner,name", "-q", ".owner.login + \"/\" + .name")
	out, err := cmd.Output()
	if err != nil {
		return "", ""
	}
	parts := strings.SplitN(strings.TrimSpace(string(out)), "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", ""
}

func printUsage() {
	fmt.Printf(`gao - GitHub Agent Orchestrator v%s

Usage:
  gao              Launch the dashboard
  gao init         Create a default config file
  gao version      Show version
  gao help         Show this help

Config: ~/.config/gao/config.yaml

Dashboard controls:
  ↑↓/jk           Navigate
  Tab              Switch panels (Issues ↔ Sessions)
  s                Spawn Claude Code session for selected issue
  a                Attach to selected session
  o                Open issue/PR in browser
  x                Kill selected session
  r                Refresh
  ?                Help
  q                Quit
`, version)
}
