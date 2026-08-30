---
name: bug-resolution
description: Use when S7 produces an investigation-ready Finding batch, or when an approved RepairContract is ready for S9 execution
category: methodology
version: 2.0.0
---
# S8 Investigation and Bug Projection

## Authority

Runtime legality is defined by `docs/loop-definition.json`; the S8/S9 stage contract is defined by `docs/agent-protocol.md`. This Skill explains how an Agent supplies the missing Case/Contract facts and does not override either authority.

S8 has one investigation chain:

```text
sealed S7 Finding / ObservationBatch
  -> reversible InvestigationCase
  -> HypothesisResults and CausalModel
  -> approved RepairContract
  -> S9 repair
```

- S7 `Finding` and its encounter/raw evidence are immutable facts. S8 consumes them; it does not rewrite them.
- `InvestigationCase` is the S8 authority. It owns grouping, split/merge revisions, hypotheses, evidence, causal reasoning, blast radius, detection gap, and route.
- `RepairContract` is the only repair authority. S9 executes its approved revision and hash.
- A canonical BUG is a human-readable compatibility projection created **after** `RepairContract` approval. It is never an S8 intake, grouping, approval, or repair prerequisite.

## Entry Conditions

- A sealed S7 `ObservationBatch` or an exact Finding set is available.
- Each Finding has an immutable identity/hash, baseline, expected/observed facts, and the recorded boundary or code-inspection scope needed for investigation.
- The Loop is in S8/`bug_resolution`.
- Existing Cases and approved Contracts have been checked for overlap and duplicates.

An incomplete but safe S7 batch enters S8 with explicit `capture_gaps`; do not silently turn a gap into a reproduction task. If the Finding set is not sealed or cannot be identified exactly, stop intake and repair the S7 handoff first.

## Required Inputs

| Input | Use |
|:---|:---|
| Finding exact set and hashes | Bind the Case without changing S7 facts |
| Encounter/raw evidence | Read `last_good -> wall_action -> first_bad`, timeline, state delta, side effects, and terminal state before considering reproduction |
| Baseline and traceability refs | Bound the affected implementation/specification surface |
| Existing Cases/Contracts | Detect duplicate, overlap, split, or follow-on work |
| S7 capture gaps and detection claims | Carry known uncertainty into the causal and repair contracts |
| Finder/checkpoint availability | Continue with the original investigator or an authorized replacement |

## Evidence Policy

1. **Do not reproduce the S7 symptom by default.** First consume the frozen encounter, raw evidence, state deltas, and code-inspection trail.
2. Request a `FindingSupplement` only when a discriminator is missing and a new observation can distinguish competing hypotheses. The supplement is append-only and must reference the Finding and the discriminator it answers.
3. If the discriminator cannot be obtained safely, record the gap and route `investigate_more` or `human_req_change`; do not manufacture certainty by repeating the same journey.
4. S8 owns root-cause investigation and detection-gap reasoning. S7 owns claim completeness and observation capture. S8 may use S7 gaps as facts, but does not rerun the S7 review protocol merely to make the intake look complete.

## Procedure

1. **Ingest and bind.** Create or resume one `InvestigationCase` from the exact Finding set, ObservationBatch hash, and baseline. Reject changed or ambiguous input. Do not create a BUG at this point.
2. **Group reversibly.** Group by a shared candidate mechanism and compatible repair boundary, never merely by file, symptom wording, or agent ownership. Record the grouping rationale. Split or merge by Case revision while preserving prior hashes and the exact Finding coverage.
3. **Form competing hypotheses.** Each hypothesis must state the invariant or authority it may violate, the evidence that would support/refute it, and a discriminator. Do not write a root cause as an untested conclusion.
4. **Dispatch independent questions.** Assign sub agents by evidence question, not one agent per Finding. Allocate a unique `assignment-*` id, register it on the Hypothesis, then run `runtime investigation dispatch` so the id is bound to the Investigator workgroup/Task/Agent lifecycle. An assignment includes Finding IDs, boundary refs, hypothesis, discriminator, read/command scope, expected evidence, stop condition, and forbidden product changes. The worker reports generic `PLAN_REPORT` and then continues; a second approval round is not required. Workers submit evidence and `HypothesisResult` only, using the same bound Assignment id.
5. **Build the causal model.** The supported model must connect `trigger -> violated invariant -> faulty mechanism -> propagation -> symptoms`, and must state blast radius and detection gap. Every source Finding is explained, explicitly split, linked as duplicate, or routed as unresolved; unexplained Findings cannot be hidden in prose.
6. **Choose exactly one route per Case.** Use only:
   - `s9_repair` — implementation, contract, test, tooling, configuration, database, dependency, or environment repair is required;
   - `s2_spec_rework` — the specification/design is internally inconsistent or incomplete;
   - `human_req_change` — the requested behavior or requirement must change;
   - `s7_no_change` — expected behavior, test-only false alarm, or transient condition with no product/spec change;
   - `investigate_more` — evidence or discrimination is insufficient;
   - `duplicate` — follow an existing canonical Case/Contract.
7. **Create and approve the RepairContract.** For `s9_repair`, bind the exact Finding set, violated invariant, causal model, repair scope, forbidden scope, compatibility/migration/rollback expectations, symptom assertions, root-invariant assertion, detection-gap assertion, and stop/escalation conditions. Main approves; Architect review is conditional on a cross-module, source-of-truth, or contract-boundary change.
   First register a `human_decision` evidence artifact scoped to `s8_contract_approval:<runtime_id>@<current-runtime-revision>` and record the exact draft SHA-256 in that decision artifact. The approval handoff is then the CAS command `runtime investigation contract approve --case-id <case> --file <draft> --approved-by <actor> --approval-hash <sha256> --approval-evidence-id <evidence-id>`. It must succeed before S9 work begins; it writes the approved Contract hash, creates the next immutable Case revision, and advances to `bug_resolution.repair_readback`.
8. **Project and hand off.** Only after approval, generate the canonical BUG compatibility projection and the S9 work package idempotently from the Case and RepairContract. S9 reads the approved Contract hash and does not rediscover the root cause. A projection failure must not roll back the approved investigation; retry the projection from the same Case/Contract revision.
   The executable S9 path is: `runtime repair session open` → `runtime repair plan compile` → **the generic L4 `agent-message` PLAN_REPORT checkpoint, when the worker is platform-dispatched** → **`runtime repair plan-report submit` (one domain report per repair Assignment, must include a failing red pre-fix check; product writes stay denied)** → **`runtime repair execution begin` (the only verb that releases implementation writes)** → `runtime repair result submit` (changed_artifacts must exactly match the session diff; status vocabulary added/deleted/modified) → `runtime repair changeset compute` → `runtime repair impact create/commit` (invalidates affected historical PASS evidence in the same transaction) → independent `runtime repair targeted create/commit` (verifier must differ from the repair Builder) → `runtime repair handoff create/commit` (fires TR-012, bumps the round, writes the S7 seed). Copyable `--file` shapes for the domain artifacts are in [`docs/examples/s7-s9/`](../../docs/examples/s7-s9/). The generic PLAN_REPORT is an Agent-lifecycle checkpoint; it does not replace the S9 domain `repair-plan-report`, and the domain file must not be passed as `SendMessage(plan_ref=...)`. After every command, read `runtime repair status`; its `next_action` is the Controller checkpoint. A non-pass RepairResult or scope deviation routes back to S8 and cannot be advanced by hand-editing the pointer. For a non-`blocked` targeted failure, run `runtime investigation route --case-id <case> --route investigate_more --reason "targeted reverification requires causal reassessment" --reassessment-evidence <targeted-path>`; this creates a new revision of the same Case, preserves the original Finding set and failure hash, clears the superseded Contract pointer, and retires the old `review.repair` pointer. A `blocked` targeted result first resolves the blocker, runs `runtime repair targeted resume --actor <actor> --reason <resolution>`, and submits a new independent reverification; it is not causal evidence yet.

## Blocking and Recovery

Every blocked result must expose all four fields:

```text
BLOCKED: <short reason>
Missing: <field, evidence, or authorization>
Next: <one concrete command or action>
Verify: <status/readback command or artifact>
```

Use these recovery rules:

- **Unsealed or changed Finding set:** stop before Case creation; return to S7 to seal or re-export the exact batch, then verify the batch hash.
- **Missing discriminator:** append a `FindingSupplement` for the named discriminator; if it is unsafe or unavailable, route `investigate_more` instead of repeating the symptom.
- **Original finder unavailable:** resume from the saved Case/checkpoint with an authorized replacement investigator; do not discard the Case or require a fresh reproduction.
- **Case revision conflict:** reload the latest Case, preserve its source Finding exact set, and submit a new revision; never overwrite a newer revision.
- **Unexplained Finding or unsupported causal edge:** keep the Case open, add a targeted hypothesis assignment, and verify that every source ID is covered before Contract approval.
- **RepairContract incomplete:** revise the Case/Contract and run the completeness check again; do not create a BUG or dispatch S9.
- **Route requires specification or requirement change:** set the corresponding route, preserve evidence and causal work, and hand off to S2 or the human decision point.
- **S9 projection or dispatch failure after approval:** retry the idempotent projection using the approved Contract hash; do not create a second Case or alter the approved Contract.
- **S9 targeted failure:** read the failed TargetedReverification from `runtime repair status`. For `fail_same_cause`, `fail_new_cause`, `scope_changed`, or `stale`, use the exact `investigation route ... --reassessment-evidence <targeted-path>` action shown by `next_action`; it is the same Case's causal reassessment revision, not a new BUG or an S9 retry loop. For `blocked`, resolve the environment/authority blocker, run `runtime repair targeted resume --actor <actor> --reason <resolution>`, and submit a new independent targeted result first; this path does not create causal evidence.

## Outputs

- A versioned `InvestigationCase` with exact Finding coverage, grouping history, hypotheses, evidence, causal model, blast radius, detection gap, and route.
- For `s9_repair`, an approved `RepairContract` with revision/hash and S9 handoff.
- For other routes, the route rationale and the required next owner/action.
- A canonical BUG only as a post-approval compatibility projection.

## Stop Conditions

Stop and surface the named route when:

- the evidence cannot support a causal model;
- a requirement or specification decision is needed;
- a Finding remains unexplained;
- the proposed repair only hides a symptom or changes the source contract without authorization;
- a stale revision, integrity failure, or missing authority cannot be recovered through the rules above.

## Exit Conditions

- Every source Finding is covered by an InvestigationCase disposition without rewriting the immutable observation.
- A Case routed to `s9_repair` has an approved RepairContract with a causal model, bounded blast radius, detection-gap assertion, symptom assertions, and exact source Finding coverage.
- A Case routed elsewhere records one route, its reason, the next owner, and the evidence needed to resume.
- S9 receives the approved RepairContract hash; it does not receive an open-ended BUG report or rediscover the root cause.

## Non-Goals

- Do not create a rich BUG before the Case and RepairContract exist.
- Do not use a BUG as the source of truth for grouping, root cause, approval, or repair scope.
- Do not default to E2E reproduction when S7 encounter/raw evidence is available.
- Do not let S8 repair product code or let S9 reinterpret the causal model.
- Do not count targeted re-verification as a new clean S7 round.
