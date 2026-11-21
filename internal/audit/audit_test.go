package audit

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coolbox13/loadout/internal/config"
	"github.com/coolbox13/loadout/internal/loadgraph"
	"github.com/coolbox13/loadout/internal/model"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func noBinaries(string) (string, error) { return "", errors.New("not found") }

func run(t *testing.T, home string) Result {
	t.Helper()
	cfg := config.Default(home)
	cfg.Snapshot.Repo = filepath.Join(home, "Documents", "backup")
	env := loadgraph.Env{Home: home, ClaudeDir: filepath.Join(home, ".claude"), Skip: cfg.SkipSet()}
	return Run(Options{Config: cfg, Env: env, LookPath: noBinaries})
}

func all(r Result) []model.Finding {
	out := append([]model.Finding(nil), r.Global.Findings...)
	for _, p := range r.Projects {
		out = append(out, p.Findings...)
	}
	return out
}

func TestRunSkipsSnapshotRepoAndDedupesGlobalFindings(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, ".claude/CLAUDE.md"), "@missing-global.md\n")
	write(t, filepath.Join(home, "Documents/a/CLAUDE.md"), "a\n")
	write(t, filepath.Join(home, "Documents/b/CLAUDE.md"), "b\n")
	write(t, filepath.Join(home, "Documents/backup/projects/Documents/a/CLAUDE.md"), "copy\n")

	r := run(t, home)

	if len(r.Projects) != 2 {
		t.Fatalf("projects = %d, want 2 (snapshot repo excluded)", len(r.Projects))
	}
	n := 0
	for _, f := range all(r) {
		if strings.Contains(f.Message, "missing-global.md") {
			n++
		}
	}
	if n != 1 || len(r.Global.Findings) == 0 {
		t.Fatalf("global dangling import reported %d times; want once, under global", n)
	}
}

func TestRunDetectsFrameworkOnlyFromWiring(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, "Documents/wired/CLAUDE.md"), "@.wolf/OPENWOLF.md\n")
	write(t, filepath.Join(home, "Documents/wired/.wolf/OPENWOLF.md"), "protocol\n")
	write(t, filepath.Join(home, "Documents/mention/CLAUDE.md"), "plain\n")
	write(t, filepath.Join(home, "Documents/mention/.wolf/x"), "data\n")
	write(t, filepath.Join(home, "Documents/mention/.claude/skills/s/SKILL.md"), "---\nname: s\ndescription: d\n---\nRead .wolf/anatomy.md first.\n")

	r := run(t, home)

	for _, p := range r.Projects {
		has := false
		for _, f := range p.Findings {
			has = has || (f.Check == "D4" && strings.Contains(f.Message, "OpenWolf"))
		}
		want := strings.HasSuffix(p.Dir, "wired")
		if has != want {
			t.Errorf("%s: OpenWolf finding = %v, want %v", p.Dir, has, want)
		}
	}
}

func TestRunReportsSettingsSecretsWithoutValues(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, ".claude/settings.json"), `{"env":{"OPENAI_API_KEY":"sk-proj-abcdefghijklmnopqrstuv"}}`)
	write(t, filepath.Join(home, "Documents/a/CLAUDE.md"), "a\n")

	r := run(t, home)

	found := false
	for _, f := range r.Global.Findings {
		if f.Check == "D5" && strings.Contains(f.Message, "env.OPENAI_API_KEY") {
			found = true
			if strings.Contains(f.Message, "sk-proj") {
				t.Fatal("finding leaks the secret value")
			}
		}
	}
	if !found {
		t.Fatalf("no settings secret finding: %+v", r.Global.Findings)
	}
}
