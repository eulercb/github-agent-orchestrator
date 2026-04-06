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
	"github.com/eulercb/github-agent-orchestrator/internal/statusbar"
)

// Panel identifies which panel is focused.
type Panel int

// Panel constants.
const (
	PanelIssues Panel = iota
	PanelSessions
	PanelPRs
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
	cfgPath       string

	// State
	currentView   View
	activePanel   Panel
	issues        []github.Issue
	prList        []github.PullRequest
	prCache       map[string]*github.PullRequest // "repo:branch" -> PR
	issuesCursor  int
	sessionCursor int
	prCursor      int
	repoIndex     int
	statusBarText string
	errorMsg      string
	confirmMsg    string
	confirmAction func() tea.Msg
	loading       bool
	prFilter      string
	filterInput   textinput.Model
}

// NewModel creates the initial TUI model.
func NewModel(cfg *config.Config, ghClient *github.Client, sessMgr *claude.Manager) Model {
	// Build the status bar provider with the built-in fallback.
	// The command comes from config; refresh runs async via refreshStatusBar().
	sbCmd := cfg.StatusBar.Command
	if sbCmd == "" && cfg.CCUsage.Enabled && cfg.CCUsage.Command != "" {
		sbCmd = cfg.CCUsage.Command
	}

	ti := textinput.New()
	ti.Placeholder = config.DefaultSearch
	ti.CharLimit = 256

	// Pre-populate with the current search filter from config.
	if len(cfg.Repos) > 0 {
		ti.SetValue(cfg.Repos[0].Filters.Search)
	}

	cfgPath, _ := config.Path()

	return Model{
		cfg:         cfg,
		gh:          ghClient,
		sessions:    sessMgr,
		statusProv:  statusbar.NewProvider(sbCmd, nil),
		keys:        DefaultKeyMap(),
		prCache:     make(map[string]*github.PullRequest),
		filterInput: ti,
		cfgPath:     cfgPath,
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

type prListLoadedMsg struct {
	prs []github.PullRequest
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

type worktreesSyncedMsg struct {
	added   int
	removed int
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
	return tea.Batch(
		m.fetchIssues(),
		m.fetchPRList(),
		m.refreshStatuses(),
		m.syncWorktrees(),
		m.tickCmd(),
	)
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
		if msg.err != nil {
			m.errorMsg = fmt.Sprintf("PR refresh: %v", msg.err)
			// Merge successful lookups into existing cache
			for k, v := range msg.prs {
				m.prCache[k] = v
			}
		} else {
			m.prCache = msg.prs
			m.errorMsg = ""
		}
		return m, filterCmd
	case prListLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.errorMsg = fmt.Sprintf("PR list refresh: %v", msg.err)
		} else {
			m.prList = msg.prs
			m.errorMsg = ""
			lastIdx := len(m.prList) - 1
			if lastIdx < 0 {
				lastIdx = 0
			}
			if m.prCursor > lastIdx {
				m.prCursor = lastIdx
			}
		}
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
		if msg.added > 0 || msg.removed > 0 {
			var parts []string
			if msg.added > 0 {
				parts = append(parts, fmt.Sprintf("%d added", msg.added))
			}
			if msg.removed > 0 {
				parts = append(parts, fmt.Sprintf("%d removed", msg.removed))
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
		if m.activePanel == PanelPRs {
			cmds = append(cmds, m.fetchPRList())
		}
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
		switch m.activePanel {
		case PanelIssues:
			m.activePanel = PanelSessions
		case PanelSessions:
			m.activePanel = PanelPRs
		case PanelPRs:
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
	case key.Matches(msg, m.keys.ImportWorktrees):
		cmd := m.syncWorktrees()
		return m, cmd
	case key.Matches(msg, m.keys.Open):
		cmd := m.openInBrowser()
		return m, cmd
	case key.Matches(msg, m.keys.Delete):
		if m.activePanel == PanelSessions {
			m.killSession()
		}
	case key.Matches(msg, m.keys.ClearSession):
		if m.activePanel == PanelPRs {
			m.clearSessionForPR()
		}
	case key.Matches(msg, m.keys.Refresh):
		m.loading = true
		return m, tea.Batch(m.fetchIssues(), m.fetchPRList(), m.refreshStatuses())
	case key.Matches(msg, m.keys.Filter):
		if m.activePanel == PanelSessions {
			break
		}
		m.currentView = ViewFilter
		// Size the input to fit the bordered box (minus padding and borders).
		inputWidth := m.width - 10
		if inputWidth < 20 {
			inputWidth = 20
		}
		m.filterInput.Width = inputWidth
		// Populate the editor with the current filter for the active panel.
		switch m.activePanel {
		case PanelPRs:
			m.filterInput.SetValue(m.prFilter)
		default:
			if repo := m.currentRepo(); repo != nil {
				m.filterInput.SetValue(repo.Filters.Search)
			} else {
				m.filterInput.SetValue("")
			}
		}
		m.filterInput.Focus()
		return m, m.filterInput.Cursor.BlinkCmd()
	}
	return m, nil
}

// updateFilter handles all messages while the filter editor is active.
// Enter applies the filter, Esc cancels, window resizes update width,
// and everything else (including cursor blink) is forwarded to the
// textinput so it stays functional.
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
			if m.activePanel == PanelPRs {
				m.prFilter = query
				m.prCursor = 0
				cmd := m.fetchPRList()
				return m, cmd
			}
			repo := m.currentRepo()
			if repo != nil {
				repo.Filters.Search = query
			}
			m.issuesCursor = 0
			cmd := m.fetchIssues()
			return m, cmd
		case tea.KeyEsc:
			// Cancel: restore the previous value.
			m.currentView = ViewDashboard
			m.filterInput.Blur()
			if m.activePanel == PanelPRs {
				m.filterInput.SetValue(m.prFilter)
			} else if repo := m.currentRepo(); repo != nil {
				m.filterInput.SetValue(repo.Filters.Search)
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
	case PanelPRs:
		m.prCursor += delta
		if m.prCursor < 0 {
			m.prCursor = 0
		}
		lastIdx := len(m.prList) - 1
		if lastIdx < 0 {
			lastIdx = 0
		}
		if m.prCursor > lastIdx {
			m.prCursor = lastIdx
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

func (m *Model) selectedPR() *github.PullRequest {
	if m.prCursor >= 0 && m.prCursor < len(m.prList) {
		return &m.prList[m.prCursor]
	}
	return nil
}

func (m *Model) findSessionByRepoBranch(repo, branch string) *claude.Session {
	sessions := m.sessions.Sessions()
	for i := range sessions {
		if sessions[i].Repo == repo && sessions[i].Branch == branch {
			return &sessions[i]
		}
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
		issues, err := m.gh.ListIssues(repo.Filters.Search)
		return issuesLoadedMsg{issues: issues, err: err}
	}
}

func (m *Model) fetchPRList() tea.Cmd {
	search := m.prFilter
	var repos []string
	for i := range m.cfg.Repos {
		repos = append(repos, m.cfg.Repos[i].FullName())
	}
	return func() tea.Msg {
		if len(repos) == 0 {
			return prListLoadedMsg{err: fmt.Errorf("no repos configured")}
		}
		prs, err := m.gh.ListPRs(repos, search)
		return prListLoadedMsg{prs: prs, err: err}
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

	// Use the issue's own repo when available (search results carry it).
	issueRepo := issue.Repository.NameWithOwner
	if issueRepo == "" {
		issueRepo = repo.IssueRepoFullName()
	}

	// Check if session already exists for this issue
	existing := m.sessions.FindByIssue(issueRepo, issue.Number)
	if existing != nil {
		m.errorMsg = fmt.Sprintf("Session already exists for issue #%d", issue.Number)
		return nil
	}

	issueNum := issue.Number
	issueTitle := issue.Title
	repoCopy := *repo

	// Override issue source so the session records the issue's actual repo,
	// not the default config repo (matters for cross-repo search results).
	if issueRepo != repo.IssueRepoFullName() {
		parts := strings.SplitN(issueRepo, "/", 2)
		if len(parts) == 2 {
			repoCopy.IssueSource = &config.IssueSource{
				Owner: parts[0],
				Name:  parts[1],
			}
		}
	}

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
		return worktreesSyncedMsg{added: result.Added, removed: result.Removed}
	}
}

func (m *Model) openInBrowser() tea.Cmd {
	switch m.activePanel {
	case PanelIssues:
		issue := m.selectedIssue()
		if issue == nil {
			return nil
		}
		repo := issue.Repository.NameWithOwner
		if repo == "" {
			repo = m.currentRepo().IssueRepoFullName()
		}
		number := issue.Number
		ghClient := m.gh
		return func() tea.Msg {
			return openBrowserMsg{err: ghClient.OpenInBrowser(repo, number)}
		}
	case PanelSessions:
		sess := m.selectedSession()
		if sess == nil {
			return nil
		}
		pr, ok := m.prCache[prCacheKey(sess.Repo, sess.Branch)]
		if ok && pr != nil {
			repo := sess.Repo
			number := pr.Number
			ghClient := m.gh
			return func() tea.Msg {
				return openBrowserMsg{err: ghClient.OpenInBrowser(repo, number)}
			}
		}
	case PanelPRs:
		pr := m.selectedPR()
		if pr == nil {
			return nil
		}
		repoName := pr.Repository.NameWithOwner
		if repoName == "" {
			if repo := m.currentRepo(); repo != nil {
				repoName = repo.FullName()
			}
		}
		if repoName == "" {
			return nil
		}
		number := pr.Number
		ghClient := m.gh
		return func() tea.Msg {
			return openBrowserMsg{err: ghClient.OpenInBrowser(repoName, number)}
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

func (m *Model) clearSessionForPR() {
	pr := m.selectedPR()
	if pr == nil {
		return
	}
	repoName := pr.Repository.NameWithOwner
	if repoName == "" {
		if repo := m.currentRepo(); repo != nil {
			repoName = repo.FullName()
		}
	}

	sess := m.findSessionByRepoBranch(repoName, pr.HeadRef)
	if sess == nil {
		m.errorMsg = fmt.Sprintf("No session found for PR #%d", pr.Number)
		return
	}

	sessID := sess.ID
	m.confirmMsg = fmt.Sprintf("Clear session %q for PR #%d? (y/n)", sess.ID, pr.Number)
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

// shellQuoteSession wraps a string in single quotes for safe shell interpolation.
func shellQuoteSession(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func (m *Model) tickCmd() tea.Cmd {
	return tea.Tick(10*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
