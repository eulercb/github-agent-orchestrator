// Package github wraps the gh CLI for interacting with GitHub issues and PRs.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
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
	Number         int        `json:"number"`
	Title          string     `json:"title"`
	State          string     `json:"state"`
	URL            string     `json:"url"`
	Draft          bool       `json:"isDraft"`
	HeadRef        string     `json:"headRefName"`
	ReviewDecision string     `json:"reviewDecision"`
	Author         User       `json:"author"`
	Labels         []Label    `json:"labels"`
	Repository     Repository `json:"-"` // set by caller, not from JSON
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
		query = "is:open assignee:@me"
	}
	if !hasTypeQualifier(query) {
		query = "is:issue " + query
	}
	// Split the query into individual terms so each qualifier is passed as
	// a separate positional argument to gh (gh search issues is:open is:issue ...)
	// rather than a single quoted string. Quoted segments like
	// label:"good first issue" are preserved as a single argument.
	args := []string{"search", "issues"}
	args = append(args, splitQuery(query)...)
	args = append(args,
		"--json", "number,title,state,url,labels,assignees,body,author,repository",
		"--limit", "50",
	)

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

// splitQuery splits a search query into tokens, preserving quoted segments.
// For example: `is:open label:"good first issue" assignee:@me` becomes
// ["is:open", `label:"good first issue"`, "assignee:@me"].
func splitQuery(query string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := false

	for _, r := range query {
		switch {
		case r == '"':
			inQuote = !inQuote
			current.WriteRune(r)
		case r == ' ' && !inQuote:
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
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

// ListPRs fetches pull requests for the given repositories using "gh pr list".
// Results from all repos are merged. The search parameter is passed to --search
// when non-empty. Each returned PR has its Repository field set.
func (c *Client) ListPRs(repos []string, search string) ([]PullRequest, error) {
	var allPRs []PullRequest
	var firstErr error
	for _, repoFullName := range repos {
		args := []string{"pr", "list",
			"--repo", repoFullName,
			"--state", "open",
			"--json", "number,title,state,url,isDraft,headRefName,reviewDecision,author,labels",
			"--limit", "50",
		}
		if search != "" {
			args = append(args, "--search", search)
		}

		out, err := runGH(args...)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("list PRs for %s: %w", repoFullName, err)
			}
			continue
		}

		var prs []PullRequest
		if err := json.Unmarshal(out, &prs); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("parse PR list for %s: %w", repoFullName, err)
			}
			continue
		}
		for i := range prs {
			prs[i].Repository = Repository{NameWithOwner: repoFullName}
		}
		allPRs = append(allPRs, prs...)
	}
	if len(allPRs) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return allPRs, nil
}

// FindPRForIssue searches for a PR in prRepo that references the given issue.
// Uses GitHub search to find PRs that reference the issue number.
func (c *Client) FindPRForIssue(prRepo, issueRepo string, issueNumber int) (*PullRequest, error) {
	// GitHub's linked:issue search qualifier isn't available via gh, so we
	// search for PRs mentioning the issue number. For cross-repo references
	// the full "owner/repo#N" form is used.
	var searchTerm string
	if issueRepo == prRepo {
		searchTerm = fmt.Sprintf("%d", issueNumber)
	} else {
		searchTerm = fmt.Sprintf("%s#%d", issueRepo, issueNumber)
	}

	args := []string{"pr", "list",
		"--repo", prRepo,
		"--state", "all",
		"--search", searchTerm,
		"--json", "number,title,state,url,isDraft,headRefName,reviewDecision,author,labels",
		"--limit", "1",
	}

	out, err := runGH(args...)
	if err != nil {
		return nil, fmt.Errorf("find PR for issue %s#%d: %w", issueRepo, issueNumber, err)
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

// FindPRForBranch looks for a PR with the given head branch.
func (c *Client) FindPRForBranch(repoFullName, branch string) (*PullRequest, error) {
	args := []string{"pr", "list",
		"--repo", repoFullName,
		"--head", branch,
		"--state", "all",
		"--json", "number,title,state,url,isDraft,headRefName,reviewDecision,author,labels",
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

// LinkedIssue represents a GitHub issue linked to a pull request via
// closing references (e.g. "Closes #42" in the PR body).
type LinkedIssue struct {
	Number     int    `json:"number"`
	Title      string `json:"title"`
	Repository string `json:"repository"` // "owner/name"
}

// FindLinkedIssue looks up the first issue linked to the PR on a given branch
// using the GitHub GraphQL API. Returns nil when no PR or linked issue is found.
func (c *Client) FindLinkedIssue(repoFullName, branch string) (*LinkedIssue, error) {
	parts := strings.SplitN(repoFullName, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo name %q", repoFullName)
	}
	owner, name := parts[0], parts[1]

	query := fmt.Sprintf(`{
  repository(owner: %q, name: %q) {
    pullRequests(headRefName: %q, first: 1, orderBy: {field: UPDATED_AT, direction: DESC}) {
      nodes {
        closingIssuesReferences(first: 1) {
          nodes {
            number
            title
            repository { nameWithOwner }
          }
        }
      }
    }
  }
}`, owner, name, branch)

	out, err := runGH("api", "graphql", "-f", "query="+query)
	if err != nil {
		return nil, fmt.Errorf("graphql linked issues: %w", err)
	}

	var result struct {
		Data struct {
			Repository struct {
				PullRequests struct {
					Nodes []struct {
						ClosingIssuesReferences struct {
							Nodes []struct {
								Number     int    `json:"number"`
								Title      string `json:"title"`
								Repository struct {
									NameWithOwner string `json:"nameWithOwner"`
								} `json:"repository"`
							} `json:"nodes"`
						} `json:"closingIssuesReferences"`
					} `json:"nodes"`
				} `json:"pullRequests"`
			} `json:"repository"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse graphql response: %w", err)
	}
	if len(result.Errors) > 0 {
		var messages []string
		for _, gqlErr := range result.Errors {
			if gqlErr.Message != "" {
				messages = append(messages, gqlErr.Message)
			}
		}
		if len(messages) == 0 {
			return nil, fmt.Errorf("graphql linked issues: unknown error")
		}
		return nil, fmt.Errorf("graphql linked issues: %s", strings.Join(messages, "; "))
	}

	prs := result.Data.Repository.PullRequests.Nodes
	if len(prs) == 0 {
		return nil, nil
	}
	issues := prs[0].ClosingIssuesReferences.Nodes
	if len(issues) == 0 {
		return nil, nil
	}

	return &LinkedIssue{
		Number:     issues[0].Number,
		Title:      issues[0].Title,
		Repository: issues[0].Repository.NameWithOwner,
	}, nil
}

// OpenInBrowser opens an issue or PR in the default browser using gh browse.
func (c *Client) OpenInBrowser(repo string, number int) error {
	_, err := runGH("browse", strconv.Itoa(number), "--repo", repo)
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
