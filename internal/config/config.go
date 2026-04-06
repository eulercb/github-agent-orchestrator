// Package config handles loading and managing gao configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration for gao.
type Config struct {
	ReposDir    string        `yaml:"repos_dir"`
	TrackIssues bool          `yaml:"track_issues"`
	IssueFilter string        `yaml:"issue_filter"`
	Spawn       SpawnConfig   `yaml:"spawn"`
	StatusBar   StatusBar     `yaml:"status_bar"`
	Attach      AttachConfig  `yaml:"attach"`
	CCUsage     CCUsageConfig `yaml:"ccusage"`
	SessionDir  string        `yaml:"session_dir"`
}

// DefaultIssueFilter is the fallback issue filter used when no search query
// is configured. It shows open issues assigned to the current user.
const DefaultIssueFilter = "is:open assignee:@me"

// SpawnConfig controls how Claude Code sessions are created.
type SpawnConfig struct {
	Command     string `yaml:"command"`
	UseWorktree bool   `yaml:"use_worktree"`
	BaseBranch  string `yaml:"base_branch"`
}

// StatusBar configures the bottom status bar.
type StatusBar struct {
	Command string `yaml:"command"`
}

// AttachConfig controls how sessions are attached.
type AttachConfig struct {
	UseWarp *bool `yaml:"use_warp"`
}

// CCUsageConfig configures optional ccusage integration.
type CCUsageConfig struct {
	Enabled bool   `yaml:"enabled"`
	Command string `yaml:"command"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() Config {
	return Config{
		TrackIssues: true,
		IssueFilter: DefaultIssueFilter,
		Spawn: SpawnConfig{
			Command:     "claude --dangerously-skip-permissions",
			UseWorktree: true,
			BaseBranch:  "",
		},
		StatusBar: StatusBar{
			Command: "",
		},
		Attach: AttachConfig{},
		CCUsage: CCUsageConfig{
			Enabled: false,
			Command: "ccusage",
		},
		SessionDir: "",
	}
}

// expandTilde replaces a leading "~/" or bare "~" in a path with the user's
// home directory. Go's filepath functions do not perform shell-style tilde
// expansion, so this must be done explicitly.
func expandTilde(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("determine user home directory: %w", err)
		}
		return filepath.Join(home, path[1:]), nil
	}
	return path, nil
}

// ExpandReposDir resolves the repos directory path, expanding tildes.
// Returns an error if repos_dir is not configured.
func (c *Config) ExpandReposDir() (string, error) {
	if c.ReposDir == "" {
		return "", fmt.Errorf("repos_dir not configured")
	}
	return expandTilde(c.ReposDir)
}

// Dir returns the config directory path (~/.config/gao).
func Dir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("get user config dir: %w", err)
	}
	return filepath.Join(configDir, "gao"), nil
}

// Path returns the config file path.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// SessionsPath returns the path for the sessions state file.
// If sessionDir is non-empty, it is used instead of the default config dir.
func SessionsPath(sessionDir string) (string, error) {
	if sessionDir != "" {
		return filepath.Join(sessionDir, "sessions.yaml"), nil
	}
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sessions.yaml"), nil
}

// Load reads the config file, falling back to defaults.
func Load() (Config, error) {
	cfg := DefaultConfig()

	cfgPath, err := Path()
	if err != nil {
		return cfg, err
	}

	data, err := os.ReadFile(cfgPath) //nolint:gosec // config path is derived from user's config dir
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config: %w", err)
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}

	return cfg, nil
}

// Save writes the config to disk.
func Save(cfg *Config) error {
	cfgPath, err := Path()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o750); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	header := []byte("# gao - GitHub Agent Orchestrator configuration\n# See: https://github.com/eulercb/github-agent-orchestrator\n\n")
	return os.WriteFile(cfgPath, append(header, data...), 0o600)
}
