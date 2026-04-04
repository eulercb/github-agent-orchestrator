// Package claude manages Claude Code agent sessions.
package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/eulercb/github-agent-orchestrator/internal/config"
	"github.com/eulercb/github-agent-orchestrator/internal/tmux"
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

// Session tracks a Claude Code agent session.
type Session struct {
	ID           string    `yaml:"id"`
	IssueNumber  int       `yaml:"issue_number"`
	IssueTitle   string    `yaml:"issue_title"`
	Repo         string    `yaml:"repo"`
	IssueRepo    string    `yaml:"issue_repo,omitempty"`
	Branch       string    `yaml:"branch"`
	TmuxSession  string    `yaml:"tmux_session"`
	WorktreePath string    `yaml:"worktree_path"`
	CreatedAt    time.Time `yaml:"created_at"`
	Status       Status    `yaml:"status"`
	LastActivity string    `yaml:"last_activity"`
}

// IssueRepoName returns the repo the issue was fetched from.
// Falls back to Repo for backward compatibility with older sessions.
func (s *Session) IssueRepoName() string {
	if s.IssueRepo != "" {
		return s.IssueRepo
	}
	return s.Repo
}

// Manager handles the lifecycle of Claude Code sessions.
type Manager struct {
	cfg       *config.Config
	tmux      *tmux.Client
	sessions  []Session
	mu        sync.RWMutex
	stateFile string
}

// NewManager creates a session manager.
func NewManager(cfg *config.Config, tmuxClient *tmux.Client) (*Manager, error) {
	stateFile, err := config.SessionsPath(cfg.SessionDir)
	if err != nil {
		return nil, err
	}

	m := &Manager{
		cfg:       cfg,
		tmux:      tmuxClient,
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
func (m *Manager) SpawnSession(repo *config.RepoConfig, issueNumber int, issueTitle string) (*Session, error) {
	sessionName := fmt.Sprintf("gao-%s-%s-%d", repo.Owner, repo.Name, issueNumber)
	branch := fmt.Sprintf("claude/issue-%d", issueNumber)

	// Check if session already exists
	if m.tmux.SessionExists(sessionName) {
		return nil, fmt.Errorf("session %q already exists", sessionName)
	}

	repoDir := m.cfg.Spawn.RepoDir
	if repoDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("determine user home directory: %w", err)
		}
		repoDir = filepath.Join(home, repo.Name)
	}

	// Build the command to run inside tmux
	spawnCmd := m.cfg.Spawn.Command
	if spawnCmd == "" {
		spawnCmd = "claude --dangerously-skip-permissions"
	}

	// Determine base branch for worktree
	baseBranch := m.cfg.Spawn.BaseBranch
	if baseBranch == "" {
		// Use symbolic ref to derive the default branch
		baseBranch = "$(git symbolic-ref refs/remotes/origin/HEAD 2>/dev/null | sed 's@^refs/remotes/origin/@@' || echo main)"
	}

	// Compose the full command: optionally create worktree, then run claude
	var fullCmd string
	var worktreePath string
	if m.cfg.Spawn.UseWorktree {
		worktreePath = filepath.Join(repoDir, ".worktrees", branch)
		worktreeParent := filepath.Dir(worktreePath)

		// Quote the base branch ref when it comes from config (not the $() auto-detect)
		originRef := "origin/" + baseBranch
		if m.cfg.Spawn.BaseBranch != "" {
			originRef = shellQuote("origin/" + baseBranch)
		}

		fullCmd = fmt.Sprintf(
			"cd %s && git fetch origin && mkdir -p %s && git worktree add %s -b %s %s && cd %s && %s",
			shellQuote(repoDir),
			shellQuote(worktreeParent),
			shellQuote(worktreePath),
			shellQuote(branch),
			originRef,
			shellQuote(worktreePath),
			spawnCmd,
		)
	} else {
		fullCmd = fmt.Sprintf("cd %s && (git checkout %s || git checkout -b %s) && %s",
			shellQuote(repoDir),
			shellQuote(branch),
			shellQuote(branch),
			spawnCmd,
		)
	}

	if err := m.tmux.NewSession(sessionName, "", ""); err != nil {
		return nil, fmt.Errorf("create tmux session: %w", err)
	}

	if err := m.tmux.SendKeys(sessionName, fullCmd); err != nil {
		if killErr := m.tmux.KillSession(sessionName); killErr != nil {
			return nil, fmt.Errorf("send spawn command: %w; cleanup tmux session %q: %v", err, sessionName, killErr)
		}
		return nil, fmt.Errorf("send spawn command: %w", err)
	}

	issueRepo := repo.IssueRepoFullName()
	sess := Session{
		ID:           sessionName,
		IssueNumber:  issueNumber,
		IssueTitle:   issueTitle,
		Repo:         repo.FullName(),
		IssueRepo:    issueRepo,
		Branch:       branch,
		TmuxSession:  sessionName,
		WorktreePath: worktreePath,
		CreatedAt:    time.Now(),
		Status:       StatusRunning,
	}

	// When not using worktrees, set WorktreePath to the repo dir
	if !m.cfg.Spawn.UseWorktree {
		sess.WorktreePath = repoDir
	}

	m.mu.Lock()
	m.sessions = append(m.sessions, sess)
	m.mu.Unlock()

	if err := m.saveState(); err != nil {
		// Rollback: remove the session from in-memory state
		m.mu.Lock()
		for i := len(m.sessions) - 1; i >= 0; i-- {
			if m.sessions[i].ID == sess.ID {
				m.sessions = append(m.sessions[:i], m.sessions[i+1:]...)
				break
			}
		}
		m.mu.Unlock()
		// Best-effort cleanup of the tmux session
		if killErr := m.tmux.KillSession(sessionName); killErr != nil {
			return nil, fmt.Errorf("save state: %w; cleanup tmux session %q: %v", err, sessionName, killErr)
		}
		return nil, fmt.Errorf("save state: %w", err)
	}

	return &sess, nil
}

// RefreshStatuses updates the status of all sessions by checking tmux.
func (m *Manager) RefreshStatuses() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i := range m.sessions {
		s := &m.sessions[i]

		if !m.tmux.SessionExists(s.TmuxSession) {
			s.Status = StatusStopped
			continue
		}

		claudeRunning := m.tmux.IsProcessRunning(s.TmuxSession, "claude")
		if !claudeRunning {
			s.Status = StatusDone
		} else {
			s.Status = StatusRunning
			// Try to detect waiting state from pane output
			output, err := m.tmux.CapturePaneOutput(s.TmuxSession, 5)
			if err == nil {
				s.LastActivity = extractLastActivity(output)
				if isWaitingForInput(output) {
					s.Status = StatusWaiting
				}
			}
		}
	}
}

// RemoveSession removes a session from tracking and optionally kills the tmux session.
func (m *Manager) RemoveSession(id string, killTmux bool) error {
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

	if killTmux {
		if err := m.tmux.KillSession(m.sessions[idx].TmuxSession); err != nil {
			return fmt.Errorf("kill tmux session %q: %w", m.sessions[idx].TmuxSession, err)
		}
	}

	m.sessions = append(m.sessions[:idx], m.sessions[idx+1:]...)
	return m.saveStateLocked()
}

// FindByIssue finds a session for a specific issue.
// The issueRepo parameter is matched against the session's issue repo
// (falling back to Repo for backward compatibility with older sessions).
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

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
