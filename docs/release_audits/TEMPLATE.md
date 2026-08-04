# Architecture Release Audit：{release-or-topic}

> 文件名：`YYYY-MM-DD_<release-or-topic>_architecture_audit.md`  
> 规则：`docs/rules/release-architecture-audit.md`  
> 结论：APPROVED / APPROVED_WITH_NON_BLOCKING_RISKS / BLOCKED  
> Architect：{name / agent}  
> 日期：YYYY-MM-DD  
> 关联版本 / PR / 分支：{link or identifier}
> Runtime ref：`{runtime-id}@{revision}`
> Clean-round evidence：`{review-evidence-ref}`

## 1. Release Scope

| 项 | 内容 |
|:---|:---|
| 版本 / Topic | {release-or-topic} |
| 目标分支 | master / main |
| 影响模块 | {modules} |
| 关联需求 | {REQ / bug / TD} |
| 关联合同 | {contracts} |

### Document Links

| 关系 | 文档 | 用途 |
|:---|:---|:---|
| upstream | `docs/requirements/REQ-{id}.md` | 发布范围 |
| upstream | `docs/tasks/index.md` | 完成任务 |
| evidence | `docs/reports/review/REV-{id}.md` | 交付正确性 |
| evidence | `docs/reports/qa/QA-{id}.md` | 工程质量 |
| evidence | `docs/reports/acceptance/ACC-{id}.md` | 验收与接管 |
| related | `docs/reports/bugs/BUG-{id}.md` | 缺陷状态 |

## 2. Changed Paths

| 路径 | 变更类型 | 风险 |
|:---|:---|:---|
| `{path}` | added / changed / deleted | low / medium / high |

## 3. State Machine Audit

### 3.1 Impacted States

```text
{state flow}
```

### 3.2 Checks

| 检查项 | 结论 | 证据 |
|:---|:---|:---|
| 新增状态有进入条件 | pass/fail/NA | {evidence} |
| 新增状态有退出条件 | pass/fail/NA | {evidence} |
| 失败路径区分 retryable / non-retryable | pass/fail/NA | {evidence} |
| retry 使用持久化 backoff | pass/fail/NA | {evidence} |
| 控制面能解释状态 | pass/fail/NA | {evidence} |

## 4. Transaction / UoW / Session Audit

| 检查项 | 结论 | 证据 |
|:---|:---|:---|
| 同一业务写入不跨多个 session 写同一对象 | pass/fail/NA | {evidence} |
| 长耗时 I/O 不持有 DB 写事务 | pass/fail/NA | {evidence} |
| flush/commit 失败后立即 rollback | pass/fail/NA | {evidence} |
| failure recorder 使用独立 session | pass/fail/NA | {evidence} |
| raw SQL 未绕过必要 defaults / hooks / 审计字段 | pass/fail/NA | {evidence} |

## 5. Concurrency and Idempotency Audit

| 检查项 | 结论 | 证据 |
|:---|:---|:---|
| 扫描任务有 claim / lease / SKIP LOCKED | pass/fail/NA | {evidence} |
| 多 worker 不会领取同一任务 | pass/fail/NA | {evidence} |
| 创建逻辑有 DB upsert 或唯一键兜底 | pass/fail/NA | {evidence} |
| retry / 重复事件 / 进程重启后幂等 | pass/fail/NA | {evidence} |
| 跨实例安全不依赖进程内 lock | pass/fail/NA | {evidence} |

## 6. Data Model / Identity / Migration Audit

| 检查项 | 结论 | 证据 |
|:---|:---|:---|
| 唯一键与业务 identity 一致 | pass/fail/NA | {evidence} |
| 默认值不会污染 identity | pass/fail/NA | {evidence} |
| 历史数据满足新约束 | pass/fail/NA | {evidence} |
| migration 可重复执行、可验证、可回滚或有补救方案 | pass/fail/NA | {evidence} |
| enum/string 扩展已同步 schema / API / 前端 / 文档 | pass/fail/NA | {evidence} |

## 7. Call Site and Runtime Topology Audit

| 检查项 | 结论 | 证据 |
|:---|:---|:---|
| 新增参数所有 call site 显式传递 | pass/fail/NA | {evidence} |
| 共享状态对象没有被错误拆成多个实例 | pass/fail/NA | {evidence} |
| 隔离状态对象没有被错误做成全局单例 | pass/fail/NA | {evidence} |
| 后台 loop / event handler / admin API 使用一致 DI | pass/fail/NA | {evidence} |
| 测试覆盖真实运行路径 | pass/fail/NA | {evidence} |

## 8. Observability Audit

| 检查项 | 结论 | 证据 |
|:---|:---|:---|
| 新异常映射为稳定短错误码 | pass/fail/NA | {evidence} |
| DB 记录错误码而非长 traceback | pass/fail/NA | {evidence} |
| 日志保留原始异常和业务上下文 | pass/fail/NA | {evidence} |
| 控制面能区分等待、重试、失败、人工处理 | pass/fail/NA | {evidence} |
| 有诊断 SQL 或 runtime metric | pass/fail/NA | {evidence} |

## 9. Verification Evidence

| Evidence | Path | Result | Release Relevance |
|:---|:---|:---|:---|
| Builder commands | `docs/tasks/TASK-{id}.md` | pass/fail | build and test execution |
| Delivery verification | `docs/reports/review/REV-{id}.md` | PASS/FIX_REQUIRED | functional, contract, integration, regression |
| QA | `docs/reports/qa/QA-{id}.md` | PASS/RELEASE_BLOCKED | coverage, security, performance, reliability |

## 10. Documentation and Release Boundary

| 检查项 | 结论 | 证据 |
|:---|:---|:---|
| bug / TD / review report 反映当前实现状态 | pass/fail/NA | {evidence} |
| 范围外代码未混入，或已补齐迁移/测试/部署说明 | pass/fail/NA | {evidence} |
| 技术债已明确标注 | pass/fail/NA | {evidence} |
| 数据修复脚本有执行前后校验和防御性 WHERE | pass/fail/NA | {evidence} |
| 发布说明包含迁移顺序、回滚策略、验证命令 | pass/fail/NA | {evidence} |

## 11. Release Changes

| 类型 | 变更 | 兼容性 | 迁移/操作 | 回滚 |
|:---|:---|:---|:---|:---|
| feature / fix / contract / data / config | {变更摘要} | compatible / breaking | {步骤或 N/A} | {方式} |

## 12. Blocking Findings

| 编号 | 严重度 | 问题 | 证据 | 处理要求 | Owner |
|:---|:---|:---|:---|:---|:---|
| ARA-001 | P0/P1/P2 | {issue} | {evidence} | {required action} | {owner} |

## 13. Non-Blocking Risks

| 风险 | 影响 | Owner | 后续 TD / bug |
|:---|:---|:---|:---|
| {risk} | {impact} | {owner} | {link} |

## 14. Sign-off Questions

- [ ] 本版本新增或修改了哪些状态？每个状态怎么退出？
- [ ] 哪些链路跨 session / UoW？是否必须统一事务边界？
- [ ] 哪些任务可能并发执行？并发时如何幂等？
- [ ] 哪些唯一键或索引改变了业务 identity？历史数据是否满足？
- [ ] 哪些 raw SQL 绕过了 ORM 行为？是否补齐 defaults 和审计字段？
- [ ] 哪些失败会自动重试？重试是否持久化？
- [ ] 哪些失败需要人工处理？控制面如何展示？
- [ ] 多实例运行时是否安全？
- [ ] 测试是否覆盖真实 DB / migration / 并发，而不是只覆盖 mock？
- [ ] 文档、TD、review report 是否与代码一致？

## 15. Final Decision

结论只能选择一个：

- [ ] APPROVED
- [ ] APPROVED_WITH_NON_BLOCKING_RISKS
- [ ] BLOCKED

说明：

{decision rationale}

## 16. Human Release Gate

| 项 | 状态 |
|:---|:---|
| Loop status | inactive / awaiting_human_release |
| Delivery verification evidence | pass/fail |
| QA evidence | pass/fail |
| Release architecture audit | pending / approved / blocked |
| Human release approval | pending / approved / rejected |
| Squash merge authorization | pending / approved / rejected |

Loop mode may prepare and evaluate this engineering audit. It cannot grant
human release approval or execute the final squash merge to `master/main`.
