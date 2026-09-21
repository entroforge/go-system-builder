# Agentic Coding Headers — Vibe Coding Engineering Blueprint

A documentation engineering framework for AI/Agent-assisted software delivery. Provides complete process templates, engineering rules, and role definitions covering the full lifecycle from requirements analysis, architecture design, contract decomposition, task orchestration, to delivery and acceptance.

## Core Philosophy

**Documents are contracts. Agents execute against contracts.** Humans own goals, trade-offs, and final acceptance; Agents execute efficiently within contract boundaries.

Vibe Coding should not be "coding while chatting." The engineering approach decomposes a project into verifiable document baselines first, then lets Agents execute against them — every phase must answer: what to solve, what not to solve, how to implement, how to verify, and who owns what.

## Directory Structure

Start at the [documentation index](docs/README.md) or [document map](docs/DOCUMENT-MAP.md).

```text
blueprint/                   # Template design and maintenance: L1–L4; never packaged
docs/                        # Target-project documentation
├── control/                  # Executable Definition, policy, protocol, protected commands
├── requirements/     # REQ templates and project requirements
├── design/                   # Experience Foundation, derivation, prototypes, design decisions
├── architecture/             # Technical architecture, shared models, state and dataflow
├── dev/contracts/            # FE/BE/SYNC and integration contracts
├── dev/tasks/                # Task index, task sheets and dispatch plan
├── reports/release-audits/   # Release audit template; sibling report categories retain their roles
├── guides/                   # Installation, onboarding, concepts and engineering workflow
├── rules/                    # Execution constraints
└── examples/                 # Self-contained learning and regression fixtures
```

The [stage protocol](docs/control/agent-protocol.md) defines S0–S11, and the
Loop Definition controls legal transitions. Framework documents explain the
design across L1–L4; implementation and tests remain in the source tree.

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

1. Follow the [installation guide](docs/guides/install.md) using a release and a fresh empty target.
2. Fill the installed project metadata and project map.
3. Read [onboarding](docs/guides/getting-started.md) and the [engineering workflow](docs/guides/engineering-loop.md).
4. Use the installed Harness to validate and bind a human-locked REQ.

Do not copy the factory's entire docs tree into a project or overlay an existing Runtime.
Existing projects keep their matching complete release; this version rejects legacy and mixed layouts.

## Success Criteria

We don't measure by document count, but by these outcomes:
- New Agents understand context through task sheets and referenced documents
- Developers don't need to repeatedly ask "what if..."
- Interfaces, state machines, and error codes each have a single source of truth
- Every change traces back to a requirement, contract, task, or bug
- Every delivery includes test evidence, acceptance conclusions, and rollback plans

## License

MIT License
