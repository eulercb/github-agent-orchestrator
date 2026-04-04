// Package styles defines the visual theme for the TUI.
package styles

import "github.com/charmbracelet/lipgloss"

var (
	// Primary color used for active selections and branding.
	Primary = lipgloss.Color("#7C3AED")
	// Secondary color used for section titles and accents.
	Secondary = lipgloss.Color("#06B6D4")
	// Success color for positive states.
	Success = lipgloss.Color("#10B981")
	// Warning color for in-progress states.
	Warning = lipgloss.Color("#F59E0B")
	// Danger color for errors and stopped states.
	Danger = lipgloss.Color("#EF4444")
	// Muted color for less prominent text.
	Muted = lipgloss.Color("#6B7280")
	// Text is the default foreground color.
	Text = lipgloss.Color("#E5E7EB")
	// BgDark is used for status bar backgrounds.
	BgDark = lipgloss.Color("#1F2937")

	// TitleBar styles the top bar of the dashboard.
	TitleBar = lipgloss.NewStyle().
			Background(Primary).
			Foreground(lipgloss.Color("#FFFFFF")).
			Padding(0, 1).
			Bold(true)

	// StatusBar styles the bottom status line.
	StatusBar = lipgloss.NewStyle().
			Background(BgDark).
			Foreground(Text).
			Padding(0, 1)

	// HelpBar styles the keyboard shortcut hints.
	HelpBar = lipgloss.NewStyle().
		Foreground(Muted).
		Padding(0, 1)

	// SectionTitle styles panel headers.
	SectionTitle = lipgloss.NewStyle().
			Foreground(Secondary).
			Bold(true).
			Padding(0, 1)

	// SelectedItem styles the currently highlighted row.
	SelectedItem = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(Primary).
			Padding(0, 1)

	// NormalItem styles unselected rows.
	NormalItem = lipgloss.NewStyle().
			Foreground(Text).
			Padding(0, 1)

	// MutedText styles secondary information.
	MutedText = lipgloss.NewStyle().
			Foreground(Muted)

	// StatusWorking styles the working status indicator.
	StatusWorking = lipgloss.NewStyle().Foreground(Warning).Bold(true)
	// StatusWaiting styles the waiting status indicator.
	StatusWaiting = lipgloss.NewStyle().Foreground(Secondary).Bold(true)
	// StatusDone styles the done status indicator.
	StatusDone = lipgloss.NewStyle().Foreground(Success).Bold(true)
	// StatusStopped styles the stopped status indicator.
	StatusStopped = lipgloss.NewStyle().Foreground(Danger).Bold(true)

	// PRDraft styles draft PR labels.
	PRDraft = lipgloss.NewStyle().Foreground(Muted)
	// PROpen styles open PR labels.
	PROpen = lipgloss.NewStyle().Foreground(Success)
	// PRMerged styles merged PR labels.
	PRMerged = lipgloss.NewStyle().Foreground(Primary)
	// PRApproved styles approved PR labels.
	PRApproved = lipgloss.NewStyle().Foreground(Success).Bold(true)

	// LabelStyle styles issue labels.
	LabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(Primary).
			Padding(0, 1)

	// BorderedBox styles bordered containers for help and confirm views.
	BorderedBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Muted).
			Padding(0, 1)
)
