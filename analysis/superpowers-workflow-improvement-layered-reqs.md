# Superpowers 对照分析与工作流改进分层 REQ

> 状态：discovery
> 日期：2026-07-18
> 范围：工作流与 Harness 自演进候选需求
> 说明：本文不是已锁定 REQ，不授权实现、状态迁移或发布。候选 REQ 仍需由人选择、拆分、编号和锁定。
> 历史定位：本文保留为来源分析；最终推荐以
> `analysis/final-project-development-workflow.md` 为准。

## 1. 结论摘要

Superpowers 最值得借鉴的不是它的固定流水线，而是它把 Agent 行为当成可观测、可测试、可迭代的工程对象：

- Claim 必须绑定新鲜 Evidence；
- 调试必须先形成根因假设，再做最小实验；
- Skill 不是普通说明文，而是需要压力测试的行为程序；
- 任务边界应值得一次独立审查，而不是越小越好；
- 每任务审查与整分支审查应有不同范围；
- 机械工作可以降成本，判断工作不能因便宜而降级；
- 工作区是否隔离、由谁创建、最终集成到哪里，必须显式表达；
- 负向指令、正向 recipe、review tripwire 应通过真实 Agent eval 选择，而不是凭写作者直觉。

本项目比 Superpowers 更进一步的地方，是已经把状态合法性、Revision CAS、指纹、证据失效和部分权限边界下沉到 Harness，而不只依赖 Skill 自律。这些能力应当保留。

本项目当前最需要反思的不是“流程够不够完整”，而是两个问题：

1. **声明的保障是否真的被机器执行。** 当前存在文档、Manual、CLI projection、Skill 与 Guard 实现不一致的事实。
2. **机器可靠之后，路径是否仍然过度固定。** 当前缺少任务分类、不确定性评估、Spike、共享发现、契约有效性复核、临时状态和收敛删除阶段。

因此总体方向不是把当前 Loop 变轻，也不是照搬 Superpowers，而是：

```text
可靠的治理内核
+ 风险与不确定性路由
+ 可演化的契约
+ 与风险匹配的验证
+ 行为级 Eval
```

一句话目标：

> 从 Fixed Full-Depth Workflow 演进为 Risk-Adaptive Governance，同时保证任何流程裁剪都不能静默削弱人类边界、证据真实性和状态一致性。

## 2. 对照后的取舍

### 2.1 应直接吸收的原则

| Superpowers 实践 | 本项目吸收方式 |
|:---|:---|
| Evidence before claims | 继续强化 Evidence 指纹、实际命令输出、Claim 到 Evidence 的机器映射 |
| Systematic debugging | 将“复现 → 根因 → 单一假设 → 最小实验 → 回归证据”纳入 BUG 生命周期 |
| Skill behavior evals | 建立真实 Agent 会话 Eval，与 Go/Schema 机制测试分层 |
| Task right-sizing | TASK 以“值得一次独立 Gate 的最小交付单元”为边界，不按 2–5 分钟动作拆分 |
| Global constraints + Interfaces | 将全局约束和跨 TASK 接口变为结构化、可提取字段 |
| Task-scoped review + broad final review | 局部 Gate 只审局部变更；完整 Review 只在风险和发布边界需要时运行 |
| Fresh context with curated dispatch | 保留单责任上下文，但通过 Discovery Ledger 共享跨任务知识 |
| Judgment guardrail | 模型层级按判断密度选择；Review、冲突裁决和计划失效判断不能机械降级 |
| Detect and defer | 优先识别现有 Harness/工作区状态，不与平台原生能力争夺所有权 |
| Instruction micro-tests | Skill 文案变化先做小样本行为测试，再进入端到端 Eval |

### 2.2 应保留的本项目优势

- 人类锁定 REQ、批准 REQ 变化和最终发布；
- Loop Definition 负责合法性，Runtime 负责当前事实，Evidence 负责证明；
- Runtime Revision CAS、原子快照、JSONL Journal 和 reconcile；
- 文档及 Evidence 指纹与失效传播；
- 两阶段 Agent 激活和路径/工具范围交集；
- Builder、Verifier、QA、E2E 的责任分离；
- BUG 根因调查、Canonical BUG、原发现责任 targeted re-verification；
- Clean Round 不接受跨轮次、失效 Evidence 或开放阻塞 BUG；
- 自动化在 `awaiting_human_release` 终止。

### 2.3 不应照搬的部分

- 不应让所有改动都先走同样长度的 Brainstorming 和人工批准；
- 不应在实现前冻结完整函数体、内部类名和逐步代码；
- 不应对配置、删除、Spike、性能、重构和行为开发统一使用严格 TDD；
- 不应把 Fresh Agent 等同于 Fresh Ignorance；
- 不应把“符合旧 Plan”置于“Plan 是否仍然有效”之上；
- 不应让每个 TASK 都支付同样的 Subagent + Review 固定成本；
- 不应为了成本把判断型 reviewer 换成明显较弱的模型；
- 不应在完成阶段通过 `merge-base`、当前 HEAD 或分支名猜测业务集成目标；
- 不应把流程步骤是否执行，误认为产品和架构质量已经成立。

## 3. 当前基线中的关键反思点

以下不是理论风险，而是本次对照源码和文档后确认的现状。

### 3.1 声明—执行差距

1. 根目录 `loop-harness.md` 声明的 Loop Definition SHA-256 为：
   `1b20cfa6d1f297db28b72b9eb6650d7d336425e460d89834bbf7cd4f092a1a59`。
   当前 `docs/loop-definition.json` 的实际 SHA-256 为：
   `ef5aecabd98aeafcd5c22689766535bd29ae7ecf4d1f83a4bce0d0123e3d3b3a`。
   Manual 已经漂移。

2. `docs/agent-protocol.md` 把 `status` / `next` 描述为包含
   `objective`、`completed`、`open_items`、`missing`、`done_when`、
   `protocol_ref` 等字段；当前 `internal/cli/run.go` 实际只返回基础游标、
   `primary_skill`、`action` 和 `human_required`。Driver 所依赖的
   `next.missing` 目前没有实现。

3. `skills/specification-planning/SKILL.md` 仍引用 `PTR-PLAN-01..08`，
   但当前 Loop Definition 的 planning phase 只有 `design`，且
   `transitions=[]`。Manual 仍列出旧的 PTR-PLAN 转换。

4. `internal/transition/guards.go` 已明确标注大多数 Guard 是
   `evidenceBackedGuard` stub。除少量状态派生检查和 Clean Round 检查外，
   多数语义化 Guard 名称最终只检查 Evidence map 非空。源码自身称其为
   `guard-theater pattern`。

5. 两阶段激活、Runtime integrity、scope violation 和 policy tamper
   在 Hook policy 中多数是 `warn`；Hook adapter 会返回
   `permissionDecision="allow"`，原操作已经执行。这与“激活前无副作用”
   等强不变量的文字表达并不等强。

6. `docs/project.yaml` 声明 `delivery_mode: full`，Blueprint 又提到
   lightweight mode，但当前 Runtime、Router 和 Transition 并未实际消费
   delivery mode。它还是文档选项，不是执行策略。

### 3.2 固定路径风险

- S2/S3/S4 倾向在实现前固定设计、合同、TASK 和路径；
- Specification Planning 明确要求 rework 不重新打开 settled design，
  但实现阶段新证据可能正好证明 settled design 无效；
- TASK 生命周期没有 `exploratory`、`provisional`、`integrated`、
  `deprecated`、`removed` 等表达；
- 所有 REQ 最终都以完整 Delivery + QA + E2E Clean Round 为中心，
  缺少根据变化性质证明 N/A 或使用更窄 Gate 的一等机制；
- 没有 Spike 生命周期、Discovery Ledger 或显式 Failed Approaches；
- 没有专门的 Convergence / Deletion Pass；
- Git 规则定义分支命名和目标，但没有持久化 base ref、base SHA、
  integration target 与 workspace provenance；
- Skill 只有静态结构测试，没有真实 Agent 行为 Eval，因此无法证明
  长 Skill、负向指令、重复 inlined methodology 或 routing wording
  实际产生了期望行为。

## 4. 分层需求模型

本文使用四层需求：

| 层级 | 回答的问题 | 产物 |
|:---|:---|:---|
| L0 Outcome | 最终必须成为怎样的工作流 | 宪法级结果 |
| L1 Capability | 系统必须具备哪些能力 | 候选顶层 REQ |
| L2 Functional | 每个能力需要哪些具体行为 | 可实现需求 |
| L3 Acceptance | 如何证明行为真的成立 | 自动化和 Agent Eval 场景 |

## 5. L0 Outcome REQ

### REQ-L0-001 风险自适应治理

工作流必须根据任务类型、影响风险和信息不确定性选择执行与验证路径，
而不是对所有变化强制同一条完整流水线。

约束：

- 自适应只能改变中间工程路径；
- 不能跳过人类锁定 REQ、REQ 变更批准和最终发布批准；
- 不能通过降低 Acceptance 含义来获得更短路径；
- 任何路径裁剪必须有可审计的分类、理由和 Evidence。

### REQ-L0-002 声明保障等于机器保障

任何被文档称为“禁止”“必须”“唯一写入者”或“Gate”的能力，都必须
对应明确的机器执行语义，或被诚实标注为 advisory / attested，而不能
使用强语义名称包装弱实现。

### REQ-L0-003 新证据可以推翻旧计划，但不能静默改变人类意图

实现阶段发现的新事实可以触发设计、合同、TASK、测试和执行路径变化；
只要不改变锁定 REQ 的用户价值、范围和 Acceptance，系统应支持自主
回到正确层次重规划。改变 REQ 语义时仍必须暂停并交还人类。

### REQ-L0-004 Evidence 是完成声明的前置条件

任何“完成、通过、修复、可发布”声明都必须指向针对当前指纹、当前
baseline generation 和当前 review round 的新鲜 Evidence；Agent 报告
本身不是权威状态变化。

## 6. L1/L2/L3 候选 REQ

### REQ-101 治理真值与执行完整性（P0）

目标：先消除流程剧场，使文档、投影、Guard、Hook、Manual 和 Runtime
表达同一套真实能力。

#### L2 功能需求

- REQ-101.1：每个 Gate 必须声明执行类型：
  `enforced`、`evidence-attested`、`advisory`。
- REQ-101.2：`enforced` Gate 必须有独立语义实现，不得落入只检查
  Evidence 非空的通用 stub。
- REQ-101.3：Guard Spec 中描述的每个检查必须能映射到实现函数、
  测试和失败码。
- REQ-101.4：`status` 与 `next` 必须有版本化 JSON Schema；文档示例、
  CLI 输出和 Driver 消费字段必须由同一 schema/conformance test 约束。
- REQ-101.5：`next.missing` 必须来自当前 stage `done_when` 的实际差集，
  而不是固定提示字符串。
- REQ-101.6：Manual 必须作为纯生成物；其 Definition SHA 与当前文件
  不一致时，CI 和 release 必须失败。
- REQ-101.7：Skill 不得引用 Loop Definition 中不存在的 transition、
  phase、guard、action 或 lifecycle state。
- REQ-101.8：激活前写入、激活范围越界、Runtime integrity failure、
  未授权 policy 修改必须重新审定严重度。若它们仍被定义为不变量，
  Hook 必须 deny；若保留 warn，文档必须改称 observation。
- REQ-101.9：拒绝的 transition 必须有可审计 rejected event；文档不得
  声称存在但实现未记录。

#### L3 验收场景

- 修改 Loop Definition 后不重新生成 Manual，`validate --all` 失败并报告
  `MANUAL_DEFINITION_DRIFT`。
- 删除一个 `next` schema 必填字段，CLI conformance test 失败。
- Skill 引用不存在的 `PTR-PLAN-03`，drift check 精确报告文件和行号。
- 创建一个仅使用通用 stub 的 `enforced` Guard，doctor/CI 拒绝注册。
- 未激活 Agent 写入 activated scope 外路径，Hook 返回 deny，文件不变。
- Runtime 指纹失配时执行副作用工具，工具不执行并提供 reconcile 路径。

### REQ-102 Skill 行为 Eval 与流程度量（P0）

目标：把 Skill、Agent prompt、routing 和 recovery wording 当作行为代码，
建立真实 Agent 会话层的回归测试。

#### L2 功能需求

- REQ-102.1：测试体系分为两层：
  - mechanism tests：Go、Schema、Hook payload、CLI、脚本；
  - behavior evals：真实 Agent 是否按预期分类、路由、停下、升级和验证。
- REQ-102.2：核心行为至少覆盖：Skill 触发、风险分类、Spike 路由、
  激活前副作用、错误 Plan 挑战、Evidence-before-claim、BUG 根因流程、
  Human Gateway、Convergence pass。
- REQ-102.3：每个 Skill 行为变化必须提供 before/after 场景；关键行为
  至少 N=5 运行，记录方差，不能用单次通过证明稳定性。
- REQ-102.4：指令微调必须包含 no-guidance control；自动评分结果必须
  人工检查命中样本，防止否定词和引用文本误判。
- REQ-102.5：记录 outcome、违规类型、工具调用、turn 数、token/cost、
  人类中断次数、review loop 次数和误报/漏报。
- REQ-102.6：Eval 必须包含 adversarial pressure：简单任务借口、赶时间、
  用户要求跳过、计划与质量冲突、上下文 compact、错误 reviewer 建议。
- REQ-102.7：没有行为 Evidence 的 Skill 大规模重写不得进入 release。

#### L3 验收场景

- 给 Agent 一个“只改一行配置”的任务，验证 Router 选择 targeted profile，
  同时仍要求相应配置验证，不强制生成完整架构文档。
- 给 Agent 一个 Plan 明确要求 assertion-free test 的任务，reviewer 必须
  把它识别为 plan-mandated defect，并升级给具备判断权的控制者/人类。
- 对同一 prompt 的正向 recipe、负向 prohibition 和无 guidance 版本各跑
  N=5，只有行为指标更优且无质量回退的版本可采用。
- 模拟 compact 后恢复，Agent 不得重复创建已完成 TASK/assignment/evidence。

### REQ-103 风险与不确定性路由（P1）

目标：让 `delivery_mode` 从静态字段升级为由机器消费、可审计、可升级的
执行 Profile。

#### L2 功能需求

- REQ-103.1：每个 Bound REQ 或 change batch 必须先分类：
  `small_change`、`behavior_feature`、`bugfix`、`refactor`、`exploration`、
  `third_party_integration`、`performance`、`migration`、`large_cross_module`。
- REQ-103.2：不确定性必须至少评估：需求、架构、依赖行为、数据、运行
  环境、可观测性、用户/业务决策。
- REQ-103.3：风险必须至少评估：安全、权限、数据损失、兼容性、迁移、
  并发、可靠性、性能、跨组件、UI/E2E、回滚难度。
- REQ-103.4：Router 输出一个 Runtime 持久化 profile：
  `exploratory`、`targeted`、`standard`、`full_depth`，包含依据和触发器。
- REQ-103.5：风险或不确定性增加时自动升级 profile；降级必须有正面
  Evidence，且不得让已经触发的 Human Gateway 失效。
- REQ-103.6：Profile 只决定所需设计深度、任务拆分、Agent/Review 规模
  和 Evidence 维度，不改变锁定 REQ 与发布边界。
- REQ-103.7：无法分类时默认选择更保守 profile，但必须记录未知项，
  不能永久以“保守”为由制造不必要的全流程成本。

#### 建议 Profile

| Profile | 适用 | 最小路径 |
|:---|:---|:---|
| exploratory | 高不确定、第三方未知、架构探索 | Spike → Discovery → Contract checkpoint |
| targeted | 小改动、文档/配置、低风险局部变化 | bounded change → targeted validation |
| standard | 明确行为功能、普通 bug/refactor | stable contract → implementation → selected review |
| full_depth | 高风险、跨模块、迁移、安全、发布关键 | 当前完整 Loop + 全量适用 Gate |

#### L3 验收场景

- 文案修正且无 normative/executable change 时，Router 选择 targeted，
  产生 documentation-only impact Evidence，不启动 E2E Team。
- 未知第三方 API 行为时，Router 选择 exploratory；正式合同锁定被阻止，
  直到 Discovery Evidence 完成。
- 数据迁移 + 回滚困难时强制 full_depth，不能由 Agent 自主降级。
- 实现中发现跨模块影响后，profile 从 standard 升级 full_depth，并使
  受影响 Evidence 失效。

### REQ-104 稳定契约、实现自由与契约演化（P1）

目标：固定用户可观察行为和架构不变量，但不在信息不足时冻结内部实现。

#### L2 功能需求

- REQ-104.1：Contract/TASK 必须区分四类字段：
  - stable behavior：外部行为、业务不变量、兼容性、Acceptance；
  - architecture constraints：必须遵守的依赖方向、事务、安全等约束；
  - provisional decisions：待实现验证的假设；
  - implementation freedom：类、函数、文件和内部抽象的可选空间。
- REQ-104.2：内部函数签名和文件结构只有在跨 TASK 接口或稳定公共契约
  时才可锁定。
- REQ-104.3：TASK 必须包含 `known_uncertainty`、`discovery_required`、
  `change_triggers`、`cleanup_required`。
- REQ-104.4：新证据证明 settled design 无效时，允许返回 Design Validity
  Review；不能只允许“修正被标记条款而不重开设计”。
- REQ-104.5：Contract Change Proposal 必须包含 original contract、
  new discovery、insufficiency、proposed contract、impact 和 cleanup。
- REQ-104.6：不改变 REQ 语义的设计/合同变更可由 Architect 路由回
  planning 并执行 impact invalidation；改变 REQ 语义必须暂停交还人类。
- REQ-104.7：Spec reviewer 必须先判断 Spec/Plan 是否仍有效，再判断实现
  是否合规。Plan-mandated defect 不因“符合计划”而降级。

#### L3 验收场景

- TASK 计划新增 Service，但实现发现已有 helper 完整满足稳定行为；
  Builder 可保留现有架构并记录 deviation，不因缺少新 Service 被判 FAIL。
- 第三方响应无法表达原合同状态时，系统生成 Contract Change Proposal，
  旧测试和 activation 失效，不能增加隐藏 workaround 后继续。
- Reviewer 发现 Plan 与安全规则冲突时输出 `spec_invalid`，不输出
  `spec_compliant` 后继续。

### REQ-105 Spike 与共享 Discovery Memory（P1）

目标：实现 Fresh Reasoning + Shared Discovery，避免 Fresh Agent 变成
Fresh Ignorance。

#### L2 功能需求

- REQ-105.1：引入 `exploratory` / `provisional` 工作状态；它们不等于
  Builder completion，也不能进入 Acceptance。
- REQ-105.2：Spike 必须 time-boxed、scope-bounded，并默认禁止将实验代码
  直接作为生产实现提交。
- REQ-105.3：Spike 输出必须是 Discovery Evidence：真实约束、失败模式、
  接口样例、性能数据、测试 fixture、风险、已否定方案和建议 checkpoint。
- REQ-105.4：维护 fingerprinted Discovery Ledger，至少记录：
  architecture decisions、known pitfalls、existing helpers、changed contracts、
  failed approaches、cross-task discoveries。
- REQ-105.5：每次 Agent readback 必须读取与 assignment scope 相关的 Ledger
  条目；Harness 记录其指纹，变化后 activation 失效或要求重新确认。
- REQ-105.6：Discovery 必须有适用范围、来源、置信度和失效条件，不能把
  临时观察永久升级为全局规则。
- REQ-105.7：Spike 结束后必须执行 Contract checkpoint：采纳、继续探索、
  否定方案或升级为 Human Gateway。

#### L3 验收场景

- Agent A 发现 ORM 错误包装约定并写入 Ledger；Agent B 的 readback 自动
  包含该条目，不能再创建同义 error helper。
- Spike 产生的临时代码若被正式 TASK 引用，Gate 失败并要求显式 promote
  或删除。
- Ledger 条目被新版本依赖推翻后，引用它的 activation 和 provisional
  contract 被标记 stale。

### REQ-106 自适应测试、Review 与 Claim–Evidence（P1）

目标：验证强度由变化类型和风险决定，Evidence 质量不因路径变短而下降。

#### L2 功能需求

- REQ-106.1：验证策略按 change type 路由：
  - behavior feature → acceptance-first / TDD；
  - bugfix → reproduce + regression-test-first + root cause；
  - refactor → characterization + structural review；
  - exploration → Spike validation，不要求生产 TDD；
  - configuration → targeted validation；
  - deletion → dependency analysis + regression；
  - performance → benchmark-first；
  - migration → real schema/data rehearsal + rollback evidence。
- REQ-106.2：测试优先稳定可观察边界和业务不变量，不默认冻结内部调用
  次数、helper 或 private method。
- REQ-106.3：每个 Claim 必须声明证明命令/证据、执行时间、指纹和结果；
  `should pass`、历史运行和 Agent 自报不能作为通过证据。
- REQ-106.4：Task review 必须 diff-first、task-scoped；只有可命名的跨切面
  风险才能扩大检索范围。
- REQ-106.5：同一代码未变化且已有有效实现者测试 Evidence 时，reviewer
  不得机械重跑完整测试；若阅读产生具体疑问，可运行 focused test。
- REQ-106.6：完整 branch/release review 保留广度，与 task review 使用
  不同 scope contract。
- REQ-106.7：适用 Verification responsibility 由 profile + risk matrix
  计算；N/A 必须有正面 Evidence，不能以 silence 代替。
- REQ-106.8：Clean Round 应表示“所有适用风险维度在同一轮通过”，而不是
  “无论变化类型都机械启动相同团队”。full_depth 仍要求完整 DV/QA/E2E。

#### L3 验收场景

- 纯 refactor 没有新增行为时，Gate 要求 characterization 和 regression，
  不要求为每个新 private helper 演示 RED。
- 性能优化没有 before/after benchmark 时不得宣称完成。
- Task reviewer 扩大到全仓库搜索时必须在报告中给出 named risk 和检查对象。
- 实现者已对当前 SHA 运行 full suite；reviewer 未发现新疑问时不重复运行。
- 非 UI 低风险 change 的 E2E N/A 必须引用“无用户流/浏览器边界变化”的
  impact evidence，而不是空白。

### REQ-107 Agent 编排、任务粒度与判断权（P1）

目标：让 Subagent 隔离服务于聚焦，而不是制造重复探索、重复 Gate 和判断降级。

#### L2 功能需求

- REQ-107.1：TASK 是“拥有独立测试循环并值得 reviewer 单独拒绝”的最小
  单元；setup/config/docs 应并入需要它们的交付 TASK。
- REQ-107.2：TASK 必须结构化携带 Global Constraints 和跨 TASK Interfaces，
  Harness 生成 task brief，不依赖 Orchestrator 每次重新转述。
- REQ-107.3：Fresh Agent 必须获得最小直接规格 + 相关 Discovery Ledger，
  不读取整个计划，也不继承无关聊天历史。
- REQ-107.4：并行依据 independent failure domain、无共享写入和无顺序
  dependency，而不是 TASK 数量。
- REQ-107.5：模型选择按 judgment density：机械实现可用低成本模型；
  架构、review severity、BLOCKED 诊断、Plan invalidity、Human Gateway
  识别必须由高判断能力模型或人类处理。
- REQ-107.6：弱模型遇到 `BLOCKED`、`cannot verify`、plan conflict、
  suspected false positive 时必须上行，不得自行吸收判断。
- REQ-107.7：Progress Ledger 必须持久化 task/assignment/review 状态，
  compact/resume 后不得重复 dispatch。
- REQ-107.8：两阶段激活继续作为副作用边界，但 readback 长度和字段可按
  profile 调整；任何 profile 都不能跳过实际 scope 授权。

#### L3 验收场景

- 一个仅创建 `.gitignore` 的步骤不能独立生成 TASK，除非它有独立可拒绝
  的安全/发布结果。
- compact 后恢复，Router 从 Progress Ledger 得知 TASK 已通过 review，
  不重新派发 Implementer。
- Reviewer 发现 plan-mandated defect 时，低层 controller 必须升级，不能
  把 reviewer finding 解释成计划合规。
- 两个 assignment 写同一文件时 conflict graph 强制串行或重新分区。

### REQ-108 Convergence、Deletion 与复杂度预算（P2）

目标：让完成意味着系统已经收敛，而不只是所有增量步骤都做过。

#### L2 功能需求

- REQ-108.1：大型、探索性或跨模块改动在最终完整验证前必须执行
  Convergence / Deletion Pass。
- REQ-108.2：检查并处理：Spike 遗留、重复 helper、过时 adapter、临时
  compatibility layer、无用抽象、过度 mock、dead/debug code、失效测试、
  重复文档和 provisional decision。
- REQ-108.3：TASK/Artifact 生命周期支持 `provisional`、`integrated`、
  `deprecated`、`removed`，并记录替代关系和删除 Evidence。
- REQ-108.4：新增复杂度必须能映射到稳定行为、风险缓解或架构约束；
  无映射的复杂度默认删除。
- REQ-108.5：删除操作必须运行依赖分析和影响分析，不能因“清理”绕过
  Evidence invalidation。
- REQ-108.6：Convergence reviewer 必须能挑战新增抽象，即使它们完全符合
  原 Plan。

#### L3 验收场景

- Spike 产生两个同义 helper；Convergence Gate 在 final review 前要求合并
  或给出两个稳定责任的证据。
- 新增 compatibility layer 没有真实 caller，YAGNI 检查要求删除。
- 删除过时 adapter 后，影响分析重新计算相关测试和 Evidence。

### REQ-109 Workspace 与 Branch Lineage Contract（P2）

目标：让隔离、所有权和集成目标成为持久化事实，而不是结束阶段的推断。

#### L2 功能需求

- REQ-109.1：创建 workspace 前先检测现有 worktree、submodule、detached
  HEAD 和平台原生隔离；优先 defer 给 Harness 原生能力。
- REQ-109.2：手工创建 worktree 时必须显式指定 start point，并解析为
  immutable base SHA；创建后验证实际 HEAD。
- REQ-109.3：持久化 Branch Lineage Manifest：`branch`、`base_ref`、
  `base_sha`、`integration_target`、`worktree_path`、`owner/provenance`、
  `runtime_id`、`status`。
- REQ-109.4：完成阶段必须读取 Manifest，不得通过 current HEAD、cwd、
  `merge-base`、branch naming 猜测 integration target。
- REQ-109.5：只允许创建方或平台所有者清理 workspace；未知 provenance
  默认保留并交还人类。
- REQ-109.6：基线测试失败时记录 pre-existing Evidence，未经人类决定
  不进入会混淆归因的正式实现。

#### L3 验收场景

- 当前 workspace 在 `main`，业务 parent 是 `develop`；新 branch 必须从
  Manifest 指定的 develop SHA 创建，而不是当前 HEAD。
- Base ref 后续前移，Manifest 同时保留创建时 base SHA 和业务目标 ref。
- Harness-owned detached workspace 在 finish 时不得被项目脚本删除。

## 7. 依赖关系与建议拆分

```text
REQ-101 治理真值 ───────┐
                         ├─> REQ-103 风险路由
REQ-102 行为 Eval ──────┘          │
                                   ├─> REQ-104 契约演化
                                   ├─> REQ-105 Discovery
                                   ├─> REQ-106 自适应验证
                                   └─> REQ-107 Agent 编排

REQ-104 + REQ-105 + REQ-106 ─────> REQ-108 Convergence
REQ-101 + Git/Runtime ownership ──> REQ-109 Branch Lineage
```

建议正式拆成七个可锁定 REQ，而不是一个超大 REQ：

| 顺序 | 正式 REQ 候选 | 包含 | 原因 |
|:---|:---|:---|:---|
| 1 | Runtime Truth Contract | REQ-101 | 不先修真值差距，后续路由不可可信 |
| 2 | Skill Behavior Eval Harness | REQ-102 | 为后续每项行为变化建立验证手段 |
| 3 | Risk/Uncertainty Router | REQ-103 | 自适应工作流的决策内核 |
| 4 | Evolvable Contract + Discovery | REQ-104、105 | Spike、Ledger、Change Proposal 必须一起设计 |
| 5 | Adaptive Verification + Judgment | REQ-106、107 | 验证矩阵、review scope、模型判断权相互依赖 |
| 6 | Convergence and Deletion | REQ-108 | 建立收敛阶段，避免增量技术债 |
| 7 | Workspace Lineage | REQ-109 | 可独立交付的 Git/Workspace 能力 |

## 8. 推荐优先级

### P0：先让系统诚实且可测

1. REQ-101 治理真值与执行完整性；
2. REQ-102 Skill 行为 Eval 与流程度量。

这两项不直接让流程更灵活，但决定后续任何“自适应”是否可信。

### P1：建立自适应主干

1. REQ-103 风险与不确定性路由；
2. REQ-104 稳定契约与契约演化；
3. REQ-105 Spike 与 Discovery Ledger；
4. REQ-106 自适应验证；
5. REQ-107 Agent 编排与判断权。

### P2：优化系统长期复杂度与集成

1. REQ-108 Convergence / Deletion；
2. REQ-109 Workspace / Branch Lineage。

## 9. 第一阶段不应做什么

- 不应立即修改 S0–S11 全状态机；
- 不应先添加更多 Skill 和更多 Gate；
- 不应先实现四种 delivery profile，再补 Eval；
- 不应以 lightweight 为名直接跳过现有验证；
- 不应把所有 warn 一次性升级 block，而不区分真正不变量与恢复提示；
- 不应在 Guard stub 未治理前继续添加更多语义化 Guard 名称；
- 不应把本分析直接标记 locked 或让 Loop 自动执行；
- 不应以 Superpowers 的流程作为新的权威来源，它只能是比较材料。

## 10. 建议的下一步

下一步应先为 `Runtime Truth Contract` 单独起草正式 REQ，范围只包括：

1. Manual SHA 漂移；
2. `status/next` projection contract；
3. Planning Skill 与 Loop Definition 漂移；
4. Guard semantic typing 和 stub 清理边界；
5. Hook warn/block 与不变量对齐；
6. 对应 conformance tests。

该 REQ 完成后，再以真实 Agent Behavior Eval 为第二个正式 REQ。风险路由
和自适应验证应建立在这两个基础上，而不是与基础治理修复混在同一批次。

## 11. 证据索引与判断边界

本文刻意区分三类内容：

1. **已确认事实**：可由当前仓库文件或实现直接复核；
2. **外部经验**：来自 Superpowers 的实践与复盘，证明某种问题或方向值得
   研究，但不自动成为本项目要求；
3. **候选推演**：REQ-101..109，是基于前两类材料形成的设计假设，必须在
   正式锁定前继续做成本、兼容性和行为 Eval。

### 11.1 Superpowers 侧主要阅读材料

| 材料 | 用于支持的判断 |
|:---|:---|
| `superpowers/superpowers-retrospective-and-reflections.md` | 成本、review loop、模型判断能力、正向指令和行为 Eval 的复盘依据 |
| `superpowers/skills/using-superpowers/SKILL.md` | Mandatory Skill invocation 的行为约束方式 |
| `superpowers/skills/brainstorming/SKILL.md`、`writing-plans/SKILL.md` | 设计批准与细粒度计划的价值和冻结风险 |
| `superpowers/skills/subagent-driven-development/SKILL.md`、`executing-plans/SKILL.md` | Fresh Agent、任务边界、review 结构与编排成本 |
| `superpowers/skills/test-driven-development/SKILL.md` | 严格 TDD 原则及其适用性边界 |
| `superpowers/skills/systematic-debugging/SKILL.md` | 根因假设、最小实验和回归链路 |
| `superpowers/skills/verification-before-completion/SKILL.md` | Claim–Evidence 原则 |
| `superpowers/skills/using-git-worktrees/SKILL.md`、`finishing-a-development-branch/SKILL.md` | workspace 隔离、基线和完成阶段推断风险 |
| `superpowers/docs/superpowers/specs/2026-06-10-strict-cost-sdd-design.md` | 机械工作与判断工作的模型分层 |
| `superpowers/docs/superpowers/specs/2026-06-09-sdd-task-scoped-review-dispatch-design.md` | task-scoped review 与双 verdict |
| `superpowers/docs/superpowers/specs/2026-06-10-positive-instruction-redesign-design.md` | 正向 recipe 相对负向禁令的实验思路 |
| `superpowers/docs/superpowers/specs/2026-05-06-lift-drill-into-evals-design.md` | Skill 行为测试从 drill 演进到 eval |
| `superpowers/docs/superpowers/specs/2026-04-06-worktree-rototill-design.md` | worktree detect/defer、provenance 和行为实验 |

### 11.2 本项目侧主要阅读材料

| 材料 | 用于支持的判断 |
|:---|:---|
| `docs/loop-definition.json`、`loop-harness.md` | 权威状态机与生成 Manual 的漂移事实 |
| `docs/agent-protocol.md`、`internal/cli/run.go` | `status/next` 声明与真实 projection 的差距 |
| `skills/loop-orchestration/SKILL.md`、`skills/specification-planning/SKILL.md` | Driver 消费约定和 Planning transition 漂移 |
| `internal/transition/guards.go`、`guard_specs.go`、`engine.go` | Guard 注册、语义声明、Evidence 校验与 stub 边界 |
| `docs/hook-policy.json`、`internal/policy/engine.go`、`internal/hook/adapter.go` | warn/block 强度和副作用边界的真实语义 |
| `internal/runtime/store.go`、`stage.go` | CAS、Journal、Runtime 状态与 Stage projection |
| `internal/impact/analysis.go`、`internal/verification/clean_round.go` | 指纹失效传播和 Clean Round 的现有基础 |
| `internal/assignment/*.go` | Agent、TASK、BUG 当前生命周期和缺失状态 |
| `skills/two-phase-activation/SKILL.md`、`team-planning/SKILL.md` | 激活、scope、团队责任和 readback 模型 |
| `skills/bug-resolution/SKILL.md`、`testing-strategy/SKILL.md` | BUG 根因、targeted re-verification 和验证策略 |
| `docs/project.yaml`、`docs/vibe-coding-blueprint.md` | `delivery_mode` 声明和 fixed full-depth 基线 |

### 11.3 尚待验证的核心假设

- targeted profile 是否真的减少成本，同时不增加漏检；
- Discovery Ledger 是否降低重复探索，还是制造新的阅读和失效负担；
- task-scoped review 在本项目的责任分离下是否仍优于固定全量 review；
- `enforced / evidence-attested / advisory` 三分法是否足够表达所有 Gate；
- 风险分类能否达到可重复的一致性，以及哪些分类必须由人类裁决；
- Convergence Gate 的收益是否足以覆盖新增的 review 成本。

这些假设应由 REQ-102 的行为 Eval 证明；在此之前，不应把它们写成
“Superpowers 已经证明适用于本项目”的结论。
