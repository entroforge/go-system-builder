# Design Tokens

Token files are the **implementation** of Design Grammar, not the Kernel.
Replace primitive values after F4. Keep semantic role names stable.

Source of truth: `tokens.json` (DTCG 2025.10). `tokens.css` is the CSS
variable projection. Regenerate it with:

```bash
loop-harness design-foundation emit-css --root .
```

## Semantic roles ↔ Grammar

| CSS variable | Token path | Allowed family | Grammar back-ref |
|:---|:---|:---|:---|
| `--color-surface-page` | `color.surface.page` | surface | Color: environment |
| `--color-surface-raised` | `color.surface.raised` | surface | Shape & Surface |
| `--color-content-ink` | `color.content.ink` | content | Typography / Color |
| `--color-content-meta` | `color.content.meta` | content | Typography |
| `--color-content-border` | `color.content.border` | content | Shape & Surface |
| `--color-content-border-strong` | `color.content.border-strong` | content | Shape & Surface |
| `--color-action-promise` | `color.action.promise` | action | Color / Interaction: scarce emphasis |
| `--color-status-success` | `color.status.success` | status | Interaction |
| `--color-status-warning` | `color.status.warning` | status | Interaction |
| `--color-status-blocking` | `color.status.blocking` | status | Interaction |
| `--color-status-info` | `color.status.info` | status | Interaction |
| `--color-brand-mark` | `color.brand.mark` | brand | Color: brand at meaningful moments |

Color semantics may only use `surface.*`, `content.*`, `action.*`, `status.*`,
`brand.*`. Page-level names (`page-login-button-hover`) are forbidden.

Every semantic color must remain traceable to a Design Law in
`docs/design/design-language.md`. The starter file points at LAW-01; the
target project rewrites `$description` after F4.

## Prototype usage

HTML prototypes link `packages/design-tokens/tokens.css` and use the
variables above. Do not introduce a new hex. Lint:

```bash
loop-harness design-foundation check --root .
```
