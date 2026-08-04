---
name: document-verification
description: Use when reviewed contracts and candidate tasks require independent specification verification
category: methodology
version: 1.1.0
---
# Document Verification

## Authority
The reviewer produces evidence; the Orchestrator evaluates the gate. Runtime authority lives in `docs/loop-definition.json`; the S5 stage contract lives in `docs/agent-protocol.md`; the method summary is inlined below.

## Entry Conditions
- The Loop is in `document_verification`.
- Planning has produced reviewed contracts and a candidate TASK batch.
- All artifacts carry fingerprints (path + version + SHA-256 + baseline generation).

## Required Inputs
| Input | Path / field | Why |
|:---|:---|:---|
| Candidate TASK batch | `docs/tasks/TASK-*.md` | verify executability and Closing Contracts |
| Contracts | `docs/contracts/FE-*.md`, `BE-*.md`, `SYNC-*.md` | verify REQ coverage and consistency |
| Locked REQ | runtime `bound_req.path` | top-down traceability source |
| Design + UI design package | `docs/design/**` | contract and TASK linkage target |
| Rules | `docs/rules/*.md` | naming, api-design, security constraints |
| Team manifest | assignment manifest for this round | single-responsibility and independence |

## Procedure
1. Read the assigned TASK bottom-up: TASK → its primary contract → related contracts → locked REQ → design/UI design package → applicable rules.
2. Check REQ coverage: every acceptance criterion in the REQ maps to at least one contract clause, and every contract clause maps to at least one TASK.
3. Check consistency: contracts agree on data shapes, error codes, state transitions, and API surfaces across FE/BE/SYNC boundaries.
4. Check executability: each TASK has a clear input, output, allowed paths, forbidden paths, and required evidence — a Builder can execute it without further design decisions.
5. Check link integrity: every cross-document reference resolves to a real file at the declared version with a matching fingerprint.
6. Check Closing Contracts: every TASK declares what evidence must exist to close it, and that evidence is feasible given the current toolchain.
7. Produce a REV report per assigned dimension with explicit PASS/FAIL/N-A and referenced evidence paths.
8. Aggregate results: if all dimensions PASS, the Orchestrator requests TR-003 (`document_pass`). If any dimension requires a document fix, request TR-004 (`document_fix_required`). If a REQ change is needed, request TR-005 (`req_change_required`).

## Outputs
- REV reports under `docs/reports/review/` with per-dimension verdicts.
- A recommendation: TR-003, TR-004, or TR-005, with the affected references for each failing dimension.

## Exit Conditions
- All assigned dimensions have explicit PASS or FAIL evidence, and the Orchestrator has requested the appropriate transition.

## Stop Conditions
Stop immediately and surface to the human if any of:
- A fingerprint changed mid-review (the artifact was edited after verification started).
- A required document layer is missing entirely.
- Two authorities contradict each other and cannot be reconciled without a REQ decision.
- Reviewer independence is compromised (the reviewer authored the artifact under review).

## Non-Goals
- Do not repair the reviewed documents — return them to `planning.rework`.
- Do not lock the batch or activate Builders — that follows TR-003.
- Do not judge implementation quality or browser behavior — those belong to the S7 Delivery Verifier, QA, and E2E Browser workgroups.

## Inlined Methodology

Loop Engineering uses one stable Agent Definition plus phase-one read-back prompt plus main-session approval plus phase-two activation prompt plus runtime-aware PreToolUse observation. The same Agent remains alive across both conversations so its document reading and submitted understanding stay in one context. Phase one is not enforced by trusting the prompt. The Agent Definition declares a static maximum tool set, while Hooks use the Agent lifecycle and activation record to report writes or persistent side effects before phase two. Read order: `TASK -> CONTRACTS -> REQ -> UI DESIGN PACKAGE/DESIGN -> RULES` for normal assignments; `BUG -> TASK -> CONTRACTS -> REQ -> UI DESIGN PACKAGE/DESIGN -> RULES` for repair assignments. References are ordered and fingerprinted. Structured read-back response: `ready`, `conflict`, `missing_input`, or `out_of_scope`. A `ready` response must include objective and user-visible value, single assigned responsibility, REQ/contract/TASK/optional BUG clauses covered, planned files or review surfaces, allowed and forbidden actions, dependencies and integration boundaries, expected outputs and evidence, Closing Contract or review conclusion criteria, selected Skills and how they apply, assumptions, risks and unresolved questions, and document fingerprints actually read. Phase-two activation references approved read-back message ID and hash, approval evidence ID, current runtime revision, activation ID and expiration condition, allowed tools, allowed read and write paths, allowed command classes, expected output paths, checkpoints and stop conditions, and task/BUG and document fingerprints. Activation scope cannot exceed the prospective scope declared in phase one or the Agent Definition maximum. Stop and escalation conditions include: required document or Skill missing; fingerprint differs from envelope; specifications conflict; requested work exceeds responsibility or paths; new requirement or contract decision needed; irreversible or human-controlled action required; a human-only Hook block; tests reveal a blocking issue outside assigned scope. Prompt minimality: prompt content must not include copied specification sections, generic engineering checklists already in Skills, role doctrine already in the Agent Definition, Loop procedure already in Methodology Skills, guessed missing context, or hidden instructions that contradict the envelope.
