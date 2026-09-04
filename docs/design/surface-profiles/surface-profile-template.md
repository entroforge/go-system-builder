# Surface Profile — {name}

> 只在出现 **第二个** Surface 时复制本文件。单 Surface 把差分写在 `DESIGN.md` §8，Profile/version = `inline`。
>
> ID：SUR-0X
> 版本：vX.Y.Z
> 继承：DESIGN.md@vX.Y.Z
> 用户/任务：{一句话}

## Inherits

<!-- foundation-contract:v1 surface-inherits -->
| ID | 保留？ yes / no（no 必须引用 EX-*） | 本 Surface 的可观察落点 |
|:--|:--|:--|
| INV-01 | yes | |
| GR-01 | yes | |
| ROLE-action-promise | yes | |

## Variant selections

<!-- foundation-contract:v1 surface-variants -->
| Variant | 选择 | 理由 |
|:--|:--|:--|
| density | low / medium / high | |
| whitespace | compact / balanced / spacious | |
| navigation | local / global / task-first | |
| type-scale | compact / default / expressive | |
| evidence-depth | summary / expandable / inline | |
| freshness | relaxed / timestamped / realtime | |
| brand-expression | wordmark / accent / ambient | |

## Adds / suppresses

| 动作 | GR/ROLE/PATTERN | 理由 | 是否需要 extension |
|:--|:--|:--|:--|

## Exceptions

| EX-* | 范围 | 到期/复查条件 |
|:--|:--|:--|

## Contrast proof

| PROOF-* | 对比 Surface | 证明哪些 invariant 保持、哪些 variant 改变 |
|:--|:--|:--|
