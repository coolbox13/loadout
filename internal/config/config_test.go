package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nope.yaml"), "/h")

	if err != nil || !reflect.DeepEqual(cfg, Default("/h")) {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
}

func TestLoadOverlaysAndExpandsHome(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(p, []byte("roots: [\"~/code\"]\nbudgets:\n  claude_md_lines: 150\nchecks:\n  disable: [d7]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(p, "/h")

	if err != nil {
		t.Fatal(err)
	}
	if cfg.Roots[0] != "/h/code" || cfg.Budgets.ClaudeMDLines != 150 || cfg.Budgets.ProjectAlwaysTokens != 8000 {
		t.Fatalf("cfg = %+v", cfg)
	}
	if cfg.Enabled("D7") || !cfg.Enabled("D5") {
		t.Fatal("disable list not applied case-insensitively")
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(p, []byte("max_depth: 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(p, "/h"); err == nil {
		t.Fatal("expected validation error")
	}
}
