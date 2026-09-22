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
