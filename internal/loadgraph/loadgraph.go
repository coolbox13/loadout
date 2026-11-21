// Package loadgraph models what Claude Code loads into context for a given
// working directory, and when. The rules follow the Claude Code memory docs
// plus behaviour verified with canary tests (user-level path-scoped rules).
package loadgraph

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/coolbox13/loadout/internal/mdparse"
	"github.com/coolbox13/loadout/internal/model"
)

// MaxImportHops is Claude Code's documented import depth.
const MaxImportHops = 4

// maxLazyDepth bounds the walk for subdirectory CLAUDE.md files.
const maxLazyDepth = 8

// Env describes the machine: where home and ~/.claude are, and which
// directory names never contain instructions worth loading.
type Env struct {
	Home      string
	ClaudeDir string
	Skip      map[string]bool
}

// Graph is the load model for one project directory.
type Graph struct {
	Project  string          `json:"project"`
	Sources  []model.Source  `json:"sources"`
	Findings []model.Finding `json:"findings"`
}

// Total sums estimated tokens for sources loaded at the given moment.
func (g Graph) Total(when model.When) int {
	sum := 0
	for _, s := range g.Sources {
		if s.When == when {
			sum += s.Tokens
		}
	}
	return sum
}

type builder struct {
	env     Env
	graph   Graph
	visited map[string]bool
}

// Build returns the load graph for project.
func Build(env Env, project string) Graph {
	b := &builder{env: env, graph: Graph{Project: project}, visited: map[string]bool{}}

	b.addFile(filepath.Join(env.ClaudeDir, "CLAUDE.md"), model.KindClaudeMD, model.Always, "", 0)
	b.addRules(filepath.Join(env.ClaudeDir, "rules"))
	for _, dir := range ancestorsRootFirst(project) {
		b.addFile(filepath.Join(dir, "CLAUDE.md"), model.KindClaudeMD, model.Always, "", 0)
		b.addFile(filepath.Join(dir, "CLAUDE.local.md"), model.KindClaudeMD, model.Always, "", 0)
	}
	b.addFile(filepath.Join(project, ".claude", "CLAUDE.md"), model.KindClaudeMD, model.Always, "", 0)
	b.addRules(filepath.Join(project, ".claude", "rules"))
	for _, base := range []string{env.ClaudeDir, filepath.Join(project, ".claude")} {
		b.addDescriptions(filepath.Join(base, "agents"), model.KindAgent)
		b.addSkills(filepath.Join(base, "skills"))
	}
	b.addLazy(project)
	return b.graph
}

// ancestorsRootFirst returns dir and every parent, outermost first.
func ancestorsRootFirst(dir string) []string {
	var chain []string
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		chain = append([]string{d}, chain...)
		if parent := filepath.Dir(d); parent == d {
			return chain
		}
	}
}

func (b *builder) addFile(path string, kind model.Kind, when model.When, importedBy string, hops int) {
	abs, err := filepath.Abs(path)
	if err != nil || b.visited[abs] {
		return
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return
	}
	b.visited[abs] = true
	text := string(data)
	b.graph.Sources = append(b.graph.Sources, model.Source{
		Path: abs, Kind: kind, When: when, Tokens: model.EstimateTokens(text), ImportedBy: importedBy,
	})
	b.resolveImports(abs, text, when, hops)
}

func (b *builder) resolveImports(file, text string, when model.When, hops int) {
	for _, imp := range mdparse.Imports(text) {
		if imp.Escaped {
			b.find(model.Warn, file, imp.Line, fmt.Sprintf(`import uses undocumented @\~ syntax: @\%s (write @%s)`, imp.Target, imp.Target))
		}
		target := b.resolve(file, imp.Target)
		if _, err := os.Stat(target); err != nil {
			b.find(model.Error, file, imp.Line, fmt.Sprintf("dangling import @%s: %s does not exist", imp.Target, b.short(target)))
			continue
		}
		if hops+1 > MaxImportHops {
			b.find(model.Warn, file, imp.Line, fmt.Sprintf("import @%s is beyond %d hops and will not load", imp.Target, MaxImportHops))
			continue
		}
		b.addFile(target, model.KindImport, when, file, hops+1)
	}
}

func (b *builder) resolve(fromFile, target string) string {
	switch {
	case strings.HasPrefix(target, "~/"):
		return filepath.Join(b.env.Home, target[2:])
	case filepath.IsAbs(target):
		return target
	}
	return filepath.Join(filepath.Dir(fromFile), target)
}

func (b *builder) addRules(dir string) {
	for _, path := range markdownFiles(dir) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		front, _ := mdparse.Frontmatter(string(data))
		if front.HasGlobsKey && len(front.Paths) == 0 {
			b.find(model.Warn, path, 1, "frontmatter uses globs:, which Claude Code does not document; this rule loads in every session (use paths:)")
		}
		if len(front.Paths) == 0 {
			b.addFile(path, model.KindRule, model.Always, "", 0)
			continue
		}
		abs, _ := filepath.Abs(path)
		if b.visited[abs] {
			continue
		}
		b.visited[abs] = true
		b.graph.Sources = append(b.graph.Sources, model.Source{
			Path: abs, Kind: model.KindRule, When: model.OnPath, Globs: front.Paths, Tokens: model.EstimateTokens(string(data)),
		})
	}
}

// addDescriptions records agent descriptions: only name + description are
// always in context, the body loads when the agent runs.
func (b *builder) addDescriptions(dir string, kind model.Kind) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			b.addDescription(filepath.Join(dir, e.Name()), kind)
		}
	}
}

func (b *builder) addSkills(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		skill := filepath.Join(dir, e.Name(), "SKILL.md")
		if _, err := os.Stat(skill); err == nil {
			b.addDescription(skill, model.KindSkill)
		}
	}
}

func (b *builder) addDescription(path string, kind model.Kind) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	front, _ := mdparse.Frontmatter(string(data))
	b.graph.Sources = append(b.graph.Sources, model.Source{
		Path: path, Kind: kind, When: model.Always, Tokens: model.EstimateTokens(front.Name + ": " + front.Description),
	})
}

func (b *builder) addLazy(project string) {
	root := filepath.Clean(project)
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(root, path)
			depth := len(strings.Split(rel, string(filepath.Separator)))
			if path != root && (b.env.Skip[d.Name()] || strings.HasPrefix(d.Name(), ".") || depth > maxLazyDepth) {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "CLAUDE.md" && filepath.Dir(path) != root {
			b.addFile(path, model.KindClaudeMD, model.Lazy, "", 0)
		}
		return nil
	})
}

func markdownFiles(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), ".md") {
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func (b *builder) find(sev model.Severity, path string, line int, msg string) {
	b.graph.Findings = append(b.graph.Findings, model.Finding{Check: "D3", Severity: sev, Path: path, Line: line, Message: msg})
}

func (b *builder) short(path string) string {
	if rel, err := filepath.Rel(b.env.Home, path); err == nil && !strings.HasPrefix(rel, "..") {
		return "~/" + rel
	}
	return path
}
