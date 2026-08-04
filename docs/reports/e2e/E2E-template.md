# E2E Tester Evidence: E2E-{id}

> Status: draft / PASS / FIX_REQUIRED / BLOCKED / invalidated
> Runtime ref: `{runtime-id}@{revision}`
> Review round: {n}
> Workgroup manifest: `{team-manifest-path}`
> Assignment: `{assignment-id}`
> Responsibility: E2E-USER-FLOW / E2E-CONSOLE-NETWORK
> Agent: `{agent-id}`
> Activation: `{activation-ref}`

## 1. Header

REQ ID + round + bound runtime revision + spec file path.

| Field | Value |
|:---|:---|
| REQ | REQ-{id} |
| review round | {n} |
| runtime ref | `{runtime-id}@{revision}` |
| spec chain | REQ `{sha256}` / contracts `{sha256}` / TASK `{sha256}` |

## 2. Stack-State Table

Containers / ports / running versions — proves the environment the bundle was
executed against. **Observed** versions, not "expected".

| Component | Version | Port | Evidence |
|:---|:---|:---|:---|
| backend | {commit} | {port} | `{ref}` |
| frontend | {commit} | {port} | `{ref}` |
| db | {version} | {port} | `{ref}` |
| browser | {name/version} | n/a | `{ref}` |
| playwright | {version} | n/a | `{ref}` |

## 3. Test-Architecture Diagram

Agent assignments + journey sequence (one box per `test()`). Pair this diagram
with the `USER-FLOW-{REQ-id}-{module}.md` it transcribes; every flow box must
trace to a `F-NNN` / `PATH-*` step in the source flow file.

```text
[login.spec] → [fund-list.spec] → [fund-detail.spec] → [kyc-review.spec]
      │                │                    │                    │
   F-001/PATH-001  F-002/PATH-001       F-003/PATH-001       F-004/PATH-001
```

## 4. Real-Browser Flow Execution

Per-PATH walk table with step-level evidence (mirrors e2e-browser-testing
SKILL §Evidence Bundle and serves as the legal source for BLOCKED findings).

| Field | Value |
|:---|:---|
| executed path | PATH-{id}: {path title} |
| entry point used | {declared PATH entry point} |
| direct URL used | no / yes: {declared reason from PATH} |
| flow file | `USER-FLOW-{REQ-id}-{module}.md` |

| Step ID | User action performed | Expected visible result | Observed visible result | Result | Evidence |
|:---|:---|:---|:---|:---|:---|
| PATH-{id}-001 | {click/type/navigation from PATH} | {expected} | {observed} | PASS / FAIL / BLOCKED | `{screenshot/trace/video/log}` |

## 5. API CRUD Walk

`# · Method · Path · Status · Notes` with **observed** HTTP codes (not
expected). Every required CRUD operation the affected flows depend on must
appear here.

| # | Method | Path | Status | Notes |
|:---|:---|:---|:---|:---|
| 1 | GET | `/api/funds` | 200 | observed at {ts} |
| 2 | POST | `/api/funds` | 201 | observed at {ts} |

## 6. CDP Findings

Bugs caught only at browser level (silent 4xx, uncaught exceptions, console
errors). Each finding cites the JSONL line so a reviewer can replay.

| Finding ID | CDP event | URL / Route | JSONL line | Notes |
|:---|:---|:---|:---|:---|
| E2E-CDP-001 | `Runtime.exceptionThrown` | `/funds` | evidence/run-001.jsonl#L42 | {description} |

## 7. Console And Network

| Check | Expected | Observed | Result | Evidence |
|:---|:---|:---|:---|:---|
| console errors | none except documented benign warnings | {observed} | PASS / FAIL | `{log-ref}` |
| failed network requests | none for required flow | {observed} | PASS / FAIL | `{trace-ref}` |
| request/response contract | matches FE/BE/SYNC contract | {observed} | PASS / FAIL | `{trace-ref}` |

## 8. Evidence Validity + Status-Code Distribution

| Field | Value |
|:---|:---|
| baseline generation | {n} |
| review round | {n} |
| app fingerprint | `{sha256/commit}` |
| supersedes | `{evidence-id}` / none |
| invalidated by | `{impact-id}` / none |

Status-code distribution (sanity check on coverage, e.g.
`2xx: 47, 3xx: 3, 4xx: 2 (expected), 5xx: 0`):

| Class | Count | Expected | Notes |
|:---|:---|:---|:---|
| 2xx | {n} | {expected} | {notes} |
| 3xx | {n} | {expected} | {notes} |
| 4xx | {n} | {expected} | {notes} |
| 5xx | {n} | 0 | {notes} |

Evidence inventory paths:

```text
docs/reports/e2e/evidence/{RUN_ID}.jsonl
docs/reports/e2e/screenshots/{RUN_ID}-{step}.png
```

## 9. BUG Drafts Surfaced + Idempotent Re-Execution

BUG drafts surfaced from this bundle (one block per finding that should
become a canonical BUG; shape matches `docs/reports/bugs/BUG-NNN.md` so
filing is a copy-paste):

### BUG draft: {short title}
- 严重度: P0 / P1 / P2
- 关联 flow: F-NNN
- 关联 step: 步骤 N
- 关联 prototype: {file}.html
- 现象: {what happened, with screenshot ref}
- 期望: {what flows.md said should happen}
- 复现: {exact `pnpm playwright test --grep` command}
- CDP evidence: {JSONL path + line number}
- 关联代码: {file:line if CDP stack trace identified it}
- 建议根因: {hypothesis, marked as such — do not assert without code review}

Idempotent re-execution instructions — anyone running these commands must see
the same pass/fail pattern (timestamps differ; counts and verdicts match):

```bash
# Environment
export WEB_BASE_URL=http://127.0.0.1:58080
docker compose up -d   # if backend services needed

# Seed (per-persona tokens + test data)
pnpm run seed:test

# Run
pnpm playwright test web/e2e/REQ-{id}-round{N}-cdp-{feature}.spec.ts

# Evidence lands at
ls docs/reports/e2e/evidence/*.jsonl
ls docs/reports/e2e/screenshots/*.png
```

## Result

```text
PASS / FIX_REQUIRED / BLOCKED
```

Requested lifecycle event: `{event}`

Findings are symptoms. They enter S8 finding investigation before any repair
Builder is activated.
