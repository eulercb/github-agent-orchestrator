// Package styles defines the visual theme for the TUI.
package styles

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	Primary   = lipgloss.Color("#7C3AED")
	Secondary = lipgloss.Color("#06B6D4")
	Success   = lipgloss.Color("#10B981")
	Warning   = lipgloss.Color("#F59E0B")
	Danger    = lipgloss.Color("#EF4444")
	Muted     = lipgloss.Color("#6B7280")
	Text      = lipgloss.Color("#E5E7EB")
	BgDark    = lipgloss.Color("#1F2937")

	// Component styles
	TitleBar = lipgloss.NewStyle().
			Background(Primary).
			Foreground(lipgloss.Color("#FFFFFF")).
			Padding(0, 1).
			Bold(true)

	StatusBar = lipgloss.NewStyle().
			Background(BgDark).
			Foreground(Text).
			Padding(0, 1)

	HelpBar = lipgloss.NewStyle().
		Foreground(Muted).
		Padding(0, 1)

	SectionTitle = lipgloss.NewStyle().
			Foreground(Secondary).
			Bold(true).
			Padding(0, 1)

	SelectedItem = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(Primary).
			Padding(0, 1)

	NormalItem = lipgloss.NewStyle().
			Foreground(Text).
			Padding(0, 1)

	MutedText = lipgloss.NewStyle().
			Foreground(Muted)

	// Status indicators
	StatusWorking = lipgloss.NewStyle().Foreground(Warning).Bold(true)
	StatusWaiting = lipgloss.NewStyle().Foreground(Secondary).Bold(true)
	StatusDone    = lipgloss.NewStyle().Foreground(Success).Bold(true)
	StatusStopped = lipgloss.NewStyle().Foreground(Danger).Bold(true)

	// PR status
	PRDraft    = lipgloss.NewStyle().Foreground(Muted)
	PROpen     = lipgloss.NewStyle().Foreground(Success)
	PRMerged   = lipgloss.NewStyle().Foreground(Primary)
	PRApproved = lipgloss.NewStyle().Foreground(Success).Bold(true)

	// Labels
	LabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(Primary).
			Padding(0, 1)

	// Borders
	BorderedBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Muted).
			Padding(0, 1)
)
