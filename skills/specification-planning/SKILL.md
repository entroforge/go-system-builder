---
name: specification-planning
description: Use when designing architecture, final UI design packages, contracts, and task decomposition in the planning phase
category: methodology
version: 1.1.0
---
# Specification Planning

## Authority
The locked REQ is the baseline. Design, contracts, and tasks must trace back to it. Runtime authority lives in `docs/loop-definition.json`; stage contracts live in `docs/agent-protocol.md`; the method summary is inlined below.

## Entry Conditions
- The Loop is in `planning` (any phase: initialize, design, ui_prototype, contract_drafting, task_drafting, rework).
- The locked REQ is readable and its fingerprint matches the runtime baseline.

## Required Inputs
| Input | Path / field | Why |
|:---|:---|:---|
| Locked REQ | runtime `bound_req.path` | source of acceptance criteria and scope |
| Existing design | `docs/design/**` | reuse and conflict detection |
| Module prototypes | `docs/design/prototypes/<module>/{index.html, stories.md, flows.md, *.html}` | prototype gate input; current code IS the baseline |
| Rules | `docs/rules/*.md` | naming, security, api-design, state-machine constraints |
| Loop Definition | `docs/loop-definition.json` | planning exit transition and executable guards |

## Procedure
1. Read the locked REQ end-to-end; extract acceptance criteria, scope boundaries, and non-goals.
2. Draft the architecture: components, data model, state machines, data flow. Record decisions as ADRs under `docs/design/decisions/`.
3. Determine UI impact. If the REQ changes a user-visible surface, complete the UI design package before locking affected contracts; otherwise record that UI impact is `none` and continue.
4. For UI-impacting work, ensure the affected module's prototype set at `docs/design/prototypes/<module>/` exists and reflects the REQ's target behavior — `index.html` + `stories.md` + `flows.md` + page HTML files. The current implementation IS the baseline (no separate capture); the prototype describes the target only. The hybrid split is: HTML files (`index.html` + page HTML) come from `skills/ui-prototyping/SKILL.md`; `stories.md` content comes from `skills/user-story-design/SKILL.md` (with `USER-STORY-template.md` as the canonical format reference); `flows.md` content comes from `skills/user-flow-design/SKILL.md` (with `USER-FLOW-template.md` as the canonical format reference).
5. Draft contracts in order: FE-contract → BE-contract → SYNC-contract. Each must link to the REQ clause it satisfies and to the locked design/prototype.
6. Decompose into TASKs: each TASK binds one contract, has a Closing Contract (forbidden paths + required evidence), and obeys single-responsibility. Verify no TASK spans two contracts.
7. Check the actual `TR-002 planning_ready` contract: at least one locked contract and one complete TASK must exist, with the required planning evidence current. Request `TR-002` only when that contract is satisfied.
8. If document verification returns `document_fix_required` (`TR-004`), repair the affected documents. Re-open an architecture or UI decision only when verification evidence shows that the decision itself is invalid; otherwise keep rework bounded to the flagged contract or TASK.

## Outputs
- Architecture and ADR records under `docs/design/`.
- Locked final UI design package (when UI impact is changed) with fingerprints for HTML prototype, user story, and user flow.
- FE/BE/SYNC contracts with REQ and design traceability.
- TASK batch with Closing Contracts and single-responsibility assignments.
- Current planning evidence required by `TR-002`.

## Exit Conditions
- The planning checkpoint is committed and the Loop transitioned to `document_verification`.

## Stop Conditions
Stop immediately and surface to the human if any of:
- The REQ is ambiguous about a core acceptance criterion.
- A design decision conflicts with an immutable rule and cannot be resolved without REQ clarification.
- UI design package review is rejected by the human.
- User story, user flow, and prototype contradict each other in a way that changes product semantics.
- A contract cannot trace to a REQ clause.

## Non-Goals
- Do not implement code (that is the Builder Agent's job).
- Do not verify contracts (that is `document-verification`).
- Do not create the Agent Team (that is `team-planning`).

## Inlined Methodology

Planning is a single executable Loop phase (`planning.design`), not a document-production state machine. Architecture, optional UI design, contracts, and TASKs are work products within that phase; they do not each require a runtime transition. `TR-002 planning_ready` is the only planning exit and evaluates the current planning package. `TR-004 document_fix_required` returns failed documents to planning for evidence-bounded rework. The TASK lifecycle remains `candidate -> reviewed -> locked -> in_progress -> review -> done`; contracts and TASKs must trace back to the locked REQ. Invalid or guard-failing events do not change state, execute no side effect, and record a rejected event. Idempotency uses CAS revision checks; one committed transition per runtime revision.
