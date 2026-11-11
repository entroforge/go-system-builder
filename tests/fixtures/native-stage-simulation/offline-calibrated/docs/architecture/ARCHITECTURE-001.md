# 架构：模拟离线归档清单

> 状态：draft
> 版本：v1.0.0
> Architect：Main
> 关联需求：`docs/requirements/REQ-001.md`

> SIMULATION / test-only 未接收候选。没有发生过的锁定、批准、检查、回执与审查不在此预填；Main 按实际阶段使用原入口形成事实。

## 1. 架构目标

离线样本清单条目以新文档显式记录模拟用途与来源。仅验证模拟文件及引用存在，不证明业务功能。

## 2. 系统上下文

准备器提供只读 `simulation/products/offline-sample.txt`；Builder 创建 `simulation/deliveries/offline-note.txt`；Main 实际合并并运行检查；原消费者接收本人答卷。

## 3. 容器视图

无服务容器、UI、API、数据库或部署。

## 4. 模块职责

| 模块 | 职责 | 输入 | 输出 |
|:--|:--|:--|:--|
| 离线样本清单条目 | 明确模拟用途、记录来源 | `simulation/products/offline-sample.txt` | `simulation/deliveries/offline-note.txt` |
| 原流程控制面 | 原责任接收与接续 | 实际原答卷和事件 | 原 Runtime 与回执；作者不得伪造 |

## 5. 数据流

输入 `simulation/products/offline-sample.txt` 保持 `SIMULATION_OFFLINE\n`；S6 从共同起点创建并提交新输出 `simulation/deliveries/offline-note.txt`；Main 集成实际交付分支后执行 `make check` 与 `make check DELIVERY=simulation/deliveries/offline-note.txt`。S0–S5 只预检输入及输出未存在，不要求未来输出已交付。

## 6. 状态机

无业务状态机。阶段事实由原消费者确定。

## 7. 数据模型

只读输入和新占位文档，无共享模型。

## 8. 接口契约

BE-001 §7 → TASK-001-01。FE/SYNC 不适用，依据 REQ §A4-1/§A4-2。

## 9. 安全设计

所有选择与结论标注 SIMULATION；正常工具、本地提交、Main 合并可用；不 push、部署或真实发布。

## 10. 性能基线

无业务性能指标；sidecar 只记录实际可观察时间。

## 11. 部署和回滚

无部署；保留失败与来源事实，沿原恢复入口继续。

## 12. 决定

方向为候选；Main 按实际 S2 原入口消费本次模拟选择，不预填历史批准。
