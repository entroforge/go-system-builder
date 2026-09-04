# Project Design Foundation Rule

---
rule_id: R-DESIGN-FOUNDATION-01
category: Design
status: locked
owner: Project Manager / Architect
scope: user-visible screens, visual behavior, copy, motion, images, and visible states
---

## 1. Rule

A product with user-visible interface must have a published Project Design
Foundation before the first `UI impact=changed` REQ is locked. Later UI REQs
inherit that Foundation; they do not invent a brand from a UI library default
or from the prettiest local component.

The Foundation lives outside the Loop. It does not own a cursor and does not
add a stage. Runtime currently does **not** hard-block a missing Foundation.
`loop-harness design-foundation check` is advisory (existence, token hex,
near-duplicate components). Do not hang it on `validate --all`. Promote to
`--strict` only after `blueprint/L6-design-foundation-replay.md` shows a
stable skip.

## 2. Authority

| Artifact | Path | Answers |
|:---|:---|:---|
| Kernel | `docs/design/DESIGN.md` | Worldview, thesis, tensions, law index, anti-principles |
| Grammar | `docs/design/design-language.md` | How the thesis generates visual, interaction, content, and motion choices |
| Surface | `docs/design/surface-profiles/*.md` | Same grammar, different density and posture |
| Proof | `docs/design/proof/` | Style tiles, anchor/stress screens, golden flows |
| Derivation | `docs/design/derivation/REQ-{id}.md` | Why this REQ looks this way |
| Exception | `docs/design/decisions/EX-{id}.md` (legacy `docs/design/exceptions/EX-{id}.md` still recognized) | Scoped, time-boxed deviation |
| Module package | `docs/design/prototypes/<module>/` | Current module truth (S2) |

`DESIGN.md` must stay short. Token values, component APIs, and page catalogs
do not belong there. Google DESIGN.md, if used at all, is a derived snapshot
under `docs/design/proof/portable/` (legacy `docs/design/portable/` still recognized) and is not the project authority.

Control logic: `blueprint/L4-project-design-foundation.md`.
Landing spec: `blueprint/L5-project-design-foundation.md`.
Procedure: `.claude/skills/design-foundation/SKILL.md` (template: `skills/design-foundation/SKILL.md`).

## 3. Trigger

An agent must check `docs/design/DESIGN.md` when any of the following is true:

- first understanding of a project that includes user-visible UI;
- the first REQ that may set `UI impact=changed`;
- a new consumer, operations, mobile, or other product surface;
- pages keep using default UI, one-off styles, or duplicate components;
- different REQs express the same semantics with different visuals;
- the current style cannot explain a new business or brand value.

Pure backend, infrastructure, or non-visible REQs only read an existing
Foundation. They do not start F1–F6.

## 4. Default behavior

1. Read `DESIGN.md`.
2. Classify it as `missing`, `draft`, `published` and covering the surface,
   `published` but unable to cover a new surface, or `N/A` (no UI product).
3. If the work will change UI and the Foundation is missing, draft, stale, or
   uncovered: pause batch page generation, load `design-foundation`, complete
   F0–F6, and obtain the three human confirmations (direction, kernel, publish).
4. If Foundation is `published`: S0 records the version reference only; S2
   writes a Design Derivation Note before expanding the module package.
5. Do not ask low-information questions such as "what color do you like" or
   "what style do you want". Hand up 1–3 value choices with a recommendation.
6. Write confirmed results to disk. Later agents read the documents, not chat.

S0 must not copy Kernel or Grammar into the REQ. A few style sentences in
REQ §B are not a substitute for Foundation.

## 5. Conflict order

| Conflict | Default resolution |
|:---|:---|
| Page vs Grammar | Fix the page |
| Component-library default vs Kernel | Wrap, restyle, or drop the default; the library is not the authority |
| Current REQ vs Foundation | Propose an exception or a global revision; the human judges value change |
| Grammar cannot support real work | Revise Kernel/Grammar and assess blast radius; do not patch every page |
| Golden screenshot vs Foundation | Decide whether the baseline is stale; a PNG is not above the thesis |

## 6. Forbidden

- Starting layout and components before Kernel confirmation.
- Three candidate directions that only swap color, type, and radius.
- Using "modern / simple / tech / youthful" as the Kernel.
- Silently promoting a local champion component into a global rule. New UI
  pieces stay module-local until `docs/design/decisions/CP-*.md` (legacy `docs/design/components/CP-*.md` still recognized) and a
  `design-foundation check` duplicate review justify reuse or promotion.
- Asking the human to pick spacing, hex values, or button shapes one by one.
- Treating pixel snapshot equality as proof that the thesis is correct.

## 7. Honest gap

`UI impact` remains the only machine-parsed REQ field (`none` / `changed` /
`unknown`). Foundation reference, Surface, posture, and Derivation Note are
semantic fields. PTR-PLAN-01 does not check Foundation. Missing Foundation
must still stop the **agent default path**; it does not yet fail a Runtime gate.
The CLI check reports the gap; it does not judge Thesis quality.
