// Package model holds the types shared by every loadout package.
package model

import "strconv"

// When describes the moment a source enters Claude's context.
type When string

const (
	Always When = "always"  // loaded at session start
	OnPath When = "on-path" // loaded when Claude touches a matching file
	Lazy   When = "lazy"    // loaded when Claude works inside a subdirectory
)

// Kind is what sort of instruction source a file is.
type Kind string

const (
	KindClaudeMD Kind = "claude-md"
	KindImport   Kind = "import"
	KindRule     Kind = "rule"
	KindAgent    Kind = "agent-description"
	KindSkill    Kind = "skill-description"
)

// Source is one instruction file (or description) in a load graph.
type Source struct {
	Path       string   `json:"path"`
	Kind       Kind     `json:"kind"`
	When       When     `json:"when"`
	Globs      []string `json:"globs,omitempty"`
	Tokens     int      `json:"tokens"`
	ImportedBy string   `json:"imported_by,omitempty"`
}

// Severity ranks findings. Error means "fix this", Info means "worth knowing".
type Severity string

const (
	Error Severity = "error"
	Warn  Severity = "warn"
	Info  Severity = "info"
)

// Rank orders severities so callers can filter with a threshold.
func (s Severity) Rank() int {
	switch s {
	case Error:
		return 3
	case Warn:
		return 2
	case Info:
		return 1
	}
	return 0
}

// Finding is one problem a check detected.
type Finding struct {
	Check    string   `json:"check"`
	Severity Severity `json:"severity"`
	Path     string   `json:"path"`
	Line     int      `json:"line,omitempty"`
	Message  string   `json:"message"`
}

// Key identifies a finding for de-duplication across projects that share
// global sources.
func (f Finding) Key() string {
	return f.Check + "\x00" + f.Path + "\x00" + strconv.Itoa(f.Line) + "\x00" + f.Message
}

// charsPerToken is the usual rough ratio for English and code; loadout
// reports estimates with a ≈ prefix and never claims exact counts.
const charsPerToken = 4

// EstimateTokens returns a rough token count for text.
func EstimateTokens(text string) int {
	return (len(text) + charsPerToken - 1) / charsPerToken
}
