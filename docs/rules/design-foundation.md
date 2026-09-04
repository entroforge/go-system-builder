# Project Design Foundation Rule

---
rule_id: R-DESIGN-FOUNDATION-01
category: Design
status: locked
owner: Project Manager / Architect
scope: user-visible screens, visual behavior, copy, motion, images, and visible states
---

## 1. Rule

A product with user-visible interface must **check** Project Design
Foundation at F0 before the first `UI impact=changed` REQ is locked.
Choose `local` / `core` / `extended` / `N/A` at F0 (this rule §4). Local one-shot
UI does **not** publish a Project Foundation. Core and Extended later UI
REQs inherit the Foundation; they do not invent a brand from a UI library
default or from the prettiest local component. Do not set `published` while
human confirmations are still PENDING.

The Foundation lives outside the Loop. It does not own a cursor and does not
add a stage. Runtime currently does **not** hard-block a missing Foundation.
`loop-harness design-foundation check` is advisory (existence, token hex,
near-duplicate components). Do not hang it on `validate --all`. Promote to
`--strict` only after a real product fills
`docs/reports/design-foundation/FOUNDATION-REPLAY-template.md` and shows a
stable skip.

## 2. Authority

| Artifact | Path | Answers |
|:---|:---|:---|
| Kernel | `docs/design/DESIGN.md` | Section 0 Next-agent card and §8 SUR row are the required read for a later REQ; worldview/thesis remain supporting |
| Grammar | `docs/design/design-language.md` | Optional; Core+thin may leave as debt. Open only if the card cannot answer Must not |
| Surface | `DESIGN.md` §8 (`inline`) first; `docs/design/surface-profiles/*.md` only from the second surface | Same grammar, different density and posture |
| Proof | `docs/design/proof/` | Style tiles, anchor/stress screens, golden flows |
| Derivation | `docs/design/derivation/REQ-{id}.md` | Why this REQ looks this way |
| Exception | `docs/design/decisions/EX-{id}.md` (legacy `docs/design/exceptions/EX-{id}.md` still recognized) | Scoped, time-boxed deviation |
| Module package | `docs/design/prototypes/<module>/` | Current module truth (S2) |

`DESIGN.md` must stay short. Token values, component APIs, and page catalogs
do not belong there. Google DESIGN.md, if used at all, is a derived snapshot
under `docs/design/proof/portable/` (legacy `docs/design/portable/` still recognized) and is not the project authority.

Control logic: this rule and `skills/design-foundation/SKILL.md`.
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

1. **F0 — decide investment before drawing.** Before any UI work, record
   `design investment` in `project-map.md`: `local / core / extended / N/A`
   with basis (reuse, handoff, Surface count, shared component/token, risk).
   `local` means a single surface, single module, no shared token/component,
   no handoff — only a module-local `derivation/REQ-{id}.md` (`Foundation: local`),
   never a published `DESIGN.md`. Any one condition fails → re-run F0 and
   upgrade to Core. Do not promote a local primitive or hex to project scope.
   `N/A` is only for products with no user-visible UI. Investment (`local/core/extended`)
   and F2 path (`thin/full`) are orthogonal — a thin Core is still Core.
2. Read `DESIGN.md`.
3. Classify it as `missing`, `draft`, `in-review`, `provisional`, `published` and covering
   the surface, `published` but unable to cover a new surface, `superseded`, or `N/A`.
   `provisional` is not a lock. `published` plus a confirmation line that still
   says PENDING is a fake lock — set `provisional` and wait. Only `published` (with dates recorded) is a lock; `draft`/`in-review`/`provisional`/`superseded` must not be treated as a covering lock for a new `changed` REQ.
4. If the work will change UI and the Foundation is missing, draft, stale,
   uncovered, or lacks section 0: pause batch page generation, load
   `design-foundation`, complete F0–F6. Local work exits at F0 without a
   Project `DESIGN.md`. Core+thin may leave `design-language.md` as debt. F2 uses the thin path (this rule §4) when
   the ritual is already a fact (one merged confirmation: recommendation vs
   category default + Laws/Anti + Next-agent card); otherwise the full path
   (2–3 worlds must differ in ≥3 of six dimensions, three confirmations:
   direction → kernel → publish). Do not invent extra worlds sharing the same
   product role — use the thin path instead.
5. If Foundation is `published` (confirmation dates recorded, not PENDING):
   S0 records the version reference only
   (`DESIGN.md@version`, `SUR-*@version`, posture). S2 **only reads the cold-start
   handoff packet** — `DESIGN.md` §0 Next-agent card + current `SUR-*` diff +
   current `derivation/REQ-{id}.md` — budgeted together ≤120 lines / ≤12 KB;
   open `design-language.md` only if the card cannot answer Must not. The SUR
   diff is `DESIGN.md` §8 (`inline`) until a second surface exists. Write
   the Derivation Note (active LAW/ANTI/INV by ID, Must not by ID, one macro
   composition, one stress state) before expanding the module package. Do not
   promote a construction hex or library Primary into the brand; only
   `semantic_token_only` values from `tokens.css` are legal after F6.
6. Do not ask low-information questions such as "what color do you like" or
   "what style do you want". Hand up 1–3 value choices with a recommendation.
7. Write confirmed results to disk. Later agents read the documents, not chat.

S0 must not copy Kernel or Grammar into the REQ. A few style sentences in
REQ §B are not a substitute for Foundation. Grammar: nine dimensions are
routed once (`active/inherited/debt/N/A`); only `active` dimensions get a
`GR-*` rule — do not fill `inherited/debt/N/A` with prose to pass a field count.
Project checks in `design-checks.json` must each cite a source `LAW/ANTI/INV`;
ad-hoc case lint (e.g. banning a single green hex) is not a generic engine rule
until it is declared there.

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
- Inventing two extra worlds that share the product role of a ritual already
  marked as fact in the Evidence Field (use the thin path instead).
- Using "modern / simple / tech / youthful" as the Kernel.
- Promoting a construction hex or a component-library Primary into the brand.
- Silently promoting a local champion component into a global rule. New UI
  pieces stay module-local until `docs/design/decisions/CP-*.md` (legacy `docs/design/components/CP-*.md` still recognized) and a
  `design-foundation check` duplicate review justify reuse or promotion.
- Asking the human to pick spacing, hex values, or button shapes one by one.
- Treating pixel snapshot equality or a filled `DESIGN.md` as proof that the
  thesis is correct. Inheritance is tested by a later REQ, not by template
  completeness.
- Treating Foundation as a substitute for S2 architecture, data, or contracts.
- Setting `published` while direction, kernel, or publish confirmation is
  still PENDING.
- Scoring Foundation on whether a primary button has a click handler.

## 7. Honest gap

`UI impact` remains the only machine-parsed REQ field (`none` / `changed` /
`unknown`). Foundation reference, Surface, posture, and Derivation Note are
semantic fields. PTR-PLAN-01 does not check Foundation. Missing Foundation
must still stop the **agent default path**; it does not yet fail a Runtime gate.
The CLI check reports the gap; it does not judge Thesis quality.
