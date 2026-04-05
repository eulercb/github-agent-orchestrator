package claude

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eulercb/github-agent-orchestrator/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractLastActivity(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "empty input",
			input:  "",
			expect: "",
		},
		{
			name:   "single line",
			input:  "Working on feature X",
			expect: "Working on feature X",
		},
		{
			name:   "multiple lines",
			input:  "line1\nline2\nlast line here",
			expect: "last line here",
		},
		{
			name:   "trailing empty lines",
			input:  "line1\nlast content\n\n\n",
			expect: "last content",
		},
		{
			name:   "long line truncation",
			input:  "This is a very long line that exceeds eighty characters and should be truncated to fit within the display area nicely",
			expect: "This is a very long line that exceeds eighty characters and should be truncated ...",
		},
		{
			name:   "only whitespace",
			input:  "   \n   \n   ",
			expect: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractLastActivity(tt.input)
			assert.Equal(t, tt.expect, got)
		})
	}
}

func TestIsWaitingForInput(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect bool
	}{
		{
			name:   "empty",
			input:  "",
			expect: false,
		},
		{
			name:   "working output",
			input:  "Reading file src/main.go\nEditing file",
			expect: false,
		},
		{
			name:   "claude prompt with angle bracket",
			input:  "some output\nclaude > ",
			expect: true,
		},
		{
			name:   "waiting for your response",
			input:  "I've made the changes.\nWaiting for your response",
			expect: true,
		},
		{
			name:   "question mark prompt",
			input:  "Would you like to proceed? ",
			expect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isWaitingForInput(tt.input)
			assert.Equal(t, tt.expect, got)
		})
	}
}

func TestParseWorktreeList(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect []Worktree
	}{
		{
			name:   "empty input",
			input:  "",
			expect: nil,
		},
		{
			name: "single worktree",
			input: "worktree /home/user/myrepo/.worktrees/claude/issue-42\n" +
				"HEAD abc123\n" +
				"branch refs/heads/claude/issue-42\n\n",
			expect: []Worktree{
				{Path: "/home/user/myrepo/.worktrees/claude/issue-42", Branch: "claude/issue-42"},
			},
		},
		{
			name: "multiple worktrees",
			input: "worktree /home/user/myrepo\n" +
				"HEAD abc123\n" +
				"branch refs/heads/main\n\n" +
				"worktree /home/user/myrepo/.worktrees/claude/issue-1\n" +
				"HEAD def456\n" +
				"branch refs/heads/claude/issue-1\n\n" +
				"worktree /home/user/myrepo/.worktrees/claude/issue-2\n" +
				"HEAD 789abc\n" +
				"branch refs/heads/claude/issue-2\n\n",
			expect: []Worktree{
				{Path: "/home/user/myrepo", Branch: "main"},
				{Path: "/home/user/myrepo/.worktrees/claude/issue-1", Branch: "claude/issue-1"},
				{Path: "/home/user/myrepo/.worktrees/claude/issue-2", Branch: "claude/issue-2"},
			},
		},
		{
			name: "detached HEAD worktree",
			input: "worktree /home/user/myrepo\n" +
				"HEAD abc123\n" +
				"branch refs/heads/main\n\n" +
				"worktree /home/user/myrepo/.worktrees/detached\n" +
				"HEAD def456\n" +
				"detached\n\n",
			expect: []Worktree{
				{Path: "/home/user/myrepo", Branch: "main"},
				{Path: "/home/user/myrepo/.worktrees/detached", Branch: ""},
			},
		},
		{
			name: "no trailing newline",
			input: "worktree /home/user/myrepo\n" +
				"HEAD abc123\n" +
				"branch refs/heads/main",
			expect: []Worktree{
				{Path: "/home/user/myrepo", Branch: "main"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseWorktreeList(tt.input)
			if assert.Len(t, got, len(tt.expect)) {
				for i := range got {
					assert.Equal(t, tt.expect[i].Path, got[i].Path, "worktree[%d].Path", i)
					assert.Equal(t, tt.expect[i].Branch, got[i].Branch, "worktree[%d].Branch", i)
				}
			}
		})
	}
}

func TestWorktreeMetadata(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("write and read", func(t *testing.T) {
		meta := &worktreeMetadata{IssueNumber: 42, IssueRepo: "acme/app"}
		require.NoError(t, writeWorktreeMetadata(tmpDir, meta))

		got, err := readWorktreeMetadata(tmpDir)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, 42, got.IssueNumber)
		assert.Equal(t, "acme/app", got.IssueRepo)
	})

	t.Run("read missing file", func(t *testing.T) {
		got, err := readWorktreeMetadata(filepath.Join(tmpDir, "nonexistent"))
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("write creates .claude directory", func(t *testing.T) {
		newDir := filepath.Join(tmpDir, "fresh-worktree")
		require.NoError(t, os.MkdirAll(newDir, 0o750))
		require.NoError(t, writeWorktreeMetadata(newDir, &worktreeMetadata{IssueNumber: 7}))

		info, err := os.Stat(filepath.Join(newDir, ".claude"))
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("omitempty issue_repo", func(t *testing.T) {
		dir := filepath.Join(tmpDir, "no-repo")
		require.NoError(t, os.MkdirAll(dir, 0o750))
		require.NoError(t, writeWorktreeMetadata(dir, &worktreeMetadata{IssueNumber: 5}))

		got, err := readWorktreeMetadata(dir)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, 5, got.IssueNumber)
		assert.Empty(t, got.IssueRepo)
	})
}

func TestImportWorktree(t *testing.T) {
	stateDir := t.TempDir()

	cfg := &config.Config{
		Repos: []config.RepoConfig{
			{Owner: "acme", Name: "app"},
		},
		SessionDir: stateDir,
	}

	mgr, err := NewManager(cfg, nil)
	require.NoError(t, err)

	repo := &cfg.Repos[0]

	t.Run("reads metadata file", func(t *testing.T) {
		wtDir := filepath.Join(t.TempDir(), "wt-42")
		require.NoError(t, os.MkdirAll(wtDir, 0o750))
		require.NoError(t, writeWorktreeMetadata(wtDir, &worktreeMetadata{
			IssueNumber: 42,
			IssueRepo:   "acme/app",
		}))

		wt := &Worktree{Path: wtDir, Branch: "claude/issue-42"}
		sess, err := mgr.ImportWorktree(repo, wt)
		require.NoError(t, err)
		assert.Equal(t, "gao-acme-app-42", sess.ID)
		assert.Equal(t, 42, sess.IssueNumber)
		assert.Equal(t, "acme/app", sess.IssueRepo)
		assert.Equal(t, StatusStopped, sess.Status)
		assert.Equal(t, 0, sess.PID)
	})

	t.Run("cross-repo metadata", func(t *testing.T) {
		wtDir := filepath.Join(t.TempDir(), "wt-cross")
		require.NoError(t, os.MkdirAll(wtDir, 0o750))
		require.NoError(t, writeWorktreeMetadata(wtDir, &worktreeMetadata{
			IssueNumber: 7,
			IssueRepo:   "other/repo",
		}))

		wt := &Worktree{Path: wtDir, Branch: "claude/issue-other-repo-7"}
		sess, err := mgr.ImportWorktree(repo, wt)
		require.NoError(t, err)
		assert.Equal(t, "gao-acme-app-other-repo-7", sess.ID)
		assert.Equal(t, 7, sess.IssueNumber)
		assert.Equal(t, "other/repo", sess.IssueRepo)
	})

	t.Run("no metadata falls back to unassociated", func(t *testing.T) {
		wtDir := filepath.Join(t.TempDir(), "wt-bare")
		require.NoError(t, os.MkdirAll(wtDir, 0o750))

		wt := &Worktree{Path: wtDir, Branch: "feature/something"}
		sess, err := mgr.ImportWorktree(repo, wt)
		require.NoError(t, err)
		assert.Equal(t, "gao-acme-app-feature-something", sess.ID)
		assert.Equal(t, 0, sess.IssueNumber)
		assert.Empty(t, sess.IssueRepo)
	})

	t.Run("detached worktree", func(t *testing.T) {
		wtDir := filepath.Join(t.TempDir(), "detached-abc")
		require.NoError(t, os.MkdirAll(wtDir, 0o750))

		wt := &Worktree{Path: wtDir, Branch: ""}
		sess, err := mgr.ImportWorktree(repo, wt)
		require.NoError(t, err)
		assert.Equal(t, "gao-acme-app-detached-abc", sess.ID)
		assert.Equal(t, 0, sess.IssueNumber)
	})

	t.Run("collision detection", func(t *testing.T) {
		// Re-use the same worktree dir from "reads metadata file" test.
		// Create a new worktree dir with the same issue to trigger collision.
		wtDir := filepath.Join(t.TempDir(), "wt-42-dup")
		require.NoError(t, os.MkdirAll(wtDir, 0o750))
		require.NoError(t, writeWorktreeMetadata(wtDir, &worktreeMetadata{
			IssueNumber: 42,
			IssueRepo:   "acme/app",
		}))

		wt := &Worktree{Path: wtDir, Branch: "claude/issue-42"}
		_, err := mgr.ImportWorktree(repo, wt)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})
}
