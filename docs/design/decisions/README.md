# docs/design/decisions

本目录收敛三类设计决策记录，沿用同一套 ADR 流程（人确认、版本化、可回灌）。单 Surface 项目通常不需要 `surface-profiles/`；第二 Surface 出现时再加。

| 记录 | 模板 | 何时写 |
|:---|:---|:---|
| 方向/架构/发布 | `ADR-template.md` → `ADR-{id}.md` / `DFD-{id}.md` | F2 方向选择、F3 内核确认、F6 发布、breaking change |
| 例外 | `EXCEPTION-template.md` → `EX-{id}.md` | `exception` 姿态：业务必须偏离某条 Law / Grammar，且有明确范围与期限 |
| 组件/模式提案 | `COMPONENT-PROPOSAL-template.md` → `CP-{id}.md` | 现有组件/Surface 无法表达所需语义，或同一关系在第二模块重复出现 |

## 纪律

- 新 UI 片段先落在 `prototypes/<module>/`，**不静默升为全局**。第二模块出现同语义再提 `CP-*`；与 Kernel 冲突但业务必须 → `EX-*`。
- `EX-*` 必须有范围、期限、禁止扩散面、复查条件；到期未复查视为过期。
- 重复组件由 `loop-harness design-foundation check --root .` 顾问式提示（`component_repeat`），默认不阻断 `validate --all`。
- 遗留路径 `docs/design/components/CP-*.md` 与 `docs/design/exceptions/EX-*.md` 仍被校验兼容识别，新项目请用本目录。

## 命名

见 `docs/rules/naming.md` 中 `design exception` / `component proposal` 行。
