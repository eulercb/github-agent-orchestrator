// Package claude manages Claude Code agent sessions.
package claude

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/eulercb/github-agent-orchestrator/internal/config"
	"github.com/eulercb/github-agent-orchestrator/internal/github"
	"github.com/eulercb/github-agent-orchestrator/internal/process"
	"github.com/eulercb/github-agent-orchestrator/internal/repo"
)

// Status represents the current state of a Claude Code session.
type Status string

// Session status constants.
const (
	StatusRunning Status = "working"
	StatusWaiting Status = "waiting"
	StatusDone    Status = "done"
	StatusStopped Status = "stopped"
)

// gitTimeout is the default timeout for git subprocesses.
const gitTimeout = 60 * time.Second

// Session tracks a Claude Code agent session.
type Session struct {
	ID           string    `yaml:"id"`
	IssueNumber  int       `yaml:"issue_number"`
	IssueTitle   string    `yaml:"issue_title"`
	Repo         string    `yaml:"repo"`
	IssueRepo    string    `yaml:"issue_repo,omitempty"`
	Branch       string    `yaml:"branch"`
	PID          int       `yaml:"pid"`
	LogFile      string    `yaml:"log_file"`
	WorktreePath string    `yaml:"worktree_path"`
	CreatedAt    time.Time `yaml:"created_at"`
	Status       Status    `yaml:"status"`
	LastActivity string    `yaml:"last_activity"`
}

// IssueRepoName returns the repo the issue was fetched from.
// Falls back to Repo when IssueRepo is empty.
func (s *Session) IssueRepoName() string {
	if s.IssueRepo != "" {
		return s.IssueRepo
	}
	return s.Repo
}

// Manager handles the lifecycle of Claude Code sessions.
type Manager struct {
	cfg       *config.Config
	gh        *github.Client
	sessions  []Session
	mu        sync.RWMutex
	stateFile string
}

// NewManager creates a session manager.
func NewManager(cfg *config.Config, gh *github.Client) (*Manager, error) {
	stateFile, err := config.SessionsPath(cfg.SessionDir)
	if err != nil {
		return nil, err
	}

	m := &Manager{
		cfg:       cfg,
		gh:        gh,
		stateFile: stateFile,
	}

	if err := m.loadState(); err != nil {
		return nil, err
	}

	return m, nil
}

// Sessions returns a copy of tracked sessions.
func (m *Manager) Sessions() []Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Session, len(m.sessions))
	copy(out, m.sessions)
	return out
}

// SpawnSession creates a new Claude Code session for an issue.
// The r parameter identifies the target repo for the session.
// issueRepo is the "owner/name" of the repo the issue belongs to.
func (m *Manager) SpawnSession(r *repo.Repo, issueRepo string, issueNumber int, issueTitle string) (*Session, error) {
	var sessionName, branch string
	if issueRepo != "" && issueRepo != r.FullName() {
		issueRepoID := strings.ReplaceAll(issueRepo, "/", "-")
		sessionName = fmt.Sprintf("gao-%s-%s-%s-%d", r.Owner, r.Name, issueRepoID, issueNumber)
		branch = fmt.Sprintf("claude/issue-%s-%d", issueRepoID, issueNumber)
	} else {
		sessionName = fmt.Sprintf("gao-%s-%s-%d", r.Owner, r.Name, issueNumber)
		branch = fmt.Sprintf("claude/issue-%d", issueNumber)
	}

	// Check if session already exists
	m.mu.RLock()
	for i := range m.sessions {
		if m.sessions[i].ID == sessionName {
			m.mu.RUnlock()
			return nil, fmt.Errorf("session %q already exists", sessionName)
		}
	}
	m.mu.RUnlock()

	repoDir := r.LocalPath

	// Set up git worktree or branch
	var workDir string
	if m.cfg.Spawn.UseWorktree {
		worktreePath := filepath.Join(repoDir, ".worktrees", branch)
		if err := m.setupWorktree(repoDir, worktreePath, branch); err != nil {
			return nil, fmt.Errorf("setup worktree: %w", err)
		}
		workDir = worktreePath
	} else {
		if err := m.setupBranch(repoDir, branch); err != nil {
			return nil, fmt.Errorf("setup branch: %w", err)
		}
		workDir = repoDir
	}

	// Persist issue metadata inside the worktree so that future imports
	// can associate the worktree with the issue without relying on branch names.
	if m.cfg.Spawn.UseWorktree {
		if writeErr := writeWorktreeMetadata(workDir, &worktreeMetadata{
			IssueNumber: issueNumber,
			IssueRepo:   issueRepo,
			IssueTitle:  issueTitle,
		}); writeErr != nil {
			return nil, fmt.Errorf("persist worktree metadata: %w", writeErr)
		}
	}

	// Determine log file path
	logDir, err := config.Dir()
	if err != nil {
		return nil, fmt.Errorf("determine log directory: %w", err)
	}
	logFile := filepath.Join(logDir, "logs", sessionName+".log")

	// Parse spawn command
	spawnCmd := m.cfg.Spawn.Command
	if spawnCmd == "" {
		spawnCmd = "claude --dangerously-skip-permissions"
	}

	// Start the process in the background
	pid, err := process.StartBackground(workDir, logFile, "sh", "-c", spawnCmd)
	if err != nil {
		return nil, fmt.Errorf("start claude process: %w", err)
	}

	sess := Session{
		ID:           sessionName,
		IssueNumber:  issueNumber,
		IssueTitle:   issueTitle,
		Repo:         r.FullName(),
		IssueRepo:    issueRepo,
		Branch:       branch,
		PID:          pid,
		LogFile:      logFile,
		WorktreePath: workDir,
		CreatedAt:    time.Now(),
		Status:       StatusRunning,
	}

	m.mu.Lock()
	m.sessions = append(m.sessions, sess)
	m.mu.Unlock()

	if err := m.saveState(); err != nil {
		// Rollback: remove the session from in-memory state and kill the process
		m.mu.Lock()
		for i := len(m.sessions) - 1; i >= 0; i-- {
			if m.sessions[i].ID == sess.ID {
				m.sessions = append(m.sessions[:i], m.sessions[i+1:]...)
				break
			}
		}
		m.mu.Unlock()
		if killErr := process.Kill(pid); killErr != nil {
			return nil, fmt.Errorf("save state: %w; cleanup process %d: %v", err, pid, killErr)
		}
		return nil, fmt.Errorf("save state: %w", err)
	}

	return &sess, nil
}

// RefreshStatuses updates the status of all sessions by checking processes.
func (m *Manager) RefreshStatuses() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.sessions {
		s := &m.sessions[i]

		if !process.IsRunning(s.PID) {
			if s.Status == StatusRunning || s.Status == StatusWaiting {
				s.Status = StatusDone
			} else if s.Status != StatusDone {
				s.Status = StatusStopped
			}
			continue
		}

		s.Status = StatusRunning

		// Try to detect waiting state from log output
		if s.LogFile != "" {
			output, err := process.ReadLastLines(s.LogFile, 5)
			if err == nil && output != "" {
				s.LastActivity = extractLastActivity(output)
				if isWaitingForInput(output) {
					s.Status = StatusWaiting
				}
			}
		}
	}
}

// BackfillIssueTitles populates empty IssueTitle fields by looking up
// linked issues via the GitHub API. Sessions whose titles are already
// set or that have no branch are skipped.
func (m *Manager) BackfillIssueTitles() {
	if m.gh == nil {
		return
	}

	m.mu.RLock()
	type backfillEntry struct {
		id     string
		repo   string
		branch string
	}
	var entries []backfillEntry
	for i := range m.sessions {
		s := &m.sessions[i]
		if s.IssueTitle == "" && s.Branch != "" && s.Repo != "" {
			entries = append(entries, backfillEntry{id: s.ID, repo: s.Repo, branch: s.Branch})
		}
	}
	m.mu.RUnlock()

	if len(entries) == 0 {
		return
	}

	// Resolve titles outside the lock (makes network calls).
	type resolved struct {
		id    string
		title string
	}
	var results []resolved
	for _, e := range entries {
		linked, err := m.gh.FindLinkedIssue(e.repo, e.branch)
		if err != nil || linked == nil || linked.Title == "" {
			continue
		}
		results = append(results, resolved{id: e.id, title: linked.Title})
	}

	if len(results) == 0 {
		return
	}

	m.mu.Lock()
	for _, r := range results {
		for i := range m.sessions {
			if m.sessions[i].ID == r.id && m.sessions[i].IssueTitle == "" {
				m.sessions[i].IssueTitle = r.title
				break
			}
		}
	}
	m.mu.Unlock()

	_ = m.saveState()
}

// RemoveSession removes a session from tracking and optionally kills the process.
func (m *Manager) RemoveSession(id string, killProcess bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := -1
	for i := range m.sessions {
		if m.sessions[i].ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("session %q not found", id)
	}

	if killProcess && process.IsRunning(m.sessions[idx].PID) {
		if err := process.Kill(m.sessions[idx].PID); err != nil {
			return fmt.Errorf("kill process for session %q: %w", id, err)
		}
	}

	m.sessions = append(m.sessions[:idx], m.sessions[idx+1:]...)
	return m.saveStateLocked()
}

// FindByIssue finds a session for a specific issue.
func (m *Manager) FindByIssue(issueRepo string, issueNumber int) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for i := range m.sessions {
		if m.sessions[i].IssueRepoName() == issueRepo && m.sessions[i].IssueNumber == issueNumber {
			sess := m.sessions[i]
			return &sess
		}
	}
	return nil
}

// Worktree represents a git worktree discovered on disk.
type Worktree struct {
	Path   string // Absolute path to the worktree
	Branch string // Branch checked out in the worktree
}

// worktreeMetadata is persisted in each worktree's .claude/gao.local.yaml
// to reliably associate the worktree with an issue, independent of branch
// naming conventions.
type worktreeMetadata struct {
	IssueNumber int    `yaml:"issue_number"`
	IssueRepo   string `yaml:"issue_repo,omitempty"`
	IssueTitle  string `yaml:"issue_title,omitempty"`
}

// metadataFile is the relative path to the gao metadata file inside a worktree.
const metadataFile = ".claude/gao.local.yaml"

// writeWorktreeMetadata persists issue metadata into the worktree so that
// future imports can read it without relying on branch naming conventions.
func writeWorktreeMetadata(worktreePath string, meta *worktreeMetadata) error {
	metaPath := filepath.Join(worktreePath, metadataFile)
	if err := os.MkdirAll(filepath.Dir(metaPath), 0o750); err != nil {
		return fmt.Errorf("create metadata directory: %w", err)
	}
	data, err := yaml.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal worktree metadata: %w", err)
	}
	if err := os.WriteFile(metaPath, data, 0o600); err != nil {
		return fmt.Errorf("write worktree metadata: %w", err)
	}
	return nil
}

// readWorktreeMetadata reads the gao metadata file from a worktree.
// Returns nil (no error) when the file does not exist.
func readWorktreeMetadata(worktreePath string) (*worktreeMetadata, error) {
	metaPath := filepath.Join(worktreePath, metadataFile)
	data, err := os.ReadFile(metaPath) //nolint:gosec // path derived from worktree
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read worktree metadata: %w", err)
	}
	var meta worktreeMetadata
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse worktree metadata: %w", err)
	}
	return &meta, nil
}

// SyncResult summarises the changes made by SyncWorktrees.
type SyncResult struct {
	Added   int
	Removed int
}

// SyncWorktrees discovers worktrees across all repos in repos_dir, registers
// new ones as sessions, and removes sessions whose worktree no longer exists.
func (m *Manager) SyncWorktrees() (*SyncResult, error) {
	repos, err := m.discoverRepos()
	if err != nil {
		return nil, err
	}

	type repoWorktree struct {
		r  *repo.Repo
		wt Worktree
	}

	var discovered []repoWorktree
	scannedRepos := make(map[string]bool)

	for i := range repos {
		r := &repos[i]
		out, gitErr := gitRun(r.LocalPath, "worktree", "list", "--porcelain")
		if gitErr != nil {
			continue
		}

		scannedRepos[r.FullName()] = true
		worktrees := parseWorktreeList(out)

		repoDirAbs, absErr := filepath.Abs(r.LocalPath)
		if absErr != nil {
			continue
		}

		for _, wt := range worktrees {
			wtAbs, wtErr := filepath.Abs(wt.Path)
			if wtErr != nil {
				continue
			}
			// Skip the main worktree (repo root itself).
			if wtAbs == repoDirAbs {
				continue
			}
			discovered = append(discovered, repoWorktree{r: r, wt: wt})
		}
	}

	// Build set of discovered worktree paths.
	discoveredPaths := make(map[string]bool, len(discovered))
	for _, d := range discovered {
		if abs, err := filepath.Abs(d.wt.Path); err == nil {
			discoveredPaths[abs] = true
		}
	}

	// Find which discovered worktrees are not yet tracked.
	m.mu.RLock()
	trackedPaths := make(map[string]bool, len(m.sessions))
	for i := range m.sessions {
		if p := m.sessions[i].WorktreePath; p != "" {
			if abs, err := filepath.Abs(p); err == nil {
				trackedPaths[abs] = true
			}
		}
	}
	m.mu.RUnlock()

	var newEntries []repoWorktree
	for _, d := range discovered {
		abs, absErr := filepath.Abs(d.wt.Path)
		if absErr != nil {
			continue
		}
		if !trackedPaths[abs] {
			newEntries = append(newEntries, d)
		}
	}

	// Resolve issues for new worktrees outside the lock (may call GitHub API).
	var newSessions []Session
	for _, d := range newEntries {
		issueNumber, issueRepo, issueTitle, resolveErr := m.resolveWorktreeIssue(d.r, &d.wt)
		if resolveErr != nil {
			continue
		}
		name := buildSessionName(d.r, &d.wt, issueNumber, issueRepo)
		newSessions = append(newSessions, Session{
			ID:           name,
			IssueNumber:  issueNumber,
			IssueTitle:   issueTitle,
			Repo:         d.r.FullName(),
			IssueRepo:    issueRepo,
			Branch:       d.wt.Branch,
			WorktreePath: d.wt.Path,
			CreatedAt:    time.Now(),
			Status:       StatusStopped,
		})
	}

	// Under a single lock: add new sessions, prune stale ones, save.
	m.mu.Lock()

	// Remove sessions whose worktree is gone (only for repos we scanned).
	var kept []Session
	var removed int
	for i := range m.sessions {
		s := &m.sessions[i]
		if s.WorktreePath == "" || !scannedRepos[s.Repo] {
			kept = append(kept, *s)
			continue
		}
		abs, absErr := filepath.Abs(s.WorktreePath)
		if absErr != nil || discoveredPaths[abs] {
			kept = append(kept, *s)
			continue
		}
		removed++
	}

	// Add new sessions, skipping ID collisions.
	existingIDs := make(map[string]bool, len(kept))
	for i := range kept {
		existingIDs[kept[i].ID] = true
	}
	var added int
	for i := range newSessions {
		if existingIDs[newSessions[i].ID] {
			continue
		}
		kept = append(kept, newSessions[i])
		existingIDs[newSessions[i].ID] = true
		added++
	}

	m.sessions = kept
	m.mu.Unlock()

	if err := m.saveState(); err != nil {
		return nil, fmt.Errorf("save state: %w", err)
	}

	return &SyncResult{Added: added, Removed: removed}, nil
}

// discoverRepos returns the list of repos found under repos_dir.
func (m *Manager) discoverRepos() ([]repo.Repo, error) {
	reposDir, err := m.cfg.ExpandReposDir()
	if err != nil {
		return nil, err
	}
	return repo.Discover(reposDir)
}

// DiscoverRepos exposes repo discovery for use by the TUI.
func (m *Manager) DiscoverRepos() ([]repo.Repo, error) {
	return m.discoverRepos()
}

// parseWorktreeList parses the porcelain output of `git worktree list --porcelain`.
func parseWorktreeList(output string) []Worktree {
	var worktrees []Worktree
	var current Worktree

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "worktree "):
			current = Worktree{Path: strings.TrimPrefix(line, "worktree ")}
		case strings.HasPrefix(line, "branch refs/heads/"):
			current.Branch = strings.TrimPrefix(line, "branch refs/heads/")
		case line == "":
			if current.Path != "" {
				worktrees = append(worktrees, current)
				current = Worktree{}
			}
		}
	}
	// Handle last entry if output doesn't end with blank line
	if current.Path != "" {
		worktrees = append(worktrees, current)
	}

	return worktrees
}

// resolveWorktreeIssue determines the issue number, repo, and title for a worktree.
func (m *Manager) resolveWorktreeIssue(r *repo.Repo, wt *Worktree) (issueNumber int, issueRepo, issueTitle string, err error) {
	meta, metaErr := readWorktreeMetadata(wt.Path)
	if metaErr != nil {
		return 0, "", "", metaErr
	}
	if meta != nil && meta.IssueNumber > 0 {
		// Backfill IssueTitle from GitHub when metadata was written by an
		// older version that didn't persist the title.
		if meta.IssueTitle == "" && m.gh != nil && wt.Branch != "" {
			linked, ghErr := m.gh.FindLinkedIssue(r.FullName(), wt.Branch)
			if ghErr == nil && linked != nil && linked.Title != "" {
				meta.IssueTitle = linked.Title
				_ = writeWorktreeMetadata(wt.Path, meta)
			}
		}
		return meta.IssueNumber, meta.IssueRepo, meta.IssueTitle, nil
	}

	if m.gh != nil && wt.Branch != "" {
		linked, ghErr := m.gh.FindLinkedIssue(r.FullName(), wt.Branch)
		if ghErr == nil && linked != nil {
			issueRepo = linked.Repository
			_ = writeWorktreeMetadata(wt.Path, &worktreeMetadata{
				IssueNumber: linked.Number,
				IssueRepo:   issueRepo,
				IssueTitle:  linked.Title,
			})
			return linked.Number, issueRepo, linked.Title, nil
		}
	}

	return 0, "", "", nil
}

// buildSessionName generates a human-readable session ID.
func buildSessionName(r *repo.Repo, wt *Worktree, issueNumber int, issueRepo string) string {
	switch {
	case issueNumber > 0 && issueRepo != "" && issueRepo != r.FullName():
		issueRepoID := strings.ReplaceAll(issueRepo, "/", "-")
		return fmt.Sprintf("gao-%s-%s-%s-%d", r.Owner, r.Name, issueRepoID, issueNumber)
	case issueNumber > 0:
		return fmt.Sprintf("gao-%s-%s-%d", r.Owner, r.Name, issueNumber)
	default:
		safe := strings.NewReplacer("/", "-", ".", "-").Replace(wt.Branch)
		if safe == "" {
			safe = filepath.Base(wt.Path)
		}
		return fmt.Sprintf("gao-%s-%s-%s", r.Owner, r.Name, safe)
	}
}

func (m *Manager) setupWorktree(repoDir, worktreePath, branch string) error {
	// git fetch origin
	if out, err := gitRun(repoDir, "fetch", "origin"); err != nil {
		return fmt.Errorf("git fetch: %s (%w)", out, err)
	}

	// Determine base branch
	baseBranch := m.cfg.Spawn.BaseBranch
	if baseBranch == "" {
		out, err := gitRun(repoDir, "symbolic-ref", "refs/remotes/origin/HEAD")
		if err != nil {
			baseBranch = "main"
		} else {
			baseBranch = strings.TrimPrefix(out, "refs/remotes/origin/")
		}
	}

	// Create worktree parent directory
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o750); err != nil {
		return fmt.Errorf("create worktree parent: %w", err)
	}

	// git worktree add
	if out, err := gitRun(repoDir, "worktree", "add", worktreePath, "-b", branch, "origin/"+baseBranch); err != nil {
		return fmt.Errorf("git worktree add: %s (%w)", out, err)
	}

	return nil
}

func (m *Manager) setupBranch(repoDir, branch string) error {
	// Try checking out existing branch first.
	if out, err := gitRun(repoDir, "checkout", branch); err != nil {
		// Only create the branch if it truly does not exist locally.
		if _, verifyErr := gitRun(repoDir, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); verifyErr != nil {
			// Branch does not exist — create it.
			if createOut, createErr := gitRun(repoDir, "checkout", "-b", branch); createErr != nil {
				return fmt.Errorf("git checkout -b %s: %s (%w)", branch, createOut, createErr)
			}
			return nil
		}

		// Branch exists but checkout failed for another reason (e.g. uncommitted changes).
		return fmt.Errorf("git checkout %s: %s (%w)", branch, out, err)
	}

	return nil
}

// gitRun executes a git command with a per-command timeout so that a slow
// fetch doesn't consume the budget for subsequent fast commands.
func gitRun(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (m *Manager) loadState() error {
	data, err := os.ReadFile(m.stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read sessions state: %w", err)
	}
	if err := yaml.Unmarshal(data, &m.sessions); err != nil {
		return fmt.Errorf("unmarshal sessions state: %w", err)
	}
	return nil
}

func (m *Manager) saveState() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.saveStateLocked()
}

func (m *Manager) saveStateLocked() error {
	dir := filepath.Dir(m.stateFile)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create state directory %q: %w", dir, err)
	}
	data, err := yaml.Marshal(m.sessions)
	if err != nil {
		return fmt.Errorf("marshal sessions state: %w", err)
	}
	if err := os.WriteFile(m.stateFile, data, 0o600); err != nil {
		return fmt.Errorf("write sessions state to %q: %w", m.stateFile, err)
	}
	return nil
}

func extractLastActivity(output string) string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			runes := []rune(line)
			if len(runes) > 80 {
				return string(runes[:80]) + "..."
			}
			return line
		}
	}
	return ""
}

func isWaitingForInput(output string) bool {
	lower := strings.ToLower(output)
	lines := strings.Split(strings.TrimSpace(lower), "\n")
	if len(lines) == 0 {
		return false
	}
	lastLine := strings.TrimSpace(lines[len(lines)-1])

	containsIndicators := []string{
		"waiting for your",
		"what would you like",
		"claude >",
		"> ",
		"? ",
	}
	for _, indicator := range containsIndicators {
		if strings.Contains(lastLine, indicator) {
			return true
		}
	}

	// Also check if line ends with a question mark (prompt)
	if strings.HasSuffix(lastLine, "?") {
		return true
	}

	return false
}
