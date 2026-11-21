// Package report renders an audit result for humans or as JSON.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/coolbox13/loadout/internal/audit"
	"github.com/coolbox13/loadout/internal/model"
)

// JSON writes the result as indented JSON.
func JSON(w io.Writer, r audit.Result) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// Text writes a human-readable report. Findings below minSev are hidden but
// still counted in the header.
func Text(out io.Writer, r audit.Result, home string, minSev model.Severity) error {
	w := &errWriter{w: out}
	sh := shortener(home)
	archived := 0
	for _, p := range r.Projects {
		if p.Archived {
			archived++
		}
	}
	e, wa, in := counts(r)
	w.printf("loadout audit · %d projects (%d archived) · %d errors, %d warnings, %d info\n\n", len(r.Projects), archived, e, wa, in)

	g := r.Global
	w.printf("Every session starts with ≈%s tokens from ~/.claude:\n", num(g.Always))
	for _, k := range sortedKeys(g.ByKind) {
		w.printf("  %-20s ≈%7s  (%d files)\n", k, num(g.ByKind[k]), g.Counts[k])
	}
	w.println()

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	tp := &errWriter{w: tw}
	tp.println("PROJECT\tALWAYS\tON-PATH\tLAZY\tERR\tWARN\tINFO\t")
	projects := append([]audit.ProjectReport(nil), r.Projects...)
	sort.SliceStable(projects, func(i, j int) bool { return projects[i].Always > projects[j].Always })
	for _, p := range projects {
		pe, pw, pi := tally(p.Findings)
		name := sh(p.Dir)
		if p.Archived {
			name += " (archived)"
		}
		tp.printf("%s\t≈%s\t≈%s\t≈%s\t%s\t%s\t%s\t\n", name, num(p.Always), num(p.OnPath), num(p.Lazy), dash(pe), dash(pw), dash(pi))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if tp.err != nil {
		return tp.err
	}

	w.println("\nALWAYS includes the global layer above. Tokens are estimates (≈4 characters per token).")
	printFindings(w, "Global findings", g.Findings, sh, minSev)
	for _, p := range r.Projects {
		printFindings(w, sh(p.Dir), p.Findings, sh, minSev)
	}
	return w.err
}

// errWriter keeps the first write error so callers check once at the end.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) Write(p []byte) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	n, err := e.w.Write(p)
	e.err = err
	return n, err
}

func (e *errWriter) printf(format string, args ...any) { _, _ = fmt.Fprintf(e, format, args...) }
func (e *errWriter) println(args ...any)               { _, _ = fmt.Fprintln(e, args...) }

func printFindings(w *errWriter, title string, fs []model.Finding, sh func(string) string, minSev model.Severity) {
	var shown []model.Finding
	for _, f := range fs {
		if f.Severity.Rank() >= minSev.Rank() {
			shown = append(shown, f)
		}
	}
	if len(shown) == 0 {
		return
	}
	w.printf("\n%s\n", title)
	for _, f := range shown {
		loc := sh(f.Path)
		if f.Line > 0 {
			loc = fmt.Sprintf("%s:%d", loc, f.Line)
		}
		w.printf("  %-5s %s  %s\n      %s\n", f.Severity, f.Check, loc, f.Message)
	}
}

func counts(r audit.Result) (int, int, int) {
	e, w, i := tally(r.Global.Findings)
	for _, p := range r.Projects {
		pe, pw, pi := tally(p.Findings)
		e, w, i = e+pe, w+pw, i+pi
	}
	return e, w, i
}

func tally(fs []model.Finding) (e, w, i int) {
	for _, f := range fs {
		switch f.Severity {
		case model.Error:
			e++
		case model.Warn:
			w++
		case model.Info:
			i++
		}
	}
	return e, w, i
}

func shortener(home string) func(string) string {
	return func(p string) string {
		if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
			return "~/" + rel
		}
		return p
	}
}

func num(n int) string {
	s := fmt.Sprintf("%d", n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func dash(n int) string {
	if n == 0 {
		return "·"
	}
	return fmt.Sprintf("%d", n)
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
