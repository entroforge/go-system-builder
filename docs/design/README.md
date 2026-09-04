# Project Design Foundation

This directory holds the project-level design language **and** the existing
module design packages. They are not the same authority.

| Layer | Path | Answers | Does not answer |
|:---|:---|:---|:---|
| Foundation (Loop-external) | `DESIGN.md` + siblings below | Why the product looks and behaves this way across REQs | A single REQ's pages or component APIs |
| Module package (S2) | `prototypes/<module>/` | The current module's stories, flows, cases, and HTML | The brand Kernel |

Copy the templates into live files when the target project has user-visible UI.
Do not fill Kernel, Grammar, or Proof in this template repository.
HTML is the only legal carrier for Style Tiles and Screens; Figma links are not authority.

Later `UI impact=changed` agents open **two files**: `DESIGN.md` §0 and §8,
plus `derivation/REQ-{id}.md`. They do not search `research/`, Grammar, or
`surface-profiles/` unless §0 cannot answer Must not.

## Required templates (copy first)

| Template | Live file | When |
|:---|:---|:---|
| `DESIGN-template.md` | `DESIGN.md` (status `draft`, then `provisional` until confirmed) | Core/Extended F3; Local does not copy this |
| `proof/style-tiles/STYLE-TILE-template.html` | `proof/style-tiles/direction-a.html` … | F2: one HTML per candidate on the full path; thin path = one recommended tile |
| `derivation/DERIVATION-template.md` | `derivation/REQ-{id}.md` | Every `UI impact=changed` REQ, before S2 tracks |

## On-demand templates (create only when needed)

| Template | When |
|:---|:---|
| `research/evidence-field-template.md` | Full F2 only — ritual is not yet a fact in the brief. Thin path: do not copy |
| `design-language-template.md` | Extended, or Core when a second surface appears. Core+thin: debt |
| `surface-profiles/surface-profile-template.md` | **Second** surface only. First surface stays `inline` in `DESIGN.md` §8 |
| `decisions/ADR-template.md` | Direction / exception / component-proposal records (see `decisions/README.md`) |

`docs/reports/design-foundation/FOUNDATION-REPLAY-template.md` is filled after two `changed` REQs in a real product, not in this factory. Do not check trial HTML or a product Kernel into this repository.

## Bootstrap

1. Run `skills/design-foundation` F0 first. Record `local / core / extended / N/A`
   in `docs/project-map.md`. A one-shot screen with no handoff is Local: write a
   module Derivation only; do not copy `DESIGN-template.md`.
2. Core/Extended: copy `DESIGN-template.md` → `DESIGN.md` as `draft`. Do **not**
   copy Evidence Field or a Surface Profile on the thin path.
3. Copy `design-language-template.md` only on Extended, or when Core grows a
   second surface. Core+thin may keep Grammar as debt.
4. Fill §0 (action role, value/status color, button vs sentence) before pages.
   Confirmation still PENDING → status `provisional`, never `published`.
5. After F4 in the **target** project, replace primitive values in that project's
   `packages/design-tokens/tokens.json` and run `loop-harness design-foundation emit-css --root .`.
   Do not brand-lock this factory's tokens.
6. After a dated publish, each later UI REQ writes `derivation/REQ-{id}.md`.
7. Optional: `loop-harness design-foundation export-portable --root .` writes a derived Google DESIGN.md under `docs/design/proof/portable/` (not authority).

If the product has no user-visible interface, record Foundation as `N/A` in
`docs/project-map.md` and skip this tree until UI appears.

Authority and procedure: `docs/rules/design-foundation.md`,
`skills/design-foundation/SKILL.md`.
