# REQ-001 整体派发计划

> REQ: REQ-001
> Dispatch policy: waves-v1
> Revision: 1
> Status: complete

入口：[需求](../../requirements/REQ-001.md) · [合同](../contracts/CONTRACTS-001.md)
规则：[整体派发计划](../../rules/dispatch-plan.md)

Status=complete 只表示静态计划文档完成并可交原 gate；不表示任一 Builder 责任为 `done`。责任进度按 `docs/rules/state-machine.md` 的五态语义查看，实时执行进度不得回填到本文。提交 TASK 与计划后再进入 S5。
下列空框是静态计划，冻结后不回填；实时进度用 `loop-harness s6 status --capacity <实际并发总槽位>` 查看。

## W1 · 输入齐备的独立任务并行

- [ ] [TASK-001-01：登记模拟交付来源台账并核对受管交付物标记](TASK-001-01.md)

并行依据：本 REQ 只有一个 TASK，不存在同波次写域重叠或共享可变资源。TASK-001-01 的 prospective write scope 为 `simulation/products/artifact.txt`（本轮模拟数据使用现有夹具，不实际写入），不与任何其他责任竞争资源，也不需要 Resource order 表。

## Resource order

只填不能通过唯一 owner、收窄范围或隔离解决的资源顺序；无则保留空表。
这不是产物依赖，不能重复抄 TASK Dependencies。

| Before | After | Resource | Reason |
|:---|:---|:---|:---|

## 集成与执行

复用 TASK/manifest 的 required checks。Worker 子分支提交后由 Main 合并回 REQ 绑定开发分支并验证；有效集成才会在实时清单打勾。回收后及时清理 worktree。容量不足只排队，槽位释放后持续补位。

## History

| Date | Revision | Change |
|:---|:---|:---|
| 2026-10-21 | 1 | 初始派发计划（simulation/test-only）：单项 TASK-001-01 覆盖 BE-001 §7 |
