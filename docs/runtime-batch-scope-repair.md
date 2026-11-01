# Repair a foreign-REQ execution batch

Use this maintenance path only for the historical registration bug where
explicitly foreign REQ documents were registered into the current execution
batch. It is not a normal stage transition or a way to skip Builder work.

The normal path now filters explicit top-of-document REQ ownership when
registering design/contracts/tasks. Body references are dependencies, not
ownership. Unlabelled legacy documents retain compatibility behavior; do not
infer ownership from numeric filename suffixes. New TASKs should declare
`Source REQ refs`. TR-003 consumes the already registered
current-generation tasks; it must not rescan and expand the reviewed batch.
The task checker uses the current REQ's index clause universe. Contracts
proven to belong to foreign indexes remain readable dependencies without
forcing unrelated tasks into the current coverage denominator.

## Preconditions

Stop concurrent Claude writes and preserve a complete paired Runtime/journal
backup plus current uncommitted work. The repair command accepts only an
unpaused `building` runtime before S7, with a locked unchanged REQ, no
non-document-verifier agent, no Builder/completion evidence and no current
worktree integration artifacts. Review the original registered documents and
actual S5 responsibilities; historical SHA values are not qualifications.
Do not alter files or fabricate evidence to make a preflight pass.

## Inspect and apply

Use the installed candidate first against a complete isolated project copy.
The default command is read-only and emits a reviewable plan:

```bash
.claude/bin/loop-harness runtime repair-batch-scope --root . > /tmp/batch-scope-plan.json
.claude/bin/loop-harness runtime repair-batch-scope --root . --apply-plan /tmp/batch-scope-plan.json
```

The plan identifies the Runtime, existing responsibility, removed registrations,
retained tasks and affected reviews. The historical maintenance implementation
may still require hashes; if it does, preserve the original state and report that
system recovery gap instead of supplying or recomputing them. Runtime writer
coordination must not be replaced with a new hash or author proof.

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
