# Design Grammar

> 编译自：DESIGN.md@vX.Y.Z
> 版本：vX.Y.Z
> 规则：只写 active 维度；inherited/debt/N/A 不用散文填充。

## 1. Dimension coverage

<!-- foundation-contract:v1 dimensions -->
| 维度 | 状态 active/inherited/debt/N/A | 依据约束 | 继承源或 DEBT | Proof |
|:--|:--|:--|:--|:--|
| Information | active | LAW-01 | — | PROOF-01 |
| Composition | active | LAW-01 | — | PROOF-01 |
| Color | inherited | INV-02 | `packages/ui@vX` | PROOF-02 |
| Typography | debt | LAW-03 | DEBT-03 | — |
| Shape & Surface | N/A | — | {理由} | — |
| Image & Icon | N/A | — | {理由} | — |
| Interaction | active | LAW-02 | — | PROOF-02 |
| Content | active | LAW-01 | — | PROOF-01 |
| Motion | debt | LAW-02 | DEBT-04 | — |

## 2. Constraint explanations

只为需要背景的 LAW/ANTI/INV 补充信念、依据、代价与边界。§0 已有的 Do/Don't 不在此改写。

## 3. Compilation and selection rules

<!-- foundation-contract:v1 grammar-rules -->
| ID | Dimensions | Source constraints | 条件/任务 | 优先采用 | 避免 | 允许变化 | Bindings | Proof |
|:--|:--|:--|:--|:--|:--|:--|:--|:--|
| GR-01 | Information, Interaction | LAW-01, ANTI-01 | 承诺 | 先依据后行动 | 双主行动 | 文案/密度 | ROLE-action-promise, PAT-confirm | PROOF-01 |

每一行可展开为：因为【Source】所以在【条件】优先【关系】避免【冲突】允许【变化】通过【Proof】判断。

## 4. Variant catalog

| Variant | 合法值 | 默认值 | 由哪些 SUR-* 选择 |
|:--|:--|:--|:--|
| density | low / medium / high | medium | SUR-01 |
| brand-expression | wordmark / accent / ambient | accent | SUR-01 |

## 5. Semantic roles and patterns

<!-- foundation-contract:v1 bindings -->
| ID | 含义 | Source GR-* | Token / component / pattern | 状态 active/reserved/debt |
|:--|:--|:--|:--|:--|
| ROLE-action-promise | 用户将做出可追踪承诺 | GR-01 | `color.action.promise` | active |
| PAT-confirm | 高风险承诺模式 | GR-01 | `packages/ui/Confirm` | active |
