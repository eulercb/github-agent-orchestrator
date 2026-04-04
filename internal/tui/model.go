// Package tui implements the Bubble Tea terminal UI for gao.
package tui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/eulercb/github-agent-orchestrator/internal/claude"
	"github.com/eulercb/github-agent-orchestrator/internal/config"
	"github.com/eulercb/github-agent-orchestrator/internal/github"
)

// Panel identifies which panel is focused.
type Panel int

const (
	PanelIssues   Panel = iota
	PanelSessions
)

// View identifies the current screen.
type View int

const (
	ViewDashboard View = iota
	ViewHelp
	ViewConfirm
)

// Model is the top-level Bubble Tea model.
type Model struct {
	cfg           config.Config
	gh            *github.Client
	sessions      *claude.Manager
	keys          KeyMap
	width, height int

	// State
	currentView   View
	activePanel   Panel
	issues        []github.Issue
	prCache       map[string]*github.PullRequest // branch -> PR
	issuesCursor  int
	sessionCursor int
	repoIndex     int
	statusBarText string
	errorMsg      string
	confirmMsg    string
	confirmAction func() tea.Msg
	loading       bool

	// Ticker for auto-refresh
	lastRefresh time.Time
}

// NewModel creates the initial TUI model.
func NewModel(cfg config.Config, ghClient *github.Client, sessMgr *claude.Manager) Model {
	return Model{
		cfg:      cfg,
		gh:       ghClient,
		sessions: sessMgr,
		keys:     DefaultKeyMap(),
		prCache:  make(map[string]*github.PullRequest),
	}
}

// Messages

type issuesLoadedMsg struct {
	issues []github.Issue
	err    error
}

type prsLoadedMsg struct {
	prs map[string]*github.PullRequest
}

type statusRefreshMsg struct{}

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

type statusBarUpdateMsg struct {
	text string
}

type tickMsg time.Time

type errMsg struct {
	err error
}

// Init starts the application.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.fetchIssues(),
		m.refreshStatuses(),
		m.tickCmd(),
	)
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
		}
		return m, m.fetchPRs()
	case prsLoadedMsg:
		m.prCache = msg.prs
		return m, nil
	case statusRefreshMsg:
		m.sessions.RefreshStatuses()
		m.updateStatusBar()
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
		}
		return m, nil
	case openBrowserMsg:
		if msg.err != nil {
			m.errorMsg = fmt.Sprintf("Browser open failed: %v", msg.err)
		}
		return m, nil
	case statusBarUpdateMsg:
		m.statusBarText = msg.text
		return m, nil
	case tickMsg:
		m.sessions.RefreshStatuses()
		m.updateStatusBar()
		m.lastRefresh = time.Now()
		return m, tea.Batch(m.tickCmd(), m.fetchPRs())
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
		return m, m.spawnSession()
	case key.Matches(msg, m.keys.Attach):
		return m, m.attachSession()
	case key.Matches(msg, m.keys.Open):
		return m, m.openInBrowser()
	case key.Matches(msg, m.keys.Delete):
		return m, m.killSession()
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
		if m.issuesCursor >= len(m.issues) {
			m.issuesCursor = len(m.issues) - 1
		}
	case PanelSessions:
		sessions := m.sessions.Sessions()
		m.sessionCursor += delta
		if m.sessionCursor < 0 {
			m.sessionCursor = 0
		}
		if m.sessionCursor >= len(sessions) {
			m.sessionCursor = len(sessions) - 1
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
		issues, err := m.gh.ListIssues(*repo)
		return issuesLoadedMsg{issues: issues, err: err}
	}
}

func (m *Model) fetchPRs() tea.Cmd {
	return func() tea.Msg {
		prs := make(map[string]*github.PullRequest)
		sessions := m.sessions.Sessions()
		for _, s := range sessions {
			if s.Branch == "" {
				continue
			}
			pr, err := m.gh.FindPRForBranch(s.Repo, s.Branch)
			if err == nil && pr != nil {
				prs[s.Branch] = pr
			}
		}
		return prsLoadedMsg{prs: prs}
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
		sess, err := m.sessions.SpawnSession(repoCopy, issueNum, issueTitle)
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
			cmd := exec.Command("warp-cli", "open-tab", "--", "tmux", "attach-session", "-t", sessionName)
			if err := cmd.Run(); err != nil {
				return errMsg{err: fmt.Errorf("warp attach: %w", err)}
			}
			return nil
		}
	}

	// Default: suspend TUI, attach, resume
	return tea.ExecProcess(
		exec.Command("tmux", "attach-session", "-t", sessionName),
		func(err error) tea.Msg {
			if err != nil {
				return errMsg{err: fmt.Errorf("tmux attach: %w", err)}
			}
			return statusRefreshMsg{}
		},
	)
}

func (m *Model) openInBrowser() tea.Cmd {
	switch m.activePanel {
	case PanelIssues:
		issue := m.selectedIssue()
		if issue == nil {
			return nil
		}
		url := issue.URL
		return func() tea.Msg {
			cmd := exec.Command("open", url)
			if err := cmd.Run(); err != nil {
				// Try xdg-open for Linux
				cmd = exec.Command("xdg-open", url)
				err = cmd.Run()
				return openBrowserMsg{err: err}
			}
			return openBrowserMsg{}
		}
	case PanelSessions:
		sess := m.selectedSession()
		if sess == nil {
			return nil
		}
		pr, ok := m.prCache[sess.Branch]
		if ok && pr != nil {
			url := pr.URL
			return func() tea.Msg {
				cmd := exec.Command("open", url)
				if err := cmd.Run(); err != nil {
					cmd = exec.Command("xdg-open", url)
					err = cmd.Run()
					return openBrowserMsg{err: err}
				}
				return openBrowserMsg{}
			}
		}
	}
	return nil
}

func (m *Model) killSession() tea.Cmd {
	sess := m.selectedSession()
	if sess == nil {
		return nil
	}

	sessID := sess.ID
	m.confirmMsg = fmt.Sprintf("Kill session %q? (y/n)", sess.ID)
	m.currentView = ViewConfirm
	m.confirmAction = func() tea.Msg {
		err := m.sessions.RemoveSession(sessID, true)
		return sessionKilledMsg{id: sessID, err: err}
	}
	return nil
}

func (m *Model) updateStatusBar() {
	sessions := m.sessions.Sessions()
	var running, waiting, done, stopped int
	for _, s := range sessions {
		switch s.Status {
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

	m.statusBarText = strings.Join(parts, "  ")
}

func (m *Model) tickCmd() tea.Cmd {
	return tea.Tick(10*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
