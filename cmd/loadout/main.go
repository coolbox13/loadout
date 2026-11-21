// Command loadout shows what Claude Code loads before you type, and what is
// wrong with it, across every project on the machine.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/coolbox13/loadout/internal/audit"
	"github.com/coolbox13/loadout/internal/config"
	"github.com/coolbox13/loadout/internal/loadgraph"
	"github.com/coolbox13/loadout/internal/model"
	"github.com/coolbox13/loadout/internal/report"
	"github.com/coolbox13/loadout/internal/snapshot"
)

// version is set at release time via -ldflags "-X main.version=…".
var version = "dev"

const usage = `loadout — what Claude Code loads before you type

Usage:
  loadout audit [flags] [dir ...]   report load cost and findings (read-only)
  loadout graph [dir]               list every source that loads for one directory
  loadout snapshot [--commit MSG]   back up instruction files to the snapshot repo
  loadout version

Audit flags:
  --json              machine-readable output
  --min-severity S    hide findings below S (info|warn|error; default warn)
  --fail-on S         exit 1 if any finding is at or above S (for CI)
  --config PATH       config file (default ~/.config/loadout/config.yaml)
`

// errFindings signals a --fail-on threshold hit; main maps it to exit 1.
var errFindings = errors.New("findings at or above --fail-on threshold")

func main() {
	out := &errWriter{w: os.Stdout}
	err := run(os.Args[1:], out)
	if err == nil {
		err = out.err // e.g. a closed pipe while printing
	}
	switch {
	case errors.Is(err, errFindings):
		os.Exit(1)
	case err != nil:
		_, _ = fmt.Fprintln(os.Stderr, "loadout:", err) // nothing left to report a stderr failure to
		os.Exit(2)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		_, err := fmt.Fprint(out, usage)
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("find home directory: %w", err)
	}
	switch args[0] {
	case "audit":
		return runAudit(args[1:], out, home)
	case "graph":
		return runGraph(args[1:], out, home)
	case "snapshot":
		return runSnapshot(args[1:], out, home)
	case "version":
		_, _ = fmt.Fprintln(out, "loadout", version)
		return nil
	case "help", "-h", "--help":
		_, err := fmt.Fprint(out, usage)
		return err
	}
	return fmt.Errorf("unknown command %q (try loadout help)", args[0])
}

func loadConfig(path, home string) (config.Config, loadgraph.Env, error) {
	if path == "" {
		path = config.DefaultPath(home)
	}
	cfg, err := config.Load(path, home)
	env := loadgraph.Env{Home: home, ClaudeDir: filepath.Join(home, ".claude"), Skip: cfg.SkipSet()}
	return cfg, env, err
}

func runAudit(args []string, out io.Writer, home string) error {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "JSON output")
	minSev := fs.String("min-severity", "warn", "info|warn|error")
	failOn := fs.String("fail-on", "", "info|warn|error")
	cfgPath := fs.String("config", "", "config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, env, err := loadConfig(*cfgPath, home)
	if err != nil {
		return err
	}
	min, err := parseSeverity(*minSev)
	if err != nil {
		return err
	}

	res := audit.Run(audit.Options{Config: cfg, Env: env, LookPath: exec.LookPath, Dirs: fs.Args()})
	if *asJSON {
		err = report.JSON(out, res)
	} else {
		err = report.Text(out, res, home, min)
	}
	if err != nil {
		return err
	}
	return checkFailOn(res, *failOn)
}

func checkFailOn(res audit.Result, failOn string) error {
	if failOn == "" {
		return nil
	}
	threshold, err := parseSeverity(failOn)
	if err != nil {
		return err
	}
	all := append([]model.Finding(nil), res.Global.Findings...)
	for _, p := range res.Projects {
		all = append(all, p.Findings...)
	}
	for _, f := range all {
		if f.Severity.Rank() >= threshold.Rank() {
			return errFindings
		}
	}
	return nil
}

func runGraph(args []string, out io.Writer, home string) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	_, env, err := loadConfig("", home)
	if err != nil {
		return err
	}
	g := loadgraph.Build(env, abs)
	for _, when := range []model.When{model.Always, model.OnPath, model.Lazy} {
		_, _ = fmt.Fprintf(out, "%s (≈%d tokens)\n", when, g.Total(when))
		for _, s := range g.Sources {
			if s.When != when {
				continue
			}
			extra := ""
			if len(s.Globs) > 0 {
				extra = fmt.Sprintf("  paths=%v", s.Globs)
			}
			_, _ = fmt.Fprintf(out, "  ≈%6d  %-17s %s%s\n", s.Tokens, s.Kind, s.Path, extra)
		}
	}
	return nil
}

func runSnapshot(args []string, out io.Writer, home string) error {
	fs := flag.NewFlagSet("snapshot", flag.ContinueOnError)
	message := fs.String("commit", "", "commit the snapshot with this message")
	cfgPath := fs.String("config", "", "config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, _, err := loadConfig(*cfgPath, home)
	if err != nil {
		return err
	}
	res, err := snapshot.Run(snapshot.Options{Config: cfg, Home: home, Message: *message})
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "snapshot: %d global entries, %d projects → %s\n", res.Global, res.Projects, cfg.Snapshot.Repo)
	_, _ = fmt.Fprintf(out, "secret scan: clean (%s)\n", res.Scanner)
	switch {
	case *message == "":
		_, _ = fmt.Fprintln(out, "not committed (pass --commit MSG)")
	case res.Committed:
		_, _ = fmt.Fprintln(out, "committed")
	default:
		_, _ = fmt.Fprintln(out, "no changes to commit")
	}
	return nil
}

func parseSeverity(s string) (model.Severity, error) {
	sev := model.Severity(s)
	if sev.Rank() == 0 {
		return "", fmt.Errorf("unknown severity %q (use info, warn or error)", s)
	}
	return sev, nil
}

// errWriter keeps the first write error; main checks it once after run.
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
