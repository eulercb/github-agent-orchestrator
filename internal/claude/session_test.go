package claude

import (
	"testing"

	"github.com/stretchr/testify/assert"
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

func TestIssueNumberFromBranch(t *testing.T) {
	tests := []struct {
		branch string
		expect int
	}{
		{"claude/issue-42", 42},
		{"claude/issue-1", 1},
		{"claude/issue-999", 999},
		{"issue-7", 7},
		{"prefix/issue-10", 10},
		{"claude/issue-42-extra", 0},
		{"claude/issue-42-suffix", 0},
		{"main", 0},
		{"feature/something", 0},
		{"claude/issue-", 0},
		{"claude/issue-abc", 0},
	}

	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			got := issueNumberFromBranch(tt.branch)
			assert.Equal(t, tt.expect, got)
		})
	}
}
