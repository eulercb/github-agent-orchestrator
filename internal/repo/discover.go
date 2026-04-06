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
	"sync"
	"time"
)

// gitTimeout is the default timeout for git subprocesses.
const gitTimeout = 30 * time.Second

// maxParallel limits the number of concurrent git subprocesses to avoid
// overwhelming the system when repos_dir contains many repositories.
const maxParallel = 8

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

	// Filter to git directories first (cheap stat check).
	var gitDirs []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirPath := filepath.Join(absReposDir, entry.Name())
		if _, statErr := os.Stat(filepath.Join(dirPath, ".git")); statErr != nil {
			continue
		}
		gitDirs = append(gitDirs, dirPath)
	}

	// Parse GitHub remotes concurrently with bounded parallelism.
	// Each goroutine writes to its own slot so no mutex is needed.
	type repoResult struct {
		repo Repo
		ok   bool
	}
	results := make([]repoResult, len(gitDirs))
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	wg.Add(len(gitDirs))
	for i, dirPath := range gitDirs {
		go func(idx int, dir string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			owner, name, parseErr := parseGitHubRemote(dir)
			if parseErr != nil {
				return
			}
			results[idx] = repoResult{
				repo: Repo{Owner: owner, Name: name, LocalPath: dir},
				ok:   true,
			}
		}(i, dirPath)
	}
	wg.Wait()

	// Collect successful results in original directory order.
	repos := make([]Repo, 0, len(results))
	for _, r := range results {
		if r.ok {
			repos = append(repos, r.repo)
		}
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
