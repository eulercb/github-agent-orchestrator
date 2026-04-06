package repo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initGitRepo creates a bare git repo with a GitHub remote and returns its path.
func initGitRepo(t *testing.T, parentDir, name, remoteURL string) {
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
	run("remote", "add", "origin", remoteURL)
}

func TestDiscover(t *testing.T) {
	reposDir := t.TempDir()

	// Create repos with different remote formats.
	initGitRepo(t, reposDir, "https-repo", "https://github.com/acme/https-repo.git")
	initGitRepo(t, reposDir, "ssh-repo", "git@github.com:acme/ssh-repo.git")
	initGitRepo(t, reposDir, "no-dot-git", "https://github.com/acme/no-dot-git")

	// Create a non-GitHub repo (should be skipped).
	initGitRepo(t, reposDir, "gitlab-repo", "https://gitlab.com/acme/gitlab-repo.git")

	// Create a plain directory (not a git repo, should be skipped).
	require.NoError(t, os.MkdirAll(filepath.Join(reposDir, "not-a-repo"), 0o750))

	// Create a file (should be skipped).
	require.NoError(t, os.WriteFile(filepath.Join(reposDir, "somefile.txt"), []byte("hi"), 0o600))

	repos, err := Discover(reposDir)
	require.NoError(t, err)

	// Should find exactly the 3 GitHub repos.
	require.Len(t, repos, 3)

	// Build a map for easier assertions.
	byName := make(map[string]*Repo)
	for i := range repos {
		byName[repos[i].Name] = &repos[i]
	}

	// HTTPS remote
	r, ok := byName["https-repo"]
	require.True(t, ok, "https-repo not found")
	assert.Equal(t, "acme", r.Owner)
	assert.Equal(t, "acme/https-repo", r.FullName())
	assert.True(t, filepath.IsAbs(r.LocalPath), "LocalPath should be absolute")

	// SSH remote
	r, ok = byName["ssh-repo"]
	require.True(t, ok, "ssh-repo not found")
	assert.Equal(t, "acme", r.Owner)
	assert.Equal(t, "acme/ssh-repo", r.FullName())

	// HTTPS without .git suffix
	r, ok = byName["no-dot-git"]
	require.True(t, ok, "no-dot-git not found")
	assert.Equal(t, "acme", r.Owner)
	assert.Equal(t, "acme/no-dot-git", r.FullName())

	// Non-GitHub repos should not appear.
	_, ok = byName["gitlab-repo"]
	assert.False(t, ok, "gitlab-repo should not be discovered")
}

func TestDiscoverEmptyDir(t *testing.T) {
	repos, err := Discover(t.TempDir())
	require.NoError(t, err)
	assert.Empty(t, repos)
}

func TestDiscoverNonexistentDir(t *testing.T) {
	_, err := Discover(filepath.Join(t.TempDir(), "nope"))
	assert.Error(t, err)
}

func TestRemotePattern(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		owner   string
		repo    string
		matches bool
	}{
		{"https with .git", "https://github.com/acme/app.git", "acme", "app", true},
		{"https without .git", "https://github.com/acme/app", "acme", "app", true},
		{"ssh", "git@github.com:acme/app.git", "acme", "app", true},
		{"ssh without .git", "git@github.com:acme/app", "acme", "app", true},
		{"gitlab", "https://gitlab.com/acme/app.git", "", "", false},
		{"bitbucket", "git@bitbucket.org:acme/app.git", "", "", false},
		{"empty", "", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := remotePattern.FindStringSubmatch(tt.url)
			if tt.matches {
				require.Len(t, matches, 3, "expected 3 captures for %q", tt.url)
				assert.Equal(t, tt.owner, matches[1])
				assert.Equal(t, tt.repo, matches[2])
			} else {
				assert.Less(t, len(matches), 3, "should not match %q", tt.url)
			}
		})
	}
}
