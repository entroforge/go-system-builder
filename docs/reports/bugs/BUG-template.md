# Canonical BUG Compatibility Projection: BUG-{id}

> This document is generated **after** an `InvestigationCase` disposition; for
> `s9_repair`, the Case must also have an approved `RepairContract`. It is a
> human-readable compatibility projection for reporting, task tracking, and
> legacy integrations. It is not an S8 intake, grouping, root-cause, approval,
> or repair authority.

## Projection Metadata

| Field | Value |
|:---|:---|
| BUG ID | `BUG-{id}` |
| Projection status | reported / assigned / fixing / fixed / verifying / closed / rejected / duplicate |
| Route | `s9_repair` / `s2_spec_rework` / `human_req_change` / `s7_no_change` / `investigate_more` / `duplicate` |
| InvestigationCase | `{case-id}@{revision}` |
| Case hash | `{sha256}` |
| RepairContract | `{contract-id}@{revision}` or `not applicable` |
| Contract hash | `{sha256}` or `not applicable` |
| Source Findings | `{exact Finding ID set}` |
| ObservationBatch | `{batch-id}@{revision}` |
| Runtime ref | `{runtime-id}@{revision}` |
| Severity | P0 / P1 / P2 / P3 |
| Generated after approval | `{approval-ref}` |

If any authority reference is missing, do not fill the gap in this file. Return
to the Case or Contract and use:

```text
BLOCKED: BUG projection is missing an approved authority
Missing: <Case/Contract reference or hash>
Next: read or revise the referenced InvestigationCase/RepairContract
Verify: compare the projection metadata with the approved Case/Contract hash
```

## Authority and Traceability

The authoritative read order is:

```text
InvestigationCase -> approved RepairContract -> this BUG projection -> S9 task
```

| Kind | ID | Path | Revision | SHA-256 | Relevant clauses |
|:---|:---|:---|:---|:---|:---|
| Finding set | `{finding-ids}` | `{evidence paths}` | `{revision}` | `{sha256}` | observed facts |
| InvestigationCase | `{case-id}` | `{case path}` | `{revision}` | `{sha256}` | grouping/causal model/route |
| RepairContract | `{contract-id}` | `{contract path}` | `{revision}` | `{sha256}` | approved repair/verification |
| TASK | `TASK-{id}` | `docs/tasks/...` | `{version}` | `{sha256}` | derived execution scope |
| REQ/design/rule | `{id}` | `{path}` | `{version}` | `{sha256}` | affected authority |

Do not edit the Finding, Case, or RepairContract from this document. If the
projection disagrees with an authority artifact, the authority artifact wins;
regenerate this projection from its approved revision.

## 1. Observed Facts

This section is a readable projection of the source Findings and S7 encounter
evidence. It must not introduce new observations.

| Field | Value |
|:---|:---|
| Expected | `{REQ/contract/TASK assertion}` |
| Observed | `{frozen observed fact}` |
| User/data/system impact | `{impact}` |
| Operation path | `{short journey from entry to wall}` |
| Last known good | `{state/evidence ref}` |
| Wall action | `{operation that crossed the boundary}` |
| First bad result | `{visible/request/response/persisted contradiction}` |
| Before-fix evidence | `{evidence refs}` |

The S8 source is the Finding encounter/raw evidence. Reproduction steps are
included only when they were explicitly required by a discriminator and are
linked to that supplement.

## 2. Root Cause and Causal Model

Copy this section from the approved Case/RepairContract. Do not turn a
hypothesis into a root cause by editing this projection.

| Causal element | Approved value | Evidence refs |
|:---|:---|:---|
| Violated invariant/authority | `{value}` | `{refs}` |
| Faulty mechanism | `{value}` | `{refs}` |
| Propagation | `{value}` | `{refs}` |
| Primary root cause | `{value}` | `{refs}` |
| Contributing factors | `{value}` | `{refs}` |
| Blast radius | `{checked surfaces / exclusions}` | `{refs}` |
| Detection gap | `{missing oracle/test/guard}` | `{refs}` |

The root cause must explain every source Finding in the Case. Unexplained IDs,
split Cases, and duplicate links belong in the Case revision, not in an
invented BUG paragraph.

## 3. Approved Repair Contract Projection

### 3.1 Repair scope

`{files/modules/interfaces/data structures the Builder may change}`

### 3.2 Forbidden scope

`{files/modules/contracts/requirements the Builder must not change}`

### 3.3 Compatibility and rollout

`{migration, compatibility, rollout, rollback, and data-safety expectations}`

### 3.4 Required verification assertions

```text
assert {source Finding symptom} == absent or expected behavior restored
assert {violated invariant} == satisfied
assert {detection-gap assertion} == covered
assert {regression surface} == passing with fresh evidence
```

### 3.5 S9 handoff

| Field | Reference |
|:---|:---|
| Repair assignment | `{assignment-id}` |
| Contract approval | `{approval-ref}` |
| Builder activation | `{activation-ref}` |
| Repair fingerprint | `{commit/sha256}` |
| Impact analysis | `{impact-ref}` |
| Invalidated evidence | `{evidence-ids}` |

S9 executes the approved Contract revision and hash. It must not broaden the
scope, replace the root cause, or weaken the assertions through this BUG file.

## 4. Verification and Closure

| Verification | Owner | Result | Evidence |
|:---|:---|:---|:---|
| Targeted source-Finding verification | `{assignment-id}` | pass / fail / blocked | `{ref}` |
| Root-invariant verification | `{assignment-id}` | pass / fail / blocked | `{ref}` |
| Detection-gap verification | `{assignment-id}` | pass / fail / blocked | `{ref}` |
| Required complete S7 round | round `{n}` | pass / pending | `{clean-round-ref}` |

Targeted verification can close the repair projection only when every approved
assertion passes. It never creates a clean S7 round; a complete Delivery + QA +
E2E round remains a separate gate.

If verification is blocked, preserve the Contract and report:

```text
BLOCKED: <verification step>
Missing: <evidence, environment, or authorization>
Next: <one recovery action or route>
Verify: <fresh evidence ref or status readback>
```

## 5. Route and Deduplication History

| Field | Value |
|:---|:---|
| Canonical Case | `{case-id}@{revision}` |
| Duplicate of | `BUG-{id}` / `none` |
| Split from | `{case-id}` / `none` |
| Merged from | `{case-ids}` / `none` |
| Route rationale | `{approved reason}` |

| Date | Event | Actor | Case/Contract revision | Evidence |
|:---|:---|:---|:---|:---|
| | | | | |
