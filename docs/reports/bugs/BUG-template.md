# Canonical BUG: BUG-{id}

> Status: reported / accepted / assigned / fixing / fixed / verifying / closed / rejected / duplicate
> Severity: P0 / P1 / P2 / P3
> Runtime ref: `{runtime-id}@{revision}`
> Found in review round: {n}
> Finding evidence: `{REV/QA/E2E evidence ref}`
> Original responsibility: `{assignment-id}`
> Owner: PM / Architect

## 1. Fingerprinted Specification Chain

Repair read order is BUG -> TASK -> contracts -> REQ -> UI/design -> rules.

| Order | Kind | ID | Path | Version | SHA-256 | Relevant clauses |
|:---|:---|:---|:---|:---|:---|:---|
| 1 | BUG | BUG-{id} | `docs/reports/bugs/BUG-{id}.md` | {version} | `{sha256}` | all |
| 2 | TASK | TASK-{id} | `docs/tasks/TASK-{id}.md` | {version} | `{sha256}` | scope/Closing Contract |
| 3 | contract | {id} | `docs/contracts/{id}.md` | {version} | `{sha256}` | §{n} |
| 4 | REQ | REQ-{id} | `docs/requirements/REQ-{id}.md` | {version} | `{sha256}` | FR/NFR |
| 5 | design/rule | {id} | `{path}` | {version} | `{sha256}` | §{n} |

## 2. Observed Contradiction

| Field | Value |
|:---|:---|
| expected | {REQ/contract/TASK assertion} |
| observed | {reproducible fact} |
| user/data/system impact | {impact} |
| reproduction | {steps/command/sample} |

## 3. Root Cause Investigation

| Hypothesis | Evidence | Result |
|:---|:---|:---|
| {hypothesis} | {code/test/log/data} | confirmed / rejected |

Accepted root cause: {supported explanation}

The root cause must explain the observed symptom. Browser traces, console logs,
network captures, screenshots, or deterministic command output are required
when the finding came from E2E evidence.

## 4. Closing Contract

The Closing Contract has **four explicit sub-fields** (per
`docs/agent-protocol.md` §S8 actions step 5 + `skills/bug-resolution/SKILL.md`
§Closing Contract). Each must be filled before the BUG is `accepted`.

### 4.1 Repair scope

{precise file paths / functions / data structures the Builder is permitted to
modify. Anything outside this scope is forbidden.}

### 4.2 Forbidden scope

{file paths / functions / data structures the Builder MUST NOT touch, even
if a related fix would be tempting. Examples: cross-module refactors,
out-of-scope REQ clauses, lock files, generated code.}

### 4.3 Before-fix evidence

{concrete reproduction evidence captured BEFORE the repair: command output,
screenshot/trace, JSONL line, deterministic test failure, or observed HTTP
status. Browser traces, console logs, network captures, or screenshots are
required when the finding came from E2E evidence.}

### 4.4 Retest contract

{how the repair will be re-verified: which original-responsibility agent
re-runs the targeted check, which test command proves the fix, which
historical PASS evidence is invalidated, what fresh evidence IDs replace it.}

```text
assert {old contradiction} == absent
assert {required behavior/field/state} == expected
assert {regression test} fails_before_and_passes_after
assert {data repair invariant} == satisfied
```

## 5. Acceptance And Repair

| Field | Reference |
|:---|:---|
| BUG acceptance evidence | `{approval-ref}` |
| repair assignment | `{assignment-id}` |
| Builder activation | `{activation-ref}` |
| repair fingerprint | `{commit/sha256}` |
| impact analysis | `{impact-ref}` |
| invalidated evidence | `{evidence-ids}` |

No repair starts before BUG acceptance and phase-two activation.

## 6. Verification

| Verification | Owner | Result | Evidence |
|:---|:---|:---|:---|
| targeted original-responsibility re-check | `{assignment-id}` | pass / fail | `{ref}` |
| required complete review round | round {n} | pass / pending | `{clean-round-ref}` |

## 7. Deduplication And History

Canonical BUG: BUG-{id} / duplicate of BUG-{id}

| Date | Event | Actor | Runtime revision | Evidence |
|:---|:---|:---|:---|:---|
| | | | | |
