# Portable DESIGN.md

This directory holds a **derived** Google DESIGN.md snapshot. It is not the
project authority.

Authoritative sources:

- Kernel: `docs/design/DESIGN.md`
- Grammar: `docs/design/design-language.md`
- Values: `packages/design-tokens/tokens.json`

Generate:

```bash
loop-harness design-foundation export-portable --root .
```

Do not hand-edit the exported file. Do not put component APIs here — Atlassian
testing showed agents then re-implement buttons instead of reusing the live
system. Query Storybook MCP (`tools/ui-lab/README.md`) for components.
