# UI Lab

This directory is the **wiring note** for a live component laboratory in a
target project. The template factory does not ship a product Storybook.

UI Lab is the implementation layer: Agent queries **what currently exists**.
Project Design Foundation (`docs/design/DESIGN.md`) remains the language
layer: **why a choice is allowed**. Live component knowledge must not rewrite
the Kernel (L4 §12 Storybook MCP boundary).

## When to stand this up

Stand up Storybook when the target project has Vue or React UI that agents
will implement or restyle. Pure backend products skip this directory.

## Storybook MCP

Install [`@storybook/addon-mcp`](https://storybook.js.org/docs/ai/mcp/overview)
so agents can call `docs-list` and `docs-show` before changing UI.

### Vue

Vue 3 + Vite needs both:

1. `componentsManifest` so MCP can enumerate components;
2. `experimentalDocgenServer` on `@storybook/vue3-vite`.

Example `.storybook/main.ts` fragment (adapt to the target app; do not copy
a fake component API into `DESIGN.md`):

```ts
import type { StorybookConfig } from "@storybook/vue3-vite";

const config: StorybookConfig = {
  addons: ["@storybook/addon-mcp"],
  framework: {
    name: "@storybook/vue3-vite",
    options: {
      experimentalDocgenServer: true,
    },
  },
  core: {
    disableTelemetry: true,
  },
};
export default config;
```

Point `componentsManifest` at the real source glob the app already uses
(for example `src/components/**/*.vue`). Do not invent a second component
tree for the agent.

### React

Use the framework's default docgen. MCP still needs `@storybook/addon-mcp`
and a manifest the addon can read. Prefer the project's existing Storybook
over a parallel “agent-only” lab.

## Agent default path

Before implementing or restyling a control:

1. `docs-list` — is there already a component for this semantic role
   (`action.promise`, `status.blocking`, dialog, drawer…)?
2. `docs-show` — which props, states, and stories already exist?
3. Reuse or wrap that component. Do **not** re-implement it from
   `docs/design/portable/DESIGN.md` or from a library demo.
4. If nothing fits, keep the new piece **module-local** and file
   `docs/design/components/CP-*.md`. Do not silently promote a local champion.

Quality bar: `skills/frontend-engineering/SKILL.md`.
Duplicate names: `loop-harness design-foundation check --root .`.

## What this is not

- Not a Design Kernel.
- Not a license to generate a third primary button from Element Plus / Shadcn defaults.
- Not a replacement for Proof Set (Style Tile, Anchor, Stress, Golden Flow).
