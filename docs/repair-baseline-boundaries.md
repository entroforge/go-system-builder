# Repair baseline boundaries and safe recovery

The Session baseline is immutable. Freshness and changeset preserve its historical
file membership; current ignore rules cannot remove that ownership. New capture
excludes generated output before reading its bytes.

- Main-tree tracked files remain protected from the new ownership exclusions.
- Git-registered nested worktrees belong to their own assignments. They are
  excluded only when the main index does not track content under that root.
  An arbitrary directory called .worktrees is not enough. Git before list -z
  is supported via quoted porcelain parsing.
- Python environments need a regular pyvenv.cfg marker, a Git ignore verdict,
  and no main-index tracked content. Vendored or unverifiable trees stay in
  scope. Missing Git does not grant exemptions.
- Runtime logs require a narrow output name plus Git ignored/untracked status;
  tracked fixtures remain protected, including staged and committed deletions,
  renames and later ignore-rule changes. Arbitrary extensionless files, .txt,
  .json and compressed archives are not automatically logs. The historical
  .claude/multi.json launcher profile exception is exact, not all of .claude.
- Excluded directories/logs are classified before file bytes are read. Included
  files are hashed as streams. No mtime-only pass cache is used.

A missing result receipt does not imply no implementation work: session
replacement checks plural result/plan refs, dispatched owners and actual diff.
If progress exists, resume with the original baseline instead of re-opening.
The Runtime fingerprint uses the Session's single captured digest. Drift errors
report all detected blockers (first 50 paths in the diagnostic), without file
contents, instead of forcing one full scan per newly revealed blocker.

This classification does not prove every possible generated file safe to ignore.
Existing cache-path exceptions remain compatibility rules. Do not add a broad
.gitignore bypass or erase legacy evidence to avoid investigating an unknown.

## Historical entries and Hook code

A legacy Session containing a runtime log without historical provenance keeps
that entry protected. An unchanged entry does not create a phantom deletion;
actual edits or deletion remain visible to freshness and changeset checks.
New Sessions exclude genuinely ignored, untracked log output as usual.

If such an old Session blocks on log drift, preserve its original artifacts and
inspect the drift. Use the supported Session replacement procedure only when its
preconditions allow it; otherwise retain the old environment and resolve the
migration at an approved baseline change window. Do not edit baseline hashes,
re-sign historical evidence, or claim generated logs as product fixes to clear a
gate. Current Git ignore status alone cannot prove historical ownership.

Only `.claude/hook-decisions.jsonl` is excluded as the Hook decision journal.
The `.claude/hooks/` directory and similarly named Hook source files remain
implementation inputs; edits/deletions require the normal scope and evidence
checks. This S9 baseline protection is not an OS sandbox or a substitute for
reviewing executable Hooks before enabling them.
