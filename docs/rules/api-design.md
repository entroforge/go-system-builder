# API Design Rule

---
rule_id: R-API-01
category: Engineering
status: locked
owner: Project Manager / Architect
scope: REST, GraphQL, gRPC, webhooks, events, API contracts
---

## 1. Rule

APIs are contracts, not implementation details.

Fields, error codes, auth, rate limits, idempotency, and caller behavior must be documented, testable, and traceable.

## 2. Use When

Use this rule for APIs, events, webhooks, FE/BE sync, response fields, auth, rate limits, idempotency, or error codes.

## 3. Hard Rules

- Document API fields in `docs/contracts/SYNC-*.md`.
- Define auth, authorization, error codes, rate limits, and idempotency.
- Use or generate `X-Request-ID`.
- Use ISO 8601 UTC for time fields.
- Do not use floating point for money or precision-sensitive values.
- Error codes must define caller or UI behavior.
- Do not expose stack traces, SQL, secrets, or internal paths.

## 4. REST Defaults

| Item | Default |
|:---|:---|
| path | plural resource names, e.g. `/api/v1/orders` |
| methods | GET read, POST create, PUT/PATCH update, DELETE remove |
| pagination | cursor pagination by default |
| errors | stable error codes |
| idempotency | idempotency key or equivalent for create/callback/retry paths |

## 5. Error Shape

```json
{
  "error": {
    "code": "E1001",
    "message": "caller-safe message",
    "details": {},
    "request_id": "uuid"
  }
}
```

## 6. Change Gates

| Change | Compatibility | Required Action |
|:---|:---|:---|
| optional field added | compatible | update SYNC contract and contract tests |
| required field added | breaking | ADR + contract version bump |
| field removed | breaking | ADR + contract version bump |
| field meaning changed | breaking | ADR + affected-party approval |
| error code added | conditional | update caller behavior and tests |

## 7. Evidence

- `docs/contracts/SYNC-*.md`
- contract/API tests
- error-code tests
- linked state doc when API changes state

