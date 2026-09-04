# Design Grammar

> 编译自：DESIGN.md@v1.0.0
> 版本：v1.0.0

## 1. Dimension coverage

<!-- foundation-contract:v1 dimensions -->
| 维度 | 状态 active/inherited/debt/N/A | 依据约束 | 继承源或 DEBT | Proof |
|:--|:--|:--|:--|:--|
| Information | active | LAW-01 | — | PROOF-01 |
| Composition | active | LAW-01 | — | PROOF-01 |
| Color | active | LAW-01 | — | PROOF-01 |
| Typography | active | LAW-01 | — | PROOF-01 |
| Shape & Surface | active | LAW-01 | — | PROOF-01 |
| Image & Icon | active | LAW-01 | — | PROOF-01 |
| Interaction | active | LAW-01 | — | PROOF-01 |
| Content | active | LAW-01 | — | PROOF-01 |
| Motion | active | LAW-01 | — | PROOF-01 |

## 3. Compilation and selection rules

<!-- foundation-contract:v1 grammar-rules -->
| ID | Dimensions | Source constraints | 条件/任务 | 优先采用 | 避免 | 允许变化 | Bindings | Proof |
|:--|:--|:--|:--|:--|:--|:--|:--|:--|
| GR-01 | Information, Composition, Color, Typography, Shape & Surface, Image & Icon, Interaction, Content, Motion | LAW-01 | 承诺 | 先依据后行动 | 双主行动 | 文案 | ROLE-action-promise | PROOF-01 |

<!-- foundation-contract:v1 bindings -->
| ID | 含义 | Source GR-* | Token / component / pattern | 状态 active/reserved/debt |
|:--|:--|:--|:--|:--|
| ROLE-action-promise | 承诺 | GR-01 | color.action.promise | active |
