// Package process manages background processes for Claude Code agents.
package process

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// StartBackground starts a command in the background with output redirected to logFile.
// The process runs in its own session (setsid) so it survives parent exit.
// Returns the process PID.
func StartBackground(dir, logFile, name string, args ...string) (int, error) {
	if err := os.MkdirAll(filepath.Dir(logFile), 0o750); err != nil {
		return 0, fmt.Errorf("create log directory: %w", err)
	}

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) //nolint:gosec // log path is derived from config dir
	if err != nil {
		return 0, fmt.Errorf("open log file %q: %w", logFile, err)
	}

	cmd := exec.CommandContext(context.Background(), name, args...) //nolint:gosec // command comes from user config
	cmd.Dir = dir
	cmd.Stdout = f
	cmd.Stderr = f
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		_ = f.Close()
		return 0, fmt.Errorf("start process: %w", err)
	}

	pid := cmd.Process.Pid

	// Reap the process in the background to avoid zombies.
	go func() {
		_ = cmd.Wait()
		_ = f.Close()
	}()

	return pid, nil
}

// IsRunning checks if a process with the given PID is still alive.
func IsRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// Kill sends SIGTERM to the process group for the given PID.
func Kill(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid %d", pid)
	}
	// Kill the entire process group (negative PID targets the group).
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		// Fall back to killing just the process itself.
		if err2 := syscall.Kill(pid, syscall.SIGTERM); err2 != nil {
			return fmt.Errorf("kill process %d: %w", pid, err2)
		}
	}
	return nil
}

// ReadLastLines reads the last n lines from a file by seeking near the end.
func ReadLastLines(path string, n int) (string, error) {
	f, err := os.Open(path) //nolint:gosec // log path is derived from config dir
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	const tailSize = 4096
	stat, err := f.Stat()
	if err != nil {
		return "", err
	}

	offset := stat.Size() - tailSize
	if offset < 0 {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return "", err
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n"), nil
}
