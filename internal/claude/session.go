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
	"github.com/eulercb/github-agent-orchestrator/internal/process"
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
	sessions  []Session
	mu        sync.RWMutex
	stateFile string
}

// NewManager creates a session manager.
func NewManager(cfg *config.Config) (*Manager, error) {
	stateFile, err := config.SessionsPath(cfg.SessionDir)
	if err != nil {
		return nil, err
	}

	m := &Manager{
		cfg:       cfg,
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
	var sessionName, branch string
	if repo.IssueSource != nil && repo.IssueRepoFullName() != repo.FullName() {
		// Include the effective issue source repo identifier to avoid collisions
		// when issues come from a different repo than where PRs are opened,
		// including configurations where IssueSource overrides only the owner.
		issueRepoID := strings.ReplaceAll(repo.IssueRepoFullName(), "/", "-")
		sessionName = fmt.Sprintf("gao-%s-%s-%s-%d", repo.Owner, repo.Name, issueRepoID, issueNumber)
		branch = fmt.Sprintf("claude/issue-%s-%d", issueRepoID, issueNumber)
	} else {
		sessionName = fmt.Sprintf("gao-%s-%s-%d", repo.Owner, repo.Name, issueNumber)
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

	repoDir := m.cfg.Spawn.RepoDir
	if repoDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("determine user home directory: %w", err)
		}
		repoDir = filepath.Join(home, repo.Name)
	}

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

	issueRepo := repo.IssueRepoFullName()
	sess := Session{
		ID:           sessionName,
		IssueNumber:  issueNumber,
		IssueTitle:   issueTitle,
		Repo:         repo.FullName(),
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

func (m *Manager) setupWorktree(repoDir, worktreePath, branch string) error {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	// git fetch origin
	cmd := exec.CommandContext(ctx, "git", "fetch", "origin")
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch: %s (%w)", strings.TrimSpace(string(out)), err)
	}

	// Determine base branch
	baseBranch := m.cfg.Spawn.BaseBranch
	if baseBranch == "" {
		refCmd := exec.CommandContext(ctx, "git", "symbolic-ref", "refs/remotes/origin/HEAD")
		refCmd.Dir = repoDir
		out, err := refCmd.Output()
		if err != nil {
			baseBranch = "main"
		} else {
			baseBranch = strings.TrimPrefix(strings.TrimSpace(string(out)), "refs/remotes/origin/")
		}
	}

	// Create worktree parent directory
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o750); err != nil {
		return fmt.Errorf("create worktree parent: %w", err)
	}

	// git worktree add
	wtCmd := exec.CommandContext(ctx, "git", "worktree", "add", worktreePath, "-b", branch, "origin/"+baseBranch)
	wtCmd.Dir = repoDir
	if out, err := wtCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree add: %s (%w)", strings.TrimSpace(string(out)), err)
	}

	return nil
}

func (m *Manager) setupBranch(repoDir, branch string) error {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	// Try checking out existing branch first.
	cmd := exec.CommandContext(ctx, "git", "checkout", branch)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		checkoutOutput := strings.TrimSpace(string(out))

		// Only create the branch if it truly does not exist locally.
		verifyCmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
		verifyCmd.Dir = repoDir
		if verifyErr := verifyCmd.Run(); verifyErr != nil {
			// Branch does not exist — create it.
			createCmd := exec.CommandContext(ctx, "git", "checkout", "-b", branch)
			createCmd.Dir = repoDir
			if createOut, createErr := createCmd.CombinedOutput(); createErr != nil {
				return fmt.Errorf("git checkout -b %s: %s (%w)", branch, strings.TrimSpace(string(createOut)), createErr)
			}
			return nil
		}

		// Branch exists but checkout failed for another reason (e.g. uncommitted changes).
		return fmt.Errorf("git checkout %s: %s (%w)", branch, checkoutOutput, err)
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
