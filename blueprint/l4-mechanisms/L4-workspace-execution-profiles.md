# L4 Registered workspace execution and bounded repair authority

Version: 1.0 (2026-09-21). Design decision implemented; real Claude workflow
acceptance remains a separate release condition. This file is framework design,
not a target-project runtime authority or an installation asset.

## Evidence, alternatives and decision

PR #8 review 5265399074 identified two reproduced failures on a68950b:
relocating Main passed PlanRebind but failed the real CLI Writer authority check;
ready Workers could not run pwd, git diff --stat or git log. Earlier delivery
failures required preserving source/merge identity and distinguishing retry from
code rework. These justify the following limited mechanisms, not unrestricted
new lifecycle states. The review and machine outputs belong in PR/CI or ignored
maintenance evidence, not target-project reports.

| Decision | Existing mechanism insufficient because | Minimal response | Cost and rejected alternative |
| --- | --- | --- | --- |
| Relocation transaction | A normal Writer correctly rejects a different root, including recovery at the moved root | Exact source snapshot, path-only delta, existing commit marker/journal, idempotent pointers | One durable relocation intent; reject generic skip-authority, hand edits and a second Runtime |
| Worker observation | File tools do not expose Git history/diffs; blanket Bash denial interrupts diagnosis | Fixed observe views, argv execution and safe Git controls; exact pwd | No new approval or revision; arbitrary shell parser and git-prefix allowlists are rejected |
| Runner preflight | Linux isolation/dependency failures previously appeared after dispatch | Probe before publishing execution/launch; explicit check_location main alternative | Linux dependencies, offline checks and 512 MiB remain visible costs; never silently run an unsandboxed Worker check |
| Check timing | Worker and merge-tree checks may have different inputs and costs | Paired checks remain default; reviewed post_merge exception | Saves a Worker run but discovers failures after merge; no general performance gain is claimed without measurement |
| Bounded repair grant | Repeating owner approval for unchanged, module-local technical repairs interrupts work | Optional scoped, fingerprinted, finite grant plus review per exact contract | Grant/usage evidence and explicit revocation semantics; no new release or REQ authority |

The owner requested preserving Main and scoped Workers while reducing repeated
interruptions. This permits implementing the options; it does not establish that
all projects should select them. Registered Worker execution is an explicit
prepare/launch choice, not a global replacement of Claude's native subagent mode.
Existing native flows retain their own controls. Unmeasured benefits remain
hypotheses to evaluate in real projects, not release claims.

## Sources, consumers and failure recovery

| Fact | Single source | Consumer | Invalidated by / recovery |
| --- | --- | --- | --- |
| REQ root/dev/release binding | bound_req.workspace | Writer, registry validation, integration | explicit REQ change or narrow relocation; never checkout to guess authority |
| Execution owner, scope, generation, base, check_location | Runtime workspace registry | prepare, launch, adapters, policy | revoke/replace; preserve old generation in history |
| Frozen input hashes | execution inputs | bootstrap/check/commit/delivery | changed input blocks; replan, never silently refresh hashes |
| Bootstrap files/digest | immutable bootstrap receipt + registry digest | launch/adapters | changed installed assets; preserve and explicitly replace |
| Claude UUID | execution platform_session_id | launcher/Hook identity mapping | resume same UUID or explicit replacement; exit0 does not complete task |
| Launch attempts/lease | Main launch records and process-owned lock | launcher/replacement/rebind | process exit releases lease; receipt retained for diagnosis |
| Worker check result | tree-hashed check receipt | operator/Worker report | changed tree requires rerun; no substitute for Main verified checkpoint |
| Integration stage and tested HEAD | existing checkpoint | Main integrator, ack, successor scheduling, cleanup | failure preserved at exact stage; retry/rework, not new parallel state machine |
| Rework evidence | archived checkpoint/candidate + Runtime ref | rework/resubmission | new candidate must be verified; no Git rollback |
| Relocation identity | immutable source/request/before/after intent | narrow Runtime transaction and pointer updater | CAS drift, wrong Git identity or active checkpoint blocks; same request resumes |
| Repair authority | human grant or REQ-pinned project policy hash | contract approval | scope/REQ/generation/expiry/revocation/hash/budget; return to normal human approval |
| Grant usage and exact technical review | Runtime configuration.repair + registered review SHA | approval transaction | atomic budget consumption; repeat approved contract is idempotent |

No observation creates completion evidence or increments Runtime revision.
Receipts referenced by active execution/checkpoints must remain available; archive
with the corresponding Runtime after terminal handoff. Never delete evidence to
make a failed check appear fresh. Platform launch/check logs are diagnostic;
Runtime/journal and immutable evidence remain authority.

## Worker capability contract

`runtime workspace observe --view cwd|status|diff|staged|log` is a fixed capability.
It resolves the registered Worker, validates owner/generation/bootstrap/inputs,
uses fixed Git argv, disables external diff/textconv, pager, fsmonitor and lazy
fetch, strips Git environment overrides, and bounds output/time. It does not
claim to sandbox arbitrary programs. File tools remain subject to normal scope.
Mutating commands use commit/check/report/deliver adapters; human-only actions
remain human-only. Unsupported raw Bash gets executable observation commands.

prepare --check-location worker (compatible default) probes actual isolation and
snapshot size; launch repeats the probe before reserving the Claude UUID. Main
mode is chosen explicitly at preparation, persisted on the execution and cannot
be toggled in flight. It removes Worker check commands but does not waive Main
required_checks or independent verification. An existing execution must be
replaced under existing guards to change runners. Network-dependent checks need
preinstalled offline dependencies or an explicitly selected Main runner. Native
Windows/macOS do not gain this project's Linux isolation because Claude offers
its own sandbox there; the two mechanisms are distinct.

## Check timing and convergence

Ordinary assignments retain Worker required_checks, normal merge, then Main
required_checks. post_merge requires an explicit reviewed dispatch plan stating
why a Worker run is unsuitable or its measured duplicate cost warrants deferral.
The immutable manifest selects the mode; no edit of active manifests. Static
identity/scope/locked-input/conflict checks stay in both modes. No result from a
Worker tree certifies the merged tree.

After a post-merge failure Main already contains the candidate. Preserve its
merge and source/target/check receipts. Environment failure retries the same
preserved stage; code failure reopens the registered owner through rework and
requires a new candidate/check result. No automatic reset/stash, ack, cleanup or
successor release before verified. Main is an integration workspace, not an
implicit deployment target. Work that cannot tolerate a failed merge in Main
must retain paired checks (which also cannot guarantee merge-tree success).

## Authorization and independence

L3 S8 defines one-shot human approval, per-REQ grant and REQ-pinned policy as
separate authority sources. No delegation means the prior human path. Grants do
not waive S2 design authority, REQ amendments, destructive-action approval,
review budgets, independent S9 verification, fresh S7 or S11. Technical reviewers
make semantic judgments; booleans are not machine proofs. Technical approval is
not an implementation acceptance report, and its reviewer is not automatically
the final verifier. S9 explicitly rejects a verifier who owns the repair.
Revocation prevents new approvals; stop existing approved work through explicit
pause/revoke, retaining history. Project policy has finite contract budget and
REQ/generation/hash lifetime even when no wall-clock expiry is selected.

## Acceptance and evolution

Required automation: real CLI relocation success/repeat and commit-window
recovery; conflicting old/new authority; observation outputs and helper/shell
injection rejection; no Main/Worker Git mutations; capability refusal before
launch; post-merge failure preservation; grant budget/revocation/identity tests;
full tests, race checks, release graph and installation checks.

Required real-platform acceptance: observe, edit, declared dependency/check path,
report, Main merge, failed-check rework, session interruption/resume and independent
verification using actual Claude Hooks. Record CLI version, platform, runner,
source/merge SHA, failures and next actions. CI/cross-build/native Hook smoke do
not replace it. Missing login/platform capability is NOT EXECUTED. Collect actual
costs and interruption counts before promoting opt-in modes to defaults.

Change record: 2026-09-21 — PR #8 reproduced R1/R2 plus L3/L4 conflict prompted
this minimal recovery/observation decision and restored paired-check guidance.
Revisit through L1's evidence→proposal→cost/consumer→versioned change protocol.
