# Prelude: Loop Engineering Onboarding

This document has two jobs:

1. Explain where each artifact lands when applying this template to a real
   project.
2. Explain where behavior lives once the project is running.

It is not runtime authority. Runtime authority lives in
`.claude/loop-state.json` and `docs/loop-definition.json`.

---

## 1. Applying This Template

This repository is a **template factory**. Its layout differs from the layout a
target project uses. The release tarball (`vibe-coding-loop-template-<version>`)
contains only the artifacts a target project needs.

### Source repository layout (this repo)

```text
vibe-coding/
├── AGENTS-template.md        template source for target AGENTS.md (Layer 1 entry)
├── prelude.md                this file
├── loop-template.md          template source for target .claude/loop.md (Layer 2 Wake-up)
├── settings.json             template source for target .claude/settings.json (Layer 3 Hooks)
├── skills/                   template source for target .claude/skills/
├── agents/                   template source for target .claude/agents/
├── docs/                     template sources (README, rules, contracts, design, ...)
├── Makefile                  build/test/verify/release targets
├── cmd/ internal/ go.mod     Go Harness source (compiled into loop-harness binary)
├── tests/                    Harness test fixtures
├── packaging/                release packaging (include list, install guide)
├── loop-harness.md           generated agent-facing Manual (gitignored; produced by `make manual`)
└── .claude/                  REQ-002 self-evolution instance runtime (NOT a template)
    ├── loop-state.json       active Loop runtime plus resumable milestone
    ├── loop-events.jsonl     audit journal
    ├── hook-decisions.jsonl  hook audit
    └── bin/loop-harness      local dev build (gitignored)
```

Note: `.claude/` in this repo holds the **local instance runtime** for Loop
Engineering applied to this template repository. It may be intentionally
inactive; it is not part of the template and is excluded from the release
tarball.

### Release tarball layout (what users receive)

```text
vibe-coding-loop-template-<version>/
├── INSTALL.md                     short apply guide
├── prelude.md                     this file (full version)
├── AGENTS-template.md             Layer 1 entry source
├── loop-template.md               Layer 2 Wake-up Prompt source
├── loop-harness.md                Layer 2 Manual source -> .claude/bin/loop-harness.md
├── settings.json                  Layer 3 Hook registration
├── skills/                        SKILL.md files
├── agents/                        7 agent definitions
├── docs/                          all templates + Loop definitions + rules
├── packages/design-tokens/        Foundation token source + CSS
├── tools/ui-lab/                  Storybook MCP wiring
├── tools/visual-qa/               snapshot-drift protocol
└── .claude/bin/loop-harness       precompiled binary for this platform
```

The Manual is generated at release time from `docs/loop-definition.json` plus
the guard spec registry compiled into the binary, so it always matches the
shipped binary's behavior. The tarball ships it at the root; the install
guide copies it to `.claude/bin/loop-harness.md` next to the binary so an
`ls .claude/bin/` shows both.

### Target project layout (where each artifact lands)

Apply the tarball to a target project using this mapping:

| Tarball entry | Target project location | Action |
|:---|:---|:---|
| `AGENTS-template.md` | `AGENTS.md` (project root) | copy and fill in `{project name}`, commands; Layer 1 entry |
| `prelude.md` | `prelude.md` (project root) | copy as-is |
| `loop-template.md` | `.claude/loop.md` | copy as-is; Layer 2 Wake-up Prompt |
| `loop-harness.md` | `.claude/bin/loop-harness.md` | copy as-is; Layer 2 agent-facing Manual (transition checklist) |
| `settings.json` | `.claude/settings.json` | copy as-is; Layer 3 Hook registration |
| `skills/` | `.claude/skills/` | copy recursively as-is |
| `agents/` | `.claude/agents/` | copy recursively as-is |
| `docs/` | `docs/` | copy recursively; fill `project.yaml`, `project-map.md` |
| `packages/` | `packages/` | copy recursively (design tokens) |
| `tools/ui-lab/` | `tools/ui-lab/` | copy recursively |
| `tools/visual-qa/` | `tools/visual-qa/` | copy recursively |
| `.claude/bin/loop-harness` | `.claude/bin/loop-harness` | copy as-is from the matching platform tarball |

After copying, initialize the runtime:

```bash
# The Loop runtime files are NOT in the tarball. Use the harness to seed them.
.claude/bin/loop-harness init --root .  # writes a fingerprint-valid inactive runtime
.claude/bin/loop-harness doctor --root .
.claude/bin/loop-harness validate --all --root .
```

`init` computes the local Loop Definition and Hook policy fingerprints and
writes a schema-valid inactive `.claude/loop-state.json` only when no runtime
files exist. It refuses to overwrite a state file or journal; if its own
bootstrap marker remains after an interruption, rerun `init` to complete it.
Use it to bootstrap a project; after a terminal REQ, a human uses `loop-harness runtime
rollover --approved-by <identity> --approval-evidence <human-decision-id>
--root .` instead. Rollover archives the completed Runtime and journal before
creating the clean inactive pair. The evidence ID must reference valid
`human_decision` evidence produced by that identity and scoped to
`runtime_rollover:<terminal-runtime-id>@<terminal-revision>`. When adding the
evidence, pass `--scope-ref runtime_rollover:current`; the harness expands it
to the commit's terminal revision. This is an
auditable local authorization record, not an external identity-provider
assertion. The first
`loop-harness req bind` on a human-locked REQ overwrites the inactive placeholder
authorization and sets the cursor to S1.

The target project ends up with this shape:

```text
user-project/
├── AGENTS.md                          filled from AGENTS-template.md (Layer 1)
├── prelude.md
├── docs/
│   ├── project.yaml                   filled
│   ├── project-map.md                 filled from template
│   ├── agent-protocol.md              authoritative Main Spine (S0-S11 stage contracts)
│   ├── loop-definition.json           legal Loop transitions
│   ├── hook-policy.json               Hook enforcement policy (Layer 3)
│   ├── requirements/                  empty; use REQ-template.md
│   ├── contracts/ tasks/ reports/     empty; filled by Loop progression
│   └── rules/                         as-is
├── packages/design-tokens/            from tarball (tokens.json + tokens.css)
├── tools/ui-lab/                      Storybook MCP wiring
├── tools/visual-qa/                   snapshot protocol; pixels ≠ Thesis
└── .claude/
    ├── settings.json                  from tarball root (Layer 3 registration)
    ├── loop.md                        from loop-template.md (Layer 2 Wake-up)
    ├── skills/                        from tarball root
    ├── agents/                        from tarball root
    ├── bin/loop-harness               precompiled only (see INSTALL.md §4 for source build)
    ├── bin/loop-harness.md            agent-facing Manual (transition checklist)
    ├── loop-state.json                seeded inactive by `loop-harness init` (includes milestone)
    ├── loop-events.jsonl              empty, seeded by `loop-harness init`
    └── hook-decisions.jsonl           created at runtime
```

`bin/loop-harness.md` is regenerated by `loop-harness init` and `loop-harness
manual`. Hook recovery packets point to it whenever the Agent is blocked or
the next action is unclear; `warn`/`block` messages also append a deep link.

---

## 2. Responsibility Map

Once a project is running, behavior lives in these places:

```text
AGENTS.md                         static constitution and routing
Loop Definition                   legal state transitions
.claude/loop-state.json           current facts + resumable milestone
Methodology Skills                event procedures
Best-practice Skills              professional quality criteria
Agent Definitions                 identity and maximum capabilities
team manifest + message envelope  assignment and activation scope
Hooks                             Controller trigger + positive guidance + deterministic permission enforcement
versioned docs                    specifications and evidence
```

## 3. Main Session

The main session is the Orchestrator. It reads current runtime state, loads
`.claude/skills/loop-orchestration/SKILL.md`, selects one legal event, and
routes to one primary methodology. It does not reproduce all Loop procedures in
its permanent prompt.

## 4. Subagents

Claude Code subagents use `.claude/agents/*.md`. Every assignment is singular
and fingerprinted. The default dispatch mode is `plan_checkpoint`: the Worker
reads the fingerprinted chain, sends one PLAN_REPORT, and continues
immediately (the PostToolUse observer records the checkpoint and activates the
Worker). `plan_approval_required` keeps the two-round flow: phase one is
read-only and follows the document order in the request; phase two is a
separate activation bound to the approved read-back, tools, paths, commands,
and current runtime revision.

After the main session dispatches, the active Driver work is to watch for that
Agent's PLAN_REPORT (or, in approval mode, collect and verify the read-back,
then approve, reject, revoke, or reactivate the same Agent). The main session
must not finish the delegated responsibility itself while the Agent is pending.

Team construction uses `.claude/skills/team-planning/SKILL.md` and a manifest
validated by:

```bash
loop-harness validate --all --root .
loop-harness team launch --root . \
  --manifest <manifest.json> \
  --request-template <readback-request.json>
```

## 5. Progressive Disclosure

Load procedures only when their trigger applies:

| Situation | Skill |
|:---|:---|
| start, recovery, pause, resume, next action | `loop-orchestration` |
| first UI REQ, missing/stale DESIGN.md, new product surface | `design-foundation` |
| design, UI, contracts, candidate tasks | `specification-planning` |
| contract/task batch review | `document-verification` |
| teammate spawn or reactivation | `agent-dispatch` |
| workgroup planning | `team-planning` |
| blocking finding and repair | `bug-resolution` |
| changed artifact or stale evidence | `impact-analysis` |
| complete review-round decision | `clean-round-evaluation` |
| acceptance and release handoff | `acceptance-and-handoff` |

Best Practices are selected from risk tags and the assigned responsibility.

## 6. Immutable Boundaries

- A Loop binds one locked REQ.
- `/loop` only schedules the Wake-up Prompt. `loop-harness req bind` binds the
  human-locked REQ and is sufficient engineering authorization; no separate
  `/goal` is required.
- Loop automation cannot change or lock the REQ.
- UI-impacting work first records `design investment` (`local / core / extended / N/A`). Local one-shot UI does not publish `docs/design/DESIGN.md`. Core/Extended need a published Project Design Foundation before later `UI impact=changed` REQs inherit it, then a reviewed final UI design package (`docs/design/prototypes/{module}/` with `index.html` + page HTML files with the 4-field header, plus `stories.md` and `flows.md` carrying S-NNN / F-NNN entries each with its REQ-id) before contract lock.
- No side effect occurs before activation.
- Delivery Verifier, QA, and E2E Tester work uses single-responsibility assignments.
- Blocking findings create or reference a canonical BUG before repair.
- Repair invalidates affected evidence and is followed by targeted re-verification and a new complete review round.
- Acceptance and release audit require one current clean round.
- Final squash merge and formal release require human release approval.
