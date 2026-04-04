// Package tmux manages tmux sessions for Claude Code agents.
package tmux

import (
	"fmt"
	"os/exec"
	"strings"
)

// Session represents a tmux session.
type Session struct {
	Name      string
	Created   string
	Attached  bool
	Windows   int
	PaneCount int
}

// Client wraps tmux CLI interactions.
type Client struct{}

// NewClient returns a new tmux client.
func NewClient() *Client {
	return &Client{}
}

// ListSessions returns all tmux sessions.
func (c *Client) ListSessions() ([]Session, error) {
	out, err := run("list-sessions", "-F",
		"#{session_name}\t#{session_created_string}\t#{session_attached}\t#{session_windows}")
	if err != nil {
		if strings.Contains(err.Error(), "no server running") ||
			strings.Contains(err.Error(), "no sessions") {
			return nil, nil
		}
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	var sessions []Session
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 4 {
			continue
		}
		sessions = append(sessions, Session{
			Name:     parts[0],
			Created:  parts[1],
			Attached: parts[2] == "1",
		})
	}
	return sessions, nil
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
		args = append(args, command)
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
	cmd := exec.Command("pgrep", "-P", pid, "-f", pattern)
	return cmd.Run() == nil
}

func run(args ...string) (string, error) {
	cmd := exec.Command("tmux", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tmux %s: %s (%w)", args[0], strings.TrimSpace(string(out)), err)
	}
	return string(out), nil
}
