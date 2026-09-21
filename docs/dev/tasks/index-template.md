# REQ-{id} 整体派发计划

> REQ: REQ-{id}
> Dispatch policy: waves-v1
> Revision: 1
> Status: draft

入口：[需求](../../requirements/REQ-{id}.md) · [合同](../contracts/CONTRACTS-{id}.md)
规则：[整体派发计划](../../rules/dispatch-plan.md)

Status=complete 只表示计划文档完成。提交 TASK 与计划后再进入 S5。
下列空框是静态计划，冻结后不回填；实时进度用 `loop-harness s6 status --capacity <实际并发总槽位>` 查看。

## W1 · 输入齐备的独立任务并行

- [ ] [TASK-{id}-01：交付物](TASK-{id}-01.md)
- [ ] [TASK-{id}-02：交付物](TASK-{id}-02.md)

并行依据：{具体独立写域与可变资源隔离；共享只读模型不增加依赖}。

## W2 · 对应前置回收验证后启动

- [ ] [TASK-{id}-03：交付物](TASK-{id}-03.md)

实际前置只写在 TASK Dependencies；无需等待 W1 中无关任务。
删除不适用波次，编号从 W1 连续递增。每个非取消 TASK 恰好出现一次。

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
| | 1 | 初始派发计划 |
