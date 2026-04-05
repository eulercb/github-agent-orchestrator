// Package github wraps the gh CLI for interacting with GitHub issues and PRs.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/eulercb/github-agent-orchestrator/internal/config"
)

// Issue represents a GitHub issue.
type Issue struct {
	Number     int        `json:"number"`
	Title      string     `json:"title"`
	State      string     `json:"state"`
	URL        string     `json:"url"`
	Labels     []Label    `json:"labels"`
	Assignees  []User     `json:"assignees"`
	Body       string     `json:"body"`
	Author     User       `json:"author"`
	Repository Repository `json:"repository"`
}

// Repository identifies a GitHub repository in search results.
type Repository struct {
	NameWithOwner string `json:"nameWithOwner"`
}

// Label represents a GitHub label.
type Label struct {
	Name string `json:"name"`
}

// User represents a GitHub user.
type User struct {
	Login string `json:"login"`
}

// PullRequest represents a GitHub pull request.
type PullRequest struct {
	Number         int    `json:"number"`
	Title          string `json:"title"`
	State          string `json:"state"`
	URL            string `json:"url"`
	Draft          bool   `json:"isDraft"`
	HeadRef        string `json:"headRefName"`
	ReviewDecision string `json:"reviewDecision"`
}

// PRStatus summarizes the state of a PR for display.
type PRStatus struct {
	State            string // OPEN, MERGED, CLOSED
	Draft            bool
	Approved         bool
	ChangesRequested bool
	ReviewRequired   bool
}

// Client interacts with GitHub via the gh CLI.
type Client struct{}

// NewClient returns a new GitHub client.
func NewClient() *Client {
	return &Client{}
}

// ListIssues fetches issues using "gh search issues" with the given search
// query. The query can span multiple repos via qualifiers like
// "repo:org/a repo:org/b is:open". Automatically adds "is:issue" if no
// type qualifier is present. No repo scoping is applied beyond what the
// query itself contains.
func (c *Client) ListIssues(search string) ([]Issue, error) {
	query := search
	if query == "" {
		query = config.DefaultSearch
	}
	if !hasTypeQualifier(query) {
		query = "is:issue " + query
	}
	args := []string{"search", "issues",
		query,
		"--json", "number,title,state,url,labels,assignees,body,author,repository",
		"--limit", "50",
	}

	out, err := runGH(args...)
	if err != nil {
		return nil, fmt.Errorf("search issues: %w", err)
	}

	var issues []Issue
	if err := json.Unmarshal(out, &issues); err != nil {
		return nil, fmt.Errorf("parse search results: %w", err)
	}
	return issues, nil
}

// hasTypeQualifier returns true if the query already contains a qualifier
// that distinguishes issues from PRs (e.g. "is:issue", "is:pr", "type:issue").
func hasTypeQualifier(query string) bool {
	lower := strings.ToLower(query)
	for _, q := range []string{"is:issue", "is:pr", "type:issue", "type:pr"} {
		if strings.Contains(lower, q) {
			return true
		}
	}
	return false
}

// FindPRForBranch looks for a PR with the given head branch.
func (c *Client) FindPRForBranch(repoFullName, branch string) (*PullRequest, error) {
	args := []string{"pr", "list",
		"--repo", repoFullName,
		"--head", branch,
		"--state", "all",
		"--json", "number,title,state,url,isDraft,headRefName,reviewDecision",
		"--limit", "1",
	}

	out, err := runGH(args...)
	if err != nil {
		return nil, fmt.Errorf("find PR for branch %s: %w", branch, err)
	}

	var prs []PullRequest
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, fmt.Errorf("parse PRs: %w", err)
	}

	if len(prs) == 0 {
		return nil, nil
	}
	return &prs[0], nil
}

// GetPRStatus returns a summarized PR status.
func (c *Client) GetPRStatus(pr *PullRequest) PRStatus {
	if pr == nil {
		return PRStatus{}
	}

	return PRStatus{
		State:            pr.State,
		Draft:            pr.Draft,
		Approved:         pr.ReviewDecision == "APPROVED",
		ChangesRequested: pr.ReviewDecision == "CHANGES_REQUESTED",
		ReviewRequired:   pr.ReviewDecision == "REVIEW_REQUIRED",
	}
}

// OpenInBrowser opens a URL in the default browser using gh browse.
func (c *Client) OpenInBrowser(url string) error {
	_, err := runGH("browse", "--url", url)
	return err
}

func runGH(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gh %s: %s: %w", strings.Join(args, " "), string(exitErr.Stderr), err)
		}
		return nil, err
	}
	return out, nil
}
