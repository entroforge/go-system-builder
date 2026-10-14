# Source and release boundaries

The source repository contains the reusable framework, implementation, tests,
build tooling and contributor reference material. The installable release
contains the assets explicitly listed in `include.txt`, generated CLI binaries,
the generated manual, installation guide and release inventory.

## Repository layout and Claude Code

The source checkout is a template factory, not an installed application. Keep
the two layouts distinct:

| Source | Installed project / purpose |
| --- | --- |
| `CLAUDE.md` | Source-maintainer instructions only; excluded from releases |
| `AGENTS-template.md` | Project-root `AGENTS.md` |
| `skills/<name>/SKILL.md` | `.claude/skills/<name>/SKILL.md` |
| `agents/<role>.md` | `.claude/agents/<role>.md` |
| `settings.json` | `.claude/settings.json`, including Hook registration |
| `loop-template.md` | `.claude/loop.md`, the Harness wake-up prompt |
| generated binary and `loop-harness.md` | `.claude/bin/` |
| `docs/` allowlisted files | Supported guides, rules, templates and examples |
| `cmd/`, `internal/`, `tests/` | Framework implementation and tests; not installed |
| `blueprint/` | Maintainer design reference; not installed |
| `.local/`, `dist/` | Local evidence, rollback materials and generated outputs; not committed |

The checked-in root `settings.json`, `skills/` and `agents/` are the reusable
sources. The source checkout's ignored `.claude/` is local state; it is not a
second source of truth for those assets. In a business project, shared Claude
settings and role/skill definitions can be versioned at their installed
`.claude/` paths, while personal settings and Runtime data need separate handling.
Do not copy this source checkout's blanket `.claude/` ignore rule to a business
project as a general Claude Code recommendation.

Official references reviewed on 2026-10-07:

- [Memory and project instructions](https://code.claude.com/docs/en/memory):
  keep persistent instructions concise (the suggested target is under 200 lines).
  Imports load their contents too; splitting a long instruction file into imports
  does not reduce context by itself. This source's short `CLAUDE.md` uses navigation
  paths rather than importing all templates, Skills or historical reports.
- [Skills](https://code.claude.com/docs/en/skills): project skills live under
  `.claude/skills/`. Names/descriptions are available for discovery; full content
  normally loads on invocation. Skills explicitly preloaded into subagents have
  different loading behavior. Keep detailed task procedures in the relevant Skill.
- [Subagents](https://code.claude.com/docs/en/sub-agents): project definitions live
  under `.claude/agents/`. Keep role definitions separate from project memory.
- [Settings](https://code.claude.com/docs/en/settings): shared project settings live
  in `.claude/settings.json`; `.claude/settings.local.json` holds local overrides.

The current Memory documentation says direct `AGENTS.md` loading requires Claude
Code 2.1.277 or later and depends on the presence of `CLAUDE.md`/local instructions.
That does not retroactively certify the framework's reference version 2.1.276.
This housekeeping change does not change target-project instruction loading,
installer behavior or platform compatibility claims.

Ordinary Markdown reports are not all automatically loaded because they exist
in the repository. Likewise, `.gitignore` is not an access-control boundary or a
guarantee that an agent cannot read archived files. Archival keeps maintenance
history out of the normal source/release inventory; concise entry instructions
and task-specific reading control context size. Do not move every `docs/rules/`
file into `.claude/rules/`: rules without path scoping load at startup.

Claude Code documents configuration and context-loading conventions; it does
not mandate a universal layout for Go source, release evidence or archives.
The separation below is this repository's maintenance policy.

## Maintenance history and reusable documentation

Keep public usage, upgrade and recovery instructions in `docs/`, blank report
templates in `docs/reports/`, and worked examples in `docs/examples/`. A guide
with “repair” in its name remains public when it describes supported recovery
behavior rather than a specific maintenance session.

Do not commit dated optimization plans, session transcripts, local verification
logs, source snapshots, patches or project-instance evidence to this template.
Local maintenance history may be retained under the ignored
`.local/framework-maintenance/` directory. This is not a durable team archive:
preserve required audit evidence in team artifact storage before removing the
checkout. Release evidence belongs in CI artifacts or release attachments,
with version, source identity and actual platform acceptance status.

Archive dated reports and their referenced logs together, preserving original
bytes, relative directory structure and a path/SHA-256 manifest. Keep a local
README as the archive entry point. If links need relocation, provide a marked
reading copy and retain the untouched original. Never rewrite a historical
receipt merely to make it describe the current release.

Move `.bak-*` source snapshots into the same ignored archive instead of leaving
them beside active Go files. Keep the real regression tests in source control.
Do not relocate active installation/rollback directories just for appearance:
their receipts and scripts may bind exact paths. Root `dist/` is generated output,
not an alternative authoritative source tree.

For a documentation-only reorganization, check moved-file hashes, local links,
release inclusion and preservation of unrelated files. It does not require
rebuilding binaries, launching business requirements or running browser tests.

## Release inclusion and publication

Documentation entries in `include.txt` are individual files. Add new public
guides and templates explicitly, then verify the staged release. Do not replace
these entries with recursive `docs` or `docs/reports` entries. Packaging cleanup
rules are additional protection, not the inclusion policy.

Before publishing:

1. Run source checks with `make ci-verify`.
2. Inspect the archive and run the staged release checks (`make doctor-staged`):
   `release-graph validate`, `init`, `doctor` and `validate --all` in a disposable copy.
3. Verify extracted bytes with `tools/release-manifest.py` before initialization
   changes them. Check the license, installation guide, public documentation,
   templates and supported-platform binaries are present.
4. Record actual Claude Code workflow acceptance separately. Packaging checks
   and cross-compilation do not attest that a real Claude session passed.

Building an archive does not publish it or authorize a formal release.
