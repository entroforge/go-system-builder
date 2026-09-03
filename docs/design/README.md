# Project Design Foundation

This directory holds the project-level design language **and** the existing
module design packages. They are not the same authority.

| Layer | Path | Answers | Does not answer |
|:---|:---|:---|:---|
| Foundation (Loop-external) | `DESIGN.md` + siblings below | Why the product looks and behaves this way across REQs | A single REQ's pages or component APIs |
| Module package (S2) | `prototypes/<module>/` | The current module's stories, flows, cases, and HTML | The brand Kernel |

Copy the templates into live files when the target project has user-visible UI.
Do not fill Kernel, Grammar, or Proof in this template repository.

## Bootstrap

1. Copy `DESIGN-template.md` → `DESIGN.md` and leave status `draft`.
2. Copy `research/evidence-field-template.md` → `research/evidence-field.md`.
3. Copy `design-language-template.md` → `design-language.md` after Kernel confirmation.
4. Copy `surface-profiles/surface-profile-template.md` for each product surface.
5. Run `skills/design-foundation` (F0–F6) before the first `UI impact=changed` REQ.
6. After F4, replace primitive values in `packages/design-tokens/tokens.json` and run `loop-harness design-foundation emit-css --root .`.
7. After publish, each UI REQ writes `derivation/REQ-<id>.md` from `derivation/DERIVATION-template.md`.
8. Optional: `loop-harness design-foundation export-portable --root .` writes a derived Google DESIGN.md under `portable/` (not authority).
9. Observe real-product use with `research/FOUNDATION-REPLAY-template.md` (L6). Do not fill Kernel in this template repository.

If the product has no user-visible interface, record Foundation as `N/A` in
`docs/project-map.md` and skip this tree until UI appears.

Authority and procedure: `docs/rules/design-foundation.md`,
`blueprint/L4-project-design-foundation.md`,
`blueprint/L5-project-design-foundation.md`.
