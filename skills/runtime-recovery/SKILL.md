---
name: runtime-recovery
description: Safely diagnose, plan, and rebuild a damaged Loop Runtime when loop-state.json or its journal is missing, malformed, schema-invalid, semantically invalid, inconsistent, or blocked by a pending durable operation; use for Runtime integrity failures, recovery self-lockout investigations, and runtime recover inspect/plan/apply work.
---

# Runtime Recovery

Use this skill whenever the Runtime control plane is not trustworthy. Treat
`loop-state.json` as a rebuildable projection, not as permission to guess or
to edit the file by hand. The recovery workflow is deliberately low-freedom:
inspect first, classify the failure, build a fingerprinted plan, obtain
explicit approval, then apply atomically.

## Authorities

- Current legal lifecycle: `docs/loop-definition.json`.
- Stage/operator contract: `docs/agent-protocol.md`.
- Recovery architecture and trust ordering: `references/runtime-recovery-reference.md`.
- Runtime implementation: `internal/runtime/recovery.go`, `internal/runtime/store.go`.
- CLI boundary: `internal/cli/recover_command.go`.

Do not invent a second lifecycle machine in the recovery code. Recovery must
reuse the current schema, semantic validator, Quality Gates, and Transition
Guards.

## Non-negotiable safety rules

- Do not edit `.claude/loop-state.json`, `.claude/loop-events.jsonl`, or a
  pending marker manually.
- Do not use `runtime rollover` to escape a malformed, unreadable, or
  semantically invalid Runtime.
- Do not treat `runtime reconcile` as a universal repair command. It is only
  appropriate when the state is readable and the journal is provably behind
  the committed snapshot.
- Do not infer the active REQ from timestamps, newest files, directory order,
  or artifact presence. The operator must supply one explicit locked REQ.
- Do not fabricate evidence, revive active agents/leases, or skip an earlier
  gate because a later document exists.
- Do not use a generic `--force` bypass. On drift, conflict, or unknown gate,
  stop and create a new inspection/plan.

## Workflow

### 1. Freeze and classify before mutation

Record the repository root, explicit REQ candidate, command/error output, and
whether each Runtime artifact exists. If the Hook blocks the suggested
recovery command, use an external terminal or a permitted read-only path; do
not repeatedly retry the same command through the blocked Hook channel.

Run the read-only inventory with an explicit locked REQ:

```bash
go run ./cmd/loop-harness runtime recover inspect \
  --root . --req docs/requirements/REQ-NNN.md
```

Classify the result:

| Observation | Correct route |
|:---|:---|
| State is readable/semantic-valid and only journal tail is missing | `runtime reconcile`, after verifying the exact missing event |
| State, journal, or JSON encoding is malformed; BOM is present; schema/semantic validation fails | `recover plan` → `recover apply` |
| A recovery/commit/fingerprint/rollover pending marker exists | Complete it through an explicit writer/recovery path; never repair it from a read-only command |
| Source files changed after planning | Reject the plan and inspect/plan again |
| Trusted sources disagree or replay reaches an unknown gate | Stop; surface the conflict/unknown gate; do not guess |

If classification is uncertain, choose recovery plan/apply. It is safer to
quarantine and rebuild conservatively than to mutate an ambiguous Runtime.

### 2. Build a content-addressed plan

```bash
go run ./cmd/loop-harness runtime recover plan \
  --root . --req docs/requirements/REQ-NNN.md
```

Review the generated plan before approval. Confirm all of the following:

- REQ path, ID, locked status, version, and SHA-256 are correct.
- Runtime, journal, pending markers, definition, policy, documents, and
  imported evidence are listed with fingerprints.
- The base mode is explicit: pending completion, exact replay, artifact
  reconstruction, or conservative seed.
- `target_cursor`, `confidence`, `replay_trace`, and `import_findings` are
  conservative and contiguous. REQ-only recovery must stop at
  `planning.design` / S2.
- Candidate state and journal are under the plan directory and have recorded
  SHA-256 values.
- The plan does not revive agents, leases, activations, or unverified BUG/TASK
  progress.

Never modify a plan to make it pass review. A changed input invalidates it.

### 3. Apply only with explicit approval

```bash
go run ./cmd/loop-harness runtime recover apply \
  --root . \
  --plan .claude/recovery/rr-<plan-hash>/plan.json \
  --approved-by <human-operator>
```

Apply must quarantine the old bytes, validate the candidate pair, write a
durable pending marker, replace state and journal atomically, write the
recovery manifest, and retire only the exact planned pending sources. If the
process stops, rerun the same approved apply; do not create a second plan
unless inputs drifted.

### 4. Verify the new Runtime

After apply, verify the manifest and quarantine paths, then run:

```bash
go run ./cmd/loop-harness validate --all --root .
go run ./cmd/loop-harness doctor --root .
go run ./cmd/loop-harness status --root .
go run ./cmd/loop-harness ready --root .
```

Check that the bound REQ digest matches disk, state revision and journal tail
agree, imported evidence is current and fingerprinted, recovered agents are
inactive, and the next cursor is a formal gate result rather than an inferred
milestone. Record recovery manifest, plan hash, quarantine path, and
verification output as evidence.

## Stop and escalate

Stop without mutation when the REQ is not locked, a plan input drifts, trusted
sources conflict, replay has no progress/repeats a cursor, a gate is unknown,
or the requested progress depends on an unresolved business decision. Report
the exact artifact, expected/actual hash, cursor, and safe next action.

Load [runtime-recovery-reference.md](references/runtime-recovery-reference.md)
when exact error codes, implementation seams, recovery-source precedence, or
test coverage are needed.
