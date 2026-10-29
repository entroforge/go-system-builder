# ReviewResult Evidence: RESULT-{id}

> Status: draft / pass / finding / req_change_required / release_blocked / invalidated
> Runtime ref: `.claude/loop-state.json` ({runtime-id})
> ReviewPlan: {review-plan-id} (round {n}, revision {n})
> Assignment: {assignment-id} — lens {delivery|qa|e2e}
> Agent: {agent-id}
> Verdict: 由原答卷及原消费者接收记录投影

本 Markdown 是原 ReviewResult 的可读投影，不是必须另交的第二份结果。正常 S7 在已发行原卷中填写本人专业 `answer`，保存并返回准确引用，由 `SubagentStop` 调原消费者接收。下表的身份、轮次、代际和摘要均从原记录投影，作者不计算、抄写或拼装机器封套。保留的显式 Canonical Result 入口只用于具名兼容／恢复，不是正常原卷的前置。

## §1 Result identity

| Field | Value |
|:--|:--|
| result_id | review-result-{...} |
| review_plan_id / review_round | {plan} / {round} |
| baseline_generation | {n} |
| subject_digest | 系统绑定原计划的冻结对象摘要；作者保留原卷机器预填，不抄 Runtime |
| verification_artifact_digest | 冷启动 E2E 的实际资产摘要，由原接收器绑定并校验；原卷预填或 null 均不要求作者计算 |

本地证据在原卷中选择 `path:<实际相对路径>` 或 `command_output:<原实际捕获路径>`。原接收／捕获机制固定摘要、字节数和截断信息，消费时核对原来源，不能刷新旧摘要掩盖漂移。

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
