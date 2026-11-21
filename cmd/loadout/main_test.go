package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/coolbox13/loadout/internal/audit"
	"github.com/coolbox13/loadout/internal/model"
)

// isolate points HOME at an empty temp dir so tests never read the real
// ~/.claude or ~/.config/loadout of whoever runs them.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

func TestRunVersionAndUsage(t *testing.T) {
	isolate(t)
	var out bytes.Buffer

	if err := run([]string{"version"}, &out); err != nil || !strings.Contains(out.String(), "loadout") {
		t.Fatalf("version: %v %q", err, out.String())
	}
	out.Reset()
	if err := run(nil, &out); err != nil || !strings.Contains(out.String(), "Usage:") {
		t.Fatalf("usage: %v %q", err, out.String())
	}
}

func TestRunRejectsUnknownCommandAndSeverity(t *testing.T) {
	isolate(t)
	var out bytes.Buffer

	if err := run([]string{"frobnicate"}, &out); err == nil {
		t.Fatal("expected error for unknown command")
	}
	if err := run([]string{"audit", "--min-severity", "loud", t.TempDir()}, &out); err == nil {
		t.Fatal("expected error for unknown severity")
	}
}

func TestCheckFailOnThreshold(t *testing.T) {
	res := audit.Result{Projects: []audit.ProjectReport{{Findings: []model.Finding{{Severity: model.Warn}}}}}

	if err := checkFailOn(res, "error"); err != nil {
		t.Fatalf("warn must not fail an error threshold: %v", err)
	}
	if err := checkFailOn(res, "warn"); !errors.Is(err, errFindings) {
		t.Fatalf("warn must fail a warn threshold: %v", err)
	}
	if err := checkFailOn(res, ""); err != nil {
		t.Fatal("no threshold means never fail")
	}
}

func TestRunAuditAndGraphOnADirectory(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	var out bytes.Buffer

	if err := run([]string{"audit", "--json", dir}, &out); err != nil || !strings.Contains(out.String(), `"projects"`) {
		t.Fatalf("audit --json: %v %q", err, out.String())
	}
	out.Reset()
	if err := run([]string{"graph", dir}, &out); err != nil || !strings.Contains(out.String(), "always") {
		t.Fatalf("graph: %v %q", err, out.String())
	}
}
