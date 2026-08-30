# Architecture Release Audit：{release-or-topic}

> 文件名：`YYYY-MM-DD_<release-or-topic>_architecture_audit.md`  
> 规则：`docs/rules/release-architecture-audit.md`  
> 结论：APPROVED / APPROVED_WITH_NON_BLOCKING_RISKS / BLOCKED  
> Architect：{name / agent}  
> 日期：YYYY-MM-DD  
> 关联版本 / PR / 分支：{link or identifier}
> Runtime ref：`{runtime-id}@{revision}`
> Clean-round evidence：`{review-evidence-ref}`

> 先完成 coverage inventory 和反证审查，再填写 Final Decision。审计不是
> “测试全绿”的复述；没有检查的项目必须保持 `UNKNOWN`，不能改写成 `N/A`
> 或 `APPROVED_WITH_NON_BLOCKING_RISKS`。

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

### 2.1 Coverage And Responsibility Reconciliation

| Inventory ID | Source / changed path / risk | Expected invariant | Owner / independent reviewer | Acceptance or audit evidence | Disposition |
|:---|:---|:---|:---|:---|:---|
| COV-001 | {REQ / path / risk} | {expected} | {owner} | {ACC / REV / QA / E2E / audit ref} | pass / N/A / UNKNOWN / fail |

The inventory is frozen for this audit. Removing an item requires a recorded
scope rationale tied to the locked REQ and changed paths; it must not be used
to make the metrics pass. Reuse the `COV-*` IDs from the ACC where possible;
add only audit-specific system-invariant items here.

### 2.2 Counterevidence Ledger

| Inventory ID / audit area | What would disprove PASS? | Adversarial check | Evidence | Outcome / route |
|:---|:---|:---|:---|:---|
| {COV-001} | {failure, boundary, permission, concurrency, migration, rollback or stale-doc case} | {check performed} | {ref} | pass / N/A / UNKNOWN / fail → {route} |

Every `pass` needs a concrete counterevidence check. An unanswered check is
`UNKNOWN` and blocks an approved conclusion.

The machine manifest must contain explicit rows for `requirement`, `contract`,
`changed_path`, and `audit_area`; a hard category cannot be omitted to create
an empty 100% denominator. Use an evidence-backed `not_applicable` row only
when the locked scope proves that category is genuinely out of scope.

Machine companion: save the frozen ledger as JSON and validate all eight audit
areas before registering the audit evidence:

```text
loop-harness s10 manifest validate --file <release-audit-manifest.json> --type release_audit
```

If the audit result is `BLOCKED`, use `--outcome blocked` so the structured
ledger can preserve its blocking findings and route the registered envelope
through TR-018 to `paused`. This routed validation still requires the complete
inventory, counterevidence links, and eight audit-area rows; it only relaxes
the zero-blocker requirement needed by an approval artifact.

Copyable JSON shape: `docs/examples/s10/release-audit-manifest.json`.

The release-audit evidence envelope must carry `audit_manifest_path` and
`audit_manifest_sha256`. The Quality Gate consumes the JSON ledger described
by `internal/schema/assets/s10-audit-manifest.schema.json`; this Markdown is
the human-readable explanation and cannot replace the ledger. After
validation, register the envelope with `runtime evidence add`; do not invoke
`runtime transition` manually.

### 2.3 Markdown 渲染（single source）

本模板各节的表格正文由 S10 manifest 渲染生成，而不是手工维护的第二份载体：
manifest 是机器事实的唯一来源，Markdown 只是其人类可读投影。

```text
loop-harness s10 manifest render --file <release-audit-manifest.json> \
  --output docs/reports/audits/<audit-id>.md
```

渲染前 manifest 必须先通过 `s10 manifest validate`；渲染器不自行校验之外
的事实。手工编辑渲染产物不会改变 Gate 消费的 JSON ledger；如需修改结论，
修改 manifest 后重新渲染。

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
- [ ] 本次 coverage inventory 是否完整、冻结且 100% 有 disposition？
- [ ] 每个 PASS 的反证问题是否有证据回答？是否仍有 `UNKNOWN`、无 owner 风险或无 tracking 的技术债？

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
