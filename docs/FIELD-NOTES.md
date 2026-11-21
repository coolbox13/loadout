# Field notes

loadout came out of cleaning up the Claude Code instructions on one real
developer machine by hand: one Mac, about 40 projects, a global `~/.claude` grown
by framework installers. These notes record what that cleanup taught, and then
what building the MVP taught. Each note ends with what a tool should do about it.

## The numbers

| | Before | After |
|---|---|---|
| Global always-loaded instructions | ~3,000 words | ~830 words |
| Project CLAUDE.md, 39 files | 6,923 lines | 1,875 lines |
| Files over the 200-line guidance | 13 | 0 |
| Broken `@imports` that nobody noticed | 6 | 0 |
| Projects still running an uninstalled framework | 11 | 0 |
| Credentials found in instruction files and settings | 8 | 0 in instruction files |

## Backup and discovery

1. **`~/.claude` cannot be the repo.** It is gigabytes of transcripts, caches and
   plugin data. → Snapshot an explicit whitelist into a separate repo.
2. **Settings files hold live secrets.** API keys, access tokens, and a password
   inside a database URL sitting in an `args` list with no secret-looking key.
   → Redact by key name, by value prefix and by URL userinfo, then gate on an
   independent scanner.
3. **Default scanner rules miss credentials written in prose.** A database
   password in a connection string inside a CLAUDE.md passed gitleaks' default
   rules. → Ship extra rules, and assume a backup may already hold what a scan
   missed.
4. **Scanner fingerprints are path-relative.** Scanning an absolute path
   silently ignored the allowlist. → Scan from the repo root.
5. **A shallow depth limit silently misses projects.** Depth 4 missed six nested
   projects, and the backup looked complete when it wasn't. → Discover deep, use
   explicit excludes, and print the list.
6. **Installed is not loaded.** A framework installer left 57 Markdown files in
   `~/.claude` that Claude Code never reads. → Model what actually loads, and
   report dead weight separately.

## The global layer

7. **Most global text was generic best practice** that the model already
   follows: KISS/DRY, OWASP lists, coverage mandates. It loaded in every session
   of every project, including repos with no code. → Classify each rule as
   *user-specific* (keep), *generic* (cut), or *situational* (scope it).
8. **Stale facts and misplaced scope.** The global rules held outdated model
   IDs, obsolete tool names, and one project's details. → Flag version numbers,
   model IDs and project names in global files.
9. **Canaries beat assumptions.** The docs did not say whether `paths:`
   frontmatter works in user-level rules. A rule containing a nonce, plus two
   headless `claude -p` runs (before and after reading a matching file), proved
   it does. → Verify load behaviour empirically and re-check after updates.
10. **Imports break silently.** `@\~/...` is undocumented syntax, and its
    targets had moved anyway, so nothing loaded and nothing complained.
    → Resolve every import.
11. **Agent and skill descriptions are always loaded too.** Many of them said
    "MUST BE USED" or "PROACTIVELY", which nudges the model to spawn helpers for
    trivial work. → Include descriptions in the budget and in the tone check.

## Projects

12. **Stale facts are the norm.** Commands for scripts that no longer exist,
    wrong ports, wrong language versions, "zero warnings" claims, "not yet
    implemented" for shipped features, phase logs. → Deterministic fact checks
    against the repo pay off.
13. **A framework is more than its import.** One uninstalled framework stayed
    wired in through four attachment points: an `@import`, a rule with
    `globs: **/*`, hooks in project `.claude/settings.json` on every
    Read/Write/Edit, and a data directory. The hooks kept it half alive.
    → Removal must find every attachment point.
14. **Harvest before deleting, with a staleness check.** The framework's
    learned notes partly contradicted later decisions. → Keep only durable
    entries, and check each one against the repo.
15. **Template boilerplate passes for project docs.** One 400-line CLAUDE.md
    contained no project facts at all. → Flag files whose content is barely
    specific to the repo.
16. **Parents and clones duplicate.** A parent CLAUDE.md loads in every child,
    and children restated it. Clones of the same repo kept diverging copies.
    → Deduplicate along the load graph.
17. **Leave tool-managed marker blocks alone.** Some tools re-insert them, so
    removing them only causes churn.
18. **Commit time is full of surprises:**
    - feature branches
    - throwaway clones on PR branches
    - users' own uncommitted edits next to yours
    - a gitignored CLAUDE.md
    - a failing pre-commit hook that has nothing to do with the change

    → Commit only instruction paths (`--only`), and report anything left
    behind.
19. **Parallel workers with one shared written brief** cleaned about 40 files
    in minutes with consistent decisions. → Fan out per project, and keep
    one rubric as the single source of policy.

## Building the MVP

20. **Green unit tests, wrong first real run.** Real files produced five classes
    of false positive:
    - code expressions read as secrets
    - a prose mention read as framework wiring
    - a path matched inside a longer path
    - a shell keyword taken for a missing binary
    - the tool auditing its own snapshot repo

    → Turn each one into a regression test.
21. **A parity run against the script it replaced was the cheapest bug hunt.**
    It found:
    - lost executable bits
    - JSON numbers turned into float64
    - a commit sweeping in unrelated changes
    - a bug in the old script

    → Diff the new output against the old before switching.
22. **After cleanup, skill descriptions are the biggest per-session cost**, at
    ≈2k of ≈4.3k global tokens. → Rank sources by cost, not only projects.
23. **Unchecked writes matter in a CLI that pipes into other tools.** → Use an
    error-capturing writer, checked once.
