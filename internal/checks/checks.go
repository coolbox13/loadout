// Package checks holds the deterministic checks (D1–D7 in DESIGN.md).
// Every check is a pure function over text or a load graph, so it can be
// tested without touching a real machine.
package checks

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/coolbox13/loadout/internal/loadgraph"
	"github.com/coolbox13/loadout/internal/mdparse"
	"github.com/coolbox13/loadout/internal/model"
	"github.com/coolbox13/loadout/internal/secrets"
)

// Budget (D1) flags a project whose always-loaded estimate exceeds limit.
func Budget(g loadgraph.Graph, limit int) []model.Finding {
	total := g.Total(model.Always)
	if total <= limit {
		return nil
	}
	return []model.Finding{{
		Check: "D1", Severity: model.Warn, Path: g.Project,
		Message: fmt.Sprintf("≈%d tokens load in every session (budget %d)", total, limit),
	}}
}

// Size (D2) flags CLAUDE.md files over the line budget. Claude Code's docs
// recommend staying under 200 lines per file.
func Size(src model.Source, text string, maxLines int) []model.Finding {
	if src.Kind != model.KindClaudeMD {
		return nil
	}
	lines := strings.Count(text, "\n")
	if lines <= maxLines {
		return nil
	}
	return []model.Finding{{
		Check: "D2", Severity: model.Warn, Path: src.Path,
		Message: fmt.Sprintf("%d lines (budget %d): move reference material to docs/ and point to it", lines, maxLines),
	}}
}

var shoutRe = regexp.MustCompile(`\b(MUST|NEVER|ALWAYS|CRITICAL|IMPORTANT|MANDATORY|REQUIRED)\b`)

// Shouting (D7) flags files that lean on all-caps directives. Current models
// follow plain instructions; shouting makes them over-apply rules.
func Shouting(path, text string, threshold int) []model.Finding {
	count := 0
	for _, line := range mdparse.ProseLines(text) {
		count += len(shoutRe.FindAllString(line.Text, -1))
	}
	if count <= threshold {
		return nil
	}
	return []model.Finding{{
		Check: "D7", Severity: model.Info, Path: path,
		Message: fmt.Sprintf("%d all-caps directives (MUST/NEVER/ALWAYS…); plain sentences work better", count),
	}}
}

var forcefulRe = regexp.MustCompile(`(?i)(must be used|proactively|immediately|automatically activated)|\bMUST\b|\bALWAYS\b`)

// DescriptionTone (D7) flags agent and skill descriptions that push the model
// to invoke them. Descriptions are in context every session.
func DescriptionTone(path, text string) []model.Finding {
	front, _ := mdparse.Frontmatter(text)
	matches := forcefulRe.FindAllString(front.Description, -1)
	if len(matches) == 0 {
		return nil
	}
	return []model.Finding{{
		Check: "D7", Severity: model.Warn, Path: path, Line: 1,
		Message: fmt.Sprintf("description says %q: invites over-triggering; describe when it helps instead", strings.Join(matches, ", ")),
	}}
}

// Secrets (D5) reports suspected credentials, masked.
func Secrets(path, text string) []model.Finding {
	var out []model.Finding
	for _, h := range secrets.Scan(text) {
		sev := model.Error
		if h.Rule == secrets.RuleAssignment {
			sev = model.Warn
		}
		out = append(out, model.Finding{
			Check: "D5", Severity: sev, Path: path, Line: h.Line,
			Message: fmt.Sprintf("possible secret (%s: %s); move it to .env or a keychain, and rotate it if it was ever committed", h.Rule, h.Masked),
		})
	}
	return out
}
