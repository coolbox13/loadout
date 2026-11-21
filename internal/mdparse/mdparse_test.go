package mdparse

import (
	"reflect"
	"testing"
)

func targets(imps []Import) []string {
	out := []string{}
	for _, i := range imps {
		out = append(out, i.Target)
	}
	return out
}

func TestImportsFindsLineStartAndInlineImports(t *testing.T) {
	text := "# Title\n@RTK.md\nSee @docs/api.md and @~/.claude/x.md for more.\n"

	got := Imports(text)

	want := []string{"RTK.md", "docs/api.md", "~/.claude/x.md"}
	if !reflect.DeepEqual(targets(got), want) {
		t.Fatalf("targets = %v, want %v", targets(got), want)
	}
	if got[0].Line != 2 || got[1].Line != 3 {
		t.Fatalf("lines = %d,%d, want 2,3", got[0].Line, got[1].Line)
	}
}

func TestImportsIgnoresFencedBlocksAndCodeSpans(t *testing.T) {
	text := "```python\n@router.get(\"/x\")\n@app.md\n```\nUse `@inline.md` here.\n~~~\n@tilde.md\n~~~\n"

	if got := Imports(text); len(got) != 0 {
		t.Fatalf("expected no imports, got %v", targets(got))
	}
}

func TestImportsIgnoresEmailsAndDecoratorsInProse(t *testing.T) {
	text := "Mail hello@example.com.\nThe @Override annotation and @router.get(\"/a\") in prose.\n"

	if got := Imports(text); len(got) != 0 {
		t.Fatalf("expected no imports, got %v", targets(got))
	}
}

func TestImportsFlagsEscapedTilde(t *testing.T) {
	got := Imports("@\\~/.claude/rules/go.md\n")

	if len(got) != 1 || !got[0].Escaped || got[0].Target != "~/.claude/rules/go.md" {
		t.Fatalf("got %+v, want one escaped import of ~/.claude/rules/go.md", got)
	}
}

func TestImportsStripsTrailingPunctuation(t *testing.T) {
	got := Imports("Read @docs/setup.md, then @NOTES.md.\n")

	want := []string{"docs/setup.md", "NOTES.md"}
	if !reflect.DeepEqual(targets(got), want) {
		t.Fatalf("targets = %v, want %v", targets(got), want)
	}
}

func TestFrontmatterParsesPathsList(t *testing.T) {
	text := "---\npaths:\n  - \"**/*.go\"\n  - go.mod\n---\n# Go\nbody\n"

	fm, body := Frontmatter(text)

	if !reflect.DeepEqual(fm.Paths, []string{"**/*.go", "go.mod"}) {
		t.Fatalf("paths = %v", fm.Paths)
	}
	if body != "# Go\nbody\n" {
		t.Fatalf("body = %q", body)
	}
}

func TestFrontmatterAcceptsPathsAsString(t *testing.T) {
	fm, _ := Frontmatter("---\npaths: \"src/**/*.ts\"\n---\nx\n")

	if !reflect.DeepEqual(fm.Paths, []string{"src/**/*.ts"}) {
		t.Fatalf("paths = %v", fm.Paths)
	}
}

func TestFrontmatterRecordsUnsupportedGlobsKey(t *testing.T) {
	fm, _ := Frontmatter("---\ndescription: wolf\nglobs: \"**/*\"\n---\nx\n")

	if !fm.HasGlobsKey || len(fm.Paths) != 0 || fm.Description != "wolf" {
		t.Fatalf("got %+v", fm)
	}
}

func TestFrontmatterAbsentReturnsWholeText(t *testing.T) {
	fm, body := Frontmatter("# No frontmatter\n")

	if fm.Present || body != "# No frontmatter\n" {
		t.Fatalf("got %+v %q", fm, body)
	}
}

func TestProseLinesSkipsFences(t *testing.T) {
	text := "MUST do\n```\nNEVER in code\n```\nALWAYS after\n"

	got := ProseLines(text)

	want := []Line{{1, "MUST do"}, {5, "ALWAYS after"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
