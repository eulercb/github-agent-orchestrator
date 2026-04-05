package process

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadLastLines(t *testing.T) {
	tests := []struct {
		name    string
		content string
		n       int
		expect  string
	}{
		{
			name:    "fewer lines than requested",
			content: "line1\nline2\n",
			n:       5,
			expect:  "line1\nline2",
		},
		{
			name:    "exact number of lines",
			content: "a\nb\nc\n",
			n:       3,
			expect:  "a\nb\nc",
		},
		{
			name:    "more lines than requested",
			content: "line1\nline2\nline3\nline4\nline5\n",
			n:       2,
			expect:  "line4\nline5",
		},
		{
			name:    "single line no newline",
			content: "only line",
			n:       3,
			expect:  "only line",
		},
		{
			name:    "empty file",
			content: "",
			n:       5,
			expect:  "",
		},
		{
			name:    "multi-byte characters",
			content: "こんにちは\n世界\nテスト\n",
			n:       2,
			expect:  "世界\nテスト",
		},
		{
			name:    "trailing whitespace lines",
			content: "content\n   \n\n",
			n:       5,
			expect:  "content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "test.log")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatal(err)
			}

			got, err := ReadLastLines(path, tt.n)
			if err != nil {
				t.Fatalf("ReadLastLines() error: %v", err)
			}
			if got != tt.expect {
				t.Errorf("ReadLastLines() = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestReadLastLinesNonexistentFile(t *testing.T) {
	_, err := ReadLastLines("/nonexistent/path/file.log", 5)
	if err == nil {
		t.Error("ReadLastLines() expected error for nonexistent file, got nil")
	}
}

func TestReadLastLinesLargeFile(t *testing.T) {
	// Create a file larger than tailSize (4096 bytes) to exercise the seek path.
	dir := t.TempDir()
	path := filepath.Join(dir, "large.log")

	var lines []string
	for i := range 200 {
		lines = append(lines, strings.Repeat("x", 30)+string(rune('A'+i%26)))
	}
	content := strings.Join(lines, "\n") + "\n"

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ReadLastLines(path, 3)
	if err != nil {
		t.Fatalf("ReadLastLines() error: %v", err)
	}

	resultLines := strings.Split(got, "\n")
	if len(resultLines) != 3 {
		t.Errorf("expected 3 lines, got %d: %q", len(resultLines), got)
	}
}
