# Fork migration and framework review — 2026-09-15

## Repository lineage

The destination clone is `jarodlaw23/go-system-builder`, a GitHub fork of
`entroforge/go-system-builder`. Its `origin/main` and `upstream/main` both
pointed to `1df7763` when inspected. The older checkout's origin is
`jarodlaw23/loop-harness`; its latest fetched commit is `df31016`.
Do not confuse that repository's updates with commits already accepted upstream.

The migration branch `fix/harness-portability-and-evidence` retains the three
existing commits `58755c9`, `89b123a`, and `df31016`, plus the reviewed working-tree
changes. The original checkout remains untouched. Future pull requests target
`entroforge/go-system-builder:main` from the branch on the fork; copying the
changes back into another local checkout is not a prerequisite.

Historical session reports, archives, original hashes and validation logs are
kept locally under `.git/local-migration/20260915/` in the destination. They are
not public PR artifacts. No product Runtime, journal or binary is migrated.

## Review findings and repairs

### P1: self-declared deletion bypassed the S7 frozen baseline

The later local G14 change accepted any absent subject whose `kind` was
`deleted`, without verifying deletion evidence. Restoring a file with exactly
its former bytes also passed. Ten negative scenarios reproduced this behavior
against the original local implementation.

The final implementation requires an indexed, valid, hash-verified deletion
fact in the same Runtime and baseline generation, explicitly cited by the
current round entry or the current ReviewPlan's ChangeImpact source refs.
A prior round's deletion can therefore carry forward without inventing a new
fact. The latest live change-impact fact for the path governs; later
add/modify records cannot be overridden by an earlier deletion. A declared
deleted subject must still be absent, even if reappearing bytes match its hash.
Unreadable or unverifiable candidate change-impact history fails closed.

Tests cover historical citation, self-declaration, missing citation, tampering,
foreign Runtime, old generation, invalidation, later modification, identical
file resurrection, wrong digest, and stale plan coordinates.

### P1: S10 alias recovery could conceal document drift

One of the inherited local commits treated any failed authoritative document
read as recoverable if another document shared its digest. This included
hash drift, unrelated paths, and different identities/kinds.

Recovery now applies only to an absent case-variant path for the same document
identity and kind with the same registered digest. A readable but altered
original is an error. Negative tests cover tampering, unrelated paths,
different identities and different document kinds. Case-alias absence tests
explicitly require a case-sensitive filesystem; they do not misstate Windows
filesystem behavior.

### Retained protections from the preceding integration

- REQ fingerprints retain raw-byte SHA-256. CRLF parsing does not silently
  migrate existing Runtime identities or rewrite locked files.
- S10 gate evidence references select the actual envelope whose manifest was
  validated; the Controller no longer chooses a different older envelope.
- Historical unauthorized producers never qualify as evidence. A qualified
  replacement can satisfy the slot without mutating an old envelope.
- Investigation re-entry follows the active investigating/investigate_more
  pointer within bug_resolution; product-write authority is still separate.
- Sequential RepairResult aggregation retains the later path version; the
  complete batch is still checked against the actual Session diff and impact,
  with assignment dependencies, locks and duplicate-submission checks intact.
- Invalid explicit prompt_ref paths cannot silently select another Assignment;
  repository escape and ambiguous assignment cases remain rejected.
- /dev/null handling remains a narrow read-probe exception, not arbitrary
  interpreter or compound-command authorization.
- Tests that previously depended on the developer's ignored Runtime now use
  isolated roots and seeded state.

## Claude Code official documentation check

Official pages retrieved on 2026-09-15:

- https://code.claude.com/docs/en/hooks
- https://code.claude.com/docs/en/memory#agentsmd

Confirmed documented behavior:

- `systemMessage` is a user notice; supported event-specific
  `hookSpecificOutput.additionalContext` fields carry model context.
- Hook decision fields and exit-code behavior depend on the event. A
  SessionStart/SubagentStart notice is not an approval barrier. The framework
  must retain its separate dispatch and tool-use gates.
- Claude Code reads CLAUDE.md; `@AGENTS.md` imports shared instructions.
  Installation preserves existing CLAUDE.md content and avoids duplicate
  standalone imports.
- Imported files still consume context. Deduplication and compact packets do
  not establish measured token savings. AGENTS-template.md is 291 lines;
  the official guidance targets under 200, so a later editorial reduction is
  advisable without removing safety or lifecycle contracts.

S7/S9/S10, Runtime, manifests, producer roles and fingerprint policy are this
framework's own contracts, not workflows prescribed by Claude Code. The
extra `quality_gate` payload is framework metadata, not an official Claude
Code field. Tests verify emitted payloads and process behavior; this review
is not interactive Claude Code UI acceptance or token-billing measurement.

## Validation and limitations

Validation logs are retained in the destination's local migration archive.

Completed checks on Linux:

- `go test -p 1 ./... -skip '^TestOneOfPruningFallsBackToBranchSummary$'`: PASS.
- `go vet ./...`: PASS.
- Race suites for hook, hookctx, policy, qualitygate, review, repair: PASS.
- Additional acceptance race suite and alias regression: PASS.
- `git diff --check`: PASS.

The full serial suite excludes only
`TestOneOfPruningFallsBackToBranchSummary`, the previously reproduced baseline
schema-summary assertion failure. That failure is not relabeled PASS.
Targeted deletion and alias integrity tests, relevant race checks and go vet
are also recorded. A passing suite is not proof of every deployment scenario.

No binary was installed, no active product state was changed, and no branch
was pushed or PR submitted during this local migration. Before publishing a
PR, include the known baseline test limitation and the platform/UI boundaries
in the PR description; do not advertise an unqualified all-platform approval.
