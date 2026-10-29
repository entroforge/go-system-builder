# SIM-08 完整整理：`pending system_followup` 混义

> 现场：`source-ledger` F-009（`loop-events.jsonl` sequence 16 `transition_committed TR-003`，
> `gate_id=GATE-DOCUMENT-PASS`，两个 guard 均通过）。结论：S5 第二份答卷**已被接收**，
> followup 指向阶段出口层面的恢复，不是答卷层面的重交。但提示措辞同时指向两者，
> Main 曾重交答案，白跑一趟。

## 1. 状态机事实（不争）

- 答案接收（submission accepted）与阶段推进（transition committed）是两个独立事实。
- `ControlStatus == "pending"` 的准确含义：**答案已收，出口未走**。
  机器侧用 `Waiting{Reason: "system_followup", Owner: "Main", Ref: FollowupID}` 表达，
  Owner 明确是 Main——这是给 Main 的活，不是给作者的活。
- 测试侧已锁机器语义（`stage_answer_e2e_test.go:472` 禁止 replay 报假 pending；
  `:899` 要求 control deferral 必须暴露给 Main）。机器通道是对的，问题只在人读的文案。

## 2. 六处人读文案逐条定级

| # | 位置 | 原文要点 | 定级 | 理由 |
|---|---|---|---|---|
| 1 | `internal/cli/s5_guidance.go:98`（S5 状态板） | "Review answer accepted; stage exit followup remains pending (system_followup …). Main inspects …, then uses …recover…; do not ask the reviewer to submit again." | **真混义，必修** | "accepted" 与 "pending" 并列，主语不明。这是 Main 主动看的状态板，无"本人可结束"上下文，误读概率最高；仿真踩的就是这处 |
| 2 | `internal/submission/receipt.go:263`（S5 回执） | "本人审查交付已接收，可以结束本责任；Main 检查原 system_followup …的具体错误，沿原接续恢复；…无需重新提交审查答案。" | 顺手修 | 作者视角清楚（可以走了）。Main 视角"检查…具体错误"没说 pending 的是出口；回执主受众是作者，伤害小一档 |
| 3 | `internal/submission/task_planning.go:310`（S4 回执） | "S4 本人交付已接收…Main 检查原 TR-002 门与 system_followup …的具体缺项。真实材料修复后使用 …recover… 接续原出口，不重交…" | 顺手修 | 同上；且已有点名 TR-002，比 S5 好半步 |
| 4 | `internal/submission/service.go:309`（S2 回执） | "本人交付已接收，可以结束本责任；Main 检查原接续 … 与后继准备，无需重交已接收答案。" | 顺手修 | 同上 |
| 5 | `internal/submission/design.go:263`（S2 回执 pending） | "Design report was recorded; the original stage outlet is pending. Resolve the named material gaps; … Do not repeat the professional report." | 可不动 | 主语是 outlet（出口），本来就是对的；只是英文，可顺手对齐中文口径 |
| 6 | `internal/submission/contracts.go:228,239`（S3 回执） | "S3 答卷已接收；Main/系统需恢复原阶段接续，无需重新提交合同判断。" | 可不动 | 主语清楚（恢复接续），已是正确示范 |

设计侧 `design.go:340` 的 preparationFailed（"preceding report recorded…preparation failed"）
主语清楚，不在修法范围。`Waiting` 结构化字段机器消费，不混义，不动。

## 3. 修法（文案层，零状态机改动）

- 第 1 处拆两句，第一句讲答案（已接收、禁重交），第二句讲出口
  （pending 的是出口 + 恢复命令），主语点名：

  > `Review answer accepted; do not submit again. Stage exit followup remains
  > pending (system_followup …): Main inspects the stage exit failure, then uses
  > …recover….`

- 第 2/3/4 处各加半句"阶段出口接续尚未完成"定语，不动结构：

  > `…Main 检查原 system_followup …（阶段出口接续尚未完成）的具体缺项…`

- 第 5/6 处不动（已是正确示范）。

## 4. 验证

- 定向跑 `TestS5*` 回执/状态板测试 + `stage_answer_e2e_test.go` 的 pending 断言；
- 文案改动不碰状态机，`work_status_test.go` 的 `workstate.Done` 映射不受影响。
