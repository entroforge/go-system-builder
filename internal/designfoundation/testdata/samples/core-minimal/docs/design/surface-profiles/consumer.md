# Surface Profile — consumer
> ID：SUR-01
> 版本：v1.0.0
> 继承：DESIGN.md@v1.0.0
> 用户/任务：消费者查看与确认

## Inherits

<!-- foundation-contract:v1 surface-inherits -->
| ID | 保留？ yes / no（no 必须引用 EX-*） | 本 Surface 的可观察落点 |
|:--|:--|:--|
| LAW-01 | yes | evidence before CTA |
| GR-01 | yes | card + single promise |
| ROLE-action-promise | yes | primary button |

## Variant selections

<!-- foundation-contract:v1 surface-variants -->
| Variant | 选择 | 理由 |
|:--|:--|:--|
| density | medium | default |
| whitespace | balanced | readable |
| navigation | task-first | checkout flow |
| type-scale | default | body |
| evidence-depth | summary | card |
| freshness | timestamped | order time |
| brand-expression | accent | checkout |

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
| PROOF-01 | — | anchor consumer.html |

