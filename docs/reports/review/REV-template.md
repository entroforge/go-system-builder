# Review Evidence: REV-{id}

> Status: draft / PASS / FIX_REQUIRED / REQ_CHANGE_REQUIRED / invalidated
> Runtime ref: `{runtime-id}@{revision}`
> Review round: {n}
> Workgroup manifest: `{team-manifest-path}`
> Assignment: `{assignment-id}`
> Responsibility: {DV/VER responsibility ID}
> Agent: `{agent-id}`
> Activation: `{activation-ref}`

## 1. Fingerprinted Inputs

| Kind | ID | Path | Version | SHA-256 |
|:---|:---|:---|:---|:---|
| REQ | REQ-{id} | `docs/requirements/REQ-{id}.md` | {version} | `{sha256}` |
| contract | {id} | `docs/contracts/{id}.md` | {version} | `{sha256}` |
| TASK | TASK-{id} | `docs/tasks/TASK-{id}.md` | {version} | `{sha256}` |
| implementation | {scope} | `{path}` | {commit/version} | `{sha256}` |

## 2. Assigned Conclusion

This report evaluates exactly one manifest responsibility and scope partition.

| Scope | Expected behavior / criterion | Result | Evidence |
|:---|:---|:---|:---|
| {module/clause/path} | {criterion} | PASS / FAIL / N/A | {command/path/sample} |

N/A requires a recorded rationale and evidence.

## 3. Findings

| Finding ID | Severity | Location | Expected | Observed | Evidence | Canonical BUG |
|:---|:---|:---|:---|:---|:---|:---|
| REV-F001 | P0/P1/P2/P3 | `{path:line}` | {contract/REQ} | {fact} | {evidence} | BUG-{id} / pending / N/A |

Blocking findings cannot be repaired in this assignment. They enter
`.claude/skills/bug-resolution/SKILL.md`.

## 4. Checks

| Check | Command / Method | Result | Evidence ref |
|:---|:---|:---|:---|
| {check} | `{command}` | pass / fail / blocked / not_run | `{ref}` |

## 5. Evidence Validity

| Field | Value |
|:---|:---|
| baseline generation | {n} |
| implementation fingerprint | `{sha256/commit}` |
| valid for review round | {n} |
| supersedes | `{evidence-id}` / none |
| invalidated by | `{impact-id}` / none |

## 6. Result

```text
DOCUMENT_PASS / DOCUMENT_FIX_REQUIRED / REQ_CHANGE_REQUIRED
PASS / FIX_REQUIRED
```

Requested lifecycle event: `{event}`
