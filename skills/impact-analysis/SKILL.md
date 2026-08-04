---
name: impact-analysis
description: Use when specifications, implementation, repairs, or out-of-band changes may invalidate downstream evidence
category: methodology
version: 1.1.0
---
# Impact Analysis

## Authority
This Skill identifies affected artifacts and marks evidence; it does not approve transitions. Runtime authority lives in `docs/loop-definition.json` and `.claude/loop-state.json`; the method summary is inlined below.

## Entry Conditions
- A concrete changed artifact or fingerprint is known: specification rework, Builder completion, BUG repair, or an out-of-band change.
- The runtime is readable and its baseline generation and review round are known.

## Required Inputs
| Input | Path / field | Why |
|:---|:---|:---|
| Changed paths | committed diff or explicit `--changed` list | traversal seed |
| Traceability graph | REQ → design → contracts → TASK → modules → tests → evidence | both-direction closure |
| Current evidence | runtime `evidence[]` with valid/invalid/superseded | what may need invalidation |
| Change-type matrix | inlined method summary below | minimum invalidation responsibilities |
| Fingerprints | runtime artifact fingerprints vs working tree | detect drift |
| Review round + baseline | runtime `lifecycle` | clean-round immutability boundary |

## Procedure
1. Enumerate every changed artifact path (specification, contract, TASK, source, test, config, database, dependency, documentation-only).
2. Classify each change by type: requirement, design, contract, task, implementation, test, configuration, database, dependency, documentation_only.
3. Invoke `loop-harness impact analyze --root . --changed <path>...` to compute impacted evidence from the runtime traceability graph.
4. Apply the Always-Invalidates responsibilities for each change type (e.g. contract change always invalidates delivery/integration evidence scoped to that contract).
5. Expand with Conditional Expansion using scope and dependency closure (UI, integration, security, migration, regression as applicable).
6. Traverse both directions from each changed artifact — forward to downstream evidence, reverse to upstream specifications. Store source/target IDs, relation type, rule ID, decision, rationale per edge.
7. Decide new status per impacted evidence: `invalid` (changed or integrity conflict), `superseded` (replaced, retained for audit), `retain` (requires positive proof — absence of a known path is not proof of no impact).
8. Escalate conservatively when traceability is missing, behavior crosses trust boundaries, provenance is unknown, fingerprints drifted, or security/migration impact is uncertain. Narrowest defensible level: assignment → workgroup → review round → baseline (pause for human).
9. Mark any clean-round record `invalid` if a relevant change occurred after its frozen fingerprint set — clean rounds are immutable historical evidence.
10. Emit the change-impact record and re-verification plan: targeted responsibilities to re-run, and whether a full new round is required.

## Outputs
- Change-impact record (changed artifacts, closure edges, invalidation decisions, rationale).
- Evidence invalidation set: evidence ID → new status with rule ID.
- Re-verification plan distinguishing targeted rechecks from a full review round.

## Exit Conditions
- Every changed artifact has bounded downstream impact recorded with a defensible decision per edge.

## Stop Conditions
Stop immediately and surface to the human if any of:
- A changed artifact has no traceability links.
- Impact cannot be bounded (escalate to baseline and pause).
- Working-tree fingerprints disagree with the runtime snapshot.
- A `documentation_only` change cannot prove no normative clause or executable instruction changed.

## Non-Goals
- Do not silently retain PASS evidence — uncertainty expands impact.
- Do not change evidence from `invalid` back to `valid`; re-verification creates new evidence with a new ID.
- Do not decide release readiness or transition authority.

## Inlined Methodology

Impact analysis enumerates every changed artifact path and classifies by type (requirement, design, contract, task, implementation, test, configuration, database, dependency, documentation_only). It traverses traceability links in both directions from every changed artifact and stores source/target IDs, relation type, rule ID, decision (`invalidate`, `supersede`, `retain`, `reverify`), and rationale. The first nine change types invalidate a minimum responsibility set (responsibility invalidation matrix); documentation_only is valid only when impact analysis proves no normative clause, executable instruction, fingerprinted activation input, or evidence meaning changed. Conservative escalation expands impact when traceability links are absent, trust boundaries are crossed, browser/runtime evidence is affected, or test/evidence provenance is unknown; narrowest defensible level is assignment -> workgroup -> review round -> baseline (pause for human). Evidence states are `valid`, `invalid`, `superseded`; invalid evidence is never revived. Clean-round records are immutable historical evidence: a relevant change after the frozen fingerprint set marks them `invalid` and requires a new complete Delivery + QA + E2E round.
