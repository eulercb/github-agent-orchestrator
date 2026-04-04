// Package tmux manages tmux sessions for Claude Code agents.
package tmux

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Client wraps tmux CLI interactions.
type Client struct{}

// NewClient returns a new tmux client.
func NewClient() *Client {
	return &Client{}
}

// SessionExists checks if a tmux session exists.
func (c *Client) SessionExists(name string) bool {
	_, err := run("has-session", "-t", name)
	return err == nil
}

// NewSession creates a new tmux session in detached mode.
func (c *Client) NewSession(name, workDir, command string) error {
	args := []string{"new-session", "-d", "-s", name}
	if workDir != "" {
		args = append(args, "-c", workDir)
	}
	if command != "" {
		args = append(args, "--", command)
	}
	_, err := run(args...)
	if err != nil {
		return fmt.Errorf("create session %q: %w", name, err)
	}
	return nil
}

// SendKeys sends keystrokes to a tmux session.
func (c *Client) SendKeys(session, keys string) error {
	_, err := run("send-keys", "-t", session, keys, "Enter")
	if err != nil {
		return fmt.Errorf("send keys to %q: %w", session, err)
	}
	return nil
}

// CapturePaneOutput captures the visible content of a tmux pane.
func (c *Client) CapturePaneOutput(session string, lines int) (string, error) {
	startLine := fmt.Sprintf("-%d", lines)
	out, err := run("capture-pane", "-t", session, "-p", "-S", startLine)
	if err != nil {
		return "", fmt.Errorf("capture pane %q: %w", session, err)
	}
	return out, nil
}

// KillSession destroys a tmux session.
func (c *Client) KillSession(name string) error {
	_, err := run("kill-session", "-t", name)
	if err != nil {
		return fmt.Errorf("kill session %q: %w", name, err)
	}
	return nil
}

// IsProcessRunning checks if a process matching a pattern is running in the session.
func (c *Client) IsProcessRunning(session, pattern string) bool {
	// Get the pane PID and check child processes
	out, err := run("list-panes", "-t", session, "-F", "#{pane_pid}")
	if err != nil {
		return false
	}
	pid := strings.TrimSpace(out)
	if pid == "" {
		return false
	}

	// Check if any child process matches the pattern
	cmd := exec.CommandContext(context.Background(), "pgrep", "-P", pid, "-f", pattern)
	return cmd.Run() == nil
}

func run(args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %s (%w)", args[0], strings.TrimSpace(string(out)), err)
	}
	return string(out), nil
}
