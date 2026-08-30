---
name: clean-round-evaluation
description: Use when the S7 round nears its exit — inspect the machine CleanRound evaluation over the ReviewPlan's exact Claim set (read-only; the round consumer owns the write)
category: methodology
version: 1.1.0
---
# Clean Round Evaluation

## Authority
The CleanRound is machine-owned (L3-S7 §10, `docs/loop-definition.json` TR-009 / `clean_round_valid`): the round consumer registers the snapshot inside the final `runtime review-result submit`, and TR-009's guard recomputes the full conjunction at promotion time. This skill is the read-only inspection entry — nobody hand-writes a clean-round record or an aggregate PASS.

## Entry Conditions
- The Loop is in phase `verification.clean` (the consumer already closed the round), or `verification.running` with the final Claim Result about to land.
- A ReviewPlan is registered for the current round and every required Claim has a consumed disposition (pass, or finding — finding rounds go to TR-008, never clean).
- No in-flight change is pending.

## Required Inputs
| Input | Path / field | Why |
|:---|:---|:---|
| Team manifests | runtime `entities.teams[]` manifests | team completeness and responsibility coverage |
| Dimension evidence | runtime `evidence[]` Delivery/QA/E2E results | PASS/N/A per responsibility |
| Evidence validity | each evidence entry `status` and `review_round` | reject invalid or mixed-round evidence |
| Open BUGs | runtime `entities.bugs[]` | no blocking BUG may remain open |
| Frozen fingerprints | runtime artifact fingerprints for the round | detect mid-round change |
| Baseline generation | runtime `baseline.generation` | must match across every record |

## Procedure
1. Invoke `loop-harness s7 status --root .` for the claim-level board, then `loop-harness verification clean-round --root .` for the machine evaluation.
2. Check `review_plan_clean`: the ReviewPlan belongs to the current round and closed clean (a plan that never closed cannot be promoted).
3. Check `all_required_claims_pass`: every required Claim has a consumed pass Result; N/A Claims carry their plan-level disposition with source and rationale.
4. Check `no_findings_current_round`: any current-round Finding forecloses the clean path — that round goes to S8 via TR-008.
5. Check `no_invalidated_pass_evidence`: no current-round review evidence (result / finding / batch / snapshot) is invalid.
6. Check `no_open_blocking_bugs`: no blocking BUG remains open; every BUG closed during the round has targeted re-verification evidence.
7. Check `clean_round_snapshot_registered`: the machine snapshot exists as current-round evidence.
8. All pass → let the hook commit TR-009 (`verification.clean` → `acceptance`). A correctable gap → produce the missing Claim Result via `runtime review-result submit`; an integrity conflict → the round is stale, start a new round.
9. Do not compute a weighted score or partial PASS — the result is strictly `pass` or not.

## Outputs
- `clean_round_valid` evidence record (immutable) referenced by ID and hash, or
- `clean_round_incomplete` with failed guard names, specific failing evidence/BUG/fingerprint, and required next actions.

## Exit Conditions
- The machine CleanRound exists and TR-009 promotes the round into acceptance, **or**
- The board names the exact unproven Claim / integrity fact with its next action.

## Stop Conditions
Stop immediately and surface to the human if any of:
- Evidence mixes review rounds or baseline generations.
- A required dimension is missing PASS or justified N/A.
- Manifests are incomplete or team coverage is missing.
- Required PASS evidence is `invalid` or `superseded`.
- A blocking BUG is open.
- A mid-round change invalidated the frozen fingerprint set.

## Non-Goals
- Do not treat targeted re-verification as satisfying any guard.
- Do not rewrite an existing clean-round record on later change — invalidate and require a new round.
- Do not decide acceptance or release — that belongs to `acceptance-and-handoff`.

## Inlined Methodology

Machine CleanRound evaluation (L3-S7 §10) is a strict conjunction over the current ReviewPlan: the plan belongs to this round and closed clean; every required Claim has a consumed pass Result (plan-level N/A carries source and rationale — silence is not N/A); no current-round Finding exists; no current-round review evidence is invalid; no blocking BUG is open and every BUG closed during the round has targeted re-verification evidence; and the machine snapshot is registered as evidence. TR-009's `clean_round_valid` guard recomputes all of it at promotion. Targeted re-verification is performed by the original finding responsibility and may close a BUG but never creates a clean round. ACC and release audit reference the clean-round snapshot by ID and hash; later changes do not rewrite it — they invalidate it and require a new round. Nobody computes a weighted score or partial PASS: the result is strictly `pass` or not.
