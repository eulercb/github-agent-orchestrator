package process

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartBackgroundAndIsRunning(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "test.log")

	pid, err := StartBackground(dir, logFile, "sh", "-c", "sleep 30")
	if err != nil {
		t.Fatalf("StartBackground() error: %v", err)
	}
	if pid <= 0 {
		t.Fatalf("StartBackground() returned invalid pid: %d", pid)
	}

	// Process should be running.
	if !IsRunning(pid) {
		t.Errorf("IsRunning(%d) = false, want true", pid)
	}

	// Kill it and verify it stops.
	if err := Kill(pid); err != nil {
		t.Fatalf("Kill(%d) error: %v", pid, err)
	}

	// Wait briefly for the process to exit.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !IsRunning(pid) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if IsRunning(pid) {
		t.Errorf("IsRunning(%d) = true after Kill, want false", pid)
	}
}

func TestStartBackgroundWritesLog(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "test.log")

	pid, err := StartBackground(dir, logFile, "sh", "-c", "echo hello && echo world")
	if err != nil {
		t.Fatalf("StartBackground() error: %v", err)
	}

	// Wait for the short-lived process to finish.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !IsRunning(pid) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	got, err := ReadLastLines(logFile, 5)
	if err != nil {
		t.Fatalf("ReadLastLines() error: %v", err)
	}
	if !strings.Contains(got, "hello") || !strings.Contains(got, "world") {
		t.Errorf("log output = %q, want it to contain 'hello' and 'world'", got)
	}
}

func TestKillAlreadyDeadProcess(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "test.log")

	pid, err := StartBackground(dir, logFile, "sh", "-c", "true")
	if err != nil {
		t.Fatalf("StartBackground() error: %v", err)
	}

	// Wait for it to exit on its own.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !IsRunning(pid) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Killing an already-dead process should not return an error.
	if err := Kill(pid); err != nil {
		t.Errorf("Kill(%d) on dead process returned error: %v", pid, err)
	}
}

func TestIsRunningInvalidPID(t *testing.T) {
	if IsRunning(0) {
		t.Error("IsRunning(0) = true, want false")
	}
	if IsRunning(-1) {
		t.Error("IsRunning(-1) = true, want false")
	}
}

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
