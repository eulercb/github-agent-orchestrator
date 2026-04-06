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
	assert.Empty(t, cfg.Repos)
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
	cfg.Repos = []RepoConfig{
		{
			Owner: "testowner",
			Name:  "testrepo",
			Filters: IssueFilters{
				Search: "is:open assignee:@me repo:testowner/testrepo",
			},
		},
	}

	require.NoError(t, Save(&cfg))

	// Verify file exists
	cfgPath := filepath.Join(tmpDir, "gao", "config.yaml")
	_, err := os.Stat(cfgPath)
	require.NoError(t, err, "config file not created")

	// Load it back
	loaded, err := Load()
	require.NoError(t, err)
	require.Len(t, loaded.Repos, 1)
	assert.Equal(t, "testowner/testrepo", loaded.Repos[0].FullName())
	assert.Equal(t, "is:open assignee:@me repo:testowner/testrepo", loaded.Repos[0].Filters.Search)
}

func TestRepoFullName(t *testing.T) {
	r := RepoConfig{Owner: "foo", Name: "bar"}
	assert.Equal(t, "foo/bar", r.FullName())
}

func TestIssueRepoFullName(t *testing.T) {
	// Without IssueSource, falls back to main repo
	r := RepoConfig{Owner: "foo", Name: "bar"}
	assert.Equal(t, "foo/bar", r.IssueRepoFullName())

	// With IssueSource, returns the issue source repo
	r.IssueSource = &IssueSource{Owner: "org", Name: "issues"}
	assert.Equal(t, "org/issues", r.IssueRepoFullName())

	// With empty IssueSource fields, falls back to main repo
	r.IssueSource = &IssueSource{Owner: "", Name: ""}
	assert.Equal(t, "foo/bar", r.IssueRepoFullName())

	// Partial IssueSource: only Name set, Owner inherited from main repo
	r.IssueSource = &IssueSource{Owner: "", Name: "other-repo"}
	assert.Equal(t, "foo/other-repo", r.IssueRepoFullName())

	// Partial IssueSource: only Owner set, Name inherited from main repo
	r.IssueSource = &IssueSource{Owner: "other-org", Name: ""}
	assert.Equal(t, "other-org/bar", r.IssueRepoFullName())
}

func TestRepoLocalDir(t *testing.T) {
	repo := &RepoConfig{Owner: "acme", Name: "app"}

	t.Run("local_path wins", func(t *testing.T) {
		cfg := Config{
			ReposDir: "/repos",
			Spawn:    SpawnConfig{RepoDir: "/legacy"},
		}
		repo := &RepoConfig{Owner: "acme", Name: "app", LocalPath: "/custom/app"}
		dir, err := cfg.RepoLocalDir(repo)
		require.NoError(t, err)
		assert.Equal(t, "/custom/app", dir)
	})

	t.Run("repos_dir fallback", func(t *testing.T) {
		cfg := Config{ReposDir: "/repos", Spawn: SpawnConfig{RepoDir: "/legacy"}}
		dir, err := cfg.RepoLocalDir(repo)
		require.NoError(t, err)
		assert.Equal(t, "/repos/app", dir)
	})

	t.Run("spawn.repo_dir legacy fallback", func(t *testing.T) {
		cfg := Config{Spawn: SpawnConfig{RepoDir: "/legacy"}}
		dir, err := cfg.RepoLocalDir(repo)
		require.NoError(t, err)
		assert.Equal(t, "/legacy", dir)
	})

	t.Run("home fallback", func(t *testing.T) {
		cfg := Config{}
		dir, err := cfg.RepoLocalDir(repo)
		require.NoError(t, err)
		home, _ := os.UserHomeDir()
		assert.Equal(t, filepath.Join(home, "app"), dir)
	})
}

func TestSaveAndLoadWithIssueSource(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	cfg := DefaultConfig()
	cfg.Repos = []RepoConfig{
		{
			Owner: "myorg",
			Name:  "myapp",
			IssueSource: &IssueSource{
				Owner: "myorg",
				Name:  "project-issues",
			},
			Filters: IssueFilters{
				Search: "is:open repo:myorg/project-issues",
			},
		},
	}

	require.NoError(t, Save(&cfg))

	loaded, err := Load()
	require.NoError(t, err)
	require.Len(t, loaded.Repos, 1)

	repo := &loaded.Repos[0]
	assert.Equal(t, "myorg/myapp", repo.FullName())
	require.NotNil(t, repo.IssueSource)
	assert.Equal(t, "myorg/project-issues", repo.IssueRepoFullName())
	assert.Equal(t, "is:open repo:myorg/project-issues", repo.Filters.Search)
}
