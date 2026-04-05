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
		cfgPath, pathErr := config.Path()
		if pathErr != nil {
			return fmt.Errorf("config path: %w", pathErr)
		}

		_, statErr := os.Stat(cfgPath)
		if statErr != nil && !os.IsNotExist(statErr) {
			return fmt.Errorf("stat config %s: %w", cfgPath, statErr)
		}

		if os.IsNotExist(statErr) {
			// Non-interactive context (piped stdin, CI): don't block on prompts.
			if fi, fiErr := os.Stdin.Stat(); fiErr == nil && fi.Mode()&os.ModeCharDevice == 0 {
				fmt.Fprintf(os.Stderr, "No config found. Run 'gao init' interactively to create one.\n")
				return nil
			}

			// No config file found — run init automatically.
			fmt.Println("No config found. Let's set one up!")
			fmt.Println()

			if initErr := doInit(); initErr != nil {
				return fmt.Errorf("init: %w", initErr)
			}

			// Reload config after init.
			cfg, err = config.Load()
			if err != nil {
				return fmt.Errorf("load config after init: %w", err)
			}

			fmt.Printf("\nConfig created at %s\n", cfgPath)
		}

		if len(cfg.Repos) == 0 {
			fmt.Printf("No repos configured. Add a repo to %s:\n\n", cfgPath)
			fmt.Println("repos:")
			fmt.Println("  - owner: your-github-username")
			fmt.Println("    name: your-repo-name")
			fmt.Println("    filters:")
			fmt.Println("      assignee: '@me'")
			fmt.Println("      state: open")
			fmt.Println()
			return nil
		}

		fmt.Println("Initialization complete! Starting dashboard...")
		fmt.Println()
	}

	ghClient := github.NewClient()

	sessMgr, err := claude.NewManager(&cfg)
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

	if err := doInit(); err != nil {
		fmt.Fprintf(os.Stderr, "gao: init: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nConfig created at %s\n", cfgPath)

	cfg, loadErr := config.Load()
	if loadErr != nil {
		fmt.Fprintf(os.Stderr, "gao: failed to read config after init: %v\n", loadErr)
		os.Exit(1)
	}

	if len(cfg.Repos) > 0 {
		fmt.Println("Run 'gao' to start the dashboard.")
	} else {
		fmt.Println("Add a repo to the config, then run 'gao' to start.")
	}
}

// doInit runs the interactive config setup and saves the result.
func doInit() error {
	scanner := bufio.NewScanner(os.Stdin)
	cfg := config.DefaultConfig()

	// Try to detect the current repo from git remote.
	detectedOwner, detectedName := detectGitRepo()

	owner := prompt(scanner, "Repository owner", detectedOwner)
	name := prompt(scanner, "Repository name", detectedName)

	if owner != "" && name != "" {
		issueOwner := prompt(scanner, "Issue source repo owner (blank to use same repo)", "")
		issueName := prompt(scanner, "Issue source repo name (blank to use same repo)", "")

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

		// Default blank issue source fields to the main repo values.
		resolvedIssueOwner := issueOwner
		if resolvedIssueOwner == "" {
			resolvedIssueOwner = owner
		}
		resolvedIssueName := issueName
		if resolvedIssueName == "" {
			resolvedIssueName = name
		}

		if resolvedIssueOwner != owner || resolvedIssueName != name {
			repo.IssueSource = &config.IssueSource{
				Owner: resolvedIssueOwner,
				Name:  resolvedIssueName,
			}
		}

		cfg.Repos = []config.RepoConfig{repo}
	}

	if err := config.Save(&cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	return nil
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
