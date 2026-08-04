# Vibe Coding Blueprint

> Background reference. Agents do not need to read this for normal work.
> For execution, read `docs/agent-protocol.md` first.

## 1. Purpose

Vibe Coding is not “chat and code”.

This documentation system turns human intent into explicit baselines:

```text
project facts -> requirements -> req bind -> design/UI design package -> contracts -> tasks -> execution -> verification -> acceptance
```

The goal is to make AI/Agent delivery traceable, reviewable, and safe to coordinate.

## 2. Core Principles

1. Docs are the source of truth.
2. Chat is not a baseline.
3. The main session is Project Manager / Architect.
4. Sub agents work from explicit tasks only.
5. Each stage has an output and a gate.
6. Requirements must be locked before design, contracts, tasks, or Builder work.
7. UI-impacting requirements must pass UI Design Package Gate before FE/BE/SYNC contract lock.
8. A human-locked REQ plus an Engineering Loop binding (`loop-harness req bind`) is required before formal contracts, tasks, Agent Teams, or implementation. Claude `/loop` is a separate lifetime — it only delivers the Layer 2 Wake-up Prompt and does not bind a REQ or replace `/goal`.
9. Locked contracts define Builder boundaries.
10. Sub agents prove understanding before activation.
11. Document Verifiers approve contract/task logic before Builder activation.
12. S7 runs Delivery Verifier, QA, and E2E Browser workgroups through single-responsibility sub agents in Agent Teams.
13. S7 blocking findings enter S8 root-cause investigation, then accepted canonical BUGs enter S9 Builder repair plus original-responsibility targeted re-check.
14. Development, verification, findings, and fixes repeat until one complete deep review round fully passes.
15. Release audit starts only after that clean review round.
16. Loop mode stops before human release approval and squash merge.
17. Verification evidence must land in reports or acceptance docs.
18. Repeated lessons become rules, templates, or skills.

## 3. Why Progressive Disclosure

Agents should not start by reading a long methodology document.

Use this order:

| Layer | File | Purpose |
|:---|:---|:---|
| Fast protocol | `docs/agent-protocol.md` | decide what to do now |
| Project entry | `AGENTS.md` | project-specific facts and commands |
| Project map | `docs/project-map.md` | current stage, facts, PM todo, stage check |
| Templates | `docs/*-template.md` | create the current stage output |
| Rules | `docs/rules/*.md` | apply only when triggered |
| Blueprint | this file | understand the method, not daily execution |

## 4. Document Baselines

| Baseline | Path | Meaning |
|:---|:---|:---|
| Project facts | `docs/project-map.md` | known facts, unknowns, current stage |
| Requirements | `docs/requirements/` | user value, scope, flows, acceptance |
| Design | `docs/design/` | architecture, state, data, decisions |
| UI Design Packages | `docs/design/prototypes/` | final HTML prototype, user story, and user flow for UI changes |
| Contracts | `docs/contracts/` | Builder execution boundary |
| Tasks | `docs/tasks/` | owner, allowed files, steps, acceptance |
| Reports | `docs/reports/` | review, bug, QA, acceptance evidence |
| Release | `docs/release_audits/` | release changes, migration, rollback, and human release gate |

Locked baselines change only through `docs/rules/change-control.md`.

## 5. Role Model

| Role | Responsibility |
|:---|:---|
| Project Manager / Architect | lifecycle, PM todo, design, dispatch, verification, acceptance |
| Contractor | contract drafting after gates are met |
| Builder | implementation inside locked task and contract boundaries |
| Document Verifier | contract/task coverage, logic, links, versions, and Closing Contract |
| Delivery Verifier Team | requirement, contract, task, module, integration, and regression correctness |
| QA Team | code, architecture, maintainability, tests, security, performance, and reliability |
| E2E Browser Team | locked user flows, real-browser behavior, console, network, screenshots, and traces |
| Librarian | docs, rules, templates, and knowledge cleanup |

The user talks to the main session. Sub agents do not negotiate scope with the user.

## 6. Stage Model

The operational stage model is defined in `docs/agent-protocol.md`.

Short version:

```text
S0 requirement_design
S1 initialize
S2 design
S3 contracts
S4 tasks
S5 document_verification
S6 build
S7 full_verification_round
S8 finding_investigation
S9 bug_resolution
S10 acceptance_and_audit
S11 human_release_gateway
```

Lightweight mode may skip standalone S5/S6 design docs only when the design decisions are written into contracts or tasks.

Lightweight mode cannot skip UI Design Package Gate for UI-impacting requirements.

Engineering Loop binding authorizes work inside one locked REQ. Claude `/loop`
is only a wake-up scheduler; it does not lock requirements, skip evidence, or
authorize release.

## 7. New Project Rule

If a project is only a scaffold:

- record scaffold facts
- list unknowns
- ask for goals, users, scope, flows, exceptions, permissions, and acceptance
- do not turn scaffold structure into target architecture
- do not lock contracts or dispatch Builders

## 8. Git And Release

Git flow is defined in `docs/rules/git-branch-release.md`.

Important constraints:

- docs and baselines land before implementation branches
- Builder feature branches require locked tasks and locked contracts
- release to `master/main` requires release audit
- release squash merge must be synced back to `develop`

## 9. Skills

Skills are reusable operational knowledge.

Create or update a skill only when a pattern repeats and is useful beyond one task.

Good skill content:

- trigger conditions
- hard rules
- common workflow
- known pitfalls
- verification method
- references

## 10. Success Criteria

This documentation system is working when an Agent can quickly answer:

- What stage is the project in?
- What requirement is active?
- What does the PM todo say?
- Is goal mode active?
- Is an Engineering Loop bound to a locked REQ (and is Claude `/loop` running independently)?
- If UI impact exists, did UI Design Package Gate pass?
- Has the sub agent's understanding been approved and activated?
- Did Document Verifiers approve the execution documents?
- Do Delivery Verifier, QA, and E2E Browser workgroups have same-round evidence?
- Which gate blocks the next step?
- Which document is the source of truth?
- Who owns the next action?
- What evidence proves completion?
