// Package tui implements the Bubble Tea terminal UI for gao.
package tui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"text/template"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/eulercb/github-agent-orchestrator/internal/claude"
	"github.com/eulercb/github-agent-orchestrator/internal/config"
	"github.com/eulercb/github-agent-orchestrator/internal/github"
	"github.com/eulercb/github-agent-orchestrator/internal/statusbar"
)

// Panel identifies which panel is focused.
type Panel int

// Panel constants.
const (
	PanelIssues Panel = iota
	PanelSessions
)

// View identifies the current screen.
type View int

// View constants.
const (
	ViewDashboard View = iota
	ViewHelp
	ViewConfirm
)

// prCacheKey builds a unique key for the PR cache from repo and branch.
func prCacheKey(repo, branch string) string {
	return repo + ":" + branch
}

// Model is the top-level Bubble Tea model.
type Model struct {
	cfg           *config.Config
	gh            *github.Client
	sessions      *claude.Manager
	statusProv    *statusbar.Provider
	keys          KeyMap
	width, height int

	// State
	currentView   View
	activePanel   Panel
	issues        []github.Issue
	prCache       map[string]*github.PullRequest // "repo:branch" -> PR
	issuesCursor  int
	sessionCursor int
	repoIndex     int
	statusBarText string
	errorMsg      string
	confirmMsg    string
	confirmAction func() tea.Msg
	loading       bool
}

// NewModel creates the initial TUI model.
func NewModel(cfg *config.Config, ghClient *github.Client, sessMgr *claude.Manager) Model {
	// Build the status bar provider with the built-in fallback.
	// The command comes from config; refresh runs async via refreshStatusBar().
	sbCmd := cfg.StatusBar.Command
	if sbCmd == "" && cfg.CCUsage.Enabled && cfg.CCUsage.Command != "" {
		sbCmd = cfg.CCUsage.Command
	}

	return Model{
		cfg:        cfg,
		gh:         ghClient,
		sessions:   sessMgr,
		statusProv: statusbar.NewProvider(sbCmd, nil),
		keys:       DefaultKeyMap(),
		prCache:    make(map[string]*github.PullRequest),
	}
}

// Messages

type issuesLoadedMsg struct {
	issues []github.Issue
	err    error
}

type prsLoadedMsg struct {
	prs map[string]*github.PullRequest
	err error
}

type statusRefreshMsg struct{}

type statusBarUpdatedMsg struct {
	text string
}

type sessionSpawnedMsg struct {
	session *claude.Session
	err     error
}

type sessionKilledMsg struct {
	id  string
	err error
}

type openBrowserMsg struct {
	err error
}

type tickMsg time.Time

type errMsg struct {
	err error
}

// Init starts the application.
func (m Model) Init() tea.Cmd { //nolint:gocritic // tea.Model interface requires value receiver
	return tea.Batch(
		m.fetchIssues(),
		m.refreshStatuses(),
		m.tickCmd(),
	)
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) { //nolint:gocritic // tea.Model interface requires value receiver
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case issuesLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.errorMsg = fmt.Sprintf("Failed to load issues: %v", msg.err)
		} else {
			m.issues = msg.issues
			m.errorMsg = ""
			// Clamp cursor to new list bounds
			lastIdx := len(m.issues) - 1
			if lastIdx < 0 {
				lastIdx = 0
			}
			if m.issuesCursor > lastIdx {
				m.issuesCursor = lastIdx
			}
		}
		cmd := m.fetchPRs()
		return m, cmd
	case prsLoadedMsg:
		if msg.err != nil {
			m.errorMsg = fmt.Sprintf("PR refresh: %v", msg.err)
			// Merge successful lookups into existing cache
			for k, v := range msg.prs {
				m.prCache[k] = v
			}
		} else {
			m.prCache = msg.prs
		}
		return m, nil
	case statusRefreshMsg:
		m.sessions.RefreshStatuses()
		cmd := m.refreshStatusBar()
		return m, cmd
	case statusBarUpdatedMsg:
		m.statusBarText = msg.text
		return m, nil
	case sessionSpawnedMsg:
		if msg.err != nil {
			m.errorMsg = fmt.Sprintf("Spawn failed: %v", msg.err)
		} else {
			m.errorMsg = ""
			m.activePanel = PanelSessions
		}
		return m, nil
	case sessionKilledMsg:
		if msg.err != nil {
			m.errorMsg = fmt.Sprintf("Kill failed: %v", msg.err)
		} else {
			// Clamp cursor after removal
			sessions := m.sessions.Sessions()
			lastIdx := len(sessions) - 1
			if lastIdx < 0 {
				lastIdx = 0
			}
			if m.sessionCursor > lastIdx {
				m.sessionCursor = lastIdx
			}
		}
		return m, nil
	case openBrowserMsg:
		if msg.err != nil {
			m.errorMsg = fmt.Sprintf("Browser open failed: %v", msg.err)
		}
		return m, nil
	case tickMsg:
		m.sessions.RefreshStatuses()
		cmd := m.fetchPRs()
		return m, tea.Batch(m.tickCmd(), cmd, m.refreshStatusBar())
	case errMsg:
		m.errorMsg = msg.err.Error()
		return m, nil
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Confirm view intercepts keys
	if m.currentView == ViewConfirm {
		switch msg.String() {
		case "y", "Y", "enter":
			m.currentView = ViewDashboard
			if m.confirmAction != nil {
				action := m.confirmAction
				m.confirmAction = nil
				return m, func() tea.Msg { return action() }
			}
		case "n", "N", "esc":
			m.currentView = ViewDashboard
			m.confirmAction = nil
		}
		return m, nil
	}

	// Help view
	if m.currentView == ViewHelp {
		if key.Matches(msg, m.keys.Back) || key.Matches(msg, m.keys.Help) {
			m.currentView = ViewDashboard
		}
		return m, nil
	}

	// Dashboard keys
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.currentView = ViewHelp
	case key.Matches(msg, m.keys.Tab):
		if m.activePanel == PanelIssues {
			m.activePanel = PanelSessions
		} else {
			m.activePanel = PanelIssues
		}
	case key.Matches(msg, m.keys.Up):
		m.moveCursor(-1)
	case key.Matches(msg, m.keys.Down):
		m.moveCursor(1)
	case key.Matches(msg, m.keys.Spawn):
		if m.activePanel == PanelIssues {
			cmd := m.spawnSession()
			return m, cmd
		}
	case key.Matches(msg, m.keys.Attach):
		if m.activePanel == PanelSessions {
			cmd := m.attachSession()
			return m, cmd
		}
	case key.Matches(msg, m.keys.Open):
		cmd := m.openInBrowser()
		return m, cmd
	case key.Matches(msg, m.keys.Delete):
		if m.activePanel == PanelSessions {
			m.killSession()
		}
	case key.Matches(msg, m.keys.Refresh):
		m.loading = true
		return m, tea.Batch(m.fetchIssues(), m.refreshStatuses())
	}
	return m, nil
}

func (m *Model) moveCursor(delta int) {
	switch m.activePanel {
	case PanelIssues:
		m.issuesCursor += delta
		if m.issuesCursor < 0 {
			m.issuesCursor = 0
		}
		lastIdx := len(m.issues) - 1
		if lastIdx < 0 {
			lastIdx = 0
		}
		if m.issuesCursor > lastIdx {
			m.issuesCursor = lastIdx
		}
	case PanelSessions:
		sessions := m.sessions.Sessions()
		m.sessionCursor += delta
		if m.sessionCursor < 0 {
			m.sessionCursor = 0
		}
		lastIdx := len(sessions) - 1
		if lastIdx < 0 {
			lastIdx = 0
		}
		if m.sessionCursor > lastIdx {
			m.sessionCursor = lastIdx
		}
	}
}

func (m *Model) currentRepo() *config.RepoConfig {
	if m.repoIndex < len(m.cfg.Repos) {
		return &m.cfg.Repos[m.repoIndex]
	}
	return nil
}

func (m *Model) selectedIssue() *github.Issue {
	if m.issuesCursor >= 0 && m.issuesCursor < len(m.issues) {
		return &m.issues[m.issuesCursor]
	}
	return nil
}

func (m *Model) selectedSession() *claude.Session {
	sessions := m.sessions.Sessions()
	if m.sessionCursor >= 0 && m.sessionCursor < len(sessions) {
		return &sessions[m.sessionCursor]
	}
	return nil
}

// Commands

func (m *Model) fetchIssues() tea.Cmd {
	return func() tea.Msg {
		repo := m.currentRepo()
		if repo == nil {
			return issuesLoadedMsg{err: fmt.Errorf("no repos configured")}
		}
		issues, err := m.gh.ListIssues(repo)
		return issuesLoadedMsg{issues: issues, err: err}
	}
}

func (m *Model) fetchPRs() tea.Cmd {
	return func() tea.Msg {
		prs := make(map[string]*github.PullRequest)
		sessions := m.sessions.Sessions()
		var firstErr error
		for i := range sessions {
			s := &sessions[i]
			if s.Branch == "" {
				continue
			}
			pr, err := m.gh.FindPRForBranch(s.Repo, s.Branch)
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("%s@%s: %w", s.Repo, s.Branch, err)
				}
				continue
			}
			prs[prCacheKey(s.Repo, s.Branch)] = pr
		}
		return prsLoadedMsg{prs: prs, err: firstErr}
	}
}

func (m *Model) refreshStatuses() tea.Cmd {
	return func() tea.Msg {
		return statusRefreshMsg{}
	}
}

func (m *Model) spawnSession() tea.Cmd {
	issue := m.selectedIssue()
	repo := m.currentRepo()
	if issue == nil || repo == nil {
		return nil
	}

	// Check if session already exists for this issue
	existing := m.sessions.FindByIssue(repo.FullName(), issue.Number)
	if existing != nil {
		m.errorMsg = fmt.Sprintf("Session already exists for issue #%d", issue.Number)
		return nil
	}

	issueNum := issue.Number
	issueTitle := issue.Title
	repoCopy := *repo

	return func() tea.Msg {
		sess, err := m.sessions.SpawnSession(&repoCopy, issueNum, issueTitle)
		return sessionSpawnedMsg{session: sess, err: err}
	}
}

func (m *Model) attachSession() tea.Cmd {
	sess := m.selectedSession()
	if sess == nil {
		return nil
	}

	sessionName := sess.TmuxSession

	// Check if Warp is available and configured
	useWarp := false
	if m.cfg.Attach.UseWarp != nil {
		useWarp = *m.cfg.Attach.UseWarp
	} else {
		// Auto-detect Warp
		_, err := exec.LookPath("warp-cli")
		useWarp = err == nil
	}

	if useWarp {
		return func() tea.Msg {
			cmd := exec.CommandContext(context.Background(), "warp-cli", "open-tab", "--", "tmux", "attach-session", "-t", sessionName)
			if err := cmd.Run(); err != nil {
				return errMsg{err: fmt.Errorf("warp attach: %w", err)}
			}
			return nil
		}
	}

	// Use the configurable attach command template, falling back to tmux attach
	attachCmd := m.resolveAttachCommand(sessionName)

	return tea.ExecProcess(
		exec.CommandContext(context.Background(), "sh", "-c", attachCmd),
		func(err error) tea.Msg {
			if err != nil {
				return errMsg{err: fmt.Errorf("attach: %w", err)}
			}
			return statusRefreshMsg{}
		},
	)
}

// resolveAttachCommand applies the session name to the attach command template.
func (m *Model) resolveAttachCommand(sessionName string) string {
	cmdTmpl := m.cfg.Attach.Command
	if cmdTmpl == "" {
		cmdTmpl = "tmux attach-session -t {{.Session}}"
	}

	tmpl, err := template.New("attach").Parse(cmdTmpl)
	if err != nil {
		return "tmux attach-session -t " + shellQuoteSession(sessionName)
	}

	var buf strings.Builder
	data := struct{ Session string }{Session: shellQuoteSession(sessionName)}
	if err := tmpl.Execute(&buf, data); err != nil {
		return "tmux attach-session -t " + shellQuoteSession(sessionName)
	}
	return buf.String()
}

func (m *Model) openInBrowser() tea.Cmd {
	switch m.activePanel {
	case PanelIssues:
		issue := m.selectedIssue()
		if issue == nil {
			return nil
		}
		url := issue.URL
		ghClient := m.gh
		return func() tea.Msg {
			return openBrowserMsg{err: ghClient.OpenInBrowser(url)}
		}
	case PanelSessions:
		sess := m.selectedSession()
		if sess == nil {
			return nil
		}
		pr, ok := m.prCache[prCacheKey(sess.Repo, sess.Branch)]
		if ok && pr != nil {
			url := pr.URL
			ghClient := m.gh
			return func() tea.Msg {
				return openBrowserMsg{err: ghClient.OpenInBrowser(url)}
			}
		}
	}
	return nil
}

func (m *Model) killSession() {
	sess := m.selectedSession()
	if sess == nil {
		return
	}

	sessID := sess.ID
	m.confirmMsg = fmt.Sprintf("Kill session %q? (y/n)", sess.ID)
	m.currentView = ViewConfirm
	m.confirmAction = func() tea.Msg {
		err := m.sessions.RemoveSession(sessID, true)
		return sessionKilledMsg{id: sessID, err: err}
	}
}

func (m *Model) refreshStatusBar() tea.Cmd {
	prov := m.statusProv
	sessions := m.sessions.Sessions()

	return func() tea.Msg {
		// Try the external status bar provider first (custom command or ccusage).
		// This may shell out, so it runs async to avoid blocking the event loop.
		if prov != nil {
			prov.Refresh()
			if text := prov.Text(); text != "" {
				return statusBarUpdatedMsg{text: text}
			}
		}

		// Built-in fallback: session counts
		var running, waiting, done, stopped int
		for i := range sessions {
			switch sessions[i].Status {
			case claude.StatusRunning:
				running++
			case claude.StatusWaiting:
				waiting++
			case claude.StatusDone:
				done++
			case claude.StatusStopped:
				stopped++
			}
		}

		parts := []string{
			fmt.Sprintf("Sessions: %d", len(sessions)),
		}
		if running > 0 {
			parts = append(parts, fmt.Sprintf("⚡ %d working", running))
		}
		if waiting > 0 {
			parts = append(parts, fmt.Sprintf("⏳ %d waiting", waiting))
		}
		if done > 0 {
			parts = append(parts, fmt.Sprintf("✓ %d done", done))
		}
		if stopped > 0 {
			parts = append(parts, fmt.Sprintf("✗ %d stopped", stopped))
		}

		return statusBarUpdatedMsg{text: strings.Join(parts, "  ")}
	}
}

// shellQuoteSession wraps a session name in single quotes for safe shell interpolation.
func shellQuoteSession(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func (m *Model) tickCmd() tea.Cmd {
	return tea.Tick(10*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
