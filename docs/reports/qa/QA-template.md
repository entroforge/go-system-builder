# QA Evidence: QA-{id}

> Status: draft / PASS / FIX_REQUIRED / RELEASE_BLOCKED / invalidated
> Runtime ref: `{runtime-id}@{revision}`
> Review round: {n}
> Workgroup manifest: `{team-manifest-path}`
> Assignment: `{assignment-id}`
> Responsibility: {QA responsibility ID}
> Best Practice: `.claude/skills/{skill}/SKILL.md`
> Agent: `{agent-id}`
> Activation: `{activation-ref}`

## 1. Fingerprinted Inputs

| Kind | ID / Scope | Path | Version | SHA-256 |
|:---|:---|:---|:---|:---|
| REQ | REQ-{id} | `docs/requirements/REQ-{id}.md` | {version} | `{sha256}` |
| TASK | TASK-{id} | `docs/tasks/TASK-{id}.md` | {version} | `{sha256}` |
| source/tests | {scope} | `{path}` | {commit/version} | `{sha256}` |
| Skill | {skill} | `.claude/skills/{skill}/SKILL.md` | {version} | `{sha256}` |

## 2. Assigned Quality Conclusion

Each report evaluates one responsibility. Team coverage and N/A decisions live
in the manifest, not in this report.

| Criterion | Evidence | Result |
|:---|:---|:---|
| {criterion from assigned Best Practice} | {path/command/sample} | PASS / FAIL / N/A |

## 3. Findings

| Finding ID | Severity | Location | Finding | Evidence | Canonical BUG |
|:---|:---|:---|:---|:---|:---|
| QA-F001 | P0/P1/P2/P3 | `{path:line}` | {fact} | {evidence} | BUG-{id} / pending / N/A |

## 4. Checks

| Check | Command / Method | Result | Evidence ref |
|:---|:---|:---|:---|
| {check} | `{command}` | pass / fail / blocked / not_run | `{ref}` |

## 5. Targeted Re-verification

| BUG | Original assignment | Repair fingerprint | Result | Evidence |
|:---|:---|:---|:---|:---|
| BUG-{id} | `{assignment-id}` | `{sha256/commit}` | pass / fail | `{ref}` |

Targeted re-verification does not satisfy a complete clean round.

## 6. Evidence Validity

| Field | Value |
|:---|:---|
| baseline generation | {n} |
| review round | {n} |
| supersedes | `{evidence-id}` / none |
| invalidated by | `{impact-id}` / none |

## 7. Result

```text
PASS / FIX_REQUIRED / RELEASE_BLOCKED
```

Requested lifecycle event: `{event}`
