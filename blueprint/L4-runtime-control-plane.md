# L4 — 运行时控制面与横切治理机制（Runtime Control Plane & Cross-cutting Governance）

> 层：第四层｜机制域：权威状态、指纹体系、证据链、追溯分母链、精确集求值、门禁分类学、写屏障家族、观测采集与脱敏、暂停/阻塞/终止保障、会话恢复投影、错误信息契约、债务与兼容性登记
>
> 上游：L1 D1～D7 与五公理；L2 生命周期目标、全局规则与「REQ 授权生命周期」概念层
>
> 姊妹篇（五家族）：[L4 Agent 调度与治理](L4-agent-dispatch-governance.md)=「谁在责任上行动」——拓扑、PLAN_REPORT、等待/idle/stop、恢复的对象模型与 Hook 决策矩阵；[L4 权威状态机与迁移事务核心](L4-state-transition-core.md)=「事实住哪、怎样变更才算合法」——存储模型、单写者、崩溃写序、实体生命周期索引、guard/action 引擎与自动推进仲裁；[L4 Runtime revision 使用与命令协调](L4-revision-usage.md)=「revision 谁需要知道」——内部提交序号、单主会话默认命令、对象版本边界与复杂度准入；[L4 Hook 与平台事件接线](L4-hook-platform-wiring.md)=「决策如何坐上平台总线」——事件注册、payload 契约、输出/退出码契约、fail-open/closed 态度分界；**本文档=「内容怎样才算合规」**——指纹与证据规则、追溯分母、精确集求值、人闸与人机界面契约、错误信息契约、债务登记等数据契约与行为准则层。
>
> 层内权威声明（本文件成立的前提）：**凡列入本目录的机制域，其目标态定义以本文为唯一权威**。L2 中同名全局规则只保留"存在与用途"的声明；各 L3 只写"本阶段消费哪种实例化"。L3 正文与本文件冲突时，先核对代码现状，再按 L1 第五部分演化协议回改落后的一方。唯一的显式外移：「注意力分配/渐进披露」是文档写作纪律而非运行时机制，权威仍在 [L3-README](L3-README.md#注意力分配原则各-l3-sn-的共用判定尺本节是唯一权威各-stage-只引用不复述)。
>
> 状态：v0.3.0。每节区分【目标态设计】与【当前实现】；差距在 §15 显式列出，不把意图写成既成事实。v0.3 起存储与引擎类细节移交姊妹篇，本文收敛为内容合规规则层。

## 0. 本文件的准入逻辑

《Agent 调度与治理》进入第四层的理由不是"它讲的是共用的东西"，而是它满足一条完整的判据：存在一个可以独立建模的机制域——有自己的对象模型（Assignment Record）、正交状态线、消息契约、Hook 决策矩阵、风险规则与恢复语义——该域被五个以上 stage 以相同方式消费，且任何 L3 单独定义都会产生变体漂移。

对剩下的全部 S0～S11 内容用同一条判据重新筛过，得到本文的十三个域。四个曾考虑但**被拒收**的候选及理由（拒收同样是结论的一部分）：

| 拒收候选 | 理由 |
|:--|:--|
| 三角色自审（implementer/e2e-tester/maintainer） | D4 方法论逼问，载体是模板字段与 skill，不是可独立建模的运行时机制；身份叫法差异在相应 L3 内修正即可 |
| TASK 依赖 DAG 与批次调度 | 已被调度文档 §6 完整覆盖（一张权威图、Ready/Dispatchable 分离），重复建域即制造第二控制面 |
| 三角色自审之外的审查 checklist（五问+半问等） | 同拒收理由一；属模板与 skill 层 |
| Release-ready package 独立成域 | 它只是人闸交接物的一种形状，机械骨架已由 §7.2 人闸契约承载；作为字段规范记录于该节，不足独立机制 |

另有一条结构事实促成本文件的存在：即使不新增域，v0.1.0 之前的现状是这些机制的**定义散落在最先需要它们的那个 L3 里**（CAS 写在 S1、指纹家族写在 S7、exact-set 散在三份纠错文档、脱敏闸写在 S7 capture 节），后来者只能从实例反推通用规则，正是层定义要消除的"各自写一套变体"。

---

## 1. 权威状态机制（D1）

### 1.1 单一权威与单一写者

- `.claude/loop-state.json` 是唯一的工程事实源；`journal` 只追加；终态周期整体归档到 `runtime-archive`（带 disposition），REQ 文件状态只是给浏览者的可读性镜像。
- **所有权威写入都经 Runtime Store/Writer 完成，由单写者内部串行化并自动分配 revision**。Hook 是触发器不是存储：PostToolUse(SendMessage) 捕获 PLAN_REPORT 后落库走的也是同一条 Runtime 写路径，不自带第二套存储；revision 的 Agent-facing 边界见 [revision 篇](L4-revision-usage.md)。
- 板（board）、CLI 回执、hook 投影都是**只读视图**；视图不参与判定真值，与权威不一致时触发 reconcile 而非互相说服。
- 调度控制面（Assignment/plan/Result/checkpoint）必须解析到项目级共享存储，不跟随 Worker worktree cwd 各写一份，也不靠 git merge 汇总运行态（详接调度篇 §2.2 的硬边界）。

### 1.2 请求体 vs 持久记录双形态约定

所有接受 `--file <request>.json` 的动词遵守同一形状区分：

| 形态 | 键风格 | 典型差异 | 校验者 |
|:--|:--|:--|:--|
| 请求体（authoring 文件） | 引用式键：`session_ref`、`occurred_at`、`source_refs` | 不含运行时派生字段（无 `record_type`、`revision`、approved hash） | CLI 解析 + 请求契约一致性测试 |
| 持久记录（artifact/schema） | 身份键：`session_id`、`reported_at`，并含 `record_type`、revision、hash 等由 Runtime 填写的字段 | 由 Runtime 在事务内产出 | artifact schema |

不要拿请求文件去对持久化 schema 校验；约定由 `TestS7S9RequestExamplesTrackRequestContracts` 防漂移，适用于未来一切新增 `--file` 动词。

### 1.3 崩溃安全原则

原则句仅此一条：**半完成状态必须可从磁盘无损重建，而不是可推理出来**。写序细节（marker 协议、恢复顺序、原子原语）与对账三角（doctor / validate --all / reconcile 各自能修什么）的唯一权威见 [状态机核心篇 §4/§9](L4-state-transition-core.md)。

### 1.4 身份的最小纪律

进入 Runtime 实体投影、PostToolUse sender binding 和 auto-chain 的 agent 标识必须非空、无控制字符、非 authoring 占位符（`TODO(planner):...` 在注册即被拒绝并指正）；不强制统一前缀，不对合法平台 ID 收紧格式。人侧 actor 一律是仓库内自我声明（`--approved-by` / `--actor`），**不是身份认证**——这一信任边界适用于全部人闸（§7.3）。

---

## 2. Revision 语义指针

revision 的统一语义、对象版本分层、Agent-facing 命令默认值、可选 CAS 和复杂度准入统一见 [L4 Runtime revision 使用与命令协调](L4-revision-usage.md)。本文件只消费其中的结果：指纹、generation、review round、证据 validity 和对象 hash 各自承担自己的事实身份，不借 revision 代言。

---

## 3. 指纹体系（sha256 家族）

一个算法、多种用途；每种用途都有明确的生产者与消费者，都机器可公证（D6）：

| 指纹 | 定义 | 生产者 | 主要消费者 |
|:--|:--|:--|:--|
| documents[] 登记指纹 | 文档 path/version/sha256 入册 | PTR-PLAN-01/02、TR-002 等 transition 的 register action | registered-document drift 复核、exact-subject 门（S5） |
| REQ 基线指纹 | bound_req 登记的 sha256，每次规格类迁移前与磁盘重算比对 | req bind | TR-004/007/023 的 `req_baseline_unchanged` guard |
| frozen_subjects / subject_digest | 轮内冻结主体集（path+sha 排序列表的聚合 sha256） | ReviewPlan 注册 | ReviewResult 强校验字段、`s7 status` 看板披露、TR-008/TR-009 边界 |
| baseline digest | ObservationBatch / handoff / clean snapshot 携带的基线快照哈希 | seal / handoff / snapshot 事务 | S8 intake 四硬门、S9 session open、S10 复算 |
| workspace digest | E2E 验证工件目录 sha256（sorted 相对路径:文件哈希行） | `s7 workspace-digest` | cold_start 轮 result 提交与 seal 收口的闭环校验 |
| changeset 工件对 | 每 changed artifact 的 path + 内容 sha256（删除件带 base/last-good sha） | changeset compute | ChangeImpact、TargetedReverification 绑定 |
| 权威事务哈希 | Case revision sha、Contract approved hash、handoff ref+SHA | route / approve / handoff commit | 下游每一跳的前置校验 |
| activation 哈希链 | plan_report 文件字节级哈希链 | agent lifecycle 事务 | readback/activation fail-closed 实校验 |

> **收敛方向（RC-10 Step B — 已落地）**：`ComputeTriple`/`ComputeRevisionPair`（`internal/runtime/fingerprint.go`）产出三元 `state_hash + evidence_hash + baseline_hash` 与二元 `state_revision + evidence_generation`；首个消费者为 `RefreshFingerprints` 输出 `FingerprintResult.Triple`。八家族表仍保留：切换是逐消费者的迁移事务。

冻结不变量与受控例外：

- **轮内冻结基线漂移 → 整轮 stale**（产品代码、CASE/PATH、配置均属冻结面）；
- **局部失效例外**：E2E spec 自身在其 Result 之后被修改，只 invalid 该 Result 及显式依赖它的 Claims，不宣告整轮 stale——这是精确失效原则而非宽松放行；
- 代际隔离：generation/amend 使下游证据全量失效；旧轮 evidence 永不污染新轮分母。

---

## 4. 证据链机制（D6 / C3）

### 4.1 公共信封

一切证据共享最小公共字段：`id / kind / path / sha256 / produced_by (+producer_responsibility) / conclusion / review_round / status(valid|invalid)`；kind 决定其余特有字段。规划类信封（planning_design / planning_contract / planning_task）与审查类信封（document_review 及 REV 系列）只是 kind 的差异，不允许任何一个 stage 私自增删公共字段——新增公共字段是本节的修订事项。kind 注册表与槽位由 evidence catalog 控制；pipeline-owned kind 会显式堵死手工登记路径（如 finding_supplement）。

### 4.2 失效的四条家族

| 家族 | 触发 | 粒度 |
|:--|:--|:--|
| 代际型 | amend / generation bump | 全部下游证据失效 |
| 轮际型 | 新 review round 开启 | 旧轮证据不入新轮分母 |
| 修复型 | ChangeImpact 提交 / 受控 plan revise | 类型化规则字段：`invalidation_rule=change_impact_<status>`、`plan_revision`；失效与替代在同一事务内落账 |
| 人闸型 | S11 驳回处置 | 按 evidence kind 粗粒度：reject_acceptance（TR-028）失效 acceptance+acceptance_record+release_audit+release_audit_record；reject_release_audit（TR-029）仅失效 audit 两类、保留 acceptance（S11 不读正文原因，按 kind 失效是有意取舍） |

四类证据命运词汇全域统一：`invalidated`（行为依据已变）/ `superseded`（已有 fresh replacement）/ `retained`（依赖图证明无关，须附理由）/ `required_reverification`（必须在定向复验或下一完整轮重新执行）。

### 4.3 干净轮能量函数（承接 L2 总览授权）

L2 总览规定「必需维度集合的权威定义归第四层门禁语义设计」，定义居所即本节。「未满足的必需验证维度数」由 `verification.EvaluateCleanRound` 的七项命名检查承载（当前实现核对日 2026-08-28）：

1. `review_round_started` —— 当前 review round ≥ 1；
2. `review_plan_clean` —— 本轮 ReviewPlan 存在、同轮且 status=clean；
3. `all_required_claims_pass` —— 每条 required Claim disposition=pass 且绑定已消费 Result；N/A 保持 not_applicable；
4. `no_findings_current_round` —— 本轮存在已确认 Finding 即否决干净路径；
5. `no_invalidated_pass_evidence` —— 本轮 result/finding/batch/clean 快照四类证据不得有 invalid；
6. `no_open_blocking_bugs` —— 仅 P0 计入阻断，closed P0 必须有含其 ID 的当轮 targeted_reverification；
7. `clean_round_snapshot_registered` —— 必须存在本轮 valid 的机器 clean_round 快照（TR-009 只认机器快照）。

主干推进使未满足项单调不增（D7）；机器分母之外的事实（P1～P3 风险、AC 来源真实性）由 S10 人工层补充，不属于该能量函数。

---

## 5. 追溯分母链（单一验证分母）

### 5.1 设计对象

L2 全局规则点名：**S2 场景包的 `case.id` 是 S2→S7 验证链的唯一分母；REQ 验收标准（AC）与 CASE 必须双向可达**。这个机制是一个贯穿六个 stage 的数据契约，此前散落在五份 L3 的各自小节里，此处统一定义其成员与守恒规则：

```text
AC ──(bridge)──> FR ──> Rule/Branch ──> CASE(case.id)
                        PATH/Story（用户轨补充维度）
CASE ──(contracts check 反向闭合)──> 契约条款 cell `{contract-id} §n`
条款 ──(tasks check 正向覆盖)──> TASK §3 Delivered Clauses
CASE ──(e2e_assets 绑定)──> E2E Claim 的 path + CASE id + SHA
CASE ──(ACC 第 3 节)──> 每个 AC 行的 expected/evidence/result
```

### 5.2 五条守恒规则

1. **单一生产者**：`cases.json`、`scenario-coverage.json` 由 `scenario generate` 从 scenario-model/facts×stories 规则原子生成；手写副本不存在合法地位，`scenario validate` 以字节比对守住这一点（源与投影不双写的投影纪律）。
2. **反向闭合**：每条 CASE 至少映射一条契约条款；每条款至少一个 TASK；断裂即 gate 红（ContractsCheck 五类机械工作）。cell `{contract-id} §n` 是稳定语义单位，不是实现章节号。
3. **受控 N/A 只有两种权威出口**：NFR id 或 REQ §A4 未决项指针；e2e lens 上表现为 `applicability=not_applicable` + na_rationale + source_refs 指回影响分析。沉默格（coverage matrix 中未解释的组合）必须被 cross-matrix 检测暴露为缺口而不是留白通过。
4. **断链定位返回**：bridge/check 失败的报错携带 token/clause/CASE/SHA 定位（公理五），修对应层产物后仅重审受影响职责——不要求全链重来。
5. **冻结联动**：CASE/PATH 属 §3 冻结面，其变更使整轮 stale 并要求重新生成——分母在验证开始后不再漂移。

### 5.3 各段消费方式（各 L3 据此声明，不重述规则）

S0 保证 AC 可判定并持有 §A4 出口；S2 生产分母；S3/S4 反向闭合；S5 把闭合纳入独立审查 subjects；S7 用 CASE 绑定 e2e_assets 与 focus_key（focus_key 不是 angle registry 的复活，只回答"哪类上下文一起读"）；S10 沿 ACC 逐 AC 回收证据。任何 stage 发现"第二套 case 编号体系"即违规。

---

## 6. 精确集求值纪律（exact-set）

### 6.1 三条公设

跨 stage 反复出现同一个求值原语——"拿完整声明的有限集合做精确匹配求值，而不是抽样或代表性判断"。其规则本体在此统一定义（直接对抗失效目录中的 Eroding Goals 与 Goodhart）：

1. **集合先于结论固化**：被求值的承诺面必须在求值开始前完整枚举并持久化；事后缩小集合换取绿灯是非法操作（防"这次先算了"）。
2. **覆盖是划分，不是采样**：同一求值域内的成员关系构成互斥完备划分（如 Assignment 带 non_overlap_boundary），允许 N/A 但不允许遗漏；沉默不是不适用。
3. **豁免个体化且需背书**：每个 not_applicable 都要有理由与来源（接 §5.2 规则 3）；blocked 成员不退出集合——以 `blocked_by_confirmed_finding` 投影保留义务（必带 blocking finding id、failed_precondition、evidence_refs、after_repair_required=true），阻塞解除后义务立即复活。

### 6.2 全系统实例清单

| 求值点 | 承诺集 | 机器判定 |
|:--|:--|:--|
| S4 tasks check | 条款宇宙 ↔ TASK §3 双向覆盖 + DAG 无环 | 结构检查六项 |
| S7 ReviewPlan 注册 | required claims 集、assignments 对 claims 的互斥精确覆盖（exact-set Claim↔Assignment）、TASK 进 source_refs（缺失即拒）、cold_start 单 Assignment 不得独占全部 required e2e Claims 且跨 ≥2 focus 维度 | overlap / overload validator |
| S7 submit→seal | ObservationBatch.finding_ids == 本轮 immutable findings 的 exact set；drained_assignment_ids 记录已 drain 的责任集 | intake 四硬门的第一道 |
| S9 RepairResult | changed_artifacts 与会话真实 diff 做 exact-set 比对（多报 extra_or_changed / 少报 missing_or_changed 两栏差集列出）；repair_units 结果齐全性 | result submit 硬门 |
| S9 TargetedReverification | 每 Contract 每 assertion 一一对应（symptom-N / root-N / gap-N 兼容别名 detection-N） | targeted commit 完备性门 |
| S10 coverage_inventory | 本次变更声明的有限审查全集开局固化，不得为通过而临时缩小 | ACC 映射 + UNKNOWN=0 等硬指标 |

### 6.3 差集报告的标准形

exact-set 不匹配的错误输出统一为两栏差集：missing（承诺了未兑现）/ extra（出现了未承诺），分别附下一步动词。它是 §12 错误契约在覆盖场景下的专用形态；任何新的 exact-set 消费点直接继承，不为单点发明第三种报告格式。

---

## 7. 门禁行为准则与人机界面（D2 / D3）

### 7.1 迁移形态学与引擎本体（指针）

迁移 ID 五类形态（TR 主生命周期 / GTR 人闸边界 / PTR 子相位及命名纪律 / runtime-authority 专用事务如 `S8-REPAIR-CONTRACT-APPROVAL` / 阶段 cursor）与 guard/action 引擎机制、auto_trigger 仲裁的唯一权威已上移至 [状态机核心篇 §2/§5/§6](L4-state-transition-core.md)；本文件不再维护该表。CI 一致性（`TestProtocolTransitionIdsResolvable` 等）作为两侧共用的守卫继续有效。

本域在本文件只保留三条**行为准则**：

1. 迁移内要持久化的副产物一律在 action 里原子完成，不留"下一步自己补"的尾巴；
2. quality gate 挂在工具调用的自然路径上评估——阶段不放行时正确反应是继续补产物（工作不中断），只有不可逆动作被硬阻断（D3 的执行干预 vs 生命周期推进之辨）；
3. 声明存在的每个 guard/action/gate 必须能被 CI 解析到注册处，无主孤儿非法。

### 7.2 人闸机械形态契约

REQ 七动词生命周期（bind/pause/resume/amend/unbind/abort/approve；生命周期语义的概念定义归 L2「REQ 授权生命周期」，本节定义其在 runtime 的机械形态）与 S11 六枚举处置共享同一骨架：

1. **结构性停机两层**：loop-definition 标 `eligible=false + human_boundary=true`（辅 forbidden event 如 automated_s11_decision）→ Controller 对该 cursor 返回空自动候选。两层独立起作用。
2. **固定处置枚举 + 专用动词**：人只能从预定义处置选择（S11 六枚举 approve/defer/reject_defect/reject_acceptance/reject_release_audit/abort ↔ TR-025..030）；每个处置映射固定去向，不接受自由 target-state。
3. **人闸只做三件事**：记名（actor 自我声明）、留证（decision evidence 绑定 Runtime identity、disposition、当前 release context 和一次性 decision ID）、选去向。approve 只记录授权，永不执行 merge/publish/deploy/release；rollover 是独立 API 而非闸的一部分。Runtime revision 由 Writer 内部记录，不是人闸输入。
4. **交接物压缩视图的最小字段（目标态）**：交给人的一切就绪包应含——关键事实 IDs and hashes、已完成范围、可信依据、唯一未决事实、影响与残余风险、建议处置、恢复点、automation stops 声明。当前无独立 runtime entity/schema（诚实缺口，§15）。
5. **零副作用拒绝**：未知处置、缺证据或业务 context 不匹配一律非零退出且不产生状态变更；内部 revision 冲突由 Writer/命令协调处理，不转化为正常 Agent 操作税。

### 7.3 阶段锁定时序契约

「registered ≠ 物理冻结」三分法：登记（获得指纹合法性）→ 可控返工（合法流程更新并重登记）→ 硬拦截（阶段感知写保护激活）。S0 锁定→S1 bind 窗口依赖人锁定纪律并有诚实缺口标注。新 stage 冻结语义沿用此三分法，不允许未声明的中间强度。

### 7.4 歧义裁决原则（fail-closed on ambiguity）

凡是机器面对多个并发信号竞争同一决策点的场合，一律不猜：

- selector 唯一事实源：verdict 类信号走单一 selector（SEL-*-OUTCOME）；两个候选并存时 Controller 拒绝评估（空 candidates 或失败闭合），绝不按隐式优先级挑选；
- 确实需要排序的，层级必须写成定义而不是调度器的习惯：如 Case mixed-route 的权威顺序 REQ > spec > repair > duplicate > no-change；上表实算时只能引用该定义；
- 对象无法识别时不猜测绑定：PostToolUse 定位不到 agent 即静默跳过（观察路径 fail-open），屏障拦截不到对象即整体 fail-closed——两个方向都是定义内的行为（§8）。

---

## 8. 写屏障家族（D2 / D5）

policy engine 的 deny 规则是跨 stage 沉淀的核心资产，六条主规则：

| rule | 语义 | 适用相位 | 识别依赖 |
|:--|:--|:--|:--|
| `locked_artifact_write` | 写入命中 locked artifact 即拒绝 | 全局（building 起） | 仅路径匹配 |
| `reviewer_product_write` | 验证相位产品面写入拒绝（验证者只许写报告面与 workspace） | verification | 仅路径+相位，不依赖身份 |
| `assignment_write_before_plan` | 计划回执落库前的首次写入拒绝（首写屏障） | 派发执行责任 | 依赖三级 agent 识别；主会话豁免 |
| `repair_write_before_execution` | 领域 PlanReport 通过并 execution begin 之前产品写拒绝 | bug_resolution planning/reproducing | 同上 |
| `repair_assignment_scope` | fixing 相位逐工具校验写面 ⊆ 经 PlanReport 绑定的 Assignment scope | fixing | 同上 |
| `unauthorized_task_self_claim` | teammate 不得认领未被派发的 Team task | TaskUpdate 面 | Team payload |

统一语义约束：

- 拒绝信息 = 命中规则 token + 含义 + 下一个动词（missing-token 词表模式全套共享）；
- 豁免面对齐：`.claude/`、`docs/reports/`、计划声明的 `verification_artifact_workspace`；
- 控制面不可读 **fail-closed**（宁停勿盲放行）；对象无法识别时观察类路径**静默降级**——两个方向均在 §7.5 定义之内；
- 发布面：`protected_commands.json` **已不再作为 Hook 拦截依据**（hook-policy 的 extension_protected_commands 置空，且有测试注释锁死"Hook no longer loads docs/release_audits/protected_commands.json"；分类器仅供 Bash 变异路径分析）。发布保护的现状 = S11 结构性停机 + 人操作纪律，这是有意边界而非待办（事实核查与归属见《[Hook 接线](L4-hook-platform-wiring.md)》§10）。

---

## 9. 观测采集与脱敏治理

### 9.1 目标态设计

一切自动进入 journal/evidence/timeline 的观测内容都必须过同一道脱敏闸，规则属于本文件而不属于采集工具：

- **敏感值模式表**：password/token/secret/api-key/bearer 的键名与常见值型模式命中即拒绝写入，报错指明命中的字段——采集端不自行维护放行例外；
- **exec 面加固**：capture exec 对 argv 中的机密形参硬拒；终端流输出以占位符改写后回流，防止 secret 泄漏进日志与 evidence；
- **缓冲纪律**：步骤缓冲不可读时显式 warn 而非静默丢弃；提交侧 `--captures <dir>` 会把 encounter.timeline 为空的 Finding 自动并入 steps.jsonl，使 step-bound evidence 校验随之生效——采集不是装饰，并入即受证据校验约束；
- **诚实边界**：harness 提供捕获协议与 CLI；浏览器/console/network 的注入式采集属产品侧 wrapper（manual 已写契约，实现归产品，不算 harness 缺口）。

### 9.2 当前实现

双闸门脱敏、argv 硬拒、占位符改写已在 capture step/exec 落地并被回归测试覆盖（R10 类问题封堵）；注入式 wrapper 待产品侧。

---

## 10. 暂停、阻塞与终止保障（D7 / C4）

### 10.1 暂停是单一结构

所有通向 paused 的迁移收敛到同一检查点结构：`capture_pause_checkpoint` 产出一等公民 `pause_record`（游标 + 指纹 + generation + round）。TR-010/TR-011 以 `pause_checkpoint_recorded` 为 guard；TR-018（审计阻断）与 TR-026（defer）在 action 里生成 pause_record。resume 前**逐文件核对暂停时刻指纹，漂移即拒**；漂移了走 amend（generation+1 下游全失效）。

### 10.2 Blocker 模型：阻塞必留痕，解除必有动词

任何 blocked 状态必须同时满足：(a) 看板可见（含 blocker 引用与恢复指引）；(b) 存在命名解除动词并把解除证据经 Runtime 落账。当前三种解除载体并存：

| 阶段 | 阻塞呈现 | 解除载体 |
|:--|:--|:--|
| S7 | Assignment blocked 行 + blocker_ref 投影 | `runtime agent-event --event blocker_resolved --message <file>` |
| S8 | Clarification 类阻塞 | FindingSupplement（discriminator 判别门：hypothesis_id+discriminator+expected_outcomes 或 `--in-round-note`，二者互斥） |
| S9 | targeted 复验被环境/外部阻塞 | 先解除阻塞再 `runtime repair targeted resume --actor --reason` 回原 checkpoint |

合并为通用 Blocker 实体是待决演进（§15）；合并前共同不变量即 (a)+(b)，禁止出现没有解除动词或看板不可见的第四种 blocked。

### 10.3 终止保障

纠错预算集中在运行时配置（如 `configuration.repair.max_full_review_rounds`、单缺陷尝试上限），在看板披露 `(round N of M)`。超限 = 升格为人的信号并暂停；连续重入而能量函数不降同样视为振荡信号（D7）。预算是收敛判据的执行形式——"还在修"不等于"在收敛"。

---

## 11. 会话恢复与投影包（D1 / D3）

- **SessionStart 恢复包内容契约**：当前位置（stage/round 与预算消耗）、board 三桶（running/queued/blocked）、未消费 Result、required Claim 覆盖缺口、seed/handoff provenance（若有）、**唯一下一步**（含可直接复制的下一命令）。
- **PreCompact** 保证 Assignment/plan/result/checkpoint 已持久化后才允许压缩。
- 渐进披露三原则作用于恢复面（原则本体在 L3-README 文档纪律节）：恢复包只给最小集，方法论按步骤加载。
- 无法识别对象时恢复逻辑静默降级；从权威状态重建永远优于从记忆续写。

---

## 12. 错误信息契约与词汇边界（公理五）

### 12.1 七字段错误结构

面向 agent 的拒绝/失败反馈向以下结构收敛（推广到全部 CLI/hook/gate/transition 拒绝）：

```text
code              机器可分类的错误码（typed error）
observed          观察到的反例事实
expected          期望成立的条件
owner             当前动作归属（谁修）
next_action       一个可直接执行的下一动词
preserves         本方案保住了什么现场（可选）
```

配套两个全局开关：schema oneOf 判别剪枝（只报目标分支真实叶子错误；`LOOP_HARNESS_SCHEMA_VERBOSE=1` 恢复全集）；stale 类错误一律 §2 三段式。exact-set 场景追加两栏差集标准形（§6.3）。造错句成本高是对的——它是传达公理的主载体。

### 12.2 词汇边界

| 近义词组 | 边界 |
|:--|:--|
| 通用 `plan_report` vs 领域 PlanReport（`repair_plan_report`） | 前者是平台生命周期检查点；后者是领域执行计划证据。不可互换（细节归调度篇） |
| assignment id 前缀族 | `assignment-*` 通用；平台别名 `assignment-s9-*` 仅作 generic checkpoint 绑定；领域 `repair-assignment-*`；Runtime 字段 `assignment_owners`。新增拼写先入表 |
| complete ≠ done ≠ integrated | TASK 文档完成 ≠ Builder Result submitted ≠ consumed/integrated；三条状态线不合并 |
| 计划批准 vs 合同批准 | dispatch_mode 的 plan approval ≠ RepairContract 的 contract approve；对象、动词、迁移完全不同 |
| epoch 三兄弟 | generation（基线代际）/ review_round（验证轮次）/ revision（见 [L4 revision 使用篇](L4-revision-usage.md)；Runtime 只作内部提交序号，对象版本另行定义）；引用时必须带限定词 |
| angle 与 focus_key | angle 生命周期已删除；focus_key 只是 Assignment 分组的上下文维度提示，不是第二套检查分类 registry |

---

## 13. 债务与兼容性决策登记

L2 全局规则「债务登记」（类型/影响/成本/负责人，债可累积但必须可视可排期）与 owner 后兼容裁定（2026-08-18：兼容不是默认美德，每次选择兼容须登记决策理由、影响面与移除路径；**无登记的兼容即无声负债**）需要一个机制居所，定于此：

【目标态】两类一等登记对象，随验收汇编一并入册并在 S11 就绪包可见：
- `tech_debt_entry`：type / impact / cost / owner / tracking_artifact；
- `compat_decision`：rationale / affected_surface / removal_path（责任人 + 触发条件）；不移除的兼容每隔周期复核一次登记仍有效。

【当前实现】仅模板与 prose 承载（ACC/release audit 模板的债务节），无独立 runtime entity/kind——这是 §15 显式登记的目标缺口，不是已完成能力。在此之前，"债是否存在"以验收材料正文为准，机器不做宣称。

---

## 14. 跨 L3 消费地图

●=核心消费 ○=轻量消费 −=不适用。消费细节归各 L3，本表保证全覆盖与无重复定义。

| 机制域 | S0 | S1 | S2 | S3 | S4 | S5 | S6 | S7 | S8 | S9 | S10 | S11 |
|:--|:-:|:-:|:-:|:-:|:-:|:-:|:-:|:-:|:-:|:-:|:-:|:-:|
| 权威状态/单写者协调 | ○ | ● | ○ | ○ | ○ | ○ | ● | ● | ● | ● | ● | ● |
| 指纹体系 | ○(sha 可算) | ● | ○ | ○ | − | ● | ● | ● | ● | ● | ● | ○(对账) |
| 证据链/失效家族 | − | ○ | ○ | ○ | ○ | ● | ● | ● | ○ | ● | ● | ●(kind 型失效) |
| 能量函数七检查 | − | − | − | − | − | − | − | ●(生产) | − | − | ●(复算) | ○(间接) |
| 追溯分母链 | ○(AC 判定性+§A4) | − | ●(生产) | ●(反向闭合) | ●(正向覆盖) | ○(subjects) | − | ●(assets/绑定) | ○(boundary refs) | ○ | ●(逐 AC 回收) | ○ |
| 精确集求值 | − | − | − | − | ●(宇宙↔TASK) | ○ | ○ | ●(claims/answers/batch) | ○(exact findings) | ●(units/artifacts/assertions) | ●(inventory 固化) | − |
| 门禁分类学/人闸契约 | ●(锁定手势) | ● | ○ | ○ | ○ | ○ | ○ | ● | ● | ● | ● | ● |
| 歧义裁决原则 | − | − | − | − | − | − | ○ | ●(selector) | ●(route 层级) | ● | ●(selector) | ●(空候选停机) |
| 写屏障家族 | − | − | − | − | − | ○(审后不改稿) | ● | ● | ● | ● | − | ○(protected cmds) |
| 观测采集与脱敏 | − | − | − | − | − | − | ○ | ● | ○(supplement 敏感值) | ○ | ○ | − |
| 暂停/Blocker/终止保障 | − | ●(控制面动词) | ○ | ○ | − | ○ | ○ | ● | ● | ● | ○ | ●(defer/abort) |
| 恢复投影/SessionStart | − | ○ | ○ | ○ | ○ | ○ | ● | ● | ● | ● | ○ | ○ |
| 错误契约/词汇边界 | ○ | ○ | ○ | ○ | ○ | ○ | ● | ● | ● | ● | ● | ● |
| 债务/兼容性登记 | − | − | ○(ADR 备选) | ○(breaking 决策) | − | − | − | − | − | − | ●(登记时机) | ○(包内可见) |

---

## 15. 当前事实边界（诚实条款）

截至 2026-08-28：

1. `protected_commands.json` **已退出 Hook 主拦截路径**（2026-08-28 核实：hook-policy extension_protected_commands 置空 + 测试锁死不再加载；分类器仅供 Bash 变异路径分析）。发布保护的现状由 S11 结构性停机与人操作纪律承担，属有意边界；若未来要恢复命令级硬拦，须走《Hook 接线》§9 治理流程。
2. 三种 Blocker 解除载体（§10.2）并存是有意的兼容现状；统一前禁止新增第四种事件名。
3. legacy PTR-BUG-02/04/08 别名仍在 schema/协议中以兼容投影存在，均已标注 legacy-only。
4. 仍是目标态、暂无代码落点的部分：request/persisted 双形态约定的 schema 化校验（现由示例 README + 测试守护）、§7.3 的人闸交接物最小字段（无独立 entity/schema）、§13 债务登记实体、protected commands 接线、三种 Blocker 合并。写代码时不得以"文档已写"推定已实现。
5. encounter 字段集是否提升为独立 schema、TR-016 是否改为消费 rich change-impact（现为 AffectedPaths，分母偏窄），均为待 owner 裁决项。
6. L3 尾部审计块是历史记录；冲突时以各 L3「当前事实」小节及代码为准。本次审查据此修正了 L3-S5 的 two-phase-activation 残留与 L3-S10 干净轮分母旧口径（angle）。
7. RC-10 段轮转已落地（2026-08-28）：journal 10k 行阈值触发 `maybeRotateJournalLocked` 归档为 `loop-events.jsonl.archive.<seq>.jsonl` 段（marker 事务+段感知 `inspectJournal`/`journalLineCount`/`journalContains`，见《状态机核心篇》§11）；§3 指纹家族收敛方向为三元 hash + 二元 revision（收敛事务待设计）；review-result submit CAS 事务新增 per-phase 时长指标 `loop_s7_submit_phase_ms`（best-effort，`internal/review/submit.go` + `internal/metrics/s7.go`）。三者均为观测/登记，不新增任何强制。

---

## 16. L1 映射与 DoD

| 准则 | 本文落点 |
|:--|:--|
| D1 权威外置 | §1 全节；§11 恢复从权威重建；§5 分母链让"覆盖了多少"可从产物重读 |
| D2 自然路径 | §7.2 gate 挂 PreToolUse；§8 屏障埋在写路径；§9 采集挂在动作路径上 |
| D3 门是顾问 | §7.4 registered≠冻结；deny 信息=指导；structural stop 只在人闸与不可逆 |
| D4 引导性产物 | §1.2 请求体字段逼问作者填真值；§12.1 错误字段逼问运行时报真相 |
| D5 三级强制 | §8 屏障分级与豁免设计；强制的每一级都有名字 |
| D6 三方收敛 | §3 指纹机器公证；§4 证据链独立背书；§6 覆盖划分对抗抽样式绿灯 |
| D7 收敛可观测 | §4.3 能量函数七检查；§10.3 预算与振荡升格 |
| 公理一～五 | §2 词词典（原型：版本号制度）；§1 分工；§14 每格有消费者；§10.3 保险定价；§12 理由随机制走 |
| 失效目录 | Eroding Goals/Goodhart → §6 精确集；Shifting the Burden → §5.2 受控 N/A；冰山 → §4.3/§10.3 |

**DoD**：十三个域均有权威代码落点或 §15 显式缺口登记；各 L3 修订不再复制机制定义；新增跨 stage 手法先入本文件再入 stage 文档；CI（迁移 ID 可解析、示例契约追踪、指引动词存在性三类测试）持续绿灯。

## 变更记录

| 日期 | 版本 | 变更 | 依据 |
|:--|:--|:--|:--|
| 2026-08-28 | v0.3.0 | 四家族定位定稿：迁移 ID 形态学、guard/action 引擎、auto_trigger 仲裁、崩溃写序与对账命令的本体移交新立的《权威状态机与迁移事务核心》，事件注册/payload/输出/失败态度移交新立的《Hook 与平台事件接线》；本文件收敛为内容合规规则与词汇词典层。事实更正：protected_commands 已于早前批次退出 Hook 主拦截路径（先前版本误标为"接线待办"） | owner 批准的基石抽取批次；两份子代理代码事实核查 |
| 2026-08-28 | v0.2.0 | 按《调度篇》的 L4 准入逻辑复审全文：明确本文件为其覆盖域的唯一权威定义处（去除指向 L2/L3 的权威倒挂）；新增五个机制域——追溯分母链（单一验证分母规则的本体化）、精确集求值纪律（含差集报告标准形）、观测采集与脱敏治理、歧义裁决原则（selector fail-closed/显式层级）、债务与兼容性决策登记；人闸契约补交接物最小字段；词汇边界增 focus_key 条目；附四项拒收候选及理由 | owner 复核：L4 逻辑应与调度篇同构——定义机制而非综述；追问是否还有漏网机制 |
| 2026-08-28 | v0.1.0 | 初版：对 S0～S11 全量文档审查后，把 Agent 调度之外贯穿多个 stage 的九个机制域统一沉淀；承接 L2 能量函数权威定义授权；记录已修正的两处 L3 残留与若干诚实缺口 | owner 指示：全面回顾 L3 设计理念，检查与 L1/L2 一致性，将跨 Stage 贯穿机制沉淀为单独的 L4 设计汇总 |
