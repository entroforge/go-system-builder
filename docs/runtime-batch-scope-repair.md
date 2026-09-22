# Repair a foreign-REQ execution batch

Use this maintenance path only for the historical registration bug where
explicitly foreign REQ documents were registered into the current execution
batch. It is not a normal stage transition or a way to skip Builder work.

The normal path now filters explicit top-of-document REQ ownership when
registering design/contracts/tasks. Body references are dependencies, not
ownership. Unlabelled legacy documents retain compatibility behavior; do not
infer ownership from numeric filename suffixes. New TASKs should declare
`Source REQ refs`. TR-003 consumes the already registered, fingerprinted
current-generation tasks; it must not rescan and expand the reviewed batch.
The task checker uses the current REQ's index clause universe. Contracts
proven to belong to foreign indexes remain readable dependencies without
forcing unrelated tasks into the current coverage denominator.

## Preconditions

Stop concurrent Claude writes and preserve a complete paired Runtime/journal
backup plus current uncommitted work. The repair command accepts only an
unpaused `building` runtime before S7, with a locked unchanged REQ, no
non-document-verifier agent, no Builder/completion evidence and no current
worktree integration artifacts. Every registered document and valid S5
review must still match its recorded SHA-256. Otherwise diagnose separately;
do not alter files or fabricate evidence to make the preflight pass.

## Inspect and apply

Use the installed candidate first against a complete isolated project copy.
The default command is read-only and emits a reviewable plan:

```bash
.claude/bin/loop-harness runtime repair-batch-scope --root . > /tmp/batch-scope-plan.json
.claude/bin/loop-harness runtime repair-batch-scope --root . --apply-plan /tmp/batch-scope-plan.json
```

The plan pins the runtime ID/revision, serialized state hash, all inspected
file hashes, removed registrations, retained tasks and invalidated reviews.
Apply rechecks it under the Runtime writer's revision lock. A changed plan,
concurrent state change, file drift or replay is rejected.

The journaled `BATCH-SCOPE-REPAIR` mutation removes only provably foreign
current-generation registrations, preserves historical files and task/agent
entities, invalidates valid document-review evidence and returns to S5.
No requirement amendment, new baseline generation, approval, completion
report or integration evidence is synthesized. Existing authored work is
preserved. The next Hook refreshes the S5 milestone; independent reviewers
must issue new evidence over the corrected registered subject set before
TR-003 can open S6 again. Do not reuse the invalidated PASS by changing its ID.

## Limits

This path does not migrate an already executed batch. Absence of registered
Builder evidence is not proof that nobody edited product files: inspect the
working tree and worktrees before using it. It does not resolve semantic
contract findings or promise S5 will pass. Unlabelled historical contracts
may remain as reference documents until explicit ownership is established.
UI Skill and Design Foundation migration are separate work.
