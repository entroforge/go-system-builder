# Project Map — 模拟离线归档清单

> SIMULATION / test-only 未接收候选。没有发生过的锁定、批准、检查、回执与审查不在此预填；Main 按实际阶段使用原入口形成事实。

## Project Facts

这是隔离纯文档模拟；无业务实现、UI/API/数据库/部署。预置只读输入 `simulation/products/offline-sample.txt`，S6 待创建输出 `simulation/deliveries/offline-note.txt`。目标是实际原责任从 S0 接续到模拟 S11。

## Baseline Index

所有本目录文档仅为未接收候选：REQ-001 → ARCHITECTURE-001 / ADR-001 → CONTRACTS-001 / BE-001 → TASK-001-01 / index-REQ-001。
当前阶段、已接收材料与下一责任以项目真实 Runtime、原答卷和原消费者为准；本图不填写 Runtime 身份、来源摘要、检查或审查历史。

## Decisions and checks

Main 按实际阶段通过原入口消费 `simulation/DECISIONS.md` 合成选择。S0–S5 预检只核对 marker 和未来输出尚未存在；S6 真创建输出后 Main 合并再检查。首个 Reviewer 明示模拟 Finding，自交原卷；S8/S11 不把预设选择当作已接收批准。
