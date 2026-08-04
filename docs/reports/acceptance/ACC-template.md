# Acceptance Evidence: ACC-{id}

> Status: draft / passed / blocked / invalidated
> Runtime ref: `{runtime-id}@{revision}`
> Bound REQ: REQ-{id}
> Baseline generation: {n}
> Clean review round: {n}
> Clean-round evidence: `{review-evidence-ref}`
> PM / Architect: {name}

## 1. Fingerprinted Baseline

| Artifact | Path | Version | SHA-256 |
|:---|:---|:---|:---|
| REQ | `docs/requirements/REQ-{id}.md` | {version} | `{sha256}` |
| design/UI | `{path/N/A}` | {version/N/A} | `{sha256/N/A}` |
| contracts | `docs/contracts/CONTRACTS-{id}.md` | {version} | `{sha256}` |
| tasks | `docs/tasks/index.md` | {version} | `{sha256}` |

## 2. Clean Round

| Evidence | Workgroup manifest | Review round | Result | Validity |
|:---|:---|:---|:---|:---|
| Delivery verification | `{manifest/ref}` | {n} | PASS | current |
| QA | `{manifest/ref}` | {n} | PASS | current |
| E2E Browser | `{manifest/ref}` | {n} | PASS | current |
| open blocking BUGs | `{bug-index/ref}` | {n} | none | current |

All required dimensions must be PASS or evidence-backed N/A in the same round.

## 3. Requirement Acceptance

| REQ / Contract clause | Expected | Evidence | Result |
|:---|:---|:---|:---|
| FR-{id} / {contract} §{n} | {behavior} | {REV/QA/E2E/test/sample} | pass / fail |

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

| Risk | Severity | Owner | Tracking artifact |
|:---|:---|:---|:---|
| {risk} | P3 | {owner} | {REQ/BUG/TD} |

## 6. Decision

```text
passed / blocked
```

Acceptance does not authorize release. The next evidence is a release
architecture audit; final publication still requires human release approval.
