# Release Architecture Audit Rule

---
rule_id: R-P05
legacy_id: "023"
category: Process
status: locked
owner: Project Manager / Architect / Release Owner
scope: release, hotfix, merge to master/main
---

## 1. Rule

Every release needs an architecture release audit before merge to `master/main`.

The audit is not another code review. It verifies that system-level invariants still hold.

## 2. Required Output

Create:

```text
docs/release_audits/YYYY-MM-DD_<release-or-topic>_architecture_audit.md
```

The report must be committed with the release.

Allowed conclusions:

```text
APPROVED
APPROVED_WITH_NON_BLOCKING_RISKS
BLOCKED
```

`BLOCKED` means no merge and no production release.

## 3. Audit Areas

| Area | Questions |
|:---|:---|
| state machine | new states, exit conditions, terminal states, retry/dependency behavior |
| transaction/UoW | session boundaries, rollback behavior, long I/O inside transactions |
| concurrency/idempotency | claim/lease, duplicate delivery, race windows, upsert keys |
| data model | identity keys, defaults, enum expansion, historical data compatibility |
| call sites | new params, dependency injection, global/shared instances |
| errors/observability | stable error codes, logs, control-plane diagnosis, metrics |
| tests | unit, service, integration, migration, concurrency, regression samples |
| docs/release scope | TASK/BUG/REV/QA/ACC state, release changes, migration order, rollback |

## 4. Blocking Findings

Block release when:

- state has no exit path
- migration is missing from the formal release path
- raw SQL was not validated against real schema
- unique key changed without historical conflict check
- concurrent create depends only on application-level SELECT
- critical DB behavior is tested only with mocks
- out-of-scope batch code is mixed into release
- failure recording may run in an invalid session

## 5. Sign-Off Questions

Before approval, the PM / Architect must answer:

1. What states changed and how does each exit?
2. Which paths cross sessions or UoW boundaries?
3. Which tasks can run concurrently and how are they idempotent?
4. Which keys or indexes changed business identity?
5. Which raw SQL bypasses ORM behavior?
6. Which failures retry automatically and where is retry state stored?
7. Which failures require manual handling and how does the control plane show them?
8. Is multi-instance execution safe?
9. Do tests cover real DB/migration/concurrency paths?
10. Are docs, TDs, bugs, and review reports consistent with code?

## 6. Forbidden

- release without audit report
- PR comment as the only audit evidence
- `BLOCKED` release
- release audit that ignores migration, concurrency, or state changes
- treating an approved architecture audit as human release approval

## 7. Human Release Boundary

The audit is engineering evidence. After it passes, Loop automation enters
`awaiting_human_release`. Squash merge, publication, deployment, and formal
release still require explicit human release approval.
