package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines all keyboard shortcuts for the TUI.
type KeyMap struct {
	Up              key.Binding
	Down            key.Binding
	Back            key.Binding
	Quit            key.Binding
	Tab             key.Binding
	Spawn           key.Binding
	Attach          key.Binding
	ImportWorktrees key.Binding
	Open            key.Binding
	OpenIssue       key.Binding
	Delete          key.Binding
	Refresh         key.Binding
	Help            key.Binding
	Filter          key.Binding
	ToggleIssues    key.Binding
	ToggleDebug     key.Binding
}

// DefaultKeyMap returns the default key bindings.
// Kept minimal and single-layer to avoid conflicts with terminal shortcuts.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		Back: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "back"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "switch panel"),
		),
		Spawn: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "spawn agent"),
		),
		Attach: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "attach session"),
		),
		ImportWorktrees: key.NewBinding(
			key.WithKeys("w"),
			key.WithHelp("w", "scan worktrees"),
		),
		Open: key.NewBinding(
			key.WithKeys("o"),
			key.WithHelp("o", "open in browser"),
		),
		OpenIssue: key.NewBinding(
			key.WithKeys("O"),
			key.WithHelp("O", "open issue in browser"),
		),
		Delete: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "kill session"),
		),
		Refresh: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "refresh"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter"),
		),
		ToggleIssues: key.NewBinding(
			key.WithKeys("i"),
			key.WithHelp("i", "toggle issues"),
		),
		ToggleDebug: key.NewBinding(
			key.WithKeys("d"),
			key.WithHelp("d", "toggle debug"),
		),
	}
}
