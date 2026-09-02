# L3-S7 — 完整验证轮（Verification Round）

> 层：第三层 ｜ 上游：L2 §S7、S6 已集成的实现与 Builder Result ｜ 下游：S8 缺陷调查、S10 验收或 paused
>
> Macro-stage：S7 Discovery → S8 Diagnosis → S9 Remediation。S7 记录可复现表象以及真正“撞墙”的操作现场，S8 在无需默认重新复现症状的前提下推导根因与 RepairContract，S9 执行完整根因修复；三者共同审查和修复开发结果，但任何一层不得越过自己的判断权限。
>
> 横切机制：[L4 Agent 调度与治理](./L4-agent-dispatch-governance.md)。L4 负责 Assignment、PLAN_REPORT、连续执行、Hook、idle/stop、恢复和拓扑选择；本文只定义 S7 特有的验证规划、Claims、波次、ReviewResult、Finding、ObservationBatch 与 clean-round 收口。
>
> 设计状态：本文件同时作为当前机制契约和目标设计；§13 区分已落地能力、兼容投影和真实差距，历史审计段落保留当时快照。未在“当前事实”中明确标为已落地的内容，不得当作现有代码能力。

> **Revision 口径**：Runtime `revision` 是 Writer 内部提交序号；ReviewPlan、Assignment 和 verification artifact 的版本由各自工具生成。S7 只提交 Claim、Result、Finding、digest 和 `next` 等业务事实，不要求 Reviewer/Main Agent 读取、计算或复制 Runtime revision。目标命令边界统一见 [L4 Runtime revision 使用与命令协调](L4-revision-usage.md)；§13 的旧 CAS 文案仅为历史实现记录。

## 0. 阅读方式与一句话结论

S7 不应继续被实现成“Delivery、QA、E2E 三段人工串行，再由 Agent 手工拼聚合 PASS”的流程。目标形态是：

> **冻结一个基线，生成一份 ReviewPlan，将适用验证 Claims 编成 Assignment DAG；DV、QA、E2E 都按真实验证面派出 1..N 个互补 Reviewer，不设 Agent 数量或 token 预算上限。静态 Claims 先完成，行为 Claims 再按真实依赖执行；普通 Finding 只关闭 clean 路径，不关闭安全的发现路径。每个 Assignment 只提交一份 Canonical ReviewResult；所有 required Claims 都有明确 disposition 后，无矛盾则机器生成 CleanRound，有矛盾则把不可变 Findings 封装成 ObservationBatch 交给 S8。**

建议阅读顺序：

1. §1～§2：理解 S7 的价值核心和需要删除的复杂度；
2. §3～§5：理解 ReviewPlan、Claims、Assignment、ReviewResult 与两波流程；
3. §6～§10：理解三类审查、Sub-agent 派发、工具必经路径与 clean round；
4. §13～§15：维护者按迁移顺序实施并验收。

## 1. S7 的立意、边界与不变量

### 1.1 为什么需要 S7

S6 交付的是 Builder 对实现和检查结果的声明，不是独立结论。S7 必须让与 Builder 分离的 Reviewer 在同一冻结基线上回答三个不能互相替代的问题：

| Lens | 要回答的问题 | 典型事实 |
|:--|:--|:--|
| Delivery Verification（DV，schema 中仍为 `delivery`） | 做的是不是当前锁定规格要求的东西，交付边界和跨模块接缝是否完整 | REQ/设计/契约/TASK 是否兑现，模块、接缝、数据流和回归是否遗漏 |
| QA | 实现本身是否逻辑自洽、边界清晰、符合项目惯用模式并易于维护 | 架构/设计模式适配、SOLID/依赖方向、抽象与复用、可读性、复杂度、错误/状态/并发逻辑、测试、安全、性能、可靠性和迁移质量 |
| E2E | 用户或外部参与者是否能从声明入口得到预期行为 | 真实交互、console/network、持久化、副作用、拒绝与恢复路径 |

S7 的价值不是三份报告，而是一份可复算事实：**所有适用 Claims 在同一基线上有独立结论，结论仍有效，没有未路由的阻断 finding，也没有用局部重验冒充完整轮。** 对黑盒 Agent 生产的代码，DV/QA 还是 E2E 前最重要的白盒治理抓手：能从代码、契约和测试中看出的设计债、边界混乱和逻辑风险，应尽量在这里一次发现，而不是等用户旅程撞墙后再回头治理。

### 1.2 阶段目标与完成定义

| 项目 | 目标定义 |
|:--|:--|
| 输入 | S6 已集成实现；Canonical Builder Result；当前 generation 的 REQ/design/contracts/TASK；change impact；风险标签；CASE/PATH；可运行环境 |
| 核心工作 | 冻结 baseline → 生成 ReviewPlan/required Claims → 编排不设数量上限的 DV/QA/E2E Assignments → 先静态、后行为地完成有真实依赖的 Claims → 发现时冻结 encounter 现场并继续安全发现 → 受控求差确认无新增 Claim → clean 或 ObservationBatch 路由 |
| 最小工程权威 | 一份 ReviewPlan、若干 Assignment、每个 Assignment 一份 ReviewResult；通过时一份 CleanRound，失败时一组 immutable Findings + 一份 ObservationBatch |
| 目标完成 | 所有 required Claims 均有被消费的 pass；所有 not_applicable disposition 均在计划中有来源和理由；baseline 未漂移；无阻断 finding/BUG；CleanRound 原子生成并进入 S10 |
| 失败出口 | 实现/质量/行为 finding → S8；REQ 变化或 release blocker → paused；基线漂移或轮不完整 → 新完整轮 |

### 1.3 S7 负责与不负责

S7 负责：

- 定义本轮要证明的 Claims、适用性和证据 oracle；
- 独立执行 Delivery、QA 和适用 E2E；
- 将 DV/QA 各自按真实审查侧重点拆成 1..N 个 focus Assignments，完成静态质量发现面，而不是固定各派一个通用 Reviewer；
- 将 E2E 按 persona、入口、flow、状态/副作用、负向/恢复路径和环境边界拆成 1..N 个 Assignments；首次覆盖为空时执行 E2E cold start，而不是把设计、写 spec、执行和取证压给一个通用 Reviewer；
- 对 Reviewer Result 做结构、身份、scope、fingerprint 和覆盖消费；
- 将可复现表象、简短操作动线和逐步骤现场证据保存为 immutable Finding，使 S8 无需默认重新复现症状即可开始因果调查；
- 在 required Claims 都有 disposition、且受控求差不再产生新 Claim 后，以 ObservationBatch 原子传给 S8；
- 从当前事实计算 CleanRound。

S7 不负责：

- 修改被审产品实现或 locked 规格；
- 替 Builder 补普通单元/集成测试；
- 在发现 finding 后直接修复或关闭 BUG；
- 让 Reviewer 自己推进 Runtime、伪造 requested event 或手写聚合 PASS；
- 做验收和发布决定。

### 1.4 不可退让的不变量

1. **冻结产品基线，分离验证产物**：一轮所有 Result 必须绑定相同 generation、产品代码/配置/S6 测试和 authoritative subject fingerprints；这些变化使本轮 stale。S7 cold-start 新建的 E2E spec/fixture/evidence 使用独立、由工具生成的 verification-artifact revision，允许增长，但每份 Result 必须绑定自己实际使用的 artifact digest。
2. **角色独立**：Reviewer 不能验证自己在 S6/S9 写入的产品结果；原 finding 的 targeted re-verification 由原责任方承担，但它不能生成 clean round。
3. **职责无沉默**：每个适用 Claim 必须有明确结论；`not_applicable` 是有来源、有理由的决定，不是缺报告的别名。
4. **结论不聚合造假**：Lens/round 状态只由被消费的 ReviewResult 计算，不能由 Orchestrator 另写一条总 PASS 覆盖局部缺失。
5. **先保全现场，再扩展观察**：Finding 不是一句症状和一份复现脚本。S7 必须先保存最后正常点、撞墙动作、首个异常点、终态、状态差异和可关联 raw evidence；否则不得把普通 Finding 标为 investigation-ready。
6. **发现即判定 cannot-clean，不等于停止发现**：首个阻断 Finding 确认后停止所有修复行为，但普通且可安全观察的 Finding 仍须完成当前 frozen baseline 的 required Claims。只有 Result/Finding 明确暴露新的、可定位的 affected surface 时才允许一次受控 ReviewPlan revision；不以“继续找更多可能性”开放递归范围。只有 P0/安全/数据破坏类立即 stop-the-line，并保存已经产生的只读现场证据与 capture gaps。
7. **S8 不默认重现症状**：S7→S8 的 Definition of Ready 是“可直接形成初始因果模型和竞争假设”，不是“把复现劳动转交给 Investigator”。S8 后续 observation 只为区分假设，而不是重新证明 Finding 存在。
8. **白盒问题优先，但不能替代行为发现**：预先规划的 required DV/QA static Claims 必须先得到完整 disposition，再按真实依赖启动行为 E2E。普通 static Finding 会关闭 clean path，但不会取消仍可安全执行的 E2E Claims；S7 仍要把真实旅程、负向路径和副作用观察做完，避免 S8/S9 只收到代码问题而漏掉同一根因的运行表象。
9. **模式适配不是模式配额**：QA 必须判断实现是否使用了项目惯用技巧或合适设计模式，也必须识别模式缺失和模式滥用；不能用“出现 Factory/Strategy/SOLID 名词数量”代替上下文判断。
10. **覆盖优先于 token 节省**：S7 不设置 Reviewer 数量上限、固定 WIP 或 token budget gate。只要存在独立且必要的验证面，就创建足够的 Assignment；平台并发不足只影响排队，不得删除、合并过载 scope 或降低 required coverage。优化目标是去掉重复阅读、空报告和无消费机制，不是省掉专业判断。
11. **机器做确定性汇总**：Agent 负责专业判断和标注 failure boundary；Harness 负责采集时间线、证据绑定、集合、指纹、身份、状态、覆盖和路由，不把业务判断伪装成规则分数。

## 2. 复杂度—收益审计与机制取舍

### 2.1 保留、改变和删除

| 当前机制 | 收益 | 成本/风险 | 目标决策 |
|:--|:--|:--|:--|
| Delivery、QA、E2E 三种 Lens | 防止 Builder 自证和单视角漏检 | 需要独立角色与证据 | **保留** |
| 同轮、冻结指纹、有效性 | 防止混用历史 PASS | 需要可靠控制面 | **保留并强化** |
| 首个 finding 立即杀死整轮并去 S8 | 减少错误基线上的继续验证 | 可能只带走第一个表象，让 S8 重做发现 | **改为 cannot-clean + coverage-complete discovery + ObservationBatch** |
| 自由文本 `preconditions/inputs/steps` | 写法简单 | 常变成“如何重做”的脚本，缺少当次实际动作、首次偏差和状态差异 | **改为 Finding 内嵌 discriminated `encounter`；人只写短摘要和边界，Harness 自动采集 timeline** |
| 每个 finding 另建 Failure Episode 报告/状态机 | 可以承载完整现场 | 新增长期权威、ID、同步、恢复和 gate 成本 | **不新增顶层控制面；复杂 raw trace 作为 typed evidence，Finding 只保存索引与摘要** |
| S8 默认再次复现 bug | 可重新观察最新现场 | 重复 S7 成本、污染现场、遇到间歇问题时可能永远无法开始调查 | **删除默认要求；仅在判别竞争假设时请求受控 follow-up observation** |
| targeted reverify 后再开 full round | 防止局部修复破坏其他面 | 会增加一次完整轮 | **保留** |
| Delivery → QA → E2E 全串行 | 可避免早期已失败时继续昂贵 E2E | 增加排队和 lead time；同轮一致性实际由冻结基线保证 | **改为 ReviewPlan DAG + 两波调度** |
| 每个 responsibility 一个 Assignment/Agent | 表面上责任清晰 | 微任务、回执、报告和回收数量膨胀 | **改为一个 Assignment 覆盖同上下文多个 Claims** |
| DV 固定一个 Reviewer、QA 固定一个 Reviewer | 调度简单 | 单个上下文容易只看主路径；架构、逻辑、测试和维护性互相挤压，黑盒代码缺陷逃到 E2E/后续维护 | **每个 Lens 按 focus clusters 生成 1..N Assignments；N 由风险/边界/上下文决定** |
| E2E 固定一个 Reviewer | 表面上入口单一、容易管理 | 首次覆盖为空时，一个 Agent 同时理解所有旅程、设计 oracle、写 spec、准备数据、执行与取证，极易上下文过载并漏掉负向/恢复/副作用 | **E2E 也按 coverage matrix 生成 1..N Assignments；cold start 默认拆分，不把全需求压给单 Agent** |
| 固定 WIP=2、Agent 上限或 token budget gate | 成本可预测 | 为节省当前轮 token 主动制造漏检，缺陷会在 S8/S9/后续迭代成倍返工 | **删除质量层资源上限；只保留真实依赖、resource lock、平台并发容量和安全门，排队不裁 coverage** |
| 每个 checklist 点派一个 Reviewer | 看似覆盖充分 | 重复读仓库、结论重叠、Agent 数量和汇总成本爆炸 | **Claims 细，Assignment 按共享 read set/方法聚类；禁止 generic full-review 重复派发** |
| 强制使用或计数设计模式 | 可提醒结构化设计 | 容易模式崇拜和过度工程，简单逻辑被抽象成迷宫 | **QA 做 pattern-fit judgment：缺模式、错模式和过度模式都可形成 Finding，但必须给 context/risk evidence** |
| angle declaration、继承、至少三个、逐条 disposition | 强迫 Reviewer 提前思考具体检查点 | 与 responsibility/risk/check 重叠；维护第二套分类；当前链路不闭合 | **删除独立生命周期，意图并入 Claim** |
| readback → approval → activation | 降低误解风险 | 正常任务多一次同步等待；Agent 易在第一轮结束 | **按 L4 改为 plan_checkpoint 后连续执行** |
| 逐职责 evidence + Markdown + phase aggregate envelope | 兼顾人读和路由 | 同一事实多写、词汇漂移、aggregate 假绿 | **只提交 Canonical ReviewResult，视图自动生成** |
| Agent 调用 clean-round Skill 再写 wrapper | 解释方法 | 纯函数被包装为人工步骤 | **Evaluator 机器执行并自动持久化** |
| PTR clean 后 TR 再重复 clean guards/record | 多一道保险 | 双状态、双动作、恢复困难 | **一次 close-round 事务** |
| Agent 手写 requested_event/pause evidence/Params | 暴露路由意图 | 让 Worker 理解 Controller 内部协议 | **Result verdict 驱动工具自动路由** |
| 任意下一次 PreToolUse 才推进 | 借自然事件触发 | 因果关系隐蔽，可能等待无关操作 | **在 semantic submit 后立即求值** |

### 2.2 要删除的是重复控制面，不是专业判断

机制减法不能误删以下能力：

- Delivery、QA、E2E 仍由不同专业 Lens 给出结论；
- DV、QA、E2E 每个 Lens 都允许 1..N 个专业 Reviewer，且没有人为最大值；删掉的是“一责任一 Agent”的机械映射和重复泛审，不是多视角验证；
- token 是本阶段购买缺陷发现率和可维护性的必要投入；不得以成本、默认并发数或“一个 Agent 应该能做完”为理由合并认知过载的 Assignment；
- QA 必须审查设计模式/项目惯用法的适配、逻辑自洽、边界/依赖方向、可读性和维护风险，而不退化成 lint + tests；
- 安全、性能、可靠性、迁移等高风险专项仍按 impact 触发；
- E2E 仍需真实浏览器和原始证据，不因统一 Result 而退化成一条文字 PASS；
- 运行型 Finding 仍需一份真实 encounter；但操作时间线、network/console/trace 应由工具自动采集，不让 Reviewer 手写第二份测试日志；
- `not_applicable` 仍需明确来源和理由；
- CleanRound 仍是严格合取，只是不再需要 Agent 手工组装。

## 3. 目标权威模型：ReviewPlan → Assignment → ReviewResult → CleanRound / ObservationBatch

### 3.1 单一事实链

```mermaid
flowchart LR
    S6["S6 Integrated Baseline"] --> RP["ReviewPlan<br/>frozen baseline + claims + DAG"]
    RP --> DV["DV focus Assignments<br/>1..N"]
    RP --> QA["QA focus Assignments<br/>1..N"]
    RP --> E2E["E2E coverage Assignments<br/>1..N as applicable"]
    DV --> DR["DV ReviewResults<br/>1..N"]
    QA --> QR["QA ReviewResults<br/>1..N"]
    E2E --> ER["E2E ReviewResults<br/>1..N"]
    DR --> C["Round Consumer"]
    QR --> C
    ER --> C
    C -->|"finding"| F["immutable Finding"]
    F --> OB["sealed ObservationBatch"]
    OB --> S8["S8 Diagnosis"]
    C -->|"req/release blocker"| P["paused"]
    C -->|"all applicable claims pass"| CR["CleanRound Snapshot"]
    CR --> S10["S10"]
```

权威分工：

| 事实 | 唯一工程权威 | 非权威投影 |
|:--|:--|:--|
| 本轮要审什么、为什么适用 | ReviewPlan Claims | Prompt、Team task、Markdown 看板 |
| 谁负责、scope、checks、dispatch mode | Assignment | Agent/teammate 活会话状态 |
| Reviewer 实际怎么审、看到什么 | ReviewResult + raw evidence refs | 聊天 final、聚合摘要 |
| 失败时看到的表象、操作现场和 failure boundary | immutable Finding（含 encounter）+ sealed ObservationBatch | BUG 草稿、聊天摘要、复现脚本、root-cause 猜测 |
| 本轮是否完整、有效、blocker-free | CleanRound evaluator/snapshot | 人工总 PASS |

所有控制面记录遵循 L4 的共享存储不变量：即使 E2E spec 或报告写在隔离 worktree，ReviewPlan、Assignment、plan checkpoint、ReviewResult 和 round checkpoint 仍必须写入项目级共享控制面；Writer 在内部串行写入并记录提交 revision。

### 3.2 ReviewPlan

每个 review round 只有一份权威 ReviewPlan：

| 字段 | 含义 |
|:--|:--|
| `review_plan_id` / `review_round` / `revision` | 计划身份、轮次和工具生成的计划版本；不要求 Agent 手工推进 |
| `baseline_generation` | 当前冻结 generation |
| `frozen_subjects[]` | S6 产品代码、配置、已有产品测试、REQ、设计、契约、TASK、CASE/PATH 的精确版本与指纹 |
| `change_impact` | S6 变化面、依赖接缝、风险标签、历史 BUG 与回归面 |
| `claims[]` | 本轮必须回答的具体可审计命题；每条 Claim 记录 `lens/source/target/assertion/oracle/applicability` |
| `assignments[]` | Claims 的责任分组和 DAG 投影 |
| `claim_coverage_view` | 由 Claims 的 disposition 计算出的静态、行为、必需发现等查询视图；不作为第二套长期权威 |
| `e2e_coverage_state` | `cold_start / regression_available / not_applicable`；由当前 CASE/PATH 到有效可执行 E2E spec/evidence 的映射计算，不由 Agent 主观填写 |
| `verification_artifact_workspace` | 仅在 E2E `cold_start` 需要新建 spec/fixture/dataset 时启用的独立写面；已有验证资产时不创建额外控制面 |
| `dispatch_capacity_policy` | 固定为 L4 `coverage_complete`；required Assignment 逻辑数量不设上限，物理槽位不足只进入 queued；不因此开放无限 Claim 扩张 |
| `dispatch_policy` | 优先级、真实依赖、共享环境/账号/spec 写面的 resource locks 和平台容量；不得含 Reviewer/token 硬上限 |
| `status` | `planned / running / cannot_clean / discovery_draining / observation_sealed / clean / paused / stale`；close-round 是 submit 内部事务，不是持久化状态 |

ReviewPlan 在 S7 入口生成初始完整 required Claims。平台是否当下允许 launch 某个 Agent 是调度事实，不应删除或合并 required Assignment。只有 Result/Finding 明确暴露一个初始 plan 未覆盖、且可以通过 `source_ref + affected_surface` 定位的新表面时，才允许一次受控 ReviewPlan 对象版本；版本号由工具生成，之后重新计算受影响 Claims，完成后即关闭本轮计划。所谓“无新增 Claim”是一次计算结果，不是 Agent 可以自行递归扩大范围的长期状态。

### 3.3 Claim：取代重复的 angle/责任/检查叙事

Claim 是“本轮必须由 Reviewer 判断的一条具体命题”，不是泛化分类词：

| 字段 | 要求 |
|:--|:--|
| `claim_id` | round 内稳定身份 |
| `lens` | `delivery / qa / e2e` |
| `target` | 精确模块、路径、contract clause、CASE/PATH、运行时不变量或失败模式 |
| `assertion` | 要证明的陈述 |
| `oracle` | 什么可观察事实算 pass/fail |
| `method` | 代码审查、命令、真实浏览器步骤、trace 检查等 |
| `required_evidence` | 最低证据类型，不复制证据正文 |
| `applicability` | `required / not_applicable`；N/A 必须有 `source_refs + rationale` |
| `source_refs` | REQ、risk、impact、BUG history、Closing Contract 或 CASE/PATH 来源 |
| `focus_key` | round-local 专业侧重点，用于 1..N Assignment 分组；无全局 registry、继承、最小数量或独立生命周期 |
| `priority/cost` | 用于决定顺序、环境准备和资源调度，不得用于删除 required Claim 或限制 Reviewer 数量 |
| `depends_on[]` / `resource_locks[]` | 仅记录真实前置条件和共享资源冲突 |

不再要求“每个 Lens 至少三个 angle”。Claims 数量由真实影响面决定：小变更可以少而具体，高风险跨组件变更必须增加接缝与专项 Claims。

`focus_key` 不是复活 angle registry。它只回答“哪类专业上下文最适合一起读”，并必须被 Assignment grouping 和 coverage view 消费。典型值可来自本轮事实动态生成：DV 的 requirement traceability、architecture/contract conformance、integration completeness；QA 的 design/boundary、logic/state/error、maintainability/idiom、test/oracle，以及风险触发的 security/performance/reliability/migration。没有适用 Claim 就不创建空 focus。

历史学习不再通过 append-mostly angle registry 强制逐条确认；它从以下事实自然进入新 ReviewPlan：

- 当前 open/closed BUG 和 targeted re-verification；
- change-impact invalidation；
- 历史 escaped defect 和 flaky test；
- 现有 CASE/PATH、Closing Contract 与风险标签；
- 上一轮未消费或 stale 的 Claim。

### 3.4 Assignment：调度粒度不等于 Claim 粒度

S7 Assignment 复用 L4 Assignment，只增加 `claim_ids[]`、`focus_keys[]` 和 `non_overlap_boundary` 投影。Lens 与 Agent 不是一对一：一个 Lens 可以生成 1..N 个 Assignments；一个 Assignment 仍只有一个执行 owner。一个 Assignment 可以覆盖多个 Claims，当且仅当：

- 属于同一角色 Lens；
- authoritative read set、工具上下文和证据目的地高度重合；
- 合并后仍可逐 Claim 给出独立结论；
- 不破坏 Builder/Reviewer、Original Finder/Repair Builder 等 separation edge；
- 工作量仍能在一个可恢复上下文内完成；
- 不制造并行写冲突或共享环境冲突。

`non_overlap_boundary` 写明该 Assignment 主责回答什么，以及相邻 Reviewer 不应重复做什么。跨焦点重叠只有在能提供不同 oracle 时允许，例如 architecture Reviewer 看 dependency direction，maintainability Reviewer 看同一服务的认知复杂度；两者不能都提交一份没有独立问题定义的“全量代码 review”。

必须拆开 Assignment 的情况：

- 不同专业角色或不同结论权限；
- 安全/release/权限等需要额外独立性；
- 不兼容的工具、Skills、环境或 write scope；
- E2E Agent 会争用同一账号、数据集、端口或 spec 文件；
- 合并后计划无法清晰表达每个 Claim 的 oracle 和检查。

Assignment 数量不是预设配额，也没有最大值。Planner 先生成 Claims、focus/flow graph，再按可恢复上下文聚类；凡是专业判断、read set/方法、persona/旅程、风险边界或环境/数据隔离使单 Agent 可能过载，就继续拆分并派足 Reviewer。合并只为共享紧密上下文，不为省 token；重复 read set + 相同 oracle 的泛审应合并，但不同 oracle 的独立视角必须保留。

### 3.5 Canonical ReviewResult

每个 Assignment 只提交一份结构化 ReviewResult：

```json
{
  "result_id": "review-result-...",
  "assignment_id": "assignment-...",
  "assignment_revision": 3,
  "review_plan_id": "review-plan-...",
  "review_round": 2,
  "baseline_generation": 4,
  "subject_digest": "sha256:...",
  "verification_artifact_digest": "sha256:...",
  "claim_results": [
    {
      "claim_id": "claim-...",
      "conclusion": "pass",
      "method": "...",
      "observed": "...",
      "evidence_refs": ["..."]
    }
  ],
  "checks": [{"command": "...", "result": "pass", "evidence_refs": ["..."]}],
  "findings": [],
  "deviations": [],
  "verdict": "pass"
}
```

约束：

- `claim_results[]` 必须与 Assignment 的 Claim 集合精确相等，不得漏项或越权添加；
- Claim 结论为 `pass / fail`；可恢复阻塞使用 Assignment 的 BLOCKER checkpoint，不伪装成专业结论；`not_applicable` 是 ReviewPlan disposition，不是 Reviewer 在 Result 中临时选择的结论；
- E2E Result 必须同时绑定 frozen product `subject_digest` 与本次实际使用的 `verification_artifact_digest`；spec/fixture 在 Result consumed 后变化，只 invalid 依赖它的 Result 并要求重跑，不使产品 baseline stale；
- `verdict` 由 Claim 结果和 finding 类型校验，不允许与局部结果矛盾；
- 每个 fail Claim 必须引用一份待提交 Finding；运行型 Finding 还必须带 `encounter`，不能只把 `observed` 或测试报错复制进来；
- `evidence_refs[]` 中需要由 Harness 在本地校验的仓库文件使用显式 `path:<repo-relative-path>` 形式；裸 refs 保留给浏览器 trace、平台 artifact ID 等外部/符号证据，已登记的 runtime evidence ID 则按其 path+sha256 复核；

Claim 的 round-level disposition 由 Planner/consumer 计算为 `planned / running / pass / finding / not_applicable / blocked / stale`。`blocked` 不是 Reviewer 自报的专业结论；只有工具能根据已确认 Finding、失败前置条件和证据，把它投影为 `blocked_by_confirmed_finding`。该投影必须带 `blocking_finding_ids[] + failed_precondition + evidence_refs + after_repair_required=true`，不另建一套长期 Claim 状态。
- Markdown、图表和 E2E 原始产物可以继续作为人读/raw evidence，但不再充当另一份状态权威；
- Agent final text 只是通知，不能替代 ReviewResult。

### 3.6 Finding：只陈述表象，不抢答根因

Finding 是 S7 从某条失败 Claim 中提取的不可变观察事实。它必须足以让一个没有原会话上下文的 S8 Investigator**不重新复现症状，也能建立初始因果边界、竞争假设和判别计划**：

| 字段 | 含义 |
|:--|:--|
| `finding_id` | 稳定身份；S8/S9 全链只引用，不改写 |
| `review_plan_id / review_round / baseline_generation / subject_digest` | 证明观察发生在哪个冻结世界 |
| `assignment_id / claim_id / lens / original_finder` | 谁从哪个验证命题观察到 |
| `expected` + `authority_refs[]` | 预期行为及 REQ/contract/CASE/PATH/Closing Contract 来源 |
| `observed` | 实际可观察表象，不混入原因判断 |
| `observation_mode` | `user_flow / api_flow / command_flow / state_transition / code_inspection`；决定 encounter 的最低字段，不强迫代码审查伪造用户动线 |
| `encounter` | 本次真实发生的“操作现场”，见下文；不是未来复现脚本 |
| `reproducibility` | 对已经实际执行样本的 `always / intermittent / once_with_deterministic_trace` 记录；不要求为填数量额外重跑 |
| `evidence_refs[]` | trace、request/response、console、screenshot、video、data snapshot、command output 等 typed refs；关键证据还须绑定 encounter step/checkpoint |
| `correlation_refs[]` | trace ID、request ID、entity ID、账号、时间窗等跨表象关联键 |
| `visible_impact` | 对用户、数据、系统和风险的已观察影响 |
| `negative_facts[]` | 邻近但仍正常的行为，用于 S8 排除假设 |
| `open_questions[]` | S7 尚未回答的问题 |
| `hypotheses[]` | 可选线索，必须标为 unverified，不得成为路由权威 |

`encounter` 是 Finding 内的 discriminated union，不是新的顶层 authority、独立报告或生命周期。下面是统一字段全集；`journey_summary / scenario_ref / runtime_context / timeline / last-good / wall-action / first-bad / evidence-or-capture-gap` 为共同核心，其余字段由 `observation_mode` 决定 required/optional，避免为不适用场景制造空字段：

| 字段 | 含义 |
|:--|:--|
| `journey_summary` | 1～3 句短动线：`入口/目标 → 关键动作 → 撞墙动作 → 首个异常/终态` |
| `scenario_ref / entrypoint` | CASE/PATH、页面、API、命令、测试或代码入口 |
| `runtime_context` | build/commit、环境、浏览器/CLI/runtime 版本、locale/timezone、flags/config/dependency refs；不复制 baseline 已有正文 |
| `actor_context` | persona、role、tenant、账号类型和权限；敏感值只存脱敏 ref/hash |
| `initial_state_ref` | 操作前 UI/数据/系统状态和 test seed |
| `timeline[]` | 当次实际步骤；每步含 sequence/time、action、target、sanitized input ref、observed checkpoint、evidence refs |
| `last_good_checkpoint` | 最后一个明确正常的状态或检查点 |
| `wall_action` | 触发或暴露首个偏差、使预期动线无法正常继续的操作、请求、命令、状态转换或代码证据点；不暗示它就是根因 |
| `first_bad_checkpoint` | 最早可确认的偏差，不等同于最终错误提示 |
| `blocked_continuation` | 原本下一步以及为什么无法继续；若仍可继续，记录受损后的实际分支 |
| `terminal_state` | 页面、请求、数据、进程或调用链最后停在哪里 |
| `state_delta_refs` | before/after UI、payload、response、持久化、cache、event 或调用路径差异 |
| `side_effects` | 已持久化、部分写入、重复操作、脏数据、错误提示、禁止副作用和恢复行为 |
| `attempt_variants[]` | 实际重试和每次唯一变化的条件；不要求为了填字段额外重复危险操作 |
| `capture_gaps[]` | 因安全、权限、环境或工具限制未捕获的现场，以及对调查的影响 |
| `cleanup_state` | 测试数据/会话是否保留、清理或不可再访问 |

示例：`从客户详情进入编辑页 → 修改税号 → 点击保存；页面提示成功，但刷新后税号为空，后续审核无法继续。` 这句话帮助 S8 在数秒内理解动线；真正调查仍读取对应 timeline、request/response、状态差异和 correlation refs。

最重要的调查边界是：

```text
last_good_checkpoint → wall_action → first_bad_checkpoint
```

不同 observation mode 只要求与其真实形态相符的最小现场：

| mode | 必需现场 |
|:--|:--|
| `user_flow` | entrypoint、用户动线、关键 UI checkpoint、wall action、terminal state、network/console/state refs |
| `api_flow` | 调用方状态、sanitized request、response、correlation/time window、下游状态差异 |
| `command_flow` | cwd/tool version、sanitized command/inputs、stdout/stderr/exit、产生或未产生的 artifacts |
| `state_transition` | before state、event/action、expected/actual transition、after state 和副作用 |
| `code_inspection` | inspection entry、symbol/call/data-flow trail、最后成立的不变量、首个违反点和源码证据；不要求伪造“点击步骤” |

复杂度控制遵循“短摘要 + 自动 trace”：Reviewer 只负责 `journey_summary`、failure boundary 和专业判断；浏览器、测试 runner、CLI wrapper 与 trace collector 自动生成 timeline、时间戳、请求/响应、console、状态快照和 hashes。大量 raw data 只作为 typed evidence 保存，Finding 不复制正文。无法自动采集时才允许人工最小记录。

Finding 禁止将以下内容写成已确认事实：`root_cause`、`repair_scope`、`suggested_fix`、canonical BUG mapping。S7 可以观察“填写后不显示”和“填写后无法保存”，但不能仅凭直觉将二者合并为“后端 DTO 错误”。

原 Finding 永不覆盖更新。S8 需要补充判别信息时，通过 original finder 产生 `FindingSupplement`，追加新的 observation/evidence/correlation refs，并保持 `supplements_finding_id` 和独立 hash；Supplement 不用于补做 S7 本应交付的基础现场。

### 3.7 ObservationBatch：S7→S8 的无损交接

首个 confirmed Finding 打开本轮 ObservationBatch；所有 required Claims 都有 disposition，且一次受控 coverage 求差不再产生新 Claim 后 seal：

| 字段 | 含义 |
|:--|:--|
| `observation_batch_id` | S7→S8 handoff 身份 |
| `review_plan_id / frozen_baseline_digest` | 所有 Finding 的共同观察基线 |
| `finding_ids[]` | 本批不可变 Finding exact set |
| `drained_assignment_ids[]` | 首个 Finding 后为完成剩余 required Claims 而派发、收尾并被消费的观察任务 |
| `drain_policy` | `complete_required_claims / immediate_stop`；普通安全 Finding 固定前者，只有 P0/安全/数据破坏类使用后者 |
| `claim_coverage_summary` | required Claims 的 exact set、按 lens/来源的 disposition、最终 ReviewPlan revision 和受控 coverage 求差结果；静态、E2E、Discovery 只是该视图的筛选，不再各自产生 summary 权威 |
| `cancelled_or_non_gating_assignment_ids[]` | 仅记录高危停线、权威 pause 或 baseline stale 导致的取消；普通 cannot-clean 不取消 required discovery Assignment |
| `unobserved_claim_ids[]` | 正常 ordinary batch 必须为空；仅 immediate-stop/safety gap 可保留 exact IDs 与原因，不能用来节省 token 或提前进入 S8 |
| `original_finder_routes[]` | 可继续联系/resume 的 Agent、Assignment、responsibility；会话失联时仍可从 Finding 恢复 |
| `investigation_readiness` | 每个 Finding 的 encounter/evidence 完整性和显式 capture gaps；不是“可再次复现”评分 |
| `severity_summary / stop_reason` | 为什么立即停线，或为什么按普通策略继续完整发现 |
| `sealed_at / sealed_by / revision` | 不可变交接和工具生成的版本信息 |

ObservationBatch 只打包事实，不做语义去重，也不预分配 canonical BUG。两个 Finding 表象相近仍分别保留；是否同根由 S8 的 InvestigationCase 证明。

这里的 coverage 求差是一次受控计划收口，不是开放式资源边界：初始 Claims 完成后，仅允许由 Result/Finding 的 `source_ref + affected_surface` 触发一次 revision；revision 后的 exact set 完成即收敛。它不按 Agent 数、token、墙钟时间或“已经发现足够多问题”截断，也不允许 Agent 无来源地继续增加 Claim。

`investigation_readiness` 只有两个可交接状态：`ready`，或仅用于 P0/安全/数据破坏类的 `ready_with_safety_gaps`。它不是主观分数。普通 Finding 缺 wall action、first-bad、关键 state/evidence 时不得 seal；高危 Finding 可以停止危险复现，但必须逐项列出 capture gap、停止理由和 S8 可安全使用的既有证据。

对所有普通 Finding，`claim_coverage_summary` 是 seal 条件：初始及一次受控 revision 后的 required Claims 必须全部得到 `pass / finding / not_applicable / blocked` exact disposition。工具只在已有 Finding 客观导致 build/start/entry/precondition 不成立、继续操作无信息增益或不安全时，把 `blocked` 投影为 `blocked_by_confirmed_finding`；它不是 PASS，也不能满足修复后新轮的 Claim。普通环境/凭证/工具缺失复用 Assignment BLOCKER 留在 S7，不能伪装成产品因果 blocker；尚未计划或因省 token 未执行的验证面不能 seal。P0/安全/数据破坏的 immediate-stop 例外必须显式保留未完成 Claims，供后续安全新轮恢复。

Evidence freeze 是 `runtime review-result submit` 前后的原子子步骤和短生命周期 capture buffer，不新增 `capturing` phase、人工审批或独立恢复账。若进程中断，恢复依据仍是 Assignment + capture artifacts + Result draft。

### 3.8 CleanRound

CleanRound 是由 ReviewPlan、被消费的 ReviewResult、fingerprint 和 BUG/finding 状态计算出的不可变快照。它不是 Reviewer 自己填写的报告，也不是单独派给某个 Agent 的判断任务。

## 4. S7 的规划与有思路的派发

### 4.1 入口条件

进入 S7 前必须满足：

1. S6 所有纳入本轮的 Builder Result 已被集成并消费；
2. 没有 in-flight 产品写 Assignment；
3. 当前代码、配置和 authoritative documents 可生成统一 frozen digest；
4. change impact、风险标签和 CASE/PATH 索引可读取；
5. 需要真实环境的 Claims 有环境准备责任或明确 blocker；
6. review round ID 和 ReviewPlan revision 已创建。

入口不要求三个 Workgroup 已经 launch；它要求整轮覆盖面已经可计划。

### 4.2 Claim 生成顺序

Main/Planner 按以下顺序生成 Claims：

1. **Closing Contract**：每条 S6 声明必须有独立验证 oracle；
2. **REQ/contract/CASE/PATH**：生成符合性和用户可观察行为 Claims；
3. **change impact**：为跨模块、API、数据库、配置和依赖接缝增加 Claims；
4. **DV focus decomposition**：按 requirement traceability、architecture/contract conformance、module/integration completeness 拆出白盒交付 Claims；
5. **QA static quality decomposition**：按 design/boundary、pattern/idiom fit、logic/state/error、maintainability/readability、testability/oracle、debt/operability 生成基线 Claims；这是所有实现都要回答的质量面，不依赖先出现 E2E 症状；
6. **E2E coverage inventory**：将 persona、声明入口、CASE/PATH、正向/负向/边界/恢复、状态持久化、禁止副作用和跨层可观察点映射到现有可执行 spec/evidence；计算 `cold_start / regression_available / not_applicable`；
7. **E2E coverage decomposition**：对缺失或被 change impact 失效的映射生成 required E2E Claims，不把“已有一条 happy path”当作完整覆盖；
8. **风险标签**：按需增加 security、performance、reliability/concurrency、migration 等专项；
9. **历史缺陷**：将相关 BUG、escaped defect、flaky area 转成回归 Claim；
10. **负向 oracle**：检查拒绝、无副作用、持久化、恢复和错误传播；
11. **覆盖求差**：所有 authoritative obligations、适用 static quality focus 与 E2E surfaces 必须映射到 required Claim 或有证据的 N/A；Result/Finding 只有暴露新 affected surface 时才允许一次 revision，revision 后闭合最终 Claim set。

Planner 只生成机器和 Reviewer 真正消费的字段，不复制规格正文；Claim 通过 `source_refs` 指回权威内容。

只要本轮包含产品代码、测试、配置、schema 或迁移实现变化，DV 和 QA 都至少有一个 required Assignment；`N=0` 仅允许纯文档/元数据变化，且必须通过 impact/source 证明没有可执行或可维护性影响。这里的最小值保证 Lens 不被跳过，最大值仍由 focus graph 而不是固定 roster 决定。

### 4.3 E2E 的适用性

“完整验证轮”表示所有**适用观察面**都完整，不等于每次变化机械创建浏览器 Agent。

| 影响面 | E2E 决策 |
|:--|:--|
| 用户可见 UI、入口、交互、路由或浏览器状态变化 | 必须执行相关模块完整 CASE/PATH |
| 后端/API 变化会影响现有用户旅程 | 必须执行受影响旅程及负向路径 |
| auth、权限、持久化、异步状态影响浏览器结果 | 必须执行对应真实路径和副作用 oracle |
| 纯内部重构但 change impact 指向关键回归面 | 按风险执行回归 E2E |
| 纯文档、纯构建工具或无浏览器 oracle 的内部变化 | 可 N/A，但必须记录 impact/source/rationale；不能仅凭 `ui_impact=none` 静默跳过 |

是否需要 E2E 由 `required_surfaces` 和可观察行为决定，单一标签只能作为输入，不能直接成为结论。

E2E 数量同样没有预设上限。Planner 先计算本需求的 coverage state：

| coverage state | 判定 | 计划要求 |
|:--|:--|:--|
| `cold_start` | 本需求首次从 S6 进入 S7，或当前 generation 的 required CASE/PATH 没有可复用且指纹有效的 executable spec/evidence 映射 | 从零建立 coverage matrix，并按可恢复 flow context 拆成 1..N E2E Assignments；不得默认交给一个通用 Agent |
| `regression_available` | 已有 spec/evidence 与当前 CASE/PATH、入口、selector/contract 和 environment fingerprint 对齐 | 复用有效资产，对 changed/invalidated/gap surfaces 补 Claims；仍按实际规模派 1..N，不固定一个回归 Agent |
| `not_applicable` | 没有用户/外部参与者可观察行为，且 impact/source/rationale 完整 | 不创建空 E2E Agent；N/A 进入 ReviewPlan 并参与 close gate |

`cold_start` 不是一个新阶段或人工审批门，只是 Planner/Assignment generator 的必消费输入。它至少展开以下 coverage dimensions：persona/权限、声明入口、核心 flow clusters、跨页/跨服务接缝、正向与负向路径、边界数据、持久化与刷新、禁止副作用、错误可见性与恢复、必要浏览器/设备/环境。每个 dimension 必须映射到 Claim/Assignment/oracle；不适用项要有证据，而不是由单个 E2E Agent 在执行中临时决定不测。

### 4.4 Assignment 分组算法

Planner 不按 responsibility catalog 机械一项一 Agent，而按以下键分组：

```text
role lens
+ round-local focus cluster
+ module / bounded context
+ authoritative read set
+ tool and Skill context
+ evidence destination
+ resource lock / write conflict
+ independence requirement
```

典型分组不是“一个 Lens 一个 Agent”，而是：

- DV 可按 requirement traceability、architecture/contract conformance、cross-module completeness 分成多个 Assignments；小变更上下文高度重合时也可合并为一个；
- QA 可按 design/boundary、pattern/idiom fit、logic/state/error、maintainability/readability、testability/oracle、debt/operability 分成多个 Assignments；同一模块很小时可以合并，相互挤压或需不同 Skills 时必须拆开；
- security、performance、reliability/concurrency、migration 只有在风险触发、方法明显不同或需要独立判断时单独派发；
- E2E 按 persona/权限、入口与 flow cluster、状态/持久化/副作用、负向/边界/恢复、浏览器/环境和 spec 写面拆成 1..N Assignments；同一可恢复 flow context 可合并 user-flow 与 console/network Claims，共享账号/数据/端口/spec 文件时用 resource lock 或隔离 worktree，而不是减少覆盖；
- `cold_start` 下禁止把 coverage design、全部 spec authoring、所有 flow execution、evidence capture 和结果提交塞进一个全需求 Assignment；Planner 必须按上下文容量拆到每个 Worker 可以完整读懂、执行和交卷。

Planner 应输出统一 coverage matrix：每个 required DV/QA focus 与 E2E persona/flow/surface → Claim IDs → Assignment ID → oracle。矩阵是 ReviewPlan 的计算视图，不再新增“review roster”文档。若某个重要 focus/surface 没有 Claim，plan/validate 指出缺口；若多个 Assignment 的 Claim/read set/oracle 高度重复，planner 要求合并或写出独立判断理由。这里的 overlap validator 只删除重复劳动，不能以“读了相同文件”为由合并不同 persona、状态或 oracle。

### 4.5 调度优先级与并发

S7 使用“先建立前置事实、再把所有必要验证做完”的流动策略：

1. 能快速确认 authority、contract 和代码结构的 Delivery/QA 检查优先，为 E2E 提供可靠 oracle 与 impact 边界；
2. 安全、权限、迁移等可能需要 stop-the-line 的高风险 Claim 提前；
3. E2E coverage inventory、环境/账号/数据/入口/spec write-surface preflight 与 DV/QA 并行，避免静态 Claims 完成后才发现 E2E cold start 或环境空白；
4. 所有 required DV/QA static Claims 必须先完整 disposition，再按真实依赖解锁行为 E2E，但普通 Finding 不取消后续安全 E2E；
5. static Claims 完成后释放仍适用的 E2E 和专项 Assignment；`cannot_clean` 只决定最终去 S8，不降低 required coverage；
   Assignment DAG 的依赖只有在上游 Result 已被消费并形成 terminal Claim disposition 后才进入 Ready；P0、pause、seal/clean 会先关闭 admission gate，不在同一事务里释放排队 Assignment；
6. 不设置固定 WIP、Agent 数量上限或 token budget gate。所有依赖独立且 resource lock 不冲突的 ready Assignments 都可派发；平台并发槽不足时进入确定性队列，不能删 Claim、缩 oracle 或把多个过载 scope 强塞给一个 Agent；
7. Assignment 拆分以“一个 Worker 能在一个可恢复上下文内完整理解并高质量交卷”为准。token 成本不能否决必要拆分；只有 read set、方法和 oracle 高度重合的真实重复工作才合并；
8. 普通 Finding 后继续完成剩余 required Claims；只有 Result/Finding 携带 `source_ref + affected_surface` 时才触发一次受控 Claim revision，revision 后完成 exact disposition 即收口；
9. P0/安全/数据破坏类仍立即停线；资源冲突只改变顺序，普通环境 blocker 保持 S7 并给恢复动作。仅当 confirmed product Finding 客观使某 Claim 不可执行时，工具才将 `blocked` 投影为 `blocked_by_confirmed_finding`，并保留修复后必验义务，不能把它伪装成 PASS。

因此，S7 有逻辑上的无限 Reviewer 容量，只有物理平台并发和真实资源互斥。调度器管理队列与锁，不管理“最多允许花多少质量 token”。

该原则通过 ReviewPlan `dispatch_capacity_policy=coverage_complete` 下沉到 L4。它不是 Prompt 建议：Assignment/Task 派发 Hook 必须保留 exact required set，并将暂时无物理槽位的工作标为 queued；任何试图把本轮切回 `bounded_flow` 的 revision 都属于质量策略降级，必须由人显式改变 L3 policy，而不是由 Main 为省成本临时决定。

## 5. 两波验证与端到端闭环

### 5.1 为什么不是三段全串行

Delivery 与 QA 都是在同一 frozen baseline 上进行的独立读审，QA 不需要等待 Delivery PASS 才能判断代码和测试质量。同轮一致性由 baseline digest 保证，不由排队顺序保证。

严格串行只在存在真实依赖时有价值，例如：

- E2E 环境构建依赖前置集成成功；
- 某个行为/昂贵检查需要静态 Claims 或环境前置事实先完整 disposition；普通 Finding 改变出口，但不取消该检查；
- 多个 E2E Assignment 争用相同数据或账号；
- 专项检查需要前一 Claim 产出的定位结果。

这些依赖应写入 Assignment DAG，而不是固化成所有项目都必须经过的阶段墙。

### 5.2 Wave A：高信息、低成本与环境预检

默认包含：

- 全部 required DV focus Assignments：requirements/authority traceability、contract/architecture conformance、module/integration completeness；
- 全部 required QA static focus Assignments：design/boundary、pattern/idiom fit、logic/state/error、maintainability/readability、testability/oracle、debt/operability；
- 早期阻断价值高的安全、权限、架构或迁移 Claims；
- E2E 环境、账号、入口、selector、数据和浏览器可达性 preflight。

E2E preflight 可以与 1..N DV/QA Assignments 并行，但只是解锁真实执行的 enabling Assignment，不产生产品行为 PASS，也不能替代 E2E Claims 或满足 CleanRound。行为 E2E 在静态 Claims 全部得到 disposition 后按依赖解锁；普通 Finding 会使本轮 `cannot_clean`，但不会阻止后续安全 E2E 发现。只有 critical stop、权威 pause、baseline stale 或真实不可执行 blocker 才不继续。

Wave A 结果：

- 有 confirmed ordinary finding：先把当前 session 的 inspection/operation trail、last-good/wall/first-bad、before/after 状态和 raw evidence 冻结到 Finding encounter，再原子登记 Result + Finding，ReviewPlan 标 `cannot_clean` 并打开 ObservationBatch；继续完成剩余静态 Claims，随后仍释放可安全执行的 E2E 和专项 Assignments；
- 新 Finding 或 pass Result 暴露具体 affected surface：revision ReviewPlan、生成对应 Claims/Assignments 后继续；不做无来源的全仓漫游，但也不以“计划已经写完”为理由忽略新证据；
- P0/安全/数据破坏类：保存已产生现场后立即 seal，不因追求“更多问题”延误止损；
- 需要改 REQ：原子创建 pause checkpoint，进入 paused；
- release blocker：原子创建 release checkpoint，进入 paused；
- 环境/凭证/工具 blocker：保留 S7，给出可恢复下一步；不能以 API 直调绕过真实 E2E；若是已确认产品 Finding 使 build/start/entry 不成立，则对受影响 Claims 计算 `blocked_by_confirmed_finding`，带证据进入最终 batch；
- 静态 Claims complete：不论当前是 clean candidate 还是 ordinary `cannot_clean`，只要安全且可执行，就解锁行为 E2E Ready Set。

### 5.3 Wave B：真实行为与昂贵专项

默认包含：

- 按 coverage matrix 拆成 1..N Assignments 的所有适用 E2E CASE/PATH；
- console/network、持久化、禁止副作用、拒绝和恢复 oracle；
- 必要的性能、可靠性、并发或迁移演练；
- ReviewPlan 根据 Wave A 事实显式新增、且经过工具生成对象版本的 Claim。

行为波次不是允许任意扩 scope。新增 Claim 必须来自 Result/Finding 的 `source_ref + affected_surface`，且每轮最多进行一次受控 ReviewPlan 对象版本，再重新计算受影响 Assignment。版本号由工具生成，所有 required Claims 有 disposition 后，本轮计划即收口；首次 E2E `cold_start` 下，spec/fixture/evidence 写面按 Assignment 隔离或加锁，平台排队可以延长时间，但不得减少 E2E Agents 或合并过载 flow。

### 5.4 主流程

```mermaid
flowchart TD
    IN["S6 Results consumed"] --> FREEZE["Freeze baseline + open review round"]
    FREEZE --> PLAN["Generate ReviewPlan<br/>DV/QA/E2E coverage + DAG"]
    PLAN --> WA["Wave A<br/>DV/QA static Claims<br/>+ E2E preflight"]
    WA --> RA{"Submit/evaluate ReviewResults"}
    RA -->|"ordinary finding"| EF["Evidence freeze + immutable Finding<br/>mark cannot-clean"]
    RA -->|"pass"| RECOMP["Recompute impact + coverage"]
    EF --> RECOMP
    RA -->|"P0/security/data-destructive"| STOP["Freeze safe evidence + capture gaps"]
    STOP --> SEAL["Seal ObservationBatch"]
    RA -->|"REQ change"| PR["TR-010 → paused"]
    RA -->|"release blocker"| PB["TR-011 → paused"]
    RA -->|"recoverable environment blocker"| REC["Stay S7<br/>one recovery action"]
    REC --> WA
    RECOMP --> DONE{"All required Claims<br/>have disposition?"}
    DONE -->|"no"| READY{"Ready/queued required Assignment?"}
    READY -->|"yes"| STATIC{"Static Claims dependency<br/>fully dispositioned?"}
    READY -->|"no: blocked"| BLOCK["Stay S7<br/>show exact recovery action"]
    STATIC -->|"no"| WA
    STATIC -->|"yes"| WB["Wave B<br/>1..N E2E + required specialties"]
    WB --> RB{"Submit/evaluate ReviewResults"}
    RB -->|"ordinary finding"| EF
    RB -->|"pass"| RECOMP
    RB -->|"P0/security/data-destructive"| STOP
    RB -->|"REQ change"| PR
    RB -->|"release blocker"| PB
    RB -->|"baseline drift"| STALE["Round stale<br/>new complete round"]
    DONE -->|"yes"| GAP{"Coverage diff finds<br/>new concrete surface?"}
    GAP -->|"yes"| REVISE["One controlled ReviewPlan revision<br/>add source-backed Claims"]
    REVISE --> DONE
    GAP -->|"no: plan closed"| OUT{"Findings exist?"}
    OUT -->|"yes"| SEAL
    OUT -->|"no"| CLOSE["Machine close-round transaction"]
    SEAL --> S8["TR-008 → S8 Diagnosis"]
    CLOSE --> CR["Immutable CleanRound"]
    CR --> S10["TR-009 → S10"]
```

## 6. 三类 Lens 如何工作

### 6.1 Delivery：是否做对东西

Delivery Verification（DV）从当前模块完整真相而不是触发变更的一小段 REQ 开始。它可以按不同 focus 派出 1..N 个 Reviewers，至少检查适用的：

- REQ/AC 与可观察结果；
- design、FE/BE/SYNC contract 与实际实现；
- TASK Closing Contract 与实际 diff/checks；
- 当前模块 CASE/PATH 和负向行为；
- 跨组件接缝和回归面；
- Builder Result 声明与实际实现是否一致。

典型 DV focus：

| focus | 主要问题 |
|:--|:--|
| requirement/acceptance traceability | 每条权威要求是否在实现、配置、迁移和测试中有真实落点 |
| architecture/contract conformance | 组件职责、FE/BE/SYNC contract、数据/控制方向是否符合设计 |
| module/integration completeness | 是否遗漏消费者、生成物、注册、依赖接缝、负向路径或回归面 |

Builder Result 只是入口和声明，不能转抄为 Delivery PASS。DV 不修改产品代码或被审测试。DV 可以观察到架构不符合，但它的判断核心是“是否忠实交付设计”；QA 的核心则是“即使功能对了，这个实现本身是否值得维护”。相邻 Claims 必须在 coverage matrix 中写清 oracle，避免两组 Reviewer 提交同一份泛化意见。

### 6.2 QA：是否以可交付方式实现

QA 是 S7 的白盒质量治理主力，不是“跑 lint、看 coverage、确认测试存在”。它要在 E2E 之前判断 Builder 交出的代码是否逻辑自洽、边界清晰、符合项目惯用模式，另一个 Agent 能否低风险地读懂和修改。QA baseline 至少覆盖：

| focus | 必须回答的问题 | 典型 evidence |
|:--|:--|:--|
| design/boundary | 责任是否单一，模块/层/接口边界和依赖方向是否清晰，source-of-truth 是否唯一 | package/module graph、interfaces、imports、ownership、data/control flow |
| pattern/idiom fit | 存在稳定变化点、状态机、策略切换、适配、资源生命周期等问题时，是否使用项目惯用模式；是否用条件堆叠/复制粘贴代替结构；是否反向过度设计 | repository conventions、language/framework idioms、extension points、branch/duplication structure |
| logic/state/error | 正常/异常/边界路径是否自洽，状态转换、error ownership、rollback、并发和资源释放是否完整 | call/state graph、error paths、transactions、locks/goroutines/tasks、cleanup |
| maintainability/readability | 命名和领域语言是否一致，抽象层级是否稳定，函数/类型是否职责集中，认知/圈复杂度、隐藏副作用和硬编码是否制造改错风险 | code trail、complexity/duplication signals、API shape、comments/implicit contracts |
| testability/oracle | 设计是否可隔离测试，测试是否验证行为而非实现细节，负向/边界/集成接缝是否有有效 oracle | unit/integration tests、fixtures、mocks/fakes、failure assertions、flaky signals |
| debt/operability | 临时兼容、TODO、feature flag、日志/观测、迁移路径是否有 owner 和退出条件 | tracking refs、config、telemetry、migration/rollback plan |

设计模式判断遵循 **fit over count**：

1. 先识别真实设计压力：变化轴、状态复杂度、跨边界适配、生命周期、复用和测试隔离；
2. 再判断项目现有惯用法、语言/框架 idiom 或某个模式是否能显著降低耦合和改错风险；
3. 缺失合适结构、选错抽象、绕开现有模式，以及为未来假设滥用 Factory/Strategy/多层 interface，都属于 QA 判断范围；
4. 不要求每个实现出现命名设计模式，也不以 SOLID/checklist 命中数判 PASS；简单问题用直接清晰实现通常优于模式仪式；
5. pattern-related Finding 必须给出 `context → observed structure → violated quality invariant → maintenance/change risk → code evidence`，不能只写“这里应该用某模式”。

QA 发现的边界混乱、双重 source-of-truth、条件分支扩散、错误处理不闭合、不可测试耦合等，都是 `code_inspection` Finding，可在没有 E2E 表象时直接进入 ObservationBatch/S8。这样 S8 能把结构性 Finding 与后续运行时表象建立因果关系，而不是让维护者等到用户撞墙才看到技术债。

一个 QA Agent 很难同时深入所有 focus。Planner 按 change impact 和上下文将它们组合为 1..N Assignments；同一小模块可由一个 Reviewer 覆盖多个紧密 focus，跨层/高风险/认知负载高时拆开。专项 Claims 仅在 impact/risk 触发时增加：security、performance、reliability/concurrency、migration。每个专项 Agent 必须有明确 target/oracle，不能只执行泛化 checklist。

### 6.3 E2E：真实入口和完整行为

E2E 不是一个 Reviewer，也不是一条 happy path。它是一组按 coverage matrix 派发的 1..N Assignments，数量不设上限。每个 Assignment 负责一个边界清晰、可在单个上下文中完成的 persona/flow cluster，并可以在授权的独立写面内连续完成 spec/fixture 准备、真实执行、证据冻结和逐 Claim 交卷；不再额外制造“设计 Agent → 写脚本 Agent → 执行 Agent”的固定流水线。

首次需求从 S6 进入 S7 且 `e2e_coverage_state=cold_start` 时，Planner 必须先把空白摊开，不能让一个 Agent 边探索边独自决定覆盖范围：

| work pack 维度 | 典型拆分依据 |
|:--|:--|
| persona / permission | 匿名、普通用户、管理员、不同租户/权限边界 |
| entry / flow cluster | 声明入口、核心任务链、跨页/跨服务接缝 |
| state / side effects | 保存、刷新、缓存、异步任务、消息、审计、禁止副作用 |
| negative / boundary / recovery | 校验失败、权限拒绝、极值/空值、超时、重试、回滚和恢复 |
| environment / compatibility | 必要浏览器、设备、locale/timezone、feature flag、迁移前后状态 |
| spec/evidence write surface | 可独立写入的 spec、fixture、dataset、trace/截图目录；冲突面使用 worktree/resource lock |

coverage matrix 先把这些维度映射为 exact Claims，再生成足够多的 E2E Assignments。一个 Worker 可以合并同一 flow context 的紧密 Claims，但如果 PLAN_REPORT 显示它需要同时掌握多个 persona、多个入口、大量独立数据状态或多个写面，Assignment generator 必须继续拆分，而不是期待 Agent 靠更长上下文硬扛。`regression_available` 也只代表可以复用资产，不代表只需要一个 Agent；失效面、变更面和历史缺陷仍按实际规模拆分。

E2E 对每个适用 CASE/PATH 至少证明：

- 从声明入口进入，并按声明控件顺序交互；
- 除非 PATH 明确声明，不用 deep link 代替入口流程；
- 不用直接 API/数据库调用代替用户步骤；
- 每个 material step 有可见断言和 raw evidence；
- console/network、HTTP 状态和契约接缝可观察；
- 持久化结果与禁止副作用成立；
- 负向路径分别说明 `visible`、`terminal_state`、`persisted_effects`、`forbidden_side_effects`、`rejection`、`expected_state`、`recovery`；
- flow/CASE 与真实 UI 冲突时形成规格 finding，不在 Playwright spec 中发明产品行为。

Harness 应从真实执行自动形成 encounter timeline：记录 material action、sanitized input、可见 checkpoint、URL/selector、时间戳、network/console、trace/request/entity correlation 和必要 state snapshots。发生 fail 时自动冻结失败前后的 evidence window；Reviewer 只标记 `journey_summary`、`last_good_checkpoint`、`wall_action` 和 `first_bad_checkpoint`。

Harness 可以机械校验 flow fingerprint、CASE/PATH coverage、step-to-evidence binding、trace/截图存在性、console/network 摘要、failure boundary 和字段完整性；“用户行为是否正确”仍由 E2E Reviewer 判断。自动采集不得记录 password/token/secret 或无必要 PII，敏感输入使用脱敏 ref/hash 和独立 redaction manifest。

## 7. Sub-agent / Agent Team 派发方式

### 7.1 拓扑选择

| 工作类型 | 默认拓扑 | 原因 |
|:--|:--|:--|
| DV focus Assignments（1..N） | 多个 Agent Team teammates + `plan_checkpoint` | 按 traceability/conformance/completeness 分工，共享 frozen baseline 和 coverage matrix |
| QA focus Assignments（1..N） | 多个 Agent Team teammates + `plan_checkpoint` | 按 design/logic/maintainability/tests/专项分工，与 DV 并行 |
| E2E flow Assignments（1..N） | 多个 teammate 或自定义 Sub-agent + `plan_checkpoint` | 按 persona/flow/state/negative/recovery/environment 分工；数量由 coverage matrix 决定，不设上限 |
| E2E cold-start spec/fixture/evidence 独立写面 | 多个调用级 worktree isolation 自定义 Sub-agent 优先 | 让不同 flow packs 并行走实；冲突写面加锁，控制面仍共享，不把全部 authoring 压给一人 |
| 高风险破坏性测试、生产式数据操作 | `plan_approval_required` | 在真正风险边界使用原生 Plan approval |

不要因为当前平台有 Agent Team 就按 checklist 机械一项一 Agent，也不要因为调度方便就固定只派一个 DV、一个 QA、一个 E2E。Agent 数量由 focus/flow graph、上下文边界、Skills、独立 oracle、认知负载和隔离需求决定，没有质量政策上限。coverage matrix 用来发现漏派、过载和真正重复派发；平台容量只决定哪些先 running、哪些 queued。

### 7.2 默认计划回执

所有执行型 S7 Assignment 默认使用 L4 `plan_checkpoint`：

1. Worker 读取 Assignment、Claims 和指向的 authoritative documents；
2. 通过 SendMessage 发送 PLAN_REPORT，至少包含 objective、claim coverage、planned paths、review methods、checks、dependencies、risks/blockers；
3. PLAN_REPORT 不是 final response；发送后立即继续；
4. Main 对齐时保持沉默；scope、oracle、检查或依赖偏移时才发送 CORRECTION；
5. Worker 最终提交 ReviewResult，然后发送 COMPLETION；
6. Main/round consumer 成功消费 Result 后才把 Assignment 标 consumed。

S7 不再维护 readback approval 和 activation 的第二轮开工许可。高风险任务通过 L4 的 `plan_approval_required` 升级，而不是让所有正常审查都等待批准。

### 7.3 Main Agent 的 S7 调度循环

```text
S7_DISPATCH_LOOP()
1. Read ReviewPlan, frozen baseline, Assignments and consumed Results.
2. Revalidate baseline digest; if drifted, mark round stale and stop dispatch.
3. Recompute applicable Claims, DV/QA/E2E coverage views, DAG, resource locks and Ready Set. The static/E2E/discovery views are projections of one Claim set, not separate state machines.
4. Detect E2E cold_start/regression_available/N/A; generate enough bounded E2E flow Assignments before execution.
5. Dispatch every independent ready Assignment allowed by real dependencies/resource locks/platform slots; queue overflow without deleting or merging coverage.
6. Keep behavior E2E dependency-locked until static Claims have exact disposition; run E2E preflight/spec isolation setup in parallel.
7. Consume PLAN_REPORT:
   - mechanical scope/check/dependency validation;
   - semantic claim/oracle judgment;
   - reject overloaded generic DV/QA/E2E plans and split the Assignment;
   - silence if aligned, CORRECTION/revision if drifted.
8. Consume ReviewResult transactionally.
9. On finding verdict, freeze current encounter timeline/evidence window; require last-good/wall/first-bad or explicit safe capture gaps.
10. Mark cannot-clean and open/update ObservationBatch; prohibit repair, but continue all ordinary safe required discovery Assignments.
11. For critical findings, preserve already-produced safe evidence and seal immediately.
12. After every Result, recompute coverage; only a concrete `source_ref + affected_surface` may trigger the one permitted ReviewPlan revision.
13. Once static Claims have exact disposition, unlock all safe ready E2E/required specialty Assignments even when cannot_clean.
14. On pause verdict, create the authoritative checkpoint and stop all new dispatch.
15. Seal ordinary ObservationBatch only when the final required Claim set has exact disposition and `unobserved_claim_ids` is empty.
16. When every applicable claim is consumed without findings, compute and persist CleanRound.
17. Mark Team tasks completed only after Result/round consumption.
```

### 7.4 idle、stop 和失联

复用 L4 决策矩阵：

- 没有 PLAN_REPORT 就 idle/stop：Hook 要求先发计划并继续；
- 只有计划没有 ReviewResult：阻止把计划当交卷；
- Result 已提交等待消费：允许 idle，不自动领取下一个 Claim；
- Main 纠偏后：同一 Agent 继续当前 Assignment；
- 会话已正常结束但可恢复：resume 原 Agent；
- Agent 被取消/killed 且不可恢复：从共享 Assignment、plan checkpoint 和 evidence refs 重派 replacement，不创建第二份运行态真相。

## 8. 指引必须埋在 Agent 的必经之路

`agent-protocol.md` 只保留 S7 入口、三类 Lens、四种出口和对本文件/L4 的链接。持续执行规则分布如下：

| 必经位置 | S7 特有内容 | 控制方式 |
|:--|:--|:--|
| `verification plan` | 从 impact/Closing Contract/CASE/PATH 生成 Claims、DV/QA focus graph、E2E coverage state/matrix、DAG、建议分组 | 缺 coverage、cold start 未拆分或固定“一 Lens 一 Agent”造成 scope 过载时返回具体缺口 |
| ReviewPlan schema | target、assertion、oracle、method、focus key、Claim coverage view、可选 verification artifacts、`dispatch_capacity_policy=coverage_complete`、evidence、N/A 来源 | 计划生成/变更时结构校验；禁止 Reviewer/token 上限字段裁剪 required coverage，也禁止无 source 的递归扩张 |
| Coverage matrix | required DV/QA focus 与 E2E persona/flow/surface → Claims → Assignment → oracle；检测漏派、单 Agent 过载和重复泛审 | 自动投影，不新增 roster 文档 |
| Assignment generator | frozen product digest、verification-artifact revision、claim/focus/flow IDs、non-overlap boundary、scope、checks、Skills、resource locks、dispatch mode、context-load estimate | 派发前必须生成；过载就拆分，Prompt 只是投影 |
| Agent Definition | Lens、最大工具和产品代码禁止写入边界 | 长期稳定角色能力 |
| Spawn prompt | 最小读序、专业 focus/flow pack、non-overlap boundary、Claims、pattern-fit 或 E2E oracle、cold-start 写面、PLAN_REPORT 后继续、Result 提交命令 | Worker 第一轮必然收到 |
| PostToolUse(SendMessage) | 记录 PLAN_REPORT/BLOCKER/COMPLETION | 消息必经路径 |
| 首次写入型 PreToolUse | 缺 plan、revision 过期、approval 缺失时拒绝；通过后不重复提醒 | 开工必经路径 |
| Reviewer 的 Write/Edit/Bash PreToolUse | 产品代码/locked 规格越界 hard deny；只允许 verification workspace 内的 evidence、E2E spec/fixture 和授权命令 | 每次 mutation 必经路径；自动更新 artifact revision，不改 product digest |
| 浏览器/测试/CLI/trace wrapper | 自动记录 material action、checkpoint、sanitized input、时间戳、network/console/state refs | 执行动作自然产生 capture buffer，不要求 Agent 另写日志 |
| fail observer/PostToolUse | 冻结失败前后 evidence window，提示标注 last-good/wall/first-bad | 撞墙当下保全易失现场；不启动修复 |
| ReviewResult/Finding scaffold | 逐 Claim conclusion、observed、encounter summary、failure boundary、evidence binding、capture gaps | 只要求人补机器无法判断的语义 |
| `runtime review-result submit` | schema、身份、scope、fingerprint、coverage、encounter/investigation readiness、verdict 校验 | Result/Finding 进入控制面的唯一入口 |
| TeammateIdle/SubagentStop | 计划不能当交卷，缺 Result 不能结束 | Worker 停止前必经路径 |
| Round consumer | 自动聚合、finding/pause 路由、Claim coverage 求差与 CleanRound 计算 | 普通 Finding 继续发现；不接受人工 aggregate PASS |
| SessionStart/PreCompact | 当前 round、running/queued/blocked Assignments、未消费 Result、Claim coverage 缺口和唯一下一步 | 跨会话恢复；平台容量不足不丢 queued coverage |

错误反馈必须只给当前缺失事实和一个下一动作，例如：

```text
REVIEW RESULT REJECTED: claim QA-UNIT-TEST has no observed result or evidence_refs.
Next: update review-result-17.json for QA-UNIT-TEST, then rerun
loop-harness runtime review-result submit --assignment-id assignment-qa-fund --result ...
```

不能把整份 S7 或 `agent-protocol.md` 注入每次 Hook。

## 9. ReviewResult 提交与出口事务

### 9.1 `runtime review-result submit` 的原子职责

一次提交必须在同一个 Runtime Writer 事务中完成；内部提交 revision 由 Writer 记录，Agent 不提供：

1. 解析 Assignment 和当前控制面对象；
2. 校验 producer/role/independence；
3. 校验 product subject digest 与 frozen baseline；E2E Assignment 同时校验 verification-artifact revision/digest；
4. 校验 Claim 集合、逐项结论、required checks 和 evidence refs；
5. 校验 write/review scope deviations；
6. 对 fail Claim 从 capture buffer 固化 encounter timeline/evidence window，校验 `journey_summary` 和 failure boundary；
7. 按 `observation_mode` 校验 required state/evidence，计算 `investigation_readiness`；
8. 持久化 Canonical ReviewResult；
9. 更新 Assignment `result_submitted`；
10. 若有 finding，创建 immutable Finding 并绑定原 Assignment/Claim，将其加入当前 ObservationBatch；
11. 根据 verdict 更新 ReviewPlan：`cannot_clean`、pause 或继续验证；`cannot_clean` 只关闭 clean path，不取消普通 required discovery；
12. critical finding 不继续危险动作，保存已产生证据和明确 `capture_gaps` 后立即 seal；普通 finding 若 encounter 未 ready，保持原 Reviewer/session 做一次现场补全，不把任务推给 S8；
13. 普通 finding ready 后固定 `drain_policy=complete_required_claims`：继续释放并消费 required DV/QA/E2E Claims；新 evidence 指向具体 affected surface 时进行一次 ReviewPlan revision；
14. 静态 Claims 未 100% disposition 时不启动行为 E2E；最终 required Claim set 未达到 exact disposition 时保持 S7/Assignment BLOCKER，不 seal、不以 `unobserved` 绕过；`blocked_by_confirmed_finding` 必须通过 finding/evidence/after-repair validator；
15. seal 时原子持久化 ObservationBatch、Finding exact set、`claim_coverage_summary`、final ReviewPlan revision 和 readiness 摘要并路由 S8；
16. 返回唯一下一动作；
17. consumer 成功后更新 Assignment `consumed`，再解锁 drain policy 允许继续的 DAG 下游。

提交命令不要求 Agent 另写 phase envelope、requested event、pause wrapper 或 transition Params。

“现场补全”只允许标注 capture buffer、补取当前仍在的只读状态或关联既有日志，不默认从入口重新执行整条旅程。若普通 Finding 的关键现场已经不可恢复，复用现有 Assignment BLOCKER/checkpoint 留在 S7，修复采集条件或由授权人决定是否可安全重建；不新增 capture 状态，也不能伪装成 ready 后把复现债务转给 S8。

### 9.2 Verdict 与路由

| Result verdict | 前置事实 | 工具行为 | 去向 |
|:--|:--|:--|:--|
| `pass` | 所有 assigned Claims pass，无 finding/deviation；N/A 只存在于已批准 ReviewPlan | 持久化并交 round consumer | 继续 Ready Set 或 close round |
| `finding` | 至少一个 confirmed fail；Finding 有真实 encounter、failure boundary、step-bound evidence，或高危情况下有明确 capture gaps | 原子创建 immutable Finding、标 cannot-clean、禁止修复；普通安全 finding 继续完成剩余 required Claims，高危立即 seal | seal 后 TR-008 → S8 |
| `req_change_required` | 当前 authoritative REQ 无法支持正确实现/判断 | 创建 pause checkpoint，不由 Reviewer 改 REQ | TR-010 → paused |
| `release_blocked` | security/compliance/环境等需人工决定 | 创建 release checkpoint | TR-011 → paused |

Finding 是观察事实，不在 S7 自动变成 accepted BUG。S8 负责消费已保存的 encounter、聚类、假设验证、因果推导和 RepairContract；S7 必须保证 ObservationBatch、全部 Finding IDs、readiness 和 raw evidence refs 真正随 TR-008 handoff 到达 S8，不能只改变 lifecycle cursor，也不能默认让 S8重新复现症状。

环境、凭证、工具或输入缺失且可恢复时，Worker 发送 BLOCKER，Assignment 进入 blocked 并保留恢复动作，不提交“blocked ReviewResult”。baseline、round 或对象版本变化时，submit 拒绝旧 Result 并将 round/Assignment 标 stale；`stale` 是控制面状态，不是 Reviewer verdict。Runtime revision 的变化本身不要求 Agent 参与处理。

### 9.3 N/A 的控制

N/A 只允许两条路径：

1. Planner 在 ReviewPlan 中依据 impact/source 明确标 `not_applicable`；
2. Reviewer 发现适用性判断错误，提交 PLAN_REPORT/BLOCKER 请求 ReviewPlan revision，由 Main/Planner 更新后再提交 Result。

Reviewer 不能在最终 Result 中单方面把 required Claim 改成 N/A。机器也不能把缺 Result 当 N/A。

ReviewPlan 标为 `not_applicable` 的 Claim 不再派发，也不要求生成一份“空审查”Result；它由 close-round 直接校验 disposition 的 source 和 rationale。

## 10. CleanRound 的正确语义

### 10.1 严格条件

机器生成 CleanRound 前必须同时满足：

1. ReviewPlan 属于当前 review round 和 baseline generation；
2. DV/QA/E2E coverage matrix 完整，单一 required Claim set 全部 dispositioned，且没有 unresolved generic-overlap/overload；
3. 所有 `required` Claims 都有当前 Assignment 的被消费 Result；
4. 每个 required Claim `conclusion=pass`；所有 N/A 均来自当前计划且理由/来源完整；
5. Result kind、producer role、Assignment、Claim、round 和 generation 精确对应；
6. consumed Result 引用的 product subject fingerprints 仍与 frozen baseline 相同，verification artifact digest 仍与该 Result 实际执行版本相同；
7. 没有被消费的 PASS 已 invalid/superseded；无关 invalid evidence 不应污染本轮；
8. 当前轮不存在 confirmed blocking Finding；一旦确认，S7 必须原子标 `cannot_clean`、继续完成剩余 required Claims，并在最终 Claim set 完成后路由 S8，不能继续尝试 clean；
9. 没有业务定义为 blocking 的 open BUG，不以“只有 P0”代替语义 blocker；
10. 若本轮承接 S9，targeted re-verification 只作为 BUG 闭合前置，不替代任何 full-round Claim；
11. Reviewer/Builder independence、scope compliance 和 Result deviations 均满足；
12. 适用 E2E 的 coverage state、flow fingerprint、CASE/PATH coverage、spec/fixture digest 与 raw evidence 结构验证通过；
13. 没有 in-flight 产品写或尚未消费的 review Assignment。

### 10.2 一次收口

当最后一份 required ReviewResult 被消费时，round consumer 运行一次 close-round 事务：

```text
recompute focus/claim/assignment exact set
→ validate DV/QA/E2E coverage views and final Claim set
→ validate consumed results
→ validate product baseline freshness + verification artifact digests
→ validate findings/bugs/independence
→ write immutable CleanRound snapshot
→ set ReviewPlan clean
→ commit S7 → S10 handoff
```

不再把 `clean_round_evaluation` 当成需要派 Agent 的业务阶段，不要求执行者先写一条通用 `clean_round/pass` envelope 再由纯函数重新检查，也不在相邻 transition 重复 `record_clean_round`。

CleanRound 必须保存用于重算的最小引用和 digest，而不是复制所有 raw evidence：ReviewPlan ref、Result IDs/hashes、frozen product baseline digest、verification artifact digests、Claim disposition 摘要、Finding/BUG 摘要、evaluated_at/by 和 evaluator version。

### 10.3 stale 与新完整轮

以下任一事实使当前轮 stale：

- 产品代码、S6 产品测试、配置、依赖或 authoritative documents 变化；
- ReviewPlan required Claim 集合发生实质变化；
- 被消费 Result 的 subject/evidence 失效；
- S8/S9 修复产生新实现；
- 运行环境版本变化到足以改变 oracle。

S7 在授权 verification workspace 中新增/修订 cold-start E2E spec、fixture 或 evidence 不使产品 round stale；它只增加由工具生成的 artifact revision。某份 Result consumed 后，其绑定的 spec/fixture digest 若变化，该 Result 和依赖 Claim 变为 invalid，必须用新 artifact revision 重跑。若所谓 E2E spec 修改越界改变了产品测试、CASE/PATH 或 locked 规格，则按产品/权威变化使整轮 stale。

新轮创建新 ReviewPlan 和新 Result IDs；历史记录保留但不进入当前集合。旧轮 evidence 不因仍在 index 中而否决新轮，也不能因 ID 相同而满足新轮。

## 11. 生命周期与状态投影

### 11.1 目标状态

S7 控制面以 ReviewPlan 状态表达整轮收敛：

```text
planned -> running -> clean
              |           |
              |           +-> stale -> planned(new round)
              +-> cannot_clean -> discovery_draining -> observation_sealed -> S8
              |                    (critical finding skips drain)
              +-> paused -> human/REQ/release decision
```

`discovery_draining` 按 `drain_policy` 投影：普通安全 Finding 完成剩余 required Claims；critical finding 跳过 drain。这样复用一个状态表达两种确定性策略，不新增 `static_reviewing_after_failure`、`e2e_after_failure` 等阶段。`cannot_clean` 后即使其余 Result 全 pass，也只能丰富 ObservationBatch，不能回到 clean path；但不能因此取消尚未执行的 required E2E。

Delivery/QA/E2E 的 `pending/running/passed/finding/blocked/stale` 是从 Claims/Assignments/Results 计算的 Lens 视图，不再作为三段互相等待的独立权威状态机。

### 11.2 与现有 Transition 名称的目标映射

| 事件 | 目标行为 |
|:--|:--|
| S6 `TR-006` 进入 S7 | 冻结 baseline、打开 review round、创建 ReviewPlan |
| blocking finding | Result submit 原子登记 Finding 并标 cannot-clean；ObservationBatch seal 后携 exact Finding set 由 `TR-008` 进入 S8 |
| req change | Result submit 创建 checkpoint，`TR-010` 进入 paused |
| release blocker | Result submit 创建 checkpoint，`TR-011` 进入 paused |
| all applicable Claims consumed | close-round 原子生成 CleanRound，`TR-009` 进入 S10 |
| stale/incomplete | 创建新 ReviewPlan/review round，从 Ready Set 重新完整验证 |

现有 `PTR-VERIFY-01/02/03/04/05` 在迁移期间可以保留为兼容投影，但不能继续要求 Agent 手工制作 phase aggregate 才能推进。迁移完成后应由 Assignment DAG 和 result consumer 取代。

## 12. 职责分布与 L1 准则

### 12.1 职责落点

| 职能 | 主责 | 权威载体 |
|:--|:--|:--|
| baseline freeze/change impact | Main/Orchestrator + Harness | ReviewPlan frozen subjects |
| Claims 与适用性 | Planner/Main | ReviewPlan |
| Focus/flow coverage、Assignment grouping、队列/资源锁、拓扑 | Planner/Scheduler/Main | ReviewPlan coverage matrix + Claim coverage view + L4 Assignment |
| DV 专业判断 | 1..N Delivery Reviewers | 各 focus 的 ReviewResult claim results |
| QA 专业判断 | 1..N QA Reviewers | design/logic/maintainability/test/专项 ReviewResult |
| E2E 专业判断、cold-start spec/fixture 与 raw evidence | 1..N E2E Reviewers | 各 persona/flow/surface 的 ReviewResult + spec/trace/JSONL/PNG/video refs |
| encounter 自动采集与脱敏 | Harness/wrappers | transient capture buffer + typed evidence |
| journey/failure boundary 标注 | 原 Reviewer | Finding nested encounter |
| mechanical focus/claim coverage、freshness、identity | Harness | plan validator + result submit + round evaluator |
| finding 观察事实 | 原 Reviewer + Harness | Finding entity linked from ReviewResult；自动事实与人工语义分工 |
| BUG 调查与接受 | S8 | canonical BUG lifecycle |
| clean round | deterministic evaluator | immutable CleanRound snapshot |
| REQ/release 决策 | Human Gateway | pause/release checkpoint |

### 12.2 L1 映射

| L1 准则 | S7 落点 |
|:--|:--|
| D1 权威外置 | ReviewPlan、Assignment、ReviewResult、Finding、CleanRound 都落共享控制面；聊天和 Markdown 只是投影/证据 |
| D2 自然路径观测 | plan 在 SendMessage、操作在 execution wrapper、撞墙在 fail observer、结果在 submit、停止在 idle/stop、收口在 result consumer 上观察 |
| D3 门是顾问 | 缺失时返回具体 Claim/字段/下一命令；真实越权与 stale 才 hard fail |
| D4 引导性产物 | DV/QA focus + E2E flow coverage matrix、specific Claims 和 non-overlap boundary 迫使 1..N Reviewers 形成互补判断；Finding 的短 journey/failure boundary 形成可交接事实 |
| D5 三级强制 | Prompt/Skill 引导 → Schema/Assignment 结构 → PreToolUse/submit/round evaluator 机械控制 |
| D6 三方收敛 | Builder 闷头产出，1..N DV/QA Reviewers 从不同白盒视角挑战，1..N E2E Reviewers 穷举适用真实行为，机器汇总；REQ/release blocker 交人 |
| D7 收敛可观测 | ReviewPlan board 展示 Ready/Running/Blocked/Consumed Claims 和唯一下一步 |
| 公理二 分工 | Main 计划调度，Reviewer 判断，Harness 计算，人握需求/发布；Reviewer 不修产品 |
| 公理三 消费 | 每个字段必须被 Assignment、Result validator、Finding flow 或 CleanRound 消费；不再维护 wrapper |
| 公理四 成本 | 必要 Reviewer/token 是防止缺陷逃逸和反复返工的质量投资，不设上限；只消除重复泛审和无消费控制面。static-first 保证 oracle 清晰，不用于取消 E2E；encounter 内嵌 Finding 且自动采集 |
| 公理五 传达 | pass/finding/req change/release blocked/recoverable blocked/stale 有不同出口 |

## 13. 当前实现状态与历史迁移记录

> **2026-08-18 审计（P0+强耦合 P1 落地）**：批次 A+B 已实施。控制面已迁为 ReviewPlan 模型 ——
> `review-plan.schema.json`（单一 required Claim set + Assignment 精确分割 + `dispatch_capacity_policy=coverage_complete` 固定值）、
> `review-result.schema.json`（Canonical ReviewResult + verdict 枚举 + fail-Claim→Finding 绑定）、
> `finding.schema.json`（discriminated `encounter`，按 `observation_mode` 分模式必填字段，含 capture gaps）、
> `observation-batch.schema.json`（exact Finding set + coverage summary + investigation readiness）四个嵌入 schema 落地；
> `internal/review` 实现两个动词：`runtime review-plan --file`（校验精确分割/lens 分离/N/A 不派发/依赖无环/DV-QA 最小覆盖后钉入共享控制面，phase→running）
> 与 `runtime review-result submit --assignment-id --result`（单 CAS：result evidence + immutable Findings + claim disposition 投影 + reviewer agent working→reported
> + cannot_clean/draining 状态机 + 事务内 round consumer——普通 finding drain 完成后 seal ObservationBatch、P0 立即 seal、无 finding 生成机器 CleanRound、
> pause verdict 在事务内创建唯一 checkpoint）。phase machine 重写为 ReviewPlan 状态投影（planned/running/cannot_clean/discovery_draining/observation_sealed/clean，
> PTR-VERIFY-01..05 删除）；TR-006/012/016 落 planned；TR-008 改绑 sealed ObservationBatch（guard `observation_batch_sealed` 做 exact-set 校验，
> `record_finding_batch` 从 state 读 Finding 实体、按 finding 内容哈希去重建 BUG 草稿）；TR-009 guard 换 `clean_round_valid`（重算
> `verification.EvaluateCleanRound`：plan clean + 逐 Claim consumed pass + 无本轮 Finding + 无 invalid 本轮 review 证据 + 无 open blocking BUG + 快照已注册）；
> TR-010/011 单载体化（verdict 事务建 checkpoint，transition 只移 cursor，guard `pause_checkpoint_recorded`）。gate 侧 GATE-VERIFY-DELIVERY/QA/E2E-PASS
> 与 GATE-CLEAN-ROUND-INCOMPLETE 删除；GATE-VERIFY-CLEAN-ROUND-PASSED/BLOCKING-FINDING 接 exact-set 求值（missing token 进 `cleanround:`/`batch:` 族，
> missingtokens 图例覆盖四个 S7 gate）；PTR-BUG-01 与 GATE-BUG-DRAFTS-READY 改消费 `observation_batch_record`（S8 从 sealed batch 起步）。
> reviewer 写路径 hard deny 落地（policy engine 第三条 block reason `reviewer_product_write`：verification 阶段 Write/Edit/MultiEdit/NotebookEdit
> 仅允许 `.claude/`、`docs/reports/` 与 plan 声明的 `verification_artifact_workspace`）。register-workgroup 绑定 ReviewPlan（manifest assignment
> 携带 `claim_ids[]`/`focus_keys[]`/`non_overlap_boundary`，精确集校验 + lens 匹配 + behavior 波次等 static 求齐，dispatcher 写入 agent 并翻 Claims→running；
> 阶段校验放宽为 running/cannot_clean/draining）。指引层：agent-protocol #s7 改为动词驱动序列、`docs/reports/review/RESULT-template.md` 统一投影模板、
> QA/E2E 模板 verdict 枚举对齐、README 状态图/进度表/叙事更新、clean-round-evaluation skill 改为机器 CleanRound 只读检查、
> `loop-harness s7 status` 只读看板（claim disposition / assignment 消费 / finding / 出口）。证据目录新增 `review-result`/`finding`/`observation_batch`
> 三个 registered kind（`review_result_record`/`finding_record`/`observation_batch_record` slot 别名兼容旧词汇）。
> 未做（按 §13.3-§13.5 排期）：angle 生命周期正式退役（guards 仍在注册表但已无 transition 引用，P1 第 3 项余留——schema/CLI/registry 文件删除）、
> two-phase-activation→plan_checkpoint 切换、PostToolUse(SendMessage) 计划回执捕获（仍被 migration 模板显式禁止，需先反转禁令）、
> Claim 自动生成与 coverage 求差、ReviewPlan 受控 revision、E2E cold-start 写面隔离、encounter 自动采集 wrapper。§13.1 下表保留为历史差距记录。
>
> **2026-08-18 审计（批次 C：P1 余项 + P2 落地，§13.3/§13.4 全清）**：
> - **angle 生命周期删除**：8 个整文件移除（transition angle_complete_guard、runtime/angles、cli/angles_command、两份 schema）；guards 注册表与 semanticChecks、
>   evidence catalog kind、loop-state 枚举、team-manifest 的 `inherited_angles`/`$defs.inheritedAngle`、run.go dispatcher 全清；fixtures 死代码（WriteVerificationDimensionPass 等）删除；
>   有效意图由 Claim.source_refs 承接（§3.3）。`dispatched_at` 字段保留（WS2 的派发时间戳）。
> - **plan_checkpoint 连续执行**：team-manifest assignment 增 `dispatch_mode`（plan_checkpoint 默认 / plan_approval_required / one_shot），register-workgroup 写入 agent 行；
>   `entity_lifecycles.agent` 增 `understanding_submitted --activation_sent--> activated` 直通；activation 哈希链语义保留（plan_checkpoint 下绑定 PLAN_REPORT 文件字节）；
>   `plan_report` 消息类型入 agent-message schema（assignment_id/revision/objective/planned_paths/steps/assertion_checks/dependencies/risks_blockers）；
>   `skills/agent-dispatch` 取代 two-phase-activation；agents/*.md Phase Contract 全部改写。
> - **PostToolUse(SendMessage)**：migration 禁令反转（白名单 7 事件、黑名单删 PostToolUse、测试翻转）；settings.json + hook-policy 注册；
>   `internal/hook/posttooluse.go` 观察者（三级识别：payload agent_id → teammate_name → 唯一等待 agent 兜底；识别失败静默）；CAS 幂等写 `plan_reported_ref`；
>   policy engine 第四条 block reason `assignment_write_before_plan`（spawned/reading + 无 plan checkpoint + 可识别 agent → block；主会话天然豁免）。
> - **ReviewPlan 增值闭环**：`runtime review-plan revise --file --source-ref --affected-surface`（每轮至多一次受控 revision；变更必须绑定触发证据与受影响面；
>   已消费 result 自动 invalidate）；`loop-harness s7 draft` 只读生成器（TASK→DV 追溯 claim、QA focus 桩、E2E 状态评估）；注册时 TASK 覆盖 block（当前 generation 每个 TASK
>   必须出现在 ≥1 claim.source_refs）；E2E cold-start 写面（注册时创建 `e2e-workspace/<plan-id>/` 并钉 digest，submit 按 digest 绑定，收口重算拒绝漂移）；
>   capture buffer（`loop-harness capture step` 追加脱敏 JSONL，秘密模式硬拒绝；`review-result submit --captures` 并入空 timeline 的 finding）。
> - **平台边界（如实声明）**：PostToolUse 子代理 payload 不带 agent_id 时识别静默跳过（观察冗余，权威注册仍走 runtime 动词）；浏览器/runner 注入式 encounter
>   采集属产品侧 wrapper，harness 提供 capture buffer + 脱敏 gate + 并入绑定；L4 其余 P0（官方 payload 字段接入 teammate_name/transcript_path 等）不在本批。
>
> §13.2–§13.5 的 P0/P1/P2 项至此全部落地；§13.1 下表保留为历史差距记录。
>
> **2026-08-18 审计（复杂度审查批次：沙箱代入驱动）**：在沙箱中完整驱动了 planned→draft→注册→派发→plan_report→直通 activation→submit→seal→TR-008 链，修复了 5 个真实缺陷并记录 2 个已知摩擦：
> - **路由 verdict gate 误伤**（正确性）：TR-010/011 的 gate 把当前轮普通 pass/finding 结果当作 naming-error conflict（历史 deferred-conflict 机制误用于"等待稀有 verdict"的 gate），导致健康 round 期间每次 PreToolUse 都显示 gate unknown。新增 `EvidenceRequirement.RoutingVerdict`：路由 gate 的 conclusion 不匹配静默跳过。
> - **sealed 产物缺 envelope 身份字段**（正确性，被上一条掩盖）：机器生成的 ObservationBatch / CleanRound 文件缺 evidence_id/kind/runtime_id/producer 字段，gate 的信封一致性检查必然 :schema——真实流必然失败。两处构建器 + schema 已补齐。
> - **doc/CLI 断链**：协议写 `review-result submit` 而 CLI 不消化动词词，flag 静默不解析。已修。
> - **draft 可用性**：`s7 draft` 缺 created_at 导致注册即 schema 拒绝；`next` 在 planned 阶段不提 draft。已修。
> - **角色错配提示**：activation 成功消息给 reviewer 也打印 worktree/task-complete 引导；已改为按 role_family 分流。
> - 已知摩擦（记录未修）：reviewer manifest 仍 19 个必填顶层字段，写作负担重（候选：`s7 manifest-draft` 生成器，后续批次评估）；schema oneOf 错误信息冗长（santhosh 枚举所有分支），靠 examples 缓解。
>
> **2026-08-22 审计（L4 P0 平台接线 + P3 余项 + 跨阶段接口，五工作包并行）**：
> - **官方 payload 接入**：`policy.Input` 无损保留 TeammateIdle/SubagentStop 官方字段（teammate_name/team_name/transcript_path/
>   agent_transcript_path/last_assistant_message/stop_hook_active）；TeammateIdle 无 agent_id 时在 hook 入口归一为 teammate_name，
>   hookctx/controller/audit 全链路同一身份；PostToolUse 识别链插入官方顶层 teammate_name 一级。
> - **TeammateIdle/SubagentStop 真实平台控制**（`internal/hook/stopidle.go`）：计划前 idle、计划后无 Result 的 idle/stop → block 并
>   exit 2 + stderr 反馈送回同一 Agent；`stop_hook_active`、未识别 Agent、one_shot、已 reported/blocked 一律 fail-open allow（边界如实声明）。
>   判定事实源与 Controller 的 idle/stop handler 相同（hookctx + assignment 行），未新造状态机。测试基于 2.1.218 文档记载 payload 形状；
>   真实平台 doctor 未做（环境无 Claude Code 运行时）。
> - **PreToolUse(TaskUpdate) self-claim 门**：新规则 `unauthorized_task_self_claim`（settings.json matcher + `policy.EvaluateAgentScoped`），
>   队友不得把未派发的 Team task 认领给自己；自有任务的状态更新不受影响。规则与 `reviewer_product_write`/`assignment_write_before_plan`
>   同样只作为代码规则存在——hook-policy.schema.json 把 rules 钉死为 minItems/maxItems=2，文档登记需先放宽 schema（或维持
>   "文档只收两条最小安全规则"的既定设计）。
> - **FindingSupplement 落地**：`finding-supplement.schema.json`（additionalProperties:false 封死 root_cause/repair 字段）+
>   `runtime finding-supplement --finding --file [--authorized-by]`；校验 schema、独立内容 hash、Finding 存在且属当前 round、
>   作者=original_finder 或经授权；原 Finding 与 sealed ObservationBatch 字节不变（测试逐一断言）。因 loop-state.schema.json 封闭，
>   索引暂落独立控制面文档 `.claude/review/finding-supplements.json`（lock 序列化 CAS）；并入主状态需放宽 entities/evidence kind 枚举。
> - **S7 运营指标机器可采集子集**（`internal/metrics/s7.go`，§14.2）：round shape（Assignment/Claim 数、plan revision gauge）、
>   逐 Claim lead time、submit accepted/rejected（首次成功率）、finding 数、首个 finding→seal 时长、clean round 数；
>   `s7 status` 追加只读摘要段。需人工判断的指标（pattern-fit 质量、逃逸归因）不采集。
> - **`s7 manifest-draft`**：从控制面生成 register-workgroup 可用的 manifest 草稿；team-manifest schema 实际 20 个必填顶层字段
>   （此前记录的 19 已漂移），草稿 20/20 预填，仅剩 agent_id 与 QA/DV skill_refs 两个 TODO。
> - **schema 根级 oneOf 错误剪枝**（`internal/schema/errors.go`）：可判别时只报目标分支真实叶子错误，不可判别时给截断摘要；
>   完整错误经 `LOOP_HARNESS_SCHEMA_VERBOSE=1` 恢复，errors.As 不受影响。
> - **SessionStart/PreCompact S7 恢复投影**（§8 要求落地）：verification 阶段的恢复包含当前 round/plan 指针、
>   running/queued/blocked Assignment 三桶、未消费 Result、required Claim 覆盖缺口和唯一下一动作，全部从共享控制面计算。
> - **兼容别名审计**：删除零消费者的自名别名 kind 与 qa/e2e_review_record slot；有真实消费者（测试 fixture、REQ-039 兼容）的保留
>   并逐条注释权威与删除条件。`docs/loop-definition.json` 引用均为保留的 slot 名，无需改动。
> - **协议瘦身**：agent-protocol.md #s7 段重写为入口/三类 Lens/四种出口 + blueprint 链接（被 schema/CLI/hook 机械强制的细则删除）；
>   loop-orchestration 的 Agent Activation Barrier 整节改写为 plan_checkpoint 现实；5 处 two-phase-activation 残留引用清理。
> - **§14.1 测试矩阵补齐（30 个新测试，仅新建文件）**：plan validator（capacity policy/DV-QA N=0/TASK source_refs 覆盖/cold-start
>   workspace）、submit 原子性（错 round/generation/digest、fail Claim↔Finding 绑定、裸 user-flow Finding、code_inspection 免 UI 步骤、
>   P0 capture gaps、双 finder 同症状各自保留、drain→seal 链、seal 后迟到 Result、重复提交、capture 并入）、E2E workspace digest
>   绑定与 stale、CleanRound 旧轮污染/无 result_ref/targeted reverify 不替代 full round、reviewer 写路径 hard deny、
>   behavior-wave 解锁顺序。审计同时确认了 §14.1 中**机制未实现**的精确清单：overlap/泛审 validator（non_overlap_boundary 仅声明式）、
>   cold-start overload validator、regression_available 指纹复用、平台并发槽/resource lock 排队调度、blocked_by_confirmed_finding
>   投影（当前无任何 blocked 写入路径）、Finding 现场丢失的 BLOCKER 复用、S8 重跑默认拒绝 gate、worktree 共享控制面。
> - **仍未做（下一批候选）**：Controller 的"假唤醒"（HandleTeammateIdleForController resume 分支 CAS activated、idle 中自动分配下一
>   TASK，L4 P2 范围）；buildGuidance 的 Facts 自造字段（assignment_reported 等）在官方 payload 下恒真导致 guidance 误报 blocked；
>   真实 2.1.218 平台 doctor；supplement 索引并入主 loop-state（需 schema 放宽）；浏览器/runner 注入式 encounter 采集 wrapper（产品侧）；
>   §14.1 全量场景测试矩阵补齐（本批起持续补）。
>
> **2026-08-22 审计（机制缺口批次二，五工作包并行）**：
> - **Controller idle 语义收敛（L4 P2）**：HandleTeammateIdleForController 删除 resume 分支的假唤醒 CAS 与 idle 中自动分配下一 TASK
>   （十个死代码函数移除）；handler 只投影 guidance，真实唤醒由 stopidle.go 的 exit 2 承担。buildGuidance 不再读官方 payload 不存在的
>   自造 Facts 字段，SubagentStop/TeammateIdle 的 blocked 判定改为与 stopidle.go 共用控制面事实源（hookctx + assignment 行）。
>   首写屏障裁决落地：assignment_write_before_plan 接入 wire 路径（EvaluateAgentScoped），豁免面对齐 reviewer_product_write
>   （.claude/、docs/reports/、verification workspace），9 个 pin 旧契约的测试逐条改写。
> - **blocked_by_confirmed_finding 投影落地**（internal/review/blocked.go）：Result 可声明 blocked_claims，七步校验链
>   （finding 本轮已确认 / failed_precondition ∈ build|start|entry|precondition / evidence_refs 必填 / after_repair_required 恒 true /
>   与 claim_results 互斥 / 不得越出本 Assignment / N/A 不可 blocked）；seal 时 claim_coverage_summary.blocked_claims[] 携完整绑定；
>   CleanRound exact-set 不把 blocked 当 pass。防滥用：无 confirmed Finding 的 blocked 声明直接拒绝。
> - **现场丢失 BLOCKER 复用**：Result 显式声明 site_lost[] → 独立 CAS 把 Assignment 置 blocked 并绑 work_blocked_ref，不消费、不 seal、
>   不伪装 ready；P0 禁止走 site_lost（仍走 capture_gaps + immediate seal）；readiness 拒绝信息提示 site_lost 出路。
> - **overlap / cold-start overload validator**（plan.go）：overlap 确定性规则=同 lens + target/method/oracle 三集合全同才疑似重复，
>   oracle 不同或双方 non_overlap_boundary 互异即放行；cold_start 下单 E2E Assignment 独占全部 required e2e Claims 且跨 ≥2 个
>   focus 维度即拒绝。resource lock 排队落地：锁冲突不拒绝不裁减，queue_reason 记录持有的锁，holder Result 消费后重评估派发；
>   loop-state schema 的 review.assignments 增 resource_locks/queue_reason。worktree 共享控制面核验通过（真实 git worktree 集成测试
>   证明 --root 指向项目根时读写同一 .claude/ 控制面，不带 --root 不静默重锚）。
> - **capture exec 自动采集 wrapper**：`loop-harness capture exec --assignment <id> -- <cmd>` 自动落 command_flow timeline step
>   （cwd/工具版本/脱敏命令/exit code/截断摘要+全文 hash 引用/产物 digest 差集）；失败自动冻结 evidence window（failure.json +
>   wall-action 候选标记），exit code 透传；双闸门脱敏（argv 执行前硬拒、stream 命中秘密模式改写为 withheld 占位符）；
>   8MiB 级输出有界。手册新增 capture 章节与产品侧浏览器/runner 注入契约。
> - **supplement 并入主 loop-state**：entities.finding_supplements 入 schema，SubmitSupplement 走主 runtime CAS；旧
>   `.claude/review/finding-supplements.json` 首次写入时按 supplement_id 幂等迁移并删除，迁移前 runtime 仍可兜底读。
>   discriminator 判别门落地：默认要求 hypothesis_id + discriminator + expected_outcomes 三件套（S8 重跑默认拒绝），
>   `--in-round-note` 豁免轮内补充且与 hypothesis_id 互斥。manual `evidence add --kind finding_supplement` 被显式堵死
>   （pipeline-owned，recovery 信任边界不入 importable kinds）。
> - **仍未做（后续批次）**：真实 2.1.218 平台 doctor（环境限制）；浏览器/runner 注入式采集的产品侧 wrapper 实现（契约已写入手册）；
>   regression_available 的 spec/selector/environment 指纹复用校验（当前 cold_start 与 regression 的区分已可计算，复用有效性判定未做）；
>   qualitygate fixture 迁移后删除保留的旧 lens kind 别名（删除条件已逐条注释在 catalog.go）。
>
> **2026-08-22 审计（五视角 E2E 代入测试：Planner / QA Reviewer / E2E cold-start / 对抗犯错 / 恢复+复杂度）**：
> 五个 tester 在独立沙箱用真实二进制黑盒实测，产出约 30 条发现。对抗性测试结论：**12 类攻击 11 类 D3 合规（缺什么+一个下一动作）、
> 零误伤、零死胡同**——门体系整体健康。缺陷去重后分三档：
> - **本批已修（3）**：① wire 路径不投影 `verification_artifact_workspace`（buildSafetyInput 缺口）——E2E cold-start Reviewer 唯一授权写面
>   被 PreToolUse 真实 hard deny，P0 功能缺陷，已修并加回归测试；② 错误信息承诺的 `s7 workspace-digest` 子命令不存在——已实现；
>   ③ "agent is reading/activated…"错误列十个状态不给下一步——改为给出 plan_report → activation_sent → work_started 的具体命令序列。
> - **高优先（多视角交叉确认，未修）**：④ clean round 边界 `next`（让跑 TR-009）与 `ready`（禁止手调 transition）指引矛盾；
>   ⑤ reading→working 推进无顺滑路径（三个 tester 独立撞墙，Reviewer 估计 70% 新人在此放弃；③的文案修复只缓解未根治）；
>   ⑥ oneOf 错误剪枝在生产 CLI 路径休眠——review 系列 schema 用 anyOf 无根级 oneOf、agent-event 路径未走剪枝包装，单测绿但生产不生效；
>   ⑦ 恢复包三缺陷：cannot_clean 不明说 drain 继续、sealed 后进 S8 丢 TR-008/batch id 来源、`Missing: claim_results` 噪音；
>   ⑧ S7 前置状态无引导路径（五个 tester 全部靠手搓 loop-state.json 才能进入 S7——上手成本的第一放弃点）。
> - **中低优先（记录待修）**：capture exec 子进程秘密 pass-through 到终端（证据文件安全但 UI 泄漏）；RegisterPlan 失败遗留 workspace
>   目录（原子性缺口）；validateInvestigationReadiness 的引擎级必填字段（last_good_checkpoint 等）不在 example/schema 中披露；
>   readiness 一次只报一个缺陷（应聚合）；supplement 判别门先于存在性校验；post-seal submit 不带 TR-008 指引；manifest-draft 不预填
>   validation 五元组；`runtime review-plan` 单动词双语义（register/revise）；register-workgroup 泄漏 S6 词汇（--task/--task-id）；
>   REQ 文件名即 ID 未提前披露；`validate --all` 被无关 skill frontmatter 卡住；journal cursor 错位无 reconcile 引导。
> - **复杂度总结论（重点考察项）**：38 个概念中 22 个必须主动理解、11 个工具托管知名即可、5 个完全隐藏——隐藏层级健康。
>   与旧世界（三段串行+手写聚合 PASS+手写 pause wrapper+S8 重现）相比**净复杂度持平**：操作层更简（六步链塌缩为一次 submit、
>   零手写 transition），词汇层更重（exact-set、双 digest、encounter 判别联合），但每个概念对应 §2.1 一个具名失效模式，无镀金。
>   测试员给出的 5 条削减建议全部是披露修复（恢复包补 TR-008/batch id、review-plan 拆动词、claim_results 改名、cannot_clean 明说
>   drain、s7 status 预披露 workspace digest 要求），**不需要新机制**——视为设计未过度建构的信号。
> - **虚假报警澄清**：cold-start tester 报告的 B7（未注册 agent 绕过 reviewer_product_write）经核实不成立——Controller 的
>   buildSafetyInput 从已提交 snapshot 独立推导 CurrentState，reviewer_product_write 不需要 agent 身份，fake agent 仍被拦；
>   hookctx 失败只使 agent 级规则（TaskUpdate 门、首写屏障）fail-open。
>
> **2026-08-22 审计（高优先缺口根因修复批次，四工作包并行）**：五个高优先缺口全部关闭——
> - **next/ready 矛盾**：核验确认 TR-008/TR-009 均在 hook 自动提交路径（auto_trigger + 证据自动绑定），纯文案说谎；
>   run.go 四处（next 提示 ×2 + submit 成功消息 ×2）统一改为"下一次 PreToolUse 自动提交，不要手调 CLI"。
> - **恢复包三缺陷**：cannot_clean/draining 恢复包明说 drain_policy 与"≠结束"；S8 入口 SessionStart/PreCompact 投影补
>   "entered via TR-008 with observation_batch <id>（N findings）"来源行；S7 语境下过滤裸 `claim_results` 噪音 token。
> - **oneOf 剪枝休眠**：从"根级 oneOf"推广为"最浅判别型失败节点"（零匹配 oneOf/anyOf，causes≥2），anyOf 部分匹配不剪；
>   实测真实 agent-event 错误 2974B→351B；verbose 逃生门脚注全覆盖。
> - **reading→working 断点（最大放弃点）**：plan_checkpoint 自动激活链落地——register-workgroup 预生成激活信封，
>   PostToolUse(SendMessage) 捕获 plan_report 后同事务链式推进 reading→understanding_submitted→activated→working
>   （哈希链语义保留，不绕过 AdvanceAgent 守卫；plan_approval_required 不变）；新增兜底动词 `runtime agent-begin`；
>   submit 错误文案指向一步恢复。集成审阅修复了兜底的环形死路（无预生成信封的 legacy agent 现在从 manifest 合成能力集恢复，
>   含 schema 兼容性与测试）。
> - **S7 入口引导**：`s7 draft` 非 verification 阶段给出当前阶段 + 合法入口（TR-006/012/016）+ 单一指引；QA claim target
>   从真实 change surface 推导（占位文本消除）；manifest-draft 披露 validation 五元组；手册新增 sandbox recipe 节。
> - 仍未做：真实 2.1.218 平台 doctor（环境限制）；产品侧浏览器 wrapper；regression_available 指纹复用校验；
>   capture exec 终端 pass-through 秘密泄漏（证据文件安全）；RegisterPlan 失败遗留目录；readiness 多缺陷聚合；
>   命令面拆分（review-plan register/revise、register-workgroup 的 S6 词汇）。
>
> **2026-08-23 审计（验证轮：五视角二次黑盒代入，确认修复不引入新缺陷）**：
> 五个 tester 在独立沙箱用修复后代码盲测，结论对比第一轮：
> - **第一轮缺陷全部回归**：B4 next/ready 矛盾、B2 workspace-digest 假命令、Reading→Working 断点三处都生效了；plan_checkpoint 自动激活链
>   把新 Reviewer 从派发到 submit 的手工命令从 12-15 降到 3 个；冷启动 E2E 端到端走通到 sealed batch + TR-008 自动提交 + S8。
> - **本轮新发现（修复引入/未覆盖的回归）**：
>   - **R1（P0 回归，Reviewer）**：`cloneBundledActivationExample` 从**磁盘**读 `agent-message.examples.json`，但 schema 资产本应 go:embed；
>     沙箱/目标项目里 agent-begin 必然失败且中途停在 understanding_submitted。**已修**：改读 `schema.ReadAsset`。
>   - **R2（P0 回归，Reviewer）**：链中途失败后 agent 停在 understanding_submitted，重试 agent-begin 被"幂等"短路为
>     "nothing to do"——链**永远无法恢复**。**已修**：识别当前状态从匹配步骤续推；带 TestAgentBeginResumesMidChainAfterFailure 回归测试。
>   - **R3（文案残留，恢复+复杂度）**：recovery 包 `s7RecoveryNextAction` 在 observation_sealed/clean 两个分支仍说"手动跑
>     `runtime transition --id TR-xxx`"，与上一轮 run.go 四处修复不统一。**已修**：所有 recovery/status/next 文案统一为"下一次 PreToolUse
>     自动提交，不要手调 CLI"。
>   - **R4（恢复包矛盾）**：当 planStatus 是 cannot_clean / discovery_draining 但 observation_batch 指针缺失时，recovery 包
>     出现自相矛盾（"ObservationBatch is open" + "not yet opened"）。**已修**：检测指针缺失时给出诊断指引而不是占位行。
>   - **R5（确定性问题，Planner）**：ready 在 verification.running 阶段非确定——连跑 5 次在两个 pause 门之间跳，
>     Go map 迭代顺序泄漏到用户界面。**已修**：`projectZeroSelected` 按 candidate ID 排序后选。
>   - **R6（真缺陷，对抗）**：`runtime register-workgroup --root <r>` 在 `--root` 不等于 cwd 时报"stale runtime revision"——
>     `resolveExpectedRevision` 用 `resolveRootPath` 而 `assignment.Register` 不走同一根解析。绕路：必须显式 `--state` + `--journal`。
>   - **R7（误伤，对抗）**：TeammateIdle/SubagentStop 在 lifecycle 不是 verification.running 时（如 bug_resolution），
>     controller 路径先投影为 allow，stop-idle 控制门 fail-open。S8/S10 阶段 reviewer 漏发 PLAN_REPORT 时平台不再真留住他。
>   - **R8（指引弱，对抗）**：`stale runtime revision` 错误缺 next-action；`verdict=fail`（非法枚举）schema 拒但不引导到
>     `verdict=finding + findings[]`；supplement 判别门缺哪一字段不分项报错。
>   - **R9（文档/schema 矛盾，Planner/Reviewer）**：sandbox recipe 推荐"populate baseline.{unit_test_status,
>     integration_test_status, build_status, integration_checkpoint_verified}" 但 loop-state schema 是
>     `additionalProperties: false`——键直接被拒。同时 `agents/*.md` 教 Worker 在 PLAN_REPORT JSON 体里写 `plan_ref=<path>`，
>     schema `additionalProperties: false` 也拒；`plan_ref` 应是 SendMessage 的 tool_input 参数。**已部分修**：把 plan.json 改称
>     plan-report.json 并在各 agent def 中澄清字段所在层；sandbox recipe 中的 baseline 错误键删除并替换为说明。
> - **复杂度终判（多视角一致）**：38→22 个必须主动理解的概念；3 个手工命令 / 轮；16 个机制每个对应 §2.1 具名失效模式；5 条削减建议
>   全部是披露/命名级，"不需要新机制"再被独立确认。**净复杂度持平**（操作简、词汇重、无镀金）不变。
> - **仍待修复**：R6（CLI 路径解析）、R7（controller 抢停 stop-idle 门）、R8（错误信息 next-action 加强）、
>   capture exec 终端 pass-through 秘密泄漏、RegisterPlan 失败遗留目录、命令面拆分、真实平台 doctor。
>
> **2026-08-24 审计（复杂度审查第二轮：冷读引导链 + 沙箱 round-2 pause 链代入）**：
> 方法 = 一个冷读 sub-agent 走完整引导链（manual/protocol/skill/agents/templates），同时沙箱重置 round-2
> 实际驱动 dispatch→auto-chain→pause verdict→TR-010 全链并逐条记录 agent 可见输出。
> - **冷读致命项核实（3 条全部为虚假报警）**：`scenario-model-design` skill 存在（子代理目录列表过期）；
>   "RESULT/QA/E2E 三模板同称 Canonical 投影无选用规则"实为单一 RESULT-template + 三张口径一致的 agent 卡；
>   e2e-tester 写面矛盾已在第一轮修复（cold_start 只写 verification_artifact_workspace）。
> - **确认并修复（5）**：
>   ① **manual 缺 S7 动词**（HIGH，冷读首要卡点）：恢复手册对 review-plan/review-result/capture/s7/register-workgroup
>   零条目，而 CLI usage 页脚恰恰把用户指向它。manual 生成器 Controller recovery protocol 增第 7 条（S7 动词总目 +
>   机器出口自动提交声明 + `s7 status` 板），两份 manual 重生成；
>   ② **subject_digest 不可计算**（HIGH，沙箱实证）：submit 强校验该值但 agent 无可见来源（算法只在 schema 描述里）。
>   `s7 status` 增 subject_digest 行，submit mismatch 错误与 RESULT-template 同步指向它；
>   ③ **孤儿 artifact 死路**（HIGH，沙箱实证）：Apply 期拒绝（reviewer 非 working）后 staged bytes 被保守保留，
>   纠正重交被 "file exists" 挡死且无恢复指引。CAS 失败后重读 state——revision 未变即未提交，清理 staged 文件；
>   writeArtifact 的 ErrExist 分支给出删除/换 id 指引；回归测试覆盖"拒绝→清理→纠正→同 result_id 重交成功"全链；
>   ④ **PLAN_REPORT 形状不可发现**（HIGH，沙箱实证）：合法 envelope 需 20 个必填字段（base 12 + plan 8，
>   steps/assertion_checks 是对象），skill 只列内容字段——冷写者首交必败（本审查作者代入手写也第一遍失败）。
>   agent-dispatch skill 增完整最小范例 + examples 文件指针，并澄清 plan_ref 是 SendMessage 参数不在 JSON 体内；
>   ⑤ **QA-template §5 S8 残留**（MEDIUM）：首轮报告无 BUG 可填造成困惑，标注 repair-rounds-only。
> - **平台边界如实声明（未修，设计权衡）**：首写屏障 `assignment_write_before_plan` 在 PreToolUse 只解析顶层
>   agent_id/teammate_name——当前平台 payload 两者皆无，屏障在生产休眠（WS3 方案的"同上三级识别"未接入 PreToolUse：
>   sole-reading 兜底若用于 PreToolUse 会把主会话写误归给唯一 reading agent，与"主会话豁免"矛盾）。安全底线不受影响——
>   `reviewer_product_write` 不依赖身份，verification 阶段对所有人硬拒（沙箱实证）。真实接线仍等平台 payload 官方身份
>   字段（13.A-5）。
> - **沙箱 round-2 实证链**：s7 status（含新 digest 行）→ manifest-draft（TODO(planner) 标注 + 8 条 note）→
>   register-workgroup → 故意首交失败（fail-claim-no-finding，拒绝信息点明规则）→ reading 态拒绝（含精确
>   agent-begin 恢复命令）→ 合法 PLAN_REPORT 观察 auto-chain 至 working → pause verdict 提交（checkpoint 事务内建 +
>   "route via TR-010" 指引）→ PreToolUse 自动提交 TR-010 → paused。capture 合并与 TR-008/TR-009 出口由既有确定性
>   测试覆盖（TestSubmitResultMergesCaptureBufferIntoEmptyTimeline、TestCT03913* 系）。
> - **错误信息抽检**：submit 六类拒绝（schema/round/generation/assignment_revision/digest/fail-claim）全部
>   "缺什么 + 精确下一动作"合规；agent-message oneOf 剪枝在 planReport 分支实测生效（closest-branch + 缺字段清单）。
> - **验证**：go test ./... 全绿、validate --all 通过、doctor 通过（manual current）。命令面拆分（review-plan
>   register/revise、register-workgroup S6 词汇）维持 13.A 待做不动。
>
> **2026-08-24 审计（复杂度审查第三轮：完整冷启动全链代入 + 冷读链复核）**：
> 方法 = 冷读 sub-agent 复核上轮修复后的完整引导链，同时两个全新沙盒从"TR-006 刚提交"的入口态完整驱动：
> r3 finding 轮（入口→draft→注册→双 workgroup→auto-chain→capture→finding→drain→seal→TR-008→S8 入口包）与
> r4 clean 轮（全 pass→机器 CleanRound→TR-009→acceptance）。加上 round-2 的 pause→TR-010，S7 四类出口全部沙盒实证。
> - **引导链结论（改善显著）**：SessionStart 入口包把第一动作精确到命令（`s7 draft --out plan.json` → 填 TODO oracle →
>   `runtime review-plan --file`）并附 S7 三桶投影（running/queued/blocked/unconsumed）；`s7 status` 是全程单点看板；
>   submit 的各拒绝信息全部"缺什么+精确下一动作"。注册期构造错误（task 枚举、milestone 必填、顶层多余键）逐条点名。
> - **修复（9 项）**：
>   ① **`--captures` 目录/文件不匹配静默失效**（真缺陷，沙盒实测撞到）：flag 帮助写 "dir" 而实现读文件——传目录
>   os.ReadFile 静默失败、零步骤并入、finding timeline 无声丢失。CLI 现归一目录→steps.jsonl + 帮助文案更新 +
>   空缓冲提示；新增 CLI 级回归测试（目录传参→merge 生效）；
>   ② **`capture step` 成功输出补 buffer 路径**（原先 agent 到 submit 时不知道 steps.jsonl 在哪）；
>   ③ **TR-008 篡改检测缺口**（沙盒实测：删除已索引 finding 文件后 seal 与 TR-008 照常通过）：batch gate 现对每个
>   batch finding 复验证据行存在 + 文件可读 + sha256 匹配（unindexed / unreadable / hash_mismatch 三个 missing
>   token）；req039 fixture 同步补 finding 索引行（真实 submit 事务本就会写）；
>   ④ 三张 reviewer 卡 Output Contract 补 subject_digest 来源（`s7 status` 复制；冷读确认卡内原先无此指引）；
>   ⑤ protocol #s7 dispatch 步补 `s7 manifest-draft`（冷读报告的"幻影动词"实为 protocol 漏引）；
>   ⑥ README §9"Two-Phase Agent Activation"整节重写为三模式（plan_checkpoint 默认 + auto-chain；两轮时序图保留为
>   plan_approval_required 例外）；prelude.md 激活段同步改写；
>   ⑦ agent-dispatch skill 补 SendMessage 包装示例（plan_ref 是 tool_input 参数——只给 JSON 体时冷读代理也判"易漏"）；
>   ⑧ manual item 7 补 `runtime agent-begin` 兜底动词 + submit flag 顺序与 protocol 统一（--assignment-id 在前）；
>   ⑨ journal cursor 错误补分方向指引（崩溃窗口→`runtime reconcile` 可回放；截断/手改→人工对齐）；
>   writeArtifact 的 ErrExist 文案泛化（不再在 plan 注册语境说 result_id/finding_id）。
> - **冷读卡点核实（3 条澄清非缺陷）**：AGENTS.md 在源仓库不存在但模板项目安装后存在（prelude 安装流复制）；
>   "QA-template 与 schema 不匹配"实为 markdown 投影 vs JSON 权威的关系（模板头已补权威指针行）；
>   "E2E 模板缺失"实为 RESULT-template 三 lens 通用（e2e 卡已补指针）。
> - **过程教训**：沙盒二进制必须每轮重编——本轮陈旧二进制制造了一次假"孤儿 artifact 复现"（新二进制 + Go 探针
>   双重确认 RegisterPlan 的失败清理在当前代码正确）；冷读 sub-agent 的目录列表可能过期（round-2 的
>   scenario-model-design 误报同源），其 FATAL 声明须逐条对照仓库核实。
> - **验证**：go test ./... 全绿（含新回归：captures 目录归一、batch 篡改检测）、validate --all、doctor（manual current）。
>
> **2026-08-24 审计（复杂度审查第四轮：E2E cold-start / revise / P0 三条未测路径代入）**：
> 方法 = 冷读 sub-agent 以 E2E Tester 视角走引导链 + 沙盒 r5 从 cold_start 注册起完整驱动：
> 注册（workspace 创建）→ **revise 一次成功/二次拒绝**（文案点名规则）→ **behavior-wave 门**（static 未结算时
> 注册被拒并解释 L3-S7 §5.2；结算后解锁）→ 三 workgroup + 双 PLAN_REPORT auto-chain → workspace 写 spec →
> `s7 workspace-digest` → 错 digest 提交 → 正确 digest **P0 finding** → immediate_stop seal → TR-008 自动提交 → S8。
> - **修复（6 项）**：
>   ① **digest 不匹配废轮死锁**（真缺陷，沙盒实测）：E2E 结果绑错 `verification_artifact_digest`（一次手误/晚存
>   一次 fixture 即可触发）走了 frozen-subject 漂移同款 `staleReviewPlanAfterDrift`——整轮连同已消费的 static 结果
>   全部作废，而错误信息却说"重跑后再交"，信息与后果矛盾。真实漂移威胁由 seal 时 `verifySealedArtifactDigests`
>   独立兜底；submit 时 mismatch 改为纯拒绝 + 恢复指引（重算 digest、spec 变了就重跑、再交），既有测试只断言
>   "拒绝"故不受影响；
>   ② **`s7 workspace-digest` 在 E2E 引导面零指引**（冷读卡点）：动词存在（第一轮已建、submit 错误会指向它）但
>   e2e 卡、e2e-browser-testing skill、manual item 7 全都没提。三处补齐；RESULT-template 同步；
>   ③ **P0 `capture_gaps` 要求未写进 E2E 面**：P0 无 capture_gaps 会被 submit 拒（单测既有），但 E2E 卡/skill/模板
>   都没说。三处补齐（"P0 stop-the-line：必须填 capture_gaps"）；
>   ④ **E2E-template.md 实际存在**（第三轮冷读"无 E2E 模板"系目录列表不全的误报，本轮我险些覆盖它）：其 §7 已含
>   七字段记账。但 §9 教 E2E 手写 BUG 草稿与机器自动建 BUG（TR-008 每 Finding 一份）矛盾，且缺 digest/capture_gaps
>   指引——已改写为"机器负责 BUG 草稿，你负责 investigation-ready Finding + 幂等重跑指令"；并把该模板纳入
>   migration 检查清单（required: 七字段、workspace-digest、capture_gaps）；
>   ⑤ e2e 卡七字段记账与 finding schema 槽位不对齐（schema 无 recovery/rejection/expected_state 槽）：卡与模板
>   补映射说明（写入 observed/timeline/terminal_state/side_effects/visible_impact 且可读）；
>   ⑥ protocol 补 capture step 与 inline timeline 的分工（观察中录 capture step——输出带 buffer 路径；事后编写才
>   inline；`--captures` 只并入空 timeline，接目录或文件均可）。
> - **en route 记录**：manifest skill_refs 需裸名（错误信息可懂）；assignment 结构（non_overlap_boundary 必填、
>   dependencies 在 claim 级）——`s7 manifest-draft` 生成器天然正确，手写者由 schema 错误兜底；P0-with-remaining-
>   claims 形态由 submit matrix 单测覆盖（本轮 r5 static 已结算故 unobserved 为空）。
> - **冷读误报澄清**：SendMessage 示例形状（teammate_name/message_type/plan_ref in tool_input）与观察者解析契约一致，
>   保留；workspace 相对路径疑虑由注册错误文案兜底（"must live under e2e-workspace/"）。
>   **`s7 status` blocked 看板补救与扳机提示**：原看板只报 `status=blocked`，
>   reviewer 必须 grep state 找 blocker 文件；现在单行打印 `blocker_ref=<path>` 并把
>   `runtime agent-event --event blocker_resolved --agent-id <id> --message <file>`
>   当成下一动作明示。CLI 单测覆盖 round 2 of 5 + blocked 看板两行输出。
>
> **2026-08-23 审计（验证轮后续：四工作包修复 R6/R7/R8/R10）**：
> - **R6（register-workgroup `--root` 路径不一致）已修**：所有走 `runtime.NewStore`/`runtime.NewWriter` 的入口在 `resolveExpectedRevision`
>   与 verb 注册/写入之间统一 `resolveRootPath(root, statePath)`；同步修复同类 bug 的 `runtime fingerprint` 与 `runtime reconcile-policy-ref`，
>   全文审计 `internal/cli/run.go` 的其他 17 个 verb 均已正确。回归测试 `TestRuntimeRegisterWorkgroupCommandAnchorsAgainstRoot`
>   两个子测试覆盖 cwd ≠ root + 显式相对路径 + 默认路径三种场景。
> - **R7（lifecycle ≠ verification.running 时 stop-idle 门被 controller 抢拍）已修**：evaluate 顺序改为"controller 未真 block 时永远跑
>   StopIdleDecision"——controller 在 bug_resolution/acceptance/paused 阶段给的不是真控制而是通用 guidance，不应顶替真实平台控制。
>   4 条回归测试覆盖各阶段下 TeammateIdle/SubagentStop；2 条保留测试确认 plan_approval_required/one_shot 仍 fail-open。
> - **R8（三处错误信息 next-action 加强）已修**：
>   - `stale runtime revision` 从单行扩为带 `loop-harness status --root <r>` + `--expected-revision <N>` + `runtime reconcile` 的完整指引
>     （通过 `formatFailure` 识别 `ErrStaleRevision` 并加 next-action 尾注）
>   - `verdict=fail` 在 submit 入口短路 hint 引导到 `verdict=finding + findings[]`，schema description 同步引导
>   - finding-supplement 判别门从单条覆盖三类缺失改为按字段逐项列出（单缺只显示该字段）
> - **R10（capture exec 秘密 pass-through 到终端）已修**：双闸门硬性替换——终端输出与证据文件共享同一 `review.SanitizeCapture` 闸门，
>   头部 4 KiB 缓冲 + 模式命中则终端也写 `[withheld]` 占位符。3 条回归测试覆盖 secret 泄漏、正常透传、大输出+exit code。
> - **跨批次依赖**：上一轮 R4 修改（recovery 包不能_clean 时不再说"not yet opened"）使批次 1 的 `TestBuildGuidanceS7RecoveryClaimsNoBatchOpen`
>   测试断言过时——已同步更新为断言新逻辑（drain invariant + missing-pointer diagnostic + "not yet opened" 必须不出现）。
> - **当时仍未做、现已分流**：命令面拆分（review-plan register/revise、register-workgroup 的 S6 词汇）仍是低优先兼容迁移；RegisterPlan 失败遗留目录随后已由 workspace/plan 精确清理修复并有回归测试；本轮新增确认的 revise pending-commit artifact 风险与 TR-012 修复基线绑定已完成实现并由回归测试锁定。

### 13.1 当前事实（2026-08-25）

S7 的主闭环已经落到当前实现；本表只记录仍影响行为的差距，不再把早期迁移计划误报成未实现能力：

| 当前位置 | 如实现状 | 与目标的差距 |
|:--|:--|:--|
| ReviewPlan / phase projection | 已使用 `planned/running/cannot_clean/discovery_draining/observation_sealed/clean`，close-round 是 submit 内部事务 | `closing` 仅可作为内部日志语义，不能重新加入持久化状态 |
| Plan/Assignment | schema、binding 和 validator 已消费 `claim_ids[]/focus_keys[]/non_overlap_boundary`，并校验 exact Claim set、lens、wave、overlap 与 cold-start overload；`regression_available` 的 `e2e_assets` 会在注册/修订时校验 path、CASE 和 SHA-256；changed surface 必须同时进入 `coverage_inventory`、Claim `source_refs` 和 `frozen_subjects`；TR-012 已把当前 `change_impact.changed_artifacts` 逐项绑定到 round-2 的 `frozen_subjects`、`coverage_inventory` 和 Claim source_refs | 命令面仍沿用兼容入口；真实 Claude Code 2.1.218 平台 doctor 仍需目标环境实测 |
| Reviewer dispatch | `coverage_complete` 保留全部 required Assignment；资源冲突进入 queued，锁释放后同一 CAS 将 queued Agent 唤醒为 `reading`；NotebookEdit 与其他写工具共用路径边界 | 平台真实 Claude Code 2.1.218 doctor 仍需在目标环境实测 |
| DraftPlan / QA coverage | DraftPlan 生成六个 QA baseline focus，并拆成可独立派发的 Assignment；从当前模块 `cases.json` 读取 required browser CASE，按 CASE 拆分 E2E Assignment；literal `TODO(planner)` 在注册 gate 拒绝；DraftPlan 同时投影 changed-surface `coverage_inventory` | 没有可读 CASE inventory 时仍保留显式 TODO，必须先补 S2 场景包；不能用通用 Claim 假装已完成 |
| E2E workspace | workspace 仅允许 `cold_start`；`regression_available` 不得创建写面，也必须有 required E2E Claim；声明的 E2E asset 在注册/修订时校验 path containment、可读性和 SHA-256；E2E Result 与 result artifact 均保留 `verification_artifact_digest`；Planner 从 CASE→Playwright spec 文本映射生成最小 `e2e_assets`，任一 required CASE 缺映射即回退 `cold_start` | 产品侧浏览器/console/network wrapper、selector 与 environment 的更细粒度 fingerprint 仍由产品接入；当前自动投影不猜测这些字段 |
| Finding / evidence | Result submit 原子写入 immutable Finding/ObservationBatch；required evidence 采用 typed refs，`path:`/indexed evidence 会校验存在性与指纹；capture buffer 使用严格 JSONL 和多 Finding 的 `finding_id`/`claim_id` 关联，普通 Finding 继续要求 failure boundary | 浏览器/runner 的产品侧 console/network 注入 wrapper 仍由产品接入 |
| S7→S8 handoff | ObservationBatch 保留 exact Finding set、coverage、routes、readiness；`drained_assignment_ids[]` 包含触发 seal 的当前 Assignment，避免 S8 丢失最后一份 Result | S8 的 ingest/Case/hypothesis/result/route/Contract 主链与 Investigator Assignment lifecycle bridge 已可执行；单 Runtime 多 Case 协调和只读聚合仍不是现有权威，见 [L3-S8 当前事实](L3-S8-finding-investigation.md#13-当前实现差距与迁移清单) 与 [L4 跨 L3 消费地图](L4-runtime-control-plane.md#14-跨-l3-消费地图) |
| 工具必经路径 | plan_checkpoint 连续执行、idle/stop 控制、reviewer product-write hard deny、结构化 gate diagnostics、结果消费和机器 CleanRound 已由 Hook/submit/consumer 接管；`s7 status` 直接显示 round budget、blocked ref 和恢复动词 | 命令面拆分（review-plan register/revise、dispatch-assignment）和真实平台 doctor 仍是低复杂度后续项 |

### 13.1.A S7→S8→S9 接口审计（2026-08-26）

S7 的交付边界是“可调查的观察事实”，不是根因结论：

- `review-result submit` 负责把 exact Claims、typed evidence、Finding encounter、capture gaps、baseline 和最后一个已消费 Assignment 原子收口为 `review.observation_batch`；S8 不应依赖聊天摘要或重新跑用户旅程。
- `ObservationBatch` 是 S8 的唯一正常入口。S7 不创建 canonical BUG，也不把 targeted PASS、seed 或单个 Finding 当作可直接修复的授权。
- TR-012 生成的 S7 seed 已投影到 `review.plan`，但只是 registration staging：下一轮 Planner 仍需检查 changed artifacts、TASK coverage、E2E applicability、frozen subjects 和 risk，再走正常 ReviewPlan 注册/派发/消费路径。
- Runtime `revision` 是没有最大值的内部提交序号；ReviewPlan `revision` 是同一 round 的受控计划版本，当前最多一次且由工具生成；Case/Contract revision 和 artifact SHA 是另外三类身份，不能混用。

因此 S7 只对“发现是否完整、现场是否可调查、是否正确交给 S8”负责；共同根因、架构级修复意图由 S8 负责，实际写入、影响重算、定向复验和回环由 S9 负责。S7 的低优先残留仍是命令面兼容迁移、真实 Claude Code doctor 和产品侧浏览器/console/network wrapper，不应被伪装成 S7 业务闭环缺失。

### 13.1.B S7~S9 当前闭环复审（2026-08-27）

本轮按实际落地重新确认边界：S7 只把同一轮的 ReviewPlan、Claim disposition、Finding encounter、typed evidence、capture gaps 和 sealed `ObservationBatch` 交给 S8；S8 只在 immutable Case 中登记可证伪假设、证据结果、因果闭合和路由，approved `RepairContract` 才是 S9 授权；S9 只消费该 Contract，按 RepairAssignment 走 PlanReport → execution begin → exact Result → Impact → independent Targeted → Handoff，并把 seed 交回 S7。任何聊天摘要、BUG 兼容投影、targeted PASS 或 seed 都不能越级成为下一阶段事实源。

本轮实现核对还确认两项输入层修复：S8 多 Finding/多边界证据的重复 flags 不再静默丢失；缺失/非法的因果模型、影响面或检测缺口文件会在 CLI 边界指出具体路径和下一动作。它们没有新增状态、Case、scheduler 或额外审批轮，只把既有对象的输入契约埋回必经工具路径，复杂度收益为正。Runtime revision 继续无限递增并由 Writer 作为内部提交序号记录；ReviewPlan revision 的一次受控修改仍是另一条业务对象规则。

### 13.2 历史迁移批次（已落地项的原始记录）

下列 13.2～13.5 保留为设计演进记录，不是当前待办；实际剩余项以 13.1 和 13.A 为准。

**2026-08-25 审计（复杂度审查第七轮：阅读预算、概念密度与 revise 边界）**：

- 冷读量化：首次完整阅读约 1,200 行；后续单轮最低约 769 行，最大约 3,200 行；高混淆概念集中在 subject_digest、verification_artifact_digest、dispatch_capacity_policy、exact-set、encounter、drain_policy、blocked_by_confirmed_finding、non_overlap_boundary、site_lost、exact-set coverage gate。
- 复杂度结论：这些成本主要来自 progressive disclosure 的事实核对，不新增控制面；因此不以删减覆盖换取阅读量下降。
- revise 边界实驱发现：pending commit marker 存在时，r2 artifact 不能按“revision 未变”删除；安全清理必须在 Runtime lock 内检查 pending operation、当前 revision、状态引用和 artifact SHA。
- TR-012 边界实驱发现：文档要求 round-2 冻结修复后提交，但 schema 不能做磁盘语义校验；现由 round entry 记录 `change_impact` 引用，RegisterPlan 绑定 `changed_artifacts` 到 `frozen_subjects`、`coverage_inventory` 和 Claim source_refs。
- 验证：`go test ./...`、`validate --all`、`doctor` 通过；新增 pending artifact、TR-012 baseline 缺失/完整注册矩阵测试。

1. 定义 ReviewPlan/Claim/ReviewResult/Finding/FindingSupplement/ObservationBatch/CleanRound schema；ReviewPlan 只持有单一 required Claim set，静态/E2E/discovery 是计算视图；verification-artifact workspace 仅为 E2E cold start 可选能力，Finding 内含按 observation mode 判别的 encounter，不新增顶层 Failure Episode 状态机；
2. 实现 `runtime review-result submit`（`verification result` 为历史幽灵动词，已收口至 `runtime review-result submit`；`verification` 仅保留 `clean-round`）；
3. Result submit 从 capture buffer 固化 encounter、校验 investigation readiness，并原子登记 Finding、pause checkpoint 和路由事实；实现 cannot-clean、剩余 Claim drain 与 batch seal；冻结 subject、Pinned Plan 或 E2E workspace 漂移必须把 ReviewPlan 持久化为 `stale`，而不是只返回一次性错误；
4. plan validator 校验 required DV/QA focus 与 E2E persona/flow/surface → Claim → Assignment → oracle coverage，将静态 Claim 设为行为 E2E 顺序前置，并拒绝 Agent/token 上限裁剪 required coverage或无 source 的范围扩张；
5. clean evaluator 按当前 ReviewPlan exact set 核验 conclusion/producer/round/fingerprint；
6. Reviewer 产品写路径 hard deny；
7. 使用共享控制面和 CAS，覆盖 worktree 并发。

### 13.3 历史 P1：删除重复机制

1. S7 停用 two-phase activation，接入 L4 plan_checkpoint；
2. 停止要求人工 phase aggregate envelope；
3. 将 angle 的有效数据迁入 Claims，停用 angle dispatch guard；
4. 合并 clean-round evaluation/pass 重复动作；
5. 合并 pause 双载体；
6. QA/E2E/Delivery 模板改为 ReviewResult scaffold 或自动人读视图。

### 13.4 历史 P2：优化流动与派发

1. 实现 Claim 生成与 coverage 求差；
2. 支持 Assignment `claim_ids[]/focus_keys[]/non_overlap_boundary` 与 grouping rationale；
3. 生成 DV/QA focus graph 与 E2E persona/flow/surface coverage matrix，按可恢复上下文聚合为不设上限的 1..N Assignments；
4. 实现 E2E `cold_start/regression_available/not_applicable` inventory 和 spec/fixture/evidence 写面隔离；只有 cold start 才分离 frozen product digest 与 verification-artifact revision/digest；
5. DV/QA 并行，E2E preflight 提前，behavior E2E 等待静态 Claims disposition；ordinary finding 后仍继续；
6. 支持 priority、真实 dependencies、resource locks、platform-capacity queue，删除固定 WIP/Agent/token gate；
7. 结果消费后立即重新计算 Ready Set；只有存在 `source_ref + affected_surface` 时才执行一次 ReviewPlan revision，不实现无边界 fixed-point 递归；
8. 实现浏览器/测试/CLI/trace wrapper 的 timeline 和 failure-window 自动采集；
9. ordinary finding 后先 evidence freeze，再完成剩余 required Claims；只有 critical finding immediate-stop。

### 13.5 历史 P3：协议瘦身与观测

1. `agent-protocol.md` 只保留入口索引；
2. Agent Definitions 只保留稳定角色边界；
3. PostToolUse(SendMessage)、TaskUpdate、idle/stop、SessionStart/PreCompact 接入 L4；
4. 提供 ReviewPlan board/status 和唯一下一动作；
5. 删除不再消费的旧 schema、模板、CLI 和兼容状态。

每个迁移阶段必须明确“旧/新权威切换点”，禁止长期双写后让 Gate 自行猜测。

## 14. 系统测试与运营指标

### 14.1 必须有的系统测试

| 场景 | 期望 |
|:--|:--|
| ReviewPlan 漏掉 Closing Contract obligation | plan/validate 拒绝并指出缺失 source |
| 一个 QA Assignment 覆盖四个同模块 Claims | 允许；Result 必须逐 Claim 给结论 |
| 跨模块高风险实现把 design/logic/maintainability/tests 塞给一个 generic QA Agent | plan/validate 要求按 focus/context 拆分或证明单上下文可完成 |
| 小模块的多个 QA focus 共享 read set/方法 | 允许合并为一个 Assignment，不为凑 N 增加 Agent |
| 三个 QA Agents 都执行“全量代码 review” | overlap validator 拒绝，要求 focus/non-overlap/oracle 区分或合并 |
| ReviewPlan 固定只派一名 DV 和一名 QA，但 coverage matrix 有未覆盖 focus | plan/validate 拒绝，指出 exact focus/Claim 缺口 |
| 有产品实现变化却 DV 或 QA 为 N=0 | plan/validate 拒绝；纯文档例外需 impact/source 证明 |
| E2E `cold_start`，多个 persona/入口/状态/负向路径却只生成一个全需求 Assignment | overload validator 拒绝；先形成 coverage matrix，再拆成足够多的 bounded flow Assignments |
| E2E `regression_available` | 只复用 fingerprint 有效的 spec/evidence；changed/invalidated/gap surfaces 继续生成 1..N Assignments |
| 配置 `max_reviewers=3`、`wip=2` 或 token budget 导致 required Claim 未派发 | plan/dispatch validator 拒绝裁剪；平台容量只能把 Assignment 标 queued，不能删除 coverage |
| Main 临时把 S7 capacity policy 改成 `bounded_flow` 以节省成本 | revision/schema gate 拒绝；S7 policy 固定为 `coverage_complete`，策略降级必须修改权威设计并经人工决策 |
| 20 个互相独立且无资源冲突的 ready Assignments，平台只有 6 个并发槽 | 6 个 running、其余 queued 并持续补位；所有 20 个仍在 required Claim set |
| 多个 E2E flows 共用账号/端口/spec 文件 | 使用 resource lock、隔离数据或 worktree 排队/并行；不得合并成一个过载 Agent 或跳过 flow |
| cold-start E2E Agent 在授权 verification workspace 新建 spec/fixture | 只增加 verification-artifact revision，不使 frozen product baseline stale；Result 绑定实际 digest |
| 已 consumed E2E Result 对应 spec 后续被修改 | 仅该 Result/依赖 Claims invalid 并重跑；若改到产品测试、CASE/PATH 或 locked 规格则整轮 stale |
| 简单直接实现没有命名设计模式 | 不因 pattern 数为 0 失败；QA 根据变化压力、惯用法和维护风险判断 |
| 多类型分支持续扩张却未采用已有 Strategy/handler registry 惯用法 | QA 形成 code-inspection Finding，给出结构、质量不变量和变更风险 evidence |
| 简单 CRUD 被多层 Factory/interface 包装 | QA 可形成 over-engineering Finding，不把“用了模式”自动当优点 |
| QA 只提交 lint/coverage/test-pass | Result 不满足 design/boundary、logic、maintainability 等 required Claims |
| 合并不同 role Lens | Assignment validator 拒绝 |
| Reviewer 与原 Builder 相同 | independence gate 拒绝派发/消费 |
| PLAN_REPORT 前首次 evidence/spec mutation | PreToolUse 阻止并指导发送计划后继续 |
| PLAN_REPORT 对齐 | Worker 连续执行，Main 不发送批准消息 |
| Reviewer 尝试写产品代码 | PreToolUse hard deny，给授权 evidence 路径 |
| Result 漏 Claim、错 revision、错 digest | submit 原子失败，无部分状态 |
| Result 有 finding | Result 与 immutable Finding 同事务写入；ObservationBatch seal 后，TR-008 handoff 携带 Finding exact set |
| 首个普通 static finding 出现 | 先冻结 inspection encounter 并标 cannot-clean；完成剩余静态 Claims 后仍执行可安全的 E2E Claims，最终统一 seal |
| Claim 有 blocked/unassigned required disposition | 留在 S7 BLOCKER，不 seal、不记 unobserved 绕过 |
| confirmed build/start/entry Finding 客观阻断后续 E2E | 受影响 Claims 可计算 `blocked_by_confirmed_finding`，但必须绑定 Finding/precondition/evidence 且 `after_repair_required=true`；不得当 PASS |
| 仅因 token、时间、Agent 不足把 E2E Claim 标 `blocked_by_confirmed_finding` | validator 拒绝；没有产品 Finding 与不可执行证据就保持 queued/blocked in S7 |
| 普通运行型 finding 出现 | 先冻结 encounter 并标 cannot-clean；其他预规划 flow 继续，只有新证据指向具体 affected surface 时进行一次 Claim revision |
| P0/security/data-destructive finding | 不继续危险操作；保留已产生证据、记录 safety capture gaps，立即 seal ObservationBatch 并 stop-the-line |
| 两个表象疑似同根 | S7 保留两个 Finding，不合并、不写 root cause；S8 接收 exact set |
| user-flow Finding 只有“保存失败” | submit 拒绝，要求 journey summary、last-good/wall/first-bad、terminal state 和 step-bound evidence |
| code-inspection Finding | 接受 inspection/call/data-flow trail，不要求伪造 UI steps |
| timeline step 无 evidence ref | 普通 Finding 不得 investigation-ready；指出需冻结的 checkpoint |
| 普通 Finding 关键现场已丢失 | 复用 Assignment BLOCKER 留在 S7，不新增 phase、不 seal、不把复现债务交给 S8 |
| 自动 capture 含 token/password/非必要 PII | redaction gate 拒绝持久化敏感正文，只保留脱敏 ref/hash |
| 间歇 Finding 只发生一次但有 deterministic trace | 允许 `once_with_deterministic_trace`，不要求为了次数重复危险操作 |
| Finding 缺 expected/encounter/evidence/baseline | result submit 拒绝，指出缺失字段 |
| S8 请求重新复现已确认症状 | 默认拒绝；只有明确 discriminator 的受控 follow-up observation 才追加 FindingSupplement |
| S8 请求判别观察 | 追加 FindingSupplement，不覆盖原 Finding/encounter |
| 两个 Reviewer 报同一症状 | 保留两个 source Findings；S8 通过 Case/duplicate disposition 判断关系，不在 S7 自动去重或丢证据 |
| req/release verdict | 单个权威 checkpoint 创建并进入 paused |
| behavior E2E 在静态 Claims 未完成前启动 | DAG/gate 阻止；E2E preflight 仍可并行 |
| DV/QA ordinary finding 后静态 Claims 已完整 disposition | 解锁 1..N E2E Assignments；cannot_clean 不得被误作“停止发现” |
| 首个 E2E flow 报 finding | 该 flow 冻结现场；其余安全 required E2E flows 继续，不因节省 token 提前 seal |
| Result 暴露原 impact 未包含的具体消费者/旅程 | 进行一次带 source 的 ReviewPlan revision，增加 Claims/Assignments；最终 Claim set 完成后才能 seal |
| ordinary ObservationBatch 仍有 `unobserved_claim_ids[]` | seal validator 拒绝；仅 critical immediate-stop 可携 safety gaps |
| Delivery/QA 并行 | 两者绑定同一 frozen digest，互不等待假 phase |
| E2E 不适用 | 必须有 impact/source/rationale，缺任一项不得 close |
| valid evidence 但 conclusion 非 pass | CleanRound 不通过 |
| 旧轮/无关 invalid evidence 存在 | 不污染当前 exact set |
| 当前 consumed PASS 被 invalid | 当前轮 stale，不生成 CleanRound |
| closed blocking BUG 无 targeted reverify | CleanRound 不通过 |
| targeted reverify 通过但 full Claims 未跑 | CleanRound 不通过 |
| 最后一份 Result 被消费 | 原子生成一份 CleanRound 并进入 S10，不重复记录 |
| worktree Worker 提交计划/Result | Main 与 Worker 读取同一共享控制面，无第二份运行态 |
| Session 恢复 | status 显示 in-flight、未消费 Result、Ready Set 和唯一下一步 |

### 14.2 运营指标

| 指标 | 目的 |
|:--|:--|
| S7 lead time / touch time / wait time | 判断串行等待和工具摩擦 |
| overloaded Assignment rate | 单 Assignment 跨过多独立 focus/flow/context/write surfaces 的比例，目标趋近 0；不以少 Agent 为优 |
| DV/QA/E2E coverage completeness | required focus/persona/flow/surface → Claim → Assignment → oracle 完整率，目标 100% |
| E2E cold-start matrix completeness | 首次覆盖空白时 persona/entry/positive/negative/state/side-effect/recovery/environment 的映射完整率，目标 100% |
| E2E cold-start generic-agent rate | cold start 被压给一个全需求 Agent 的比例，目标 0 |
| required Assignment queue retention | 平台并发不足时 required Assignments 仍保留为 running/queued 的比例，目标 100% |
| duplicate review overlap rate | 多 Assignment 高度重复 read set + oracle 的比例，目标趋近 0 |
| Claim lead time by mode | 按 `static / e2e / discovery` 过滤视图衡量依赖、平台容量、resource lock 和环境准备瓶颈；不反向设置 Agent/token 上限 |
| code-visible defect escape to E2E | E2E Finding 后确认本可由 DV/QA 静态发现的比例，核心目标趋近 0 |
| unobserved required Claims on ordinary batch | 目标 0；任何非 critical 缺口都说明 S7 提前交卷 |
| static findings per repair batch | 观察是否一次暴露相关技术债，不作为 Reviewer 数量 KPI |
| pattern-fit Finding quality | 含 context/observed structure/invariant/risk/evidence 的比例，目标 100%；不统计“使用模式数量” |
| PLAN_REPORT drift rate | 判断 Planner/Prompt 是否清晰 |
| mechanical plan rejection rate | 判断 scope/check/dependency 生成质量 |
| manual aggregate/wrapper count | 目标为 0 |
| Wave A finding rate | 判断 cheap-blocker 策略是否有效 |
| ordinary finding 后剩余 Claim completion | 普通 Finding 后最终 required Claim set 的完成率，目标 100% |
| E2E environment blocked time | 推动环境 preflight 和可恢复性 |
| Result submit first-pass success rate | 衡量 schema/脚手架是否顺滑 |
| ObservationBatch handoff completeness | Finding exact set/hash/baseline 必须 100%，不能只有 cursor 或单条 generic envelope |
| ObservationBatch information completeness | expected/observed/encounter/failure-boundary/evidence/baseline/finder 完整率必须 100%，高危 safety gaps 显式计数 |
| automatic encounter capture coverage | 运行型 Finding 中 timeline/时间戳/correlation/state evidence 自动生成比例，目标持续上升 |
| step-to-evidence binding completeness | material steps 和 last-good/wall/first-bad 的 evidence 绑定率，目标 100% |
| S8 symptom re-reproduction rate | S8 为确认同一症状而重跑的比例，应趋近 0；判别假设的 follow-up 不计入 |
| manual Failure Episode report count | 目标为 0，防止新增第二份人工报告和状态权威 |
| first finding → final Claim set → batch seal time | 观察完整发现成本与瓶颈；不得以压缩该时间为由跳过 required Claims |
| findings per diagnosed root cause | 观察聚类价值，不作为 finding 数量 KPI |
| false clean / false stale | 核心正确性指标，目标为 0 |
| baseline drift restart rate | 判断 S6/S7 边界和并发写控制 |
| idle/stop without Result rate | 判断连续执行协议是否生效 |

指标用于发现机制摩擦、过载和缺陷逃逸，不把 Reviewer 数、token 总量或“尽快 seal”设成优化目标。token 可以观测以做容量规划，但不能成为 S7 coverage gate；应优化的是重复劳动和等待，不是必要验证。

## 15. Definition of Done

S7 目标机制只有在以下条件全部成立时才算落地：

- ReviewPlan 是本轮 coverage、Claims、适用性和调度 DAG 的唯一权威；
- ReviewPlan 计算 required DV/QA focus、E2E persona/flow/surface coverage matrix，以及单一 required Claim set；Static、E2E、Discovery 只作为 coverage view；
- DV、QA、E2E 每个 Lens 都可按 focus/flow/context 生成 1..N Assignments，且无 Reviewer 数量、固定 WIP 或 token budget 上限；Assignment 支持同 Lens 多 Claims，并保留 necessary separation/non-overlap；
- ReviewPlan 固定声明 L4 `dispatch_capacity_policy=coverage_complete`；required work 可 queued，但不能因 capacity policy 被裁剪；
- E2E `cold_start/regression_available/not_applicable` 可确定性计算；cold start 必须分解 coverage 和 spec/fixture/evidence 写面，不能默认为一个 Agent；
- 只有 E2E cold start 启用 verification-artifact revision/digest，并与 frozen product baseline 分离；已有验证资产不创建额外控制面，cold-start authoring 不自废整轮，已消费 Result 的 artifact 变化可精确失效和重跑；
- QA baseline 覆盖 design/boundary、pattern/idiom fit、logic/state/error、maintainability/readability、testability/oracle 和 debt/operability；
- pattern-fit 同时识别模式缺失、错配和过度工程，不使用模式数量/关键词作为 gate；
- 默认 PLAN_REPORT 后连续执行，不再依赖两轮开工许可；
- PostToolUse 能捕获计划，首次 mutation 能机械检查计划；
- Reviewer 产品写域有真实 hard deny；
- 每个 Assignment 只提交一份 Canonical ReviewResult；
- phase/lens/round PASS 全部由 Result 计算，无人工 aggregate；
- finding、pause 和 Result 在同一事务闭合，不丢 canonical entity；
- 首个 finding 后能够区分 critical stop-the-line 与 ordinary complete-required-claims，并生成 immutable ObservationBatch；
- behavior E2E 在静态 Claims 完成前不能启动；普通 static finding 后仍完成剩余 E2E Claims，普通运行型 finding 后也继续其他安全 required flows；
- 每次 Result 后只允许一次基于具体 `source_ref + affected_surface` 的 Claim revision；最终 required Claim set 完成 exact disposition 后 ordinary seal；
- ordinary ObservationBatch 的 `unobserved_claim_ids[]` 必须为空；平台容量不足只能排队，不能裁 coverage；
- `blocked_by_confirmed_finding` 只是工具从 `blocked` Claim disposition 投影出的结果，必须绑定证据并在修复后新轮恢复执行；
- 普通 Finding 在 complete-required-claims drain 继续前完成 evidence freeze，并记录短 journey、last-good/wall/first-bad、终态、状态差异和 step-bound evidence；
- observation mode 能覆盖 user/API/command/state/code-inspection，而不要求不适用的伪动线；
- 高危 Finding 不为补字段重复危险动作，但 capture gaps 和停止理由完整；
- S7 Finding 不包含权威 root cause/repair suggestion，所有原始表象与现场无损交给 S8；
- S8 可在不重新复现症状的情况下开始竞争假设和因果调查；
- encounter 是 Finding 内嵌事实，raw trace 走 typed evidence，不新增独立人工报告/状态机；
- CleanRound 按 exact current set 计算并只生成一次；
- Delivery、QA 和 E2E preflight 可并行；行为 E2E 服从 static dependency 后按完整 coverage 调度，不按 token 成本裁剪；
- baseline drift 可确定性使当前轮 stale 并创建新轮；
- `agent-protocol.md` 不再承载长篇 S7 操作说明；
- §14.1 系统测试通过，false clean/实体丢失/越权写为 0。

## 16. 易错点与渐进披露

### 16.1 易错点

1. 把“删除 angle 生命周期”误解成不需要具体审查焦点；具体 target/oracle 必须进入 Claim；
2. 把 Delivery、QA、E2E 三个 Lens 固定映射成三个 Agent；每个 Lens 都应按真实 focus/flow/context 形成 1..N Assignments；
3. 反过来给每个 checklist 点派 Agent；应先生成细 Claims，再按紧密 read set、方法、Skills 和可恢复上下文聚类；
4. 让多个 Reviewer 都做“全量代码/E2E review”；必须写 focus/flow、non-overlap boundary 和独立 oracle；
5. 用 `WIP=2`、`max_reviewers` 或 token budget 裁掉 required Claims；平台容量只能排队，不能降低验证完整度；
6. E2E cold start 仍只派一个 Agent，让其独自决定覆盖、写全部 spec、准备数据、跑旅程和取证；必须先展开 matrix 再拆 Assignment；
7. 为降低 token 总量合并认知过载的 Assignment，或把“Agent 少”当 KPI；应只消除真实重复劳动；
8. 把“用了设计模式”当质量证明，或把“没有命名模式”当缺陷；QA 判断的是 pattern fit 和维护风险；
9. QA 只看 lint、coverage 和测试是否绿，忽略逻辑自洽、依赖方向、状态/error ownership、可读性和技术债；
10. 等 E2E 暴露行为问题后才检查代码结构；code-visible defects 应在静态 Claims 阶段尽量一次找完；
11. 第一个普通 static finding 后停止后续 E2E，或第一个运行型 finding 后只收在途任务；ordinary finding 只关闭 clean path，剩余 required Claims 仍要完成；
12. 把初始 ReviewPlan 当成可以无限扩张的 scope；只有带 source_ref 的具体 affected surface 才允许一次 Claim revision；
13. Delivery/QA/E2E 并行观察时使用不同代码版本；所有 Result 必须绑定同一 frozen digest；
14. 为追求并行让 E2E Agents 争用同一账号、数据、端口或 spec；应声明 resource lock、隔离数据/worktree 或确定性排队；
15. PLAN_REPORT 后等待 Main 回复；默认对齐即静默继续；
16. 把 ReviewResult final text 当成已消费；只有 submit + consumer 成功才闭合；
17. 继续手写 aggregate PASS 或 requested event；这些都是机器投影；
18. `ui_impact=none` 就自动跳过 E2E；必须结合 required surfaces 和用户可观察行为；
19. 把 `regression_available` 理解成旧 E2E 都有效；必须校验 CASE/PATH、入口、selector/contract 和 environment fingerprints；
20. E2E 环境失败后以 URL/API/DB 直调绕过；应形成 recoverable blocker 或 finding；
21. finding 后让 Reviewer 顺手修代码或在 S7 宣布根因；表象必须先经 ObservationBatch 进入 S8；
22. 把复现步骤当成本次实际 timeline；必须记录每一步真正观察到什么并绑定 evidence；
23. 只写最终报错，不标最后正常点、撞墙动作和首个异常点；S8 会失去因果边界；
24. 为“完整现场”新增一份手写 Failure Episode 报告；应内嵌 Finding 并引用自动 raw trace；
25. 为补齐次数或截图重复 P0/破坏性操作；高危场景应停线并显式记录 capture gaps；
26. 让 Harness 自动猜 root cause；自动化只采 timeline/refs，failure boundary 仍由 Reviewer 确认；
27. targeted reverify PASS 后直接进 S10；仍需新完整 ReviewPlan 和 full Claims；
28. 旧轮 evidence 仍在索引就让新轮失败，或用旧 ID 满足新轮；CleanRound 只读 exact current set；
29. 只更新本文件却不改 Hook/Schema/CLI，就宣称机制已强制；§13 明确列出实现差距。

30. 把 `PLAN_REPORT` 的 message_type 当成事实；PostToolUse 只有在 plan_ref/plan_path 指向当前 Assignment、通过 agent-message schema 校验且完成来源绑定时，才会写入 `plan_reported_ref`：S7 校验当前 ReviewPlan revision，S6/S8/S9 校验 fingerprinted workgroup manifest，非 S7 通用 checkpoint 的 `assignment_revision` 固定为 `1`；缺 ref 或错绑定只产生观察提示，不清除首写屏障。
31. 在运行时检查点不可读时让变更型 PreToolUse 继续执行；控制器无法确认写入边界时，Write/Edit/Bash 等变更型工具 fail-closed，恢复运行时后再重试。

### 16.2 阅读预算

- **理解 S7 主线**：读 §0、§1、§2、§5；
- **规划 ReviewPlan**：读 §3、§4；
- **派发 Reviewer**：读 §4.4～§4.5、§7，并对照 L4；
- **DV Reviewer**：读 Assignment focus/non-overlap、§6.1 和对应 authority/contracts；
- **QA Reviewer**：读 Assignment focus/non-overlap、§6.2 和项目语言/框架惯用法；不要加载全部无关专项；
- **E2E Reviewer**：读自己的 persona/flow pack、Claims/oracles、§6.3 和授权 spec/fixture/evidence 写面；无需加载其他独立 flow 的完整上下文；
- **编写或提交 Result/Finding**：读 §3.5～§3.7、§6.3、§8、§9；
- **维护 clean round**：读 §10～§11；
- **实现机制**：读 §13～§15，并对照 schema、team、qualitygate、verification、hook 和 controller 代码；
- **S8 消费者**：消费 Finding encounter、failure boundary、typed evidence/correlation 和 capture gaps，不应默认重新复现症状；
- **S10 消费者**：只消费 CleanRound 的 ID、hash、baseline、Result 摘要和剩余风险，不必重读所有 Reviewer 方法。

## 变更记录

| 日期 | 版本 | 变更 | 原因 |
|:--|:--|:--|:--|
| 2026-08-24 | v0.8.0 | 将实现终审收敛为可执行边界：NotebookEdit 统一路径 hard deny、DraftPlan 六个 QA focus 独立 Assignment、TODO/显式本地 evidence ref/三态 E2E workspace gate、Result artifact digest、当前 Assignment 纳入 drained handoff、resource-lock 释放唤醒 queued Agent；清理持久化 `closing` 与 S8 固定 WIP=2 叙事 | 让工具必经路径真正承载 S7→S8 闭环，避免文档声称有覆盖而实现遗漏、最后一份 Result 丢失或 Agent 排队后永不恢复 |
| 2026-08-20 | v0.7.0 | 将三类 Frontier 收敛为单一 required Claim set 与计算视图；将 coverage fixed point 改为最多一次、来源明确的 ReviewPlan revision；将 `blocked_by_confirmed_finding` 降为工具派生投影；verification-artifact workspace 限定为 E2E cold start 可选能力；ordinary finding 后继续剩余 required Claims | 终审发现多套 frontier、开放式 fixed point 和独立 blocked 状态会形成重复控制面；保留完整发现能力，但把范围扩张和状态数量限制在可验证闭环内 |
| 2026-08-20 | v0.5.0 | 将 DV/QA 明确为可按 focus 派出 1..N Reviewers 的 Static Quality Frontier；新增 focus coverage matrix、non-overlap boundary、QA design/boundary、pattern/idiom fit、logic/state/error、maintainability/test/debt 基线；ordinary static finding 后完成 preplanned frontier 再去 S8，behavior E2E 必须等待 frontier | 把黑盒 Builder 代码的设计债、边界混乱和维护风险尽量在白盒阶段一次发现，避免等 E2E 撞墙或后续 Agent 改错；同时以 focus 聚类、WIP 和 overlap validator 防止多 Agent 变成重复 checklist 与模式崇拜 |
| 2026-08-20 | v0.4.0 | 在 Finding 内新增 discriminated encounter、短 journey、last-good/wall/first-bad、状态差异、side effects、capture gaps 和 investigation readiness；增加撞墙时 evidence freeze、step-to-evidence binding、自动采集/脱敏和高危安全例外；明确 S8 不默认重新复现症状 | 保全最易丢失的操作现场，让 S8 可直接开展因果调查；采用内嵌事实 + typed raw evidence，避免新增独立报告和状态机造成复杂度失控 |
| 2026-08-20 | v0.3.0 | 将 S7 定位为 Macro-stage 的 Discovery 步骤；新增 immutable Finding、FindingSupplement、bounded discovery window、ObservationBatch 和 S8 无损 handoff；明确 S7 只记录表象、不判断根因或修复思路 | 与 S8 Diagnosis、S9 Remediation 联合设计，确保多表象可被完整接收并推导共同根因 |
| 2026-08-20 | v0.2.0 | 将 S7 重构为冻结基线、ReviewPlan Claims、Assignment DAG、两波调度、PLAN_REPORT 连续执行、Canonical ReviewResult、必经路径 Hook、原子 finding/pause 路由和机器 CleanRound；增加实现迁移、系统测试与指标 | 从软件工程和项目管理角度删除高复杂度低收益机制，并将指导下沉到工具自然路径 |

---

## 13.A · 待做缺口与本轮闭环记录（持续维护）

> 本节是把上面若干轮审计里反复出现的"仍未做"项集中到一处，便于下一个 agent 或人工拣选。每项标注：类别（机制/CLI/UX/环境/产品侧）+ 来源 + 落地时建议的第一步。

### 机制层（仍待做）

1. **命令面拆分**（CLI，§14.1 长期信号）
   - `runtime review-plan` 单一动词承担 register / revise 两种语义；改为 `runtime review-plan register` 与 `runtime review-plan revise`。
   - `runtime register-workgroup` 泄漏 S6 词汇（`--task` / `--task-id`）；S7 路径建议另起 `runtime dispatch-assignment --assignment-id --manifest`。
   - 来源：批次 1 恢复+复杂度评审、批次 2 命令面复评。

### 本轮已完成、从待办移除

2. **Revise pending-commit artifact 安全清理**（机制，P1，已完成）
   - `runtime review-plan revise` 先落盘 r2 artifact，再进入 Runtime CAS。若 `.commit-pending.json` 已写出但 state/journal 尚未完成，旧清理逻辑只看 revision 和 artifact SHA，可能删除将被 Runtime recovery 引用的 r2 文件。
   - 修复方向：Runtime lock 内检查所有 pending marker、revision、状态引用和 artifact SHA；存在 pending operation 时保留 artifact，交给 recovery 继续接管。
   - 验收：pending marker 故障窗口不删除 r2；稳定且未引用的 staged artifact 可清理；CAS 已提交或其他 writer 获胜时不误删。
   - 来源：第七轮 revise 边界实驱；代码位置 `internal/review/revise.go` / `internal/runtime/store.go`；已由 pending marker、稳定清理和 revise 保留测试覆盖。

3. **TR-012 修复基线机器绑定**（机制，P1，已完成）
   - 当前 `review-plan.schema.json` 只能检查结构；必须由 TR-012 写入 `review.round_entry.change_impact_ref`，再由 RegisterPlan 校验 canonical `change_impact.changed_artifacts` 的路径/SHA 是否进入 `frozen_subjects`、`coverage_inventory` 和 Claim source_refs。
   - 来源：第七轮冷读与 round-2 注册边界审查；已由 round-entry、RegisterPlan 校验和合法/缺失注册矩阵覆盖。

4. **Reviewer Agent identity 占位符穿透**（机制，P0，已完成）
   - `s7 manifest-draft` 仍输出 `TODO(planner):agent-id-...` 是有意的 authoring 草稿，但此前只有说明，没有注册边界；任意字符串会进入 `entities.agents[]`，并可能被 PostToolUse 观察者当作真实身份。
   - 已新增共享 `internal/identity` 校验，在 readback 请求生成、`register-workgroup`、activation envelope 预写、`runtime agent-event` 状态机、PostToolUse 识别、自动激活和 `runtime agent-begin` 恢复入口统一拒绝空白/控制字符和占位身份；观察者保持 fail-open，不产生伪绑定。注册错误会明确要求替换为真实平台 Agent ID。
   - 来源：A1/A7 沙盒 r11；回归覆盖注册不落状态、观察者不记录、auto-chain/agent-begin 不读伪身份文件。

5. **S7 status 缺少轮次来源和 Claim 映射**（CLI/UX，P1，已完成）
   - 看板现展示 `round_entry` 的 TR-012（S9 handoff seed）/TR-022（S8 no-repair re-entry）来源、handoff/impact/seed/baseline 引用；若 seed 存在但 `review.plan` 缺失，会直接显示 projection gap 和 reconcile 下一动作。
   - 每个可加载 Claim 同时显示 `focus`、`target` 和 Assignment，未消费的 Claim 也显示为 `planned`，避免 Agent 猜动态 claim id 或从原始 state 反查映射。
   - 来源：B3/A4；回归覆盖 round-2 blocked 看板和 seed projection 缺失。

6. **S8/S9 `--file` 请求形状不可发现**（CLI/UX，P1，已完成）
   - 增加 `docs/examples/s7-s9/` 下的 PlanReport、RepairResult、ChangeImpact、RepairHandoff 可复制 JSON；README 同时说明 Hypothesis 使用 flags、S9 dispatch 生成 manifest/task 且不接受 `--manifest`。
   - QA-template §5 增加 repair round 中 §2–§4 与 targeted row 共存的 worked example；S7/S9 protocol、skill、manual 均指向同一 examples 目录。
   - 来源：B1/B2/F4；选择文档样例而非增加新的运行时状态或 wrapper，保持复杂度收益比为正。

7. **`coverage_justification` 空对象误报**（机制，已核实为 false positive）
   - `Plan.CoverageJustification` 是 `*string`，草稿序列化为 JSON `null`，而不是 `{}`；schema 本身接受 string/null。新增回归测试锁定这一事实。若现场出现空对象，应视为外部手工变换或过期产物，不修改运行时契约来迁就它。
   - 同理，`e2e_coverage_state=not_applicable` 的强制 E2E N/A Claim 是有意的 exact-set 语义，已在 protocol/manual/draft note 明示；不为减少一次派发而删除它。

### CLI/UX 层

### 环境/平台层

4. **真实 Claude Code 2.1.218 平台 doctor**（环境）
   - 全程基于文档记载的 payload 形状与官方 `exit 2`/stderr 反馈语义。环境无 Claude Code 运行时，无法实测。
   - 来源：L4 §15.2 P0-3/4、S7 批次 1 闭环条件。

### 产品侧（依赖产品代码注入）

5. **产品侧浏览器/Playwright wrapper 的注入式采集**（产品侧）
   - harness 已提供 capture buffer + 脱敏 gate + 并入 binding（capture exec + capture step），产品侧 wrapper 需自行实现把 console/network/timeline 注入 buffer 的桥。
   - 手册 `loop-harness.md §capture` 的 `### Product-side wrappers` 段已写契约。
   - 来源：批次 2 §6.3/§8、批次 1 capture tester。

### 长期演进信号（非缺口，是观察）

- **readiness 多缺陷聚合**：当前 `validateInvestigationReadiness` 一次只报一个缺口，Reviewer 改完再触发第二个。改为聚合报告可减少 review iteration。
  - 来源：批次 1 Reviewer tester。
- **s7 status / next 视图合并**：当前 `status` / `next` / `s7 status` 三视图职责边界清晰但职责重合（白盒评审建议合并），评估时机待定。
- **`runtime review-result` 与 `runtime review-result submit` 双形态并存**：保留是为了向后兼容；建议在某 major 版本统一为单动词。
