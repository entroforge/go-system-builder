# Runtime Recovery Reference

## Public command contract

The CLI entrypoint is `runtime recover` in
`internal/cli/recover_command.go`:

```text
runtime recover inspect --root . --req <locked-REQ-path>
runtime recover plan    --root . --req <locked-REQ-path>
runtime recover apply   --root . --plan <plan.json> --approved-by <identity>
```

`inspect` is read-only. `plan` writes only under `.claude/recovery/`.
`apply` is the only recovery command allowed to replace the active Runtime
pair.

## Source precedence

Use exactly one source mode; never silently merge a failed high-fidelity source
with a lower-fidelity source:

1. Pending operation completion: a validated commit, fingerprint, rollover,
   or recovery marker.
2. Exact snapshot replay: only when a trusted full snapshot and gap-free,
   replayable journal tail are available.
3. Artifact reconstruction: explicit REQ plus verified documents, evidence,
   BUG/TASK records, manifests, and checkpoints.
4. Conservative seed: explicit REQ only, bound at `planning.design` / S2.

Current implementation is strongest on pending completion, artifact
reconstruction, and conservative seed. Exact snapshot replay remains a future
extension; do not claim it exists merely because a journal is present.

## Implementation seams

| Concern | Code | Contract |
|:---|:---|:---|
| Inventory / REQ validation / imports | `internal/recovery` | Explicit REQ, path containment, SHA-256, import allowlist, source conflicts |
| Replay | `internal/controller/replay.go` | Formal candidates/gates only; stop on repeat, no progress, path escape, conflict, or unknown gate |
| Durable replacement | `internal/runtime/recovery.go` | Quarantine, pending marker, candidate hashes, atomic pair replacement, manifest, idempotency |
| Ordinary Runtime writes | `internal/runtime/store.go` | `NewWriter` plus `semantic.RuntimeCandidateValidator`; read-only `NewStore` must not repair |
| CLI error contract | `internal/cli/recover_command.go` | Stable `LOOP_*` recovery error classes using `errors.Is/As` |

## Stable recovery error classes

| Code | Meaning | Action |
|:---|:---|:---|
| `LOOP_RUNTIME_MALFORMED` | Runtime bytes are missing/malformed/BOM/invalid JSON | Build a recovery plan |
| `LOOP_RECOVERY_REQ_INVALID` | Explicit REQ is missing, unlocked, malformed, or outside root | Select a valid locked REQ; do not mutate Runtime |
| `LOOP_RECOVERY_INPUT_DRIFT` | A planned input changed before apply | Discard/rebuild the plan |
| `LOOP_RECOVERY_SOURCE_CONFLICT` | Trusted sources disagree or a different plan is pending/applied | Stop and escalate conflict |
| `LOOP_RECOVERY_GATE_UNKNOWN` | Replay cannot prove the next gate | Stop at the last proven cursor |
| `LOOP_RECOVERY_PLAN_INVALID` | Plan identity/content/candidate is invalid | Do not edit it; regenerate |
| `LOOP_RECOVERY_APPLY_PENDING` | Durable recovery operation is incomplete | Retry the same apply through the recovery path |
| `LOOP_RECOVERY_ALREADY_APPLIED` | Same plan and candidate were already applied | Verify manifest and active pair; do not reapply a new plan |

## Evidence and state rules

- Evidence import must pass schema, digest, runtime/REQ/generation/round,
  producer, conclusion, and subject-reference checks.
- Artifact existence proves existence only; it does not prove that a gate
  passed.
- Recovered Agent entries are inactive. Activation/readback must be repeated.
- Recovery creates a new Runtime epoch and explicit lineage; it does not claim
  byte-identical continuity with the damaged projection.
- A later artifact cannot skip an earlier failed gate.
- The old state, journal, and known pending markers must remain byte-for-byte
  available in quarantine.

## Verification matrix

At minimum cover:

- missing, malformed, BOM, schema-invalid, and semantically-invalid state;
- stale or malformed journal and state/journal mismatch;
- pending commit/fingerprint/rollover/recovery marker completion;
- REQ invalid, path escape, input drift, source conflict, unknown gate, and
  repeated/no-progress replay;
- failure injection before quarantine, marker, state replacement, journal
  replacement, and manifest commit;
- idempotent apply and concurrent apply conflict;
- quarantine preserves original bytes and final state/journal validate.

Primary tests are under `tests/system/recovery/` and
`internal/runtime/recovery_test.go`.
