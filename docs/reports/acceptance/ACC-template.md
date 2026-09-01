# Acceptance Evidence: ACC-{id}

> Status: draft / passed / blocked / invalidated
> Runtime ref: `{runtime-id}`
> Source REQ refs: REQ-{id} / none
> Accepted module current truth: `docs/design/prototypes/{module}/`
> Baseline generation: {n}
> Clean review round: {n}
> Clean-round evidence: `{review-evidence-ref}`
> PM / Architect: {name}

> 填写纪律：先冻结审查全集，再填写结论。不能因为某项尚未检查就写成
> `N/A`，也不能用一条总体 PASS 代替逐条证据。每项结论都必须回答“什么
> 事实会证明它是错的”；找不到反证证据时写 `UNKNOWN`，不得进入 passed。

## 1. Fingerprinted Baseline

| Artifact | Path | Version | SHA-256 |
|:---|:---|:---|:---|
| REQ | `docs/requirements/REQ-{id}.md` | {version} | `{sha256}` |
| module current truth | `docs/design/prototypes/{module}/` (scenario four-pack + stories/flows/index/*.html) | current | `{sha256/N/A}` |
| contracts | `docs/contracts/CONTRACTS-{id}.md` | {version} | `{sha256}` |
| tasks | `docs/tasks/index.md` | {version} | `{sha256}` |

## 1.1 Audit Universe And Responsibility

审查全集只纳入本 REQ、合同、TASK、S7/S9 证据、changed paths 和风险触发
的对象；冻结后不得为获取 PASS 而删项。

| Inventory ID | 审查面 / 来源 | Expected / Oracle | Owner / 独立审查者 | Evidence slot | Counterevidence question | Status |
|:---|:---|:---|:---|:---|:---|:---|
| COV-001 | {REQ / contract / path / risk} | {expected + oracle} | {owner} | {evidence ref} | {what would falsify PASS?} | pass / N/A / UNKNOWN / fail |

Minimum metrics: requirement coverage 100%; contract/Closing Contract coverage
100%; declared changed-path disposition 100%; `UNKNOWN=0`; unsupported PASS 0;
audit-area completion 100%; blocking findings 0; unowned risk 0; untracked
debt 0. `N/A` requires an authoritative source and an explicit reason. The
machine manifest must contain at least one explicit `requirement`, `contract`,
and `changed_path` row. If a hard category is genuinely outside scope, keep it
in the inventory as an evidence-backed `not_applicable` row; an omitted
category is not 100% coverage.

Machine companion: save the finite ledger as JSON and validate it before
registering the acceptance evidence:

```text
loop-harness s10 manifest validate --file <acceptance-manifest.json> --type acceptance
```

If the acceptance result is intentionally `REVIEW_REQUIRED`, validate the
same structurally complete ledger with `--outcome review_required`; unresolved
rows remain visible and the registered envelope is routed by the Controller
through TR-016 back to a fresh S7 round. Do not relabel an unresolved result as
PASS.

Copyable JSON shape: `docs/examples/s10/acceptance-manifest.json`.

The acceptance evidence envelope must point to that immutable file with
`audit_manifest_path` and `audit_manifest_sha256`. The shape authority is
`internal/schema/assets/s10-audit-manifest.schema.json`; the Quality Gate
consumes the ledger, not a free-form overall PASS. After validation, register
the envelope with `runtime evidence add` and let the Controller request
TR-015.

## 2. Clean Round

| Evidence | Workgroup manifest | Review round | Result | Validity |
|:---|:---|:---|:---|:---|
| Delivery verification | `{manifest/ref}` | {n} | PASS | current |
| QA | `{manifest/ref}` | {n} | PASS | current |
| E2E Browser | `{manifest/ref}` | {n} | PASS | current |
| open blocking BUGs | `{bug-index/ref}` | {n} | none | current |

All required dimensions must be PASS or evidence-backed N/A in the same round.

## 3. Requirement Acceptance

| REQ source_ref / Rule / CASE / Story / PATH / Spec | Expected / Oracle | Evidence | Counterevidence check | Owner | Result |
|:---|:---|:---|:---|:---|:---|
| REQ-{id}/FR-{id} / BR-{id} / CASE-{id} / S-{id} / F-{id} / PATH-{id} / `web/e2e/{module}/*.spec.ts` | {behavior and oracle} | {REV/QA/E2E/test/sample} | {disproof attempted + evidence or UNKNOWN} | {owner} | pass / N/A / UNKNOWN / fail |

### Module scenario acceptance

| Gate | Expected | Evidence | Result |
|:---|:---|:---|:---|
| required allow branches | 100% | `scenario-coverage.json` | pass / fail |
| required reject branches | 100% | `scenario-coverage.json` | pass / fail |
| positive/negative ratio | meets `coverage_profile` | `scenario-coverage.json` | pass / fail |
| module regression | all current required CASE/PATH | E2E round {n} | pass / fail |

### Counterevidence Ledger

| Inventory ID / AC | What would falsify the conclusion? | Check performed | Evidence | Outcome |
|:---|:---|:---|:---|:---|
| {COV-001 / REQ source} | {rejection, boundary, retry, stale or other disproof} | {command / scenario / review} | {ref} | pass / N/A / UNKNOWN / fail |

## 4. Delivery And Operations

| Item | Value | Evidence / Owner |
|:---|:---|:---|
| delivered scope | {modules/interfaces/config/data} | {TASK/commit} |
| deployment order | {steps} | {runbook/owner} |
| migration/data handling | {steps/N/A} | {script/evidence} |
| runtime verification | {health/critical path} | {command/result} |
| rollback | {method} | {owner} |
| operations handoff | {monitoring/alerts/manual controls} | {owner} |

## 5. Remaining Non-blocking Risks

| Risk | Severity | Impact / recovery point | Owner | Tracking artifact |
|:---|:---|:---|:---|:---|
| {risk} | P3 | {impact / recovery point} | {owner} | {REQ/BUG/TD} |

## 5.1 Objective Completion

| Metric | Required | Observed |
|:---|:---:|:---:|
| REQ / contract / Closing Contract coverage | 100% | {value} |
| Declared changed-path disposition | 100% | {value} |
| Unanswered `UNKNOWN` | 0 | {value} |
| Unsupported PASS | 0 | {value} |
| Unowned risks / untracked debt | 0 | {value} |

## 6. Decision

```text
passed / blocked
```

Acceptance does not authorize release. The next evidence is a release
architecture audit; final publication still requires human release approval.
