# Bugfix Review Rule

---
rule_id: R-P06
legacy_id: "024"
category: Process
status: locked
owner: Project Manager / Architect / Verifier
scope: bugfixes, regressions, state machines, read models, data repair, admin/API contracts
---

## 1. Rule

Do not start a bugfix review from the diff.

First define a **Closing Contract**: field-level assertions proving what must be true after the fix.

## 2. Closing Contract Minimum

Before diff review, write:

```text
Closing Contract
- user-visible contradiction:
- states that must not reappear:
- API fields and values that must change:
- data pollution to repair:
- historical samples to regress:
- tests that fail before and pass after:
```

Use executable assertions when possible:

```python
assert progress.current_stage == "waiting_dependency"
assert progress.progress_percent < 100
assert response["reason"] == "waiting_kyc"
```

## 3. Bug ID Assignment

Parallel QA agents do not allocate bug IDs by themselves.

Use one of these flows:

- PM / Architect preassigns IDs, e.g. `QA-001 -> BUG-002.md`.
- QA reports findings first; PM / Architect reviews and creates canonical BUG files.

If two reports describe the same defect, keep one canonical BUG and mark the other duplicate in its status/history.

## 4. Review Layers

| Layer | Check |
|:---|:---|
| state machine | status, stage, reason, terminal states, retry, dependency |
| data model | fields, identity, aggregation keys, null meaning, migration/backfill |
| flow paths | write path, read path, retry/fallback, admin/API/manual path |
| tests | old behavior fails, new behavior passes, historical samples regress |

## 5. Test Validity Questions

Ask:

- Would the test fail if implementation returned empty data?
- Would it fail if the old bug remained?
- Does it assert exact fields, not just “no exception”?
- Does it cover real service/API paths, not only isolated helpers?
- Does it cover historical or equivalent samples?
- Does data repair have dry-run/apply evidence?

## 6. Data Repair Gates

Any write repair must include:

- target rows
- dry-run SQL
- apply SQL or script
- before/after assertions
- rollback or mitigation plan
- audit fields

## 7. Output

Bugfix review output must include:

- Closing Contract
- findings ordered by severity
- evidence from code/tests/data
- remaining risks
- close / block decision

## 8. Forbidden

- closing a bug because “related tests pass”
- reviewing only the diff without a Closing Contract
- accepting aggregate claims without sample or field-level evidence
- hiding data repair inside application defaults
- QA agents writing new BUG files with unassigned IDs
- duplicate bug reports without a canonical BUG reference
