---
name: clean-round-evaluation
description: Use when a complete Delivery Verifier, QA, and E2E Browser review round is ready for pass evaluation
category: methodology
version: 1.1.0
---
# Clean Round Evaluation

## Authority
Only same-round, current, valid evidence may support the result. Guards are defined in `docs/loop-definition.json` (PTR-VERIFY-04 and TR-009); the method summary is inlined below.

## Entry Conditions
- The Loop is in phase `verification.clean_round_evaluation`.
- Delivery Verifier, QA, and E2E Browser workgroups have reported for one review round with complete manifests.
- All Builders have reported and no in-flight change is pending.

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
1. Invoke `loop-harness verification clean-round --root .` to evaluate the four PTR-VERIFY-04 guards.
2. Check `same_review_round`: every referenced evidence belongs to the current round and baseline generation.
3. Check `all_required_dimensions_passed`: every mandatory Delivery, QA, and E2E Browser responsibility has explicit PASS or justified, recorded N/A (silence is not N/A); manifests satisfy team completeness.
4. Check `no_invalidated_pass_evidence`: no required evidence is `invalid` or `superseded`; frozen fingerprints still match.
5. Check `no_open_blocking_bugs`: no blocking BUG remains open; every BUG closed during the round has targeted re-verification evidence.
6. If all four pass: record immutable clean-round evidence and prepare TR-009 (`clean_round_passed` → `acceptance`).
7. If a guard fails correctably (missing dimension, incomplete manifest, mid-round change): request PTR-VERIFY-05 to restart at `verification.delivery` with required corrections.
8. If a guard fails on a blocking finding or integrity conflict: route to `bug-resolution` or pause.
9. Do not compute a weighted score or partial PASS — the result is strictly `pass`, `incomplete`, or `blocked`.

## Outputs
- `clean_round_valid` evidence record (immutable) referenced by ID and hash, or
- `clean_round_incomplete` with failed guard names, specific failing evidence/BUG/fingerprint, and required next actions.

## Exit Conditions
- One complete Delivery + QA + E2E Browser round passes all four guards with no blocking findings, **or**
- The round is declared `incomplete`/`blocked` with a concrete next action.

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

Clean-round evaluation requires baseline generation and review round to match across every record, frozen artifact fingerprints to still match, Delivery, QA, and E2E Browser manifests complete, every required responsibility PASS or evidenced N/A (silence is not N/A), referenced evidence to be valid (no `invalid` or `superseded` PASS evidence), no blocking BUG open, every BUG closed during the round to have targeted re-verification evidence, and no change after the latest required evidence. The four PTR-VERIFY-04 guards (`same_review_round`, `all_required_dimensions_passed`, `no_invalidated_pass_evidence`, `no_open_blocking_bugs`) implement this. Targeted re-verification is performed by the original finding responsibility and may close a BUG but never creates a clean round. ACC and release architecture audit reference the clean-round record by ID and hash; later changes do not rewrite the record — they invalidate it and require a new round. Do not compute a weighted score or partial PASS; the result is strictly `pass`, `incomplete`, or `blocked`.
