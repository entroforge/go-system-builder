# E2E Tester Evidence: E2E-{id}

> Status: draft / pass / finding / req_change_required / release_blocked / invalidated (原 ReviewResult 专业结论的可读投影)
> Runtime ref: `{runtime-id}`
> Review round: {n}
> Workgroup manifest: `{team-manifest-path}`
> Assignment: `{assignment-id}`
> Responsibility: E2E-USER-FLOW / E2E-CONSOLE-NETWORK
> Agent: `{agent-id}`
> Workspace (cold_start only): `e2e-workspace/{plan-id}/` — `verification_artifact_digest` 由原接收器从真实执行来源绑定与校验，作者不计算或抄写

本 Markdown 是已完成专业工作的可读投影，不要求再交一份报告。正常原生 S7 读取原题，只填写本人 `answer`，保存并返回准确引用，由 `SubagentStop` 调原消费者接收；不增加通用 PLAN／激活。Runtime、轮次、责任与摘要等已知信息由系统投影。

## 1. Header

Module current-truth package + source REQ refs + review round + Runtime identity + module spec path.

| Field | Value |
|:---|:---|
| module | {module} |
| source REQ refs | REQ-{id} / none |
| review round | {n} |
| runtime ref | `{runtime-id}` |
| current scenario package | `docs/design/prototypes/{module}/` · 原卷固定来源 |
| spec chain | current module spec `web/e2e/{module}/` / 原契约与 TASK 引用；指纹由系统绑定 |

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
with the module's current `flows.md`; every flow box must trace to a `F-NNN` /
`PATH-*` step and a CASE.

```text
[login.spec] → [fund-list.spec] → [fund-detail.spec] → [kyc-review.spec]
      │                │                    │                    │
   CASE-001 · F-001 · PATH-001  CASE-002 · F-002 · PATH-001  CASE-003 · F-003 · PATH-001  CASE-004 · F-004 · PATH-001
```

## 4. Real-Browser Flow Execution

Per-PATH walk table with step-level evidence (mirrors e2e-browser-testing
SKILL §Evidence Bundle and serves as the legal source for BLOCKED findings).

| Field | Value |
|:---|:---|
| executed path | PATH-{id}: {path title} |
| CASE / polarity | CASE-{id} / positive / negative |
| branch obligation | BR-{id} |
| entry point used | {declared PATH entry point} |
| direct URL used | no / yes: {declared reason from PATH} |
| flow file | `docs/design/prototypes/{module}/flows.md` |

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
| required branch | allow/reject branch is covered | {observed} | PASS / FAIL | `scenario-coverage.json` |
| `visible` | every declared visible checkpoint observed | {observed} | PASS / FAIL | `{screenshot/trace-ref}` |
| `terminal_state` | declared terminal state observed | {observed} | PASS / FAIL | `{evidence-ref}` |
| `persisted_effects` | every declared persistence assertion holds | {observed} | PASS / FAIL | `{evidence-ref}` |
| `forbidden_side_effects` | every declared forbidden effect remains absent | {observed} | PASS / FAIL | `{trace-ref}` |
| negative `rejection` | declared rejection observed | {observed} | PASS / FAIL / N/A | `{evidence-ref}` |
| negative `expected_state` | state remains at the declared rejection state | {observed} | PASS / FAIL / N/A | `{evidence-ref}` |
| negative `recovery` | declared recovery succeeds, or sourced N/A is recorded | {observed} | PASS / FAIL / N/A | `{evidence/ref or source_refs + reason}` |

The finding schema has no dedicated slot for every column above — carry the same
accounting into the Canonical ReviewResult legibly across `observed`, the encounter
`timeline` checkpoints, `terminal_state`, `side_effects`, and `visible_impact`, so S8
can consume it without re-running the flow.

## 8. Evidence Validity + Status-Code Distribution

| Field | Value |
|:---|:---|
| module package fingerprint | `{sha256}` |
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

## 9. Findings Surfaced (machine owns the BUG drafts)

The machine creates one canonical BUG draft per Finding when the ObservationBatch
seals (TR-008) — do not hand-write BUG files. What S8 needs from you is the
investigation-ready Finding in the ReviewResult (walk, boundary, evidence) and the
idempotent re-execution below. A P0 Finding seals the round immediately
(stop-the-line): populate the Finding's `capture_gaps` with what could not be
recorded and why — a P0 without capture gaps is rejected at submit.

Idempotent re-execution instructions — anyone running these commands must see
the same pass/fail pattern (timestamps differ; counts and verdicts match):

```bash
# Environment
export WEB_BASE_URL=http://127.0.0.1:58080
docker compose up -d   # if backend services needed

# Seed (per-persona tokens + test data)
pnpm run seed:test

# Run
pnpm playwright test web/e2e/{module}/ --grep 'CASE-|PATH-'

# Evidence lands at
ls docs/reports/e2e/evidence/*.jsonl
ls docs/reports/e2e/screenshots/*.png
```

## Result

```text
pass / finding / req_change_required / release_blocked
```

本人将实际逐命题判断、检查、Finding 和 verdict 写入原卷，返回准确引用。`subject_digest` 与 `verification_artifact_digest` 由原题目／接收器承接，作者保留预填或 null，不运行摘要命令补字段。本地证据只选择实际路径；原捕获／接收机制固定机器元数据，执行后真正修改脚本或夹具仍须重做受影响流程。

Findings are symptoms. They enter S8 finding investigation before any repair
Builder is activated.
