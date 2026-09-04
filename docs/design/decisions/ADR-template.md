# ADR-{id}：{决策标题}

> 状态：proposed / accepted / rejected / deprecated  
> 日期：YYYY-MM-DD  
> 决策人：{Architect}  
> 关联需求：`docs/requirements/REQ-{id}.md`

## 1. 背景

{为什么需要这个决策？}

## 2. 决策

{最终选择是什么？}

## 3. 备选方案

| 方案 | 优点 | 缺点 | 结论 |
|:---|:---|:---|:---|
| A | {优点} | {缺点} | 采用/排除 |

## 4. 影响

| 方面 | 影响 |
|:---|:---|
| 开发 | {影响} |
| 测试 | {影响} |
| 运维 | {影响} |
| 安全 | {影响} |

## 5. 后续动作

| 动作 | 负责人 | 截止日期 |
|:---|:---|:---|
| {动作} | {负责人} | YYYY-MM-DD |

## Depth Self-Review

{S2 收口前的三角色深度自审结论，一段话：implementer——哪个 oracle 我造不出来或无法区分错实现？e2e-tester——七个 oracle 维度上哪个反例我无法取证？maintainer——哪条规则会与模块演化冲突？}

## Endorsed N/A

{经背书的 N/A 清单——每条一行：`AC-{id}` → `NFR-{id}` 或 `§A4-{条目}`。此表随 ADR 一并进入 S2 人闸签核包。}

## Feedback receipt（仅当本 ADR 承载一条反馈事务时填写；否则保留为空，checker 不要求）

| 字段 | 填写 |
|:---|:---|
| Source observation | {Finding ID / counterevidence 行 / visual-qa 结果 / REQ-* PROOF-* 路径} |
| Affected constraint IDs | {被违反或被证伪的 LAW-*/ANTI-*/INV-*/GR-*/ROLE-*/PAT-*/SUR-*/EX-*，逗号分隔} |
| Classification | {local fix / module Pattern-CP / global Grammar/Token/Component extension / scoped EX / Kernel breaking change} |
| Changed edges | {Grammar / Surface / Derivation / Token / component / Proof 中实际改动的边，列文件与版本} |
| Replay evidence | {受影响边的 design-foundation check 结果 + 一个最小回放：第二份 changed REQ 冷换手或受影响 Proof 态路径} |
| Status | open / closed |

未通过前保持 `open`；收据字段必须可解析（checker 只验引用与路径，是否应改 Kernel 由人判定）。不得以报告写完视为闭环。
