package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/eulercb/github-agent-orchestrator/internal/claude"
	"github.com/eulercb/github-agent-orchestrator/internal/config"
	"github.com/eulercb/github-agent-orchestrator/internal/github"
	"github.com/eulercb/github-agent-orchestrator/internal/tui/styles"
)

// View renders the TUI.
func (m Model) View() string { //nolint:gocritic // tea.Model interface requires value receiver
	if m.width == 0 {
		return "Loading..."
	}

	switch m.currentView {
	case ViewHelp:
		return m.viewHelp()
	case ViewConfirm:
		return m.viewConfirm()
	case ViewFilter:
		return m.viewFilter()
	default:
		return m.viewDashboard()
	}
}

func (m *Model) viewDashboard() string {
	var sections []string

	// Title bar
	title := m.renderTitleBar()
	sections = append(sections, title)

	// Error message
	if m.errorMsg != "" {
		errStyle := lipgloss.NewStyle().Foreground(styles.Danger).Padding(0, 1)
		sections = append(sections, errStyle.Render("Error: "+m.errorMsg))
	}

	// Calculate space for content
	contentHeight := m.height - 3 // title + status + help
	if m.errorMsg != "" {
		contentHeight--
	}

	issueHeight := contentHeight / 2
	sessionHeight := contentHeight - issueHeight

	// Issues panel
	issuesContent := m.renderIssuesPanel(issueHeight)
	sections = append(sections, issuesContent)

	// Sessions panel
	sessionsContent := m.renderSessionsPanel(sessionHeight)
	sections = append(sections, sessionsContent)

	// Status bar
	statusBar := m.renderStatusBar()
	sections = append(sections, statusBar)

	// Help bar
	helpBar := m.renderHelpBar()
	sections = append(sections, helpBar)

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m *Model) renderTitleBar() string {
	repoName := "(no repo configured)"
	if repo := m.currentRepo(); repo != nil {
		repoName = repo.FullName()
		issueRepo := repo.IssueRepoFullName()
		if issueRepo != repo.FullName() {
			repoName = fmt.Sprintf("%s (issues: %s)", repoName, issueRepo)
		}
	}

	left := styles.TitleBar.Render(" gao ")
	right := styles.TitleBar.Render(fmt.Sprintf(" %s ", repoName))
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	mid := styles.TitleBar.Render(strings.Repeat(" ", gap))
	return left + mid + right
}

func (m *Model) renderIssuesPanel(maxHeight int) string {
	panelActive := m.activePanel == PanelIssues

	titleStyle := styles.SectionTitle
	if panelActive {
		titleStyle = titleStyle.Foreground(styles.Primary)
	}

	header := titleStyle.Render("Issues")
	if m.loading {
		header += styles.MutedText.Render(" (loading...)")
	}
	if repo := m.currentRepo(); repo != nil {
		filterText := repo.Filters.BuildSearch()
		// Reserve space for "  / " prefix (4) + "..." suffix (3) + margin (1).
		maxFilterLen := m.width - lipgloss.Width(header) - 8
		if maxFilterLen < 0 {
			maxFilterLen = 0
		}
		filterRunes := []rune(filterText)
		if len(filterRunes) > maxFilterLen {
			if maxFilterLen > 0 {
				filterText = string(filterRunes[:maxFilterLen]) + "..."
			} else {
				filterText = ""
			}
		}
		if filterText != "" {
			header += styles.MutedText.Render("  / " + filterText)
		}
	}

	var lines []string
	lines = append(lines, header)

	if len(m.issues) == 0 {
		lines = append(lines, styles.MutedText.Render("  No issues found"))
	}

	visibleCount := maxHeight - 2
	if visibleCount < 1 {
		visibleCount = 1
	}

	// Scrolling window
	start := 0
	if m.issuesCursor >= visibleCount {
		start = m.issuesCursor - visibleCount + 1
	}
	end := start + visibleCount
	if end > len(m.issues) {
		end = len(m.issues)
	}

	for i := start; i < end; i++ {
		selected := panelActive && i == m.issuesCursor

		line := m.renderIssueLine(&m.issues[i], selected)
		lines = append(lines, line)
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *Model) renderIssueLine(issue *github.Issue, selected bool) string {
	// Check if there's an active session for this issue
	repo := m.currentRepo()
	hasSession := false
	if repo != nil {
		if s := m.sessions.FindByIssue(repo.IssueRepoFullName(), issue.Number); s != nil {
			hasSession = true
		}
	}

	indicator := "  "
	if hasSession {
		indicator = "● "
	}

	number := fmt.Sprintf("#%-5d", issue.Number)
	title := issue.Title
	maxTitleLen := m.width - 33
	if maxTitleLen < 0 {
		maxTitleLen = 0
	}
	titleRunes := []rune(title)
	if len(titleRunes) > maxTitleLen {
		title = string(titleRunes[:maxTitleLen]) + "..."
	}

	var labels []string
	for _, l := range issue.Labels {
		labels = append(labels, l.Name)
	}
	labelStr := ""
	if len(labels) > 0 {
		labelStr = styles.MutedText.Render(" [" + strings.Join(labels, ", ") + "]")
	}

	content := fmt.Sprintf("%s%s %s%s", indicator, number, title, labelStr)

	if selected {
		return styles.SelectedItem.Width(m.width).Render(content)
	}
	return styles.NormalItem.Width(m.width).Render(content)
}

func (m *Model) renderSessionsPanel(maxHeight int) string {
	panelActive := m.activePanel == PanelSessions

	titleStyle := styles.SectionTitle
	if panelActive {
		titleStyle = titleStyle.Foreground(styles.Primary)
	}

	header := titleStyle.Render("Sessions")
	var lines []string
	lines = append(lines, header)

	sessions := m.sessions.Sessions()

	if len(sessions) == 0 {
		lines = append(lines, styles.MutedText.Render("  No active sessions. Select an issue and press 's' to spawn."))
	}

	visibleCount := maxHeight - 2
	if visibleCount < 1 {
		visibleCount = 1
	}

	start := 0
	if m.sessionCursor >= visibleCount {
		start = m.sessionCursor - visibleCount + 1
	}
	end := start + visibleCount
	if end > len(sessions) {
		end = len(sessions)
	}

	for i := start; i < end; i++ {
		selected := panelActive && i == m.sessionCursor

		line := m.renderSessionLine(&sessions[i], selected)
		lines = append(lines, line)
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *Model) renderSessionLine(sess *claude.Session, selected bool) string {
	// Status icon and text
	var statusIcon, statusText string
	var statusStyle lipgloss.Style
	switch sess.Status {
	case claude.StatusRunning:
		statusIcon = "⚡"
		statusText = "working"
		statusStyle = styles.StatusWorking
	case claude.StatusWaiting:
		statusIcon = "⏳"
		statusText = "waiting"
		statusStyle = styles.StatusWaiting
	case claude.StatusDone:
		statusIcon = "✓"
		statusText = "done"
		statusStyle = styles.StatusDone
	case claude.StatusStopped:
		statusIcon = "✗"
		statusText = "stopped"
		statusStyle = styles.StatusStopped
	default:
		statusIcon = "?"
		statusText = "unknown"
		statusStyle = styles.MutedText
	}

	issueRef := fmt.Sprintf("#%-5d", sess.IssueNumber)
	branchShort := sess.Branch
	branchRunes := []rune(branchShort)
	if len(branchRunes) > 25 {
		branchShort = string(branchRunes[:22]) + "..."
	}

	statusStr := statusStyle.Render(fmt.Sprintf("%s %s", statusIcon, statusText))

	// PR info
	prStr := styles.MutedText.Render("—")
	if pr, ok := m.prCache[prCacheKey(sess.Repo, sess.Branch)]; ok && pr != nil {
		prStr = m.renderPRStatus(pr)
	}

	content := fmt.Sprintf("  %s %-25s %s  %s", issueRef, branchShort, statusStr, prStr)

	// Add last activity
	if sess.LastActivity != "" && !selected {
		activitySnippet := sess.LastActivity
		activityRunes := []rune(activitySnippet)
		if len(activityRunes) > 40 {
			activitySnippet = string(activityRunes[:37]) + "..."
		}
		content += styles.MutedText.Render("  " + activitySnippet)
	}

	if selected {
		return styles.SelectedItem.Width(m.width).Render(content)
	}
	return styles.NormalItem.Width(m.width).Render(content)
}

func (m *Model) renderPRStatus(pr *github.PullRequest) string {
	status := m.gh.GetPRStatus(pr)

	var parts []string
	prRef := fmt.Sprintf("PR #%d", pr.Number)

	switch {
	case status.State == "MERGED":
		parts = append(parts, styles.PRMerged.Render(prRef+" merged"))
	case status.State == "CLOSED":
		parts = append(parts, lipgloss.NewStyle().Foreground(styles.Muted).Render(prRef+" closed"))
	case status.Draft:
		parts = append(parts, styles.PRDraft.Render(prRef+" draft"))
	case status.Approved:
		parts = append(parts, styles.PRApproved.Render(prRef+" ✓ approved"))
	case status.ChangesRequested:
		parts = append(parts, lipgloss.NewStyle().Foreground(styles.Warning).Render(prRef+" changes requested"))
	case status.ReviewRequired:
		parts = append(parts, styles.PROpen.Render(prRef+" pending review"))
	default:
		parts = append(parts, styles.PROpen.Render(prRef+" open"))
	}

	return strings.Join(parts, " ")
}

func (m *Model) renderStatusBar() string {
	text := m.statusBarText
	if text == "" {
		text = "Ready"
	}
	return styles.StatusBar.Width(m.width).Render(text)
}

func (m *Model) renderHelpBar() string {
	var items []string
	if m.activePanel == PanelIssues {
		items = []string{"↑↓ navigate", "tab switch", "/ filter", "s spawn", "o open", "r refresh", "? help", "q quit"}
	} else {
		items = []string{"↑↓ navigate", "tab switch", "/ filter", "a attach", "o open PR", "x kill", "r refresh", "? help", "q quit"}
	}
	return styles.HelpBar.Width(m.width).Render(strings.Join(items, "  "))
}

func (m *Model) viewHelp() string {
	cfgPath, _ := config.Path()
	if cfgPath == "" {
		cfgPath = "~/.config/gao/config.yaml"
	}

	help := fmt.Sprintf(`
  gao - GitHub Agent Orchestrator

  Navigation:
    ↑/k, ↓/j    Move cursor up/down
    Tab          Switch between Issues and Sessions panels
    Esc          Go back

  Actions:
    /            Edit issue filter (GitHub search syntax)
    s            Spawn a new Claude Code session for selected issue
    a            Attach to selected session (opens interactive Claude)
    o            Open issue/PR in browser
    x            Kill selected session
    r            Refresh issues and session statuses

  Other:
    ?            Toggle this help screen
    q / Ctrl+C   Quit

  Sessions auto-refresh every 10 seconds.
  Config: %s
  Press Esc to return to dashboard.
`, cfgPath)
	width := m.width - 4
	if width < 0 {
		width = 0
	}
	return styles.BorderedBox.Width(width).Render(help)
}

func (m *Model) viewFilter() string {
	content := fmt.Sprintf("\n  Issue Filter (GitHub search syntax)\n\n  %s\n\n  Enter to apply, Esc to cancel.\n  Examples: is:open  assignee:@me  label:bug  archived:false  user:my-org\n",
		m.filterInput.View())
	width := m.width - 4
	if width < 0 {
		width = 0
	}
	return styles.BorderedBox.Width(width).Render(content)
}

func (m *Model) viewConfirm() string {
	content := fmt.Sprintf("\n  %s\n\n  Press y or Enter to confirm, n to cancel.\n", m.confirmMsg)
	width := m.width - 4
	if width < 0 {
		width = 0
	}
	return styles.BorderedBox.Width(width).Render(content)
}
