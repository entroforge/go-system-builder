# L3 阶段设计

本层定义各阶段的目标、输入、工作流、角色、产物和出口。跨阶段对象与协议由 [L4](../l4-mechanisms/README.md)统一定义；阶段不另建一套执行或授权机制。

## 阶段索引

| 阶段 | 文档 | 交付重点 |
| --- | --- | --- |
| S0 | [需求设计](L3-S0-requirement-design.md) | 真实意图、验收与需求锁定 |
| S1 | [绑定与授权](L3-S1-bind.md) | 唯一活跃需求、开发分支与发布目标 |
| S2 | [设计](L3-S2-design.md) | 架构、行为、适用场景与设计依据 |
| S3 | [契约](L3-S3-contracts.md) | 共同模型、协议、分端责任与需求追溯 |
| S4 | [任务拆分](L3-S4-task-split.md) | 任务、完成判据、依赖与整体派发计划 |
| S5 | [文档验证](L3-S5-document-verification.md) | 独立审查规格链，锁定精确执行批次 |
| S6 | [构建](L3-S6-build.md) | 原生 Builder 交付与 Main 集成 |
| S7 | [完整验证轮](L3-S7-verification-round.md) | 验证计划、独立结果、干净轮或发现批次 |
| S8 | [发现调查](L3-S8-finding-investigation.md) | 竞争假设、根因、影响与修复合同 |
| S9 | [修复](L3-S9-repair.md) | 根因修复、证据处置、独立定向重验 |
| S10 | [验收与发布审计](L3-S10-acceptance-audit.md) | 需求兑现、发布风险与明确结论 |
| S11 | [人工发布门禁](L3-S11-release-gate.md) | 真实人的决定与固定去向 |

## 共用约定

各阶段的作者输入与系统事实，按 [L4 阶段答卷](../l4-mechanisms/L4-stage-form.md#24-机器事实必须由系统承接)分工。[逐阶段落实方案](../l4-mechanisms/L4-stage-form.md#121-各阶段落实机器与专业边界)覆盖 S0～S11 的正常准备、专业交付、来源固定、接收和恢复；它约束 Main 的分工与批准入口，也约束 Worker。不得把内部完整 DTO、SHA、revision、派生计数或校验时间重新交给 agent 填写；没有实际缺口的阶段不为形式一致新增机制。

每篇遵循 [Blueprint 编写与调查范式](../README.md#blueprint-编写与调查范式禁止删除本节)，围绕阶段问题、设计依据、专业工作、机制承载、完成与恢复形成连贯推导，按本阶段特点安排章节。必要字段和示例应核对实际模板、Schema 与消费者，保留理解设计所需的解释；历史方案与运行日志不进入正文。

面向代理的反馈给出当前责任、实际资料与答卷完整路径、具体缺项和下一动作。资料按需展开，不要求为了开工先读完整协议，也不要求手抄 Runtime、哈希或 revision。

正式输入按各消费者声明使用已提交 Git tree、本人工作区或磁盘证据、Runtime 快照，不能按文件名猜来源。需要下游从提交读取的规格、代码和测试应完成相应提交；私人计划、答卷、机器回执与不入库证据遵循各自原契约，不为交接制造无内容提交。来源与事务见 [L4 状态机](../l4-mechanisms/L4-state-transition-core.md#input-contract)。

## 机制分工

| 载体 | 职责 |
| --- | --- |
| Main | 专业规划、实际原生派发、依赖管理、集成与异常交接 |
| 角色与技能 | 专业能力、工作方法和明确的产物要求 |
| 模板与问卷 | 引导判断，提供实际输入、答案形状和准确位置 |
| Claude Code | 子代理创建、前后台执行、消息、恢复和结束 |
| SubagentStart / SubagentStop | 衔接子代理的工程上下文与本人专业结果 |
| Main 的保存监听 | 将本人的规划、影响、验收或真实决定交对应领域消费者 |
| Runtime Writer 与领域门 | 原子接收、幂等、工程事实与阶段推进 |

普通 Worker 工具事件不接收结果、不激活责任。Main 的读取、消息和保存也不能抢收 Worker 私人答案。本人完成、Main 集成、阶段完成分别判断，真实阻塞可交接。

原生派发统一见 [L4 子代理工程衔接](../l4-mechanisms/L4-agent-dispatch-governance.md)，答卷接收见 [L4 阶段答卷](../l4-mechanisms/L4-stage-form.md)，事件边界见 [L4 Hook 接线](../l4-mechanisms/L4-hook-platform-wiring.md)。工作责任使用 [五态模型](../l4-mechanisms/L4-state-transition-core.md#unified-work-state)，不把专业结论或资源状态当成新的工作状态。
