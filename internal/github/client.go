// Package github wraps the gh CLI for interacting with GitHub issues and PRs.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/eulercb/github-agent-orchestrator/internal/config"
)

// Issue represents a GitHub issue.
type Issue struct {
	Number    int     `json:"number"`
	Title     string  `json:"title"`
	State     string  `json:"state"`
	URL       string  `json:"url"`
	Labels    []Label `json:"labels"`
	Assignees []User  `json:"assignees"`
	Body      string  `json:"body"`
	Author    User    `json:"author"`
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
	Number            int        `json:"number"`
	Title             string     `json:"title"`
	State             string     `json:"state"`
	URL               string     `json:"url"`
	Draft             bool       `json:"isDraft"`
	HeadRef           string     `json:"headRefName"`
	Mergeable         string     `json:"mergeable"`
	ReviewDecision    string     `json:"reviewDecision"`
	StatusCheckRollup []CheckRun `json:"statusCheckRollup"`
}

// CheckRun represents a CI check.
type CheckRun struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

// PRStatus summarizes the state of a PR for display.
type PRStatus struct {
	State             string // OPEN, MERGED, CLOSED
	Draft             bool
	Approved          bool
	CIPass            bool
	HasPendingReviews bool
}

// Client interacts with GitHub via the gh CLI.
type Client struct{}

// NewClient returns a new GitHub client.
func NewClient() *Client {
	return &Client{}
}

// ListIssues fetches issues for a repo with optional filters.
func (c *Client) ListIssues(repo *config.RepoConfig) ([]Issue, error) {
	args := []string{"issue", "list",
		"--repo", repo.FullName(),
		"--json", "number,title,state,url,labels,assignees,body,author",
		"--limit", "50",
	}

	if repo.Filters.Assignee != "" {
		args = append(args, "--assignee", repo.Filters.Assignee)
	}
	if repo.Filters.State != "" {
		args = append(args, "--state", repo.Filters.State)
	}
	for _, label := range repo.Filters.Labels {
		args = append(args, "--label", label)
	}

	out, err := runGH(args...)
	if err != nil {
		return nil, fmt.Errorf("list issues for %s: %w", repo.FullName(), err)
	}

	var issues []Issue
	if err := json.Unmarshal(out, &issues); err != nil {
		return nil, fmt.Errorf("parse issues: %w", err)
	}
	return issues, nil
}

// FindPRForBranch looks for a PR with the given head branch.
func (c *Client) FindPRForBranch(repoFullName, branch string) (*PullRequest, error) {
	args := []string{"pr", "list",
		"--repo", repoFullName,
		"--head", branch,
		"--json", "number,title,state,url,isDraft,headRefName,mergeable,reviewDecision",
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

	status := PRStatus{
		State: pr.State,
		Draft: pr.Draft,
	}

	status.Approved = pr.ReviewDecision == "APPROVED"
	status.HasPendingReviews = pr.ReviewDecision == "CHANGES_REQUESTED" ||
		pr.ReviewDecision == "REVIEW_REQUIRED"

	return status
}

func runGH(args ...string) ([]byte, error) {
	cmd := exec.CommandContext(context.Background(), "gh", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gh %s: %s", strings.Join(args, " "), string(exitErr.Stderr))
		}
		return nil, err
	}
	return out, nil
}
