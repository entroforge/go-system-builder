# 开发合同总览

> 需求：REQ-001
> Shared model policy: none
> Shared model reason: REQ §A4-2 无 API、事件、共享数据；输入与输出为模拟文本，无共享模型消费者。
> 状态：draft
> 版本：v1.0.0

> SIMULATION / test-only 未接收候选。没有发生过的锁定、批准、检查、回执与审查不在此预填；Main 按实际阶段使用原入口形成事实。

## 合同清单

| 合同ID | 类型 | 负责人 | 状态 | 版本 | 联调点 |
|:--|:--|:--|:--|:--|:--|
| BE-001 | 后端 | 实际 S6 Builder | draft | v1.0.0 | N/A |

## 不适用合同族及依据

FE-001 not_applicable：REQ §A4-1 无 UI。SYNC-001 not_applicable：REQ §A4-2 无 API、事件、共享数据。

## UI 设计包输入

无 UI 或服务行为变化；全部 AC 只有 NFR-001 背书，不生成模块行为包或浏览器 spec。适用性由原阶段实际判定，未预填任何桥检查结果。

## 需求覆盖矩阵

| REQ source_ref | Rule → CASE → Story → PATH → Spec | FE 合同条款 | BE 合同条款 | SYNC 条款 |
|:--|:--|:--|:--|:--|
| REQ-001/NFR-001 | N/A：无 FR、无模块行为变化 | N/A | BE-001 §7（AC-001）/ §8（AC-002） | N/A |

## 交付

只读 `simulation/products/artifact.txt` → S6 真创建并提交 `simulation/deliveries/ledger-note.txt`。Main post-merge 实际执行 `make check` 与 `make check DELIVERY=simulation/deliveries/ledger-note.txt`。不以预置输入冒充 S6 交付。

## Shared model baseline

无共享模型基线，依据顶部 reason；不伪造模型验证。

## 合同变更记录

无已接收锁定、变更或批准历史。

## 独立模拟发布条件 AC-002

`simulation/repairable/status.txt` 初始为 PENDING，模拟发布前目标为 READY。此条件由 S7 独立 Reviewer 读取实际文件；如实发现差异时由 S8 调查并在原授权下由 S9 修正，不增加 TASK-001-01 的 S6 写域或 Main 两条检查。
