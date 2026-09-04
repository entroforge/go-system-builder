# Surface Profile — operations
> ID：SUR-02
> 版本：v1.0.0
> 继承：DESIGN.md@v1.0.0
> 用户/任务：运营批量审核与异常处理

## Inherits

<!-- foundation-contract:v1 surface-inherits -->
| ID | 保留？ yes / no（no 必须引用 EX-*） | 本 Surface 的可观察落点 |
|:--|:--|:--|
| LAW-01 | yes | need evidence before bulk action |
| GR-01 | yes | table + batch confirm |
| ROLE-action-promise | yes | bulk confirm |

## Variant selections

<!-- foundation-contract:v1 surface-variants -->
| Variant | 选择 | 理由 |
|:--|:--|:--|
| density | high | table |
| whitespace | compact | dense list |
| navigation | global | ops nav |
| type-scale | compact | more rows |
| evidence-depth | inline | inline audit |
| freshness | realtime | live queue |
| brand-expression | ambient | secondary chrome |

## Adds / suppresses

| 动作 | GR/ROLE/PATTERN | 理由 | 是否需要 extension |
|:--|:--|:--|
| — | — | — | no |

## Exceptions

| EX-* | 范围 | 到期/复查条件 |
|:--|:--|:--|
| — | — | — |

## Contrast proof

| PROOF-* | 对比 Surface | 证明哪些 invariant 保持、哪些 variant 改变 |
|:--|:--|:--|
| PROOF-02 | SUR-01 | same LAW-01/GR-01, density high vs medium |

