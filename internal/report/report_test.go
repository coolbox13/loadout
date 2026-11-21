package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/coolbox13/loadout/internal/audit"
	"github.com/coolbox13/loadout/internal/model"
)

func sample() audit.Result {
	return audit.Result{
		Global: audit.GlobalReport{Always: 4335, ByKind: map[string]int{"rule": 1128}, Counts: map[string]int{"rule": 8},
			Findings: []model.Finding{{Check: "D5", Severity: model.Warn, Path: "/h/.claude/settings.json", Message: "literal secret at env.X"}}},
		Projects: []audit.ProjectReport{
			{Dir: "/h/Documents/small", Always: 4500},
			{Dir: "/h/Documents/big", Always: 8342, Findings: []model.Finding{{Check: "D1", Severity: model.Warn, Path: "/h/Documents/big", Message: "over budget"}, {Check: "D7", Severity: model.Info, Path: "/h/Documents/big/CLAUDE.md", Line: 3, Message: "shouting"}}},
			{Dir: "/h/Documents/archief/old", Archived: true},
		},
	}
}

func TestTextSortsByCostShortensPathsAndFiltersSeverity(t *testing.T) {
	var b bytes.Buffer

	if err := Text(&b, sample(), "/h", model.Warn); err != nil {
		t.Fatal(err)
	}

	out := b.String()
	for _, want := range []string{"3 projects (1 archived)", "≈4,335", "~/Documents/big", "over budget", "(archived)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Index(out, "~/Documents/big ") > strings.Index(out, "~/Documents/small ") {
		t.Error("projects must be sorted by always-loaded cost, highest first")
	}
	if strings.Contains(out, "shouting") {
		t.Error("info finding shown despite --min-severity warn")
	}
}

func TestJSONRoundTripsFields(t *testing.T) {
	var b bytes.Buffer

	if err := JSON(&b, sample()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `"always_tokens": 8342`) {
		t.Fatalf("json missing fields: %s", b.String())
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, bytes.ErrTooLarge }

func TestTextReportsWriteErrors(t *testing.T) {
	if err := Text(failWriter{}, sample(), "/h", model.Info); err == nil {
		t.Fatal("expected write error to surface")
	}
}
