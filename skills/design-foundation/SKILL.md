---
name: design-foundation
description: Use when a project has user-visible UI but Project Design Foundation is missing, stale, or cannot cover a new surface
category: methodology
version: 1.1.0
---
# Design Foundation

## Authority

Control logic: `blueprint/L4-project-design-foundation.md`.
Landing spec: `blueprint/L5-project-design-foundation.md`.
Prompt-layer rule: `docs/rules/design-foundation.md`.
Templates: `docs/design/DESIGN-template.md` and siblings under `docs/design/`.
This Skill is Loop-external. It does not move a Runtime cursor and is not an
S0–S11 stage. Stage contracts remain in `docs/agent-protocol.md`.

## Entry Conditions

- The product includes user-visible screens, visual behavior, copy, motion,
  images, or visible states.
- `docs/design/DESIGN.md` is missing, `draft`, stale, or does not cover the
  surface about to change.
- Typical moments: first project understanding; first likely
  `UI impact=changed` REQ; a new product surface; repeated default-UI drift.

Do not enter for pure backend or non-visible work. Read an existing Foundation
if present, then continue the current stage.

## Required Inputs

| Input | Path / source | Why |
|:---|:---|:---|
| Product facts | `docs/project-map.md`, conversation, existing brand/service evidence | F1 evidence |
| Foundation templates | `docs/design/*-template.md` | D4 structure |
| Rule | `docs/rules/design-foundation.md` | default behavior and forbidden paths |
| Existing module packages | `docs/design/prototypes/<module>/` if any | stress samples, never Kernel source |
| Direction critic | `skills/design-critic/SKILL.md` | independent review before the human picks a world |

## Procedure

Human confirmations are conversation duties, not Runtime gates. After each
confirmation, write the record into `DESIGN.md` and `docs/design/decisions/`
before continuing. Chat is not the authority.

### F0 Identify

Read `DESIGN.md` if it exists. Decide: missing / draft / published-and-covering /
published-but-uncovered-surface / N/A. If published and covering, exit this
Skill and let S0/S2 consume it. Otherwise copy templates into live files and
continue.

### F1 Discover

Fill `docs/design/research/evidence-field.md`. Split facts / inferences /
references / unknowns. Look for product essence, user relationship, business
evidence, service rituals, category defaults, and constraints. Do not start
from reference screenshots. External references must be split into surface /
mechanism / transfer condition / local translation.

### F2 Direct

Produce 2–3 Design Kernel candidates. Differences must land in at least three
of: product role, information order, user relationship, aesthetic world,
interaction posture, where brand expression appears. Each candidate gets one
Style Tile from `docs/design/proof/style-tiles/STYLE-TILE-template.html`.

Each candidate must use at least two creativity sources: evidence amplification,
ritual migration, category reversal, cross-domain analogy, constraint-as-asset,
counterexample-first. Three recolors are a stop, not a hand-up.

Load `design-critic` and revise before asking the human. Hand up the critic
result plus a recommendation. Human confirmation 1: which design world.

### F3 Converge

Rewrite the chosen world into a short Kernel in `DESIGN.md`: Thesis (L4 §4.2
sentence), 2–3 tensions with bias and bounds, 3–7 Laws with Do/Don't, anti-
principles, signature relations. Adjective kernels ("modern, simple, tech")
fail and return to F1/F2. Human confirmation 2: thesis, tensions, laws,
anti-principles.

### F4 Compile

Write `design-language.md` and at least one Surface Profile. Grammar states
relationships, not hex values. Every important Law leaves the chain
Evidence → Law → Grammar rule → Surface adaptation → Proof. Define invariants,
variants, selection rules, and exceptions. Map compiled roles onto
`packages/design-tokens/tokens.json` (keep semantic names; replace primitive
values). Run `loop-harness design-foundation emit-css --root .`.

### F5 Prove

Build the minimum Proof Set: selected Style Tile, one anchor screen, one
stress screen (density, long content, error, or permission), and the contrast
needed if more than one Surface exists. Review top-down: world → laws →
shared character → justified surface change → then pixels. Human reviews
macro questions only (L4 §6.3).

If the anchor works but the stress screen fails, return to F4. If everything
is consistent but does not improve understanding, trust, or task outcome,
return to F1/F3.

### F6 Publish

Set `DESIGN.md` status to `published`, record version, open design debt, and
the three confirmation dates. Write a direction ADR under
`docs/design/decisions/` with chosen world, cost, rejected worlds, and
keepable fragments. Human confirmation 3: the Proof Set is enough for later
REQs to inherit.

Then resume the paused S0 funnel or project-map update. Do not start batch
module HTML before publish.

Bounded rollback stays on this single chain. Replay only the damaged
downstream steps (L4 §2.3).

## Outputs

- `docs/design/research/evidence-field.md`
- Human-confirmed `docs/design/DESIGN.md` (status `published`)
- `docs/design/design-language.md`
- One or more `docs/design/surface-profiles/*.md`
- Proof Set under `docs/design/proof/`
- Direction ADR under `docs/design/decisions/`
- Updated `packages/design-tokens/tokens.json` + generated `tokens.css`
- Open debt listed in `DESIGN.md`

## Exit Conditions

- Thesis is generative and exclusive; tensions, 3–7 laws, and anti-principles
  are confirmed.
- At least one Surface Profile exists.
- Style Tile plus one anchor and one stress proof exist.
- Human has confirmed direction, kernel, and publish.
- `DESIGN.md` status is `published` and later REQs can cite its version.

## Stop Conditions

- Human has not confirmed direction, kernel, or publish — wait; do not invent
  a lock.
- Candidates are the same world with different paint — return to F1.
- Thesis cannot generate or refuse unseen pages — return to F2/F3.
- Agent is being asked to paint every page before Kernel exists — refuse and
  stay on F0–F6.

## Non-Goals

- Do not add a Loop stage, cursor, or Runtime gate.
- Do not draw every page, freeze Token primitives before F4, or treat a UI library as Kernel.
- Do not rewrite S2 module packages here; published Foundation is an input to S2.
- Do not judge aesthetic quality by field completeness.
- Do not decide product value on the human's behalf.
