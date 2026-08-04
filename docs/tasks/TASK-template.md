# Task: TASK-{id}

> Status: locked / activated / working / reported / review / blocked / complete / stale
> Version: v1.0.0
> Bound REQ: REQ-{id}
> Primary contract: {FE/BE/SYNC-id}
> Closing Contract: TASK-{id}#closing-contract
> Runtime ref: `{runtime-id}@{revision}`
> Team manifest: `{team-manifest-path}`
> Assignment ID: `{assignment-id}`
> Builder Agent: `{agent-id}`

## 1. Objective

{One testable objective and user-visible value.}

## 2. Document Manifest

The phase-one request assigns read order and fingerprints. This table must link
the same authoritative chain and must not replace reading it.

| Order | Kind | ID | Path | Version | SHA-256 | Clauses |
|:---|:---|:---|:---|:---|:---|:---|
| 1 | task | TASK-{id} | `docs/tasks/TASK-{id}.md` | v1.0.0 | `{sha256}` | all |
| 2 | contract | {contract-id} | `docs/contracts/{contract-id}.md` | {version} | `{sha256}` | §{n} |
| 3 | req | REQ-{id} | `docs/requirements/REQ-{id}.md` | {version} | `{sha256}` | FR/NFR/acceptance |
| 4 | ui/design | {id/N/A} | `{path/N/A}` | {version/N/A} | `{sha256/N/A}` | §{n}/N/A |
| 5 | rule | {rule-id} | `docs/rules/{rule}.md` | locked | `{sha256}` | all |

Repair assignments prepend the canonical BUG as order 1 and shift the remaining
documents. The request remains the authority for exact order.

## 3. Contract Coverage

| REQ clause | Contract clause | Task output | Verification responsibility |
|:---|:---|:---|:---|
| FR-{id} | {contract-id} §{n} | {output} | {VER responsibility ID} |

## 4. Scope

| Type | Paths / Commands |
|:---|:---|
| read paths | `{path}` |
| prospective write paths | `{path}` |
| forbidden paths | `.claude/loop-state.json`, `{path}` |
| allowed command classes | test / lint / build / read-only |
| output paths | `{implementation/test/report paths}` |

Dynamic permission is the intersection of Agent Definition, manifest, this
scope, activation, runtime state, and Hook policy.

## 5. Selected Skills

| Skill | Category | Source | Version | SHA-256 | Applicability |
|:---|:---|:---|:---|:---|:---|
| two-phase-activation | methodology | `.claude/skills/two-phase-activation/SKILL.md` | 1.0.0 | `{sha256}` | teammate activation |
| {skill} | best-practice | `.claude/skills/{skill}/SKILL.md` | {version} | `{sha256}` | {risk/responsibility} |

## 6. Outputs And Evidence

| Output | Path | Acceptance |
|:---|:---|:---|
| implementation | `{path}` | {contract assertion} |
| tests | `{path}` | {behavior/failure coverage} |
| completion report | `{agent-message-path}` | schema-valid |
| delivery evidence | `docs/reports/review/REV-{id}.md` | assigned dimension result |
| QA evidence | `docs/reports/qa/QA-{id}.md` | assigned dimension result |
| E2E evidence | `docs/reports/e2e/E2E-{id}.md` | real-browser flow result |

## 7. Closing Contract

```text
assert {contract clause} == satisfied
assert {verification command} == pass
assert changed_paths subset_of activated_write_paths
assert scope_deviations == []
```

## 8. Dependencies

| Dependency | Required evidence | Status |
|:---|:---|:---|
| {TASK/assignment} | `{evidence-ref}` | pending / satisfied |

## 9. Lifecycle Evidence

| Evidence | Reference | Fingerprint / Revision |
|:---|:---|:---|
| document verification | `{review-evidence-ref}` | `{sha256}` |
| phase-one request | `{message-ref}` | `{sha256}` |
| approved read-back | `{message-ref}` | `{sha256}` |
| activation | `{activation-ref}` | runtime revision {n} |
| completion report | `{message-ref}` | `{sha256}` |

## 10. Findings And Repairs

| Finding | Canonical BUG | Impact record | Repair assignment | Targeted re-verification | Status |
|:---|:---|:---|:---|:---|:---|
| {finding-id} | BUG-{id} | `{impact-ref}` | `{assignment-id}` | `{evidence-ref}` | open / verified / closed |

BUG procedure is defined by `.claude/skills/bug-resolution/SKILL.md`; review
completion is defined by `.claude/skills/clean-round-evaluation/SKILL.md`.

## 11. History

| Date | Event | Actor | Runtime revision | Evidence |
|:---|:---|:---|:---|:---|
| | | | | |
