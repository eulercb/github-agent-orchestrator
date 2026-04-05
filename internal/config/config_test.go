package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Spawn.Command != "claude --dangerously-skip-permissions" {
		t.Errorf("unexpected default spawn command: %s", cfg.Spawn.Command)
	}
	if !cfg.Spawn.UseWorktree {
		t.Error("expected worktree to be enabled by default")
	}
	if cfg.CCUsage.Enabled {
		t.Error("expected ccusage to be disabled by default")
	}
	if len(cfg.Repos) != 0 {
		t.Error("expected no repos by default")
	}
}

func TestLoadMissingConfig(t *testing.T) {
	// Temporarily override config dir to a temp location
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error loading missing config: %v", err)
	}
	// Should return defaults
	if cfg.Spawn.Command != "claude --dangerously-skip-permissions" {
		t.Errorf("expected default spawn command, got: %s", cfg.Spawn.Command)
	}
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
				Assignee: "@me",
				Labels:   []string{"bug"},
				State:    "open",
			},
		},
	}

	if err := Save(&cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Verify file exists
	cfgPath := filepath.Join(tmpDir, "gao", "config.yaml")
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("config file not created: %v", err)
	}

	// Load it back
	loaded, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if len(loaded.Repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(loaded.Repos))
	}
	if loaded.Repos[0].FullName() != "testowner/testrepo" {
		t.Errorf("unexpected repo: %s", loaded.Repos[0].FullName())
	}
	if loaded.Repos[0].Filters.Assignee != "@me" {
		t.Errorf("unexpected assignee: %s", loaded.Repos[0].Filters.Assignee)
	}
	if len(loaded.Repos[0].Filters.Labels) != 1 || loaded.Repos[0].Filters.Labels[0] != "bug" {
		t.Errorf("unexpected labels: %v", loaded.Repos[0].Filters.Labels)
	}
}

func TestRepoFullName(t *testing.T) {
	r := RepoConfig{Owner: "foo", Name: "bar"}
	if r.FullName() != "foo/bar" {
		t.Errorf("expected foo/bar, got %s", r.FullName())
	}
}

func TestIssueRepoFullName(t *testing.T) {
	// Without IssueSource, falls back to main repo
	r := RepoConfig{Owner: "foo", Name: "bar"}
	if r.IssueRepoFullName() != "foo/bar" {
		t.Errorf("expected foo/bar, got %s", r.IssueRepoFullName())
	}

	// With IssueSource, returns the issue source repo
	r.IssueSource = &IssueSource{Owner: "org", Name: "issues"}
	if r.IssueRepoFullName() != "org/issues" {
		t.Errorf("expected org/issues, got %s", r.IssueRepoFullName())
	}

	// With empty IssueSource fields, falls back to main repo
	r.IssueSource = &IssueSource{Owner: "", Name: ""}
	if r.IssueRepoFullName() != "foo/bar" {
		t.Errorf("expected foo/bar for empty issue source, got %s", r.IssueRepoFullName())
	}

	// Partial IssueSource: only Name set, Owner inherited from main repo
	r.IssueSource = &IssueSource{Owner: "", Name: "other-repo"}
	if r.IssueRepoFullName() != "foo/other-repo" {
		t.Errorf("expected foo/other-repo for partial issue source, got %s", r.IssueRepoFullName())
	}

	// Partial IssueSource: only Owner set, Name inherited from main repo
	r.IssueSource = &IssueSource{Owner: "other-org", Name: ""}
	if r.IssueRepoFullName() != "other-org/bar" {
		t.Errorf("expected other-org/bar for partial issue source, got %s", r.IssueRepoFullName())
	}
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
				Assignee: "@me",
				State:    "open",
			},
		},
	}

	if err := Save(&cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if len(loaded.Repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(loaded.Repos))
	}

	repo := &loaded.Repos[0]
	if repo.FullName() != "myorg/myapp" {
		t.Errorf("unexpected repo: %s", repo.FullName())
	}
	if repo.IssueSource == nil {
		t.Fatal("expected issue_source to be set")
	}
	if repo.IssueRepoFullName() != "myorg/project-issues" {
		t.Errorf("unexpected issue repo: %s", repo.IssueRepoFullName())
	}
}
