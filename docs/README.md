# 工程文档导航

本仓库是 Loop Harness 与工程模板的工厂。先按任务选择入口，文档层级不代表运行状态或实现已完成。

| 要做什么 | 入口 |
| --- | --- |
| 了解框架为什么这样设计 | [模板设计四层主线](../blueprint/README.md) |
| 安装到一个项目 | [安装指南](guides/install.md) |
| 在目标项目开始工作 | [入门](guides/getting-started.md)、[阶段协议](control/agent-protocol.md) |
| 查阅机制与操作说明 | [工程指南](guides/engineering-loop.md) |
| 找到文档类型、权威与发布边界 | [文档地图](DOCUMENT-MAP.md) |

## 目标项目交付物

[需求](requirements/README.md) → [技术架构](architecture/README.md)与[体验设计](design/README.md) → [开发合同和任务](dev/README.md) → [验证与验收报告](reports/README.md)。

[执行控制](control/README.md)定义合法流程，[规则](rules/README.md)定义稳定约束，[示例](examples/README.md)提供可运行参考。实例的当前事实只由 `.claude/loop-state.json` 表达。

根目录 `blueprint/` 是本模板的设计与维护文档，不属于目标项目文档，不随模板安装。安装包带有适合目标项目的独立导航；规范、模板、示例和历史报告不得相互替代。
