# Project Map

> Project: {project name}
> Updated: YYYY-MM-DD
> PM / Architect: {name}
> Runtime: `.claude/loop-state.json`

## 1. Project Facts

| Fact | Value | Status |
|:---|:---|:---|
| goal | {goal} | confirmed / unknown |
| users | {roles} | confirmed / unknown |
| domain | {domain} | confirmed / unknown |
| product surfaces | {consumer / operations / none} | confirmed / unknown |
| stack | {frontend/backend/data/deploy} | confirmed / unknown |
| constraints | {schedule/security/compliance} | confirmed / unknown |

## 2. Human Stage Summary

This section is a human-facing summary. It must not override the Loop runtime.

| Field | Value |
|:---|:---|
| project stage | {S0-S11} |
| basis | {document/evidence links} |
| next human milestone | {milestone} |
| stage check | pass / blocked |
| runtime identity | {runtime-id} / inactive |
| runtime lifecycle | {state.phase} / inactive |
| bound REQ | REQ-{id} / none |

## 3. Baseline Index

| Artifact | Path | Status | Fingerprint / Version |
|:---|:---|:---|:---|
| rules | `docs/rules/README.md` | locked | {version} |
| REQ | `docs/requirements/REQ-{id}.md` | {status} | {version/hash} |
| design investment | `local / core / extended / N/A` | {复用、换手、Surface、风险依据} | upgrade when: {事实触发} |
| design foundation | `docs/design/DESIGN.md` / local / N/A | missing / draft / in-review / provisional / published / superseded | {version or scope; `provisional` is not a lock; `published+PENDING` is a fake lock} |
| design/UI | `{path}` | {status/N/A} | {version/hash} |
| contracts | `docs/contracts/CONTRACTS-{id}.md` | {status} | {version/hash} |
| tasks | `docs/tasks/index.md` | {status} | {version/hash} |
| runtime | `.claude/loop-state.json` | authoritative | {state / phase / next} |

## 4. PM Todo

| REQ | Human stage | Completed evidence | Next action | Owner | Blocker | Acceptance remaining | Runtime ref |
|:---|:---|:---|:---|:---|:---|:---|:---|
| REQ-{id} | {S0-S11} | {links} | {one action} | {role} | {none/item} | {evidence} | `{runtime-id}` |

## 5. Unknowns

| ID | Question | Affected artifact | Status | Decision |
|:---|:---|:---|:---|:---|
| U-001 | {question} | {REQ/design/contract} | open / closed | {decision} |

## 6. Stage Check

| Check | Result | Evidence |
|:---|:---|:---|
| required artifacts exist | pass / blocked | {links} |
| baseline changes are approved | pass / blocked | {change records} |
| runtime and journal are healthy | pass / blocked | `loop-harness doctor --root .` |
| next action is legal in Loop Definition | pass / blocked | {transition/guard} |
