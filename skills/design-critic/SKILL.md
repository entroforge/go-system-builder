---
name: design-critic
description: Use when design-foundation has produced two or three direction candidates and before asking the human to pick one
category: best-practice
version: 1.0.0
---
# Design Critic

## Authority

Independent review of candidate Design Kernels and Style Tiles. The producer
of the candidates must not declare them adequate. Control questions come from
`blueprint/L4-project-design-foundation.md` §4.7. The prompt-layer rule is
`docs/rules/design-foundation.md`. This Skill does not write Runtime state
and does not replace the human's direction confirmation.

## Applicability

Apply after F2 has produced 2–3 Kernel candidates with Style Tiles, and
before the direction hand-up. Also apply if F5 proofs look consistent but
empty of product character.

## Required Inputs

- `docs/design/research/evidence-field.md` (facts vs inferences vs references)
- Candidate Kernel drafts and their Style Tiles under `docs/design/proof/style-tiles/`
- `docs/rules/design-foundation.md`

Do not treat module `prototypes/` as the definition of the brand.

## Quality Criteria

Score each candidate on all six dimensions. A candidate that fails any one
dimension is not ready for the human.

| Dimension | Pass when |
|:---|:---|
| Truth | Originates in product, user, and business evidence, not a mood word |
| Distinctiveness | Relationships remain recognizable with the logo removed |
| Generativity | Can direct unseen pages and complex states, not only the tile |
| Elasticity | Can change across consumer, operations, and dense views without losing the kernel |
| Usability | Brand expression helps understanding, trust, and control rather than blocking the task |
| Feasibility | Current content, tech, and operations can keep the promise |

Also reject:

- adjective kernels;
- three skins that only swap color, type, and radius;
- candidates that all depend on the same industry cliché;
- a tile that cannot show a failure or empty state in the product's voice.

## Outputs

A written critique, not a winner ribbon:

- recommended candidate and why;
- cost of accepting it;
- why each rejected candidate fails, with keepable fragments;
- missing evidence that would force a return to F1.

Hand this to the human as part of the F2 proposal. Record the same content in
the direction ADR after confirmation.

## N/A Criteria

N/A only when no direction set is being chosen: Foundation is already
`published` and the current REQ only inherits, or the work has no user-visible
UI.

## Stop Conditions

Stop the hand-up when all candidates fail Distinctiveness or Generativity, or
when Evidence Field is mostly inferences marked as facts. Return the producer
to F1/F2 instead of asking the human to pick a skin.

## Non-Goals

- Do not compile Grammar, pick Token values, or draw module packages.
- Do not replace the human's value choice.
- Do not "average" two worlds into a third skin.
- Do not treat screenshot similarity as success.
