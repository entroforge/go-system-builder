# Upgrade a bound project

Build and install one release manifest from a clean source revision. Back up the
paired Runtime state/journal, checkpoints, installed framework and Git worktrees
before activation. Test the same release in an isolated project copy first.

Install release assets according to prelude.md, merging project-specific entry
text, commands, settings permissions and git attributes. Never copy source instance
REQs/TASKs/contracts, Runtime, journal or source-project reports into the project.
Do not initialize or rebind an existing Runtime. Do not renormalize active evidence.
Record source revision and SHA256 of each installed asset, plus explicit exceptions.

A requirement locked before Foundation installation retains its existing approved
requirements/contracts and design evidence. Installing Foundation templates does
not retroactively attest a Foundation or impose new product requirements. Apply
Foundation intake to subsequent unlocked requirements. Any actual change to the
bound requirement follows the existing human amendment Gateway.

Framework binaries and release docs are tooling, not completed business evidence.
Verify schema/definition/policy compatibility and current gate projection, exercise
Hook process boundaries, and test pending integration recovery before resuming.
Keep a rollback package; binary rollback never rewinds Runtime automatically.

Hooks are fast control-plane checks only: they evaluate the gate, persist the
resumable Milestone, and surface pending work; they never run integration
builds or tests. Heavy integration runs outside the Hook — on SubagentStop the
main session explicitly invokes `runtime task-integrate`, which performs the
merge, checks and cleanup under its own command budget. All command hooks keep
the 10-second timeout; the Harness enforces its own smaller internal budget, so
raising the platform timeout cannot cure an internal overrun. A timeout must
preserve a resumable checkpoint; increasing the timeout is not evidence that a
check passed.
