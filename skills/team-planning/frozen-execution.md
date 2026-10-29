# Frozen execution compatibility — team-planning

Read only for an existing pre-migration registered execution or explicitly frozen teammate protocol. This document does not define creation, identity, activation, resume or normal delivery of a new Claude Code native Agent. Current work follows [SKILL.md](SKILL.md) and its stage professional contract. Preserve actual human approval, scope and historical receipts; do not reinterpret old execution facts as new permission.

## Authority

For new S9 work, professional division selects repair responsibilities and an independent verifier; it does not create a Claude Team or launch a process. Pass the exact prepared native brief to Claude Code's Agent tool. Builder worktree isolation and resume belong to Claude Code. Start/Stop connect professional inputs/results; Main retains integration, checks and the fresh S7 handoff. Existing workspace executions remain frozen recovery paths. The Team/readback/activation method below is not a prerequisite of native S6 or S9.
The manifest proposes assignments; the original runtime records activation. Hooks observe progress and provide the exact questionnaire path and missing duties. Runtime authority lives in `.claude/loop-state.json`; stage contracts live in `docs/control/agent-protocol.md`; the team-planning method is inlined below.

Responsibility progress uses the five states in
[`docs/rules/state-machine.md`](../../docs/rules/state-machine.md#21-loop-responsibility-status).
Here `ready` means eligible for original dispatch, not that the responsibility
has started or finished. Team lifecycle and platform session values remain
their exact current Runtime/host fields; never ask an Agent to write them.

## Entry Conditions
- Specification scope is known (contracts, TASKs, modules, risk tags are drafted).
- The document chain is fingerprinted and ready for assignment.
- Current teammate state (if reusing or recovering a team) is readable from runtime.

## Required Inputs
| Input | Path / field | Why |
|:---|:---|:---|
| Responsibility catalog | this Skill's procedure and inlined method summary | mandatory vs risk-triggered duties |
| Contracts + TASKs | `docs/dev/contracts/**`, `docs/dev/tasks/**` | scope partitioning inputs |
| Risk tags | TASK risk fields | route Best-practice Skills per Agent |
| Agent Definitions | `.claude/agents/<role>.md` | available roles and max permissions |
| Existing manifests | runtime `entities.teams[]` | reuse freshness and continuity |
| Separation edges | this Skill's procedure and inlined method summary | independence constraints |

## Procedure
1. Enumerate mandatory responsibilities for the team type: Delivery Verifier requires REQ-source coverage, contract-implementation, module-function, integration, regression; QA requires code-quality, architecture, testing, security, performance, reliability; E2E Tester requires current module CASE/PATH coverage and console/network observation.
2. Add risk-triggered responsibilities from TASK risk tags (security-sensitive → security-review, migration → database-change, UI → frontend-engineering).
3. Partition the scope so each assignment covers exactly one auditable responsibility. Record `N/A` with justification for any responsibility that does not apply — silence is not N/A.
4. Select the Builder role by write ownership: `frontend-builder` for frontend product code and owned component tests; `backend-builder` for backend product code, migrations, and owned unit/integration tests; `test-builder` for test-only files, fixtures, helpers, and test infrastructure. An E2E responsibility always uses `e2e-tester`.
5. Build separation edges: no single Agent may verify its own output; the original finder must own targeted re-verification; Builder and Verifier for the same scope must be different Agents.
6. Check reuse freshness: if an existing Agent's fingerprints still match and its scope covers the new responsibility, reuse it; otherwise create a new assignment.
7. Compute Agent count from: modules × contracts × scenario coverage risk × migration × concurrency × security × BUG history. Do not use a fixed team size.
8. Produce one assignment per responsibility with: agent ID, role, responsibility ID, allowed read paths, allowed write paths, task-specific Skills, message template, stop conditions. Harness merges these with the role's default Skill profile before launch.
9. Validate the manifest via `loop-harness validate --all --root .` (schema + team-manifest schema).
10. Emit launch packages: one assignment brief per assignment, ready for `agent-dispatch` (plan_checkpoint).

## Outputs
- A schema-valid team manifest with assignments, count rationale, and coverage result.
- One assignment brief (launch package) per assignment.

## Exit Conditions
- Coverage is complete (all mandatory responsibilities assigned or justified N/A).
- Independence constraints hold (no separation-edge violation).
- Every assignment has the required Skills, paths, and dependencies.
- The manifest validates against `team-manifest.schema.json`.

## Stop Conditions
Stop dispatch of the affected assignment when coverage, dependencies, write ownership or role availability is invalid. This is a Worker/dispatch barrier, not automatically a Human Gateway. The Driver must first assign the missing duty, remove an erroneous dependency, serialize or repartition shared writes, or recover/reassign a stale teammate through the supported lifecycle. Preserve independent review and active assignment authority; do not silently widen scope or substitute roles. Continue unrelated unblocked work. Surface a human decision only when the remaining issue requires changed business semantics, unavailable external permission, a required explicit authorization or unrecoverable Runtime integrity; include the attempted autonomous recovery.

## Non-Goals
- Do not use fixed team sizes — compute from scope and risk.
- Do not merge different responsibilities into one assignment.
- Do not weaken individual activation scope to fit a smaller team.

## Inlined Methodology

Claude Code currently permits one Agent Team per session. Therefore `platform_team_id` identifies the one Claude Code Team; `workgroup_id` identifies a logical phase grouping inside that Team. Builder, Document Verifier, Delivery Verifier, QA and E2E Browser workgroups execute in their applicable Loop phases; workgroups are not separate nested Claude Code Teams. Document Verification responsibilities: `DV-SPEC-CONSISTENCY` (REQ/design/UI/FE/BE/SYNC completeness, logic and cross-document consistency) and `DV-TASK-EXECUTABILITY` (task coverage, links, scope, dependencies, Closing Contracts, and evidence feasibility). Delivery Verification responsibilities: `VER-REQ-GAP`, `VER-SPEC-GAP`, `VER-MODULE-COMPLETE`, `VER-INTEGRATION`, `VER-REGRESSION`. QA responsibilities: `QA-MODULE-CODE`, `QA-REUSE-ABSTRACTION`, `QA-UNIT-TEST`, `QA-INTEGRATION-TEST`, `QA-ARCHITECTURE`, `QA-SECURITY`, `QA-PERFORMANCE`, `QA-RELIABILITY`, `QA-MIGRATION`. E2E Browser responsibilities: `E2E-USER-FLOW` (real-browser execution of assigned locked `PATH-*` entries from `USER-FLOW-*`) and `E2E-CONSOLE-NETWORK` (console errors, network failures, request/response mismatch, screenshots/traces). Risk-tag derivation covers frontend (`ui`, `frontend`), api (`api`, `cross-component`), database (`database`, `migration`), security (`security`), reliability (`reliability`, possibly `concurrency`), performance (`performance`), architecture (`architecture`), regression (`regression`). Scope partitioning rules: distinct write ownership or repository boundary; independent module or bounded-context responsibility; incompatible Best Practices or tool needs; high merge-conflict paths; independently testable Closing Contracts. One assignment may not exceed 30 files or 3 material modules. Conflict-graph `must_separate` edges when responsibilities require different conclusion enums, one responsibility checks the other's authored or repaired output, original-finder independence must be preserved, security/release/permission enforcement needs independent evidence, roles have different write permissions, scopes require incompatible Skills or tools, or combining them would exceed workload limits. Teammate reuse requires same role family and responsibility ID, unchanged scope, matching fingerprints, current prior read-back/activation, no unresolved report/BUG/blocked-task conflicts, valid independence, and non-stale teammate state. Workgroup gates: Document Verification must cover both DV duties; Builder Gate requires one write owner, Closing Contract, allowed paths and dependency position per locked TASK or accepted repair BUG; Delivery Verification must cover `VER-REQ-GAP`, `VER-SPEC-GAP`, all material module partitions and every triggered integration/regression responsibility; QA Gate must cover the four baseline QA responsibilities and every triggered architecture, security, performance, reliability and migration responsibility; E2E Browser Gate must cover all required locked `PATH-*` entries plus console/network evidence.

### S5 manifest assembly (Document Verifier registration)

Assemble the manifest from Runtime, never by hand-computing machine fields:
- `documents`: every `locked`/`complete` row in Runtime `documents[]` (id/path/version/sha256 copied exactly); plus the S4 batch TASK paths and the dispatch-plan index path.
- `validated_at`: current UTC time in RFC3339Nano, machine-filled at assembly; never a fixed or midnight timestamp.
- `review_round`: entry-specific (`review-e<phase_revision>`); never reuse a historical round label.
- `separation_edges`: exactly one edge between the two DV assignments with reason `independence`; the two `agent_id` values must differ.
- Anchor binding for `runtime register-workgroup --task-id <entity> --task <path>`: the first S4 dispatch-plan TASK; entity id is the canonical `^TASK-[0-9]{3,}(?:-[0-9]{2,})?$` shape mapped from the plan filename by keeping only digit segments (`TASK-REQ-001-01.md` → `TASK-001-01`, `TASK-001-01.md` → itself); `--task` is the actual plan file path. Never pass a REQ id as `--task-id`.
- Do not use S6-only fields (`assignment_record_contract: s6-v1.2`, `closing_contract: s6-closing-v1`, `integration_check_mode`) in a document-verifier manifest; do not copy the example manifest's metadata, hashes, scope or checks.
- Both assignments' `scope` and `read_paths` cover the whole S4 batch; `skill_refs` for `DV-SPEC-CONSISTENCY` are `document-verification, specification-planning, api-contracts`, for `DV-TASK-EXECUTABILITY` are `document-verification, specification-planning, testing-strategy`.

## Dispatch scope and check-target consistency

For ordinary S6 Builder registration, prepare the current eligible TASK with
`loop-harness s6 prepare --root <authority-root> --task <TASK> --role <Builder-role> --agent-id <actual-platform-Agent-ID> --platform-team-id <actual-platform-Team-ID>` only for a frozen legacy route. New native S6 uses the top contract without these IDs.
The command projects the current Runtime, REQ, generation, locked document
fingerprints, TASK write scope, dependencies, and check commands. Main chooses the
TASK, Builder role, and actual platform Agent/Team identities. If the command
reports that the TASK has no concrete Worker-owned Closing Contract predicate,
Main must add that professional decision to the exact draft path it prints;
the draft stays unregistered until the original consumer accepts it. Use the
exact registration command printed by `s6 prepare`:
`loop-harness runtime register-workgroup --manifest <path> --task-id <TASK> --task <locked-task-path>`.
The example manifest is a schema/compatibility reference for explicit
native/legacy setup; do not copy it for the ordinary bound-workspace flow or
manually replace Runtime metadata, hashes, scope, or checks.

For the ordinary S6 bound-workspace route, after registration run
`runtime workspace prepare --root <authority-root> --assignment <id> --agent <owner>`.
Read its returned execution/context and submission paths, then run
`runtime workspace launch` with the same root, Assignment, and owner. The Hook
supplies the active view and exact answer paths. Do not add `team launch`, export
a readback template, or create a worktree before this prepare/launch sequence.
Use registered `team launch` only when the current Runtime explicitly selects
the native/legacy teammate route instead of a bound workspace.

For canonical S6 Builders (`assignment_record_contract: s6-v1.2`), `done_when`
describes Worker-owned outcomes verifiable before Result submission. The
prepared contract places TASK commands in Main `required_checks` and Main
CheckProfiles. The Builder still runs the TASK checks and cites actual outputs
in the Result; Main reruns required checks on the integrated tree. An empty
`worker_checks` list means no separate worker runner is registered; it does not
mean the TASK checks passed.


Before registering a Builder assignment, compare its TASK deliverables with write_paths, shared-type/API/route ownership and dependency order. Assign missing connection work explicitly rather than widening paths silently or instructing a component to duplicate an inaccessible authority. Reuse existing manifest fields and TASK sections; no additional parallel ledger is required.

Bind each required check to an explicit execution tree and commit. A post-merge check must use the integration target supplied by the Integrator, never prefer an old source worktree merely because it exists. Preserve the real exit status and the project's evidence reporter; report zero collection, skipped required cases and missing artifacts as failures, not successful shell wrappers.

Audit checks executed before and after merge so repeated expensive runs have an explicit purpose. Do not skip registered checks by policy text alone: changing an active contract or immutable manifest requires the existing rework/replacement procedure. In a replacement rehearsal verify not just registration, but old-assignment disposition, execution-batch selection and the check set consumed by integration.

## Integration check phase

The canonical manifest prepared for the ordinary S6 bound-workspace route uses
the existing `post_merge` mode with Main CheckProfiles. This keeps Main's
registered Required Checks on the integrated tree while the Builder records
the actual TASK-check outputs in the Result. Other manually configured or
legacy assignments retain their original reviewed check-mode contract. Static
identity, scope, locked-input and conflict inspection remains part of original
integration; on failure preserve the merge and receipts, retry an environment
failure, or use rework for code corrections. Never reset Main automatically
or edit an active manifest's mode. Target projects use
`docs/workspace-execution-recovery.md` for the runner and recovery contract.
