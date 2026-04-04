package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/eulercb/github-agent-orchestrator/internal/claude"
	"github.com/eulercb/github-agent-orchestrator/internal/github"
	"github.com/eulercb/github-agent-orchestrator/internal/tui/styles"
)

// View renders the TUI.
func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	switch m.currentView {
	case ViewHelp:
		return m.viewHelp()
	case ViewConfirm:
		return m.viewConfirm()
	default:
		return m.viewDashboard()
	}
}

func (m Model) viewDashboard() string {
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
	contentHeight := m.height - 4 // title + status + help + error
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

func (m Model) renderTitleBar() string {
	repoName := "(no repo configured)"
	if repo := m.currentRepo(); repo != nil {
		repoName = repo.FullName()
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

func (m Model) renderIssuesPanel(maxHeight int) string {
	panelActive := m.activePanel == PanelIssues

	titleStyle := styles.SectionTitle
	if panelActive {
		titleStyle = titleStyle.Foreground(styles.Primary)
	}

	header := titleStyle.Render("Issues")
	if m.loading {
		header += styles.MutedText.Render(" (loading...)")
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
		issue := m.issues[i]
		selected := panelActive && i == m.issuesCursor

		line := m.renderIssueLine(issue, selected)
		lines = append(lines, line)
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m Model) renderIssueLine(issue github.Issue, selected bool) string {
	// Check if there's an active session for this issue
	repo := m.currentRepo()
	hasSession := false
	if repo != nil {
		if s := m.sessions.FindByIssue(repo.FullName(), issue.Number); s != nil {
			hasSession = true
		}
	}

	indicator := "  "
	if hasSession {
		indicator = "● "
	}

	number := fmt.Sprintf("#%-5d", issue.Number)
	title := issue.Title
	if len(title) > m.width-30 {
		title = title[:m.width-33] + "..."
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
	return styles.NormalItem.Render(content)
}

func (m Model) renderSessionsPanel(maxHeight int) string {
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
		sess := sessions[i]
		selected := panelActive && i == m.sessionCursor

		line := m.renderSessionLine(sess, selected)
		lines = append(lines, line)
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m Model) renderSessionLine(sess claude.Session, selected bool) string {
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
	if len(branchShort) > 25 {
		branchShort = branchShort[:22] + "..."
	}

	statusStr := statusStyle.Render(fmt.Sprintf("%s %s", statusIcon, statusText))

	// PR info
	prStr := styles.MutedText.Render("—")
	if pr, ok := m.prCache[sess.Branch]; ok && pr != nil {
		prStr = m.renderPRStatus(pr)
	}

	content := fmt.Sprintf("  %s %-25s %s  %s", issueRef, branchShort, statusStr, prStr)

	// Add last activity
	if sess.LastActivity != "" && !selected {
		activitySnippet := sess.LastActivity
		if len(activitySnippet) > 40 {
			activitySnippet = activitySnippet[:37] + "..."
		}
		content += styles.MutedText.Render("  " + activitySnippet)
	}

	if selected {
		return styles.SelectedItem.Width(m.width).Render(content)
	}
	return styles.NormalItem.Render(content)
}

func (m Model) renderPRStatus(pr *github.PullRequest) string {
	status := m.gh.GetPRStatus(pr)

	var parts []string
	prRef := fmt.Sprintf("PR #%d", pr.Number)

	switch {
	case status.State == "MERGED":
		parts = append(parts, styles.PRMerged.Render(prRef+" merged"))
	case status.Draft:
		parts = append(parts, styles.PRDraft.Render(prRef+" draft"))
	case status.Approved:
		parts = append(parts, styles.PRApproved.Render(prRef+" ✓ approved"))
	case status.HasPendingReviews:
		parts = append(parts, styles.PROpen.Render(prRef+" pending review"))
	default:
		parts = append(parts, styles.PROpen.Render(prRef+" open"))
	}

	return strings.Join(parts, " ")
}

func (m Model) renderStatusBar() string {
	text := m.statusBarText
	if text == "" {
		text = "Ready"
	}
	return styles.StatusBar.Width(m.width).Render(text)
}

func (m Model) renderHelpBar() string {
	var items []string
	if m.activePanel == PanelIssues {
		items = []string{"↑↓ navigate", "tab switch", "s spawn", "o open", "r refresh", "? help", "q quit"}
	} else {
		items = []string{"↑↓ navigate", "tab switch", "a attach", "o open PR", "x kill", "r refresh", "? help", "q quit"}
	}
	return styles.HelpBar.Width(m.width).Render(strings.Join(items, "  "))
}

func (m Model) viewHelp() string {
	help := `
  gao - GitHub Agent Orchestrator

  Navigation:
    ↑/k, ↓/j    Move cursor up/down
    Tab          Switch between Issues and Sessions panels
    Enter        Select / confirm
    Esc          Go back

  Actions:
    s            Spawn a new Claude Code session for selected issue
    a            Attach to selected session (opens in tmux)
    o            Open issue/PR in browser
    x            Kill selected session
    r            Refresh issues and session statuses

  Other:
    ?            Toggle this help screen
    q / Ctrl+C   Quit

  Sessions auto-refresh every 10 seconds.
  Press Esc to return to dashboard.
`
	return styles.BorderedBox.Width(m.width - 4).Render(help)
}

func (m Model) viewConfirm() string {
	content := fmt.Sprintf("\n  %s\n\n  Press y to confirm, n to cancel.\n", m.confirmMsg)
	return styles.BorderedBox.Width(m.width - 4).Render(content)
}
