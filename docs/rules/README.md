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
