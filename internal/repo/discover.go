// Package repo discovers Git repositories under a root directory and
// extracts their GitHub owner/name from the remote URL.
package repo

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// gitTimeout is the default timeout for git subprocesses.
const gitTimeout = 30 * time.Second

// Repo represents a discovered Git repository with its GitHub identity.
type Repo struct {
	Owner     string // GitHub owner/org
	Name      string // GitHub repo name
	LocalPath string // Absolute path on disk
}

// FullName returns "owner/name".
func (r *Repo) FullName() string {
	return r.Owner + "/" + r.Name
}

// remotePattern matches GitHub remote URLs in SSH and HTTPS forms:
//
//	git@github.com:owner/name.git
//	https://github.com/owner/name.git
//	https://github.com/owner/name
var remotePattern = regexp.MustCompile(`github\.com[:/]([^/]+)/([^/\s]+?)(?:\.git)?$`)

// Discover scans all immediate subdirectories of reposDir for Git
// repositories and returns their GitHub identity parsed from the origin
// remote. Non-git directories and repos without a GitHub remote are
// silently skipped.
func Discover(reposDir string) ([]Repo, error) {
	absReposDir, err := filepath.Abs(reposDir)
	if err != nil {
		return nil, fmt.Errorf("resolve repos dir %q: %w", reposDir, err)
	}

	entries, err := os.ReadDir(absReposDir)
	if err != nil {
		return nil, fmt.Errorf("read repos dir %q: %w", absReposDir, err)
	}

	var repos []Repo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirPath := filepath.Join(absReposDir, entry.Name())

		// Quick check: is this a git repo?
		if _, statErr := os.Stat(filepath.Join(dirPath, ".git")); statErr != nil {
			continue
		}

		owner, name, parseErr := parseGitHubRemote(dirPath)
		if parseErr != nil {
			continue
		}

		repos = append(repos, Repo{
			Owner:     owner,
			Name:      name,
			LocalPath: dirPath,
		})
	}

	return repos, nil
}

// parseGitHubRemote extracts the GitHub owner and name from the origin
// remote of the git repository at dir.
func parseGitHubRemote(dir string) (owner, name string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("git remote get-url origin: %w", err)
	}

	url := strings.TrimSpace(string(out))
	matches := remotePattern.FindStringSubmatch(url)
	if len(matches) < 3 {
		return "", "", fmt.Errorf("not a GitHub remote: %s", url)
	}

	return matches[1], matches[2], nil
}
