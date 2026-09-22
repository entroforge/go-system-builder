# 文档类型与权威地图

按内容用途存放，阶段是关联信息。模板填写后才产生实例；报告只证明所指版本的观察结果。文档存在、目录层级和“active”标签均不证明实现通过。

| 目录 | 唯一职责 | 读取时机 |
| --- | --- | --- |
| [requirements](requirements/README.md) | REQ 输入、范围与验收基线 | 绑定需求与追溯上游 |
| [design](design/README.md) | 体验 Foundation、派生、完整原型包 | UI 与品牌相关工作 |
| [architecture](architecture/README.md) | 技术架构、模型、状态、数据流 | 技术设计和合同推导 |
| [dev](dev/README.md) | 合同、TASK、整体派发计划 | 规格、派发、实现 |
| [reports](reports/README.md) | 审查、缺陷、QA/E2E、验收、发布审计 | 验证与证据消费 |
| [control](control/README.md) | 阶段协议、Definition、Hook policy、受保护命令表 | 流程与权限执行 |
| [rules](rules/README.md) | 稳定执行约束 | 被当前动作或风险触发时 |
| [guides](guides/README.md) | 安装、入门和按需操作说明 | 人工查阅 |
| [examples](examples/README.md) | 自包含合成示例 | 学习、回归与夹具 |

`.claude/loop-state.json` 是当前事实，不从项目地图或聊天推断状态。Definition 决定合法迁移，policy 决定执行边界；Skill 提供方法，Agent Definition 提供角色。模板与示例不是已批准实例，历史报告不覆盖现役规范。

模板仓库的 L1–L4 设计文档不随目标项目安装。技术架构决策与体验决策按职责分别记录；`design/decisions/` 中的 ADR 模板用于体验决策与 Foundation 反馈。

工厂维护者入口：[框架分层](../blueprint/README.md)。
