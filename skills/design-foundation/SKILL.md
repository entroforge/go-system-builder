---
name: design-foundation
description: Use when a project has user-visible UI but Project Design Foundation is missing, stale, or cannot cover a new surface
category: methodology
version: 1.3.0
---
# Design Foundation

## Authority

Prompt-layer rule: `docs/rules/design-foundation.md`.
Templates: `docs/design/DESIGN-template.md` and siblings under `docs/design/`.
Checker: `loop-harness design-foundation check` (advisory; not on `validate --all`).
This Skill is Loop-external. It does not move a Runtime cursor and is not an
S0–S11 stage. Stage contracts remain in `docs/agent-protocol.md`.

## Entry Conditions

- The product includes user-visible screens, visual behavior, copy, motion,
  images, or visible states.
- `docs/design/DESIGN.md` is missing, `draft`, stale, or does not cover the
  surface about to change.
- Typical moments: first project understanding; first likely
  `UI impact=changed` REQ; a new product surface; repeated default-UI drift.
  F0 still chooses Local vs Core vs Extended — a single throwaway screen is
  Local and does not publish a Project Foundation.

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
before continuing. Chat is not the authority. Full path uses three confirmations
(direction, kernel, publish). Thin path (F2 below) merges them into one packet.

### F0 Identify

Read `DESIGN.md` if it exists. Decide: missing / draft / in-review / provisional /
published-and-covering / published-but-uncovered-surface / superseded / N/A.
(`draft`→`in-review`→`provisional`→`published`→`superseded`; only `published` with dates recorded is a lock.)

Choose the investment tier **before** F1. Record the reason in
`docs/project-map.md` (`local` / `core` / `extended` / `N/A`):

- **Local** — one-shot UI, one surface, no planned second `changed` REQ and
  no handoff. Do **not** publish a Project Foundation. Write a module-local
  note that must not be promoted. Exit this Skill.
- **Core** — later UI REQs, multi-session handoff, or long-lived UI. Continue
  F1. Prefer thin path when the ritual is already a fact.
- **Extended** — extra surfaces, high-risk commitments, or a design system
  being built. Continue F1; full F2 remains available.

Do not treat “there is UI” as “run F1–F6”. If published and covering, exit
and let S0/S2 consume §0. Otherwise copy templates into live files only for
Core/Extended.

### F1 Discover

Do **not** start from reference screenshots. Split facts / inferences /
references / unknowns. Name the ritual or category reversal if it is already
in the brief.

**Thin path:** if the brief already states a transferable ritual as fact and
that fact names the product role, **do not copy** `evidence-field-template.md`.
Cite the brief (or `project-map.md`) in §0 Source. Copying the brief into six
tables is stop-worthy waste.

**Full path only:** copy `docs/design/research/evidence-field-template.md` when
F1 has not named a ritual. External references must be split into surface /
mechanism / transfer condition / local translation. Later REQs still do not
read this file.

### F2 Direct

Choose a path from the named ritual (brief or Evidence Field), then stop inventing worlds.

**Thin path (default when the ritual is already a fact):** if the brief — or,
only on the full path, `evidence-field.md` — marks a transferable service
ritual or category reversal as a *fact* (not an inference), and that fact
already names the product role, do **not** invent two extra worlds to satisfy
a quota. Deliver:

1. recommended world = digital translation of that ritual;
2. rejected foil = the category default (ticker board, dual buy, victory green,
   library-demo primary);
3. one Style Tile for the recommended world only.

Load `design-critic`. The critic must kill any extra candidate that shares the
same product role as the evidenced ritual. Human confirmation may merge with
F3/F6 into one packet.

**Full path:** only when F1 has not named a ritual, or two worlds would bet
different product values. Then produce 2–3 Kernel candidates. Differences must
land in at least three of: product role, information order, user relationship,
aesthetic world, interaction posture, where brand expression appears. Each
candidate gets one Style Tile from
`docs/design/proof/style-tiles/STYLE-TILE-template.html`. Each candidate must
use at least two creativity sources: evidence amplification, ritual migration,
category reversal, cross-domain analogy, constraint-as-asset,
counterexample-first. Three recolors are a stop, not a hand-up. Load
`design-critic` before the hand-up. Human confirmation 1: which design world.

### F3 Converge

Rewrite the chosen world into a short Kernel in `DESIGN.md`. Fill **§0
Next-agent card first**. The card is incomplete unless it states, without
hex as brand:

1. the action semantic role (what the one solid CTA *is*);
2. how values **and** status tags are colored (library success/danger is a
   decision, not a default);
3. which commitments are buttons and which may only be sentences.

Then Thesis on Extended or when the human must pick among worlds;
Core+thin may keep Thesis to one exclusive sentence. 2–3 tensions, 3–7 Laws
with Do/Don't, anti-principles. Adjective kernels fail and return to F1/F2.
A Kernel without a complete §0 is an empty shell. Human confirmation 2 (or
the thin-path merged packet): the card, then thesis/tensions if present.

### F4 Compile

On **Core + thin**, it is enough to put ROLE/GR ids in the card Binding
column. Leave `design-language.md` as `debt` until a second surface or an
upgrade to Extended. Do not compile nine dimensions to look complete.

On Extended, or Core+full: write `design-language.md`. A single surface stays
**inline in `DESIGN.md` §8** (`Profile/version` = `inline`). Copy
`surface-profiles/surface-profile-template.md` only from the **second** surface
on. Do not create `surface-profiles/consumer.md` for the first screen.
Grammar states relationships, not hex values. Every important Law leaves the
chain Evidence → Law → Grammar rule → Surface adaptation → Proof. Map compiled
roles onto `packages/design-tokens/tokens.json` in the **target project**
(keep semantic names; replace primitive values). Run
`loop-harness design-foundation emit-css --root .` there. Do not treat the
template factory `packages/design-tokens/` as a product brand.

### F5 Prove

Build the minimum Proof Set: selected Style Tile, one anchor screen, one
stress screen (density, long content, error, or permission), and the contrast
needed if more than one Surface exists. Review top-down: world → laws →
shared character → justified surface change → then pixels. Human reviews
macro questions only.

If the anchor works but the stress screen fails, return to F4. If everything
is consistent but does not improve understanding, trust, or task outcome,
return to F1/F3.

### F6 Publish

Do **not** set `published` while direction/kernel/publish confirmations are
PENDING. Use `provisional` until a human date is recorded. `provisional` is
not a lock for later REQs.

When confirmed: set `published`, record version, open design debt, and
confirmation dates. Write a direction ADR under `docs/design/decisions/` with
chosen world, cost, rejected foil or worlds, and keepable fragments. Human
confirmation 3 (or the thin-path merged packet): later REQs can inherit from
§0 plus Derivation. Success is an inheritance trial on a later `changed` REQ,
not template completeness. Record that trial in the target product with
`docs/reports/design-foundation/FOUNDATION-REPLAY-template.md`. A wired
primary button is an implementation duty, not a Foundation score.

Then resume the paused S0 funnel or project-map update. Do not start batch
module HTML against a Core/Extended Foundation that is still `draft`. Local
may ship the one page without a Project Kernel. `provisional` is not a lock
for later REQs.

Bounded rollback stays on this single chain. Replay only the damaged
downstream steps.

### Feedback after publish

After F6, when any S7 Finding, counterevidence row, visual-qa result, or
later REQ contradicts or falsifies an active
`LAW-* / ANTI-* / INV-* / GR-* / ROLE-* / PAT-* / SUR-*` or shows an
`EX-*` boundary is wrong, do **not** rerun F1–F6. Enter the feedback
transaction — it is not closed by completing a report:

1. **Record fact** — source observation (Finding ID / counterevidence row /
   visual-qa / `REQ-*` `PROOF-*`), violated or falsified constraint IDs,
   and `REQ-* / PROOF-*` path.
2. **Classify** — local fix (module package only) / module Pattern
   or `CP-*` candidate / global Grammar/Token/Component extension
   (`ADR-*` / `DFD-*`) / scoped `EX-*` / Kernel breaking change (human
   re-confirm).
3. **Update only the classified authority** — list affected edges
   `Grammar / Surface / Derivation / Token / component / Proof`; do not
   silently promote a local composition to global.
4. **Replay** — re-run affected-edge reference/binding checks
   (`loop-harness design-foundation check`) and one minimal replay (second
   `changed` REQ cold-start or affected Proof state).
5. **Close on evidence** — write the 6-field receipt into the carrier
   (`Source observation / Affected constraint IDs / Classification /
   Changed edges / Replay evidence / Status open/closed`); checker validates
   refs/paths, human judges whether the Kernel should change. `open` until
   both pass.

Carriers reuse existing authority (no new Feedback DB): local fix → source
Finding + repair/verification evidence; module pattern → `CP-*`; global
extension → `ADR-*`/`DFD-*` + updated tables; exception → `EX-*`; breaking
change → `DFD-*` + human re-publish. This Skill, `docs/agent-protocol.md` #s7,
and `skills/acceptance-and-handoff` are the S7/S10 entry points; filling
`docs/reports/design-foundation/FOUNDATION-REPLAY-template.md` after two REQs
does not count as wired.

## Outputs

Local: a module-local note only; no Project `DESIGN.md`.

Core/Extended:

- Human-confirmed `docs/design/DESIGN.md` (`provisional` until confirmed,
  then `published`; §0 + §8 filled; Core+thin may skip §1–7)
- `docs/design/research/evidence-field.md` only on the full F2 path
- `docs/design/design-language.md` or an explicit Grammar `debt` on Core+thin
- Surface row in `DESIGN.md` §8 (`inline`); profile files from the second surface on
- Proof: one tile and/or the first REQ page
- Direction ADR under `docs/design/decisions/` when published
- Target-project tokens only when F4 compiled roles; never brand-lock factory tokens
- Open debt listed in `DESIGN.md`

## Exit Conditions

- Local: module-local note written; Skill exited; no Project Foundation.
- Core/Extended: `DESIGN.md` §0 is complete (action role, value/status color,
  button vs sentence) and has no hex brand lock.
- Human confirmed, or status is `provisional` and later REQs must not treat
  it as a lock.
- `published` is used only after confirmation dates are recorded.
- Core+thin may omit a full `design-language.md`.
- A later REQ can inherit from §0 plus Derivation.

## Stop Conditions

- Human has not confirmed direction, kernel, or publish — wait; do not invent
  a lock; do not set `published`.
- Candidates are the same world with different paint — return to F1.
- Extra worlds share the evidenced ritual's product role — drop them; use thin
  path, do not hand up a quota of three.
- Thesis cannot generate or refuse unseen pages — return to F2/F3.
- Agent is being asked to paint every page before Kernel exists — refuse and
  stay on F0–F6.

## Non-Goals

- Do not add a Loop stage, cursor, or Runtime gate.
- Do not draw every page, freeze Token primitives before F4, or treat a UI library as Kernel.
- Do not rewrite S2 module packages here; published Foundation is an input to S2.
  Foundation does not replace architecture, data, or contracts.
- Do not judge aesthetic quality by field completeness. Do not invent two extra
  worlds when the evidenced ritual already names the product role.
- Do not decide product value on the human's behalf.
- Do not promote a construction hex or library Primary into the Kernel.
- Do not treat fewer hexes or shorter HTML as architecture.
- Do not score Foundation on whether a button has a click handler.
