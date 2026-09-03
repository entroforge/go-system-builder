# Component proposals

New UI pieces start as **module-local**. They become global Grammar/Pattern
only through an explicit proposal. A REQ must not silently promote a local
champion.

## When to write a proposal

- The current Surface Profile and component set cannot express a required
  semantic role (promise, evidence, blocking status, recovery…).
- The same relationship appears in a second module.
- A library default conflicts with Design Kernel.

## Path

Copy `COMPONENT-PROPOSAL-template.md` to `CP-{id}.md` in this directory.
Run `loop-harness design-foundation check --root .` so near-duplicate names
are warned before you add a third button.

Promotion path (L4 §7.4):

1. One-off composition stays in `docs/design/prototypes/<module>/`.
2. Repeated module relationship → this proposal, still local.
3. Repeated across modules/surfaces → Grammar / Token / component revision,
   human-confirmed.
4. Conflicts with Kernel but business-required → `exceptions/EX-*.md`.
