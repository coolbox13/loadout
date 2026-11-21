package checks

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coolbox13/loadout/internal/loadgraph"
	"github.com/coolbox13/loadout/internal/model"
)

func messages(fs []model.Finding) string {
	var b strings.Builder
	for _, f := range fs {
		b.WriteString(f.Check + " " + string(f.Severity) + " " + f.Message + "\n")
	}
	return b.String()
}

func TestSizeFlagsLongClaudeMD(t *testing.T) {
	src := model.Source{Path: "/p/CLAUDE.md", Kind: model.KindClaudeMD}

	got := Size(src, strings.Repeat("line\n", 250), 200)

	if len(got) != 1 || got[0].Check != "D2" || !strings.Contains(got[0].Message, "250 lines") {
		t.Fatalf("got %s", messages(got))
	}
	if Size(src, strings.Repeat("line\n", 50), 200) != nil {
		t.Fatal("short file must not be flagged")
	}
}

func TestShoutingCountsDirectivesOutsideCode(t *testing.T) {
	text := "You MUST do x.\nNEVER do y.\nALWAYS z. IMPORTANT: w\n```\nMUST MUST MUST\n```\n"

	got := Shouting("/p/CLAUDE.md", text, 3)

	if len(got) != 1 || !strings.Contains(got[0].Message, "4 all-caps directives") {
		t.Fatalf("got %s", messages(got))
	}
	if Shouting("/p/CLAUDE.md", text, 5) != nil {
		t.Fatal("below threshold must not be flagged")
	}
}

func TestDescriptionToneFlagsForcefulAgentDescriptions(t *testing.T) {
	text := "---\nname: rev\ndescription: Reviews code. MUST BE USED for all changes. Use PROACTIVELY.\n---\nbody\n"

	got := DescriptionTone("/a/rev.md", text)

	if len(got) != 1 || got[0].Check != "D7" || got[0].Severity != model.Warn {
		t.Fatalf("got %s", messages(got))
	}
	if DescriptionTone("/a/ok.md", "---\nname: ok\ndescription: Reviews code after big changes.\n---\n") != nil {
		t.Fatal("calm description must not be flagged")
	}
}

func TestSecretsReportsMaskedHitsWithLines(t *testing.T) {
	got := Secrets("/p/CLAUDE.md", "intro\nDB=postgresql://u:Zq81mNvPx2@db.example.com/app\n")

	if len(got) != 1 || got[0].Check != "D5" || got[0].Line != 2 || strings.Contains(got[0].Message, "Zq81mNvPx2") {
		t.Fatalf("got %s", messages(got))
	}
}

func fakeLookPath(installed ...string) func(string) (string, error) {
	return func(name string) (string, error) {
		for _, i := range installed {
			if i == name {
				return "/usr/bin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
}

func TestHooksFlagsMissingScriptsAndBinaries(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, ".claude", "hooks", "ok.js")
	if err := os.MkdirAll(filepath.Dir(present), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(present, []byte("//"), 0o644); err != nil {
		t.Fatal(err)
	}
	settings := `{"hooks":{"PreToolUse":[{"matcher":"Read","hooks":[
		{"type":"command","command":"node \"$CLAUDE_PROJECT_DIR/.claude/hooks/ok.js\""},
		{"type":"command","command":"node \"$CLAUDE_PROJECT_DIR/.claude/hooks/gone.js\""},
		{"type":"command","command":"ghosttool hook claude"},
		{"type":"command","command":"p=$(cat); case \"$p\" in *x*) echo;; esac"}
	]}]}}`

	got := Hooks(filepath.Join(dir, ".claude/settings.json"), []byte(settings), dir, "/home/u", fakeLookPath("node"))

	text := messages(got)
	if !strings.Contains(text, "gone.js") || !strings.Contains(text, "ghosttool") {
		t.Fatalf("missing expected findings:\n%s", text)
	}
	if strings.Contains(text, "ok.js") || strings.Contains(text, "p=") {
		t.Fatalf("false positive:\n%s", text)
	}
}

func TestHooksIgnoresInvalidJSON(t *testing.T) {
	got := Hooks("/x/settings.json", []byte("{not json"), "/x", "/home/u", fakeLookPath())

	if len(got) != 1 || got[0].Severity != model.Warn || !strings.Contains(got[0].Message, "not valid JSON") {
		t.Fatalf("got %s", messages(got))
	}
}

func TestFrameworksFlagsWiredButUninstalledFramework(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".wolf"), 0o755); err != nil {
		t.Fatal(err)
	}
	texts := map[string]string{
		filepath.Join(dir, "CLAUDE.md"):             "@.wolf/OPENWOLF.md\n",
		filepath.Join(dir, ".claude/settings.json"): `node "$CLAUDE_PROJECT_DIR/.wolf/hooks/pre-read.js"`,
	}

	got := Frameworks(dir, texts, fakeLookPath("node"))

	if len(got) != 1 || got[0].Check != "D4" || !strings.Contains(got[0].Message, "OpenWolf") || !strings.Contains(got[0].Message, "2 places") {
		t.Fatalf("got %s", messages(got))
	}
	if Frameworks(dir, texts, fakeLookPath("node", "openwolf")) != nil {
		t.Fatal("installed framework must not be flagged")
	}
}

func TestBudgetFlagsProjectsOverLimit(t *testing.T) {
	g := loadgraph.Graph{Project: "/p", Sources: []model.Source{{When: model.Always, Tokens: 4000}}}

	if got := Budget(g, 3000); len(got) != 1 || got[0].Check != "D1" {
		t.Fatalf("got %s", messages(got))
	}
	if Budget(g, 5000) != nil {
		t.Fatal("under budget must not be flagged")
	}
}

func TestDeadWeightReportsUnloadedMarkdownDirs(t *testing.T) {
	claude := t.TempDir()
	for _, rel := range []string{"language-rules/go.md", "language-rules/zh/a.md", "rules/x.md", "projects/t.md"} {
		p := filepath.Join(claude, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := DeadWeight(claude)

	if len(got) != 1 || !strings.Contains(got[0].Message, "2 Markdown files") || !strings.Contains(got[0].Path, "language-rules") {
		t.Fatalf("got %s", messages(got))
	}
}

func TestHooksIgnoresPathFragmentsInsidePatterns(t *testing.T) {
	settings := `{"hooks":{"PreToolUse":[{"matcher":"Read","hooks":[
		{"type":"command","command":"case \"$f\" in *graphify-out/graph.json|*GRAPH_REPORT.md) exit 2;; esac"}
	]}]}}`

	if got := Hooks("/p/.claude/settings.json", []byte(settings), "/p", "/home/u", fakeLookPath()); got != nil {
		t.Fatalf("false positive: %s", messages(got))
	}
}
