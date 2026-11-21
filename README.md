# loadout

**What Claude Code loads before you type, across every project on your machine.**

Instruction files grow by accretion: framework installers drop thousands of
words into `~/.claude`, CLAUDE.md files turn into changelogs, imports rot, and
uninstalled tools keep issuing orders through leftover hooks. loadout makes
that visible and safe to clean up.

Status: MVP. Read-only audit plus snapshot backups. No LLM, no network.

```
$ loadout audit
loadout audit · 38 projects (2 archived) · 0 errors, 14 warnings, 0 info

Every session starts with ≈4,335 tokens from ~/.claude:
  agent-description    ≈  1,049  (22 files)
  rule                 ≈  1,128  (8 files)
  skill-description    ≈  1,986  (38 files)
  …

PROJECT                        ALWAYS  ON-PATH  LAZY    ERR  WARN  INFO
~/code/shop-backend            ≈8,342  ≈4,361   ≈0      ·    1     ·
~/code/field-app               ≈8,051  ≈4,361   ≈0      ·    5     ·
…
```

## Install

Download a binary for macOS or Linux (amd64/arm64) from
[Releases](https://github.com/coolbox13/loadout/releases), or build from source:

```bash
go install github.com/coolbox13/loadout/cmd/loadout@latest
```

macOS release binaries are not notarized. Run `xattr -d com.apple.quarantine ./loadout`
after extracting them, or use `go install`.

## Commands

| Command | What it does |
|---|---|
| `loadout audit [dir…]` | Load cost per project plus findings. No dirs means discover from config roots. |
| `loadout audit --json` | Machine-readable output |
| `loadout audit --fail-on error .` | CI mode: exit 1 if any finding is at or above the level |
| `loadout graph [dir]` | Every source that loads for one directory, and when |
| `loadout snapshot [--commit MSG]` | Back up instruction files into a git repo, gated by a secret scan |

## The load model

loadout models when each source enters context:

| When | Sources |
|---|---|
| **always** | `~/.claude/CLAUDE.md` + imports (≤4 hops), `~/.claude/rules/**` without `paths:`, CLAUDE.md / CLAUDE.local.md in the directory **and every parent**, project `.claude/rules/**` without `paths:`, agent and skill descriptions |
| **on-path** | rules with `paths:` frontmatter, loaded when Claude reads or edits a matching file |
| **lazy** | CLAUDE.md in subdirectories, loaded when Claude works there |

It also honours `skillOverrides` ("off" skills cost nothing) and counts the per-project
auto-memory index (`~/.claude/projects/<dir>/memory/MEMORY.md`).

Token counts are estimates at ≈2.5 characters per token. That figure is calibrated against Claude Code's
own `/context` on real files, which measured 2.0–3.0 characters per token. In tests, estimates landed
within about 5% of `/context` for the sources loadout models.

**Not modelled:** built-in skills, skills synced from claude.ai, plugin skills, MCP server
instructions, and the system prompt. For the complete, exact picture, run
`claude -p "/context"` in the project directory. Built-in and synced skills can be switched off with
`skillOverrides` in `~/.claude/settings.json`; plugin skills cannot.

## Checks

| ID | Check | Severity |
|---|---|---|
| D1 | Project's always-loaded total over budget | warn |
| D2 | CLAUDE.md over the line budget (default 200, per Claude Code docs) | warn |
| D3 | Dangling `@import`; undocumented `@\~` syntax; beyond 4 hops; `globs:` instead of `paths:` (loads always) | error / warn |
| D4 | Hook runs a missing script or a binary not on PATH; known framework wired in but uninstalled | error / warn |
| D5 | Secrets: URL passwords, token prefixes, `password: value` prose, literal secrets in settings JSON and in MCP server definitions in `~/.claude.json` | error / warn |
| D6 | Markdown in `~/.claude` that nothing loads | info |
| D7 | All-caps shouting in instructions; forceful agent/skill descriptions ("MUST BE USED", "PROACTIVELY") | info / warn |

Imports are parsed Markdown-aware: `@` inside code fences and code spans is ignored, and so are emails,
decorators and `@mentions`.

## Snapshot safety

- Whitelist only: never transcripts, history, caches or plugin data.
- `settings*.json` are copied **redacted**. Secrets are matched by key name, token prefix and URL userinfo.
- Every snapshot is scanned by the built-in rules, plus `gitleaks` when installed. Any hit blocks the
  commit, and there is no bypass flag. Accepted false positives go in `<repo>/.loadoutignore` as
  `path:rule:line`.
- The snapshot repo itself is never audited or snapshotted.

## Configuration

`~/.config/loadout/config.yaml` (all optional):

```yaml
roots: [~/Documents, ~/code, ~/src, ~/projects]   # missing roots are skipped
exclude: [node_modules, vendor, .venv, dist, build, graphify-out, target]
archive_dirs: [archief, archive]      # inventoried; only secret findings shown
max_depth: 7
budgets:
  claude_md_lines: 200
  project_always_tokens: 8000
checks:
  disable: []                         # e.g. [D7]
  shouting_per_file: 5
snapshot:
  repo: ~/claude-snapshots          # must already be a git repo (git init)
```

## Roadmap

See `docs/DESIGN.md` (and `docs/FIELD-NOTES.md` for where this came from): plan/apply for mechanical fixes, LLM-assisted
classification with a fact-preservation ledger, canary verification of load
behaviour, a Claude Code plugin, and AGENTS.md / Cursor support.

## License

MIT
