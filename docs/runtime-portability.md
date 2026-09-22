# Runtime portability and recovery

## Executables and entry points

Install all binaries from one release: darwin/arm64, darwin/amd64,
linux/arm64, linux/amd64, windows/amd64. Use the Bash launcher as
`.claude/bin/loop-harness`; use `loop-harness.ps1` from native PowerShell.
Do not copy a Linux executable over a shared Windows entry point. WSL is
Linux; native PowerShell is Windows. `version` reports the actual executable,
OS, architecture, Go version and available VCS build metadata.

Claude Code invokes the hooks registered in its settings. Codex and manual
terminals do not acquire those hooks merely by reading AGENTS.md. They must
explicitly call Harness commands and verify Runtime integrity before mutation.
Recovery inspection/planning is separate from product implementation authority.

## Storage contract

Merge packaging/project.gitattributes into a new project's attributes before
binding a REQ. CRLF conversion changes SHA-256 even when displayed text is
identical. REQ fingerprints retain raw-byte SHA-256, including for CRLF
files bound by older versions; CRLF parsing does not normalize their identity. Existing locked evidence must not be renormalized silently: preserve
bytes and use an approved recovery/migration with a recorded new baseline.

The active state and journal are one recovery unit. Never synchronize just
`loop-state.json` through Git while leaving the journal local. A Git checkout
is not a Runtime restore. Keep source/specifications in Git; keep active
Runtime state, journal, pending markers and archives together in backed-up
local storage. Existing projects must back up their active unit before
untracking an already tracked snapshot; adding an ignore rule alone does not
untrack it. Do not delete old evidence while references remain live.

Run `deployment-check --root <root>` before work in Claude Code, Codex or a
manual terminal. It detects mixed Runtime identities, tail mismatches and
state-only Git tracking; it is a preflight, not full semantic validation.

For a machine handoff, stop all writers and package a complete project
checkpoint (including uncommitted source), excluding only reproducible caches
and unrelated worktrees. Include state, journal and rotated segments, pending
markers, archives, evidence/review/workgroup material, selected locked REQ,
definition, policy, source commit/diff, binary version and a SHA-256 file
manifest. A bundle must remain private if it includes private source/config.

On the destination, unpack into a new directory, verify every manifest hash,
select the native binary, validate the state/journal pair and referenced
evidence, and only then switch the active workspace. Never unpack over a live
Runtime. Do not concatenate journals or merge Runtime JSON using Git. A
missing/mismatched pair goes through `runtime recover inspect/plan/apply`,
not normal import or repeated reconcile. This is the operator handoff
procedure; an automated bundle CLI is not provided yet.

## Recovery

1. Preserve damaged state/journal and all pending markers byte-for-byte.
2. `runtime recover inspect --root <root> --req <explicit-locked-REQ>`.
3. `runtime recover plan` with the same arguments. Review imports, source
   conflicts, target cursor, lineage and candidate hashes.
4. Apply the generated plan only with explicit operator approval. Existing
   recovery quarantines old bytes and uses a pending marker to resume an
   interrupted pair replacement. It is not a single filesystem transaction.
5. Validate the pair, retained evidence and next gate before product work.

Recovery may conservatively stop at S2; existing code is retained but its
verification claims must be re-established. A malformed or unrelated journal
cannot justify reconstructing missing historical events as if they occurred.

## Windows durability boundary

POSIX platforms fsync parent directories. Windows `os.Open(directory).Sync()`
fails because its directory handle cannot be flushed like a writable file.
On Windows the writer still flushes regular files and uses recovery markers,
but only checks directory existence/type at the directory-sync boundary.
This supports process-interruption recovery; it does not promise POSIX-equivalent
directory-entry durability after sudden power loss. Keep external backups;
do not silently interpret an unsupported directory flush as a successful one.

See https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-flushfilebuffers.
