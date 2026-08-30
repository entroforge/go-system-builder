---
name: team-planning
description: Use when creating, reusing, replacing, or recovering specialized Builder, Document Verifier, Delivery Verifier, QA, or E2E Tester workgroups
category: methodology
version: 1.3.0
---
# Team Planning

## Authority
The manifest proposes assignments; Hooks and runtime enforce activation. Runtime authority lives in `.claude/loop-state.json`; stage contracts live in `docs/agent-protocol.md`; the team-planning method is inlined below.

## Entry Conditions
- Specification scope is known (contracts, TASKs, modules, risk tags are drafted).
- The document chain is fingerprinted and ready for assignment.
- Current teammate state (if reusing or recovering a team) is readable from runtime.

## Required Inputs
| Input | Path / field | Why |
|:---|:---|:---|
| Responsibility catalog | this Skill's procedure and inlined method summary | mandatory vs risk-triggered duties |
| Contracts + TASKs | `docs/contracts/**`, `docs/tasks/**` | scope partitioning inputs |
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
Stop immediately and surface to the human if any of:
- A mandatory responsibility cannot be covered (uncovered duty).
- The dependency graph has a cycle.
- Existing teammate state is stale (fingerprints drifted) and cannot be recovered.
- Two assignments have overlapping write paths (write conflict).
- A required role or Skill is unavailable.

## Non-Goals
- Do not use fixed team sizes — compute from scope and risk.
- Do not merge different responsibilities into one assignment.
- Do not weaken individual activation scope to fit a smaller team.

## Inlined Methodology

Claude Code currently permits one Agent Team per session. Therefore `platform_team_id` identifies the one Claude Code Team; `workgroup_id` identifies a logical phase grouping inside that Team. Builder, Document Verifier, Delivery Verifier, QA and E2E Browser workgroups execute in their applicable Loop phases; workgroups are not separate nested Claude Code Teams. Document Verification responsibilities: `DV-SPEC-CONSISTENCY` (REQ/design/UI/FE/BE/SYNC completeness, logic and cross-document consistency) and `DV-TASK-EXECUTABILITY` (task coverage, links, scope, dependencies, Closing Contracts, and evidence feasibility). Delivery Verification responsibilities: `VER-REQ-GAP`, `VER-SPEC-GAP`, `VER-MODULE-COMPLETE`, `VER-INTEGRATION`, `VER-REGRESSION`. QA responsibilities: `QA-MODULE-CODE`, `QA-REUSE-ABSTRACTION`, `QA-UNIT-TEST`, `QA-INTEGRATION-TEST`, `QA-ARCHITECTURE`, `QA-SECURITY`, `QA-PERFORMANCE`, `QA-RELIABILITY`, `QA-MIGRATION`. E2E Browser responsibilities: `E2E-USER-FLOW` (real-browser execution of assigned locked `PATH-*` entries from `USER-FLOW-*`) and `E2E-CONSOLE-NETWORK` (console errors, network failures, request/response mismatch, screenshots/traces). Risk-tag derivation covers frontend (`ui`, `frontend`), api (`api`, `cross-component`), database (`database`, `migration`), security (`security`), reliability (`reliability`, possibly `concurrency`), performance (`performance`), architecture (`architecture`), regression (`regression`). Scope partitioning rules: distinct write ownership or repository boundary; independent module or bounded-context responsibility; incompatible Best Practices or tool needs; high merge-conflict paths; independently testable Closing Contracts. One assignment may not exceed 30 files or 3 material modules. Conflict-graph `must_separate` edges when responsibilities require different conclusion enums, one responsibility checks the other's authored or repaired output, original-finder independence must be preserved, security/release/permission enforcement needs independent evidence, roles have different write permissions, scopes require incompatible Skills or tools, or combining them would exceed workload limits. Teammate reuse requires same role family and responsibility ID, unchanged scope, matching fingerprints, current prior read-back/activation, no unresolved report/BUG/blocked-task conflicts, valid independence, and non-stale teammate state. Workgroup gates: Document Verification must cover both DV duties; Builder Gate requires one write owner, Closing Contract, allowed paths and dependency position per locked TASK or accepted repair BUG; Delivery Verification must cover `VER-REQ-GAP`, `VER-SPEC-GAP`, all material module partitions and every triggered integration/regression responsibility; QA Gate must cover the four baseline QA responsibilities and every triggered architecture, security, performance, reliability and migration responsibility; E2E Browser Gate must cover all required locked `PATH-*` entries plus console/network evidence.
