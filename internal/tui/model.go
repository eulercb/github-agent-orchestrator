// Package tui implements the Bubble Tea terminal UI for gao.
package tui

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/eulercb/github-agent-orchestrator/internal/claude"
	"github.com/eulercb/github-agent-orchestrator/internal/config"
	"github.com/eulercb/github-agent-orchestrator/internal/github"
	"github.com/eulercb/github-agent-orchestrator/internal/repo"
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
	ViewFilter
)

// prCacheKey builds a unique key for the PR cache from repo and branch.
func prCacheKey(repoName, branch string) string {
	return repoName + ":" + branch
}

// Model is the top-level Bubble Tea model.
type Model struct {
	cfg           *config.Config
	gh            *github.Client
	sessions      *claude.Manager
	statusProv    *statusbar.Provider
	keys          KeyMap
	width, height int
	cfgPath       string

	// Discovered repos (refreshed on sync)
	repos []repo.Repo

	// State
	currentView   View
	activePanel   Panel
	issues        []github.Issue
	prCache       map[string]*github.PullRequest // "repo:branch" -> PR
	issuesCursor  int
	sessionCursor int
	statusBarText string
	errorMsg      string
	confirmMsg    string
	confirmAction func() tea.Msg
	loading       bool
	issueFilter   string
	filterInput   textinput.Model

	// Issues pane visibility
	showIssues        bool
	issuesInitialized bool
	filterForToggle   bool // true when filter editor was opened by toggle
}

// NewModel creates the initial TUI model.
func NewModel(cfg *config.Config, ghClient *github.Client, sessMgr *claude.Manager) Model {
	// Build the status bar provider with the built-in fallback.
	sbCmd := cfg.StatusBar.Command
	if sbCmd == "" && cfg.CCUsage.Enabled && cfg.CCUsage.Command != "" {
		sbCmd = cfg.CCUsage.Command
	}

	ti := textinput.New()
	ti.Placeholder = config.DefaultIssueFilter
	ti.CharLimit = 256

	issueFilter := cfg.IssueFilter
	if issueFilter == "" {
		issueFilter = config.DefaultIssueFilter
	}

	ti.SetValue(issueFilter)

	cfgPath, _ := config.Path()

	// Discover repos at startup
	var repos []repo.Repo
	var initErr string
	if discovered, err := sessMgr.DiscoverRepos(); err != nil {
		initErr = fmt.Sprintf("Repo discovery failed: %v", err)
	} else {
		repos = discovered
	}

	showIssues := cfg.TrackIssues

	return Model{
		cfg:               cfg,
		gh:                ghClient,
		sessions:          sessMgr,
		statusProv:        statusbar.NewProvider(sbCmd, nil),
		keys:              DefaultKeyMap(),
		prCache:           make(map[string]*github.PullRequest),
		repos:             repos,
		errorMsg:          initErr,
		issueFilter:       issueFilter,
		filterInput:       ti,
		cfgPath:           cfgPath,
		showIssues:        showIssues,
		issuesInitialized: showIssues,
		activePanel:       panelForStart(showIssues),
	}
}

// panelForStart returns the initial active panel based on issues visibility.
func panelForStart(showIssues bool) Panel {
	if showIssues {
		return PanelIssues
	}
	return PanelSessions
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

type worktreesSyncedMsg struct {
	added   int
	removed int
	repos   []repo.Repo
	err     error
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
	cmds := []tea.Cmd{
		m.refreshStatuses(),
		m.syncWorktrees(),
		m.tickCmd(),
	}
	if m.showIssues {
		cmds = append(cmds, m.fetchIssues())
	}
	return tea.Batch(cmds...)
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) { //nolint:gocritic // tea.Model interface requires value receiver
	// When the filter input is active, intercept key events and window
	// resizes for the filter editor. All other messages (ticks, async
	// loads, status updates) fall through to the main switch so background
	// refreshes continue and ticks are rescheduled.
	var filterCmd tea.Cmd
	if m.currentView == ViewFilter {
		switch msg.(type) {
		case tea.KeyMsg, tea.WindowSizeMsg:
			return m.updateFilter(msg)
		default:
			// Forward to textinput for cursor blink / internal timers,
			// then fall through to the main switch below.
			m.filterInput, filterCmd = m.filterInput.Update(msg)
		}
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, filterCmd
	case issuesLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.errorMsg = fmt.Sprintf("Failed to load issues: %v", msg.err)
		} else {
			m.issues = msg.issues
			m.errorMsg = ""
			m.issuesInitialized = true
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
		return m, tea.Batch(filterCmd, cmd)
	case prsLoadedMsg:
		m.prCache = msg.prs
		return m, filterCmd
	case statusRefreshMsg:
		m.sessions.RefreshStatuses()
		cmd := m.refreshStatusBar()
		return m, tea.Batch(filterCmd, cmd)
	case statusBarUpdatedMsg:
		m.statusBarText = msg.text
		return m, filterCmd
	case sessionSpawnedMsg:
		if msg.err != nil {
			m.errorMsg = fmt.Sprintf("Spawn failed: %v", msg.err)
		} else {
			m.errorMsg = ""
			m.activePanel = PanelSessions
		}
		return m, filterCmd
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
		return m, filterCmd
	case worktreesSyncedMsg:
		if msg.err != nil {
			m.errorMsg = fmt.Sprintf("Worktree sync failed: %v", msg.err)
			return m, filterCmd
		}
		// Update discovered repos
		if msg.repos != nil {
			m.repos = msg.repos
		}
		if msg.added > 0 || msg.removed > 0 {
			var parts []string
			if msg.added > 0 {
				parts = append(parts, fmt.Sprintf("%d added", msg.added))
			}
			if msg.removed > 0 {
				parts = append(parts, fmt.Sprintf("%d removed", msg.removed))
				// Clamp cursor so it doesn't point past the end of the list.
				lastIdx := len(m.sessions.Sessions()) - 1
				if lastIdx < 0 {
					lastIdx = 0
				}
				if m.sessionCursor > lastIdx {
					m.sessionCursor = lastIdx
				}
			}
			m.errorMsg = fmt.Sprintf("Worktrees synced: %s", strings.Join(parts, ", "))
			return m, tea.Batch(filterCmd, m.fetchPRs())
		}
		m.errorMsg = ""
		return m, filterCmd
	case openBrowserMsg:
		if msg.err != nil {
			m.errorMsg = fmt.Sprintf("Browser open failed: %v", msg.err)
		}
		return m, filterCmd
	case tickMsg:
		m.sessions.RefreshStatuses()
		cmd := m.fetchPRs()
		cmds := []tea.Cmd{filterCmd, m.tickCmd(), cmd, m.refreshStatusBar()}
		return m, tea.Batch(cmds...)
	case errMsg:
		m.errorMsg = msg.err.Error()
		return m, filterCmd
	}
	return m, filterCmd
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
		if m.showIssues {
			switch m.activePanel {
			case PanelIssues:
				m.activePanel = PanelSessions
			case PanelSessions:
				m.activePanel = PanelIssues
			}
		}
	case key.Matches(msg, m.keys.Up):
		m.moveCursor(-1)
	case key.Matches(msg, m.keys.Down):
		m.moveCursor(1)
	case key.Matches(msg, m.keys.Spawn):
		if m.activePanel == PanelIssues && m.showIssues {
			cmd := m.spawnSession()
			return m, cmd
		}
	case key.Matches(msg, m.keys.Attach):
		if m.activePanel == PanelSessions {
			cmd := m.attachSession()
			return m, cmd
		}
	case key.Matches(msg, m.keys.ImportWorktrees):
		cmd := m.syncWorktrees()
		return m, cmd
	case key.Matches(msg, m.keys.Open):
		cmd := m.openInBrowser()
		return m, cmd
	case key.Matches(msg, m.keys.OpenIssue):
		if m.activePanel == PanelSessions {
			cmd := m.openSessionIssueBrowser()
			return m, cmd
		}
	case key.Matches(msg, m.keys.Delete):
		if m.activePanel == PanelSessions {
			m.killSession()
		}
	case key.Matches(msg, m.keys.Refresh):
		m.loading = true
		cmds := []tea.Cmd{m.refreshStatuses()}
		if m.showIssues {
			cmds = append(cmds, m.fetchIssues())
		}
		return m, tea.Batch(cmds...)
	case key.Matches(msg, m.keys.Filter):
		if m.activePanel != PanelIssues || !m.showIssues {
			break
		}
		m.currentView = ViewFilter
		// Size the input to fit the bordered box (minus padding and borders).
		inputWidth := m.width - 10
		if inputWidth < 20 {
			inputWidth = 20
		}
		m.filterInput.Width = inputWidth
		m.filterInput.Placeholder = config.DefaultIssueFilter
		m.filterInput.SetValue(m.issueFilter)
		m.filterInput.Focus()
		return m, m.filterInput.Cursor.BlinkCmd()
	case key.Matches(msg, m.keys.ToggleIssues):
		return m.toggleIssues()
	}
	return m, nil
}

// toggleIssues handles showing/hiding the issues pane.
func (m *Model) toggleIssues() (tea.Model, tea.Cmd) {
	if m.showIssues {
		// Hide issues
		m.cfg.TrackIssues = false
		if err := config.Save(m.cfg); err != nil {
			m.cfg.TrackIssues = true
			m.errorMsg = fmt.Sprintf("Save config: %v", err)
			return m, nil
		}
		m.showIssues = false
		if m.activePanel == PanelIssues {
			m.activePanel = PanelSessions
		}
		return m, nil
	}

	// Show issues
	m.cfg.TrackIssues = true
	if err := config.Save(m.cfg); err != nil {
		m.cfg.TrackIssues = false
		m.errorMsg = fmt.Sprintf("Save config: %v", err)
		return m, nil
	}
	m.showIssues = true

	if !m.issuesInitialized {
		// First time: open filter editor so user can configure the query
		m.activePanel = PanelIssues
		m.filterForToggle = true
		m.currentView = ViewFilter
		inputWidth := m.width - 10
		if inputWidth < 20 {
			inputWidth = 20
		}
		m.filterInput.Width = inputWidth
		m.filterInput.Placeholder = config.DefaultIssueFilter
		m.filterInput.SetValue(m.issueFilter)
		m.filterInput.Focus()
		return m, m.filterInput.Cursor.BlinkCmd()
	}

	m.activePanel = PanelIssues
	cmd := m.fetchIssues()
	return m, cmd
}

// updateFilter handles all messages while the filter editor is active.
func (m *Model) updateFilter(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEnter:
			// Apply the filter and refresh the active panel's data.
			m.currentView = ViewDashboard
			m.filterInput.Blur()
			query := strings.TrimSpace(m.filterInput.Value())
			m.loading = true
			m.filterForToggle = false
			// Persist the filter to config
			prevFilter := m.cfg.IssueFilter
			m.cfg.IssueFilter = query
			if err := config.Save(m.cfg); err != nil {
				m.cfg.IssueFilter = prevFilter
				m.errorMsg = fmt.Sprintf("Save config: %v", err)
			}
			m.issueFilter = query
			m.issuesCursor = 0
			cmd := m.fetchIssues()
			return m, cmd
		case tea.KeyEsc:
			// Cancel: restore the previous value.
			m.currentView = ViewDashboard
			m.filterInput.Blur()
			m.filterInput.SetValue(m.issueFilter)
			// If this filter was opened by toggle and user cancels,
			// revert the toggle.
			if m.filterForToggle {
				m.filterForToggle = false
				m.cfg.TrackIssues = false
				if err := config.Save(m.cfg); err != nil {
					m.cfg.TrackIssues = true
					m.errorMsg = fmt.Sprintf("Save config: %v", err)
				}
				m.showIssues = false
				m.activePanel = PanelSessions
			}
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		inputWidth := m.width - 10
		if inputWidth < 20 {
			inputWidth = 20
		}
		m.filterInput.Width = inputWidth
	}

	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	return m, cmd
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

// findRepoForIssue finds the discovered repo that matches the issue's repository.
func (m *Model) findRepoForIssue(issueRepo string) *repo.Repo {
	for i := range m.repos {
		if m.repos[i].FullName() == issueRepo {
			return &m.repos[i]
		}
	}
	// Fall back to first discovered repo if we only have one.
	if len(m.repos) == 1 {
		return &m.repos[0]
	}
	return nil
}

// Commands

func (m *Model) fetchIssues() tea.Cmd {
	search := m.issueFilter
	return func() tea.Msg {
		issues, err := m.gh.ListIssues(search)
		return issuesLoadedMsg{issues: issues, err: err}
	}
}

func (m *Model) fetchPRs() tea.Cmd {
	return func() tea.Msg {
		prs := make(map[string]*github.PullRequest)
		sessions := m.sessions.Sessions()
		for i := range sessions {
			s := &sessions[i]
			if s.Branch == "" {
				continue
			}
			pr, err := m.gh.FindPRForBranch(s.Repo, s.Branch)
			if err != nil {
				continue
			}
			prs[prCacheKey(s.Repo, s.Branch)] = pr
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
	if issue == nil {
		return nil
	}

	if len(m.repos) == 0 {
		m.errorMsg = "No repos discovered in repos_dir"
		return nil
	}

	// Determine which issue repo this belongs to.
	issueRepo := issue.Repository.NameWithOwner

	// Find the target repo for spawning (where the session/PR will live).
	r := m.findRepoForIssue(issueRepo)
	if r == nil {
		m.errorMsg = fmt.Sprintf("No local repo found for %s", issueRepo)
		return nil
	}

	// Check if session already exists for this issue
	if issueRepo == "" {
		issueRepo = r.FullName()
	}
	existing := m.sessions.FindByIssue(issueRepo, issue.Number)
	if existing != nil {
		m.errorMsg = fmt.Sprintf("Session already exists for issue #%d", issue.Number)
		return nil
	}

	issueNum := issue.Number
	issueTitle := issue.Title
	repoCopy := *r

	return func() tea.Msg {
		sess, err := m.sessions.SpawnSession(&repoCopy, issueRepo, issueNum, issueTitle)
		return sessionSpawnedMsg{session: sess, err: err}
	}
}

func (m *Model) attachSession() tea.Cmd {
	sess := m.selectedSession()
	if sess == nil {
		return nil
	}

	workDir := sess.WorktreePath
	spawnCmd := m.cfg.Spawn.Command
	if spawnCmd == "" {
		spawnCmd = "claude --dangerously-skip-permissions"
	}

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
			cmd := exec.CommandContext(context.Background(),
				"warp-cli", "open-tab", "--",
				"sh", "-c", "cd "+shellQuoteSession(workDir)+" && "+spawnCmd)
			if err := cmd.Run(); err != nil {
				return errMsg{err: fmt.Errorf("warp attach: %w", err)}
			}
			return nil
		}
	}

	// Launch the spawn command interactively in the session's worktree
	attachCmd := m.resolveAttachCommand(workDir)

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

// resolveAttachCommand builds the attach command for a session's worktree directory.
func (m *Model) resolveAttachCommand(workDir string) string {
	spawnCmd := m.cfg.Spawn.Command
	if spawnCmd == "" {
		spawnCmd = "claude --dangerously-skip-permissions"
	}
	return "cd " + shellQuoteSession(workDir) + " && " + spawnCmd
}

func (m *Model) syncWorktrees() tea.Cmd {
	sessMgr := m.sessions
	return func() tea.Msg {
		result, err := sessMgr.SyncWorktrees()
		if err != nil {
			return worktreesSyncedMsg{err: err}
		}
		// Re-discover repos after sync so the list stays current.
		repos, _ := sessMgr.DiscoverRepos()
		return worktreesSyncedMsg{added: result.Added, removed: result.Removed, repos: repos}
	}
}

func (m *Model) openInBrowser() tea.Cmd {
	switch m.activePanel {
	case PanelIssues:
		issue := m.selectedIssue()
		if issue == nil {
			return nil
		}
		issueRepo := issue.Repository.NameWithOwner
		if issueRepo == "" {
			return nil
		}
		number := issue.Number
		ghClient := m.gh
		return func() tea.Msg {
			return openBrowserMsg{err: ghClient.OpenInBrowser(issueRepo, number)}
		}
	case PanelSessions:
		sess := m.selectedSession()
		if sess == nil {
			return nil
		}
		pr, ok := m.prCache[prCacheKey(sess.Repo, sess.Branch)]
		if ok && pr != nil {
			repoName := sess.Repo
			number := pr.Number
			ghClient := m.gh
			return func() tea.Msg {
				return openBrowserMsg{err: ghClient.OpenInBrowser(repoName, number)}
			}
		}
	}
	return nil
}

func (m *Model) openSessionIssueBrowser() tea.Cmd {
	sess := m.selectedSession()
	if sess == nil {
		return nil
	}
	if sess.IssueNumber <= 0 {
		return nil
	}
	issueRepo := sess.IssueRepoName()
	if issueRepo == "" {
		return nil
	}
	number := sess.IssueNumber
	ghClient := m.gh
	return func() tea.Msg {
		return openBrowserMsg{err: ghClient.OpenInBrowser(issueRepo, number)}
	}
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

// shellQuoteSession wraps a string in single quotes for safe shell interpolation.
func shellQuoteSession(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func (m *Model) tickCmd() tea.Cmd {
	return tea.Tick(10*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
