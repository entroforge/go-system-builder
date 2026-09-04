# EX-{id}

> 范围：{模块 / 页面 / 状态}
> 期限：YYYY-MM-DD
> 复查条件：
> 是否可能晋升为全局：yes / no

局部偏离必须有范围和期限。任何 REQ 都不能把这里的成功样本静默升级为全局规范。

## 偏离哪条 Law / Grammar

## 业务必须如此的理由

## 影响面

## 禁止扩散到

## Feedback receipt（仅当本 EX 承载一条反馈事务时填写；否则保留为空，checker 不要求）

| 字段 | 填写 |
|:---|:---|
| Source observation | {Finding ID / counterevidence 行 / visual-qa 结果 / REQ-* PROOF-* 路径} |
| Affected constraint IDs | {被违反或被证伪的 LAW-*/ANTI-*/INV-*/GR-*/ROLE-*/PAT-*/SUR-*/EX-*，逗号分隔} |
| Classification | scoped EX |
| Changed edges | {本 EX 文件 + 受影响 Derivation / Proof / 组件路径} |
| Replay evidence | {受影响边的 design-foundation check 结果 + 一个最小回放路径} |
| Status | open / closed |

未通过前保持 `open`；收据字段必须可解析。`open` 直到 check 与回放均通过。
