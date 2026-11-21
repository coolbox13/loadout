package snapshot

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coolbox13/loadout/internal/config"
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

func setup(t *testing.T) (home, repo string, cfg config.Config) {
	t.Helper()
	home = t.TempDir()
	repo = filepath.Join(t.TempDir(), "backup")
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	write(t, filepath.Join(home, ".claude/CLAUDE.md"), "global\n")
	write(t, filepath.Join(home, ".claude/rules/a.md"), "rule\n")
	write(t, filepath.Join(home, ".claude/settings.json"), `{"env":{"OPENAI_API_KEY":"sk-proj-abcdefghijklmnopqrstu"},"model":"opus"}`)
	write(t, filepath.Join(home, ".claude/projects/transcript.jsonl"), "private transcript\n")
	write(t, filepath.Join(home, "Documents/app/CLAUDE.md"), "app rules\n")
	write(t, filepath.Join(home, "Documents/app/.claude/rules/r.md"), "r\n")
	write(t, filepath.Join(home, "Documents/app/src/main.go"), "package main\n")
	cfg = config.Default(home)
	cfg.Snapshot.Repo = repo
	return home, repo, cfg
}

func TestRunCopiesWhitelistAndRedactsSettings(t *testing.T) {
	home, repo, cfg := setup(t)

	res, err := Run(Options{Config: cfg, Home: home, LookPath: noGitleaks})

	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"global/CLAUDE.md", "global/rules/a.md", "projects/Documents/app/CLAUDE.md", "projects/Documents/app/.claude/rules/r.md"} {
		if _, err := os.Stat(filepath.Join(repo, rel)); err != nil {
			t.Errorf("missing %s", rel)
		}
	}
	for _, rel := range []string{"global/projects", "projects/Documents/app/src/main.go"} {
		if _, err := os.Stat(filepath.Join(repo, rel)); err == nil {
			t.Errorf("%s must not be copied", rel)
		}
	}
	settings, _ := os.ReadFile(filepath.Join(repo, "global/settings.json"))
	if strings.Contains(string(settings), "sk-proj") || !strings.Contains(string(settings), "<REDACTED>") {
		t.Fatalf("settings not redacted: %s", settings)
	}
	if res.Projects != 1 {
		t.Fatalf("projects = %d", res.Projects)
	}
}

func TestRunRefusesToCommitWhenAFileLeaksASecret(t *testing.T) {
	home, repo, cfg := setup(t)
	write(t, filepath.Join(home, "Documents/app/CLAUDE.md"), "DB: postgresql://u:Real9Pass77@db.host/app\n")

	_, err := Run(Options{Config: cfg, Home: home, Message: "snap", LookPath: noGitleaks})

	if err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("err = %v, want secret gate error", err)
	}
	if out, _ := exec.Command("git", "-C", repo, "log", "--oneline").CombinedOutput(); !strings.Contains(string(out), "does not have any commits") && len(strings.TrimSpace(string(out))) > 0 && !strings.Contains(string(out), "fatal") {
		t.Fatalf("a commit was made despite the gate: %s", out)
	}
}

func TestRunHonoursIgnoreFingerprints(t *testing.T) {
	home, repo, cfg := setup(t)
	write(t, filepath.Join(home, "Documents/app/CLAUDE.md"), "Example: postgresql://u:Real9Pass77@db.host/app\n")
	write(t, filepath.Join(repo, ".loadoutignore"), "# docs example\nprojects/Documents/app/CLAUDE.md:url-password:1\n")

	if _, err := Run(Options{Config: cfg, Home: home, LookPath: noGitleaks}); err != nil {
		t.Fatalf("ignored finding still blocked: %v", err)
	}
}

func TestRunCommitsAndReportsNoChangesOnSecondRun(t *testing.T) {
	home, repo, cfg := setup(t)
	gitIdentity(t, repo)

	first, err := Run(Options{Config: cfg, Home: home, Message: "snap", LookPath: noGitleaks})
	if err != nil || !first.Committed {
		t.Fatalf("first run: %+v %v", first, err)
	}
	second, err := Run(Options{Config: cfg, Home: home, Message: "snap", LookPath: noGitleaks})
	if err != nil || second.Committed {
		t.Fatalf("second run should be a no-op: %+v %v", second, err)
	}
}

func TestRunRejectsNonGitRepo(t *testing.T) {
	home, _, cfg := setup(t)
	cfg.Snapshot.Repo = t.TempDir()

	if _, err := Run(Options{Config: cfg, Home: home, LookPath: noGitleaks}); err == nil {
		t.Fatal("expected error for a snapshot repo that is not a git repository")
	}
}

func noGitleaks(string) (string, error) { return "", os.ErrNotExist }

func gitIdentity(t *testing.T, repo string) {
	t.Helper()
	for _, kv := range [][2]string{{"user.name", "test"}, {"user.email", "test@example.invalid"}} {
		if out, err := exec.Command("git", "-C", repo, "config", kv[0], kv[1]).CombinedOutput(); err != nil {
			t.Fatalf("git config: %v %s", err, out)
		}
	}
}

func TestRunPreservesModesAndLargeNumbers(t *testing.T) {
	home, repo, cfg := setup(t)
	script := filepath.Join(home, ".claude/hooks/run.sh")
	write(t, script, "#!/bin/sh\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(home, ".claude/settings.json"), `{"id":123456789012345678901,"timeout":10}`)

	if _, err := Run(Options{Config: cfg, Home: home, LookPath: noGitleaks}); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(repo, "global/hooks/run.sh"))
	if err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("executable bit lost: %v %v", info.Mode(), err)
	}
	settings, _ := os.ReadFile(filepath.Join(repo, "global/settings.json"))
	if !strings.Contains(string(settings), "123456789012345678901") || !strings.Contains(string(settings), `"timeout": 10`) {
		t.Fatalf("numbers changed: %s", settings)
	}
}

func TestRunCommitsOnlySnapshotPaths(t *testing.T) {
	home, repo, cfg := setup(t)
	gitIdentity(t, repo)
	write(t, filepath.Join(repo, "NOTES.md"), "user's own unrelated edit\n")

	if _, err := Run(Options{Config: cfg, Home: home, Message: "snap", LookPath: noGitleaks}); err != nil {
		t.Fatal(err)
	}

	out, _ := exec.Command("git", "-C", repo, "status", "--porcelain").CombinedOutput()
	if !strings.Contains(string(out), "NOTES.md") {
		t.Fatalf("unrelated file was committed; status: %q", out)
	}
}
