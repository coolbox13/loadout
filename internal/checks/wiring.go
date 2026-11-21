package checks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/coolbox13/loadout/internal/model"
)

type settingsFile struct {
	Hooks map[string][]struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"hooks"`
	} `json:"hooks"`
}

var (
	pathTokenRe = regexp.MustCompile(`(?:^|[\s"'=])(/[^\s"';|&)*]+\.[A-Za-z0-9]{1,5})\b`)
	binaryRe    = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// Hooks (D4) checks hook commands in a settings file: a referenced script
// that no longer exists, or a binary that is not on PATH, means the hook
// silently does nothing (or fails) on every matching event.
func Hooks(path string, data []byte, projectDir, home string, lookPath func(string) (string, error)) []model.Finding {
	var s settingsFile
	if err := json.Unmarshal(data, &s); err != nil {
		return []model.Finding{{Check: "D4", Severity: model.Warn, Path: path, Message: "settings file is not valid JSON; Claude Code may ignore it"}}
	}
	var out []model.Finding
	for _, event := range sortedKeys(s.Hooks) {
		for _, group := range s.Hooks[event] {
			for _, h := range group.Hooks {
				if h.Type != "command" {
					continue
				}
				out = append(out, checkCommand(path, event, expand(h.Command, projectDir, home), lookPath)...)
			}
		}
	}
	return out
}

func checkCommand(path, event, cmd string, lookPath func(string) (string, error)) []model.Finding {
	var out []model.Finding
	for _, m := range pathTokenRe.FindAllStringSubmatch(cmd, -1) {
		if strings.Contains(m[1], "$") {
			continue // unexpanded variable; can't judge
		}
		if _, err := os.Stat(m[1]); err != nil {
			out = append(out, model.Finding{Check: "D4", Severity: model.Error, Path: path,
				Message: fmt.Sprintf("%s hook runs %s, which does not exist", event, m[1])})
		}
	}
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return out
	}
	bin := strings.Trim(fields[0], `"'`)
	if binaryRe.MatchString(bin) && !shellWords[bin] {
		if _, err := lookPath(bin); err != nil {
			out = append(out, model.Finding{Check: "D4", Severity: model.Warn, Path: path,
				Message: fmt.Sprintf("%s hook calls %q, which is not on PATH", event, bin)})
		}
	}
	return out
}

func expand(cmd, projectDir, home string) string {
	r := strings.NewReplacer("${HOME}", home, "$HOME", home, "~/", home+"/")
	cmd = r.Replace(cmd)
	if projectDir != "" {
		cmd = strings.NewReplacer("${CLAUDE_PROJECT_DIR}", projectDir, "$CLAUDE_PROJECT_DIR", projectDir).Replace(cmd)
	}
	return cmd
}

// framework is the signature of a tool that wires itself into Claude Code.
type framework struct {
	Name    string
	Binary  string
	DataDir string
	Marker  string
}

// knownFrameworks lists tools seen leaving wiring behind after uninstall.
var knownFrameworks = []framework{
	{Name: "OpenWolf", Binary: "openwolf", DataDir: ".wolf", Marker: ".wolf/"},
}

// Frameworks (D4) flags a known framework that is still wired into a
// project (imports, rules, hooks) while its CLI is no longer installed.
// texts maps file path to content for every instruction and settings file.
func Frameworks(projectDir string, texts map[string]string, lookPath func(string) (string, error)) []model.Finding {
	var out []model.Finding
	for _, fw := range knownFrameworks {
		if _, err := os.Stat(filepath.Join(projectDir, fw.DataDir)); err != nil {
			continue
		}
		if _, err := lookPath(fw.Binary); err == nil {
			continue
		}
		var places []string
		for _, p := range sortedKeys(texts) {
			if strings.Contains(texts[p], fw.Marker) {
				places = append(places, filepath.Base(p))
			}
		}
		if len(places) == 0 {
			continue
		}
		out = append(out, model.Finding{Check: "D4", Severity: model.Warn, Path: projectDir,
			Message: fmt.Sprintf("%s is wired in %d places (%s) but %q is not installed; remove the wiring, harvest %s/ learnings first",
				fw.Name, len(places), strings.Join(places, ", "), fw.Binary, fw.DataDir)})
	}
	return out
}

// shellWords start valid hook commands without being binaries on PATH.
var shellWords = set("case", "if", "for", "while", "until", "do", "then", "exec", "cd", "echo", "printf",
	"test", "true", "false", "exit", "export", "set", "source", "read", "eval", "command", "builtin", "[", "{", "(")

// loadedDirs are ~/.claude subdirectories Claude Code reads instructions from;
// runtimeDirs hold its own state. Anything else with Markdown is dead weight.
var (
	loadedDirs  = set("rules", "agents", "commands", "skills", "output-styles", "hooks", "plugins")
	runtimeDirs = set("projects", "todos", "shell-snapshots", "statsig", "ide", "debug", "file-history", "cache",
		"sessions", "session-env", "session-data", "paste-cache", "telemetry", "downloads", "backups", "state",
		"chrome", "worktrees", "metrics", "logs", "tasks", "plans", "local")
)

// DeadWeight (D6) reports ~/.claude subdirectories holding Markdown that no
// load path reaches, typically left by framework installers.
func DeadWeight(claudeDir string) []model.Finding {
	entries, err := os.ReadDir(claudeDir)
	if err != nil {
		return nil
	}
	var out []model.Finding
	for _, e := range entries {
		if !e.IsDir() || loadedDirs[e.Name()] || runtimeDirs[e.Name()] || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(claudeDir, e.Name())
		if n := countMarkdown(dir); n > 0 {
			out = append(out, model.Finding{Check: "D6", Severity: model.Info, Path: dir,
				Message: fmt.Sprintf("%d Markdown files that Claude Code never loads; delete, or move into rules/ with paths: frontmatter", n)})
		}
	}
	return out
}

func countMarkdown(dir string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), ".md") {
			n++
		}
		return nil
	})
	return n
}

func set(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, i := range items {
		m[i] = true
	}
	return m
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
