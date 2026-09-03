# Visual QA

Playwright and Storybook snapshots prove **implementation drift** against a
checked-in baseline. They do not prove that the Design Thesis is correct
(L4 §1.3, §12 Playwright / Storybook visual testing boundary).

This template factory has no product screens to snapshot. Target projects
add baselines here (or under the app's existing Playwright/Storybook tree)
after F5 Proof Set exists.

## What a pixel match means

| Result | Means | Does not mean |
| --- | --- | --- |
| Snapshot matches | The current render still looks like last week's agreed screen | The Thesis, Grammar, or Surface Profile is right |
| Snapshot differs | Something in implementation, tokens, or the baseline moved | The new look is a better brand |

When a Golden Screen fails: first decide whether the **baseline is stale**
or the **implementation drifted**. A PNG is not above `docs/design/DESIGN.md`.

## Suggested layout (target project)

```text
tools/visual-qa/
  README.md                 # this protocol
  playwright.config.ts      # optional; reuse web/e2e if snapshots live there
  snapshots/                # committed baselines for Anchor / Stress / Golden Flow
```

Prefer attaching visual comparisons to the same HTML already used as Proof
Set (`docs/design/proof/`) or to Storybook stories of those screens. Do not
create a third set of “pretty” pages only for screenshots.

## Mechanical checks that are allowed

These are existence and drift facts, not aesthetic scores:

```bash
loop-harness design-foundation check --root .
# optional: --strict in CI after L6 shows agents skip Foundation
```

The check is **advisory** (D3). It reports missing published Kernel for a
locked `UI impact=changed` REQ, missing Derivation Notes, unregistered hex,
and near-duplicate component names. It never scores Thesis quality.

`validate --all` does **not** include this check. The template factory has
no `DESIGN.md` on purpose; hanging Foundation on doctor/validate would fail
the factory and empty backend products.

## Do not

- Treat snapshot equality as S7 / S10 acceptance of brand direction.
- Auto-update baselines because a library default changed.
- Fail-close on missing Foundation until L6 observation says agents skip F0
  as a stable failure (`blueprint/L6-design-foundation-replay.md`).
