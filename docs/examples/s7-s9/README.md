# S8/S9 request-file examples

These files are copyable starting shapes for the `--file` verbs. They are
authoring examples, not committed evidence: replace every path, SHA-256,
identity, ID, and assertion with the values from the current Runtime.

Each file is the **request body** a verb accepts (its field names track the
CLI request contract; a test pins them so renames surface here first). The
runtime derives and persists the durable record from your request — that
persisted artifact is what the schemas under `internal/schema/assets/`
(`repair-*.schema.json`, `review-evidence.schema.json`) describe, which is
why those schemas use different field names (e.g. `session_id`/`reported_at`
on the persisted record vs `session_ref`/`occurred_at` on the request). Do
not validate these request files against the artifact schemas; submit them
through the verb and let the runtime validate the derived chain and the
current revision.

## Request file → command map

| Request file | Command that consumes it |
|:---|:---|
| `repair-contract-draft.json` | `loop-harness runtime investigation contract approve --case-id <case> --file repair-contract-draft.json --approved-by <actor> --approval-hash <sha256> --approval-evidence-id <evidence-id>` |
| `repair-plan-report.json` | `loop-harness runtime repair plan-report submit --file repair-plan-report.json --actor <agent>` |
| `repair-result.json` | `loop-harness runtime repair result submit --file repair-result.json --actor <agent>` |
| `change-impact.json` | `loop-harness runtime repair impact create --file change-impact.json`, then `... impact commit --file <created-impact.json>` |
| `targeted-reverification.json` | `loop-harness runtime repair targeted create --file targeted-reverification.json`, then `... targeted commit --file <created-reverification.json>` |
| `repair-handoff.json` | `loop-harness runtime repair handoff create --file repair-handoff.json`, then `... handoff commit --file <created-handoff.json>` |

The placeholder IDs, paths and hashes in these examples are not valid evidence
for a real run. Runtime and Case writers consume current pointers when the
optional explicit revision/hash assertions are omitted.

## The normal handoff path

1. S8 registers a falsifiable hypothesis with flags, not a JSON file:

   ```text
   loop-harness runtime investigation hypothesis register \
     --case-id investigation-case-... --id hypothesis-1 \
     --assignment-id assignment-s8-hypothesis-1 \
     --statement "..." --invariant "..." \
     --discriminator "..." --support "..." --refute "..." \
     --source-finding finding-...
   ```

2. S8 dispatches the registered discriminator to a real Investigator. The
   `assignment_id` must be the one stored on the Hypothesis; the command
   generates the manifest/task and exposes the generic PLAN_REPORT checkpoint:

   ```text
   loop-harness runtime investigation dispatch \
     --case-id investigation-case-... \
     --hypothesis-id hypothesis-1 \
     --agent-id agent-investigator-1
   ```

   After the Investigator returns its read-only evidence, submit the
   hypothesis result with flags:

   ```text
   loop-harness runtime investigation hypothesis result \
     --case-id investigation-case-... --hypothesis-id hypothesis-1 \
     --assignment-id assignment-s8-hypothesis-1 \
     --method "read-only boundary trace" --observed "..." \
     --result supported --explains finding-... \
     --source-boundary src/server/decoder.go:87 \
     --evidence path:... --counterfactual "..."
   ```

   Then route the causally closed Case. For
   `s9_repair`, the route files can start from
   [`causal-model.json`](causal-model.json),
   [`blast-radius.json`](blast-radius.json), and
   [`detection-gap.json`](detection-gap.json). Approve the resulting Contract
   from [`repair-contract-draft.json`](repair-contract-draft.json):

   ```text
   loop-harness runtime investigation route \
     --case-id investigation-case-... --route s9_repair \
     --reason "..." --primary-root-cause "..." \
     --causal-model-file causal-model.json \
     --blast-radius-file blast-radius.json \
     --detection-gap-file detection-gap.json

   loop-harness runtime investigation contract approve \
     --case-id investigation-case-... \
     --file repair-contract-draft.json \
     --approved-by main-session \
     --approval-hash <draft-sha256> \
     --approval-evidence-id <human-decision-id>
   ```

   Its top-level `next` is the executable recovery action for the current Case.

   The multi-value S8 flags (`--source-finding`, `--source-boundary`,
   `--evidence`, `--explains`, and `--does-not-explain`) may be repeated or
   supplied as comma-separated values. The CLI preserves the full list; the
   Case validator remains responsible for required values, duplicates, and
   source-Finding subset checks.

3. S9 submits one domain PlanReport per repair Assignment using
   [`repair-plan-report.json`](repair-plan-report.json). This is separate from
   the generic `PLAN_REPORT` sent through `SendMessage`.
4. After the red checks are accepted, run `runtime repair execution begin`.
5. Submit one exact-unit RepairResult using
   [`repair-result.json`](repair-result.json), then compute the Changeset.
6. Create and commit ChangeImpact using
   [`change-impact.json`](change-impact.json). The `scope` of every decision
   must cover each changed artifact.
7. Create and commit independent targeted reverification using
   [`targeted-reverification.json`](targeted-reverification.json), then create
   and commit the RepairHandoff using [`repair-handoff.json`](repair-handoff.json).
   Handoff commit fires TR-012 and writes the S7 seed; inspect `loop-harness
   s7 status` before dispatching the next round.

## Non-S9 route consumption (investigation consume)

`runtime repair dispatch` does not accept `--manifest`. It generates the
manifest and task internally from the immutable RepairAssignment, then calls
the common workgroup registration path:

```text
loop-harness runtime repair dispatch \
  --assignment-id repair-assignment-... \
  --agent-id agent-... \
  --role-family backend-builder \
  --agent-definition agents/backend-builder.md
```

The generated paths are returned in the command JSON output. The Builder must
send one generic `PLAN_REPORT`, submit the domain PlanReport, and wait for
`runtime repair execution begin` before product writes are allowed.

## Artifact SHA freshness

Commit verbs can rewrite a previously created artifact (e.g. `impact
commit` flips its status): the SHA printed by the `create` verb is stale
after `commit`, and changeset filenames are content-hashed (they change
when content does). Before authoring `repair-handoff.json`, re-read every
referenced path+SHA from disk — the handoff validator compares against
the current bytes, not against what an earlier verb printed.
