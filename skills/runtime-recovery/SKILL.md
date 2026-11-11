---
name: runtime-recovery
description: Safely diagnose, plan, and rebuild a damaged Loop Runtime when loop-state.json or its journal is missing, malformed, schema-invalid, semantically invalid, inconsistent, or blocked by a pending durable operation; use for Runtime integrity failures, recovery self-lockout investigations, and runtime recover inspect/plan/apply work.
---

# Runtime Recovery

Use this skill whenever the Runtime control plane is not trustworthy. Treat
`loop-state.json` as a rebuildable projection, not as permission to guess or
to edit the file by hand. Inspect and classify the actual failure, prepare the
original recovery plan, and use the applicable authorized recovery entry.
Do not calculate or register fingerprints as a recovery qualification.

## Authorities

- Current legal lifecycle: `docs/control/loop-definition.json`.
- Stage/operator contract: `docs/control/agent-protocol.md`.
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
.claude/bin/loop-harness runtime recover inspect \
  --root . --req docs/requirements/REQ-NNN.md
```

Classify the result:

| Observation | Correct route |
|:---|:---|
| State is readable/semantic-valid and only journal tail is missing | `runtime reconcile`, after verifying the exact missing event |
| State, journal, or JSON encoding is malformed; BOM is present; schema/semantic validation fails | `recover plan` → `recover apply`; issued packet/reception indices require the latest original checkpoint |
| A recovery, commit or rollover pending marker exists | Complete it through its original writer/recovery path; a historical fingerprint marker retains its own original location |
| Source files changed after planning | Reject the plan and inspect/plan again |
| Trusted sources disagree or replay reaches an unknown gate | Stop; surface the conflict/unknown gate; do not guess |

If classification is uncertain, choose recovery plan/apply. It is safer to
quarantine and rebuild conservatively than to mutate an ambiguous Runtime.

### 2. Build the original recovery plan

```bash
.claude/bin/loop-harness runtime recover plan \
  --root . --req docs/requirements/REQ-NNN.md \
  --dev-branch <bound-development-branch> --release-upstream <bound-release-destination>
```

For an issued 1.2 Runtime, take the `latest_checkpoint` path from `recover
inspect` and add `--checkpoint <that-manifest-path>` to the plan command.
The resulting `checkpoint_restoration` / `original_pair` plan preserves the
original pair and cursor; it does not import answers or replay processed
followups. Review the exact owner, activation, revision lineage, invalidated
evidence and transition stamps. Document reconstruction cannot recreate them.
The original matching REQ, plan artifacts and independent Worker sources must
still verify. Apply refuses a live Worker launch lease, a later checkpoint,
changed known facts or a Runtime already closed by bind/unbind/rollover.

Review the generated plan before approval. Confirm all of the following:

- REQ path, ID, locked status and version identify the actual requirement.
- Runtime, journal, pending markers, definition, policy, documents and
  imported evidence identify the actual available sources and responsibilities.
- The base mode is explicit: pending completion, exact replay, artifact
  reconstruction, or conservative seed.
- `target_cursor`, `confidence`, `replay_trace`, and `import_findings` are
  conservative and contiguous. REQ-only recovery must stop at
  `planning.design` / S2.
- Candidate state and journal are available at the original plan paths.
- Artifact reconstruction does not revive agents, leases, activations, or
  unverified BUG/TASK progress. A checkpoint restores only their exact original
  durable facts, with stopped Workers and validated sources.

Never modify a plan to make it pass review. A changed input invalidates it.

### 3. Apply through the original authorized entry

```bash
.claude/bin/loop-harness runtime recover apply \
  --root . \
  --plan <actual-plan-path> \
  --approved-by <human-operator>
```

Apply must quarantine the old bytes, validate the candidate pair, write a
durable pending marker, replace state and journal atomically, write the
recovery manifest, and retire only the exact planned pending sources and journal
rotation segments. The quarantine preserves their original bytes. If the
process stops, rerun the same approved apply; do not create a second plan
unless inputs drifted.

### 4. Verify the new Runtime

After apply, verify the manifest and quarantine paths, then run:

```bash
.claude/bin/loop-harness validate --all --root .
.claude/bin/loop-harness doctor --root .
.claude/bin/loop-harness status --root .
.claude/bin/loop-harness ready --root .
```

Check the actual bound REQ and available original evidence, state revision and
journal relationship, recovered agent state and next cursor. Report the recovery
manifest, quarantine path and actual verification output. Missing historical
information remains unknown; do not require a digest to establish it.

## Stop and escalate

Stop without mutation when the REQ is not locked, a plan input drifts, trusted
sources conflict, replay has no progress/repeats a cursor, a gate is unknown,
or the requested progress depends on an unresolved business decision. Report
the actual artifact, observed conflict or unknown, cursor, and safe next action.

Load [runtime-recovery-reference.md](references/runtime-recovery-reference.md)
when exact error codes, implementation seams, recovery-source precedence, or
test coverage are needed.
