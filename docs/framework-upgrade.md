# Release compatibility and fresh installation

This release introduces layout v2. Install it only into a new, empty target with
`loop-harness install --source <verified-release> --root <empty-target>`.
The installer rejects occupied targets. Old or mixed authority layouts are rejected
by `projectlayout.Check`; this is not an in-place migration facility.

Existing projects, especially those with an active bound REQ, must remain on their
matching framework release. Do not overlay binaries, docs, skills or settings from
this release, reinitialize their Runtime, or rewrite historical evidence paths.
Completing a REQ does not by itself migrate that project's stored references.
A future migration needs a separate reviewed procedure and recovery validation.

A Runtime schema bump does not isolate every old writer. The fixed installed
predecessor tested on 2026-10-04 rejected ordinary v2 mutations but its
`runtime recover plan` / `apply` could reconstruct that state as v1. Any future
migration must first stop and isolate incompatible writers, including recovery
commands and copies of older binaries. Updating this source cannot revoke an
already installed executable's filesystem access. No migration is enabled by
this repair.

Legacy reconstruction now reports `LOOP_RECOVERY_PROTOCOL_UNSUPPORTED` when
state, journal or pending inputs contain recognized unsupported protocol
declarations. Read-only `runtime recover inspect` remains available. Preserve
the complete state/journal/artifact/pending set and use its matching recovery
implementation; do not erase version fields or markers to force reconstruction.
Damaged inputs that have lost all protocol declarations cannot prove their
version and still require the original release identity and recovery records.

Verify the package manifest before installation. Installed links are rewritten for
the project layout, so installed hashes must be recorded separately from package
hashes. Framework assets are tooling, never business acceptance evidence.

Hooks are fast control-plane checks only: they evaluate the gate, persist the
resumable Milestone, and surface pending work; they never run integration
builds or tests. Heavy integration runs outside the Hook — on SubagentStop the
main session explicitly invokes `runtime task-integrate`, which performs the
merge, checks and cleanup under its own command budget. All command hooks keep
the 10-second timeout; the Harness enforces its own smaller internal budget, so
raising the platform timeout cannot cure an internal overrun. A timeout must
preserve a resumable checkpoint; increasing the timeout is not evidence that a
check passed.

### Investigation approval pending bundles

S8 Contract approval now publishes its immutable Contract and Case revision via
Runtime pending protocol `2.2.0`. This narrowly adds
`.claude/review/investigation/contracts/` and `cases/` to transaction outputs and
keeps earlier pending schemas byte-identical. Preserve the pending marker,
staging, published artifacts and matching state/journal together after an
interruption. The matching writer recovers them before an explicit approval
operation retry. Do not mix this binary with older writers or infer a complete
migration fence from a schema-version rejection.

### S9 stop-condition completion checks

Canonical approved-contract sessions now require independent, complete stop-condition assessments at targeted PASS and handoff. Existing artifacts and approved Contract bytes are not rewritten; a historical PASS without assessments remains readable but cannot complete a live S9 chain. Create a fresh targeted reverification against the exact current Contract SHA, with one assessment per zero-based condition index. Run the matching binary and assets together, and isolate older writers: this additive artifact field is not a fence against old binaries. Generic TR-012 is rejected while a canonical RepairSession pointer exists; use `runtime repair handoff commit`.

### Zero-change confirmation artifacts

Repair confirmation uses separate `1.1.0` Session, Result, Changeset and
ChangeImpact schemas. Older `1.0.0` artifacts remain readable; empty legacy PASS
artifacts do not gain confirmation authority by relabeling or by setting
`LOOP_ALLOW_EMPTY_FINGERPRINT`. New sources must bind a prior committed handoff and
current exact subjects; follow [the confirmation and restoration procedure](runtime-operations.md#zero-change-confirmation-and-legacy-authority-restoration).
Missing old fingerprint metadata has an explicit checked restoration command,
not an automatic rebaseline. Keep matching binaries/assets and preserve historical
artifacts. This is not a new general migration facility.

## Plan 1.1 / related-module verification

This release requires matching binaries, Skills, manuals and templates. Apply only to a fresh project or an inactive, unbound existing installation using the documented backup/controlled update process; preserve its module declaration and customized configuration. Do not replace an active legacy round with this binary or reset Runtime to bypass the version check. Historical Plan 1.0 remains readable but executable new rounds require Plan 1.1. Results remain 1.0. Old binaries may ignore new JSON fields when loading a pinned plan; remove older executable paths from the active toolchain and retain a complete matching rollback set. This does not revoke external old writers. See [the scope contract](related-module-verification.md).
