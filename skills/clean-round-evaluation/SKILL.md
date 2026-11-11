---
name: clean-round-evaluation
description: Use when the current S7 review round reaches its original exit
category: methodology
version: 1.2.0
---
# S7 Round Exit

## Authority

The original S7 consumer and stage exit use accepted results and responsibility
progress; see `docs/control/agent-protocol.md` #s7 and
`docs/control/loop-definition.json` TR-008/TR-009. This Skill guides Main's
professional coordination; it does not add an evidence gate.

## Entry Conditions

The current S7 ReviewPlan has reviewer responsibilities in progress or reaching
their original handoff. Main uses the current work and accepted answers.

## Required Inputs

The current ReviewPlan, the applicable original responsibilities, their accepted
conclusions, and any actual unfinished work are sufficient for the original exit.

## Procedure

1. Main coordinates the independent responsibilities and gives each reviewer the
   needed materials. Each reviewer performs assigned professional checks and saves
   their own original answer. PreToolUse observes it; successful PostToolUse and
   Stop provide fallback through the same consumer. No extra submission is needed.
2. An accepted Finding goes to S8 with actual observations and outstanding
   responsibilities. Main decides which other safe work may continue; dangerous
   P0 work stops. Already dispatched reviewers can still deliver their original
   answers after S8 begins.
3. When applicable required responsibilities are complete and accepted conclusions
   have no Finding, the original exit advances to S10. Main retains responsibility
   for real coverage, independence and unfinished work. A green check count does
   not itself establish an author's verdict.
4. Environment blockers and unfinished work go to Main as stated; they do not
   become PASS or a fabricated product Finding. Actual pause or human decisions
   remain with their original authority.
5. If transition or preparation fails, preserve accepted answers and continue the
   specific unfinished system followup. A status command can diagnose a concrete
   gap, but is not a normal prerequisite.

## Outputs

Original accepted conclusions, applicable responsibility progress, and the
actual S8 or S10 handoff or its specific unfinished followup.

## Exit Conditions

The appropriate original handoff has advanced and prepared its next materials,
or the concrete unfinished part has been reported to its responsible owner.

## Stop Conditions

Stop unsafe work for a real P0 or other actual safety blocker. Report the original
facts and missing conditions to Main; continue other safe assigned work where
appropriate. Preserve real pause and human-decision boundaries.

## Non-Goals

Do not require an ObservationBatch, separate CleanRound snapshot, manifest or
fingerprint proof, evidence revalidation, resave, submit, returned path or second
Stop. Existing historical records remain usable when needed. S9 Targeted does not
replace a new complete S7 round; S10 acceptance and S11 release remain separate
professional and human decisions.
