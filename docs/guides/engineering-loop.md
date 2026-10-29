# Vibe Coding Loop Engineering Template

This repository is a reusable Claude Code documentation and Harness template.
Project-instance files are created in the target repository.

REQ-002 is a self-evolution: this Loop Engineering method is used to refactor
the documentation and Harness that the method itself runs on.

---

## 1. What Is A Loop

A **Loop** binds exactly one human-locked REQ and drives it, as a state machine,
from the locked requirement to the human release handoff. It slices the path
into a finite set of machine-checkable states. Every state admits only the
transitions the Loop Definition allows, subject to their declared domain
conditions. Evidence can become invalid. Human decisions remain in their
original application; Hook feedback names missing work and its next owner
without turning ordinary tools into approval gates.

The goal of Loop Engineering is not to let the model work without constraint. It
is to let Claude Code, inside one human-locked requirement scope, continuously
complete development, verification, repair and re-review under a state machine,
document specifications, professional methods and automatic gates — and finally
**stop at the human release gate**. `awaiting_human_release` is a non-terminal
human decision gateway; an explicit approval reaches the `release_authorized`
terminal, while `aborted` is the separate terminal/blocked outcome.

A Loop never modifies the REQ, never squash-merges, and never formally releases.
This is the first principle of the design.

### Control model: humans hold both ends, the Loop automates the middle

The whole loop has one simple control shape:

```mermaid
graph LR
    H1["**Human**<br/>locks REQ"]:::human
    M["**Loop automation**<br/>prototype gate → design → contracts<br/>→ TASK → native Agent dispatch<br/>→ specialized Builder → Verifier → QA → E2E Tester<br/>→ BUG repair → full re-review<br/>→ ACC → Release Architecture Audit"]:::auto
    G["**Human**<br/>S11 decision gateway"]:::human
    H2["**Human**<br/>squash merge + formal release"]:::human
    H1 ==> M ==> G ==> H2
    classDef human fill:#fce4ec,stroke:#c2185b
    classDef auto fill:#e8f5e9,stroke:#2e7d32
```

Humans decide, and only humans decide:

- REQ lock and any REQ change
- squash merge to `master/main` and formal release

The Loop may never automatically create or lock a requirement baseline, change
REQ goal, scope, priority or acceptance criteria, or publish to `master/main`.

### Three-layer architecture (REQ-003)

The framework is built as three nested layers, with a support plane underneath:

```text
Layer 3  Event Control + Guard    docs/control/hook-policy.json + Claude Code Hooks + Runtime Milestone
Layer 2  Wake-up Recovery         .claude/loop.md (consumed by Claude /loop)
Layer 1  Main-session Driver      AGENTS.md + docs/control/agent-protocol.md
```

Layer 1 is the driver and runs the entire middle on its own. Layer 2 is a
scheduled fallback. Layer 3 is the active event control plane: lifecycle Hooks
reconcile the Runtime, reuse the `status/next` projection, persist the
Milestone, emit positive `LOOP RECOVERY` guidance, run delegation preflight,
and direct same-teammate/worktree recovery; guard decisions still enforce
deterministic boundaries. **Layer 2 is a fallback wake-up; Layer 3 is the
event-driven controller, and neither is a second lifecycle.**

### Engineering Loop binding is independent of Claude `/loop`

REQ-003 decouples the two lifetimes:

- **Engineering Loop binding** is a Harness operation:

  ```bash
  .claude/bin/loop-harness req bind --approved-by <human identity>
  ```

  `--req` is optional: with exactly one bindable REQ (`req list` shows the
  pool) it is auto-discovered. It archives the inactive bootstrap runtime,
  creates the Runtime Bookmark (machine cursor `planning/design`) at revision
  `0` with an empty active journal, and records `event=req_bound` in the
  binding receipt together with the source runtime hashes. It does not start
  any schedule.

- **Claude `/loop`** is a Claude Code built-in scheduler that delivers the
  project's `.claude/loop.md` Wake-up Prompt on a cadence. It does not bind
  a REQ and does not authorize release. The independent REQ binding is
  sufficient for engineering execution, so no separate `/goal` is required.

The user may bind a REQ and never run `/loop`; the main session still drives
via Layer 1. The user may also run `/loop` against an inactive Runtime; it
surfaces `NO_BOUND_REQ` and stops.

The full automated middle segment is:

```text
human locks REQ
-> .claude/bin/loop-harness req bind
-> UI Design Package Gate (if UI impact)
-> design and contracts
-> TASK decomposition
-> Main supplies exact professional task references
-> Claude Code native Agent dispatch
-> specialized Builder development
-> Delivery Verifier + QA + E2E Tester
-> finding investigation and canonical BUG reports
-> BUG repair and targeted re-verification
-> complete re-review round (clean round)
-> ACC
-> Release Architecture Audit
-> stop at human release gate
```

After binding, normal S0→S11 continuation is Hook-driven. The Agent does not
need to call `.claude/bin/loop-harness status`/`next` on every turn: the Hook packet and
Runtime Milestone carry the canonical cursor. Manual CLI is reserved for
initialization/binding, integrity reconcile, rollback/rollover, and the release
Gateway.

---

## 2. Four Core Abstractions

These four must not be confused. Each has one single source of truth, one owner,
and one mutability rule.

| Abstraction | Single source | What it answers | Mutability | Who can change it |
|:---|:---|:---|:---|:---|
| **Locked REQ** | `docs/requirements/REQ-xxx.md` | what is required | only via human change control | **human only** |
| **Loop Definition** | `docs/control/loop-definition.json` | which transition is legal in the current state | versioned design doc | design phase (human) |
| **Loop Runtime** | `.claude/loop-state.json` | current facts + resumable Milestone | Writer-owned current-state commit sequence; lifecycle transitions use the Transition Engine and Milestone refresh uses the Runtime Store | **single Runtime control plane** |
| **Evidence** | `docs/reports/**/*.{md,json}` | that something was done and is still valid | can be marked invalid by impact-analysis | the professional responsibility that produced it |

Chat, `docs/project.yaml`, `docs/project-map.md`, TASK bodies and REQ Todo blocks are **not**
runtime authorities. They are human summaries.

```mermaid
graph TB
    REQ["Locked REQ<br/>human-controlled baseline<br/>Loop cannot change"]
    LOOP["Loop Definition<br/>machine-readable state machine<br/>defines what is legal"]
    RUNTIME["Loop Runtime<br/>.claude/loop-state.json<br/>defines current facts"]
    EVID["Evidence<br/>REV/QA/BUG/ACC<br/>proves work done and valid"]
    REQ -.->|bound 1:1| LOOP
    LOOP -.->|instantiated| RUNTIME
    RUNTIME -.->|references, can invalidate| EVID
```

---

## 3. Layered Authority Sources

The second principle is single source of truth plus progressive disclosure.
Each layer answers exactly one question and does not cross its boundary.

| Layer | File | Answers | Runtime authority |
|:---|:---|:---|:---|
| Static constitution | root `AGENTS.md` | project boundaries and where to route | entry router |
| State machine | `docs/control/loop-definition.json` | which transition is legal | **legality** |
| Current runtime | `.claude/loop-state.json` | what the current facts are | **current state** |
| Methodology + practices | `.claude/skills/` | which method applies to an event | advisory |
| Agent definitions | `.claude/agents/` | who a role is and its maximum capability | boundary |
| Hook listener | Hook events | current questionnaire path, missing duties and next action | **progress observation** |
| Loop Controller / Milestone | Hook events + `milestone` field in `.claude/loop-state.json` | where the Agent is and what it should do next | **recovery guidance** |
| Harness CLI | `.claude/bin/loop-harness` | who executes validation, transition, audit | **mechanism** |

A Skill is advisory, not authoritative. A Skill may instruct an Agent to request
a transition; it may not declare that the transition occurred. Only the
Orchestrator plus the runtime transition engine can do that.

```mermaid
graph TB
    Protocol["AGENTS.md<br/>static boundaries + routing"]:::static
    Definition["Loop Definition<br/>which transition is legal"]:::definition
    Runtime["Loop Runtime<br/>what is the current fact"]:::runtime
    Skills["Skills<br/>which method applies"]:::skill
    Agents["Agent Definitions<br/>who is this role"]:::agent
    Policy["Hook listener<br/>questionnaire path + missing duties"]:::hook
    Harness["Harness CLI<br/>who executes"]:::harness
    Protocol --> Definition --> Runtime
    Protocol --> Skills
    Protocol --> Agents
    Protocol --> Policy
    Runtime -.->|current progress| Policy
    Policy -.->|invokes| Harness
    Skills -.->|routed into| Agents
    Definition -.->|referenced by| Harness
    classDef static fill:#fce4ec
    classDef definition fill:#e3f2fd
    classDef runtime fill:#e8f5e9
    classDef skill fill:#f3e5f5
    classDef agent fill:#fff3e0
    classDef hook fill:#ffebee
    classDef harness fill:#eceff1
```

---

## 4. Component Constitution

Where §3 maps each layer to its source of truth, this table fixes each
component's **reason for existence**. One component owns exactly one concern;
no concern is split across components, and no component absorbs another's
concern.

| Component | Owns | Does not own |
|:---|:---|:---|
| **Documents** (REQ, design, contracts, tasks, reports) | what to do | how to transition, who does it |
| **Loop Definition** | which path is allowed | current facts, methodology |
| **Loop Runtime** | where we are now | what is allowed, methodology |
| **Methodology + Best-practice Skills** | how to do it well | state authority, role identity |
| **Agent Definition** | who does it (identity, tools, permissions) | per-instance scope, methodology |
| **Task Prompt** | what to do this specific time | role doctrine, methodology |
| **Hooks** | observe progress, deliver the exact questionnaire path and missing duties, trigger original graph progression | tool authorization, full engineering methodology |
| **Human** | requirements and release decisions | mid-loop engineering |

Two consequences worth stating explicitly, because they explain the shape of
every other section:

- **Hooks carry scheduling guidance, not the full methodology.** A lifecycle
  Hook event triggers the Controller and emits the current stage, one Next
  action, the `agent-protocol.md` anchor, the Manual reference, and blockers.
  The detailed procedure still belongs in the stage protocol and Skill. This
  keeps Hook useful after compact without creating a second state machine.
- **Skills do not own role identity.** `builder`, `verifier`, `qa` are not
  Skills. A role is assembled from one Agent Definition plus the smallest
  applicable Skill set; it is not a Skill.

---

## 5. Loop State Machine

The Loop Definition defines 12 top-level states, 30 top-level transitions, and 5
global transitions. The main flow is a single one-way trunk; every side branch
is a correction loop that never bypasses the trunk.

```mermaid
stateDiagram-v2
    [*] --> inactive
    inactive --> planning: TR-001 req_bound
    planning --> document_verification: TR-002 planning_ready
    document_verification --> building: TR-003 document_pass
    document_verification --> planning: TR-004 document_fix_required
    document_verification --> paused: TR-005 req_change_required
    building --> verification: TR-006 builder_batch_reported
    building --> planning: TR-007 execution_spec_change_required
    verification --> bug_resolution: TR-008 blocking_findings
    verification --> acceptance: TR-009 clean_round_passed
    verification --> paused: TR-010 req_change_required
    verification --> paused: TR-011 release_blocked
    bug_resolution --> verification: TR-012 targeted_reverification_completed
    bug_resolution --> planning: TR-013 repair_spec_change_required
    bug_resolution --> paused: TR-014 repair_req_change_required
    acceptance --> release_audit: TR-015 acceptance_completed
    acceptance --> verification: TR-016 acceptance_review_required
    release_audit --> awaiting_human_release: TR-017 audit_approved
    release_audit --> paused: TR-018 audit_blocked
    awaiting_human_release --> release_authorized: TR-025 approve
    awaiting_human_release --> paused: TR-026 defer
    awaiting_human_release --> bug_resolution: TR-027 reject_defect
    awaiting_human_release --> acceptance: TR-028 reject_acceptance
    awaiting_human_release --> release_audit: TR-029 reject_release_audit
    awaiting_human_release --> aborted: TR-030 abort
    paused --> planning: TR-020 req_baseline_updated
    release_authorized --> [*]
    aborted --> [*]
```

Main trunk:

```text
inactive
-> planning
-> document_verification
-> building
-> verification
-> acceptance
-> release_audit
-> awaiting_human_release
-> release_authorized
```

Correction loops:

```text
document_verification -> planning.design
building              -> planning.design
bug_resolution        -> planning.design
verification          -> bug_resolution -> verification.planned
acceptance            -> verification.planned
```

Any state can pause (GTR-001). Runtime integrity failure always pauses
(GTR-005). Pause exits are: `human_resume_approved` (TR-019),
`req_baseline_updated` (TR-020, generation + 1 and invalidate all downstream
evidence), or `human_abort_approved` (TR-021).

---

## 6. Three Phase Machines

Three top-level states are complex enough to own a constrained sub-machine:
`planning`, `verification`, and `bug_resolution`. The top-level state answers
"which engineering stage must resume"; the phase answers "which guarded step is
active inside that stage"; Agent / TASK / BUG lifecycles independently answer
"what may this specific entity do now".

```mermaid
stateDiagram-v2
    state planning {
        [*] --> design
        design --> document_verification: TR-002 planning_complete
    }
    state verification {
        [*] --> planned
        planned --> running: runtime review-plan
        running --> cannot_clean: finding verdict
        cannot_clean --> discovery_draining
        running --> clean
        discovery_draining --> observation_sealed
        cannot_clean --> observation_sealed: P0 stop-the-line
        running --> observation_sealed
        observation_sealed --> bug_resolution: TR-008 sealed batch
        clean --> acceptance: TR-009 machine CleanRound
    }
    state bug_resolution {
        [*] --> investigation
        investigation --> bug_report_review: PTR-BUG-01
        bug_report_review --> repair_readback: PTR-BUG-02
        bug_report_review --> investigation: PTR-BUG-03 reject
        repair_readback --> fixing: PTR-BUG-04
        fixing --> targeted_reverification: PTR-BUG-05 invalidate evidence
        targeted_reverification --> ready_for_full_review: PTR-BUG-06
        targeted_reverification --> investigation: PTR-BUG-07 fail
    }
```

The UI Design Package Gate is sequenced by design, not by accident: the final
HTML prototype, user story, and user flow must be reviewed and locked **after
the REQ is locked and before the development contract is locked**, because
FE/BE/SYNC contracts link to the locked package. Skipping the gate when UI
impact is `changed` is forbidden by INV-004.

The verification machine is a ReviewPlan status projection (`docs/control/agent-protocol.md` #s7): `planned`
waits for a registered ReviewPlan with an exactly-partitioned required Claim
set; `running` consumes static DV/QA Claims first, then behavior E2E; a finding
flips the round to `cannot_clean` but ordinary safe discovery continues
(`discovery_draining`); only a P0 finding seals the ObservationBatch
immediately. The exit transitions are machine-guarded: TR-008 consumes the
sealed batch (exact Finding set), TR-009 recomputes the CleanRound over the
exact Claim set.

Targeted re-verification never creates a clean round. It only permits returning
to `verification.planned`, where a complete review round — a fresh ReviewPlan
with every required Claim consumed — must run again.

---

## 7. Progressive Disclosure

The main session keeps only four items resident. Everything else loads on
demand, driven by the current event and state.

Resident context:

- `AGENTS.md`
- `docs/control/loop-definition.json`
- `.claude/loop-state.json`
- the `loop-orchestration` Skill

On-demand Methodology Skills route by state and event:

| State / phase | Primary Methodology |
|:---|:---|
| `inactive`, startup, recovery, `paused` | `loop-orchestration` |
| `planning.*` | `specification-planning` |
| `document_verification` | `document-verification` |
| Agent `spawned` through `activated` | `agent-dispatch` |
| `building` with reviewed TASK | none; TASK plus selected Best Practices |
| team creation, reuse, reconstruction | `team-planning` |
| `verification.planned` | `loop-orchestration` (ReviewPlan authoring) |
| `verification.running` / `cannot_clean` / `discovery_draining` | `team-planning` (Claim-bound reviewer dispatch; see `.claude/bin/loop-harness s7 status`) |
| `bug_resolution.*` | `bug-resolution` |
| any committed change requiring evidence recalculation | `impact-analysis` |
| `verification.clean` / `verification.observation_sealed` | `acceptance-and-handoff` (TR-009) / `bug-resolution` (TR-008) |
| `acceptance`, `release_audit` | `acceptance-and-handoff` |

Routing order:

```text
current runtime state and phase
-> pending legal event
-> one primary Methodology Skill
-> Agent Definition kind
-> task artifact and risk tags
-> smallest applicable Best-practice set
-> agent dispatch when a subagent is involved
```

Forbidden Skill designs: a role-as-Skill (`builder`, `verifier`, `qa`); a Skill
that owns a second state machine; one Skill per tiny transition; one giant Skill
covering planning, building, review, BUG and release; Best Practices loaded
globally without applicability.

---

## 8. Call Chain: One Transition

The professional consumer receives Main's saved answer or a native Worker's
returned artifact. The Runtime Writer records the accepted facts; the original
followup evaluates stage conditions and uses `transition.Apply` for an eligible
stage change. These are distinct commits, not a second author command chain.

```mermaid
sequenceDiagram
    participant Author as Main or native Worker
    participant Consumer as Original professional consumer
    participant Writer as Runtime Writer
    participant Engine as Original transition engine
    Author->>Consumer: Main save or native SubagentStop delivery
    Consumer->>Writer: Validate and record current professional facts
    Writer-->>Consumer: Accepted receipt and original followup
    Consumer->>Engine: Continue that followup
    Engine->>Engine: Evaluate current domain conditions and guards
    alt missing work or conflict
        Engine-->>Consumer: Concrete missing work and next owner
    else eligible transition
        Engine->>Writer: Commit original stage change
        Writer-->>Consumer: New cursor and recorded event
    end
    Consumer-->>Author: Reception facts and remaining responsibility
```

The Writer owns process locking, candidate validation, paired state/journal
updates and pending recovery. Normal authors do not read a revision and supply
N+1. Explicit CAS remains an integration/recovery choice. A recorded delivery
can survive an interrupted stage followup; resume that original followup rather
than rewriting the answer or issuing another responsibility. Read-only status
does not repair pending writes. Use the supplied original recovery action when
necessary, preserving accepted facts and the actual source workspace.

---

## 9. Native Agent dispatch and delivery

Main chooses the professional responsibility and passes its exact brief or
view/answer references to Claude Code native Agent. Claude Code owns creation,
foreground/background work, messages, wait and resume. Runtime records the
engineering responsibility; it does not create a platform Team or reserve an
opaque actor ID. The same role can have several responsibilities, so use exact
references rather than role names or launch order.

```mermaid
sequenceDiagram
    participant Main
    participant Agent as Claude Code native Agent
    participant Receiver as Original domain consumer
    Main->>Agent: Exact responsibility, inputs and professional output paths
    Agent->>Agent: Read inputs, perform assigned work, save actual artifacts
    Agent-->>Receiver: SubagentStop with exact returned references
    Receiver-->>Main: Original accepted facts or concrete missing work
    Main->>Main: Integrate contributions and complete remaining stage duties
```

| Responsibility | Work and delivery |
| --- | --- |
| S5 / S7 / S8 independent work | Read the exact question and frozen subjects, perform the assigned review or investigation, save the private answer and return its reference. Native Stop invokes the original consumer. |
| S6 Builder | Main uses `s6 prepare --task <TASK> --role <role>`, then native Agent with worktree isolation. The Builder saves its professional plan before implementing, self-checks, commits actual products and returns the exact brief and absolute Result path. Main receives and integrates separately. |
| S9 Builder | Read the approved repair inputs. Save the professional PlanReport before implementation in the actual native worktree, continue repair/self-checks, then return the brief and private Result. Stop imports those artifacts; Main integrates and runs necessary checks. |
| S9 independent Targeted verifier | Verify the assigned Contract assertions against the actual post-repair tree without product writes. Save and return the exact Targeted answer for Stop reception. |
| Main planning, impact, acceptance and human decision | Main saves its own exact questionnaire through the existing Main consumer. It does not fill or receive a Worker's private answer on that person's behalf. |

Start supplies short context; ordinary Worker tools and saves preserve work but
do not activate or receive professional delivery. The returned artifacts carry
actual observations and honest blockers, not copied machine metadata. A missing
path or source goes to Main with its concrete preparation problem.

The native responsibility can end after its actual work or blocker is returned.
Accepted delivery, Main integration, stage closure and human authorization are
separate facts; an already delivered Worker does not wait for peers or future
stages. Main reads the original receiver/integration facts before continuing.

An explicit `plan_approval_required` responsibility still waits for its original
domain approval; ordinary `plan_checkpoint` work continues without a second
approval. Existing registered frozen executions retain their exact inputs and
[recovery contract](../../skills/agent-dispatch/frozen-execution.md). Their
launcher, readback and activation machinery are not new native prerequisites.
Use the current [stage protocol](../control/agent-protocol.md) and
[native dispatch method](../../skills/agent-dispatch/SKILL.md) for exact inputs
and outputs; this guide defines no additional submission sequence.

---

## 10. Verification: Conformance, Quality, Runtime

Inside `verification`, three orthogonal questions are answered by separate
workgroups running from separate manifests. None substitutes for another, and a
clean round requires all three to PASS in the same review round.

| Team | Question | Examples of dimensions |
|:---|:---|:---|
| **Delivery Verifier** | does the implementation conform to the specification? | REQ coverage, contract implementation, TASK completion, module function, front-back-external integration, state and exception, regression |
| **QA** | does the engineering quality meet the bar? | code quality, architecture and reuse, hardcoding, unit tests, integration tests, security, performance, reliability |
| **E2E Tester** | does the product work in a real browser from the user's point of view? | user flows, Playwright specs, screenshots/traces, console errors, network failures, request/response mismatch |

The number of Agents per team is not fixed. The `team-planning` Skill computes
it from module boundaries, contract surface, risk tags, migration impact,
concurrency, security and BUG history, and emits a team manifest with single-
responsibility assignments.

A clean round requires all four conditions together:

```text
all required Delivery Verifier dimensions == PASS
AND all required QA dimensions == PASS
AND all required E2E Tester dimensions == PASS
AND all blocking BUGs are closed
AND no referenced evidence is invalidated
```

All of this must hold for one same review round. Targeted re-verification, no
matter how many dimensions it covers, can never satisfy the clean-round gate;
it only authorizes returning to `verification.planned` to run a complete round.

---

## 11. Document Graph

Top-down:

```text
locked REQ
-> design and UI design package
-> FE/BE/SYNC contracts
-> TASK
-> implementation
-> Delivery Verifier, QA, and E2E Browser evidence
-> BUG/fix/review rounds
-> ACC
-> release architecture audit
-> human release approval
```

Bottom-up, every TASK or BUG manifest must link back through every applicable
layer. Documents are tracked by `path + logical version + SHA-256 + baseline
generation`. A transition that consumes a document recomputes its hash and
rejects or pauses if it changed without a recorded change event.

Evidence validity:

| Status | Meaning |
|:---|:---|
| `valid` | usable by the current baseline and round |
| `invalid` | affected by a later change and unusable |
| `superseded` | replaced by newer valid evidence, retained for audit |

Invalid evidence is never made valid again; replacement evidence receives a new
ID. A clean round cannot reference invalidated evidence.

---

## 12. Loop Boundary

Engineering Loop binding (`.claude/bin/loop-harness req bind`) starts only for one human-
locked REQ and is independent of Claude `/loop`. The Harness may automate
engineering and release preparation, but it cannot change the REQ or execute
squash merge, publication, deployment, or formal release.

Claude `/loop` only delivers the Wake-up Prompt at `.claude/loop.md`. It does
not bind a REQ or authorize release. The independent `req bind` operation is
sufficient for engineering execution, so no separate `/goal` is required.

### Starting a REQ after a terminal Runtime

`release_authorized` and `aborted` are terminal Loop states. `awaiting_human_release`
is a non-terminal human gateway and cannot be rolled over while its decision is
pending. None of these states is made reusable by editing
`.claude/loop-state.json`. When a human has completed the release decision and
wants to begin a new REQ, they first run:

```bash
.claude/bin/loop-harness runtime rollover \
  --approved-by <human identity> \
  --approval-evidence <human-decision-id> \
  --root .
```

The command requires the named valid `human_decision` evidence to have been
produced by the specified identity and scoped to the semantic action
`runtime_rollover:<runtime_id>` for the current terminal Runtime.
Create that evidence with `runtime evidence add --scope-ref
runtime_rollover:current`; the harness resolves the token to the current
Runtime identity, while the Writer records the commit revision internally.
It archives the eligible terminal state and its
journal under `.claude/runtime-archive/`, then creates a new empty inactive
Runtime and journal. A durable pending marker recovers an interrupted rollover
before any later runtime operation. Only this clean Runtime may be bound to the
next locked REQ. `init` is bootstrap-only and refuses to overwrite existing
runtime files; rerun it if its own bootstrap marker remains after interruption.

### A blocking finding keeps the REQ acceptance gate open

There is no accepted REQ with known blocking issues. Either a clean round
passes, or any blocking Delivery Verifier, QA, or E2E Browser finding
forces S8 finding investigation through `bug_resolution` (TR-008). The original encounter and evidence
enter S8 causal investigation. Main routes the Case and obtains the required
approval for its RepairContract; a BUG Markdown file is an optional projection.
Main supplies the approved repair context to native Builders, integrates real
contributions and arranges an actually independent Targeted verifier. Affected
evidence is handled through the original impact judgment, then a fresh complete
verification round runs. Local re-verification alone never ends
the Loop.

Forbidden outcomes:

- `loop_modify_locked_req`
- `write_before_agent_activation`
- `write_outside_allowed_paths`
- `lock_unverified_execution_batch`
- `create_acc_without_clean_round`
- `run_release_audit_without_acc`
- `automated_squash_merge`
- `automated_formal_release`

### Mixed Hook modes

Human-only hard blocks:

- Loop modifying a locked REQ
- automated squash merge or formal release

Warn-and-self-correct (recoverable process completeness):

- unactivated Agent or out-of-scope write attempt
- acceptance or release preparation before a clean round
- closing a TASK without required critical evidence
- a Skill that should be loaded is missing
- document links or versions are stale
- required professional coverage has an unassigned mandatory responsibility or one without a current accepted outcome
- a BUG lacks root cause or reproduction evidence
- Verifier or QA lacks valid evidence
- impact analysis was not run after a repair
- an Agent exits without a report

---

## 13. Authority

| Concern | Source |
|:---|:---|
| static project boundaries | root `AGENTS.md` |
| project configuration | `docs/project.yaml` |
| human project/stage summary | `docs/project-map.md` |
| Loop states and guards | `docs/control/loop-definition.json` |
| current Loop state | `.claude/loop-state.json` |
| methods and best practices | `.claude/skills/` |
| role definitions | `.claude/agents/` |
| permissions and Hook decisions | `docs/control/hook-policy.json` |
| specifications and evidence | `docs/requirements/`, `docs/design/`, `docs/architecture/`, `docs/dev/contracts/`, `docs/dev/tasks/`, `docs/reports/`, `docs/reports/release-audits/` |

`docs/project.yaml`, `docs/project-map.md`, TASK files, and chat are not runtime authorities.

---

## 14. Directory Map

| Directory | Purpose |
|:---|:---|
| `docs/requirements/` | requirement baselines |
| `docs/design/` | architecture, UI, state, data, Loop contracts, and Project Design Foundation (`DESIGN.md`) |
| `docs/architecture/` | technical architecture, shared models, state and dataflow |
| `docs/dev/contracts/` | FE/BE/SYNC execution contracts |
| `docs/dev/tasks/` | executable assignments |
| `docs/reports/` | BUG, delivery, QA, and acceptance evidence |
| `docs/reports/release-audits/` | release architecture audits |
| `docs/rules/` | stable project policies |
| `.claude/skills/` | Methodology and Best-practice Skills |
| `.claude/agents/` | Agent Definitions |
| `.claude/loop-state.json` | current Loop runtime authority |
| `.claude/loop-events.jsonl` | append-only audit journal |
| `.claude/bin/loop-harness` | installed Go Harness CLI |

Human onboarding is in `docs/guides/getting-started.md`. Machine-checkable authorities are
`docs/control/loop-definition.json`, `docs/control/hook-policy.json`, and the schemas embedded
in the Harness. Reusable methods and best practices live in the Skill catalog.

---

## 15. Bootstrap

1. Follow [the installation guide](install.md) to install the packaged Harness into a new empty target. It installs Hook settings, Skills and Agent definitions, initializes inactive Runtime and validates before publishing.
2. Fill `AGENTS.md`, `docs/project-map.md` and `docs/project.yaml` with the project's stable facts.
3. If the product has user-visible UI, create `docs/design/DESIGN.md` from its template (leave `draft`) and run the installed `design-foundation` Skill before locking the first UI-changing REQ. See [the design entry](../design/README.md).
4. Run `.claude/bin/loop-harness doctor --root .` and `validate --all --root .`.
5. Bind a human-locked REQ with the installed Harness. Existing projects must keep the complete matching release; do not overlay the new docs layout.

---

## 16. Verify

```bash
make ci-verify       # PR checks: static analysis, core tests, critical seams, focused race, doctor/validate/build
make ci-verify-full  # complete tests/race plus release installation validation
make verify          # alias for ci-verify-full
.claude/bin/loop-harness doctor --root .
.claude/bin/loop-harness validate --all --root .
```

PR and main CI run `make ci-verify`. This runs all discovered packages except
`internal/cli`, `internal/integration`, `internal/investigation`, and
`tests/system/...` with `-short`. The three large scenario packages instead run
the explicit critical-seam selections in `make test-ci-smoke`: native S5/S6/S7/S8/S9
Start/Stop reception, malformed answer and source ownership, Hook tool freedom,
S10/S11 handoff and human decision authority, integration failure/resume,
checkpoint CAS, process cleanup and Case approval. The race selection covers
Runtime, recovery, locks, schemas, check execution, controller, Hook, audit,
submission and integration checkpoints. The separate Runtime portability workflow
continues to check Linux, Windows and macOS.

Full S0–S11 scenario matrices, compatibility launchers, recovery combinations,
system suites and full race instrumentation run through `make ci-verify-full`.
They are required by the release workflow. For a complete Actions run without
publishing, manually run the **CI** workflow on the desired ref with **full**
enabled; this uses a separate concurrency group from PR checks. A workflow on a
new branch is available in the Actions UI after its definition reaches the
default branch. Locally, initialize Runtime before either verification target
with `go run ./cmd/loop-harness init --root .`.

PR tests use a five-minute per-package limit and the job has a fifteen-minute
limit; full tests retain the thirty-minute per-package limit and a manual full
job has a two-hour limit. These are failure bounds, not expected durations. A
local full run can override `TEST_TIMEOUT` when needed. No retries hide failures.
When adding coverage, keep focused logic tests in the regular package suite and
add a critical CLI/integration/Case seam to `test-ci-smoke` when appropriate;
large scenario combinations belong in the complete suite. All existing tests
remain enabled there. Packaged source and installed release graphs are both
validated by `doctor-staged`, so full verification builds that archive once.

### Runtime and transitions

The Orchestrator advances the Loop exclusively through the runtime transition
mechanism; `.claude/loop-state.json` is never edited by hand.

```bash
# Recover/replay the snapshot from the journal head at startup.
.claude/bin/loop-harness runtime reconcile --root .

# Human-authorized handoff after release_authorized or aborted.
# awaiting_human_release must receive an explicit S11 decision first.
# This archives the old Runtime; it is not a Loop transition.
.claude/bin/loop-harness runtime rollover --approved-by <identity> --approval-evidence <human-decision-id> --root .

# Record exactly one current S11 human decision; this is also the legacy
# awaiting_human_release migration entrypoint. No target state is accepted.
.claude/bin/loop-harness runtime human-decision \
  --disposition <approve|defer|reject_defect|reject_acceptance|reject_release_audit|abort> \
  --actor <user|orchestrator> \
  --decision-evidence <human-decision-reference>
# reject_defect additionally requires --finding-evidence <finding-reference>.

# Apply one legal transition; the Writer owns the internal commit sequence.
.claude/bin/loop-harness runtime transition \
  --id TR-xxx --actor orchestrator \
  --evidence <slot>=<reference>
```

For a transition with `required_evidence`, repeat `--evidence` once per slot.
The left side is the requirement slot from the Manual, while the right side
is the ID or repository-relative path of a currently valid registered
evidence artifact; a `*_record` slot is not itself an evidence kind to
register. For example, TR-006 can be recovered with:

```bash
.claude/bin/loop-harness runtime transition \
  --id TR-006 --actor orchestrator \
  --evidence builder_report_record=<builder-report-id-or-path>
```

If a binding is missing, retry with the command shape shown in the error and
run `.claude/bin/loop-harness explain TR-006` (replace the ID as needed) to see each slot's
accepted registered kinds and current candidate evidence. Do not register the
`*_record` slot name; use one of the accepted persisted kinds shown by the
Manual or explain command.

Other runtime verbs: `register-workgroup`, `agent-event`, `task-complete`
(canonical Builder Result registration — one command replaces the
`agent-event completion_reported` + `evidence add` dual write),
`task-integrate` (explicit worktree integration: Inspect → non-squash
merge → verified checkpoint, identical to the SubagentStop path),
`bug-event`, `evidence`, `transition`, `change`, and `human-decision`.
Run `.claude/bin/loop-harness runtime` with no subcommand for the full list.
