# Agentic Coding Headers — Vibe Coding Engineering Blueprint

A documentation engineering framework for AI/Agent-assisted software delivery. Provides complete process templates, engineering rules, and role definitions covering the full lifecycle from requirements analysis, architecture design, contract decomposition, task orchestration, to delivery and acceptance.

## Core Philosophy

**Documents are contracts. Agents execute against contracts.** Humans own goals, trade-offs, and final acceptance; Agents execute efficiently within contract boundaries.

Vibe Coding should not be "coding while chatting." The engineering approach decomposes a project into verifiable document baselines first, then lets Agents execute against them — every phase must answer: what to solve, what not to solve, how to implement, how to verify, and who owns what.

## Directory Structure

```
docs/
├── project.yaml              # Project metadata, tech stack, baseline status
├── vibe-coding-blueprint.md  # Master blueprint: full process and engineering principles
├── rules/                    # Engineering rules (communication, naming, API design, security, etc.)
├── requirements/             # Requirements document templates
├── design/                   # Architecture, dataflow, state machine, data model, ADR templates
├── contracts/                # Frontend/backend/sync contract templates
├── tasks/                    # Task board and task sheet templates
├── reports/                  # Review, bug, test, acceptance report templates
├── release_audits/           # Pre-release architecture audit templates
├── delivery/                 # Handover and changelog templates
└── retrospective/            # Retrospective templates
```

## Seven-Phase Process

| Phase | Focus | Key Outputs |
|:---|:---|:---|
| 0. Bootstrap | Workspace, tech boundaries, collaboration rules | `project.yaml`, rule baselines |
| 1. Requirements | Turn vague ideas into verifiable requirements | `REQ-{id}.md` |
| 2. Architecture | System boundaries, module decomposition, data flow, ADRs | `ARCHITECTURE.md`, ADRs |
| 3. State & Locking | Core entity lifecycles, concurrency control | State machines, data models |
| 4. Contracts | Decompose design into independently executable contracts | `FE/BE/SYNC-{id}.md` |
| 5. Task Planning | Orchestrate contracts into executable plans | Task board, task sheets |
| 6. Development & QA | Implement against contracts, prove compliance via tests | Code, tests, review reports |
| 7. Delivery & Retro | Deliverable, observable, reviewable releases | Acceptance reports, changelogs, retros |

## Agent Roles

| Role | Responsibility | Decision Authority |
|:---|:---|:---|
| Architect | Requirements, architecture, review, acceptance — full lifecycle tech owner | Design consistency, tech trade-offs |
| Contractor | Decompose design into contracts and integration points | Contract completeness recommendations |
| Builder | Implement code and tests per contract | Implementation details within contract scope |
| Verifier | Independent testing, bug reporting, acceptance evidence | Testing conclusions |
| Librarian | Documentation maintenance, knowledge accumulation, skill distillation | Doc structure and archival |

## Contract Locking

Once a contract is marked `locked`, it becomes the sole execution reference for Builders:

- **Forbidden**: Modifying interfaces, fields, error codes, state machines, side effects, or expanding scope without approval
- **Allowed**: Choosing implementation details within scope; submitting change requests when issues are found
- **Change process**: Request → Architect approval → Version increment → Sync tasks and tests

## Engineering Rules

10 mandatory rules covering the full development lifecycle:

| Rule | Applies To |
|:---|:---|
| communication | Collaboration, task dispatch, clarification, changes |
| naming | Documents, branches, tasks, contracts, APIs |
| change-control | Changes to requirements, design, contracts, quality baselines |
| git-branch-release | Branch creation, merges, releases, hotfixes |
| security | Code, configuration, data, permissions, secrets |
| api-design | APIs, events, webhooks, error codes, field changes |
| state-machine | States, transitions, retries, dependencies, terminal states |
| error-handling | Error codes, exceptions, user messages, failure paths |
| bugfix-review | Bug fix review and closure |
| release-architecture-audit | Pre-release architecture audits |

## Quick Start

1. Copy the `docs/` directory into your new project
2. Edit `docs/project.yaml` with your project name, goals, tech stack, and constraints
3. Read `docs/vibe-coding-blueprint.md` for the complete process
4. Read `docs/rules/README.md` for all engineering rules
5. Follow the seven-phase process step by step

## Success Criteria

We don't measure by document count, but by these outcomes:
- New Agents understand context through task sheets and referenced documents
- Developers don't need to repeatedly ask "what if..."
- Interfaces, state machines, and error codes each have a single source of truth
- Every change traces back to a requirement, contract, task, or bug
- Every delivery includes test evidence, acceptance conclusions, and rollback plans

## License

MIT License
