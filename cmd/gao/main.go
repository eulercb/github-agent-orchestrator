// gao (GitHub Agent Orchestrator) is a CLI for managing Claude Code agent
// sessions alongside GitHub issues and pull requests.
package main

import (
	"bufio"
	"fmt"
	"os"
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
			if cfgPath, err := config.Path(); err == nil {
				fmt.Printf("config: %s\n", cfgPath)
			}
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

	if cfg.ReposDir == "" {
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

		if cfg.ReposDir == "" {
			fmt.Printf("No repos_dir configured. Add it to %s:\n\n", cfgPath)
			fmt.Println("repos_dir: ~/code")
			fmt.Println("track_issues: true")
			fmt.Println("issue_filter: 'is:open assignee:@me'")
			fmt.Println()
			return nil
		}

		fmt.Println("Initialization complete! Starting dashboard...")
		fmt.Println()
	}

	ghClient := github.NewClient()

	sessMgr, err := claude.NewManager(&cfg, ghClient)
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
	fmt.Println("Run 'gao' to start the dashboard.")
}

// doInit runs the interactive config setup and saves the result.
func doInit() error {
	scanner := bufio.NewScanner(os.Stdin)
	cfg := config.DefaultConfig()

	// Default repos_dir to the current working directory.
	var detectedReposDir string
	if cwd, err := os.Getwd(); err == nil {
		detectedReposDir = cwd
	}

	cfg.ReposDir = prompt(scanner, "Root directory for repos (git repos will be auto-discovered)", detectedReposDir)
	cfg.TrackIssues = promptYesNo(scanner, "Track GitHub issues?", true)
	if cfg.TrackIssues {
		cfg.IssueFilter = prompt(scanner, "Issue filter (GitHub search syntax)", config.DefaultIssueFilter)
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

// promptYesNo asks the user a yes/no question with a default value.
// Re-prompts on invalid input until a valid answer is given.
func promptYesNo(scanner *bufio.Scanner, label string, defaultVal bool) bool {
	defStr := "Y/n"
	if !defaultVal {
		defStr = "y/N"
	}
	for {
		fmt.Printf("%s [%s]: ", label, defStr)
		if !scanner.Scan() {
			return defaultVal
		}
		input := strings.TrimSpace(strings.ToLower(scanner.Text()))
		switch input {
		case "":
			return defaultVal
		case "y", "yes":
			return true
		case "n", "no":
			return false
		default:
			fmt.Println("Please enter yes or no.")
		}
	}
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
  /                Edit issue filter (GitHub search syntax)
  s                Spawn Claude Code session for selected issue
  a                Attach to selected session
  o                Open issue/PR in browser
  x                Kill selected session
  i                Toggle issues panel
  r                Refresh
  ?                Help
  q                Quit
`, version)
}
