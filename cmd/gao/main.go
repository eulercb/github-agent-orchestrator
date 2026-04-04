// gao (GitHub Agent Orchestrator) is a CLI for managing Claude Code agent
// sessions alongside GitHub issues and pull requests.
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/eulercb/github-agent-orchestrator/internal/claude"
	"github.com/eulercb/github-agent-orchestrator/internal/config"
	"github.com/eulercb/github-agent-orchestrator/internal/github"
	"github.com/eulercb/github-agent-orchestrator/internal/tmux"
	"github.com/eulercb/github-agent-orchestrator/internal/tui"
)

const version = "0.1.0"

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
		fmt.Println("No repos configured. Run 'gao init' to set up your config.")
		fmt.Printf("Config file: ")
		p, _ := config.Path()
		fmt.Println(p)
		return nil
	}

	ghClient := github.NewClient()
	tmuxClient := tmux.NewClient()

	sessMgr, err := claude.NewManager(cfg, tmuxClient)
	if err != nil {
		return fmt.Errorf("init session manager: %w", err)
	}

	model := tui.NewModel(cfg, ghClient, sessMgr)
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
	}

	cfg := config.DefaultConfig()
	cfg.Repos = []config.RepoConfig{
		{
			Owner: "your-username",
			Name:  "your-repo",
			Filters: config.IssueFilters{
				Assignee: "@me",
				State:    "open",
			},
		},
	}

	if err := config.Save(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "gao: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Config created at %s\n", cfgPath)
	fmt.Println("Edit it with your repo details, then run 'gao' to start.")
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
