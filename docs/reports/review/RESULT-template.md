# ReviewResult Evidence: RESULT-{id}

> Status: draft / pass / finding / req_change_required / release_blocked / invalidated
> Runtime ref: `.claude/loop-state.json` ({runtime-id})
> ReviewPlan: {review-plan-id} (round {n}, revision {n})
> Assignment: {assignment-id} — lens {delivery|qa|e2e}
> Agent: {agent-id}
> Verdict: 由原答卷及原消费者接收记录投影

本 Markdown 是原 ReviewResult 的可读投影，不是必须另交的第二份结果。正常 S7 在已发行原卷中填写本人专业 `answer` 并保存，由适用的自然工具事件和 Stop 兜底交原消费者接收。身份按原责任承接，不要求作者计算、登记或抄写摘要。显式 Canonical Result 入口只用于具名兼容／恢复。

## §1 Result identity

| Field | Value |
|:--|:--|
| result_id | review-result-{...} |
| review_plan_id / review_round | {plan} / {round} |
| baseline_generation | {n} |

本地证据在原卷中选择实际材料路径、命令或观察。原消费者接收本人专业表达与已有责任，不重读证据或校验摘要；实际观察与未知由本人如实说明。

## §2 Claim results (exact set)

One row per Claim in the Assignment — no missing entries, no extras.
`not_applicable` never appears here; N/A is a ReviewPlan disposition.

| claim_id | conclusion | observed | evidence refs |
|:--|:--|:--|:--|
| claim-{...} | pass / fail | {what was actually observed} | `{refs}` |

## §3 Checks

| command | result | evidence refs |
|:--|:--|:--|
| {command} | pass / fail | `{log-ref}` |

## §4 Findings (one per fail Claim)

Each Finding is immutable and carries the real encounter. The failure
boundary is `last_good_checkpoint → wall_action → first_bad_checkpoint`;
S8 investigates from it without re-reproducing the symptom.

| field | value |
|:--|:--|
| finding_id | finding-{...} |
| claim_id / lens / severity | {claim} / {lens} / {P0..P3} |
| expected (authority_refs) | {expected} (`{REQ/contract/CASE refs}`) |
| observed | {observable symptom, no cause judgment} |
| observation_mode | user_flow / api_flow / command_flow / state_transition / code_inspection |
| encounter.journey_summary | {entry/goal → key action → wall action → first anomaly/terminal} |
| encounter.last_good / wall / first_bad | {...} / {...} / {...} |
| evidence_refs / correlation_refs | `{typed refs}` / `{ids}` |
| reproducibility | always / intermittent / once_with_deterministic_trace |

Never record here: root cause, repair scope, suggested fix, canonical BUG
mapping — those belong to S8.

## §5 Deviations and validity

| field | value |
|:--|:--|
| deviations | {none / list} |
| supersedes / invalidated by | {result ids or none} |

## §6 Verdict

pass / finding / req_change_required / release_blocked
(the verdict drives the automatic route: continue, TR-008, TR-010, TR-011)
