# Rule Index

---
rule_id: RULES-INDEX
category: Process
status: locked
owner: Project Manager / Architect
scope: all agents
updated: YYYY-MM-DD
---

## Read Order

1. Root `AGENTS.md`
2. This index and every Required Rule
3. Rules referenced by the current REQ, specification, assignment, BUG, or release audit
4. The primary Methodology Skill and selected Best Practices for the current action

Rules are stable policies. Runtime gates belong to the Loop Definition and
Hooks; procedures belong to Skills; role behavior belongs to Agent Definitions.

## Required Rules

| Rule | File | Trigger |
|:---|:---|:---|
| Communication | `communication.md` | collaboration, evidence, escalation |
| Naming | `naming.md` | identifiers, documents, branches, APIs |
| Change Control | `change-control.md` | baseline change |
| Git Branch Release | `git-branch-release.md` | branch, merge, release |
| Security | `security.md` | code, config, data, auth, secrets |

## Conditional Rules

| Rule | File | Trigger |
|:---|:---|:---|
| API Design | `api-design.md` | API, event, schema, error code |
| State Machine | `state-machine.md` | lifecycle or transition |
| Error Handling | `error-handling.md` | error, retry, failure path |
| Bugfix Review | `bugfix-review.md` | BUG investigation or closure |
| Release Architecture Audit | `release-architecture-audit.md` | release preparation |
| UI Design Package | `ui-prototype.md` | visible frontend change |
| Project Design Foundation | `design-foundation.md` | user-visible UI, brand language, first UI REQ, new product surface |

## Immutable Policy

- Chat does not change a baseline.
- Locked REQ changes require human approval through change control.
- Agent permissions are the intersection of definition, manifest, activation, runtime, and Hook policy.
- Release architecture audit is engineering evidence; human release approval is a separate final boundary.

- [Shared models and contract reading](shared-model-contracts.md): common data/protocol authority, explicit model policy, progressive TASK links and real consumer validation.

## Overall dispatch plan

S4 → S5 → S6 follow [dispatch plan rules](dispatch-plan.md). Read the approved wave checklist and live S6 board; schedule compatible tasks that are ready for dispatch and integrate results promptly. `ready` is scheduling eligibility, not a responsibility status.

## Existing project conventions

For normal S4 authoring, reference the actual Rule, registered architecture or
decision and its relevant section in the TASK Document Manifest. Explain the
applicable choice, scope and exceptions in the original professional material.
Do not create or maintain a separate conventions registry or copy source
hashes and versions for this work. New decisions still follow the original
architecture and change-control process.

Existing `docs/rules/project-conventions.json` files retain their original
source checks and frozen consumers. The empty template remains a compatibility
asset; the current resolver has no first-save receiver that fills and persists
missing source metadata. It must not refresh an old registry's fingerprints
from today's bytes.

`tasks check --json` returns `task_conventions` selected for each TASK's declared
write scope. A `declared_default` must match the original locked Rules index or
the original registered locked document, its fixed hash/version, a real source
section and fixed example hashes. Registry status alone proves no approval. An absent
registry creates candidates only for API/error rules explicitly cited by the TASK.
Candidate/conflict/stale/migration rows belong to the original Architect or rule
owner; they never change business values, permission, coverage or stage gates.
Consume resolved `entries[].status`, not the registry's nested `entry.status`
claim. The projection proves source consistency; reviewers still judge semantic
applicability and whether an implementation example follows the rule.
