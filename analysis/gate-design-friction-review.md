# 本项目门禁设计与 Agent 摩擦审查

> 状态：discovery
> 日期：2026-07-18
> 范围：Loop Gate、Guard、Hook、Agent/TASK/BUG 生命周期与必需产出
> 说明：本文不修改现有 Loop 权威，不授权实现。所有建议仍需行为 Eval 和正式 REQ 锁定。
> 历史定位：本文保留为门禁审查证据；最终推荐以
> `analysis/final-project-development-workflow.md` 为准。

## 1. 核心判断

门禁的目标不是让 Agent 证明“流程做过了”，而是防止以下损失：

- 人类意图被静默改变；
- 未授权或不可逆副作用发生；
- 高风险缺陷进入集成或发布；
- 过期、伪造或错配 Evidence 支撑完成声明；
- 并发写入、状态竞争和恢复失败破坏权威事实。

除此之外，设计文档、合同、TASK、Team Manifest、Readback、Activation、
Completion Report、Review Report、ACC 和 Release Audit 都只是可能的证明手段，
不是天然需要永久保留的流程目标。

因此建议采用一条门禁宪法：

> 约束副作用和完成声明，尽量不约束思考路径；锁定稳定边界，尽量不锁定
> 内部实现；只要求能改变决策的 Evidence，不要求重复叙述同一个事实。

本项目当前最大的风险不是“门禁数量多”本身，而是多层门禁发生乘法：

```text
阶段 Gate
× Phase Machine
× Agent/TASK/BUG Lifecycle
× 每责任独立 Agent
× 每 Agent 两阶段激活
× Delivery → QA → E2E 串行
× 每层独立报告和 Evidence
```

这会把 Agent 的主要精力从探索、实现和验证，转移到上下文重读、字段同步、
状态推进、重复报告和等待下一责任完成。

## 2. 当前门禁规模

当前 `docs/loop-definition.json` 暴露：

| 类别 | 数量 |
|:---|---:|
| 顶层 transitions | 24 |
| 全局 transitions | 5 |
| Verification / BUG phase transitions | 12 |
| Agent lifecycle transitions | 12 |
| TASK lifecycle transitions | 8 |
| BUG lifecycle transitions | 10 |
| 合计 transition 定义 | 71 |
| Guard 引用 / 唯一 Guard | 81 / 70 |
| required evidence 引用 / 唯一 kind | 65 / 23 |

并不是每个任务都会经过全部 71 个 transition，但任何一次普通交付都需要同时
理解和维护多层状态的一致性。复杂度由路径组合产生，而不是由单个数字产生。

### 2.1 普通改动的固定责任基线

当前 Team Validator 至少要求以下 disposition：

- Document Verification：2 个 mandatory responsibilities；
- Delivery Verification：3 个 mandatory responsibilities；
- QA：4 个 mandatory responsibilities；
- E2E Browser：2 个 mandatory responsibilities；
- 另加每个 TASK 的 Builder 和风险触发责任。

虽然部分责任可以声明 N/A，但每个责任仍需要被枚举、分类和记录；适用责任又
要求 assignment、manifest、readback、approval、activation、report 和 Evidence。
对于小改动，这些固定成本可能显著高于实现与测试本身。

### 2.2 五类主要摩擦

1. **上下文税**：Agent 反复读取 REQ → Design → Contract → TASK → Rules，
   并在 readback 中重新表达。
2. **状态税**：同一工作事实同时体现在 Agent、TASK、Team、Evidence 和顶层
   Runtime 中，需要保持同步。
3. **串行税**：Delivery、QA、E2E 被强制按顺序运行，即使它们读取的是同一
   冻结快照且没有依赖关系。
4. **重新证明税**：局部修复经过 targeted re-verification 后仍必须重启完整
   Delivery + QA + E2E round。
5. **产出税**：同一 Evidence 被 Completion Report、REV/QA/E2E、ACC、Release
   Audit 和 Gateway Package 多次映射或转述。

## 3. 判断一个 Gate 是否值得存在

每个 Gate 都应回答以下问题；无法回答时，默认不能成为 blocking Gate。

| 问题 | 要求 |
|:---|:---|
| 它防止什么具体损失？ | 不能只写“保证质量” |
| 损失是否在此刻不可逆或代价显著上升？ | 若以后低成本可发现，不必现在阻塞 |
| 它是否能发现前一层发现不了的问题？ | 重复检查不算新增价值 |
| 判定能否由机器稳定完成？ | 不稳定判断应 advisory 或升级，不应伪装 hard gate |
| 它要求的 Evidence 是否会改变决策？ | 不改变决策的报告应生成或删除 |
| 是否有更低成本的等价证明？ | 有则允许替代，不锁死产出形式 |
| 误阻塞如何恢复？ | 必须局部恢复，不能默认重跑全流程 |
| 如何知道 Gate 长期有效？ | 记录 catch、false positive、cost、bypass 和回退 |

可以使用一个非精确但有用的经济模型：

```text
Gate Safety Value
= 缺陷到达概率 × Gate 独立检出率 × 避免损失

Gate Friction
= Agent turns + token/context + wall time + 状态同步
 + 误报恢复 + 重复 Evidence + 串行等待
```

只有增量 Safety Value 明显高于 Friction，才值得成为 blocking Gate。

## 4. 不变量与流程实现必须分离

当前 INV-001..016 混合了三种不同层次：

1. 真正的宪法级边界；
2. 当前选择的一种实现机制；
3. 当前选择的一种验证规模。

它们不应拥有相同的稳定性。

### 4.1 应继续作为硬不变量

| 当前内容 | 建议 |
|:---|:---|
| 一个 Runtime 绑定一个明确的人类意图基线 | 保留，但允许在正式绑定前存在隔离的 Discovery/Spike 授权 |
| 自动化不能修改锁定 REQ | 保留 hard block |
| Runtime 由 Harness 单写、CAS、Journal 恢复 | 保留 hard invariant |
| 不可逆、安全、生产数据和受保护发布动作需人类批准 | 保留 hard block |
| 自动化终止于 human release boundary | 保留 |
| 过期或失效 Evidence 不能支撑完成 | 保留，但按影响图局部失效 |
| 任何 REQ 语义变化交还人类 | 保留 |

### 4.2 应改为条件化策略

| 当前不变量/门禁 | 问题 | 建议 |
|:---|:---|:---|
| UI 改动必须先有完整 HTML + stories + flows 原型包 | 小 UI 修正也支付整包成本 | 由 UI 风险、交互新颖度和用户流变化触发；简单视觉/文案改动允许现状 diff + screenshot contract |
| Contract + TASK 必须联合独立验证后才能实现 | 低风险单任务也需要两名文档责任 | 多 Agent、跨组件或高歧义时保留；简单任务用 schema + 单次 challenge review |
| 所有 Agent 都完整两阶段激活 | 对只读 reviewer 也有 readback/approval/activation ceremony | 写入或高风险工具使用两阶段；纯只读 assignment 使用单阶段 capability token |
| 每种验证都要完整 single-responsibility manifest | 责任拆分被误当成独立性 | 允许一个 reviewer 合并兼容责任；只有利益冲突、不同权限或高判断风险才 must-separate |
| 每个 blocking finding 都先 Canonical BUG 再修 | 局部、明显、未逃逸缺陷也进入完整 BUG 流程 | task-local finding 直接 reopen TASK；跨范围、复发、逃逸、发布阻断或需 RCA 时升级 BUG |
| original finder 必须 targeted reverify | Agent 不可用时形成单点瓶颈 | 原责任优先；等价独立责任 + 相同 finding contract 可替代 |
| targeted PASS 后必须完整 Review 重启 | 选择性修复也清空所有验证价值 | 依据影响图重算 proof obligations；只在跨切面或 release-critical 变化时重启完整 round |
| ACC 和 release audit 必须作为独立文档存在 | 大量内容只是已有 Evidence 的再次映射 | Runtime 自动生成 projection；只有决策、风险接受和人工说明需要手写 |

### 4.3 应降级或删除的流程约束

- “一个 assignment 只能有一个 responsibility”不应是普遍规则；独立性来自
  conflict graph，而不是 responsibility 名称数量。
- `30 files / 3 material modules` 应是拆分提示，不应是没有历史数据支撑的
  hard threshold。
- Delivery → QA → E2E 的固定串行应删除；同一 frozen snapshot 上无依赖的
  reviewer 可以并行，Clean Round 用 snapshot ID 而不是执行顺序定义。
- 非 UI 改动不应启动浏览器来证明 E2E N/A；Impact Analysis 的可审计结论
  本身就是 N/A Evidence。
- 不应要求所有改动都创建新的 Architecture、FE、BE、SYNC 文件；只有发生
  对应稳定边界变化时才需要对应合同。
- 纯机械检查不应生成 Agent 报告；Harness 应直接记录命令、结果和指纹。

## 5. 对各阶段 Gate 的具体评估

### 5.1 REQ 与 Planning

值得保留的是“人类意图不可被自动化静默改变”，不是“实现前必须完整知道
所有设计细节”。

建议将入口分成两种授权：

```text
Discovery Authorization
  允许：只读探索、隔离 Spike、测量、原型、失败实验
  禁止：生产集成、迁移、正式行为承诺

Delivery Authorization
  要求：锁定用户价值、范围边界、Acceptance 和人类控制点
  允许：在这些边界内自主调整设计、TASK 和实现
```

Architecture/Contract/TASK 应按变化触发，而不是按阶段固定产出：

| 变化 | 最小规划产出 |
|:---|:---|
| 文案、非规范文档 | impact note |
| 低风险局部行为 | bounded change contract + acceptance checks |
| 跨 TASK 接口 | interface contract + dependency graph |
| 新状态/事务/权限/迁移 | architecture decision + specialized contract |
| 高不确定第三方能力 | Discovery Evidence，再决定合同 |

### 5.2 Document Verification

Document Verification 的真实目标有两个：

1. 用户意图是否被合同正确表达；
2. Builder 是否拿到足够且不矛盾的边界。

这两个判断在复杂变更中值得独立 reviewer，但不必永久绑定“两名 Agent + 两套
activation + 两份 REV”。建议：

- targeted profile：机器 traceability check + 一个 reviewer 的两个 verdict；
- standard profile：一个 reviewer，可在同一上下文判断 consistency/executability；
- full-depth profile：两个独立责任并行；
- reviewer 发现 Plan 本身不再有效时，允许回到 Design Validity，而不是只能
  修补已标记条款。

### 5.3 Assignment 与 Activation

当前两阶段激活有合理目标：防止错误 Agent 在错误范围写入。但目前大部分越界
Hook 是 `warn`，系统同时支付完整 ceremony，又没有得到等强的副作用保护。

建议按 capability 分层：

| Capability | 授权方式 |
|:---|:---|
| 只读搜索、代码阅读、review | assignment token；无需 phase-two write activation |
| 工作区内普通文件写入 | scope-bound activation；可由 schema + fingerprint 自动批准 |
| 依赖安装、生成器、迁移、权限/策略文件 | explicit two-phase approval |
| 生产数据、受保护分支、发布 | human hard gate |

同时把 manifest、readback 和 activation 中重复的 scope 字段收敛成一个
fingerprinted Assignment Contract；其他消息只引用其 ID/hash 和 delta。

### 5.4 Delivery、QA 与 E2E

验证应围绕 Claim 和 Risk，而不是围绕固定组织架构。

建议把责任列表从 mandatory teams 改成 proof obligation matrix：

```text
change facts + risk tags + stable claims
                 ↓
applicable proof obligations
                 ↓
最小无冲突 assignment cover
                 ↓
同一 frozen snapshot 并行验证
```

例如：

- `QA-MODULE-CODE`、`QA-REUSE-ABSTRACTION` 和普通 `VER-MODULE-COMPLETE`
  通常可由一个高能力 task reviewer 一次完成；
- security、migration、concurrency、release boundary 仍应保持独立 reviewer；
- Unit/Integration 结果若已由 Builder 对当前 SHA 运行，reviewer 只审测试质量和
  缺口，不应无理由重复完整命令；
- E2E 只在用户流、浏览器边界、前后端同步或历史回归风险触发；
- Clean Round 定义为“同一 snapshot 的所有 applicable obligations 通过”，
  而不是“三支固定 Team 按顺序走完”。

### 5.5 Finding 与 BUG

Systematic Debugging 值得保留，但“必须先写完整 BUG Artifact”不是根因思考的
唯一载体。建议分三级：

| Finding 类别 | 修复路径 |
|:---|:---|
| task-local、显然、尚未跨 Gate | reopen TASK → causal note → fix → regression |
| 跨责任、原因不明、重复失败 | investigation → Canonical BUG → scoped repair |
| 安全、数据、迁移、生产逃逸、发布阻断 | Canonical BUG + independent approval + targeted reverify |

所有级别都禁止无证据盲修，但只有后两级要求完整 BUG lifecycle。

修复后的验证范围由实际影响决定：

- correction scope Evidence 失效；
- dependency graph 上受影响 obligations 失效；
- 不受影响且 snapshot-compatible 的 Evidence 保留；
- 只有风险升级或跨切面变化才重新启动 full-depth round。

### 5.6 Acceptance 与 Release Audit

Acceptance 的核心是“每个稳定 Claim 是否有当前 Evidence”，它本质上是一张
机器可生成的图，不应要求 Agent再次抄写已有内容。

建议：

- ACC 默认由 Runtime projection 生成；
- 人只补充风险接受、业务解释和未自动化判断；
- Release Audit 由 risk tags 选择章节；不相关章节直接由 Applicability Engine
  生成 N/A 依据，不要求 Agent逐格填写；
- 非 release-bound 的中间 change batch 不生成 release package；
- 正式发布仍保留独立审计和人类 Gateway。

## 6. 目标门禁架构

建议将几十个流程停顿收敛为四个真正的 blocking moments：

```text
1. Intent / Discovery Authorization
   人类意图或探索边界明确

2. Side-effect Authorization
   写入、权限和高风险能力在有效 scope 内

3. Integration Claim Gate
   所有 applicable Claim 有当前 Evidence，无 blocking finding

4. Human Release Gate
   不可逆集成、发布和生产动作由人决定
```

中间仍可存在很多机器检查，但机器检查不等于对话停顿，也不等于必须创建一份
Markdown。能自动批量计算的检查应在边界一次性给出完整失败集合，而不是让 Agent
逐个 Gate 来回修复。

### 6.1 Gate 应验证属性，而不是强制仪式

例如，应验证：

- 意图已锁定；
- assignment scope 足够且未越权；
- interface compatibility 成立；
- applicable risks 有证明；
- Evidence 对应当前 snapshot；
- release 仍需人类批准。

不应直接验证：

- 一定创建了几个 Agent；
- 一定生成了某个命名的 Markdown；
- reviewer 一定按固定顺序运行；
- 每个 responsibility 一定由不同 Agent 完成；
- 每个修复一定经过相同长度的 BUG 流程。

## 7. 分层候选 REQ

### REQ-G0-001 最小充分治理

工作流必须以最少的 blocking moments 保护人类意图、副作用权限、Evidence
真实性和发布边界；不得把流程完成度本身作为产品质量的替代指标。

### REQ-G0-002 Agent 自由位于稳定边界之内

锁定 REQ 的用户价值、Acceptance、不可逆边界和稳定接口后，Agent 可以自主
探索、调整内部设计、合并任务和选择验证手段；只有越过稳定边界才升级 Gate。

### REQ-G0-003 一项事实只证明一次

同一事实必须有一个权威 Evidence source；其他报告只引用或生成 projection，
不得要求 Agent 在多个 Artifact 中重复叙述并手工保持一致。

### REQ-GATE-101 Gate Registry 与正当性（P0）

#### L2 功能需求

- 每个 Gate 声明 `protected_loss`、`applicability`、`decision_owner`、
  `enforcement`、`required_claims`、`accepted_evidence`、`recovery` 和 `sunset`。
- `enforcement` 只能是 `hard`、`computed`、`advisory`；名称不得暗示比实现
  更强的约束。
- 新增 blocking Gate 必须说明它相对已有 Gate 的独立检出价值；否则必须
  合并、替换或保持 advisory。
- 没有真实语义实现或行为 Evidence 的 Gate 不能标记 `hard`。

#### L3 验收

- 新 Gate 只写“保证质量”而无具体 protected loss，schema 拒绝。
- hard Gate 仍映射到 Evidence 非空 stub，doctor 失败。
- 两个 Gate 保护同一 Claim 且接受同一 Evidence，报告建议合并。

### REQ-GATE-102 Applicability 与 Proof Obligation Engine（P1）

#### L2 功能需求

- 根据 change facts、risk、uncertainty、历史缺陷和 stable claims 计算
  applicable obligations。
- N/A 是可审计的机器结论，不要求为不适用责任创建 Agent 或空报告。
- 风险升级增加 obligations；风险降低不得删除已触发的人类边界。
- Clean Round 改为同一 snapshot 上所有 applicable obligations 通过。

#### L3 验收

- 非 UI 文档改动不创建 E2E assignment，以 impact result 证明 N/A。
- migration change 自动增加 rollback、real-data rehearsal 和独立 reviewer。
- 实现中发现权限影响后，security obligation 自动加入且旧 clean result 失效。

### REQ-GATE-103 Progressive Assignment Authorization（P1）

#### L2 功能需求

- 只读、普通写入、高风险工具、human-only action 使用不同授权强度。
- Assignment Contract 成为 scope 唯一来源；manifest/readback/activation 只引用
  ID/hash 或声明 delta。
- 兼容 responsibility 可以由同一 Agent 覆盖；冲突图只表达真正必须分离的
  权限、利益和判断边界。
- 固定文件数/模块数阈值只作为拆分建议，由历史 telemetry 校准。

#### L3 验收

- 纯只读 reviewer 无需 phase-two activation 即可开始 review。
- 一个 reviewer 可同时给出 module completeness 与 ordinary code-quality 两个
  verdict，但不能同时验证自己实现的代码。
- security reviewer 与相关 Builder 的 assignment 被 conflict edge 强制分离。

### REQ-GATE-104 Evidence Graph 与生成型 Artifact（P1）

#### L2 功能需求

- Claim、Evidence、snapshot、risk obligation、assignment 和 invalidation 构成
  权威图。
- Completion Report、TASK status、ACC、Release Package 的重复字段由该图生成。
- 人工 Artifact 只保存新判断、例外、风险接受和无法自动计算的信息。
- 同一 Evidence 不因展示在不同 report 中被复制成多个权威记录。

#### L3 验收

- ACC 可以从当前 graph 确定性重建；删除生成文件不丢失权威事实。
- Builder 命令结果已记录后，completion projection 自动引用，无需再次粘贴。
- Evidence 失效后，所有 projection 同时反映 stale，不发生手工文档漂移。

### REQ-GATE-105 比例化 Correction Loop（P1）

#### L2 功能需求

- Finding 按 task-local、systemic、high-risk 分类，选择不同修复路径。
- Canonical BUG 只在跨 Gate、复发、原因不明、高风险或发布阻断时强制。
- targeted repair 只失效 dependency graph 上受影响 obligations。
- 原发现责任优先复验；不可用时允许等价独立责任替代。
- full round restart 必须由影响升级或 release profile 触发，不能由“发生过修复”
  单独触发。

#### L3 验收

- task reviewer 发现局部 typo，TASK reopen 后修正和 focused check 即可关闭。
- 修复认证逻辑时 security、integration 和 regression Evidence 失效，纯 UI Evidence
  若 fingerprint-compatible 则保留。
- 原 finder 不可用时，等价责任使用原 finding contract 完成独立复验。

### REQ-GATE-106 Gate Telemetry 与删除机制（P1）

#### L2 功能需求

- 记录每个 Gate 的触发数、独立发现数、误报、恢复 turns、token/cost、wall
  time、重跑范围和最终 escaped defect。
- Gate 变更必须使用 planted-defect 和 no-guidance control 的行为 Eval。
- 长期无独立发现且成本显著的 Gate 自动进入 sunset review。
- 新增 Gate 时必须声明复审日期和成功/删除标准。

#### L3 验收

- Gate 只重复前一 Gate finding，telemetry 将其标记为 zero marginal catch。
- 删除候选 Gate 的 Eval 显示漏检上升时，保留并记录证据。
- 合并两个 reviewer 后质量持平且 turns 显著下降，profile 采用合并方案。

## 8. 与已有候选 REQ 的关系

本文不是再增加六套独立建设，而是对已有分层 REQ 的门禁视角重构：

| 本文 | 对应已有候选 |
|:---|:---|
| REQ-GATE-101 | REQ-101 治理真值 |
| REQ-GATE-102 | REQ-103 风险路由 + REQ-106 自适应验证 |
| REQ-GATE-103 | REQ-107 Agent 编排 |
| REQ-GATE-104 | REQ-105 Discovery + REQ-106 Claim–Evidence |
| REQ-GATE-105 | REQ-104 契约演化 + REQ-106 自适应验证 |
| REQ-GATE-106 | REQ-102 行为 Eval |

正式化时建议将这些内容吸收到原有 REQ，而不是让“改进门禁”本身继续制造
更多文档和 Gate。

## 9. 推荐的最小实验

在修改状态机前，选择三类真实任务做 A/B 行为实验：

1. 低风险局部改动；
2. 普通跨前后端行为功能；
3. 高风险 migration / permission 改动。

对照两种路径：

- A：当前 full-depth 固定流程；
- B：四个 blocking moments + risk-derived proof obligations。

共同测量：

- 最终缺陷和 planted defect 检出率；
- Agent turns、token、wall time、重复读取和重复 Evidence 数；
- review loop 次数、误阻塞和需要人类介入次数；
- compact/resume 后的恢复正确性；
- Agent 是否更早暴露未知项、挑战错误 Plan、复用现有实现并主动删除冗余。

只有 B 在不降低高风险检出率的前提下显著减少摩擦，才应正式改变 Loop。

## 10. 第一批值得挑战的具体规则

按“高摩擦、低独立价值、易做实验”排序：

1. 删除 Delivery → QA → E2E 固定串行，改为同 snapshot 并行；
2. 允许兼容 reviewer responsibilities 合并；
3. 只读 reviewer 取消 phase-two activation；
4. 非 UI change 用 Impact Evidence 直接证明 E2E N/A；
5. ACC 改为 Evidence Graph 自动 projection；
6. task-local finding 不强制 Canonical BUG；
7. targeted repair 使用选择性 invalidation，不默认 full round restart；
8. Architecture/FE/BE/SYNC/TASK Artifact 改为 change-triggered；
9. Release Audit 模板按风险生成适用章节；
10. 用 telemetry 决定 `30 files / 3 modules` 是否仍值得保留。

其中第 1–5 项不需要先改变人类控制边界，适合作为最早的行为实验对象。
