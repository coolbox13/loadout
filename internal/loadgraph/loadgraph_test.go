package loadgraph

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coolbox13/loadout/internal/model"
)

type fixture struct {
	t    *testing.T
	home string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	return fixture{t: t, home: t.TempDir()}
}

func (f fixture) write(rel, content string) string {
	f.t.Helper()
	p := filepath.Join(f.home, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
	return p
}

func (f fixture) env() Env {
	return Env{Home: f.home, ClaudeDir: filepath.Join(f.home, ".claude"), Skip: map[string]bool{"node_modules": true}}
}

func find(g Graph, suffix string) (model.Source, bool) {
	for _, s := range g.Sources {
		if strings.HasSuffix(s.Path, suffix) {
			return s, true
		}
	}
	return model.Source{}, false
}

func hasFinding(g Graph, check, contains string) bool {
	for _, f := range g.Findings {
		if f.Check == check && strings.Contains(f.Message, contains) {
			return true
		}
	}
	return false
}

func buildStandard(t *testing.T) (fixture, Graph) {
	f := newFixture(t)
	f.write(".claude/CLAUDE.md", "# Global\n@RTK.md\n")
	f.write(".claude/RTK.md", "rtk notes\n")
	f.write(".claude/rules/common/style.md", "style rules\n")
	f.write(".claude/rules/lang/go.md", "---\npaths:\n  - \"**/*.go\"\n---\ngo rules\n")
	f.write(".claude/agents/reviewer.md", "---\nname: reviewer\ndescription: Reviews code.\n---\nlong body that is not loaded\n")
	f.write(".claude/skills/deploy/SKILL.md", "---\nname: deploy\ndescription: Deploys things.\n---\nbody\n")
	f.write("Documents/CLAUDE.md", "parent rules\n")
	f.write("Documents/proj/CLAUDE.md", "# Proj\n@docs/extra.md\n@missing.md\n@\\~/.claude/rules/go.md\n```\n@code/ignored.md\n```\n")
	f.write("Documents/proj/docs/extra.md", "extra\n")
	f.write("Documents/proj/.claude/rules/wolf.md", "---\nglobs: \"**/*\"\n---\nwolf protocol\n")
	f.write("Documents/proj/sub/CLAUDE.md", "sub rules\n")
	f.write("Documents/proj/node_modules/pkg/CLAUDE.md", "vendored\n")
	return f, Build(f.env(), filepath.Join(f.home, "Documents/proj"))
}

func TestBuildClassifiesAlwaysLoadedSources(t *testing.T) {
	_, g := buildStandard(t)

	for _, suffix := range []string{
		".claude/CLAUDE.md", ".claude/RTK.md", "rules/common/style.md",
		"Documents/CLAUDE.md", "proj/CLAUDE.md", "proj/docs/extra.md", "rules/wolf.md",
	} {
		s, ok := find(g, suffix)
		if !ok || s.When != model.Always {
			t.Errorf("%s: found=%v when=%q, want always", suffix, ok, s.When)
		}
	}
}

func TestBuildMarksImportsWithTheirImporter(t *testing.T) {
	_, g := buildStandard(t)

	s, _ := find(g, ".claude/RTK.md")
	if s.Kind != model.KindImport || !strings.HasSuffix(s.ImportedBy, ".claude/CLAUDE.md") {
		t.Fatalf("RTK source = %+v", s)
	}
}

func TestBuildTreatsPathsFrontmatterAsConditional(t *testing.T) {
	_, g := buildStandard(t)

	s, ok := find(g, "rules/lang/go.md")
	if !ok || s.When != model.OnPath || len(s.Globs) != 1 || s.Globs[0] != "**/*.go" {
		t.Fatalf("go rule = %+v (found %v)", s, ok)
	}
}

func TestBuildCountsOnlyDescriptionsForAgentsAndSkills(t *testing.T) {
	_, g := buildStandard(t)

	agent, ok := find(g, "agents/reviewer.md")
	if !ok || agent.Kind != model.KindAgent || agent.When != model.Always {
		t.Fatalf("agent = %+v (found %v)", agent, ok)
	}
	if agent.Tokens > model.EstimateTokens("reviewer: Reviews code.")+1 {
		t.Fatalf("agent tokens = %d; body must not be counted", agent.Tokens)
	}
	if skill, ok := find(g, "skills/deploy/SKILL.md"); !ok || skill.Kind != model.KindSkill {
		t.Fatalf("skill = %+v (found %v)", skill, ok)
	}
}

func TestBuildFindsLazySubdirectoriesButSkipsExcluded(t *testing.T) {
	_, g := buildStandard(t)

	if s, ok := find(g, "proj/sub/CLAUDE.md"); !ok || s.When != model.Lazy {
		t.Fatalf("sub = %+v (found %v)", s, ok)
	}
	if _, ok := find(g, "node_modules/pkg/CLAUDE.md"); ok {
		t.Fatal("node_modules CLAUDE.md must be skipped")
	}
}

func TestBuildReportsDanglingAndEscapedImports(t *testing.T) {
	_, g := buildStandard(t)

	if !hasFinding(g, "D3", "missing.md") {
		t.Errorf("no dangling-import finding for missing.md: %+v", g.Findings)
	}
	if !hasFinding(g, "D3", `@\~`) {
		t.Errorf("no escaped-import finding: %+v", g.Findings)
	}
	if hasFinding(g, "D3", "code/ignored.md") {
		t.Error("import inside a code fence must be ignored")
	}
}

func TestBuildReportsGlobsKeyAsAlwaysLoaded(t *testing.T) {
	_, g := buildStandard(t)

	if !hasFinding(g, "D3", "globs:") {
		t.Fatalf("no globs-key finding: %+v", g.Findings)
	}
}

func TestBuildStopsImportsAfterFourHops(t *testing.T) {
	f := newFixture(t)
	f.write("p/CLAUDE.md", "@a.md\n")
	f.write("p/a.md", "@b.md\n")
	f.write("p/b.md", "@c.md\n")
	f.write("p/c.md", "@d.md\n")
	f.write("p/d.md", "@e.md\n")
	f.write("p/e.md", "too deep\n")

	g := Build(f.env(), filepath.Join(f.home, "p"))

	if _, ok := find(g, "p/d.md"); !ok {
		t.Fatal("fourth hop should load")
	}
	if _, ok := find(g, "p/e.md"); ok {
		t.Fatal("fifth hop must not load")
	}
	if !hasFinding(g, "D3", "4 hops") {
		t.Fatalf("no hop-limit finding: %+v", g.Findings)
	}
}

func TestBuildSurvivesImportCycles(t *testing.T) {
	f := newFixture(t)
	f.write("p/CLAUDE.md", "@a.md\n")
	f.write("p/a.md", "@CLAUDE.md\n")

	g := Build(f.env(), filepath.Join(f.home, "p"))

	if n := len(g.Sources); n != 2 {
		t.Fatalf("sources = %d, want 2 (no duplicates)", n)
	}
}

func TestTotalSumsByWhen(t *testing.T) {
	g := Graph{Sources: []model.Source{
		{When: model.Always, Tokens: 10}, {When: model.Always, Tokens: 5}, {When: model.Lazy, Tokens: 7},
	}}

	if g.Total(model.Always) != 15 || g.Total(model.Lazy) != 7 || g.Total(model.OnPath) != 0 {
		t.Fatalf("totals wrong: %d %d %d", g.Total(model.Always), g.Total(model.Lazy), g.Total(model.OnPath))
	}
}
