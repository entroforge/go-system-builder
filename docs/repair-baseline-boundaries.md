# Repair change boundaries and recovery

Use the original RepairSession, assignments, accepted Builder results and actual
available changes to understand the repair. Main handles the real integration and
checks; independent Targeted verification and the next S7 remain separate
responsibilities. Normal Git operations can show actual changes, but a Git OID,
content hash, file fingerprint or a full-tree byte comparison is not a new
qualification for receiving a result or continuing the workflow.

Preserve user files, original receipts and in-flight responsibilities. A missing
result receipt does not prove that no work happened; a missing original baseline
does not prove that the current tree is unchanged. If available facts cannot
distinguish those cases, report the specific unknown to Main instead of guessing,
deleting an existing Session, or requiring an author to create a new proof.

For an explicit change list, use the actual paths and their observed statuses;
do not invent a deleted or changed file from incomplete history. Ignore decisions
do not by themselves establish that an existing file belongs to the framework or
is safe to remove. Hook source and tracked product files remain part of their
actual task scope; a decision journal or a generated log is not automatically a
product fix.

Some legacy Session change and replacement paths still depend on a previously
captured baseline digest and are being migrated to this contract. If such an
entry reports a digest requirement, retain the original Session and worktree,
report the system recovery gap and available facts, and do not ask the Builder
to register a replacement hash or assert that an unknown tree is unchanged.
