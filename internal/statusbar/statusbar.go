// Package statusbar provides a customizable status bar that can be populated
// by an external script, similar to Claude Code's status line approach.
package statusbar

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Provider fetches status bar text. It can be backed by a shell command,
// a built-in function, or a combination.
type Provider struct {
	command  string
	fallback func() string
	mu       sync.RWMutex
	current  string
}

// NewProvider creates a status bar provider.
// If command is empty, only the fallback function is used.
func NewProvider(command string, fallback func() string) *Provider {
	return &Provider{
		command:  command,
		fallback: fallback,
	}
}

// Text returns the current status bar text.
func (p *Provider) Text() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.current
}

// Refresh updates the status bar text.
func (p *Provider) Refresh() {
	var text string

	if p.command != "" {
		text = p.runCommand()
	}

	if text == "" && p.fallback != nil {
		text = p.fallback()
	}

	p.mu.Lock()
	p.current = text
	p.mu.Unlock()
}

// StartAutoRefresh refreshes the status bar periodically.
func (p *Provider) StartAutoRefresh(ctx context.Context, interval time.Duration) {
	p.Refresh()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.Refresh()
			}
		}
	}()
}

func (p *Provider) runCommand() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", p.command)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
