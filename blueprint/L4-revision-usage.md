# L4 — Runtime revision 使用与命令协调

> 层：第四层｜机制域：Runtime revision 的内部序号、单写者协调、命令默认语义、对象版本边界与复杂度准入
>
> 上游：L1 D1/D3/D4/D7 与五公理；L2 生命周期目标；全部 L3 Stage
>
> 姊妹篇：[L4 权威状态机与迁移事务核心](L4-state-transition-core.md)负责“事实怎样原子写入与恢复”；[L4 运行时控制面与横切治理](L4-runtime-control-plane.md)负责“内容和证据怎样合规”；本文只负责“revision 到底是什么、谁需要知道、命令怎样不把它变成人工操作税”。
>
> 权威声明：**revision 是 Runtime 内部的提交序号，不是 Agent 的正常业务输入，不是防篡改机制，也不是人类授权凭证。**各 L3 只声明消费哪一种对象事实；revision 的统一语义、外部可见性和迁移规则以本文为准。
>
> 状态：v0.1.0（2026-09-01）。Runtime 写入命令已支持省略 `--expected-revision`；参数仅作为显式高级断言保留。本文件记录统一契约，不把内部提交序号重新变成人工操作步骤。

## 0. 准入逻辑

revision 横跨 S1～S11，并同时出现在 Runtime、ReviewPlan、Case、Contract、Assignment 和证据记录中。此前各 L3 把它分别解释成 CAS、证据新鲜度、审批防重放、计划修订号和对象版本，导致 Agent 在正常流转时必须反复读取、计算和传递数字。它满足 L4 的“共享机制域 + 独立失败面 + 多 Stage 消费”准入条件，但准入结果不是把实现细节继续下沉给每个 Stage，而是收敛成一条最小外部契约。

## 1. 运行模型与总原则

本项目的正式运行模型是：**一个 REQ 对应一个 active Runtime、一个主会话负责推进，一个 Runtime Writer 负责写控制面**。Worker 负责产出被分配的 artifact；Hook 和 Controller 负责触发、投影和提醒；它们不应让 Agent 自己维护第二套 Runtime 写协议。

因此：

1. Runtime Writer 在内部使用文件锁、原子写、pending marker 和 journal，保证一次提交不会被半写或互相覆盖；这些是实现细节，不是 Agent 操作步骤。
2. `revision` 在每次 Runtime 提交时由 Writer 自动递增，用于 journal 的 `before_revision/after_revision`、崩溃恢复、审计排序和诊断定位。
3. 正常命令不要求 Agent 读取或计算 revision。命令应在 Writer 锁内读取当前 Runtime，校验当前状态，应用动作并提交；省略参数不能退化为“让 Agent 自己查数再重试”。
4. `--expected-revision` 可以保留为高级调用者的显式断言、外部集成或恢复工具的兼容参数，但不是正常 Stage 路径的必填参数。
5. 如果两个 CLI 命令必须共同完成一个业务动作，优先提供一个原子复合命令；不要求 Agent 在两个命令之间手工传递 `X`、`X+1`。

### 1.1 单写者不等于取消所有物理安全

“不考虑并发”在这里的含义是：不为假设存在的多个业务 Agent 增加操作门槛。它不意味着删除文件锁、原子 rename、journal 或崩溃恢复，因为 Hook 进程、命令进程、重试和进程崩溃仍然是文件系统层面的事实。物理写入必须串行化；Agent 不必感知串行化过程。

## 2. revision 的用途边界

| 问题 | revision 是否负责 | 正确机制 |
|:--|:--|:--|
| 记录 Runtime 提交顺序 | 是 | Writer 自动生成 `before/after_revision` 和 journal sequence |
| 崩溃恢复时判断 state/journal 是否成对 | 是，作为内部一致性字段 | pending marker、state/journal 对账和原子写序 |
| 防止两个底层进程同时写文件 | 不是单独负责 | 文件锁；revision 只是锁内的一致性字段 |
| 检测 artifact 被修改 | 否 | artifact SHA-256、登记指纹和 baseline digest |
| 认证 `actor` 是不是真人 | 否 | 当前系统不提供身份认证；actor 只是审计记名 |
| 判断 REQ/代码/CASE 是否属于同一代 | 否 | `generation`、`review_round`、对象 hash 和 source refs |
| 防止 S11 decision 重复执行 | 不应单独负责 | 固定 disposition→transition、当前 cursor、幂等键和一次性 evidence ID |
| 让 Agent 知道下一步是什么 | 否 | Hook/status 的 `next` 投影和命令回执 |

revision 不是密码学防篡改边界。手工修改 state 中的数字不能被 revision 本身阻止；真正的文件身份来自 SHA，真正的提交完整性来自 state/journal/pending 的一致性检查。把 revision 写入 decision scope 也不能替代 artifact hash、生命周期状态或一次性消费语义。

## 3. Agent-facing 命令契约

### 3.1 正常路径

正常路径的最小交互是：Agent 读取 Hook/status 给出的当前对象和唯一 `next`，提供业务输入，调用对应命令，消费回执。回执可以展示 committed revision，但不得要求 Agent 把它复制到下一条命令。

目标调用形态：

```text
runtime <verb> <business-input>
  → Writer lock
  → read current Runtime/object pointer
  → validate current cursor, identity, hashes and semantic guards
  → apply one action
  → assign revision and append journal
  → return next/status projection
```

如果命令发现当前状态不再允许该业务动作，应按状态机语义返回一个明确的下一动作；不能把所有状态变化都包装成“请重新计算 revision”。

### 3.2 高级 CAS

外部集成、恢复工具或明确需要“只有当我看到的快照仍然未变才提交”的调用者可以传 `--expected-revision`。此时它是调用者主动选择的更强前置条件，失败后由工具按 current/expected/next-action 三字段解释。该参数不能成为 Stage 说明、Skill、Agent prompt 或人类操作手册的必读项。

### 3.3 多步业务动作

证据登记随后立即触发 transition 时，应由一个领域命令在同一个 Writer 事务内完成，或由 Runtime 内部生成并消费中间 evidence。不得让 Agent：

- 先读 `X`，再手工计算 `X+1`；
- 把 Runtime revision 写入 decision artifact；
- 用 revision 代替 artifact SHA 或对象 identity；
- 为了重试普通动作而阅读完整 `loop-state.json`。

S11 的 decision scope 应绑定 disposition、Runtime identity、当前可接受的 release package/context 和一次性 decision ID；不再把每次 evidence registration 产生的 Runtime revision 暴露为人工交接协议。

## 4. 对象版本的分层

不同对象仍然可以有自己的版本字段，但它们不是同一个 Runtime revision，也不要求 Agent 手工推进：

| 对象 | 版本的真实意义 | Agent 的正常消费方式 |
|:--|:--|:--|
| Runtime `revision` | Runtime 提交序号、审计/恢复游标 | 读取 status/next；不计算、不复制 |
| ReviewPlan `revision` | 同一 review round 的计划修订代号 | 调用 register/revise；使用命令生成的 ID、hash 和缺口 |
| InvestigationCase `revision` | Case 的不可变事实/假设/路由历史 | 使用 `case_id`、当前 hash 和 status 给出的下一动作 |
| RepairContract `revision` | Contract 与批准时 Case 事实的绑定版本 | 使用 approved Contract ref/hash；不在 S9 就地改版本 |
| Assignment revision | Assignment/结果交接的对象版本 | 使用 Assignment ID 和生成的 checkpoint；不手工编号 |
| `generation` / `review_round` | 需求基线代际/验证轮次 | 由当前 status 和 evidence context 给出；不能用 Runtime revision 替代 |

对象版本需要防止的是对象事实被静默替换；这个目的由不可变 artifact、SHA、source refs 和对象级消费规则完成。Runtime revision 只负责 Runtime 提交序列，不应跨对象复制语义。

## 5. 各 Stage 的消费规则

| Stage | 只需要关心的事实 | 不应要求 Agent 做的事 |
|:--|:--|:--|
| S1 | 候选 REQ、`--approved-by`、绑定回执 | 手算 bootstrap revision；把 CAS 当成绑定授权本身 |
| S6 | 当前 Assignment、Owner、scope、checks 和真实 diff | 手工维护 Assignment revision；为普通 Builder Result 复制 Runtime revision |
| S7 | ReviewPlan/Claim/Assignment/Result 的 ID、SHA、round 和 `next` | 把 Runtime revision 当 ReviewPlan revision；为普通 result submit 先查数再传 CAS |
| S8 | Case ID/hash、Finding exact set、Hypothesis/Result、Contract readiness | 手工推进 Case revision；把 Runtime revision 写入因果 artifact |
| S9 | approved Contract ref/hash、Assignment、实际 diff、targeted assertion | 手工计算 Contract/Runtime revision；为每个 repair unit 维护第二套版本号 |
| S10 | coverage inventory、evidence SHA、baseline/generation、release package | 用 Runtime revision 代替审计分母或 package currentness |
| S11 | 当前 release package、六枚举 disposition、一次性 decision ID | 不手工维护 Runtime revision；不把 `scope@revision` 作为人类操作前提 |

各 L3 只保留对本阶段有业务意义的 generation、round、object ID/hash 和 fixed transition；涉及 Runtime revision 的地方只引用本文，不再复制 CAS 解释和手工注册步骤。

## 6. 替代性防护组合

在单主会话模型下，最小且足够的防护组合是：

1. **单写者 + 文件锁**：控制物理写入顺序，Agent 不感知。
2. **状态机 cursor + fixed transition**：保证当前业务状态只接受合法动作。
3. **幂等键 + evidence/record ID**：重复提交同一业务动作时返回已有结果或明确拒绝。
4. **artifact SHA + baseline/generation/round**：判断输入事实是否发生业务意义上的变化。
5. **journal + revision**：记录已经发生的提交并支持恢复、审计和诊断。

只有当实证表明存在第二写者、外部集成或同一动作的不可接受重复风险时，才增加显式 CAS 或 revision scope。新增门禁必须同时给出威胁、消费者、失败恢复动作和操作成本；没有这四项，不得通过复杂度评审。

## 7. 当前实现迁移要求

当前 `internal/runtime.Store.Update` 在内部使用文件锁和 `expectedRevision`，但普通入口已经把“当前值读取、提交序号分配和写入”收回 Writer；CLI 不再先读 Runtime 再回填 revision。仍需保持以下边界：

1. 普通 Runtime 命令将 `--expected-revision` 作为可选显式断言；省略时由 Writer 在锁内读取当前值并执行，不能退化为 CLI 先读后把数字回填。
2. `runtime evidence add`、`runtime human-decision` 等紧邻的业务动作提供原子复合入口，或由 Runtime 负责生成 evidence scope。
3. S11 human decision scope 移除 Runtime revision 后缀，改由当前 cursor、decision ID、package/context identity 和 fixed transition 防止错用。
4. Case/Contract/Assignment 的对象版本继续由 Runtime/对象 API 生成；CLI 仍可为外部恢复保留显式 revision/hash 断言，但普通 Agent 路径只消费 status/next。
5. 删除 L3、Agent Protocol、Manual 中要求 Agent 复制 `X`、`X+1` 或“先查 revision 再注册”的操作说明；保留内部迁移和审计说明。

任何未来重新强制要求 `--expected-revision` 的命令都必须被标为实现回归，不能被描述成 Agent 正常流转的理论必要条件。

## 8. 复杂度评审规则

revision 相关设计进入复杂度评审时，必须回答：

- 本项目声明的运行模型是否真的允许多个业务写者？
- 该门禁防的是覆盖、篡改、重放、错误路由还是崩溃恢复中的哪一个？
- 是否已有更直接的机制承担该职责？
- 正常路径是否增加读取、计算、复制、重试或人工决策步骤？
- 门禁失败后，系统能否自动恢复，还是把内部实现细节转嫁给 Agent？

“理论上更安全”不是通过理由。若收益只出现在未声明的多写者模型，而成本每次正常流转都会发生，则应删除外部门禁或将其降为内部实现/高级可选断言。

## 9. 与其他 L4 的关系

- [状态机与迁移事务核心](L4-state-transition-core.md)：实现单写者、锁、原子写、journal 和恢复；不得把内部 `Update(expectedRevision, ...)` 形状直接当作 Agent 协议。
- [运行时控制面与横切治理](L4-runtime-control-plane.md)：引用本文的 revision 语义；证据新鲜度、artifact SHA、generation 和 round 不再借 revision 代言。
- [Agent 调度与治理](L4-agent-dispatch-governance.md)：Assignment revision 只属于调度对象；普通 Worker 不手工推进它。
- 各 L3：只声明业务事实和本阶段消费点；正常路径按 `next` 执行，不复制本文件的机制细节。

## 变更记录

| 日期 | 版本 | 变更 |
|:--|:--|:--|
| 2026-09-01 | v0.1.0 | 明确单 REQ/单主会话/单 Runtime Writer 的运行模型；将 revision 降为内部提交序号；拆分 CAS、指纹、身份、代际、重放和崩溃恢复职责；规定 Agent-facing 命令默认不要求手工 revision，并登记 CLI/文档迁移要求 |
