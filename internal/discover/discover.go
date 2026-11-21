// Package discover finds every project that carries Claude instructions.
package discover

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Project is a directory holding CLAUDE.md, CLAUDE.local.md or .claude/CLAUDE.md.
type Project struct {
	Dir      string `json:"dir"`
	Archived bool   `json:"archived"`
}

// Options control the walk.
type Options struct {
	Roots       []string
	Skip        map[string]bool
	SkipPaths   []string // absolute directories never walked, e.g. the snapshot repo
	ArchiveDirs []string
	MaxDepth    int
}

var markers = []string{"CLAUDE.md", "CLAUDE.local.md", filepath.Join(".claude", "CLAUDE.md")}

// Projects walks the roots and returns projects sorted by path. Hidden
// directories and Skip names are not descended into. Depth is counted from
// each root, and deliberately generous: a shallow limit silently missed
// nested projects in practice.
func Projects(opts Options) []Project {
	seen := map[string]bool{}
	var out []Project
	for _, root := range opts.Roots {
		root = filepath.Clean(root)
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			if path != root && (opts.Skip[d.Name()] || strings.HasPrefix(d.Name(), ".") || skipPath(path, opts.SkipPaths)) {
				return filepath.SkipDir
			}
			if depth(root, path) > opts.MaxDepth {
				return filepath.SkipDir
			}
			if !seen[path] && hasMarker(path) {
				seen[path] = true
				out = append(out, Project{Dir: path, Archived: isArchived(root, path, opts.ArchiveDirs)})
			}
			return nil
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out
}

func skipPath(path string, skip []string) bool {
	for _, s := range skip {
		if filepath.Clean(s) == path {
			return true
		}
	}
	return false
}

func hasMarker(dir string) bool {
	for _, m := range markers {
		if info, err := os.Stat(filepath.Join(dir, m)); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func depth(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return 0
	}
	return len(strings.Split(rel, string(filepath.Separator)))
}

func isArchived(root, path string, archiveDirs []string) bool {
	rel, _ := filepath.Rel(root, path)
	for _, seg := range strings.Split(rel, string(filepath.Separator)) {
		for _, a := range archiveDirs {
			if strings.EqualFold(seg, a) {
				return true
			}
		}
	}
	return false
}
