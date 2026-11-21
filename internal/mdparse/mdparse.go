// Package mdparse extracts what Claude Code cares about from Markdown:
// @imports (outside code), YAML frontmatter, and prose lines.
package mdparse

import (
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Import is one @path reference found in prose.
type Import struct {
	Target  string // path as written, with any leading backslash removed
	Line    int    // 1-based
	Escaped bool   // written as @\~/..., which Claude Code does not document
}

// Line is a 1-based line number with its text.
type Line struct {
	N    int
	Text string
}

var (
	importRe    = regexp.MustCompile(`(^|[\s(\[])@(\\?)([^\s` + "`" + `]+)`)
	codeSpanRe  = regexp.MustCompile("`+[^`]*`+")
	extensionRe = regexp.MustCompile(`\.[A-Za-z0-9]{1,8}$`)
)

const trailingPunct = ".,;:!?)]"

// ProseLines returns the lines outside fenced code blocks.
func ProseLines(text string) []Line {
	var out []Line
	fence := ""
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for i, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if marker := fenceMarker(trimmed); marker != "" {
			switch {
			case fence == "":
				fence = marker
				continue
			case strings.HasPrefix(trimmed, fence):
				fence = ""
				continue
			}
		}
		if fence == "" {
			out = append(out, Line{N: i + 1, Text: raw})
		}
	}
	return out
}

func fenceMarker(trimmed string) string {
	switch {
	case strings.HasPrefix(trimmed, "```"):
		return "```"
	case strings.HasPrefix(trimmed, "~~~"):
		return "~~~"
	}
	return ""
}

// Imports returns @path references in prose, skipping fenced blocks and
// inline code spans. Only path-like targets count (containing "/", a file
// extension, or starting with "~"), which keeps @mentions, decorators and
// annotations in prose from being mistaken for imports.
func Imports(text string) []Import {
	var out []Import
	for _, line := range ProseLines(text) {
		prose := codeSpanRe.ReplaceAllStringFunc(line.Text, func(s string) string {
			return strings.Repeat(" ", len(s))
		})
		for _, m := range importRe.FindAllStringSubmatch(prose, -1) {
			target := strings.TrimRight(m[3], trailingPunct)
			if !isPathLike(target) {
				continue
			}
			out = append(out, Import{Target: target, Line: line.N, Escaped: m[2] == `\`})
		}
	}
	return out
}

func isPathLike(target string) bool {
	if target == "" || strings.ContainsAny(target, `("'<>{}=`) {
		return false
	}
	return strings.Contains(target, "/") || strings.HasPrefix(target, "~") || extensionRe.MatchString(target)
}

// Front is the frontmatter fields loadout understands.
type Front struct {
	Present     bool
	Paths       []string
	Description string
	Name        string
	HasGlobsKey bool // `globs:` is used by other tools; Claude Code documents `paths:`
}

type rawFront struct {
	Paths       any    `yaml:"paths"`
	Globs       any    `yaml:"globs"`
	Description string `yaml:"description"`
	Name        string `yaml:"name"`
}

// Frontmatter splits YAML frontmatter from the body. Malformed YAML is
// treated as no frontmatter, which matches how a loader would degrade.
func Frontmatter(text string) (Front, string) {
	if !strings.HasPrefix(text, "---\n") {
		return Front{}, text
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		return Front{}, text
	}
	head := text[4 : 4+end]
	body := strings.TrimPrefix(text[4+end+4:], "\n")

	var raw rawFront
	if err := yaml.Unmarshal([]byte(head), &raw); err != nil {
		return Front{}, text
	}
	return Front{
		Present:     true,
		Paths:       toStrings(raw.Paths),
		Description: raw.Description,
		Name:        raw.Name,
		HasGlobsKey: raw.Globs != nil,
	}, body
}

func toStrings(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
