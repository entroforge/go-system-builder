---
name: acceptance-and-handoff
description: Use when a valid clean round enters acceptance, release audit, or human release handoff
category: methodology
version: 1.3.0
---
# Acceptance And Handoff

## Role doctrine

This Skill is not a “move the Loop to S11” checklist. It is the method for a
senior acceptance owner / release auditor to prove that the entire REQ was
developed, reviewed, tested, and prepared for handoff. The shortest path is
the worst path for this project: never treat an existing clean round, a green aggregate,
or a complete-looking Markdown file as proof that the declared scope was
covered.

Unknown is unfinished. An unchecked case cannot become `N/A`; a targeted check
cannot become a full review; and a non-blocking risk still needs an owner,
tracking artifact, impact, and recovery point.

## Authority
Automation stops before squash merge, main-branch publication, or formal release. Runtime authority lives in `docs/loop-definition.json`; the stage contract lives in `docs/agent-protocol.md`; the method summary is inlined below.

## Entry Conditions
- A `clean_round_valid` evidence record exists and is referenced by ID and hash.
- The Loop is in top-level `acceptance` or `release_audit`.
- The locked REQ fingerprint still matches the runtime baseline.
- The clean round was produced by a fresh complete S7 round. S9 never enters
  S10 directly; targeted re-verification only permits the return to S7.

## Required Inputs
| Input | Path / field | Why |
|:---|:---|:---|
| Clean-round record | runtime evidence ID/hash | acceptance and audit anchor |
| Locked baseline | runtime `bound_req` + REQ fingerprint | ACC source of truth |
| ACC inputs | REQ acceptance criteria, Closing Contracts, evidence map | assemble acceptance evidence |
| Release architecture | design docs, deployment config, migration/rollback | release audit scope |
| Operations readiness | runbooks, observability, on-call | operational gate |
| Evidence fingerprints | runtime `evidence[]` | detect stale acceptance evidence |

## Procedure
1. **Freeze the audit universe.** Build a `coverage_inventory` from the locked
   REQ, design, contracts, TASK Closing Contracts, S7 Claims, S9 ChangeImpact,
   changed paths, and risk tags. Include requirements, behavior branches,
   architecture invariants, data/migration, operations, evidence integrity,
   and project-management risks. Do not shrink this list later to obtain PASS.
2. **Plan independent responsibility.** Use as many read-only reviewers as the
   inventory and risk require: requirement acceptance, architecture/code,
   data/migration/operations, evidence integrity, and adversarial review are
   separate concerns when their conclusions or authorship differ. The main
   session integrates results; it does not count duplicate PASS results as
   coverage.
3. **Re-verify the clean-round anchor.** Confirm it is current, valid,
   fingerprinted, and generated after the latest repair. If stale or if the
   product tree moved after the round, stop and require a new complete S7.
4. **Assemble the ACC.** Map every REQ acceptance criterion and every TASK
   Closing Contract to a current PASS/N/A evidence ID. For each row record
   source, expected behavior, oracle, evidence, result, and the relevant
   negative/boundary/recovery branch.
5. **Check delivery and operations.** Confirm delivered scope, deployment
   order, migration/data handling, runtime health, rollback or data remedy,
   monitoring, alerting, on-call ownership, and technical-debt tracking.
6. **Run the counterevidence review before acceptance.** For every inventory
   item ask what would disprove the conclusion. Check rejection, error,
   permission, boundary, duplicate, retry, restart, multi-instance, mock-only,
   and stale-document failure modes where applicable. Record evidence for the
   counterexample check. Any unanswered item remains `UNKNOWN` and blocks.
7. **Check objective completion.** Require requirement/contract/changed-path/
   audit coverage of 100%, zero `UNKNOWN`, zero unsupported PASS, zero
   unowned risks, zero untracked debt, and zero blocking findings. Only then
   run `loop-harness s10 manifest validate --file <manifest.json> --type acceptance`
   and register an envelope carrying `audit_manifest_path` plus its SHA-256;
   only then request TR-015 (`acceptance_completed` → `release_audit`).
8. **Execute the release architecture audit.** Review the eight system areas:
   state machine; transaction/UoW/session; concurrency/idempotency; data
   model/identity/migration; call sites/runtime topology; observability/errors;
   verification evidence; and documentation/release scope. Each area needs a
   conclusion, evidence, counterevidence check, and route for any gap.
9. **Classify findings.** A product or architecture defect returns through
   S8→S9→S7; a correctable acceptance discrepancy returns to a new complete
   S7; an architectural/migration/operations blocker pauses; an REQ or
   irreversible decision goes to the matching human Gateway.
10. **Prepare the S11 package only after reconciliation.** Include clean-round
    ID/hash, coverage metrics, ACC ID/hash, audit ID/hash, baseline generation,
    completed scope, unresolved fact, impact, residual risks and owners,
    recommendation, recovery point, and an explicit `automation stops`.
    Request TR-017. Never request, attempt, or proxy merge, publication,
    deployment, or formal release.

## Machine-checked S10 artifact

The human ACC and release-audit Markdown is rendered from the JSON manifest —
the manifest is the single source, the Markdown is its human-readable
projection. Its shape authority is
`internal/schema/assets/s10-audit-manifest.schema.json`.
Copyable starting shapes are `docs/examples/s10/acceptance-manifest.json` and
`docs/examples/s10/release-audit-manifest.json`; replace their placeholder
facts with the current Runtime evidence.
The normal handoff is:

```text
loop-harness s10 manifest validate --root <root> \
  --file <manifest.json> --type <acceptance|release_audit>
loop-harness s10 manifest render --root <root> \
  --file <manifest.json> [--output <report.md>]
loop-harness runtime evidence add --root <root> \
  --expected-revision <N> --id <id> --kind <acceptance|release_audit> \
  --path <envelope.json> --produced-by <agent> --responsibility <role>
```

The envelope JSON must contain the immutable manifest reference:
`"audit_manifest_path": "<manifest.json>"` and
`"audit_manifest_sha256": "<64 lowercase hex characters>"`. The validator
derives the hard metrics from the frozen rows and rejects unresolved
`UNKNOWN`, unsupported `PASS`, missing owners/evidence, or an incomplete
release-audit eight-area set. The hard inventory categories `requirement`,
`contract`, and `changed_path` must each have an explicit row; use an
evidence-backed `not_applicable` row rather than omitting a category. If the
gate names `s10:<type>_manifest:<id>`,
correct the manifest or envelope and register a new fingerprint; do not edit
the previously registered artifact in place.

For a routed non-passing outcome, validate with the matching mode:
`--outcome review_required` for acceptance (TR-016 back to S7), or
`--outcome blocked` for release audit (TR-018 to `paused`). These modes keep
the unresolved rows and blocker route visible, while still requiring the
complete inventory, counterevidence links, and evidence bindings.

## Outputs
- ACC evidence record (criterion → evidence mapping, baseline, clean-round reference).
- Coverage inventory and counterevidence ledger with objective completion metrics.
- Release architecture audit evidence.
- `awaiting_human_release` handoff record with all referenced evidence IDs and hashes.

## Exit Conditions
- Release audit passes and control is explicitly handed to a human at the
  non-terminal `awaiting_human_release` decision gateway.

## Stop Conditions
Stop immediately and surface to the human if any of:
- The clean-round evidence is missing, invalid, or superseded.
- ACC cannot map a required acceptance criterion to valid evidence.
- The audit universe is incomplete, contains `UNKNOWN`, or has unsupported PASS conclusions.
- A changed path has no requirement/contract/task/evidence disposition.
- Release audit has a blocking finding (architecture drift, missing migration/rollback, operations not ready).
- A product or architecture defect is found and someone proposes fixing it inside S10; route back through S8/S9/S7.
- An irreversible action (merge, publish, deploy, release) is requested without separate human approval.
- The locked REQ fingerprint no longer matches the runtime baseline.

## Non-Goals
- Do not merge, publish, deploy, or formally release — acceptance never performs release. Hook Policy does not block these commands, so the constraint is procedural: reaching S11 raises a `release_ready` Gateway package and the human decides whether release proceeds.
- Do not replace the clean-round evaluator with a narrative assertion. Delegate the machine recomputation to `clean-round-evaluation`, then perform the S10 anchor and counterevidence review described above.
- Do not modify the locked REQ or specifications to make acceptance pass.
- Do not modify product code in S10. Any such modification invalidates the release candidate and requires a fresh S7 round.

## Inlined Methodology

The Loop uses a layered state model: a top-level Loop state plus constrained phase machines for complex stages plus independent Agent/TASK/BUG lifecycles. The eleven top-level states follow the single-direction main trunk `inactive -> planning -> document_verification -> building -> verification -> acceptance -> release_audit -> awaiting_human_release`, plus the `paused` and `aborted` terminals. Correction loops never bypass the trunk. `awaiting_human_release` is terminal for Loop automation: Hooks must strong-block squash merge, production deployment, and formal release when initiated by Loop automation. Invalid transition policy: undefined or guard-failing events do not change state, execute no side effect, record a rejected event, report the failed guard, and pause on repeated runtime-integrity failure. Idempotency uses compare-and-swap or equivalent revision checks; one committed transition per runtime revision; idempotency keys for state-changing actions; stale writers reload instead of overwriting. The 16 preservation invariants (INV-001..INV-016) map to enforcement layers (schema, hook, guard, combined). ACC and release architecture audit reference the clean-round record by ID and hash; later changes do not rewrite the record.
