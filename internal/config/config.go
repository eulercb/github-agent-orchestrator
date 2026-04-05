// Package config handles loading and managing gao configuration.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration for gao.
type Config struct {
	Repos      []RepoConfig  `yaml:"repos"`
	Spawn      SpawnConfig   `yaml:"spawn"`
	StatusBar  StatusBar     `yaml:"status_bar"`
	Attach     AttachConfig  `yaml:"attach"`
	CCUsage    CCUsageConfig `yaml:"ccusage"`
	SessionDir string        `yaml:"session_dir"`
}

// RepoConfig describes a GitHub repository and its issue filters.
type RepoConfig struct {
	Owner       string       `yaml:"owner"`
	Name        string       `yaml:"name"`
	IssueSource *IssueSource `yaml:"issue_source,omitempty"`
	Filters     IssueFilters `yaml:"filters"`
}

// IssueSource specifies a different repository from which to fetch issues.
// When set, issues are fetched from this repo instead of the main repo.
type IssueSource struct {
	Owner string `yaml:"owner"`
	Name  string `yaml:"name"`
}

// FullName returns "owner/name" for the PR/session repo.
func (r *RepoConfig) FullName() string {
	return r.Owner + "/" + r.Name
}

// IssueRepoFullName returns the repo to fetch issues from.
// If IssueSource is configured, non-empty fields override the main repo;
// otherwise it falls back to the main repo.
func (r *RepoConfig) IssueRepoFullName() string {
	if r.IssueSource == nil {
		return r.FullName()
	}

	owner := r.Owner
	name := r.Name

	if r.IssueSource.Owner != "" {
		owner = r.IssueSource.Owner
	}
	if r.IssueSource.Name != "" {
		name = r.IssueSource.Name
	}

	return owner + "/" + name
}

// IssueFilters controls which issues are shown.
// When Search is set, it is passed to "gh issue list --search" and the individual
// filter fields (Assignee, Labels, State) are ignored because GitHub's search
// syntax subsumes them (e.g. "is:open assignee:@me label:bug").
type IssueFilters struct {
	Assignee string   `yaml:"assignee"`
	Labels   []string `yaml:"labels"`
	State    string   `yaml:"state"`
	Search   string   `yaml:"search,omitempty"`
}

// SpawnConfig controls how Claude Code sessions are created.
type SpawnConfig struct {
	Command     string `yaml:"command"`
	UseWorktree bool   `yaml:"use_worktree"`
	RepoDir     string `yaml:"repo_dir"`
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
		Repos: []RepoConfig{},
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
