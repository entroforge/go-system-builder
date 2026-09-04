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

## Required templates (copy first)

| Template | Live file | When |
|:---|:---|:---|
| `DESIGN-template.md` | `DESIGN.md` (status `draft`) | F3 before publish |
| `research/evidence-field-template.md` | `research/evidence-field.md` | F1 |
| `design-language-template.md` | `design-language.md` | F4 after Kernel confirmation |
| `proof/style-tiles/STYLE-TILE-template.html` | `proof/style-tiles/direction-a.html` … | F2, one HTML per candidate |
| `derivation/DERIVATION-template.md` | `derivation/REQ-{id}.md` | Every `UI impact=changed` REQ, before S2 tracks |

## On-demand templates (create only when needed)

| Template | When |
|:---|:---|
| `surface-profiles/surface-profile-template.md` | Second surface appears; single-surface projects keep §8 in `DESIGN.md` |
| `decisions/ADR-template.md` | Direction / exception / component-proposal records (see `decisions/README.md`) |

`docs/reports/design-foundation/FOUNDATION-REPLAY-template.md` (L6) is filled after two `changed` REQs in a real product, not in this factory.

## Bootstrap

1. Copy `DESIGN-template.md` → `DESIGN.md` and leave status `draft`.
2. Copy `research/evidence-field-template.md` → `research/evidence-field.md`.
3. Copy `design-language-template.md` → `design-language.md` after Kernel confirmation.
4. Run `skills/design-foundation` (F0–F6) before the first `UI impact=changed` REQ.
5. After F4, replace primitive values in `packages/design-tokens/tokens.json` and run `loop-harness design-foundation emit-css --root .`.
6. After publish, each UI REQ writes `derivation/REQ-{id}.md` from `derivation/DERIVATION-template.md`.
7. Optional: `loop-harness design-foundation export-portable --root .` writes a derived Google DESIGN.md under `docs/design/proof/portable/` (not authority).

If the product has no user-visible interface, record Foundation as `N/A` in
`docs/project-map.md` and skip this tree until UI appears.

Authority and procedure: `docs/rules/design-foundation.md`,
`blueprint/L4-project-design-foundation.md`,
`blueprint/L5-project-design-foundation.md`.
