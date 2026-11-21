package discover

import (
	"os"
	"path/filepath"
	"testing"
)

func touch(t *testing.T, root, rel string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProjectsFindsNestedSkipsExcludedAndTagsArchives(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{
		"a/CLAUDE.md",
		"group/etl/projects/processor/CLAUDE.md",
		"b/.claude/CLAUDE.md",
		"c/CLAUDE.local.md",
		"a/node_modules/x/CLAUDE.md",
		".hidden/CLAUDE.md",
		"archief/old/CLAUDE.md",
		"deep/1/2/3/4/5/6/7/8/CLAUDE.md",
	} {
		touch(t, root, rel)
	}

	got := Projects(Options{Roots: []string{root}, Skip: map[string]bool{"node_modules": true}, ArchiveDirs: []string{"archief"}, MaxDepth: 7})

	want := map[string]bool{"a": false, "archief/old": true, "b": false, "c": false, "group/etl/projects/processor": false}
	if len(got) != len(want) {
		t.Fatalf("got %d projects: %+v", len(got), got)
	}
	for _, p := range got {
		rel, _ := filepath.Rel(root, p.Dir)
		archived, ok := want[rel]
		if !ok || archived != p.Archived {
			t.Errorf("unexpected %s (archived=%v)", rel, p.Archived)
		}
	}
}

func TestProjectsSkipsExcludedPaths(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "app/CLAUDE.md")
	touch(t, root, "backup/projects/app/CLAUDE.md")

	got := Projects(Options{Roots: []string{root}, SkipPaths: []string{filepath.Join(root, "backup")}, MaxDepth: 7})

	if len(got) != 1 || filepath.Base(got[0].Dir) != "app" {
		t.Fatalf("got %+v", got)
	}
}
