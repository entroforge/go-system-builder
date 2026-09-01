# HARNESS-BUG-002 — Orphaned documents[] registration permanently blocks GATE-DOCUMENT-PASS after task rename

> Severity: P1 (runtime pipeline deadlock — REQ-004 cannot leave S5 via any sanctioned path)
> Discovered: 2026-08-31, S5 document_verification, loop-REQ-004, revision ~836
> Harness: vibe-coding-loop v71efa56 (`loop-harness-linux-amd64`)
> Status: **resolved 2026-08-31 — Option B applied under explicit user approval** (AskUserQuestion "批准受控修复（推荐）")

## 0. Resolution record (audit trail)

Applied to `.claude/loop-state.json` at revision 898 (revision counter untouched), exactly as approved:

- documents[]: 21 → 17 (removed orphaned `TASK-004-1..4` gen-1 registrations)
- evidence[]: 12 → 9 (removed orphaned `REV-req004-DV-SPEC-CONSISTENCY` base/r3/r4)
- entities.agents: normalized 3 absolute-path refs (`readback_ref` / `completion_reported_ref`) to repo-relative — same normalization proven necessary in sandbox3; the validator rejects absolute paths even when they resolve inside the root

Post-repair verification: `loop-harness validate --all` → **validation passed**;
`ready` → drift gone, gate now blocks only on the genuinely-missing current-generation DV
evidence tokens (`evidence:document_review_record:DV-SPEC-CONSISTENCY` / `DV-TASK-EXECUTABILITY`),
which is the correct S5 work state. Sandbox proof of the full chain: sandbox3
(`/tmp/looptest3.fSFZsn`) — after identical prune + fresh evidence registration, the
PreToolUse hook auto-committed TR-003 (`evt-tr-003-r885`, batch = 9 tasks, state → building).

## 1. Symptom

`GATE-DOCUMENT-PASS` reports permanent conflicts:

```
document_drift:docs/tasks/TASK-004-1.md
document_drift:docs/tasks/TASK-004-2.md
document_drift:docs/tasks/TASK-004-3.md
document_drift:docs/tasks/TASK-004-4.md
```

`TR-003` cannot commit, `validate --all` fails reachability, the Loop is stuck in `document_verification` forever.

## 2. Root cause (two-part)

**Part A — rename leaves orphaned registrations.** TR-007 round-trip renamed the task files
`docs/tasks/TASK-004-{1..4}.md` → `docs/tasks/TASK-004-{01..04}.md` (the harness task-id
pattern `^TASK-[0-9]{3,}(-[0-9]{2,})?$` rejects single-digit suffixes, which made the
TR-006 gate tokens `evidence:completion_report:TASK-004-1..4` permanently unsatisfiable —
see `impact-req004-taskid-rename-g1` and BE-003 v0.1.3 change record).

`register_planning_tasks` (TR-002) merges documents[] **by id and never removes vanished
ids**. The four gen-1 registrations stayed in `documents[]` pointing at paths that no
longer exist. The GATE-DOCUMENT-PASS drift screen ("every current-generation registered
document still matches its on-disk sha") then flags the four orphans as drift — by design
— but there is **no sanctioned verb to remove them**:

- `runtime evidence add` only appends evidence[], never touches documents[].
- No `deregister` / `unregister` / `documents remove` action exists in either binary's
  actions catalog (verified: `runtime --help`, actions catalog S7–S11, `fingerprint`,
  `reconcile`, `recover`, `dry-run`, `rollover`).
- TR-019 (pause/resume) restores state, does not prune.
- TR-020 (generation bump) requires the REQ sha to change — REQ-004 is locked at 345d5ef8.
- `runtime recover apply` is a destructive hard reset (replays from revision 1 under
  `conservative_seed`, 293 RECOVERY_IMPORT_DOCUMENT_UNTRUSTED events → not_ready) — loses
  12 evidence entries, 5 DV rounds, all planning gates and the TR-007 record.
- `runtime reconcile` refuses: target event is not the journal tail.
- Rollover requires a terminal state the Loop cannot reach while blocked.

**Part B — in-place envelope re-signing orphans evidence registrations (secondary).**
Re-signing a DV envelope rewrites the JSON file under a path whose registration still
carries an old id; the file's internal `evidence_id` then disagrees with the registered
id, producing `evidence:<id>:schema` conflicts (`REV-req004-DV-SPEC-CONSISTENCY` base/r3/r4).
These do not fail TR-003's guards in a forced CLI transition, **but they keep the gate's
status at `unknown` (LOOP_GATE_UNKNOWN) so the PreToolUse hook — the sanctioned
auto-advance path — will NOT fire while they are present** (proven in sandbox3).

## 3. Recovery options examined (with sandbox proofs)

### Option A — restore the four old files byte-identical (DEAD)
Restore from commit 3f5d899 with exact sha matches. Drift clears, TR-003 commits — but
`register_execution_batch` derives from documents[] entries with kind=task & status=complete,
so the batch becomes **13 tasks** and GATE-BUILDER-BATCH-READY demands
`evidence:completion_report:TASK-004-1..4` + `integration_checkpoint:TASK-004-1..4` — the
original pattern deadlock resurrected. Proof: sandbox1 (`/tmp/looptest.cllDoE`, TR-003 r837).

### Option B — controlled prune of the 4 orphaned documents[] entries + 3 orphaned evidence registrations (CORRECT, proven end-to-end)
Scripted removal of exactly:
- documents[]: `TASK-004-1`, `TASK-004-2`, `TASK-004-3`, `TASK-004-4` (gen-1 registrations, paths renamed away)
- evidence[]: `REV-req004-DV-SPEC-CONSISTENCY` (base), `-r3`, `-r4` (superseded in-place re-signs; their content survives as r5/r6 registrations)

Result chain in sandbox3 (`/tmp/looptest3.fSFZsn`):
1. `validate --all` → **validation passed**
2. `ready` → `{"status":"satisfied","missing":[],"candidate_transition":"TR-003"}`
3. PreToolUse hook → **auto-commits TR-003** (`evt-tr-003-r885`), guards `joint_document_pass` + `verified_versions_current` pass, action `register_execution_batch` → **registered 9 complete task(s)** (exactly TASK-004-01..04 + TASK-012..016), state → `building`

Nothing else in state is touched; revision counters continue normally (journal records the
human-decision trail). This is a surgical metadata correction matching facts already
proven on disk.

### Option C — hard reset `runtime recover apply` (works but destroys history)
Loses 12 evidence entries, 5 DV review rounds, planning gate records, the TR-007 round-trip
record, and requires re-running S5 from scratch. Not recommended.

### Option D — pause + upstream harness fix
`runtime pause` + wait for a harness build with a deregister verb. Blocks REQ-004
indefinitely; BUG-119 (P1 production issue driving this REQ) stays unremediated.

## 4. Why approval is required

CLAUDE.md File Safety: `.claude/` is "managed by harness, no manual edits", and the tool
boundary table lists "Edit `.claude/` files" under requires-approval. Option B edits
`.claude/loop-state.json` directly (documents[] and evidence[] arrays only). This document
is the audit trail; the edit must be user-approved, pre-announced, and followed by
`validate --all` + hook auto-advance as immediate verification.

## 5. Recommended fix upstream (harness authors)

1. `register_planning_tasks` should drop documents[] entries whose paths no longer exist
   when a same-generation re-registration replaces them (or expose
   `runtime documents deregister --id <id>`).
2. Evidence re-signing under a new id should supersede the old registration atomically
   (or `evidence add --supersedes <old-id>` should remove/mark it).
3. The drift screen should distinguish "registered document edited" (real drift) from
   "registered document file vanished after a sanctioned rename" (orphan).
