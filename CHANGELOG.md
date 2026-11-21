# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/).

## [1.0.1] - 2026-11-21

### Fixed

- `loadout version` printed `dev` when installed with `go install …@vX.Y.Z`.
  It now reads the module version recorded in the binary. Release binaries
  were not affected.

## [1.0.0] - 2026-11-21

First stable release.

### Commands

- `loadout audit [dir…]` reports the always, on-path and lazy load cost per
  project and lists findings, as text or `--json`. `--fail-on` gives a CI exit
  code.
- `loadout graph [dir]` lists every instruction source that loads for a
  directory, and when it loads.
- `loadout snapshot [--commit MSG]` backs up instruction files into a git
  repository. Settings are redacted, and a secret scan gates the commit
  (built-in rules, plus gitleaks when it is installed). Only `global/` and
  `projects/` are ever committed.

### Load model

- Covers `~/.claude/CLAUDE.md` and its imports (up to 4 hops), user and project
  rules with and without `paths:`, parent-directory CLAUDE.md files, lazy
  subdirectory CLAUDE.md files, agent and skill descriptions, and the
  per-project auto-memory index.
- Skills switched off via `skillOverrides` are not counted.
- Token estimate: characters / 2.5, calibrated against Claude Code's `/context`.

### Checks

- **D1:** per-project budget.
- **D2:** file length.
- **D3:** imports. Dangling, undocumented `@\~`, beyond 4 hops, or `globs:`
  used instead of `paths:`.
- **D4:** hook and framework wiring. Missing scripts or binaries, and
  uninstalled frameworks still wired in.
- **D5:** secrets. In prose, settings JSON, and MCP definitions in
  `~/.claude.json`.
- **D6:** dead weight in `~/.claude`.
- **D7:** tone. All-caps directives, and forceful agent or skill descriptions.

### Distribution

- Static binaries for macOS and Linux on amd64 and arm64, with SHA-256 checksums.

[1.0.1]: https://github.com/coolbox13/loadout/compare/v1.0.0...v1.0.1
[1.0.0]: https://github.com/coolbox13/loadout/releases/tag/v1.0.0
