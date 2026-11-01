# QA Evidence: QA-{id}

> Status: draft / pass / finding / req_change_required / release_blocked / invalidated (ReviewResult verdict vocabulary; save the supplied original answer)
> Runtime ref: `{runtime-id}`
> Review round: {n}
> Workgroup manifest: `{team-manifest-path}`
> Assignment: `{assignment-id}`
> Responsibility: {QA responsibility ID}
> Best Practice: `.claude/skills/{skill}/SKILL.md`
> Agent: `{agent-id}`
> Activation: `{activation-ref}`

This Markdown is the human-readable projection; the machine authority is the
Canonical ReviewResult JSON you submit (`internal/schema/assets/review-result.example.json`
is the scaffold; `docs/reports/review/RESULT-template.md` documents the same fields lens-neutrally).

## 1. Inputs

| Kind | ID / Scope | Path | Version |
|:---|:---|:---|:---|
| REQ | REQ-{id} | `docs/requirements/REQ-{id}.md` | {version} |
| TASK | TASK-{id} | `docs/dev/tasks/TASK-{id}.md` | {version} |
| source/tests | {scope} | `{path}` | {version if relevant} |
| module current truth | {module} | `docs/design/prototypes/{module}/` | current |
| Skill | {skill} | `.claude/skills/{skill}/SKILL.md` | {version} |

## 2. Assigned Quality Conclusion

Each report evaluates one responsibility. Team coverage and N/A decisions live
in the manifest, not in this report.

| Criterion | Evidence | Result |
|:---|:---|:---|
| {criterion from assigned Best Practice} | {path/command/sample} | PASS / FAIL / N/A |

### Scenario quality gates

| Check | Expected | Observed | Result | Evidence |
|:---|:---|:---|:---|:---|
| required allow branches | 100% | {n}/{n} | PASS / FAIL | `scenario-coverage.json` |
| required reject branches | 100% | {n}/{n} | PASS / FAIL | `scenario-coverage.json` |
| positive/negative capacity | module profile minimum | {ratio} | PASS / FAIL | `scenario-coverage.json` |
| CASE → Story → PATH → Spec | no orphan | {observed} | PASS / FAIL | module files / `web/e2e/{module}/` |
| module regression | complete current module set | {observed} | PASS / FAIL | round evidence |

## 3. Findings

| Finding ID | Severity | Location | Finding | Evidence | Canonical BUG |
|:---|:---|:---|:---|:---|:---|
| QA-F001 | P0/P1/P2/P3 | `{path:line}` | {fact} | {evidence} | BUG-{id} / pending / N/A |

## 4. Checks

| Check | Command / Method | Result | Evidence ref |
|:---|:---|:---|:---|
| {check} | `{command}` | pass / fail / blocked / not_run | `{ref}` |

## 5. Targeted Re-verification

Repair-rounds only (TR-012 re-entry): in a first-round report with no repaired BUGs,
omit this table entirely. This table **coexists** with §2–§4 — a repair round still
carries the full assigned-quality conclusion AND a Targeted Re-verification row per
closed BUG. Targeted re-verification alone never satisfies the machine CleanRound — only
a fresh complete round does (`docs/control/agent-protocol.md` #s7 / #s9).

| BUG | Original assignment | Repair reference | Result | Evidence |
|:---|:---|:---|:---|:---|
| BUG-{id} | `{assignment-id}` | `{actual repair reference}` | pass / fail | `{ref}` |

Targeted re-verification does not satisfy a complete clean round.

### Worked repair-round example

This example applies only to a repair-round report (TR-012 re-entry): a
first-round report with no repaired BUGs omits §5 entirely and should not
imitate the row below. The following is the minimum shape, not a replacement
for the rest of this report — the same repair-round QA report still fills
§2–§4 for the complete Assignment:

| BUG | Original assignment | Repair reference | Result | Evidence |
|:---|:---|:---|:---|:---|
| BUG-042 | `assignment-qa-logic-state-error` | `repair-result-042` | pass | `runtime:reverify-042` |

The row answers only whether the repaired causal assertion was independently
re-verified. It does not waive the QA conclusion, checks, or Claim coverage in
§2–§4; a later `runtime s7` submission still needs the full Claim set.

## 6. Evidence Validity

| Field | Value |
|:---|:---|
| baseline generation | {n} |
| review round | {n} |
| supersedes | `{evidence-id}` / none |
| invalidated by | `{impact-id}` / none |

## 7. Result

```text
pass / finding / req_change_required / release_blocked
```

Requested lifecycle event: `{event}`
