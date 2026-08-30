# UI Design Packages

This directory stores module-scoped current-truth UI and fact-scenario packages
for modules whose screens, interactions, rules, or visible states are designed
or changed.

Each package represents the one currently correct product and business behavior.
There is no per-REQ, per-round, or versioned copy. The current implementation is
checked as factual context; the maintained files are the module's current target,
rule model, generated cases, stories, flows, and visible states.

## Package Layout

Use one directory per module or screen family:

```text
docs/design/prototypes/{module}/
  index.html
  stories.md
  flows.md
  scenario-model.json
  cases.json
  scenario-coverage.json
  fixture-contract.json
  cross-matrix.json    # convergence-1 carrier (fact x FR x story hunt)
  *.html
  assets/
```

The package is maintained as the latest approved final design for that module.
It may match the implementation or describe the next locked change, but the
maintained artifact is always one final package per module.

## File Types

| File | Purpose |
|:---|:---|
| `index.html` | Module entry hub linking to every current page/dialog/wizard HTML file |
| `stories.md` | Complete current module story set; each story maps to scenario branches and uses `source_refs` for REQ traceability |
| `flows.md` | Complete current module journey set; each `PATH-*` maps to CASE and browser steps |
| `scenario-model.json` | Strict current module facts, rules, explicit allow/reject branches, witnesses, and oracles |
| `cross-matrix.json` | Hand-written convergence-1 carrier: every hunted fact×FR×story cell names its covering branch or a no-branch reason (silence is not N/A) |
| `cases.json` | Deterministically generated current case catalog; never a hand-maintained REQ copy |
| `scenario-coverage.json` | Deterministically generated branch and polarity coverage ledger |
| `fixture-contract.json` | Synthetic, isolated fixture setup and cleanup contract |
| `*.html` | Current page, dialog, wizard, or component prototype |
| `assets/` | Images, icons, fixtures, or local prototype assets |

## Gate

When `UI impact = changed` in a REQ:

1. Identify affected modules and create or update their package directories.
2. Read the complete existing module package before editing it.
3. Reconcile the REQ's behavior into the complete current module set; do not create a REQ copy.
4. Update `index.html`, `stories.md`, `flows.md`, the scenario four-pack, and affected `*.html` files.
5. Verify the scenario, story, flow, prototype, and test chain agree:
   - every required allow/reject branch has a case
   - required branch coverage is 100%
   - positive/negative capacity ratio meets the module profile
   - every browser-required case maps to a story, PATH, and module spec
   - story outcomes are represented in the prototype
   - each flow path names controls visible in the prototype, and the interaction-coverage map accounts for every declared control
   - contract-relevant fields, states, errors, permissions, and side effects are explicit
6. Link the current package into FE/BE/SYNC contracts before contract lock.
7. In S7, execute every required CASE/PATH in the current module package in a real browser and record round-scoped evidence.

Lightweight mode may skip standalone architecture/design docs, but it cannot
skip this gate or the full-module regression for UI-impacting requirements.
