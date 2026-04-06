package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/eulercb/github-agent-orchestrator/internal/claude"
	"github.com/eulercb/github-agent-orchestrator/internal/config"
	"github.com/eulercb/github-agent-orchestrator/internal/debug"
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

	// Reserve space for debug pane when visible.
	debugHeight := 0
	if m.showDebug {
		debugHeight = contentHeight / 3
		if debugHeight < 5 {
			debugHeight = 5
		}
		// Clamp so main content keeps at least 2 rows.
		maxDebug := contentHeight - 3 // 2 for content + 1 for separator
		if maxDebug < 0 {
			maxDebug = 0
		}
		if debugHeight > maxDebug {
			debugHeight = maxDebug
		}
		contentHeight -= debugHeight + 1 // +1 for separator
		if contentHeight < 1 {
			contentHeight = 1
		}
	}

	if m.showIssues {
		issueHeight := contentHeight / 2
		sessionHeight := contentHeight - issueHeight

		// Issues panel
		issuesContent := m.renderIssuesPanel(issueHeight)
		sections = append(sections, issuesContent)

		// Sessions panel
		sessionsContent := m.renderSessionsPanel(sessionHeight)
		sections = append(sections, sessionsContent)
	} else {
		// Sessions only — full height
		sessionsContent := m.renderSessionsPanel(contentHeight)
		sections = append(sections, sessionsContent)
	}

	// Debug pane
	if m.showDebug {
		separator := styles.DebugBorder.Render(strings.Repeat("─", m.width))
		sections = append(sections, separator)
		debugContent := m.renderDebugPane(debugHeight)
		sections = append(sections, debugContent)
	}

	// Status bar
	statusBar := m.renderStatusBar()
	sections = append(sections, statusBar)

	// Help bar
	helpBar := m.renderHelpBar()
	sections = append(sections, helpBar)

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m *Model) renderTitleBar() string {
	left := styles.TitleBar.Render(" gao ")

	// Show repos_dir on the right
	reposInfo := m.cfg.ReposDir
	if reposInfo == "" {
		reposInfo = "(no repos_dir)"
	}
	right := styles.TitleBar.Render(fmt.Sprintf(" %s ", reposInfo))
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

	filterText := m.issueFilter
	if filterText == "" {
		filterText = config.DefaultIssueFilter
	}
	if filterText != "" {
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

	// Determine if issues span multiple repos so we can show repo prefixes.
	multiRepo := false
	if len(m.issues) > 1 {
		first := m.issues[0].Repository.NameWithOwner
		for i := 1; i < len(m.issues); i++ {
			if m.issues[i].Repository.NameWithOwner != first {
				multiRepo = true
				break
			}
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

		line := m.renderIssueLine(&m.issues[i], selected, multiRepo)
		lines = append(lines, line)
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *Model) renderIssueLine(issue *github.Issue, selected, multiRepo bool) string {
	issueRepo := issue.Repository.NameWithOwner

	hasSession := false
	if issueRepo != "" {
		if s := m.sessions.FindByIssue(issueRepo, issue.Number); s != nil {
			hasSession = true
		}
	}

	indicator := "  "
	if hasSession {
		indicator = "● "
	}

	// Show repo name only when results span multiple repos.
	repoPrefix := ""
	if multiRepo && issue.Repository.NameWithOwner != "" {
		repoPrefix = styles.MutedText.Render(issue.Repository.NameWithOwner) + " "
	}

	number := fmt.Sprintf("#%-5d", issue.Number)
	title := issue.Title
	maxTitleLen := m.width - 33 - lipgloss.Width(repoPrefix)
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

	content := fmt.Sprintf("%s%s%s %s%s", indicator, repoPrefix, number, title, labelStr)

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
	if m.scanning {
		header += styles.MutedText.Render(" (scanning worktrees...)")
	} else if m.loading {
		header += styles.MutedText.Render(" (refreshing...)")
	}
	var lines []string
	lines = append(lines, header)

	sessions := m.sessions.Sessions()

	if len(sessions) == 0 {
		hint := "  No active sessions."
		if m.showIssues {
			hint += " Select an issue and press 's' to spawn."
		} else {
			hint += " Press 'w' to scan worktrees or 'i' to show issues."
		}
		lines = append(lines, styles.MutedText.Render(hint))
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

	statusStr := statusStyle.Render(fmt.Sprintf("%s %s", statusIcon, statusText))

	// PR info
	prStr := styles.MutedText.Render("—")
	if pr, ok := m.prCache[prCacheKey(sess.Repo, sess.Branch)]; ok && pr != nil {
		prStr = m.renderPRStatus(pr)
	}

	// Issue title or branch fallback — use remaining terminal width.
	issueTitle := sess.IssueTitle
	if issueTitle == "" {
		issueTitle = sess.Branch
	}
	// Calculate space used by fixed columns:
	// "  " + issueRef + " " + status + "  " + prStr + "  "
	fixedWidth := 2 + lipgloss.Width(issueRef) + 1 + lipgloss.Width(statusStr) + 2 + lipgloss.Width(prStr) + 2
	maxTitle := m.width - fixedWidth
	if maxTitle < 10 {
		maxTitle = 10
	}
	if lipgloss.Width(issueTitle) > maxTitle {
		issueTitle = ansi.Truncate(issueTitle, maxTitle-3, "...")
	}

	content := fmt.Sprintf("  %s %s  %s  %s", issueRef, statusStr, prStr, issueTitle)

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

func (m *Model) renderDebugPane(maxHeight int) string {
	header := styles.DebugTitle.Render("Debug Log")

	events := m.debugLog.Events()

	var lines []string
	lines = append(lines, header)

	if len(events) == 0 {
		lines = append(lines, styles.MutedText.Render("  No events recorded yet"))
		return lipgloss.JoinVertical(lipgloss.Left, lines...)
	}

	// Show the tail of events that fit in the available height.
	visibleCount := maxHeight - 1 // minus header
	if visibleCount < 1 {
		visibleCount = 1
	}
	start := 0
	if len(events) > visibleCount {
		start = len(events) - visibleCount
	}

	for i := start; i < len(events); i++ {
		line := m.renderDebugEvent(&events[i])
		lines = append(lines, line)
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func (m *Model) renderDebugEvent(evt *debug.Event) string {
	ts := styles.DebugTimestamp.Render(evt.StartedAt.Format("15:04:05.000"))

	var icon, msg string
	switch evt.Status {
	case debug.StatusRunning:
		icon = styles.DebugRunning.Render("⟳")
		msg = styles.DebugRunning.Render(evt.Message)
	case debug.StatusDone:
		icon = styles.DebugDone.Render("✓")
		msg = styles.DebugDone.Render(evt.Message)
	case debug.StatusError:
		icon = styles.DebugError.Render("✗")
		msg = styles.DebugError.Render(evt.Message)
	default: // StatusInfo
		icon = styles.DebugInfo.Render("·")
		msg = styles.DebugInfo.Render(evt.Message)
	}

	dur := formatDuration(evt.Duration())
	durStr := ""
	if evt.Status == debug.StatusRunning {
		durStr = styles.DebugDuration.Render(" (" + dur + "...)")
	} else if evt.Status != debug.StatusInfo {
		durStr = styles.DebugDuration.Render(" (" + dur + ")")
	}

	detail := ""
	if evt.Detail != "" {
		maxDetail := m.width - 40
		if maxDetail < 10 {
			maxDetail = 10
		}
		detailText := ansi.Truncate(evt.Detail, maxDetail, "…")
		detail = " " + styles.DebugDetail.Render(detailText)
	}

	line := fmt.Sprintf("  %s %s %s%s%s", ts, icon, msg, durStr, detail)
	return ansi.Truncate(line, m.width, "…")
}

// formatDuration returns a human-readable compact duration string.
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		minutes := d / time.Minute
		seconds := (d % time.Minute) / time.Second
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	}
}

func (m *Model) renderHelpBar() string {
	var items []string
	switch m.activePanel {
	case PanelIssues:
		items = []string{"↑↓ navigate", "tab switch", "/ filter", "s spawn", "w scan", "o open", "i hide issues", "d debug", "r refresh", "? help", "q quit"}
	case PanelSessions:
		items = []string{"↑↓ navigate", "a worktree", "w scan", "o open PR", "O open issue", "x kill"}
		if m.showIssues {
			items = append(items, "tab switch", "i hide issues")
		} else {
			items = append(items, "i show issues")
		}
		items = append(items, "d debug", "r refresh", "? help", "q quit")
	}
	return styles.HelpBar.Width(m.width).Render(strings.Join(items, "  "))
}

func (m *Model) viewHelp() string {
	cfgPath := m.cfgPath
	if cfgPath == "" {
		cfgPath = "(unknown)"
	}

	help := fmt.Sprintf(`
  gao - GitHub Agent Orchestrator

  Navigation:
    ↑/k, ↓/j    Move cursor up/down
    Tab          Switch between Issues and Sessions panels
    Esc          Go back

  Actions:
    /            Edit issue filter (GitHub search syntax, in Issues panel)
    s            Spawn a new Claude Code session for selected issue
    a            Open worktree directory in a new terminal
    w            Scan worktrees (discover new, remove stale)
    o            Open selected issue (Issues) or session PR (Sessions) in browser
    O            Open session's issue in browser (Sessions panel)
    x            Kill selected session
    i            Toggle issues panel visibility
    d            Toggle debug log pane
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
	title := "Issue Filter (GitHub search syntax)"
	examples := "is:open  assignee:@me  label:bug  repo:org/repo  user:my-org"
	content := fmt.Sprintf("\n  %s\n\n  %s\n\n  Enter to apply, Esc to cancel.\n  Examples: %s\n",
		title, m.filterInput.View(), examples)
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
