# Design: automated hygiene for Claude Code instruction files

Working name: **loadout** (what Claude loads before you type).
Status: MVP built (read-only audit + snapshot; §11 step 1). Derived from one manual
cleanup of a real machine and the MVP build. See `FIELD-NOTES.md`.

## 1. Problem

Instruction files grow by accretion. Framework installers drop thousands of
words into `~/.claude`, every project gets a CLAUDE.md that turns into a
changelog, imports rot, and uninstalled tools keep issuing orders. Nobody sees
the total, because it is spread over dozens of repos and loaded invisibly.

Cost: tokens on every turn, and worse adherence. Important rules drown in
generic ones, and forceful wording ("MUST BE USED") makes the model
over-trigger.

### Evidence from the manual run (1 Mac, 42 projects)

| Finding | Count |
|---|---|
| Global always-loaded instructions | 3,000 → 834 words |
| Project CLAUDE.md total (39 files) | 6,923 → 1,875 lines; 43.9k → 19.3k words |
| Project CLAUDE.md over the 200-line guidance | 13 → 0 |
| Unloaded installer leftovers in `~/.claude` | 57 files |
| Live secrets in `settings.json` | 5 |
| Credentials in project CLAUDE.md prose | 3 (one missed by default gitleaks rules) |
| Broken `@imports` (nothing loaded, nobody noticed) | 6 |
| Projects still running an uninstalled framework (imports + hooks) | 11 |
| Agent descriptions with "MUST BE USED / PROACTIVELY" | 13 of 22 |
| Files referencing scripts/docs/ports that no longer match the repo | most |
| Nested projects a naive scan missed | 6 |

## 2. Prior art and positioning

GitHub has about 25 small tools (`claude-md-lint`, `claude-md-optimizer`,
`claude-md-audit`, AGENTS.md linters), mostly with 0–25 stars. All work **per file or
per repo**: score a CLAUDE.md, count rules, suggest trims.

Nothing found works at the **machine level**:
- an inventory of every instruction source on the machine
- a model of what actually loads, and when
- versioned backup
- secret gating
- dead-import and abandoned-framework detection
- empirical load verification

That is the niche. Per-file trimming is a commodity; the fleet view and the
safety rails are not.

## 3. Goals and non-goals

**Goals**
1. Show the user exactly what Claude loads, per project, in tokens, with sources.
2. Find problems that are mechanically detectable, with no LLM needed.
3. Propose (not impose) rewrites where judgement is needed, via an LLM pass.
4. Make every change reversible: snapshot first, refuse to proceed without one.
5. Never move a secret anywhere it wasn't before.

**Non-goals**
- Writing CLAUDE.md from scratch (`/init` exists).
- Replacing Claude Code's own memory features or a dotfiles manager.
- Scoring "quality" with a single magic number.
- Supporting every agent tool on day one (AGENTS.md / Cursor rules come later
  through the same load-model abstraction).

## 4. Core concept: the load model

The tool's central data structure is the **load graph**: for a given working
directory, what enters context, when, and from where. Many findings are
just queries over it.

| Source | When it loads | Notes |
|---|---|---|
| `~/.claude/CLAUDE.md` + imports | every session | imports ≤4 hops; skipped inside code blocks |
| `~/.claude/rules/**/*.md` without `paths:` | every session | recursive (verified) |
| `~/.claude/rules/**/*.md` with `paths:` | on Read/Write/Edit of a matching file | verified by canary for user level; not in docs |
| CLAUDE.md / CLAUDE.local.md in cwd **and every parent** | session start | parent text loads in every child session |
| CLAUDE.md in subdirectories | lazily, on first file access there | |
| project `.claude/rules/**` | as above, project-relative | |
| agent and skill descriptions | every session | often forgotten; tone matters most here |
| plugin/hook injected context | every session | e.g. SessionStart hooks; measure via transcript |
| files on disk not referenced by anything | never | dead weight, still confusing |

Load behaviour is version-dependent and partly undocumented. So the model ships
with **canary tests**: a temporary rule containing a nonce, two `claude -p`
calls on the cheapest model, and an assertion on whether the nonce appears.
Run them on install and after each Claude Code update. Mark each model edge
*verified*, *documented* or *assumed*.

## 5. Pipeline

```
discover → snapshot ⇒ GATE → analyze → plan → apply → verify → record
```

1. **Discover**: global dir, project roots (configurable, deep by default),
   nested projects, parents, archives (tagged, not edited). Print the list, because
   a silent miss looks like completeness.
2. **Snapshot**: whitelist copy into a git repo, with JSON settings redacted
   (key-name + value-prefix + URL-userinfo nets). Then an independent scanner
   (gitleaks) acts as a **hard gate**, and false positives are allowlisted by fingerprint.
   No snapshot means no apply.
3. **Analyze**: deterministic checks (§6.1) produce findings, then an optional LLM
   pass (§6.2) classifies rule text.
4. **Plan**: findings become a change plan per file, shown as a diff. Group by
   decision type, so the user answers *"what to do with OpenWolf"* once, not
   nine times.
5. **Apply**: edit files. Where a project is a git repo, commit **only the
   instruction files** (`git commit -- <paths>`), never stage unrelated work,
   never push by default.
6. **Verify**: re-run the load graph, show before/after tokens per project, run
   canaries for any rule that moved scope, and confirm every pointer and import resolves.
7. **Record**: snapshot again, commit, and append to a change log.

## 6. Checks

### 6.1 Deterministic (no LLM, fast, CI-able)

| ID | Check |
|---|---|
| D1 | Always-loaded token total per project, broken down by source |
| D2 | File over N lines/words (default 200 lines) |
| D3 | Dangling `@import`, plus undocumented import syntax (`@\~/`) |
| D4 | Import of a protocol whose tool is not installed (binary missing, stale data dir) |
| D5 | Secrets in instruction files and settings (gitleaks rules + entropy) |
| D6 | Files in `~/.claude` that no load path reaches (dead weight) |
| D7 | Shouting: ALL-CAPS directives, "MUST BE USED", "PROACTIVELY", in agent/skill descriptions especially |
| D8 | Stale-fact markers: model IDs, version numbers, absolute dates, tool names no longer present |
| D9 | Scope leaks: project names or paths mentioned in global files |
| D10 | Duplication: near-identical passages across global ↔ project and parent ↔ child (shingling) |
| D11 | Referenced commands that don't exist (Makefile targets, package.json scripts, script paths) |
| D12 | CLAUDE.md untracked or gitignored in its own repo (informational) |
| D13 | Duplicate working copies of the same repo with diverging instructions |
| D14 | Fact drift: ports vs compose bindings, language versions vs `go.mod`/manifests, "zero warnings"/"not yet implemented" claims |
| D15 | Low repo-specificity: generic template text with few repo paths or identifiers per 100 lines |
| D16 | Framework attachment points: imports, `globs: **/*` rules, hooks in project `.claude/settings.json`, data dirs. Report all together |
| D17 | Status/history blocks: "Current phase", "Recently fixed", completed/remaining lists, dated logs |

Parsing requirement: Markdown-aware (fences, code spans, tool-managed marker
blocks such as `BEGIN:nextjs-agent-rules`). Never regex over raw text for
imports.

### 6.2 Judgement (LLM-assisted, proposes, never auto-applies by default)

- **Classify each rule:**
  - *user-specific* (keep)
  - *generic practice the model already follows* (cut)
  - *situational* (move to a path-scoped rule, skill or agent)
  - *reference material* (move to `docs/`, leave a plain-path pointer)
  - *history/status* (cut; git has it)
- **Harvest before delete.** When removing a framework, extract still-true
  project learnings into the CLAUDE.md first.
- **Propose `paths:` globs** for file-type-specific rules.
- **Fact-preservation ledger.** Every removed fact must be tagged derivable,
  generic, duplicate or moved. Unknown means keep and flag.

The fact-preservation ledger is the key quality control. It turns "the LLM
trimmed my file" into an auditable claim.

## 7. Safety requirements

- S1 Snapshot plus a clean secret scan before any write. No bypass flag; allowlist only.
- S2 Never read, copy or commit transcripts, history, caches or credentials files.
- S3 Redacted settings are never restored wholesale (placeholders); merge only.
- S4 Edit only instruction files and new `docs/` files. Never touch code.
- S5 Never stage or commit unrelated changes in a user's repo, and never push by default.
- S6 Secrets found in instruction files are reported with rotate-and-purge-history
  advice. Removing the line does not remove it from history.
- S7 Default to dry-run; `apply` requires an explicit flag or confirmation.
- S8 The secret scanner ships extra rules beyond gitleaks defaults (URL userinfo,
  credentials in prose). The defaults alone missed a live database password.
- S9 Respect repo state: commit with `--only`, never `--no-verify`, never
  commit to branches matching a configurable "don't touch" pattern (e.g. `codex/*`),
  and report anything left staged.
- S10 Call git and other tools with argument vectors, never composed shell strings.

## 8. Configuration (defaults shown)

```yaml
# ~/.config/loadout/config.yaml
roots: [~/Documents, ~/code, ~/src, ~/projects]
exclude: [node_modules, vendor, .venv, dist, build, graphify-out]
archive_globs: ["**/archief/**", "**/archive/**"]   # inventoried, never edited
snapshot:
  repo: ~/claude-snapshots
  remote_private_only: true
budgets:
  claude_md_lines: 200
  global_always_loaded_tokens: 1500
  project_always_loaded_tokens: 3000
checks:
  disable: []            # e.g. [D7]
  shouting_allow: ["NEVER run against production"]
llm:
  enabled: true
  classify_model: claude-sonnet-5-5
  canary_model: claude-haiku-5-5
apply:
  commit_in_project_repos: instruction-files-only   # or: none
  push: false
moves:
  reference_dir: docs/claude
```

## 9. Architecture

- **Core CLI** (Go, single binary, brew-installable). Handles discovery, the load graph,
  deterministic checks, snapshot/redaction, diff/plan/apply. gitleaks is
  embeddable as a Go library. Output: human report, JSON, SARIF (for CI).
- **LLM layer**: shells out to `claude -p` (uses the user's existing auth, no
  API key handling) or the Agent SDK. Batched per project, parallel across
  projects, structured JSON output validated against a schema.
- **Fan-out**: one LLM worker per project (or parent/child tree), all reading one
  versioned rubric file. That is how the manual run kept decisions consistent
  across 40 files. Each worker returns a structured result: the diff, the fact ledger,
  doubtful facts kept, and secrets found.
- **Claude Code plugin**: a skill and slash commands (`/loadout audit`,
  `/loadout fix <project>`) that call the CLI. Also a SessionStart hook that warns when
  a project's always-loaded budget is exceeded.
- **CI mode**: `loadout check` for a single repo. Deterministic checks only, non-zero
  exit on budget, secret or dangling-import failures.

## 10. Verification beyond canaries (later)

Token counts are necessary but not sufficient. Optional **adherence evals**:
for each kept rule, generate a probe task where the rule matters, then run before and
after with the cheapest capable model to check that slimming didn't lose
behaviour. Expensive, so opt-in and per project.

## 11. MVP and roadmap

1. **MVP**: discover, snapshot with gate, load graph, D1–D7, report. Read-only, with no
   LLM. Already useful: shows the bill and the leaks.
2. **v0.2**: plan/apply for mechanical fixes (D3 imports, D7 tone, D4 framework
   removal with confirmation), with per-repo instruction-only commits.
3. **v0.3**: LLM classification with fact ledger, move-to-docs, path-scope
   proposals, and canary verification.
4. **v0.4**: plugin plus SessionStart budget hook, CI mode, and AGENTS.md / Cursor
   rules via the same load-model interface.
5. Later: adherence evals, team mode (shared global rules + drift report).

## 12. Should this be built?

Yes, if it stays narrow. The manual run showed the value is mostly
**deterministic**: the bill, the leaks, dangling imports, dead frameworks, fact
drift. That part is cheap, fast, CI-able and needs no model. The LLM rewrite is
where per-file tools already compete. It is worth doing, but only with the fact
ledger, because an unaudited trim silently deletes knowledge.

Risks:
- Claude Code changes its load rules (mitigation: canaries, documented/assumed tags).
- Anthropic ships a built-in equivalent (likely in part, e.g. `/context`
  improvements). Mitigation: be the multi-project, multi-tool, safety-first layer, and
  upstream what makes sense.
- Scope creep into "AI writes your docs". Mitigation: the non-goals in §3.

The MVP (read-only fleet report) is a weekend-sized build, and it would have
caught 8 of the 10 problem types found here.

## 13. Open questions

- How stable is the load model across Claude Code releases? Canaries mitigate
  this, but could upstream expose it (`claude context --json`)? That would
  make D1 exact instead of modelled.
- Token counting: local tokenizer approximation vs. calling the count-tokens API?
- Should harvesting from abandoned frameworks be generic, or should it ship
  adapters per known framework?
- Multi-machine: snapshot repos per host, or one repo with host directories?
