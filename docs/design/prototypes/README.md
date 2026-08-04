# UI Design Packages

This directory stores module-scoped final UI design packages for requirements
that change frontend screens, interactions, or visible states.

Each package represents the intended final product state. There is no
separate dual-version prototype. The current implementation is checked
as factual context, while the maintained design artifact is the final package.

## Package Layout

Use one directory per module or screen family:

```text
docs/design/prototypes/{module}/
  prototype.html
  USER-STORY-{REQ-id}-{module}.md
  USER-FLOW-{REQ-id}-{module}.md
  assets/
```

The package is maintained as the latest approved final design for that module.
It may match the implementation or describe the next locked change, but the
maintained artifact is always one final package per module.

## File Types

| File | Purpose |
|:---|:---|
| `prototype.html` | Static HTML prototype of the final intended UI |
| `USER-STORY-{REQ-id}-{module}.md` | A catalog of real user stories: functional background, scenarios, and page-transition logic |
| `USER-FLOW-{REQ-id}-{module}.md` | A catalog of independently executable button-level `PATH-*` journeys for E2E Browser |
| `assets/` | Images, icons, fixtures, or local prototype assets |

## Gate

When `UI impact = changed` in a REQ:

1. Identify affected modules and create or update their package directories.
2. Update `prototype.html` to the final intended UI.
3. Add the matching user story and user flow.
4. Verify the story, flow, and prototype agree:
   - story outcomes are represented in the prototype
   - each flow path names controls visible in the prototype, and the interaction-coverage map accounts for every declared control
   - contract-relevant fields, states, errors, permissions, and side effects are explicit
5. Link the package into FE/BE/SYNC contracts before contract lock.
6. In S7, execute `USER-FLOW-*` paths in a real browser and record step-level evidence.

Lightweight mode may skip standalone architecture/design docs, but it cannot
skip this gate for UI-impacting requirements.
