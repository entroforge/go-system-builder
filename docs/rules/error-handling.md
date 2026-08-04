# Error Handling Rule

---
rule_id: R-ERR-01
category: Engineering
status: locked
owner: Project Manager / Architect
scope: exceptions, error codes, user messages, logging, retry, failure paths
---

## 1. Rule

Errors must be classified, observable, recoverable when possible, and testable.

Do not swallow exceptions. Do not expose internal details to users or callers.

## 2. Severity

| Level | Meaning | Action |
|:---|:---|:---|
| P0 | data loss, security incident, core path down | block release, fix immediately |
| P1 | core feature unavailable or wrong | fix before release |
| P2 | non-core issue with workaround | track and fix, do not fake closure |
| P3 | UX/copy/low-risk issue | schedule later |

## 3. Hard Rules

- Do not swallow errors.
- Do not expose stack traces, SQL, secrets, or internal paths.
- Mark retryable errors with retry policy and backoff.
- Convert dependency errors to stable error codes.
- Store stable short codes in DB; long tracebacks belong in logs.
- Unknown errors must include `request_id`.

## 4. Frontend / Caller Rules

- Every contract error code has caller behavior.
- Auth expiration has a consistent refresh or redirect behavior.
- Rate-limit errors tell the caller whether to wait or retry.
- Internal error text is not shown directly to users.

## 5. Evidence

- error-code table or `SYNC-*` contract
- error classification tests
- retry / non-retry tests
- API error response contract tests

