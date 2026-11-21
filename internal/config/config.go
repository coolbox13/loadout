// Package config loads ~/.config/loadout/config.yaml over built-in defaults.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the full user configuration. Every field has a default.
type Config struct {
	Roots       []string `yaml:"roots"`
	Exclude     []string `yaml:"exclude"`
	ArchiveDirs []string `yaml:"archive_dirs"`
	MaxDepth    int      `yaml:"max_depth"`
	Budgets     Budgets  `yaml:"budgets"`
	Checks      Checks   `yaml:"checks"`
	Snapshot    Snapshot `yaml:"snapshot"`
}

// Budgets are the thresholds for D1 and D2.
type Budgets struct {
	ClaudeMDLines       int `yaml:"claude_md_lines"`
	ProjectAlwaysTokens int `yaml:"project_always_tokens"`
}

// Checks tunes individual checks.
type Checks struct {
	Disable         []string `yaml:"disable"`
	ShoutingPerFile int      `yaml:"shouting_per_file"`
}

// Snapshot configures the backup repository.
type Snapshot struct {
	Repo string `yaml:"repo"`
}

// Default returns the configuration used when no file exists.
func Default(home string) Config {
	return Config{
		Roots:       []string{filepath.Join(home, "Documents"), filepath.Join(home, "code"), filepath.Join(home, "src"), filepath.Join(home, "projects")},
		Exclude:     []string{"node_modules", "vendor", ".venv", "venv", "dist", "build", "graphify-out", "target", "__pycache__"},
		ArchiveDirs: []string{"archief", "archive", "archived"},
		MaxDepth:    7,
		Budgets:     Budgets{ClaudeMDLines: 200, ProjectAlwaysTokens: 8000},
		Checks:      Checks{ShoutingPerFile: 5},
		Snapshot:    Snapshot{Repo: filepath.Join(home, "claude-snapshots")},
	}
}

// DefaultPath is where Load looks when no path is given.
func DefaultPath(home string) string {
	return filepath.Join(home, ".config", "loadout", "config.yaml")
}

// Load reads path over the defaults. A missing file is not an error.
func Load(path, home string) (Config, error) {
	cfg := Default(home)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	for i, r := range cfg.Roots {
		cfg.Roots[i] = expandHome(r, home)
	}
	cfg.Snapshot.Repo = expandHome(cfg.Snapshot.Repo, home)
	return cfg, cfg.validate()
}

func (c Config) validate() error {
	switch {
	case len(c.Roots) == 0:
		return errors.New("config: roots must list at least one directory")
	case c.MaxDepth < 1:
		return errors.New("config: max_depth must be at least 1")
	case c.Budgets.ClaudeMDLines < 1 || c.Budgets.ProjectAlwaysTokens < 1:
		return errors.New("config: budgets must be positive")
	}
	return nil
}

// Enabled reports whether a check ID (e.g. "D7") is switched on.
func (c Config) Enabled(check string) bool {
	for _, d := range c.Checks.Disable {
		if strings.EqualFold(d, check) {
			return false
		}
	}
	return true
}

// SkipSet returns Exclude as a lookup set.
func (c Config) SkipSet() map[string]bool {
	m := make(map[string]bool, len(c.Exclude))
	for _, e := range c.Exclude {
		m[e] = true
	}
	return m
}

func expandHome(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}
