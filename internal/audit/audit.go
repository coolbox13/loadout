// Package audit runs discovery, the load graph and every check, and
// returns one de-duplicated result for the whole machine.
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/coolbox13/loadout/internal/checks"
	"github.com/coolbox13/loadout/internal/config"
	"github.com/coolbox13/loadout/internal/discover"
	"github.com/coolbox13/loadout/internal/loadgraph"
	"github.com/coolbox13/loadout/internal/model"
	"github.com/coolbox13/loadout/internal/secrets"
)

// Options configure one audit run.
type Options struct {
	Config   config.Config
	Env      loadgraph.Env
	LookPath func(string) (string, error)
	Dirs     []string // audit only these directories; empty means discover
}

// ProjectReport summarises one project.
type ProjectReport struct {
	Dir      string          `json:"dir"`
	Archived bool            `json:"archived"`
	Always   int             `json:"always_tokens"`
	OnPath   int             `json:"on_path_tokens"`
	Lazy     int             `json:"lazy_tokens"`
	Findings []model.Finding `json:"findings"`
}

// GlobalReport summarises what every session pays before project files.
type GlobalReport struct {
	Always   int             `json:"always_tokens"`
	ByKind   map[string]int  `json:"always_tokens_by_kind"`
	Counts   map[string]int  `json:"source_counts_by_kind"`
	Findings []model.Finding `json:"findings"`
}

// Result is the whole audit.
type Result struct {
	Global   GlobalReport    `json:"global"`
	Projects []ProjectReport `json:"projects"`
}

// Run performs the audit.
func Run(opts Options) Result {
	projects := targetProjects(opts)
	r := Result{Global: GlobalReport{ByKind: map[string]int{}, Counts: map[string]int{}}}
	seen := map[string]bool{}
	loaded := map[string]bool{}

	for _, p := range projects {
		g := loadgraph.Build(opts.Env, p.Dir)
		for _, s := range g.Sources {
			loaded[s.Path] = true
		}
		pr := ProjectReport{Dir: p.Dir, Archived: p.Archived,
			Always: g.Total(model.Always), OnPath: g.Total(model.OnPath), Lazy: g.Total(model.Lazy)}
		for _, f := range projectFindings(opts, p, g) {
			if seen[f.Key()] {
				continue
			}
			seen[f.Key()] = true
			if isUnder(f.Path, p.Dir) {
				pr.Findings = append(pr.Findings, f)
			} else {
				r.Global.Findings = append(r.Global.Findings, f)
			}
		}
		r.Projects = append(r.Projects, pr)
		if len(r.Projects) == 1 {
			summariseGlobal(&r.Global, g, opts.Env.ClaudeDir)
		}
	}
	r.Global.Findings = append(r.Global.Findings, globalFindings(opts, loaded)...)
	sortFindings(r.Global.Findings)
	for i := range r.Projects {
		sortFindings(r.Projects[i].Findings)
	}
	return r
}

func targetProjects(opts Options) []discover.Project {
	if len(opts.Dirs) == 0 {
		return discover.Projects(discover.Options{
			Roots: opts.Config.Roots, Skip: opts.Config.SkipSet(), SkipPaths: []string{opts.Config.Snapshot.Repo},
			ArchiveDirs: opts.Config.ArchiveDirs, MaxDepth: opts.Config.MaxDepth,
		})
	}
	out := make([]discover.Project, 0, len(opts.Dirs))
	for _, d := range opts.Dirs {
		abs, err := filepath.Abs(d)
		if err == nil {
			out = append(out, discover.Project{Dir: abs})
		}
	}
	return out
}

func projectFindings(opts Options, p discover.Project, g loadgraph.Graph) []model.Finding {
	cfg := opts.Config
	var out []model.Finding
	add := func(check string, fs ...model.Finding) {
		if cfg.Enabled(check) && (!p.Archived || check == "D5") {
			out = append(out, fs...)
		}
	}
	for _, f := range g.Findings {
		add(f.Check, f)
	}
	add("D1", checks.Budget(g, cfg.Budgets.ProjectAlwaysTokens)...)

	texts := map[string]string{}
	for _, s := range g.Sources {
		data, err := os.ReadFile(s.Path)
		if err != nil {
			continue
		}
		text := string(data)
		if isUnder(s.Path, p.Dir) && isWiring(s.Kind) {
			texts[s.Path] = text
		}
		add("D2", checks.Size(s, text, cfg.Budgets.ClaudeMDLines)...)
		add("D5", checks.Secrets(s.Path, text)...)
		switch s.Kind {
		case model.KindAgent, model.KindSkill:
			add("D7", checks.DescriptionTone(s.Path, text)...)
		default:
			add("D7", checks.Shouting(s.Path, text, cfg.Checks.ShoutingPerFile)...)
		}
	}
	for _, path := range settingsFiles(filepath.Join(p.Dir, ".claude")) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		texts[path] = string(data)
		add("D4", checks.Hooks(path, data, p.Dir, opts.Env.Home, opts.LookPath)...)
		add("D5", settingsSecrets(path, data)...)
	}
	add("D4", checks.Frameworks(p.Dir, texts, opts.LookPath)...)
	return out
}

// isWiring reports whether a source kind can attach a framework to Claude.
// Agent and skill bodies may merely mention a framework's files.
func isWiring(k model.Kind) bool {
	return k == model.KindClaudeMD || k == model.KindImport || k == model.KindRule
}

func globalFindings(opts Options, loaded map[string]bool) []model.Finding {
	var out []model.Finding
	for _, path := range settingsFiles(opts.Env.ClaudeDir) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if opts.Config.Enabled("D4") {
			out = append(out, checks.Hooks(path, data, "", opts.Env.Home, opts.LookPath)...)
		}
		if opts.Config.Enabled("D5") {
			out = append(out, settingsSecrets(path, data)...)
		}
	}
	if opts.Config.Enabled("D5") {
		out = append(out, claudeJSONSecrets(filepath.Join(opts.Env.Home, ".claude.json"))...)
	}
	if opts.Config.Enabled("D6") {
		out = append(out, checks.DeadWeight(opts.Env.ClaudeDir)...)
		out = append(out, unreferencedTopLevel(opts.Env.ClaudeDir, loaded)...)
	}
	return out
}

func settingsFiles(dir string) []string {
	return []string{filepath.Join(dir, "settings.json"), filepath.Join(dir, "settings.local.json")}
}

func settingsSecrets(path string, data []byte) []model.Finding {
	var v any
	if json.Unmarshal(data, &v) != nil {
		return nil
	}
	var out []model.Finding
	for _, h := range secrets.ScanJSON(v) {
		out = append(out, model.Finding{Check: "D5", Severity: model.Warn, Path: path,
			Message: fmt.Sprintf("literal secret at %s; settings files get copied and backed up, prefer the OS keychain or a gitignored env file", h.Masked)})
	}
	return out
}

// claudeJSONSecrets scans only the MCP server definitions in ~/.claude.json
// (user scope and per-project); the rest of that file is Claude Code state.
func claudeJSONSecrets(path string) []model.Finding {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg struct {
		MCPServers any `json:"mcpServers"`
		Projects   map[string]struct {
			MCPServers any `json:"mcpServers"`
		} `json:"projects"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return nil
	}
	subset := map[string]any{}
	if cfg.MCPServers != nil {
		subset["mcpServers"] = cfg.MCPServers
	}
	projects := map[string]any{}
	for dir, p := range cfg.Projects {
		if p.MCPServers != nil {
			projects[dir] = map[string]any{"mcpServers": p.MCPServers}
		}
	}
	if len(projects) > 0 {
		subset["projects"] = projects
	}
	var out []model.Finding
	for _, h := range secrets.ScanJSON(subset) {
		out = append(out, model.Finding{Check: "D5", Severity: model.Warn, Path: path,
			Message: fmt.Sprintf("literal secret at %s; reference an environment variable instead (\"${VAR}\" is expanded in MCP config)", h.Masked)})
	}
	sortFindings(out)
	return out
}

// unreferencedTopLevel finds ~/.claude/*.md that no CLAUDE.md imports.
func unreferencedTopLevel(claudeDir string, loaded map[string]bool) []model.Finding {
	entries, err := os.ReadDir(claudeDir)
	if err != nil {
		return nil
	}
	var out []model.Finding
	for _, e := range entries {
		path := filepath.Join(claudeDir, e.Name())
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || e.Name() == "CLAUDE.md" || loaded[path] {
			continue
		}
		out = append(out, model.Finding{Check: "D6", Severity: model.Info, Path: path,
			Message: "not imported by any CLAUDE.md, so Claude Code never loads it"})
	}
	return out
}

func summariseGlobal(gr *GlobalReport, g loadgraph.Graph, claudeDir string) {
	for _, s := range g.Sources {
		if !isUnder(s.Path, claudeDir) {
			continue
		}
		gr.Counts[string(s.Kind)]++
		if s.When == model.Always {
			gr.Always += s.Tokens
			gr.ByKind[string(s.Kind)] += s.Tokens
		}
	}
}

func isUnder(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func sortFindings(fs []model.Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].Severity.Rank() != fs[j].Severity.Rank() {
			return fs[i].Severity.Rank() > fs[j].Severity.Rank()
		}
		if fs[i].Path != fs[j].Path {
			return fs[i].Path < fs[j].Path
		}
		return fs[i].Line < fs[j].Line
	})
}
