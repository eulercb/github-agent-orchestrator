package claude

import (
	"testing"
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
			if got != tt.expect {
				t.Errorf("extractLastActivity() = %q, want %q", got, tt.expect)
			}
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
			if got != tt.expect {
				t.Errorf("isWaitingForInput() = %v, want %v", got, tt.expect)
			}
		})
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{"simple", "'simple'"},
		{"with space", "'with space'"},
		{"it's", "'it'\"'\"'s'"},
	}

	for _, tt := range tests {
		got := shellQuote(tt.input)
		if got != tt.expect {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.input, got, tt.expect)
		}
	}
}
