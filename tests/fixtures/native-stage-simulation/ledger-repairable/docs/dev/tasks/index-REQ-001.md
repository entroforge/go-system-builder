# REQ-001 整体派发计划

> REQ: REQ-001
> Dispatch policy: waves-v1
> Revision: 1
> Status: complete

> SIMULATION / test-only 未接收候选。没有发生过的锁定、批准、检查、回执与审查不在此预填；Main 按实际阶段使用原入口形成事实。

静态计划 complete 不代表任何执行或接收事实。原阶段实际锁定、审查后方可派发。

## W1 · 输入齐备的独立任务并行

- [ ] [TASK-001-01：创建交付来源台账条目](TASK-001-01.md)

S6 只有一个任务，其唯一写域 `simulation/deliveries/ledger-note.txt`；只读 `simulation/products/artifact.txt`，无写域竞争。独立模拟状态 `simulation/repairable/status.txt` 不由 S6 改写；S7 对照 READY 目标，真实差异交 S8/S9 原责任处理。

## Resource order

| Before | After | Resource | Reason |
|:---|:---|:---|:---|

## 集成与执行

Builder 真创建、检查、提交并交原 Result；Main 集成实际交付分支，合并后执行 `make check` 与 `make check DELIVERY=simulation/deliveries/ledger-note.txt`，再 ACK 与安全清理。未发生的检查、结果和来源不回填。
