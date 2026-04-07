// Package tui implements the Bubble Tea terminal UI for gao.
package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/eulercb/github-agent-orchestrator/internal/claude"
	"github.com/eulercb/github-agent-orchestrator/internal/config"
	"github.com/eulercb/github-agent-orchestrator/internal/debug"
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
	fetchingPRs   bool
	issuesCursor  int
	sessionCursor int
	statusBarText string
	errorMsg      string
	scanning      bool
	confirmMsg    string
	confirmAction func() tea.Msg
	loading       bool
	issueFilter   string
	filterInput   textinput.Model

	// Issues pane visibility
	showIssues        bool
	issuesInitialized bool
	filterForToggle   bool // true when filter editor was opened by toggle

	// Debug pane
	debugLog  *debug.Log
	showDebug bool
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
	dbgLog := debug.NewLog()

	return Model{
		cfg:               cfg,
		gh:                ghClient,
		sessions:          sessMgr,
		statusProv:        statusbar.NewProvider(sbCmd, nil),
		keys:              DefaultKeyMap(),
		prCache:           make(map[string]*github.PullRequest),
		repos:             repos,
		errorMsg:          initErr,
		scanning:          true, // Init() triggers syncWorktrees
		issueFilter:       issueFilter,
		filterInput:       ti,
		cfgPath:           cfgPath,
		showIssues:        showIssues,
		issuesInitialized: showIssues,
		activePanel:       panelForStart(showIssues),
		debugLog:          dbgLog,
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
	err error
}

type statusRefreshMsg struct{}

type titlesBackfilledMsg struct {
	err error
}

type statusBarUpdatedMsg struct {
	text string
}

type worktreeRemovedMsg struct {
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
	m.debugLog.Infof("gao started — repos_dir: %s, %d repos discovered", m.cfg.ReposDir, len(m.repos))
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
		if msg.err != nil {
			m.errorMsg = fmt.Sprintf("Failed to load issues: %v", msg.err)
		} else {
			m.issues = msg.issues
			m.errorMsg = ""
			m.issuesInitialized = true
			m.debugLog.Infof("Issues loaded: %d results", len(msg.issues))
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
		m.fetchingPRs = false
		m.loading = false
		if msg.err != nil {
			// Preserve previously known PRs when some lookups fail,
			// and merge in the successfully refreshed entries.
			if m.prCache == nil {
				m.prCache = make(map[string]*github.PullRequest)
			}
			for k, v := range msg.prs {
				m.prCache[k] = v
			}
			m.errorMsg = fmt.Sprintf("Some PR lookups failed: %v", msg.err)
		} else {
			// Replace the cache so stale/deleted PRs are cleared.
			m.prCache = msg.prs
			if m.prCache == nil {
				m.prCache = make(map[string]*github.PullRequest)
			}
			m.errorMsg = ""
		}
		return m, filterCmd
	case statusRefreshMsg:
		m.sessions.RefreshStatuses()
		cmd := m.refreshStatusBar()
		return m, tea.Batch(filterCmd, cmd)
	case statusBarUpdatedMsg:
		m.statusBarText = msg.text
		return m, filterCmd
	case worktreeRemovedMsg:
		if msg.err != nil {
			m.errorMsg = fmt.Sprintf("Remove worktree failed: %v", msg.err)
		} else {
			m.errorMsg = ""
			m.debugLog.Infof("Worktree removed: %s", msg.id)
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
		m.scanning = false
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
		} else {
			m.errorMsg = ""
		}
		// Always refresh PRs and backfill missing issue titles after sync.
		return m, tea.Batch(filterCmd, m.fetchPRs(), m.backfillTitles())
	case titlesBackfilledMsg:
		if msg.err != nil {
			m.errorMsg = fmt.Sprintf("Title backfill: %v", msg.err)
		}
		return m, filterCmd
	case openBrowserMsg:
		if msg.err != nil {
			m.errorMsg = fmt.Sprintf("Browser open failed: %v", msg.err)
		}
		return m, filterCmd
	case tickMsg:
		m.debugLog.Info("Tick: refreshing statuses and PRs")
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
		if key.Matches(msg, m.keys.Quit) {
			m.gh.Close()
			return m, tea.Quit
		}
		if key.Matches(msg, m.keys.Back) || key.Matches(msg, m.keys.Help) {
			m.currentView = ViewDashboard
		}
		return m, nil
	}

	// Dashboard keys
	switch {
	case key.Matches(msg, m.keys.Quit):
		m.gh.Close()
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
	case key.Matches(msg, m.keys.Worktree):
		if m.activePanel == PanelSessions {
			cmd := m.openWorktree()
			return m, cmd
		}
	case key.Matches(msg, m.keys.ImportWorktrees):
		if m.scanning {
			return m, nil
		}
		m.scanning = true
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
	case key.Matches(msg, m.keys.RemoveWorktree):
		if m.activePanel == PanelSessions {
			m.removeWorktree()
		}
	case key.Matches(msg, m.keys.Refresh):
		m.loading = true
		m.gh.InvalidatePRCache()
		m.debugLog.Info("Manual refresh triggered (PR cache invalidated)")
		cmds := []tea.Cmd{m.refreshStatuses()}
		if m.showIssues {
			// issuesLoadedMsg triggers fetchPRs after issues load.
			cmds = append(cmds, m.fetchIssues())
		} else {
			cmds = append(cmds, m.fetchPRs())
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
	case key.Matches(msg, m.keys.ToggleDebug):
		m.showDebug = !m.showDebug
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
			m.gh.Close()
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
			m.debugLog.Infof("Issue filter changed: %s", query)
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

// Commands

func (m *Model) fetchIssues() tea.Cmd {
	search := m.issueFilter
	dbg := m.debugLog
	return func() tea.Msg {
		id := dbg.Start("Fetching issues: " + search)
		issues, err := m.gh.ListIssues(search)
		if err != nil {
			dbg.Error(id, err)
		} else {
			dbg.Finish(id, fmt.Sprintf("%d issues loaded", len(issues)))
		}
		return issuesLoadedMsg{issues: issues, err: err}
	}
}

func (m *Model) fetchPRs() tea.Cmd {
	if m.fetchingPRs {
		m.debugLog.Info("Skipping PR fetch: previous fetch still in progress")
		return nil
	}
	m.fetchingPRs = true
	dbg := m.debugLog
	return func() tea.Msg {
		sessions := m.sessions.Sessions()

		// Collect sessions that need PR lookups.
		type lookup struct {
			repoName string
			branch   string
		}
		var lookups []lookup
		for i := range sessions {
			if sessions[i].Branch != "" {
				lookups = append(lookups, lookup{
					repoName: sessions[i].Repo,
					branch:   sessions[i].Branch,
				})
			}
		}
		id := dbg.Start(fmt.Sprintf("Fetching PRs for %d sessions", len(lookups)))

		// Look up PRs concurrently.
		type prResult struct {
			key string
			pr  *github.PullRequest
			err error
		}
		// Bound concurrency to avoid spawning too many gh subprocesses.
		const maxParallelPR = 8
		results := make([]prResult, len(lookups))
		sem := make(chan struct{}, maxParallelPR)
		var wg sync.WaitGroup
		wg.Add(len(lookups))
		for i, l := range lookups {
			go func(idx int, repoName, branch string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				pr, err := m.gh.FindPRForBranch(repoName, branch)
				if err != nil {
					err = fmt.Errorf("%s@%s: %w", repoName, branch, err)
				}
				results[idx] = prResult{
					key: prCacheKey(repoName, branch),
					pr:  pr,
					err: err,
				}
			}(i, l.repoName, l.branch)
		}
		wg.Wait()

		prs := make(map[string]*github.PullRequest, len(lookups))
		var firstErr error
		for _, r := range results {
			if r.err != nil {
				if firstErr == nil {
					firstErr = r.err
				}
				continue
			}
			prs[r.key] = r.pr
		}
		found := 0
		for _, pr := range prs {
			if pr != nil {
				found++
			}
		}
		if firstErr != nil {
			dbg.Error(id, firstErr)
		} else {
			dbg.Finish(id, fmt.Sprintf("%d PRs found", found))
		}
		return prsLoadedMsg{prs: prs, err: firstErr}
	}
}

func (m *Model) refreshStatuses() tea.Cmd {
	dbg := m.debugLog
	return func() tea.Msg {
		dbg.Info("Refreshing session statuses")
		return statusRefreshMsg{}
	}
}

func (m *Model) backfillTitles() tea.Cmd {
	sessMgr := m.sessions
	dbg := m.debugLog
	return func() tea.Msg {
		id := dbg.Start("Backfilling issue titles")
		err := sessMgr.BackfillIssueTitles()
		if err != nil {
			dbg.Error(id, err)
		} else {
			dbg.Finish(id, "")
		}
		return titlesBackfilledMsg{err: err}
	}
}

// openWorktree opens a terminal window/tab at the session's worktree directory.
func (m *Model) openWorktree() tea.Cmd {
	sess := m.selectedSession()
	if sess == nil {
		return nil
	}

	workDir := sess.WorktreePath
	dbg := m.debugLog
	sessID := sess.ID

	// Use custom command if configured.
	if tpl := m.cfg.Worktree.OpenCommand; tpl != "" {
		return m.openWorktreeCommand(sessID, workDir, tpl)
	}

	// Auto-detect: tmux → Warp → interactive shell.
	if os.Getenv("TMUX") != "" {
		if _, err := exec.LookPath("tmux"); err == nil {
			return m.openWorktreeTmux(sessID, workDir)
		}
	}
	if os.Getenv("TERM_PROGRAM") == "WarpTerminal" {
		if _, err := exec.LookPath("open"); err == nil {
			return m.openWorktreeWarp(sessID, workDir)
		}
	}

	// Fallback: open a shell interactively (suspends TUI).
	dbg.Info(fmt.Sprintf("Opening worktree for %s interactively", sessID))
	return tea.ExecProcess(
		exec.CommandContext(context.Background(),
			"sh", "-c", "cd "+shellQuoteSession(workDir)+" && exec \"${SHELL:-sh}\" -l"),
		func(err error) tea.Msg {
			if err != nil {
				return errMsg{err: fmt.Errorf("open worktree: %w", err)}
			}
			return statusRefreshMsg{}
		},
	)
}

func (m *Model) openWorktreeTmux(sessID, workDir string) tea.Cmd {
	dbg := m.debugLog
	return func() tea.Msg {
		id := dbg.Start(fmt.Sprintf("Opening worktree for %s via tmux", sessID))
		cmd := exec.CommandContext(context.Background(),
			"tmux", "new-window", "-n", sessID, "-c", workDir)
		if err := cmd.Run(); err != nil {
			dbg.Error(id, err)
			return errMsg{err: fmt.Errorf("tmux open worktree: %w", err)}
		}
		dbg.Finish(id, "")
		return nil
	}
}

func (m *Model) openWorktreeWarp(sessID, workDir string) tea.Cmd {
	dbg := m.debugLog
	return func() tea.Msg {
		id := dbg.Start(fmt.Sprintf("Opening worktree for %s via Warp", sessID))
		cmd := exec.CommandContext(context.Background(), "open", "-a", "Warp", workDir)
		if err := cmd.Run(); err != nil {
			dbg.Error(id, err)
			return errMsg{err: fmt.Errorf("warp open worktree: %w", err)}
		}
		dbg.Finish(id, "")
		return nil
	}
}

func (m *Model) openWorktreeCommand(sessID, workDir, tpl string) tea.Cmd {
	dbg := m.debugLog
	if !strings.Contains(tpl, "{path}") {
		return func() tea.Msg {
			return errMsg{err: fmt.Errorf("worktree open_command is missing '{path}' placeholder")}
		}
	}
	fullCmd := strings.ReplaceAll(tpl, "{path}", shellQuoteSession(workDir))
	return func() tea.Msg {
		id := dbg.Start(fmt.Sprintf("Opening worktree for %s via custom command", sessID))
		cmd := exec.CommandContext(context.Background(), "sh", "-c", fullCmd)
		if err := cmd.Run(); err != nil {
			dbg.Error(id, err)
			return errMsg{err: fmt.Errorf("open worktree: %w", err)}
		}
		dbg.Finish(id, "")
		return nil
	}
}

func (m *Model) syncWorktrees() tea.Cmd {
	sessMgr := m.sessions
	dbg := m.debugLog
	return func() tea.Msg {
		id := dbg.Start("Scanning worktrees")
		result, err := sessMgr.SyncWorktrees()
		if err != nil {
			dbg.Error(id, err)
			return worktreesSyncedMsg{err: err}
		}
		dbg.Finish(id, fmt.Sprintf("%d added, %d removed", result.Added, result.Removed))
		// Re-discover repos after sync so the list stays current.
		repos, _ := sessMgr.DiscoverRepos()
		return worktreesSyncedMsg{added: result.Added, removed: result.Removed, repos: repos}
	}
}

func (m *Model) openInBrowser() tea.Cmd {
	dbg := m.debugLog
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
			dbg.Infof("Opening issue #%d in browser", number)
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
				dbg.Infof("Opening PR #%d in browser", number)
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
	dbg := m.debugLog
	return func() tea.Msg {
		dbg.Infof("Opening issue #%d in browser", number)
		return openBrowserMsg{err: ghClient.OpenInBrowser(issueRepo, number)}
	}
}

func (m *Model) removeWorktree() {
	sess := m.selectedSession()
	if sess == nil {
		return
	}

	sessID := sess.ID
	dbg := m.debugLog
	m.confirmMsg = fmt.Sprintf("Force-remove worktree for %q? This may discard uncommitted changes. (y/n)", sess.ID)
	m.currentView = ViewConfirm
	m.confirmAction = func() tea.Msg {
		id := dbg.Start(fmt.Sprintf("Removing worktree %s", sessID))
		err := m.sessions.RemoveWorktree(sessID)
		if err != nil {
			dbg.Error(id, err)
		} else {
			dbg.Finish(id, "removed")
		}
		return worktreeRemovedMsg{id: sessID, err: err}
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

		// Built-in fallback: session count
		return statusBarUpdatedMsg{text: fmt.Sprintf("Sessions: %d", len(sessions))}
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
