package claude

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/eulercb/github-agent-orchestrator/internal/config"
	"github.com/eulercb/github-agent-orchestrator/internal/repo"
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
		meta := &worktreeMetadata{IssueNumber: 42, IssueRepo: "acme/app", IssueTitle: "Fix login bug"}
		require.NoError(t, writeWorktreeMetadata(tmpDir, meta))

		got, err := readWorktreeMetadata(tmpDir)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, 42, got.IssueNumber)
		assert.Equal(t, "acme/app", got.IssueRepo)
		assert.Equal(t, "Fix login bug", got.IssueTitle)
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

func TestBackfillIssueTitles(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "sessions.yaml")

	mgr := &Manager{
		cfg:       &config.Config{},
		stateFile: stateFile,
		sessions: []Session{
			{ID: "s1", Repo: "acme/app", Branch: "claude/issue-1", IssueTitle: ""},
			{ID: "s2", Repo: "acme/app", Branch: "claude/issue-2", IssueTitle: "Already set"},
			{ID: "s3", Repo: "acme/app", Branch: "", IssueTitle: ""},
		},
	}

	t.Run("no-op when gh is nil", func(t *testing.T) {
		mgr.gh = nil
		require.NoError(t, mgr.BackfillIssueTitles())

		// Titles unchanged — no client to fetch from.
		assert.Empty(t, mgr.sessions[0].IssueTitle)
		assert.Equal(t, "Already set", mgr.sessions[1].IssueTitle)
		assert.Empty(t, mgr.sessions[2].IssueTitle)
	})

	t.Run("preserves existing titles when gh is nil", func(t *testing.T) {
		mgr.gh = nil
		require.NoError(t, mgr.BackfillIssueTitles())
		assert.Equal(t, "Already set", mgr.sessions[1].IssueTitle)
	})

	t.Run("skips sessions without branch", func(t *testing.T) {
		// s3 has no branch — should be skipped even if gh were available.
		mgr.gh = nil
		require.NoError(t, mgr.BackfillIssueTitles())
		assert.Empty(t, mgr.sessions[2].IssueTitle)
	})

	t.Run("persists title to worktree metadata file", func(t *testing.T) {
		// Simulate what BackfillIssueTitles does after resolving a title:
		// it writes both the in-memory session and the worktree metadata file.
		wtDir := filepath.Join(tmpDir, "worktree-backfill")
		require.NoError(t, os.MkdirAll(wtDir, 0o750))

		// Pre-seed metadata without a title (as if created by an older version).
		require.NoError(t, writeWorktreeMetadata(wtDir, &worktreeMetadata{
			IssueNumber: 10,
			IssueRepo:   "acme/app",
		}))

		mgr2 := &Manager{
			cfg:       &config.Config{},
			stateFile: filepath.Join(tmpDir, "sessions2.yaml"),
			sessions: []Session{
				{
					ID:           "s-wt",
					Repo:         "acme/app",
					Branch:       "claude/issue-10",
					IssueNumber:  10,
					IssueRepo:    "acme/app",
					IssueTitle:   "Resolved title",
					WorktreePath: wtDir,
				},
			},
		}
		// Manually call writeWorktreeMetadata as BackfillIssueTitles does.
		sess := &mgr2.sessions[0]
		require.NoError(t, writeWorktreeMetadata(sess.WorktreePath, &worktreeMetadata{
			IssueNumber: sess.IssueNumber,
			IssueRepo:   sess.IssueRepo,
			IssueTitle:  sess.IssueTitle,
		}))

		meta, err := readWorktreeMetadata(wtDir)
		require.NoError(t, err)
		require.NotNil(t, meta)
		assert.Equal(t, "Resolved title", meta.IssueTitle)
		assert.Equal(t, 10, meta.IssueNumber)
	})
}

func TestRefreshExistingSessions(t *testing.T) {
	tmpDir := t.TempDir()
	stateFile := filepath.Join(tmpDir, "sessions.yaml")

	wtDir := filepath.Join(tmpDir, "worktree-1")
	require.NoError(t, os.MkdirAll(wtDir, 0o750))

	absWt, err := filepath.Abs(wtDir)
	require.NoError(t, err)

	t.Run("updates stale branch name", func(t *testing.T) {
		mgr := &Manager{
			cfg:       &config.Config{},
			stateFile: stateFile,
			sessions: []Session{
				{
					ID:           "s1",
					Repo:         "acme/app",
					Branch:       "old-branch",
					IssueNumber:  5,
					IssueTitle:   "Existing title",
					WorktreePath: wtDir,
				},
			},
		}

		discovered := map[string]repoWorktree{
			absWt: {
				r:  &repo.Repo{Owner: "acme", Name: "app"},
				wt: Worktree{Path: wtDir, Branch: "renamed-branch"},
			},
		}

		mgr.refreshExistingSessions(discovered)

		assert.Equal(t, "renamed-branch", mgr.sessions[0].Branch)
		// Title and issue number should be preserved.
		assert.Equal(t, 5, mgr.sessions[0].IssueNumber)
		assert.Equal(t, "Existing title", mgr.sessions[0].IssueTitle)
	})

	t.Run("skips sessions not in discovered map", func(t *testing.T) {
		mgr := &Manager{
			cfg:       &config.Config{},
			stateFile: stateFile,
			sessions: []Session{
				{
					ID:           "s2",
					Repo:         "acme/app",
					Branch:       "some-branch",
					IssueNumber:  0,
					WorktreePath: filepath.Join(tmpDir, "nonexistent"),
				},
			},
		}

		mgr.refreshExistingSessions(map[string]repoWorktree{})

		assert.Equal(t, "some-branch", mgr.sessions[0].Branch)
		assert.Equal(t, 0, mgr.sessions[0].IssueNumber)
	})

	t.Run("no-op when nothing changed", func(t *testing.T) {
		mgr := &Manager{
			cfg:       &config.Config{},
			stateFile: stateFile,
			sessions: []Session{
				{
					ID:           "s3",
					Repo:         "acme/app",
					Branch:       "same-branch",
					IssueNumber:  10,
					IssueTitle:   "Has title",
					WorktreePath: wtDir,
				},
			},
		}

		discovered := map[string]repoWorktree{
			absWt: {
				r:  &repo.Repo{Owner: "acme", Name: "app"},
				wt: Worktree{Path: wtDir, Branch: "same-branch"},
			},
		}

		mgr.refreshExistingSessions(discovered)

		assert.Equal(t, "same-branch", mgr.sessions[0].Branch)
		assert.Equal(t, 10, mgr.sessions[0].IssueNumber)
		assert.Equal(t, "Has title", mgr.sessions[0].IssueTitle)
	})
}

// initTestGitRepoInDir creates a git repo with an initial commit in the given
// directory name under parentDir and returns its absolute path.
func initTestGitRepoInDir(t *testing.T, parentDir, name string) string {
	t.Helper()
	dir := filepath.Join(parentDir, name)
	require.NoError(t, os.MkdirAll(dir, 0o750))
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	run("config", "commit.gpgsign", "false")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test"), 0o600))
	run("add", ".")
	run("commit", "-m", "initial")
	// Set a fake GitHub remote so repo discovery can parse owner/name.
	run("remote", "add", "origin", "https://github.com/acme/"+name+".git")
	return dir
}

func TestSyncWorktrees(t *testing.T) {
	reposDir := t.TempDir()
	stateDir := t.TempDir()
	repoDir := initTestGitRepoInDir(t, reposDir, "app")

	// Create a worktree inside the repo.
	wtPath := filepath.Join(repoDir, ".worktrees", "claude-issue-42")
	cmd := exec.CommandContext(context.Background(), "git", "worktree", "add", "-b", "claude/issue-42", wtPath)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git worktree add: %s", out)

	// Write metadata for the worktree.
	require.NoError(t, writeWorktreeMetadata(wtPath, &worktreeMetadata{
		IssueNumber: 42,
		IssueRepo:   "acme/app",
	}))

	cfg := &config.Config{
		ReposDir:   reposDir,
		SessionDir: stateDir,
	}

	mgr, err := NewManager(cfg, nil)
	require.NoError(t, err)

	t.Run("discovers new worktrees", func(t *testing.T) {
		result, err := mgr.SyncWorktrees()
		require.NoError(t, err)
		assert.Equal(t, 1, result.Added)
		assert.Equal(t, 0, result.Removed)

		sessions := mgr.Sessions()
		require.Len(t, sessions, 1)
		assert.Equal(t, "gao-acme-app-42", sessions[0].ID)
		assert.Equal(t, 42, sessions[0].IssueNumber)
		assert.Equal(t, "acme/app", sessions[0].IssueRepo)
		assert.Equal(t, "claude/issue-42", sessions[0].Branch)
		assert.Equal(t, StatusStopped, sessions[0].Status)
	})

	t.Run("idempotent re-sync", func(t *testing.T) {
		result, err := mgr.SyncWorktrees()
		require.NoError(t, err)
		assert.Equal(t, 0, result.Added)
		assert.Equal(t, 0, result.Removed)
		assert.Len(t, mgr.Sessions(), 1)
	})

	t.Run("removes stale sessions", func(t *testing.T) {
		// Remove the worktree (force because metadata files are untracked).
		rmCmd := exec.CommandContext(context.Background(), "git", "worktree", "remove", "--force", wtPath)
		rmCmd.Dir = repoDir
		rmOut, rmErr := rmCmd.CombinedOutput()
		require.NoError(t, rmErr, "git worktree remove: %s", rmOut)

		result, err := mgr.SyncWorktrees()
		require.NoError(t, err)
		assert.Equal(t, 0, result.Added)
		assert.Equal(t, 1, result.Removed)
		assert.Empty(t, mgr.Sessions())
	})

	t.Run("cross-repo metadata", func(t *testing.T) {
		// Create a new worktree with cross-repo metadata.
		wtPath2 := filepath.Join(repoDir, ".worktrees", "claude-cross-7")
		cmd := exec.CommandContext(context.Background(), "git", "worktree", "add", "-b", "claude/cross-7", wtPath2)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git worktree add: %s", out)
		require.NoError(t, writeWorktreeMetadata(wtPath2, &worktreeMetadata{
			IssueNumber: 7,
			IssueRepo:   "other/repo",
		}))

		result, syncErr := mgr.SyncWorktrees()
		require.NoError(t, syncErr)
		assert.Equal(t, 1, result.Added)

		sessions := mgr.Sessions()
		require.Len(t, sessions, 1)
		assert.Equal(t, "gao-acme-app-other-repo-7", sessions[0].ID)
		assert.Equal(t, 7, sessions[0].IssueNumber)
		assert.Equal(t, "other/repo", sessions[0].IssueRepo)
	})

	t.Run("no metadata falls back to unassociated", func(t *testing.T) {
		wtPath3 := filepath.Join(repoDir, ".worktrees", "feature-bare")
		cmd := exec.CommandContext(context.Background(), "git", "worktree", "add", "-b", "feature/bare", wtPath3)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git worktree add: %s", out)

		result, syncErr := mgr.SyncWorktrees()
		require.NoError(t, syncErr)
		assert.Equal(t, 1, result.Added)

		// Find the new session (the cross-repo one from previous test still exists).
		sessions := mgr.Sessions()
		var bareSess *Session
		for i := range sessions {
			if sessions[i].Branch == "feature/bare" {
				bareSess = &sessions[i]
				break
			}
		}
		require.NotNil(t, bareSess)
		assert.Equal(t, "gao-acme-app-feature-bare", bareSess.ID)
		assert.Equal(t, 0, bareSess.IssueNumber)
		assert.Empty(t, bareSess.IssueRepo)
	})
}

func TestRemoveWorktree(t *testing.T) {
	reposDir := t.TempDir()
	stateDir := t.TempDir()
	repoDir := initTestGitRepoInDir(t, reposDir, "app")

	cfg := &config.Config{
		ReposDir:   reposDir,
		SessionDir: stateDir,
	}

	t.Run("removes worktree and session", func(t *testing.T) {
		// Create a worktree.
		wtPath := filepath.Join(repoDir, ".worktrees", "claude-issue-10")
		cmd := exec.CommandContext(context.Background(), "git", "worktree", "add", "-b", "claude/issue-10", wtPath)
		cmd.Dir = repoDir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git worktree add: %s", out)

		mgr, err := NewManager(cfg, nil)
		require.NoError(t, err)
		mgr.sessions = []Session{
			{ID: "s-10", Repo: "acme/app", Branch: "claude/issue-10", WorktreePath: wtPath},
		}

		require.NoError(t, mgr.RemoveWorktree("s-10"))
		assert.Empty(t, mgr.Sessions())

		// Verify the worktree directory is gone.
		_, statErr := os.Stat(wtPath)
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("skips removal when worktree path is main repo", func(t *testing.T) {
		mgr, err := NewManager(cfg, nil)
		require.NoError(t, err)
		mgr.sessions = []Session{
			{ID: "s-main", Repo: "acme/app", Branch: "main", WorktreePath: repoDir},
		}

		require.NoError(t, mgr.RemoveWorktree("s-main"))
		assert.Empty(t, mgr.Sessions())

		// The main repo directory must still exist.
		_, statErr := os.Stat(repoDir)
		assert.NoError(t, statErr)
	})

	t.Run("session not found", func(t *testing.T) {
		mgr, err := NewManager(cfg, nil)
		require.NoError(t, err)

		err = mgr.RemoveWorktree("nonexistent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("no local repo found", func(t *testing.T) {
		mgr, err := NewManager(cfg, nil)
		require.NoError(t, err)
		mgr.sessions = []Session{
			{ID: "s-orphan", Repo: "unknown/repo", Branch: "b", WorktreePath: "/tmp/fake"},
		}

		err = mgr.RemoveWorktree("s-orphan")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no local repo found")
	})
}

func TestBuildSessionName(t *testing.T) {
	r := &repo.Repo{Owner: "acme", Name: "app", LocalPath: "/tmp/app"}

	t.Run("with issue same repo", func(t *testing.T) {
		wt := &Worktree{Path: "/tmp/wt", Branch: "claude/issue-42"}
		assert.Equal(t, "gao-acme-app-42", buildSessionName(r, wt, 42, "acme/app"))
	})

	t.Run("with issue cross repo", func(t *testing.T) {
		wt := &Worktree{Path: "/tmp/wt", Branch: "claude/issue-7"}
		assert.Equal(t, "gao-acme-app-other-repo-7", buildSessionName(r, wt, 7, "other/repo"))
	})

	t.Run("no issue uses branch", func(t *testing.T) {
		wt := &Worktree{Path: "/tmp/wt", Branch: "feature/something"}
		assert.Equal(t, "gao-acme-app-feature-something", buildSessionName(r, wt, 0, ""))
	})

	t.Run("no issue no branch uses path", func(t *testing.T) {
		wt := &Worktree{Path: "/tmp/detached-abc", Branch: ""}
		assert.Equal(t, "gao-acme-app-detached-abc", buildSessionName(r, wt, 0, ""))
	})
}
