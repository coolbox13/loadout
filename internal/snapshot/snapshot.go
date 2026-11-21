// Package snapshot copies instruction files into a git repository for
// history and backup. Live files stay where Claude Code reads them.
//
// Safety rules (DESIGN.md §7): whitelist only, JSON settings redacted,
// and a secret scan gates every commit with no bypass flag. Known false
// positives go in <repo>/.loadoutignore as path:rule:line fingerprints.
package snapshot

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/coolbox13/loadout/internal/config"
	"github.com/coolbox13/loadout/internal/discover"
	"github.com/coolbox13/loadout/internal/secrets"
)

var (
	globalFiles  = []string{"CLAUDE.md", "RTK.md", "keybindings.json", "statusline-command.sh"}
	globalDirs   = []string{"rules", "agents", "commands", "hooks", "skills", "output-styles"}
	globalJSON   = []string{"settings.json", "settings.local.json"}
	projectFiles = []string{"CLAUDE.md", "CLAUDE.local.md", filepath.Join(".claude", "CLAUDE.md")}
	projectDirs  = []string{filepath.Join(".claude", "rules"), filepath.Join(".claude", "commands"), filepath.Join(".claude", "agents")}
	projectJSON  = []string{filepath.Join(".claude", "settings.json")}
	// skipNames are never copied, even inside whitelisted directories:
	// "synced" holds cloud-managed skills, the rest are build or VCS output.
	skipNames = map[string]bool{"synced": true, ".git": true, "node_modules": true, "__pycache__": true, ".DS_Store": true}
)

// ignoreFile lists accepted false positives in the snapshot repo.
const ignoreFile = ".loadoutignore"

// Options configure one snapshot.
type Options struct {
	Config   config.Config
	Home     string
	Message  string                       // commit message; empty means no commit
	LookPath func(string) (string, error) // nil means exec.LookPath
}

// Result describes what a snapshot did.
type Result struct {
	Global    int
	Projects  int
	Scanner   string
	Committed bool
}

// Run snapshots, scans and optionally commits.
func Run(opts Options) (Result, error) {
	repo := opts.Config.Snapshot.Repo
	if opts.LookPath == nil {
		opts.LookPath = exec.LookPath
	}
	if _, err := os.Stat(filepath.Join(repo, ".git")); err != nil {
		return Result{}, fmt.Errorf("snapshot repo %s is not a git repository (git init it first)", repo)
	}
	for _, sub := range []string{"global", "projects"} {
		if err := os.RemoveAll(filepath.Join(repo, sub)); err != nil {
			return Result{}, fmt.Errorf("clear previous snapshot: %w", err)
		}
	}

	var res Result
	claudeDir := filepath.Join(opts.Home, ".claude")
	n, err := copySet(claudeDir, filepath.Join(repo, "global"), globalFiles, globalDirs, globalJSON)
	if err != nil {
		return res, err
	}
	res.Global = n

	projects := discover.Projects(discover.Options{
		Roots: opts.Config.Roots, Skip: opts.Config.SkipSet(), SkipPaths: []string{repo},
		ArchiveDirs: opts.Config.ArchiveDirs, MaxDepth: opts.Config.MaxDepth,
	})
	for _, p := range projects {
		rel, err := filepath.Rel(opts.Home, p.Dir)
		if err != nil || strings.HasPrefix(rel, "..") {
			rel = strings.TrimPrefix(p.Dir, string(filepath.Separator))
		}
		if _, err := copySet(p.Dir, filepath.Join(repo, "projects", rel), projectFiles, projectDirs, projectJSON); err != nil {
			return res, err
		}
	}
	res.Projects = len(projects)

	scanner, err := gate(repo, opts.LookPath)
	res.Scanner = scanner
	if err != nil {
		return res, err
	}
	if opts.Message != "" {
		res.Committed, err = commit(repo, opts.Message)
	}
	return res, err
}

func copySet(base, out string, files, dirs, jsons []string) (int, error) {
	n := 0
	for _, name := range files {
		if ok, err := copyFile(filepath.Join(base, name), filepath.Join(out, name)); err != nil {
			return n, err
		} else if ok {
			n++
		}
	}
	for _, name := range dirs {
		src := filepath.Join(base, name)
		if info, err := os.Stat(src); err != nil || !info.IsDir() {
			continue
		}
		if err := copyTree(src, filepath.Join(out, name)); err != nil {
			return n, err
		}
		n++
	}
	for _, name := range jsons {
		if ok, err := copyRedacted(filepath.Join(base, name), filepath.Join(out, name)); err != nil {
			return n, err
		} else if ok {
			n++
		}
	}
	return n, nil
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		if skipNames[d.Name()] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() && d.Type()&fs.ModeSymlink == 0 {
			return nil
		}
		rel, _ := filepath.Rel(src, path)
		_, err = copyFile(path, filepath.Join(dst, rel))
		return err
	})
}

func copyFile(src, dst string) (bool, error) {
	data, err := os.ReadFile(src)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		info, statErr := os.Stat(src)
		if statErr == nil && info.IsDir() {
			return false, nil
		}
		return false, fmt.Errorf("read %s: %w", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false, err
	}
	mode := fs.FileMode(0o644)
	if info, err := os.Stat(src); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(dst, data, mode); err != nil {
		return false, err
	}
	return true, os.Chmod(dst, mode) // WriteFile keeps an existing file's mode
}

func copyRedacted(src, dst string) (bool, error) {
	data, err := os.ReadFile(src)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", src, err)
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber() // float64 would silently corrupt large integers
	if err := dec.Decode(&v); err != nil {
		return false, fmt.Errorf("%s is not valid JSON; refusing to copy it unredacted: %w", src, err)
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // keep <REDACTED> readable instead of <…
	enc.SetIndent("", "  ")
	if err := enc.Encode(secrets.RedactJSON(v)); err != nil {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(dst, buf.Bytes(), 0o644)
}

// gate scans the snapshot with the built-in rules and, when installed,
// gitleaks. Any unignored hit aborts before commit.
func gate(repo string, lookPath func(string) (string, error)) (string, error) {
	ignored := loadIgnore(filepath.Join(repo, ignoreFile))
	var leaks []string
	for _, sub := range []string{"global", "projects"} {
		_ = filepath.WalkDir(filepath.Join(repo, sub), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil || isBinary(data) {
				return nil
			}
			rel, _ := filepath.Rel(repo, path)
			for _, h := range secrets.Scan(string(data)) {
				fp := rel + ":" + h.Rule + ":" + strconv.Itoa(h.Line)
				if !ignored[fp] {
					leaks = append(leaks, fp+" ("+h.Masked+")")
				}
			}
			return nil
		})
	}
	if len(leaks) > 0 {
		return "built-in", fmt.Errorf("secret scan found %d possible secret(s); nothing committed. Fix the source file, or add the fingerprint to %s if it is a false positive:\n  %s",
			len(leaks), ignoreFile, strings.Join(leaks, "\n  "))
	}
	if _, err := lookPath("gitleaks"); err != nil {
		return "built-in", nil
	}
	cmd := exec.Command("gitleaks", "dir", ".", "--no-banner", "--redact", "--exit-code", "1")
	cmd.Dir = repo // relative scan path so .gitleaksignore fingerprints match
	if out, err := cmd.CombinedOutput(); err != nil {
		return "built-in + gitleaks", fmt.Errorf("gitleaks found secrets; nothing committed:\n%s", out)
	}
	return "built-in + gitleaks", nil
}

func loadIgnore(path string) map[string]bool {
	m := map[string]bool{}
	f, err := os.Open(path)
	if err != nil {
		return m
	}
	defer func() { _ = f.Close() }() // read-only; a close error cannot lose data
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			m[line] = true
		}
	}
	return m
}

func isBinary(data []byte) bool {
	const sniff = 8000
	if len(data) > sniff {
		data = data[:sniff]
	}
	return strings.ContainsRune(string(data), 0)
}

// snapshotPaths are the only paths a snapshot commit may touch; anything
// else in the repo (notes, scripts, staged work) is the user's.
var snapshotPaths = []string{"global", "projects"}

func commit(repo, message string) (bool, error) {
	if out, err := git(repo, append([]string{"add", "-A", "--"}, snapshotPaths...)...); err != nil {
		return false, fmt.Errorf("git add: %v: %s", err, out)
	}
	if _, err := git(repo, append([]string{"diff", "--cached", "--quiet", "--"}, snapshotPaths...)...); err == nil {
		return false, nil
	}
	if out, err := git(repo, append([]string{"commit", "-q", "--only", "-m", message, "--"}, snapshotPaths...)...); err != nil {
		return false, fmt.Errorf("git commit: %v: %s", err, out)
	}
	return true, nil
}

func git(repo string, args ...string) ([]byte, error) {
	return exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
}
