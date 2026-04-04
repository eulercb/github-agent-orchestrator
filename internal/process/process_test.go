package process

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartBackgroundAndIsRunning(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "test.log")

	pid, err := StartBackground(dir, logFile, "sh", "-c", "sleep 30")
	require.NoError(t, err)
	require.Positive(t, pid)

	// Process should be running.
	assert.True(t, IsRunning(pid), "process should be running")

	// Kill it and verify it stops.
	require.NoError(t, Kill(pid))

	// Wait briefly for the process to exit.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !IsRunning(pid) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	assert.False(t, IsRunning(pid), "process should not be running after Kill")
}

func TestStartBackgroundWritesLog(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "test.log")

	pid, err := StartBackground(dir, logFile, "sh", "-c", "echo hello && echo world")
	require.NoError(t, err)

	// Wait for the short-lived process to finish.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !IsRunning(pid) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	got, err := ReadLastLines(logFile, 5)
	require.NoError(t, err)
	assert.Contains(t, got, "hello")
	assert.Contains(t, got, "world")
}

func TestKillAlreadyDeadProcess(t *testing.T) {
	dir := t.TempDir()
	logFile := filepath.Join(dir, "test.log")

	pid, err := StartBackground(dir, logFile, "sh", "-c", "true")
	require.NoError(t, err)

	// Wait for it to exit on its own.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !IsRunning(pid) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Killing an already-dead process should not return an error.
	assert.NoError(t, Kill(pid))
}

func TestIsRunningInvalidPID(t *testing.T) {
	assert.False(t, IsRunning(0))
	assert.False(t, IsRunning(-1))
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
			require.NoError(t, os.WriteFile(path, []byte(tt.content), 0o600))

			got, err := ReadLastLines(path, tt.n)
			require.NoError(t, err)
			assert.Equal(t, tt.expect, got)
		})
	}
}

func TestReadLastLinesNonexistentFile(t *testing.T) {
	_, err := ReadLastLines("/nonexistent/path/file.log", 5)
	assert.Error(t, err)
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

	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	got, err := ReadLastLines(path, 3)
	require.NoError(t, err)

	resultLines := strings.Split(got, "\n")
	assert.Len(t, resultLines, 3)
}
