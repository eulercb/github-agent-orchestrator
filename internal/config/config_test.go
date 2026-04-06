package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, "claude --dangerously-skip-permissions", cfg.Spawn.Command)
	assert.True(t, cfg.Spawn.UseWorktree, "expected worktree to be enabled by default")
	assert.False(t, cfg.CCUsage.Enabled, "expected ccusage to be disabled by default")
	assert.Equal(t, DefaultIssueFilter, cfg.IssueFilter)
	assert.True(t, cfg.TrackIssues, "expected track_issues to be enabled by default")
}

func TestLoadMissingConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "claude --dangerously-skip-permissions", cfg.Spawn.Command)
}

func TestSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	cfg := DefaultConfig()
	cfg.ReposDir = "/tmp/repos"
	cfg.IssueFilter = "is:open assignee:@me repo:testowner/testrepo"
	cfg.TrackIssues = false

	require.NoError(t, Save(&cfg))

	// Verify file exists
	cfgPath := filepath.Join(tmpDir, "gao", "config.yaml")
	_, err := os.Stat(cfgPath)
	require.NoError(t, err, "config file not created")

	// Load it back
	loaded, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "/tmp/repos", loaded.ReposDir)
	assert.Equal(t, "is:open assignee:@me repo:testowner/testrepo", loaded.IssueFilter)
	assert.False(t, loaded.TrackIssues)
}

func TestExpandReposDir(t *testing.T) {
	t.Run("absolute path", func(t *testing.T) {
		cfg := Config{ReposDir: "/repos"}
		dir, err := cfg.ExpandReposDir()
		require.NoError(t, err)
		assert.Equal(t, "/repos", dir)
	})

	t.Run("tilde expansion", func(t *testing.T) {
		cfg := Config{ReposDir: "~/repos"}
		dir, err := cfg.ExpandReposDir()
		require.NoError(t, err)
		home, err := os.UserHomeDir()
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(home, "repos"), dir)
	})

	t.Run("bare tilde", func(t *testing.T) {
		cfg := Config{ReposDir: "~"}
		dir, err := cfg.ExpandReposDir()
		require.NoError(t, err)
		home, err := os.UserHomeDir()
		require.NoError(t, err)
		assert.Equal(t, home, dir)
	})

	t.Run("empty repos_dir", func(t *testing.T) {
		cfg := Config{}
		_, err := cfg.ExpandReposDir()
		assert.Error(t, err)
	})
}
