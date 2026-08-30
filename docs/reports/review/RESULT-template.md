# ReviewResult Evidence: RESULT-{id}

> Status: draft / pass / finding / req_change_required / release_blocked / invalidated
> Runtime ref: `.claude/loop-state.json` @ revision {n}
> ReviewPlan: {review-plan-id} (round {n}, revision {n})
> Assignment: {assignment-id} — lens {delivery|qa|e2e}
> Agent: {agent-id}
> Verdict submitted via `runtime review-result submit --assignment-id {assignment-id}`

This Markdown file is the human-readable projection of one Canonical
ReviewResult. The machine authority is the JSON submitted to
`runtime review-result submit` (scaffold: `review-result.example.json`);
this template documents the same fields for review and audit.

## §1 Result identity

| Field | Value |
|:--|:--|
| result_id | review-result-{...} |
| review_plan_id / review_round | {plan} / {round} |
| baseline_generation | {n} |
| subject_digest | sha256 over the plan's frozen_subjects (copy the value from `loop-harness s7 status` — the submit verifier rejects any other value) |
| verification_artifact_digest | cold-start E2E only: sha256 of the workspace content, copied from `loop-harness s7 workspace-digest` after the last spec/fixture write; null otherwise |

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
