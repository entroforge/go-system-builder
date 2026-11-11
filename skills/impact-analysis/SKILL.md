---
name: impact-analysis
description: Use when specifications, implementation, repairs, or out-of-band changes may invalidate downstream evidence
category: methodology
version: 1.1.0
---
# Impact Analysis

## S9 original Impact

For S9, follow the original ChangeImpact questionnaire and the S9 branch of
[bug-resolution](../bug-resolution/SKILL.md). Main considers actual changes,
necessary checks and impacts; the framework receives the original Impact without
requiring this generic command, a commit, fingerprints or an extra proof. Empty
change types and dispositions are receivable even if they resemble a default
questionnaire. Reception does not prove Main completed integration or checks;
Main may naturally update the same original Impact while work continues, and
in-flight Builder responsibilities remain available. Independent Targeted and a
fresh full S7 remain real responsibilities.

The generic method below applies to its own applicable change workflows; do not
turn its traceability, evidence inventory or pause steps into S9 reception gates.

## Authority
This Skill identifies affected artifacts and marks evidence; it does not approve transitions. Runtime authority lives in `docs/control/loop-definition.json` and `.claude/loop-state.json`; the method summary is inlined below.

## Entry Conditions
- An actual change or affected responsibility is known: specification rework,
  Builder completion, BUG repair, or an out-of-band change.
- Use the existing Runtime and original responsibility when available; do not
  require a review-round counter or fingerprint.

## Required Inputs
| Input | Path / field | Why |
|:---|:---|:---|
| Changed paths | committed diff or explicit `--changed` list | traversal seed |
| Traceability graph | REQ → design → contracts → TASK → modules → tests → evidence | both-direction closure |
| Current evidence | runtime `evidence[]` with valid/invalid/superseded | what may need invalidation |
| Change-type matrix | inlined method summary below | minimum invalidation responsibilities |
| Original responsibility | existing Plan, assignment and result references | preserve actual ownership and history |

## Procedure
1. Enumerate every changed artifact path (specification, contract, TASK, source, test, config, database, dependency, documentation-only).
2. Classify each change by type: requirement, design, contract, task, implementation, test, configuration, database, dependency, documentation_only.
3. Use the applicable existing impact entry when useful to identify affected evidence and responsibilities.
4. Assess whether each actual change affects prior conclusions; preserve unaffected facts.
5. Expand with Conditional Expansion using scope and dependency closure (UI, integration, security, migration, regression as applicable). Follow the actual providers/consumers, shared models, API/event/auth/configuration and journey/data paths in existing approved materials. REQ prototype links are discovery hints. Removing an edge or changing ownership still requires inspecting previously known consumers; do not erase them from impact by looking only at the new graph. No mandatory module graph or unconditional whole-project fallback is required.
6. Traverse both directions from each changed artifact — forward to downstream evidence, reverse to upstream specifications. Store source/target IDs, relation type, rule ID, decision, rationale per edge.
7. Assess the effect on the original evidence and explain unknowns without inventing a conclusion.
8. Where a real question cannot be resolved from available materials, hand that
   specific unknown to Main; do not invent an additional proof or human approval.
9. Preserve historical conclusions as facts about the work actually performed;
   decide necessary further verification from the actual change.
10. Express the impact and necessary re-verification in the existing workflow.

## Outputs
- Change-impact record (changed artifacts, closure edges, invalidation decisions, rationale).
- Evidence invalidation set: evidence ID → new status with rule ID.
- Re-verification plan distinguishing targeted rechecks from a full review round.

## Exit Conditions
- Every changed artifact has bounded downstream impact recorded with a defensible decision per edge.

## Stop Conditions
Stop immediately and surface to the human if any of:
- Missing traceability: Main restores the actual relationship sources or arranges bounded investigation; do not silently exclude consumers. This alone does not create a new human approval requirement.
- Impact cannot be bounded (escalate to baseline and pause).
- A `documentation_only` change cannot prove no normative clause or executable instruction changed.

## Non-Goals
- Do not silently retain PASS evidence — uncertainty expands impact.
- Do not change evidence from `invalid` back to `valid`; re-verification creates new evidence with a new ID.
- Do not decide release readiness or transition authority.

## Inlined Methodology

Follow actual changes and relevant links to identify affected responsibilities,
evidence and necessary checks. Preserve prior conclusions as historical facts,
including when later work changes their applicability. Main decides the necessary
continuation using the original Impact and actual information; do not require a
fingerprint inventory, a new evidence ledger, a round counter or proof that every
unchanged file remains byte-identical.
