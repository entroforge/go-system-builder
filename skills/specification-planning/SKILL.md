---
name: specification-planning
description: Use when designing architecture, final UI design packages, contracts, and task decomposition in the planning phase
category: methodology
version: 2.2.2
---
# Specification Planning

## Authority
The locked REQ is the baseline. Design, contracts, and tasks must trace back to it. Runtime authority lives in `docs/control/loop-definition.json`; stage contracts live in `docs/control/agent-protocol.md`; Project Design Foundation lives in `docs/rules/design-foundation.md`; the method summary is inlined below.

## Entry Conditions
- The Loop is in `planning` (phases advance design → contracts → tasks via PTR-PLAN-01/02 and TR-002).
- The current responsibility supplies the readable locked REQ. Original
  preparation and consumers verify its baseline; authors do not look up,
  compute or copy a Runtime fingerprint before starting professional work.

## Required Inputs

Use the rows relevant to the current stage and supplied responsibility. The
source inventory is not a requirement to read every file or reauthor upstream
materials; the stage routing below identifies the necessary entry.

| Input | Path / field | Why |
|:---|:---|:--|
| Locked REQ | exact path in the supplied current view/brief | source of acceptance criteria and scope; no Runtime metadata lookup required |
| Architecture batch | actual `docs/architecture/ARCHITECTURE-*.md` paths and statuses supplied by the current responsibility | reuse existing decisions and resolve the exact registration gaps; a report's one architecture path does not define the whole batch |
| Existing design | `docs/design/**` | reuse and conflict detection |
| Design Foundation | `docs/design/DESIGN.md` **§0 Next-agent card first**, then `design-language.md` and the Surface row if needed | inherit interaction/visual constraints when UI impact is `changed`; do not invent brand from a library hex; Foundation does not replace architecture |
| Derivation template | `docs/design/derivation/DERIVATION-template.md` | write `docs/design/derivation/REQ-<id>.md` before expanding HTML |
| Module current truth | UI path: `docs/design/prototypes/<module>/{index.html, stories.md, flows.md, scenario-model.json, cross-matrix.json, cases.json, scenario-coverage.json, fixture-contract.json, *.html}`; service-only path in the same directory: `scenario-model.json`, `fixture-contract.json`, `cross-matrix.json`, `stories.md`, `flows.md`, generated `cases.json` and `scenario-coverage.json` | current module behavior package; HTML/PATH/browser spec are required only for applicable browser cases |
| Rules | `docs/rules/*.md` | naming, security, api-design, state-machine, design-foundation constraints |
| Loop Definition | `docs/control/loop-definition.json` | planning exit transition and executable guards |

## Procedure — route by current responsibility

Use the exact current view/answer and its supplied inputs. In `planning.design`
(S2), read the locked REQ, actual architecture batch, applicable module packages
and conditional Foundation materials, then use steps 0–9 below. In
`planning.contracts` (S3), read the frozen REQ/S2 view, existing boundaries and
consumers, [API contracts](../api-contracts/SKILL.md),
[shared-model rules](../../docs/rules/shared-model-contracts.md) and the supplied
four contract templates; start at step 10. In `planning.tasks` (S4), read the
frozen REQ/S3 sources and supplied TASK/index templates; start at steps 11–12.
S3/S4 consume established design; they do not repeat S2's Foundation derivation,
scenario generation or architecture lock. Read additional rules when the actual
operation or diagnostic requires them, rather than all rule files up front.

### S2 — dual-track convergence

**Step 0 — route S2 by UI impact**: read the bound
REQ's top `UI impact` field. `unknown` → resolve the missing direction through
the original REQ clarification/amendment gateway before a design pass; do not
silently edit the locked baseline (the `ui_impact_resolved` guard blocks
PTR-PLAN-01). `none` → skip
Foundation derivation and UI assets (page HTML, `index.html`, PATH, and browser
specs). If any AC targets an FR, still run the behavior steps and build/update
the module's 7-file service package: `scenario-model.json`,
`fixture-contract.json`, `cross-matrix.json`, `stories.md`, `flows.md`, and the
generated `cases.json` and `scenario-coverage.json`. Use an API caller/worker as
the story role, an F-NNN operation flow without PATH, and
`browser_required:false` on each service branch. Only when all ACs have an
endorsed NFR or §A4 target and no module behavior changed may the scenario
package work be skipped. Also update the same package when existing service
behavior truth changes; if the bound REQ has no FR to source that change,
resolve it through REQ amendment before locking the package. `changed` → run
the full UI and behavior flow below.
Do not treat `none` as “no design”: locked ARCHITECTURE remains required, and
`none` never removes an FR-targeted CASE.

**Step 0.5 — consume Project Design Foundation (`changed` only)**: read
`docs/design/DESIGN.md` **§0 Next-agent card** first, then the current `SUR-*` diff
named by the REQ, then `design-language.md` only if the card cannot answer
Must not. The card must already answer action role (no hex), value **and**
status-tag color, and which commitments are buttons vs sentences. Do not treat
a hex or library Primary from a prior construction note as the brand. Do not
open `design-language.md` on Core+thin when the card already answers Must not.
If Foundation is `local` (investment tier `local`) → write
`docs/design/derivation/REQ-<id>.md` with `Foundation: local` and module-local
decisions, without publishing a project Foundation. If status is `provisional`
/ `draft` / `in-review` / `superseded` or confirmation is PENDING, it is not a lock (only `published` with dates recorded is). If Foundation is missing,
draft, in-review, provisional, superseded, lacks §0, or cannot cover this surface, stop and load
`skills/design-foundation` — do not batch-generate pages. inherit must not
add a new information posture (KPI dashboard) without `extend`. Otherwise write
`docs/design/derivation/REQ-<id>.md` as a **referenced-ID consumption list**:
active `LAW/ANTI/INV` by stable ID + opening `GR-*`, `Must not` by source ID,
`Bindings` by `ROLE/PATTERN`/component path, `Proof` by `PROOF-*` with
resolvable path, one macro composition, one stress state; `inherit`/`extend`/
`exception` posture (extend → promotion candidate note; exception →
`docs/design/decisions/EX-*.md`). One macro composition and one stress HTML
state must exist before the remaining module pages. If any required ID, version
(`DESIGN.md@version`, `SUR-*@version`), or Proof path cannot be resolved,
stop at semantic fix — do not invent a skin. See
`docs/rules/design-foundation.md`. This step is a semantic duty, not a
PTR-PLAN-01 predicate. It does not replace the architecture track. The cold-start
handoff packet is only `§0 + SUR-* diff + current Derivation` (together
≤120 lines / ≤12 KB); later implementers read that packet, not the full thesis.

Main coordinates the two S2 tracks and their two convergences; the tracks are
not a default split into two independent agents. Main may delegate a bounded
investigation while retaining the shared design context and integration.
"Free ordering" means either track may start first — inside Convergence 1 there is a hard order: **stories.md
must exist before rules/branches** (every branch's `story_refs` cites an
S-NNN; writing rules first means citing stories that do not exist yet).
Architecture constrains journeys; when a story challenges the architecture,
record the trade-off in the ADR. Reuse an applicable existing human direction;
bring a genuinely unresolved direction or requirement change to its human gateway.

**Track system (architecture → facts):**
1. Draft the architecture: components, data model, state machines, data
   flow. Record decisions as ADRs (decision/risk/consequence/alternative)
   under `docs/design/decisions/`. Resolve ordinary engineering choices within
   the authorized requirement. When a direction needs a human decision, present
   that ADR and its relevant AC-bridge N/A basis together; preserve an existing
   applicable decision instead of requiring another sign-off for every S2 run.
2. Land facts partitions from the architecture vocabulary.

**Track user (REQ §A → behavior-role stories):**
3. Write stories (S-NNN citing REQ-id) from the REQ's §A intent only. For a
   service-only package, express the API caller/worker's intent without
   inventing a screen or UI journey.
   When evolving an existing module, read its complete current package
   first — new stories must not duplicate or contradict existing S-NNN.
   Stories seed behavior completeness; they do not depend on the
   architecture track.

**Convergence 1 (behavioral completeness):**
4. Draft rules/branches (oracle written with the branch — a branch's
   polarity forces its outcome thinking) by crossing facts × REQ demand
   points (FR) × stories, rejection paths included. `source_refs` cite
   `REQ-<id>/FR-<id>` so the AC bridge can resolve. The hunt's carrier is
   `cross-matrix.json`: every meaningful cell names its covering branch or
   records a no-branch reason — silence is not N/A. Machine floors (enforced
   by `scenario validate` / generate):
   - every declared fact and every story must appear in at least one cell
     (per-fact and per-story floors; the fact×story combinations themselves
     are your hunting judgment, **not** a cartesian-product requirement);
   - a branch cell must be a branch whose rule actually cites the cell's
     `REQ-<id>/FR-<id>` in `source_refs` (the matrix joins the model, it
     does not assert alongside it). Preserve each original `req_ref` across
     requirements in the complete current module. Only current-REQ references
     join the current FR table; the AC bridge checks this REQ's obligations
     separately. An older REQ with the same FR number cannot supply them.
     A REQ-level matrix cell may join that REQ's actual FR-level rule source;
     do not rewrite sources or remove old required branches to fit a new bind;
   - a `no_branch_reason` is a rationale: name the why — at least 8
     characters including a letter (a bare "不需要"/"." is rejected;
     free-word escapes are not endorsed N/A).
5. Immediately run `.claude/bin/loop-harness scenario bridge --root .` — the
   AC source check (AC→FR→BR) needs no generated outputs; fix gaps now, not
   at close.

**Fixtures:**
6. After branches settle, write `fixture-contract.json` (synthetic data +
   cleanup) — data needs are branch-certain by now.

**Convergence 2 (operational flow):**
7. Write an F-NNN operation flow for every behavior path. Service-only
   branches use that flow without PATH, HTML, or Playwright and set
   `browser_required:false`. UI branches that require browser verification
   bind a PATH here and include the applicable prototype pages (4-field
   header); `browser_required:true` requires at least one PATH.

**Depth self-review, then close:**
8. Before closing, attack your own package in three roles — implementer:
   "which oracle can't I build or can't distinguish a wrong implementation
   by?"; e2e-tester: "which negative case can't I evidence across the
   seven oracle dimensions? (visible, terminal_state, persisted_effects,
   forbidden_side_effects — plus, for negative branches, rejection,
   expected_state, recovery)"; maintainer: "which rule will clash with
   module evolution?" Record the conclusion as one paragraph in the existing
   ADR's `## Depth Self-Review` section when an ADR is applicable, otherwise in
   the actual architecture material. Do not create an ADR, another report or
   an answer field just to hold this review. These are three views of one
   semantic review, not three reports. Reference generated counts and
   coverage instead of copying statistics into multiple documents; do not add
   direct-AC or count proofs. Profile positive/negative ratios are risk
   references, not quotas; retain every real required allow/reject branch.
9. When a module behavior package is required, close it with
   `.claude/bin/loop-harness scenario generate --module <module> --root .`
   then `scenario validate --module <module> --root .`; this validates the
   source package and generated outputs. Run `scenario bridge --root .` for
   the REQ-side AC check. The bridge requires every AC to reach a CASE or
   carry an endorsed N/A: a declared NFR id or an explicit §A4 negative-space
   pointer — §A4 is the REQ's "明确不做" table; free text is rejected as a
   silent removal from the verification denominator. If no module behavior
   package is needed because all ACs have endorsed N/A targets, run the
   bridge and omit only the package commands. Close the architecture side by
   flipping `ARCHITECTURE-<id>.md`'s top `状态` to `locked`, then close S2
   through the packet-enabled or packet-off legacy path in the
   Planning Gate Reception section below. The original design judgment and
   PTR-PLAN-01 gate remain authoritative.

**S3/S4 steps:**
10. For S3, decide FE, BE and SYNC applicability independently from the actual
    delivery. Only where a shared operation exists, converge its applicable
    data model and SYNC behavior before finalizing FE/BE responsibilities.
    FE-only local behavior or BE-only internal work may need no SYNC; SYNC may
    exist without separate FE/BE contracts. With no shared data, declare
    `Shared model policy: none` and its concrete reason; create no Schema or
    example just to fill a template. Use the applicable templates under
    `docs/dev/contracts/` (CONTRACTS / BE / FE / SYNC). Each contract traces to
    the bound REQ and applicable current-truth inputs. The CONTRACTS index
    coverage matrix is for this REQ's clause universe — one `{id} §{n}` cell
    per clause, matching the clause number declared by the target contract.
    S3 freezes the locked REQ, registered S2 architectures, the complete file
    sets of all non-template modules under `docs/design/prototypes/`, accepted
    ADRs associated with the bound REQ, and dependencies reached through actual
    supported source references. This project-wide module scope matches the
    original bridge; it is not limited to modules referenced by this REQ or its
    architecture. It does not expand this requirement's design changes or ask
    you to redesign unrelated modules. The complete source inventory supports
    traceability; read the slices relevant to current operations and follow actual
    dependencies, without recursively reading every listed module. Do not delete historical materials or
    split the REQ to reduce that source set. S3 does not use its own
    not-yet-authored CONTRACTS index as an issuance prerequisite. The Hook
    supplies the exact answer path and known or candidate output paths/templates.
    Create one index and the
    complete applicable contract set for the bound REQ. S3-created shared
    models/examples are outputs: saving the supplied answer runs the original
    shared-model checker, which discovers their dependencies and records them
    in the existing evidence subjects. Refine or create models within the established S2 meaning in S3. Do not
    modify frozen S2 models or their source membership; a required change to
    those inputs returns through formal design rework, and a change to REQ
    semantics uses requirement amendment. Set finalized contracts to `locked`, fill the existing professional
    answer. The original consumer reads the declared source view: commit Git-sourced professional outputs to
    the exact development branch named by the Hook before saving that answer.
    S3 outputs can change during the current entry; REQ/S2 inputs remain frozen.
    Save the completed answer. The gate evaluates the full current set; PTR-PLAN-02 performs the original
    registration. Do not hand-create evidence or copy machine metadata. Use
    `contracts check` and `scenario bridge` freely for professional self-review
    or diagnosis; successful manual checks are not an extra submission step
    or a substitute for the original consumer checks. See [S3](../../docs/control/agent-protocol.md#s3)
    for the current packet workflow; packet-off legacy handling is below.
11. Decompose into TASKs: each TASK binds one primary contract, declares
    its Delivered Clauses (§3) and Module Impact (§3.1), has a Closing
    Contract (§7 observable behavior, own checks, scope and handoff conditions; use as many assertions as needed), and obeys single-responsibility.
    Never hand-copy fingerprints or versions — runtime documents[] owns
    them; §2 keeps read order only.
    **拆分纪律（builder 视角）**：用一句话给出交付目标，按独立验收、责任和真实变更耦合拆分；只有实际依赖共享实现产物的下游任务才声明 foundation TASK 依赖。每个任务的 §2 清单引用必要条款切片，并标明 required/conditional/optional 与条件。结合共享读集、同文件写入、依赖和执行中的 compact/返工观察判断粒度；字节数、文件数或一次 compact 不能自动决定拆分。调整已批准批次时走原规划/审查/失效路径，并保留合同覆盖与可执行依赖。
    Reference actual locked Rules, architecture/ADR sections and applicable
    professional defaults directly in the TASK, with their concrete scope and
    exceptions. Do not create a project-conventions index, copy registered source versions or
    hashes, or make a new business decision merely to obtain analysis advice.
    A previously recorded complete project-conventions index may be read by
    the original resolver and frozen with the task context. Its source and
    drift checks remain strict; it is not a normal S4 authoring obligation.
    Absence of that optional historical index does not block ordinary authorized
    engineering choices or change TASK coverage and gates. The current REQ dispatch
    index remains required; it is a different planning deliverable.
    Plan concrete implementation, test, wiring and saved professional-evidence
    paths within the TASK scope, and explain applicable operations and checks
    without a tool whitelist. Describe the completion result and downstream
    verification semantically; actual later responsibilities supply their original
    Plan/Result paths and schemas. Legacy REV/QA/E2E report paths apply only to
    responsibilities that actually use them, without a new separate report duty.
12. Use numeric TASK IDs such as `TASK-009` or `TASK-009-01`; keep module/service
    names in the objective. For S4, complete the TASK batch and current REQ index at the exact paths and
    templates supplied by the Hook. Mark each TASK document `complete` or
    `cancelled`; cancelled TASKs exit active dispatch and clause coverage but remain
    in the complete delivery batch and answer.task_paths. Resolve any resulting
    coverage gap. Every TASK declares the actual Source REQ refs, including support
    and cancelled tasks. These document values do not change a work responsibility's
    status. Commit declared Git-sourced outputs to the Hook-named branch, fill
    the existing S4 answer, and save it once. The original consumer runs
    `tasks check`, projects planning evidence, and evaluates the existing gate
    and `TR-002`. Do not create an evidence envelope or recover Runtime state
    as ordinary authoring steps. `tasks check` and `tasks check --json` remain
    available for autonomous self-review, diagnosis, granularity and
    source-backed advisories; they are not additional submission procedures. Read
    concrete observations against the actual responsibility; byte counts are
    not token measurements or split thresholds. Unresolved assertions still
    require a professional oracle, and the analysis never executes them.
    `behavior_examples` only projects CASE IDs explicitly named in that TASK's
    Manifest Clauses against its current `cases.json`. It validates the original
    scenario source package and preserves inputs, oracle, fixture and source
    hashes; Mode and unknown conditions remain visible. Resolve `unresolved`
    questions through the original contract/design responsibility. A source
    example is not a product test result or approval of a new requirement.
    Inspect `task_conventions` with its source mode: use only proved
    `declared_default` entries and their exceptions; resolve candidate, stale,
    conflicting or migrating entries through the original owner. S4 authoring
    has no automatic document-registration proof. Absent/no matching registry
    permits ordinary authorized engineering choices, and never changes the
    original TASK coverage, gates or permission.
13. If document verification returns `document_fix_required` (`TR-004`),
    repair the affected documents. Re-open an architecture or UI decision
    only when verification evidence shows that the decision itself is
    invalid; otherwise keep rework bounded to the flagged contract or TASK.

## Planning Gate Reception（S2/S3/S4）

For S2, complete the actual applicable architecture/UI/service/ADR work. The
original responsibility-start/binding completion supplies the exact current
view/answer paths, full duties and conditional material pointers. It also shows
the architecture registration batch with its actual paths and unlocked items,
and the project-wide module source paths S3 will consume. `architecture_path`
identifies the architecture reviewed by this report; other architectures in
the supplied registration batch can also affect the outlet, including legacy
documents without explicit REQ ownership. Resolve those actual document gaps.
Keep an unused alternative as an explicitly unadopted discussion in the actual
architecture or ADR, rather than a separate active architecture awaiting
registration; do not label it `locked` or invent an ownership change just to
pass the outlet. Fill only the existing `architecture_path` and `conclusion`;
do not copy the system's
batch inventory into new answer fields. Empty conclusion is draft. Before
saving a final pass, commit the locked architecture, accepted applicable ADRs,
and other formal S2 inputs to the development branch selected by the original
S1 binding. S3 reads that committed source and checks the current files against
it; the Hook does not commit for you. Do not include unrelated changes.
Normal save records the professional report and runs its original outlet.
If S2 reception succeeded but its outlet is unfinished, repair the named
material gap and follow the original continuation. When that continuation
explicitly uses the same answer save, it resumes the outlet without registering
the report again; do not repeatedly resave unchanged work just to trigger
progress. Completed old-stage replay is read only. Ordinary Pre/Stop
only remind from current facts, without preparation or source reproof. Missing
actual material is reported precisely; pass alone cannot replace the design.
Main/Architect labels, packet equality and source hashes do not qualify authors.
Normal authoring needs no metadata copying, envelope creation, issue/consume,
or recovery command. For an actual technical failure, use the precise original
recovery named by the feedback. A receipt, PTR-PLAN-01 and prepared S3 inputs are
separate outcomes: fix an unmet lock condition in the actual document, or a
missing committed source on the bound branch, rather than rewriting an accepted
answer. A lost issued file is a system preparation gap, not a request to invent
an answer path or its metadata.

S3/S4 retain their existing questionnaires and professional duties. Read the
supplied exact view/answer paths and concrete missing items, then save the
professional answer through their original consumer; do not guess directories
or duplicate an envelope. The frozen packet-off recovery boundary below is not an
S2 normal authoring obligation or a producer-name whitelist for S2.

For S3/S4, tool access uses the exact supplied absolute filesystem paths;
answer fields `index_path`, `contract_paths` and `task_paths` contain
repository-relative values. For example, read/write the same contracts file
under its supplied absolute path with a tool, but put its repository-relative
path (`docs/dev/contracts/CONTRACTS-{id}.md`) in `index_path`. Save the
questionnaire at its exact supplied absolute answer path. Markdown links remain
relative to the containing document; do not convert them to answer-field
root-relative paths.

Professional S3/S4 rework is separate from technical continuation. Within the
same bound REQ, use
`.claude/bin/loop-harness runtime stage-answer rework --target design --reason "<concrete design correction>" --root . --agent Main`
when frozen S2 inputs must change. Use
`.claude/bin/loop-harness runtime stage-answer rework --target contracts --reason "<concrete contract correction>" --root . --agent Main`
when accepted contracts must change while S2 stays valid. While S4 is still current, use
`.claude/bin/loop-harness runtime stage-answer rework --target tasks --reason "<concrete TASK/index correction>" --root . --agent Main`
when accepted TASKs or the dispatch plan need professional correction and the
frozen REQ/S2/S3 inputs remain valid. Keep correct upstream work; a tasks return
does not refresh drifted upstream sources. After entry into S5, follow the
independent review and original document-rework route. These explicit routes
preserve the accepted history, invalidate affected downstream planning, and
supply the new current view/answer paths. If preparation fails after the
return is recorded, follow the supplied `runtime stage-answer issue` command.
No packet/entry ID or authored evidence envelope is required. Current
unreceived outputs are ordinary professional corrections, followed by required
Git delivery and saving the original answer, even if marked `locked`.
`issue` repairs missing issuance/preparation; `recover` resumes an accepted
pending outlet. Neither changes an accepted professional judgment or authorizes
editing frozen sources. `revise` is not an S3/S4 professional rework route.
REQ semantic changes retain the actual human amendment boundary; ordinary
contract gaps and compatibility choices within authorization remain professional work.
Any questionnaire reminder about human decisions means an actual unresolved
REQ amendment or decision outside existing authorization. It does not require
a fresh approval for every ADR, compatibility choice, N/A rationale or S3/S4 run;
reuse applicable existing decisions.

For S4, the Hook also supplies the current REQ index path, TASK candidate paths
and templates, frozen source inputs, and the bound Git branch. Main decides the
task breakdown and professional contents, commits the declared outputs to that
branch, fills the existing answer once, and lets the original consumer perform
the checks and `TR-002` registration. Use `tasks check` for autonomous self-review or diagnosis when useful; it is
not a required duplicate submission check. If S3 reception has succeeded but its gate remains pending, preserve the
accepted answer, repair the named gap and execute the complete original
`runtime stage-answer recover` command supplied by feedback. For accepted S4 work whose TR-002 outlet remains pending, repair the concrete
gap and execute the complete original `runtime stage-answer recover` command
supplied by feedback; ordinary observations do not continue that outlet. Do not refill or resave an accepted answer. If transition has already committed but next-stage preparation failed, repair
its named source gap and execute the complete original `runtime stage-answer issue` command supplied by feedback; `recover` continues a pending gate outlet.
Ordinary reads or
arbitrary material saves do not promise followup recovery. Before reception,
finish required Git delivery and save the original answer normally. `complete/cancelled` apply to TASK documents, not the shared
work-responsibility states.

### Frozen packet-off planning recovery

An existing packet-off Runtime may retain its original planning envelopes and
explicit evidence interface. This is maintenance of an already recorded
compatibility flow, not an alternate normal authoring procedure. Preserve the
original identity, source version, timestamp and registered bytes. Do not ask a
new S2/S3/S4 author to assemble an envelope, copy Runtime/SHA values, choose an
internal evidence ID or invent an `-r2` revision to repair preparation.

When such a frozen record needs recovery, follow the actual original diagnostic
and [runtime recovery reference](../runtime-recovery/references/runtime-recovery-reference.md).
The system/maintainer resolves that recorded format through its compatible
reader and original Writer. A current typed questionnaire is never bypassed;
missing typed preparation is a system gap, not a request to fall back to a
hand-filled legacy DTO.

## Outputs
- Locked architecture at `docs/architecture/ARCHITECTURE-<id>.md`; applicable
  ADR records under `docs/design/decisions/`, including the depth self-review
  paragraph and endorsed-N/A basis.
- Design Derivation Note at `docs/design/derivation/REQ-<id>.md` when UI
  impact is `changed`.
- Locked current module package when needed: five authored behavior sources
  and the two original generated outputs, plus actual applicable UI assets
  when UI changes. A service behavior package has no HTML, PATH, or browser spec;
  its CASEs remain in the S2→S7 denominator. Module packages are current truth,
  not per-REQ copies.
- FE/BE/SYNC contracts with REQ and design traceability.
- TASK batch with Closing Contracts and single-responsibility assignments.
- Current planning evidence required by `TR-002`.

## Exit Conditions
- The planning checkpoint is committed and the Loop transitioned to `document_verification`.
- Main continues with S5 orchestration per `docs/control/agent-protocol.md #s5`: dispatch two independent document-verifier subagents using their supplied responsibilities. This planning method ends at the handoff; the native review tasks name applicable Triggered Deep-Dives from the document-verification skill.

## Stop Conditions
Bring the affected decision to the existing human gateway when:
- A core acceptance criterion cannot be resolved from the approved REQ and existing decisions, or the required solution changes the REQ's scope or acceptance semantics.
- A design cannot comply with an immutable rule without a requirement change or a choice outside the existing authorization.
- A product or architecture direction genuinely requires a human choice that has not already been made; reuse an applicable existing decision.

Continue professional repair within the authorized requirement: complete missing
AC-to-CASE coverage, repair contract-to-REQ traceability, and reconcile stories,
flows, prototypes and architecture. An ADR change or an incomplete trace alone
does not require another approval. If repair reveals a real requirement or
product-semantics change, present that specific change and its impact to the
gateway. Never substitute free-text N/A or a passing answer for missing work.
After S3 freezes upstream sources, use the original specification rework route
for an S2 correction; do not silently edit the frozen inputs.

## Non-Goals
- Product implementation belongs to the Builder Agent; S2 still authors its applicable HTML prototypes and scenario sources.
- Independent contract verification belongs to `document-verification`; planning still performs its own required checks and self-review.
- Do not create the Agent Team (that is `team-planning`).

## Inlined Methodology

Planning is an executable Loop phase machine (design → contracts →
tasks via PTR-PLAN-01/02), not a document-production state machine.
Architecture, optional UI design, contracts, and TASKs are work products
within those phases; they do not each require a runtime transition.
`TR-002 planning_ready` is the only planning exit and evaluates the
current planning package (registered contracts + the complete TASK batch). `TR-004
document_fix_required` returns failed documents to planning for
evidence-bounded rework. Runtime 1.3 stores the shared five-state responsibility
status on its Agent and TASK work-state fields. Legacy Runtime 1.1/1.2 TASK
lifecycle values (`candidate -> reviewed -> locked -> in_progress -> review ->
done`) remain only in their original compatibility format and are not rewritten
in place. TASK document `Status: complete` means only that the planning artifact
is finished. Agents follow the current Runtime/Hook projection and never author
a generic status. Contracts and TASKs must trace back to the locked REQ. Invalid
or guard-failing events do not
change state, execute no side effect, and record a rejected event.
Idempotency uses CAS revision checks; one committed transition per runtime
revision.

## Executable acceptance checks before locking TASKs

Before locking a check command, verify its selected test inventory, browser
support policy, runtime ownership, required dependencies, and known baseline
failures. A command named cross-browser is not evidence that the current
requirement is selected by those browser projects. Reuse unchanged baseline
receipts only when their commit/configuration and declared scope apply; do not
run the entire suite at every planning step. Unknown baseline health remains
unknown, never PASS. Browser coverage changes use the existing specification
change route, not a new approval workflow.

Manual debugging and integration must use the same project runner/environment
profile for comparable evidence. Do not replace a required custom reporter,
filter away failed tests, or hide the command exit code behind `tail`/`tee`.
Resource controls may limit concurrency without reducing test coverage.

## Production-path ownership before S4 close

For each user-visible or externally consumed capability, trace the real caller through the applicable UI mount, API wrapper, registered route, authorization, service and response projection. Reuse the existing TASK/contract traceability sections: name the owning TASK for each required connection, including changes to shared types. A service implementation alone does not establish a production consumer. For backend-only work start at its actual external caller; do not invent a UI obligation.

Cross-check the files needed to implement each connection against its planned write scope. Missing ownership or contradictory scope is unfinished planning; correct it before locking/dispatch, not by copying business rules into the allowed component. Retain one canonical authority for business decisions; client-side validation requires explicit compatibility and differential checks.

For critical capabilities name at least one check crossing the actual registered boundary and one counterexample (such as reverted wiring) that would make it fail. Counts, test names, a mock returning the desired response, and a test proving an entry is absent are not delivery evidence. Record coverage gaps explicitly; do not defer a TASK's own delivery obligations to S7. This is a semantic review obligation, not a claim that the current machine gate proves end-to-end correctness.


For new batches apply [shared-model contracts in the factory](../../docs/rules/shared-model-contracts.md) (after installation: [project rule](../../docs/rules/shared-model-contracts.md)): explicit policy, native source references, validated examples, committed design closure and role-specific TASK links. Do not create a registry or copy schemas per REQ. Independent generation may run in parallel; only actual shared implementation artifacts require foundation TASK dependencies.

## Overall dispatch plan (waves-v1)

Follow [factory dispatch rules](../../docs/rules/dispatch-plan.md), or after installation
[project dispatch rules](../../docs/rules/dispatch-plan.md). S4 delivers the REQ's
index as an ordered wave checklist; S5 reviews and freezes it with TASKs. S6 reads
that plan and `s6 status --capacity <actual total slots>` before dispatch, then
recomputes after integration or capacity release. Start every compatible ready
TASK within actual capacity; do not wait for unrelated tasks in an earlier wave.
Only verified integration into the root development branch releases consumers.
Never write runtime progress back into frozen plans or TASKs.
