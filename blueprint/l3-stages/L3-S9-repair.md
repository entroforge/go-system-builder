# L3-S9 根因修复与可信度重建

2026-10-27 合同更新：新 native Builder 采用下述 D 普通原稿与默认平台 worktree 方向。本文先固定目标合同；接线、安装投影和各项验收尚需分别完成，不表示当前生产或完整 S9→S7 已通过。既有 issued／冻结来源及已成立接收事实保持原合同。

## 1. 修复完成为什么需要超过一次代码修改

S8 已经解释问题机制，并以获批 RepairContract 固定要恢复的不变量和完整修复意图。S9 将这个意图变成真实实现、必要检查和可独立复验的交付，不重新猜原因，也不把修复意图降成“让第一个报错消失”。

局部补丁可能掩盖表象，却保留造成复发的机制。例如请求映射丢字段，页面默认值能改善显示，单独加别名也可能让一次请求成功，但重复字段定义、其他消费者和缺失的契约检查仍可能继续出错。完整修复要对准已确认原因，覆盖受影响消费者和必要兼容责任，并补上原来漏检的判据。

因此，S9 的完成需要连通五种事实：本人实施与自检、实际贡献被接收、Main 必要集成与检查、变更后的证据影响处置、独立定向复验。它们不能被一个“completed”或绿色测试数替代。成功之后仍回到新的完整 S7，确认修复后的整体交付成立。

S9 继承 [S8 调查](L3-S8-finding-investigation.md)形成的准确原因与批准意图，承载[设计原则](../l1-principles/L1-design-principles.md)中的统一事实、独立责任与有界纠错要求。它不代替需求修改、规格签署或发布授权；相应判断仍归原主体和阶段。

```mermaid
flowchart LR
    CONTRACT["S8 获批合同"] --> DIVIDE["Main 分工与原生派发"]
    DIVIDE --> BUILD["本人计划、真实前检、实施与自检"]
    BUILD --> CANDIDATE["自然事件经原消费者接收候选"]
    CANDIDATE --> FINAL["Main 集成与必要检查形成 Final"]
    FINAL --> IMPACT["专业影响与旧证据处置"]
    IMPACT --> TARGET["独立定向验证"]
    TARGET -->|"必要工作全部完成"| HANDOFF["原交接与新 S7 完整轮"]
    TARGET -->|"原因或范围问题"| INVESTIGATE["回 S8 重查"]
    TARGET -->|"执行条件阻塞"| RECOVER["恢复原验证责任"]
    RECOVER --> TARGET
```

## 2. 输入、参与者与几种不同的完成

### 2.1 从获批合同开始

输入应让实施者明白“为什么修、修到哪里、怎样证明”，而不是只拿到一个文件列表：

| 输入 | 在 S9 中的用途 |
| --- | --- |
| 当前批准 RepairContract | 确定主根因、被破坏的不变量、架构意图、范围、修复单元、断言和停止条件 |
| 原 InvestigationCase | 理解支持与排除原因的证据、影响范围、漏检原因及仍明确保留的不确定性 |
| 原 Finding 与补充现场，或原人类缺陷报告 | 了解实际偏差、修前事实、角色与数据条件，保持逐项复验的连续性 |
| 准确需求、设计与契约 | 维持正确行为和已批准边界，避免实现时悄悄改预期 |
| 真实代码、测试与工程环境 | 选择实施路径、前检、自检、集成检查和运行位置 |
| 当前执行与贡献事实 | 明确谁在做什么、依赖是否满足、哪些成果已接收或已集成 |

合同保存不等于批准，S9 不能在批准前先实施试探。进入后也不让 Main 再审批一次相同意图。实施发现根因、范围或断言确实有问题时，明确交回原调查／规格路径，而不是边改合同边写代码。

由 S11 人类缺陷决定进入的 Case 可以没有 Finding 或 ObservationBatch。S8 已核对原报告与冻结产品基线并完成调查、有效合同批准时，S9 直接读取原批准合同，使用同一 Session 与 Plan 准备流程；不能要求作者制造 Finding。空来源集合只反映报告的实际形态，修复单元、断言、范围、批准摘要及独立验证责任仍须成立。

### 2.2 责任分工

| 参与者 | 本人责任 |
| --- | --- |
| Main／修复负责人 | 阅读批准目标、选择专业分工与依赖、真实原生派发、处理阻塞、集成、必要 Main 检查、影响判断和阶段交接 |
| Builder | 理解本人单元与断言，制定计划、做真实前检、实施或测试、自检并交付本人专业结果 |
| 独立定向验证者 | 在修复后产品上验证全部必要断言，报告实际观察、未执行事项和结论 |
| Claude Code | 提供原生上下文、worktree、运行、消息与恢复 |
| 原消费者与 Runtime Writer | 提供准确题干和产物位置，一次记录专业事实与工程关联，维护并发、幂等和阶段更新 |

Main 不代写 Worker 的私人专业答案；Builder 不自行给出独立复验或整轮通过。原发现者可提供专业连续性，但实际验证者仍须未参与被复验实现与合并，不能用相同角色名或不同 ID 代替真实独立分工。

### 2.3 分清工程事实与平台执行

RepairSession 是围绕批准合同组织修复资料、结果与后续交接的领域记录，不是 Claude Code 平台会话，也不是额外的启动预约。RepairPlan 将合同单元变成可分派责任；它与 Builder 的专业 PlanReport 用途不同。

Session／Plan 文件中的初始状态保留创建时含义，不能据此判定实时工作仍停在规划。当前进度看原 Runtime 责任、已接收产物及实际集成事实，不能手改这些快照追平状态。

| 事实 | 已说明什么 | 尚不能说明什么 |
| --- | --- | --- |
| 原责任与题干准备完成 | 对象、工程 owner 和准确专业输入可交付 | 平台已经启动或本人已理解 |
| 专业 PlanReport 形成 | 本人有实施思路与实际前检记录 | 后续修改、自检或集成已经完成 |
| RepairResult 候选接收 | 本人实际专业结果与返回贡献已经记录 | Main 已集成、Final 已成立 |
| Main 集成与 Final | 对应实际交付进入目标，并记录必要检查结果 | 整批影响和独立定向复验已经完成 |
| Targeted 通过 | 已批准修复的必要断言得到独立实际验证 | 系统已形成 CleanRound 或获得发布授权 |
| Handoff 与新 S7 入口 | 修复阶段实际闭合，完整验证需要重新开始 | 新 S7 已经通过 |

共用工作五态仍是 `pending/active/blocked/done/cancelled`。修复工序、专业判定、贡献集成和整个阶段出口是不同事实，不能再造一套通用激活／完成协议把它们揉在一起。

## 3. 把修复意图编译为责任与依赖

### 3.1 先读不变量，再安排实施单元

Main 从合同的修复单元、作用范围、断言和依赖出发，辨认权威源、生成物、消费者、测试、兼容与迁移步骤，再选择 backend-builder、frontend-builder 或 test-builder。不能先规定“前后端各一个人”，再将批准意图切碎塞进角色。

若一个根机制涉及公共模型及多个消费者，应按契约形成机制安排依赖：权威模型先明确，消费者在可用基线上对齐，必要数据处理按兼容顺序实施，检测与回归由明确责任完成。两个单元可以并行，前提是输入已经就绪、写面和共享资源不冲突，且中间状态不会产生新的双重权威或不可运行产品。

同一单元需要密切共享上下文和原子修改时，应保持一致实施；不同信任边界、破坏性操作、工具环境或独立验证责任需要分开。并行度随实际依赖、写冲突、测试资源和平台容量变化，不是固定人数或先收齐所有人计划的屏障。

### 3.2 当前编译与 Main 的选择

当前原编译器通常为每个合同 `repair_unit` 生成一个 `RepairAssignment`，带出 scope、`depends_on`、`resource_locks` 和所承担断言。`assertion_ids` 必须明确列出，或用原 `all` 声明展开；不能默认为每项任务复制整份合同全部断言，从而失去单元责任。

这份计划是实际专业分工的基础，不代表已有任意多合同批次和高级责任合并能力。需要调整单元粒度时，必须保持获批意图及可消费映射；不能在实施中私下合并或删除责任，留下无法解释的完成分母。

Main 在准确分工工序中选择角色和工程 owner，由原消费者登记并准备专业简报。系统已知身份、版本、路径和关联自动带出；作者不用为换取开工反复复制哈希或制作额外登记信封。登记成功仍需 Main 通过 Claude Code 原生 Agent 真正派发。已登记依赖由原 Plan／TASK 交付，Builder 不再重抄一份依赖集合；新分工不要求 `check_location`，Main 检查由实际集成工序承接。

## 4. 原生工作区与专业材料怎样交付

### 4.1 任务交给平台，专业输入交给本人

新写入责任由 Main 携准确简报使用原生 Agent 的 worktree 隔离。简报固定准备时的 Main 提交，说明本人单元、断言、真实资料、计划与结果位置、连续工作及正常汇报的来源边界。

Worker 读取 Main 根下的原题干作为只读输入，在自己的实际原生 worktree 保存私人 PlanReport 和 Result。不能让隔离 Worker 写共享 Main 的问卷，也不能由 Main 用自己的判断替它填。专业文件留在各自原工作位置，产品提交只包含真实交付，不为格式要求提交整个 `.claude`。

```text
Main 控制根
├── 原批准 Contract、Case 和当前 RepairPlan
├── 本次准确 brief 与只读专业题干
└── 原 Runtime、接收后的专业产物、集成与 Final 事实

实际原生 worktree
├── 本人合法产品修改和测试
└── .loop-submissions/native-s9/<原Plan packet>/
    ├── plan.json   本人的 flat PlanReport
    ├── result.json 本人的 flat Result
    └── …           本责任的检查输出和私人中间产物
```

新责任由原准备在准确 source pin／brief 中固定 D 版本及上述两份普通原稿的准确路径；图中的身份占位不能抄成当前任务。Plan 的原 packet.AnswerPath、Result 的系统 typed 输入与作者 flat 文件分别处理：原 packet、binding、digest 和只读题干不原位改写，适配器调用原消费者，作者只填写本人这两项原有专业产物。已有 pin／brief、旧 issued／冻结路径和实际 C source-v1 锚继续按原坐标及原读取合同恢复，不移动旧稿、不把无锚旧任务追溯升级。新 D 任务不再默认创建 C 包或保源 Git ref，也不接管 WorktreeCreate／Remove。新责任不要求 launcher、bootstrap、平台 ID 映射、通用 Team、session 预约或首写激活。

原 PlanReport 接收回执识别本责任的原 Plan packet，source pin 固定本人原稿路径和实际来源；两者与登记执行一致时，系统据此确定上图唯一的本人私人运行产物区。本人可在其中保留前检、自检日志和中间输出，它们不是产品提交，也不是额外答卷。此区不等于整个 `.loop-submissions`，不能从目录名、角色或另一个 packet 猜成本责任。哪些观察支持专业结论由作者按题目引用；系统接收实际引用的证据，不要求把每个中间文件逐一引用、提交或登记。

系统在准备／真实来源观察边界检查准确候选是否被项目、父目录、info 或 global ignore 覆盖，以及 index、base 产品和获批产品范围冲突；实际存在的本人原稿须为 Git 原始 `??`。不改用户 ignore，不全目录豁免，不要求作者复制、提交、seal、done、额外 marker 或空 commit。任意 ignore 规则可能覆盖所有可选位置，此时明确 D 保留前提不成立及原恢复责任，不声称换个路径必然解决。真实未跟踪原稿是平台保树的有条件输入，不是不可变性、作者身份或完成事实。

### 4.2 准备起点与实际 Git 起点分别保留

专业准备时 Main 可能还没有合入前置单元；实际创建原生 worktree 前，依赖可能使 Main 向前。应确保实际工作区包含准备基线与已经满足的依赖，而不是逼 Worker 从过时快照开始。

原接收器在首次准确来源观察时核对该 worktree 与 Main 的共同 Git 起点，确认它包含准备提交，再为该来源后续重试和集成保留这个实际起点。来源记录的 `purpose=native_delivery` 表示“供原结果与集成消费者使用的真实坐标”，不创建目录、安装 bootstrap 或管理平台进程。

这些记录解释成果从哪里来，不成为额外开工许可。原生创建、运行与恢复仍归平台；Main 保留专业依赖、真实集成与阶段闭环。

## 5. Builder 怎样计划、前检和实施

### 5.1 先形成自己的计划，再连续工作

Builder 先阅读获批不变量、本人单元、必要断言、实际代码入口和原现场，确定实施顺序、预期修改、消费者、测试及停止条件。专业 PlanReport 只填写 `plan`、`red_checks` 和 `proposed_paths`，在实际工作树的指定位置保存。

计划应先于本人实施形成，但保存计划不是额外授权，也不要求等待其他 Builder 的计划。默认在既有授权内继续修复、自检和交付；真正高风险步骤或合同明确批准点仍依原条件处理。缺计划由明确提醒和原反馈处理，不能在 Stop 倒填一个时间来伪造事前工作。

下面是私人专业计划的结构示例，命令、结果和路径均须替换为本人真实工作：

```json
{
  "plan": "复核共享请求映射与已锁契约的差异，修正本单元字段过滤并检查调用方；补强字段保留判据，完成自检。若需要修改锁定契约或扩大批准范围，停止并交 Main 判断。",
  "red_checks": [
    {
      "name": "字段保留前检",
      "command": "npm test -- payload",
      "result": "fail",
      "evidence_refs": ["logs/payload-before.txt"]
    }
  ],
  "proposed_paths": ["web/src/orders/payload.ts"]
}
```

`red_checks` 是保留的字段名，实际结果可为 `pass`、`fail` 或 `blocked`。检查已经通过、问题不能在当前数据下重现，或没有执行条件，都应如实说明；不能为了形式修改产品来造红。结果若挑战了批准原因或正确行为，交 Main 判断是否回 S8，不由“非红”自动否定整个责任。

`command` 记录已经运行或实际尝试的前检，不是冻结给 Main 未来执行的命令。一次前检失败也不能代替后续逐断言的真实观察。无产品修改的合法测试责任可以用 `proposed_paths=[]`，不强造文件或空提交。

### 5.2 实施完整根因修复

每项修复单元都应持续回答：恢复了哪个不变量，解决哪些来源表象，哪些消费者和数据必须一起处理，什么检查能证明没有只遮住症状。

实施应落实获批的权威归属与交互边界，处理必要的生成物、客户端、服务端、持久化或异步出口。遇到重复定义、兼容别名或过渡双写时，遵守合同给出的责任、条件和退出安排；不能把临时分叉当成永久修复，也不能借“统一机制”扩为未经分析的大范围重构。

测试应保护错误机制：优先在最靠近权威与边界的层面建立可靠判据，再按需要补集成或真实入口回归。只有特定字面输入通过、只检查成功响应或放宽原预期，都不能证明漏检已经补上。采用隔离变体、回退夹具或等价反例时记录其真实用途，不能在目标产品上制造危险回归来凑证明。

兼容、迁移、回滚与发布顺序按合同中的实际义务处理。无需迁移时不制造迁移／回滚双引用；需要迁移时则要说明真实执行、幂等与数据保护、失败停止及恢复安排。提供一个引用不等于迁移或回滚能力已经验证。

## 6. 专业结果与自然事件接收

### 6.1 本人交付记录应如实表达完成与缺口

Builder 完成实施或测试、自检后，在指定私人 Result 写本人判断、逐断言修前观察、逐单元实施、自检证据、检测补强产物、偏差和残余风险。它不是 Main 集成报告，也不是独立 Targeted。

| 专业字段 | 内容 |
| --- | --- |
| `verdict` | `completed`、`blocked`、`contract_conflict` 或 `stale`，由本人据实际工作判断 |
| `before_fix_results` | 对题干中本责任实际断言的修前观察及证据；`observed_red` 是历史名称，不要求编造失败 |
| `unit_results` | 原 unit ID、真实 `pass/fail/blocked`、实施或测试摘要、证据与恢复的不变量 |
| `detection_gap_artifacts`、`evidence_refs` | 实际文件或输出引用的字符串数组，引用形式不证明已经执行 |
| `scope_deviations`、`residual_risks` | 已知偏差和未完成原因，没有时明确 `[]` |
| `migration_ref`、`rollback_ref` | 实际需要的已有依据；不适用时可为空字符串，不强制成对 |

Worker 应在专业摘要和证据中说明本人真正运行的检查及结果，不能因为结果题形没有让作者重抄机器检查清单就省略自检。系统带出的 Main 检查也不代替 Worker 的实际验证。

例如，一项仅承担 `root-1` 的映射修复单元可以形成下列专业 Result；示例不代表仓库中已发生这些事实：

```json
{
  "verdict": "completed",
  "before_fix_results": [
    {
      "assertion_id": "root-1",
      "observed_red": "修前共享过滤规则未包含当前必填字段，原载荷对照显示其在发送前消失。",
      "evidence_refs": ["logs/payload-before.txt"]
    }
  ],
  "unit_results": [
    {
      "unit_id": "repair-unit-mapping",
      "status": "pass",
      "implementation_summary": "映射已对齐现有契约；本人运行 npm test -- payload，字段保留检查通过，输出见所引日志。",
      "evidence_refs": ["logs/payload-after.txt"],
      "restored_invariant_refs": ["root-1"]
    }
  ],
  "detection_gap_artifacts": [],
  "evidence_refs": ["logs/payload-before.txt", "logs/payload-after.txt"],
  "scope_deviations": [],
  "residual_risks": [],
  "migration_ref": "",
  "rollback_ref": ""
}
```

这只完成本单元的职责；另有测试单元或独立复验时仍须真实执行。`completed` 不关闭问题、不宣布 CleanRound，也不授权跳过 Main 集成。失败或阻塞报告可以交接，结构错误仍需修正；有实际缺口时不能用 completed 让阶段前进。

### 6.2 自然事件接收原作者产物，不代作专业判断

Worker 在准确位置保存本人原 Plan／Result，系统在自然事件按原消费者条件接收。专业汇报可以引用原简报和本人结果，但返回引用或绝对路径不是额外接收动作，也不要求 submit、done、marker、完成字段、封存、重存或无关工具调用。`SubagentStart` 只补充当前责任和准确材料的短提示，创建、执行和恢复由 Claude Code 负责。

新签发的 Plan／Result 题面直接说明这一自然接收时机。已发行题面以其原完整模板字节及摘要重建，保留原 packet、专业 ID、签发事件和路径；同一当前责任重入沿原已发行卷与原资格接续，不因题面更新另签一卷。题面更新不修改专业字段、旧原稿或既有接收事实；未知模板摘要不作为原发行接受。

接收条件是当前准确责任、真实来源、原专业字段、必要观察和证据均满足对应原消费者校验。文件存在、JSON 可解析、有 verdict、工具成功或平台停止，均不能单独证明完成。Plan 可以先独立接收，Builder 随后继续实施和自检；Plan 接收不意味着 Result 或 Candidate 已成立。草稿允许继续编辑，普通工具始终可用；缺必要条件不接收，反馈按实际缺项指向本人原稿。系统不推测作者尚未写入的未来意图：已保存且满足原合同的答案可被接收，接收后的变更沿原修订／返工合同处理。

| 自然事件 | 观察和接续范围 |
| --- | --- |
| `PreToolUse` | 有界定位当前已识别责任并观察本人原卷；就绪时进入原消费者或续同一未完成 attempt，不作为工具 ACL、不遍历消费其他责任 |
| 成功 `PostToolUse` | 观察真实保存工具刚完成的原路径写入，补足 Pre 早于保存的时序；工具失败不认作保存，读取和任意成功事件不伪装成写入 |
| `Stop`／`SubagentStop` | 在来源可证明时兜底同一原责任，交付实际接收或未完成事实；Stop 本身不认定完成，不要求作者补返回路径或再 Stop |
| Main 的实际 Agent 返回／会话恢复 | 按原来源、回执和受支持恢复入口交接未完成部分；Main 的长集成与检查仍由 Main 执行 |

这些入口使用同一专业条件及原消费者。真实任务／actor、原已发行责任及来源根必须唯一关联，不能按角色、唯一候选、最近文件、Main 同名卷或目录扫描猜选。`SubagentStop` 的平台 `agent_id` 须非空且非 `Main`；原任务／简报引用、实际子会话与 cwd 提供的坐标须共同满足原来源合同。同仓 checkout、cwd 或返回引用各自只能提供对应坐标，不能单独证明本人责任、actor→worktree 所有权或完成。缺少可证明关联时保留未知，向 Main 交付实际缺口，不要求作者制造标记。

工程 owner、批准、runtime／generation／attempt、实际 root／branch／base／head 仍由原发行和消费者核对。新 D 从准确 live checkout 读取本人 flat 原稿，经原 Plan、Result issuer、Candidate／Writer CAS 接收，不借 Main 同名文件替代；旧冻结责任保持原格式和来源合同。已收及已固定输入的分流见 §12.1，不因每次事件重新执行完整接收事务。

Candidate 与 Main 集成共用同一产品 clean 视图。两份私人原稿仍须满足当前责任、原 packet、代次及实际根匹配，路径非链接／逃逸、非 index／base 产品、与获批产品范围无冲突，且原消费者已验证并固定的原字节一致，才能投影其准确 `??` 原稿。基于原 PlanReport 回执、source pin 和当前登记执行确定的本责任私人运行产物区内，除原稿外的未追踪内容按该责任的私人运行产物处理，不算产品变化；是否被引用、文件内容、名称或扩展名不另作 clean 的准入条件，也不逐项登记或冻结。其他 packet、未能关联的来源和整个 `.loop-submissions` 不因此获得投影资格。原始 Git 状态仍看见这些文件；真实产品的 tracked／staged／修改／删除／rename、原稿误 commit 和实际 base→head 产品 diff 不因私人区而过滤，产品 scope 和全部必要检查继续生效。

原稿、原本地证据、HEAD／branch 和 typed attempt 仍在各自原消费与检查边界重核：被专业结论引用的证据按原来源捕获与校验，不因是运行产物就跳过；未引用的中间产物不因此变成证据或新增逐项摘要。接收后改稿、检查期间改稿与引用证据变化不能沿旧摘要放行。专业原稿不借日志字段承载，不新增 accepted 账本。clean 视图不授予删除权；原 verified／ACK／cleanup_pending 对该责任工作区的回收授权成立后，本人私人运行产物可随原工作区回收，无需先引用、提交或手删。原稿仍保留到其原来源后继结束及原回收授权成立。

Main 的真实 `PostToolUse(Agent/Task)` 只有无失败的实际 completed 返回、派发 prompt 唯一准确原 issued brief、返回 agentId 非空且非 Main 时，才可按对应原来源合同在同请求原预算内复核并接续已经开始的原消费者一次。D 要求同一次平台结构化 completed 响应的 worktreePath／worktreeBranch 准确对应原已登记 execution，重核冻结 D pin 摘要及原 packet／责任；该响应将 actor 与本次实际工作树关联，不新增独立 actor 所有权保证。缺结构化路径／分支时明确本次自动接续无法证明，不能从 prose 补造。D 续同一准确 live source／attempt，不以 C anchor 为前置；历史 C 仍要求返回 actor 与其原 Stop actor 一致并复核不可变来源。不得从返回 prose、启动、角色或目录扫描首次造源；原 typed 优先、已收幂等、保留具体拒错，不要求作者重存或再 Stop。这不自动执行 Main 长集成和检查，不扩展到其他阶段或独立 Targeted。共享 Writer pending 仍从原配对输入恢复并刷新同一真实事件，pending 本身不证明作者或完成。

本人真实 Plan／Result 至少保留到原来源后继结束、verified／ACK／cleanup_pending 的原准确回收授权成立。Plan／Candidate 已收或 Worker 已返回均不授权提前删除最后一份 `??`。没有原稿、可准确绑定的自然事件或可执行的后继事件时，不伪造来源、不保证无人自动完成。旧 C 锚按原版本核原字节、证据和 Git ref；无锚、丢字节、冲突来源不能补造包或同名 worktree。这里仅适配本人已完成的专业 payload，Main 不填写判断，适配器不生成 PASS。

真实 Claude Code 2.1.311 正常 acceptEdits 的隔离合成 nochange／changed 已观察到原稿 `??`、自然 Stop→原消费者→Main 实际检查→Final、Impact 准备与清理；这不是当前生产已验收。OS sandbox 在该环境实际未启用，独立 Targeted、Handoff、fresh S7、blocked／异常及完整安装投影仍待验，不能从两例推成完整阶段完成。上述样本也不单独证明无需返回 Result 路径时的 Stop 来源定位：实际子会话、cwd、原引用可用性、子卷读取时机及 Main 接收渠道须用对应真实事件验证。来源尚未识别的任务不因此获得自动恢复承诺；Main Agent completed 仅续原条件下已开始的来源／attempt，不是任意首次接收的兜底。

若 Plan 已接收、私人文件后来丢失，原不可变接收事实可用于恢复；若从未接收且私人 Plan 缺失，不能借 Main 草稿冒充。真实 blocked 或 contract_conflict 即使暂时缺 Plan，也可以把具体缺口交回 Main，但不会因此取得本人完成或 Final。

原准备 source pin 不可读或缺失时，Main 的普通进度、返回和恢复反馈给出原 packet 确定的精确 pin 路径及实际错误，由 Main 处理原准备来源；Worker 不承担该系统恢复。保留已接收 Plan 及原责任，不从缺失 pin 猜测私人 Plan／Result 路径或格式，也不因此推断平台执行已完成。简报显示缺失同样不得抹除已收事实。

实际返回坐标只供原交付接收与集成使用。无法准确关联原责任、多个来源或来源冲突时由 Main 处理，不能按角色、启动顺序或不透明平台 ID 猜 owner，也不能把旧独立来源替换成 Main。

## 7. Candidate、Main 集成与 Final 为什么分开

### 7.1 接收本人贡献不等于集成完成

本人 Result 满足原接收条件时，原消费者记录 Candidate：实际交付来源、专业内容、已知产品变化和与原责任的关联。它保留本人交付，供 Main 继续核对与集成；不能因为候选已经存在就把共享产品标成修复完成。

真实未完成事项沿原 blocker／resolution 路线交接。原结果、阻塞记录、解决事实和新尝试分别保留，不能把历史存在过的 blocker 当作当前仍有效的阻塞，也不能用解决声明自动生成修复通过。

Main 应及时消费已接收候选，不等待 Final 才开始集成。Final 正是必要集成和检查完成后的事实；反过来等待会形成循环。Main 的阅读入口优先提供自己的分工、简报、已接收产物、原合同和恢复工作，不把 Worker 的私人空答卷交给 Main 续填。已接收 Plan 继续原生本人工作；只有实际已登记的旧冻结执行保留 workspace 恢复提示。原生简报丢失属于 Main／系统准备缺口，不据文件缺失推断应切回旧启动链。

### 7.2 Main 执行真实集成与必要检查

Main 发起集成时，系统先核对候选原字节与登记摘要，并沿原派发材料、当前已登记执行和候选来源关联确认同一责任及完整执行定义。合同、计划报告、原工作区、执行代际与来源提交沿已有自动关联核对；关联不成立时在 Git 修改前拒绝，保留 Main 和 Worker 现场。原 Runtime 投影与候选消费者共用这些判断，作者继续只提交专业事实，系统维护身份和摘要。关联通过后，专业结论、实际 Main 检查与 Final 接收仍各自承担原责任。

有产品修改时，Main 使用真实 Git 贡献，通过原 `runtime workspace integrate` 路线集成到绑定目标；系统提供准确 root、执行责任和工程 owner，Main 选择适合目标与实际改动的必要检查。例如：

```text
loop-harness runtime workspace integrate --root <实际Main根> \
  --assignment <系统给出的执行责任> --agent <原owner> \
  --check-command '<实际必要的Main检查命令>'
```

这属于真实 Main 操作，不藏在 Stop、普通 Pre 或保存回调中，也不能用参数与语义不同的通用 `task-integrate` 冒充。多个必要检查可逐项提供；检查要在实际集成版本上执行，记录命令、工作目录、结果和所检提交。

Main 不能把 Plan 中的前检命令直接当成后续检查合同，也不能在没有配置或没有运行时声称已经验证。原 Final 接收读取实际检查点和成功检查，确认当前 Main 与被检版本一致。Final 尚未正式接收时，若 Main 目标已前进，原集成工序保留同一 merge，并在当前目标运行实际必要检查；旧提交的检查留作历史，不混入当前检查资格集合。缺当前成功观察或当前检查实际失败仍不能交 Final。

阻塞与解除的历史回执核对原不可变产物、消息、证据和日志接收关联。当前源文件或标签变化不抹去已经成立的接收事实，查询也不要求本人重填专业答卷；原系统产物被篡改、记录缺失或日志关联断裂则必须明确报错。历史接收不等于当前执行授权：当前答卷选择与恢复提醒共用已有验证，只有经验证的解除事实才能退下原答卷并支撑后继恢复，指针或索引字样不能单独使旧责任消失。

Git 合并已经发生、合并回执写入前中断时，Main 沿原 `workspace integrate` 恢复。原 ready 中的准确来源与目标用于有界第一父历史辨认，后续普通 Main 提交不要求再次合并或再派 Builder。只有准确原合并关系成立，才接续本来的当前 Main 检查与 Final 接收；无法证明时保留现场交 Main，不能用候选、提交说明或“分支已经合过”替代证明。

Final 已由原 Writer 正式接收，则该贡献和当时必要 Main 检查已经完成。若进程此时中断，检查点可能仍显示 `verified`；Main 再执行原 `workspace integrate` 时，系统核对原已收回执、准确候选与未改变的检查证明，以原 CAS 补齐 `acknowledged`，再继续原安全清理。合法后续目标提交不要求另派 Builder、另交 Result 或改写原 `tested_head`／检查记录。

最后一项 Final 正式接收并进入原 `impact_reconciliation` 时，原完成出口先准备准确 ChangeImpact，再进入独立 ACK／cleanup；资源 pending 不能截断已解锁的专业后继。反馈同时交付 Impact 当前责任与具名清理阻塞。Impact 准备失败保留已收 Final，沿原准备入口恢复；清理重试不重收 Final、不重跑 Main 检查、不重写已准备 Impact。清理授权、未知 ignored 和枚举竞争边界见 [Worktree §9.2](../l4-mechanisms/L4-worktree-governance.md#92-清理只处理确切已接收资源)。

这项确认不验证后续新增变化。后续贡献由其 Main 集成及完整 S7 责任验证，不能借已完成 Final 声称当前全部产品通过。目标已丢失原 merge、Worker 来源有新提交、原检查被篡改或清理遇到新文件时，保留具体现场交 Main 处理；新选却未运行的检查也不能被说成已通过。原结果不因此失效为“未完成的 Worker 答案”，不另造恢复批准或要求本人重复劳动。

### 7.3 无产品改动不要求伪造贡献

合法测试或已满足的检查责任可以没有产品差异和新提交，仍应保存真实专业结果并做必要 Main 检查。它的完成根据实际工作判定，不通过空 commit、模式修改或无关文件来制造“贡献”。

整批责任都可能真实完成而没有产品变化。例如当前行为已经满足获批断言，本轮只补充真实观察，且不需要改产品、测试、规格或配置。每项责任仍须经过原消费者候选接收与 Main 必要检查；系统从完整已接收 Final 批次得到空变化集合，不能把“没有接收结果”当成“结果证明无需变化”。

Claude Code 可能在无改动 Worker 返回后立即回收其 worktree 和临时分支。原 `SubagentStop` 已准确接收候选，且 `source_head` 就是实际共同起点时，Main 可以从原 Git 对象接续这份零源差异交付，不要求私人目录继续存在。原集成器保留真实分支和路径记录，核对来源仍在目标历史中，在当前 Main 执行必要检查，再由原消费者接收 Final、确认和收尾；没有产品差异就不执行合并。目录仍在时继续执行原干净检查：真实产品新文件或源分支变化不能借私人区忽略，本责任私人运行产物按 §6.2 分类。尚未接收、存在源变化、原对象不可读或目标不包含原来源时，仍交 Main 处理准确来源问题，目录消失本身不授予完成。作者不为保留工作区制造提交，系统也不重建平台资源。

此时原 Changeset 的 `artifacts=[]` 和 Impact 的 `changed_artifacts=[]` 如实保留空集合，`change_types=[]` 不虚构一种文件变更。Main 仍在原 `decisions` 中引用真实 Case／Contract，解释为何无需修改、哪些依据保留、哪些断言需要独立复验；没有专业判断仍不能推进。空集合的内容摘要相同，系统用已有 Session 与来源坐标区分其不可变记录，作者不另填身份或维护证明。

无改动不表示合同义务自动消失。若原原因判断、规格或修复前提已经失效，Main 应回 S8 重新处置；若原合同仍适用，独立验证者仍观察全部必要断言，实际失败或未完成仍阻断。通过原 Handoff 后开启新的完整 S7，不能把零 diff 当作干净轮次，也不要求空 commit 或无关文件。

### 7.4 Final 作为后续变化事实来源

Main 完成必要集成和检查后，原消费者记录 Final。各责任的 Final 批次保留实际变化，同一路径的后续 Final 按原 last-wins 规则带出最终值；只在最后一项出现的路径不能取代整批变化，前面仍有效的路径也不能丢失。

ChangeImpact 和自动 Handoff 都使用这份已接收批次事实，不重新扫描 Session 目录、缓存或工作区来制造第二套变化权威。删除、配置、依赖和其他实际产品变化应在真实交付与 Final 中准确表达；不能在影响判断时再由作者手工猜一份差异集合。

私人 Plan／Result 的保存或路径返回不代表原消费者已接收。Main 使用 `loop-harness runtime repair status --root <Main-root>` 查询 `missing_reports`／`missing_results`；以原接收回执和 Runtime 状态区分缺少私人产物、待接收候选与已接收 Final。接收未完成时保留原 Worker 与产物，不能用路径存在代替接收。

Builder 分工表和独立验证者分工表故意沿用同一 Builder `assignment_id` 作为锚点。先选择 Builder，再在验证者表保留原锚点；保存时系统派生独立 verification assignment 和 TASK，不要手写验证者 assignment ID。

## 8. ChangeImpact 怎样重建证据可信度

### 8.1 对已知实际变化作专业判断

Final／Main 集成已记录而原 ChangeImpact 问卷缺失时，Main 修复提示中的来源或文件系统故障，然后执行 `loop-harness runtime repair impact prepare --root <Main-root>`。该入口只在当前 `impact_reconciliation` 恢复原准备：沿已接收 Final 批次与 Session 生成同一路径，保留已有专业答案，不重复登记结果、集成或改变 Runtime。普通 Pre／Stop／Idle 仍只读。`required_reverification_ids` 是系统从 Session 推导的合同级义务编号，与 Targeted 的合同断言 `assertion_id` 不同；缺卷时恢复准备，不从合同手拼这些机器编号。


合同中的预期影响只是规划输入。实际完成后，Main 要判断已接收 Final 变化如何影响行为、契约、消费者、数据、配置、依赖和既有验证，而不是再次填写一份自报 diff。

系统提供 Final 批次的变化集合，Main 完成原平面请求中的 `change_types`、`decisions`、升级层级和证据处置。有实际变化时应说明变更类型；整批无产品变化时允许 `change_types=[]`，`decisions` 和真实处置理由仍不可空。这里不加 Worker `answer` 包装，也不另起一份影响报告。系统已知的 Case、身份、变化路径和时间保留原投射，不让作者删掉机器字段来满足另一层准入。原接收复用已收 Final 批次的变化投影，保持路径与摘要关联，避免把其他贡献的变化记入本次影响；它不重新遍历产品或认证专业理由。这些关联仍由系统准备，作者只完成实际影响判断。合同级必需重验编号由当前 Session 推导，不因作者在原卷中留下另一组编号而改变；分析时间由接收器记录，不采用作者选择的时间。产物已生成而提交中断时，系统沿准确原不可变产物恢复原时间，再交原创建消费者核对完整内容；专业内容不同仍拒绝替换原产物。

| 处置 | 含义 |
| --- | --- |
| `invalidate` | 旧证据依赖的行为或输入已改变，不能继续代表当前产品；保留历史和失效来源 |
| `supersede` | 旧依据被明确的新事实替代，保留替代关系 |
| `retain` | 有实际理由说明输入与依赖未受影响，保留其当前适用性 |
| `reverify` | 需要在独立定向或新的完整轮取得新观察，不能把计划重验写成已通过 |

Main 应解释依赖关系，而不只是列文件。预期未改、实际新增、删除或伴随生成物都需判断其原因、风险与是否仍符合获批意图。偏差可以如实记录，不能由作者自行批准；发现根因、规格或业务授权问题时交原责任处理，不以路径差异一概拒收真实报告。

### 8.2 原题形中的专业决定

以下仅展示原 ChangeImpact 请求中需要 Main 填写的专业部分；实际工作在系统给出的原文件保留其他已知字段：

```json
{
  "change_types": ["implementation"],
  "decisions": [
    {
      "source_id": "web/src/orders/payload.ts",
      "target_id": "order-submit",
      "relation": "uses-request-mapping",
      "rule_id": "IM-REQUEST-MAPPING",
      "decision": "reverify",
      "responsibility_id": null,
      "scope": ["web/src/orders/payload.ts"],
      "rationale": "提交入口依赖已改变的映射，应重新验证有效字段保留及拒绝分支，旧成功响应不足以证明当前行为。",
      "recovery_evidence": ["logs/payload-after.txt"]
    }
  ],
  "escalation_level": "assignment",
  "invalidated_evidence_ids": [],
  "superseded_evidence_ids": [],
  "retained_evidence_ids": []
}
```

`source_id` 是实际变化或来源，`target_id` 是受影响对象，`relation` 表达依赖，`decision` 才是处置。`rule_id` 使用原 `IM-...` 规则标签形状说明本次依赖判断，不要求再登记一套规则。已有证据 ID 只引用真实对象；文件或输出路径也只是定位依据，不等于已执行或已恢复。

### 8.3 记录判断与应用失效分别可见

原消费者保存影响判断，按已知变化传播影响，对未被明确替代或保留的相关证据执行原失效规则，并应用显式失效／替代决定。它不把每一项专业理由再次升级成完整证据池注册或全账本精确集准入。

影响已记录但 Final 或 Main 检查还有未完成项时，仍留在影响工序并指出责任；完成后给独立验证者准确资料和题目。不能因影响文件存在就跳过实际修复，也不能为等待下一次 Pre 才准备下一责任而形成流程空档。

原影响提交读取这些已接受事实，不重新证明整份 Session 基线或所有权威文件的实时指纹。因此不能承诺任何外部修改都会在 Impact 保存时自动被判 stale。Main 仍需识别实际规格、原因或运行条件的变化，安排正确返工；不能因系统已接受一份影响判断就继续使用已知过期依据。

证据失效不抹除曾经发生的交付。历史贡献的来源与当前 PASS 的适用性是不同事实：旧结果可能不再支持当前质量判断，但它对应的 Git 贡献和原接收历史仍需用于审计和完整变化面计算。

## 9. 独立定向复验怎样恢复局部可信度

### 9.1 人与对象都要独立、准确

Main 选择未参与对应实现与合并的执行者，提供修复后产品、原合同、现场、Final、影响判断与全部必要断言原文。它可以是原发现者，也可以是有充分背景的另一名独立者；原发现者连续性是专业帮助，不是 owner 池资格。

D 的 Builder 路径不改 Targeted 的独立专业职责。新 Targeted 以共享 Main checkout 的独立原生上下文为本轮正式接线主线，读取 Final 最终产品，只填写原责任准确提供的普通唯一直卷，不交 Builder Plan／Result、不新增产品提交。新普通路径为 `.loop-submissions/s9/<runtime>/g<generation>/<sourceDigest>/targeted-<performing-assignment>.json`；路径中的内部关联由原准备入口提供，作者不拼接编号或计算摘要。Main 不代填独立观察，也不复制成第二作者稿。

Targeted 不是 S7 issued packet，不能借 S7 模板或 Builder source 推断版本。原责任须明确固定 authoring source contract：原 Runtime／代际、Case／Session／Plan 来源、Impact、original／performing assignment、verifier owner、作者根、准确原卷及所检 Final 产品的关联。优先复用原权威责任和已冻结来源中能够证明这些关系的字段；确有缺项才在原准备／Writer 边界扩展必要来源合同，不新建接收账本或平台运行状态。新旧协议必须有明确、可复核的责任来源；无新协议的历史责任继续读取原 Main 敏感路径，不能按文件存在猜版本、迁移旧 blocked 原卷或重写 accepted 数据。此为实施合同，完整安装与正常权限下的真实 Targeted→fresh S7 仍需验收。

现成来源可复用原 verifier dispatch 的 TASK：其路径／字节摘要已由原登记器记录，Agent 的 task_ids 和 Team 的 manifest_ref／manifest_sha256 给出原分工关联。新 verifier TASK 可携带专用机器 `targeted_authoring` 协议声明，由系统生成并随原 TASK 摘要固定；selector 必须核原 TASK、manifest、owner、performing assignment 与当前修复来源，不把可变分工问卷当权威。旧 TASK 无声明即保留旧协议，未知声明拒绝，不额外增加 Runtime 作者状态。dispatch 可能早于 Final，因此最终产品 HEAD 必须在 Targeted 真正准备与接收边界从原 Final 事实核对，不能用早期 TASK 证明未来产品。具体字段与原 TASK 校验须在实现时共同验证。

共享根的产品检查只凭原责任 source contract 证明准确控制路径，核对普通 `??`、非 HEAD/index、非产品范围冲突；未知邻居和 tracked/staged 同名不得豁免。不得扩大全目录 ignore 或产品扫描排除。真实 native worktree 若缺已成立的独立 verifier source binding，反馈准确未接收；不能回落 Main 同名卷、套 Builder adapter 或把 Builder 树当已检 Final。已有 frozen 责任保持原源与原恢复合同，不强制升级为新 worktree。

原分工记录工程 owner 与验证责任，Main 仍通过原生 Agent 实际派发。验证者只完成原 Targeted 请求，不修产品来使结果通过，不代写 Main 影响判断。保存本人就绪原答卷后，Pre 有界观察调用原 Targeted 消费者，真实保存的成功 Post／Stop 在准确来源成立时兜底；正常汇报可引用原卷，不把返回路径设为额外接收手续。原字段、必要断言、Final／Impact 来源及独立性均须满足原接收条件，保存或停止本身不代替这些判断。

原接收沿既有分工解析当前工程 owner 及其原别名；答卷中的责任名称本身不能代替已知执行者。无法对应当前责任时，由 Main 修复原交付来源，不让作者猜身份、补一套登记或重交专业内容。当前提交可用已记录实现者发现明显独立性冲突，并保留不独立报告但不据此推进。它不能只凭两个 ID 自动证明实际平台上下文或合并者独立，真实分工仍由 Main 负责。

### 9.2 复验全部必要断言

定向验证包含三层核心问题：原表象是否消除，根因不变量是否恢复，漏检机制是否得到补强；再完成适用兼容、迁移、回滚与影响处置要求。不能只读 Builder 总结或只复跑一条单元测试就宣布全部通过。

系统给出 `symptom-N`、`root-N`、`gap-N` 等原槽位和断言原文。作者报告真实结果与证据，不自行发明一套 ID 或靠足够数量补数。缺少必要观察会明确反馈，额外观察不成为拒收理由，也不能掩盖原必需项的失败。

原 Targeted 上下文同时交付获批合同的 `stop_escalation_conditions` 原文与准确来源。Main 的复验交接和携带准确 Targeted 引用的原生启动都使用这项材料，不按角色名称猜责任。自由文本停止条件由独立复验者结合实际观察判断；它不是第四组必填断言，不要求作者复制合同摘要、下标或第二份评估数组，也不能据一个布尔值证明已经正确理解。

触发条件说明需要扩范围或改合同，就在原结果、原因和证据中明确报告 `scope_changed`，沿原 S8 重查与批准路径处理；真实环境原因使用 `blocked` 并保留未执行项。不能把触发条件仅记为残余风险后仍宣称 PASS。来源不可读或发生变化时由 Main 恢复原准备，不要求独立者改写正确答案。

下面展示原 Targeted 请求中的专业内容，不是另一个 Worker `answer` 信封；实际在提供的请求中保留系统关联字段：

```json
{
  "assertion_results": [
    {"assertion_id": "symptom-1", "result": "pass", "evidence_refs": ["logs/order-journey.txt"]},
    {"assertion_id": "root-1", "result": "pass", "evidence_refs": ["logs/mapping-invariant.txt"]},
    {"assertion_id": "gap-1", "result": "pass", "evidence_refs": ["logs/regression-discriminator.txt"]}
  ],
  "result": "pass",
  "failure_class": ""
}
```

每条引用应定位本人的实际观察或输出。未执行、失败或阻塞时如实填写 `fail`／`blocked`，整体结论也保留实际非通过；不能把示例 PASS 当默认答案。`continuity_reason` 与 `scope_compliance` 是可选兼容元数据，不能要求作者编造非空或 pass 来换取接收。记录时间由原接收器生成；已记录环境解除的新尝试沿原解除事实选择位置，未提交的同一原产物重试保留其时间与字节。接收时间不反推检查已经执行，真实观察仍由本人证据说明。作者无需追加时间、连续性证明或回执。

### 9.3 记录报告与允许推进是两件事

原消费者记录真实结果，按当前独立责任的最新尝试和合同必要观察判断完成。已解决环境阻塞后，同一原责任可以产生新的不可变尝试；旧失败保留历史，不永久污染新通过，也不能让一项当前失败被另一项 PASS 隐藏。

只有必要观察都真实通过且独立性成立，才进入 `ready_for_full_review`。有遗漏时继续当前验证，有环境阻塞时处理执行前提；非阻塞失败则根据实际原因回 S8 重查，不在错误因果模型上无限补丁。

定向 PASS 只建立该修复合同的局部因果可信度。它没有判断整个当前需求的交付完整性、工程质量和全部实际行为，这仍由新的 S7 独立完成。

## 10. 完成出口与新的完整验证轮

### 10.1 交接依赖哪些实际工作

S9 完成需要本人单元实施／测试与自检结束，必要 Main 集成和检查成立，专业影响判断已记录，独立验证者完成全部必要断言，且没有未履行的真实决策或阻塞。报告接收、文件存在或阶段标签本身都不能替代这些事实。

最后一项有效 Targeted 完成时，原出口自动汇总 Handoff，不另发一份 Main Handoff 答卷，不要求手工 create／commit，也不靠下一次普通 Pre 补跑整个业务链。

原 Handoff 使用已接收 Final、Impact 与 Targeted 事实。内部写出产物、更新指针与轮次由原 Writer 协调，并发或中断恢复对作者保持原同答语义；重复观察不能重复增加轮次或登记新交接。汇总的 Session、Plan、Contract、Final 与 Impact 保持原接收来源关联，列出的定向观察也必须来自已接收记录。这是已有机器事实之间的关联核对，不是要求作者列齐全部历史尝试，也不重新执行专业复验；阶段完成仍读取原消费者已经形成的当前工作事实。

### 10.2 新 S7 基线不能只覆盖修复小片

有规范 S6 贡献链时，新基线继续使用原共同实现起点到修复后 Main 的完整产品变化，包含原交付、失败残留、删除和实际修复，而不只把本次最后一个修复文件交给验证。历史交付当前质量失效，也不应因此丢掉其真实贡献来源。

TR-012 保存 Handoff、新轮号与 ReviewPlan 种子，清除旧轮通过投射，进入新的 `verification.running`。种子提供机器已知来源与待细化字段，不生成任何新的 Delivery／QA／E2E PASS。不可变种子必须满足原正式计划 Schema，因此其中的 `cold_start` 是结构占位；`PLANNER-REFINE` 保持派发门关闭，Main 在首次专业登记时决定实际资产安排。普通 S7 未决草稿仍留空，不继承这一占位决定。

TR-012 安装的 `review.plan` 种子指针不等于首次专业登记。当前问卷准备、普通保存发现、重放判断和状态提示都须识别这份准确种子，从它生成作者副本并保持身份与固定基线；不能因指针已存在而跳过问卷或把首次专业细化误判为受控修订。真正的专业登记仍交原 RegisterPlan 消费者核对。

Main 应依 [S7](L3-S7-verification-round.md)完成首次专业规划，保持种子身份、轮次、代际和固定产品基线。原未启动种子首次登记与发现后的受控修订不同；实际不需要浏览器时给出有来源的 N/A，不能把保守 cold-start 提示变成虚构浏览器工作，也不能误删适用交付和 QA 命题。

当前规范产品读取仍依赖原 TR-003 准确 TASK 锁集。正式 TASK 源需要重锁而相关读取尚未衔接时，不能仅凭 Assignment 或修复计划修订宣称新 S7 已获得有效基线；应明确原来源缺口，不能降级为空变化面绕过。

### 10.3 预算与人类决策

完整验证反复不收敛时，预算用于暴露产品、设计或治理问题。当前 S9 交接提交处检查完整轮上限；达到限制则保存已完成工作并进入原预算决策路线，由人类明确增加预算或返回治理，不自动降低断言或伪造通过。

修复过程中的常规计划、自检、真实结果接收和已授权集成继续执行。真正需要需求取舍、超出授权范围或发布决定时，先准备可审材料再交原主体，不能把每次保存和每个文件都扩为人工批准。

## 11. 失败应回到能解决它的责任

| 情形 | 下一责任 |
| --- | --- |
| 本人检查或修复未完成 | Builder 保留真实报告与阻塞，Main 根据明确缺口安排原责任继续 |
| 需要的实施范围或合同断言与获批原因冲突 | 停止受影响工作，由 Main 回 S8 修订原因或合同，不由 Builder 自行扩权 |
| 实际规格错误但 REQ 不变 | 回 S2 重建正确依据及受影响验证 |
| 正确行为需要改变锁定需求 | 原人类暂停与需求修订路线 |
| Main 集成冲突或检查失败 | Main 处理实际集成／验证，必要时安排有依据的返工，不代写 Worker 结果 |
| Targeted 的环境／权限／依赖阻塞 | 保留原验证责任，恢复条件后提交真实独立新尝试 |
| 定向结果表明原根因仍在、原因改变或范围判断失效 | 携实际失败证据回同一 S8 Case，重新判别并形成需要的新合同 |
| 来源或阶段已变，旧答卷不再当前 | Main 走原修订或恢复，不能借旧回执给新内容签字 |

S9 的非 `blocked` 定向失败通过原因果重查衔接保留原 Finding 集合和失败证据摘要，撤下旧合同当前绑定，之后再形成新假设与合同。环境阻塞不证明原因错误，不能无条件走相同回流。

已经发生的专业计划、结果、检查和批准不能在事后补造。跳过前置工作时如实记录偏差及其影响，保持可恢复事实，而不增加另一个开工协议来假装历史已正确。

## 12. 重放、阻塞解决与历史执行恢复

### 12.1 本人接收与当前有效性

自然事件先按原接收事实分流，复用原 Plan／Result 回执、typed attempt 和原恢复入口，不新增 accepted 账本或缓存：

| 原事实 | 本次观察或恢复 |
| --- | --- |
| 尚未固定输入、尚未接收 | 就绪时调用原 Plan／Result 消费者；专业字段或来源被明确拒绝后，原作者可在本人真实来源更正原稿，自然 Pre／成功 Post／Stop 按更正后的原稿重新适配。非空 typed 文件本身不证明 Writer 已固定事务 |
| 原 Candidate 已暂存或 Writer 已有 pending，事务未完成 | 续同一个原 typed attempt 和 Writer；后来 flat 改稿不能覆盖固定输入，缺原输入按原恢复交接，冲突交原修订或返工 |
| Main 以原准确输入执行显式技术恢复 | 复用该恢复入口已识别的原 typed／pending 及原消费者；不能把 Main 看到的后续 flat 当成本次技术重试的新专业稿。此前仅发行的空 draft 不算已保存输入 |
| Plan、Candidate 或 Final 已接收 | 从经验证的原回执投影已成立事实及 Main 后继，不重写 typed 输入、不重复完整接收事务；Final 也不重做已完成 Main 检查 |

分流以原责任、实际 source、已接收回执及原 Candidate／Writer 事务事实为准；提示、诊断或路径存在不能替代接收、暂存或 pending 的证据。首次尚无 execution 的实际 Stop 仍走原接收建立来源，冻结独立执行及已 complete execution 保留各自原出口；旧 C retained 和新 D 私人原稿按各自原来源资格验证。自然更正与技术恢复拥有不同输入语义，不能只因输入路径或 packet 相同就合并。

原适配首次从未保存的答卷生成 typed 时，与原 Result 消费者共用原工作区锁；消费者若明确在生成 Candidate 之前拒绝，则仅在本次刚写出的 typed 字节仍一致时恢复原空 draft。对既有非空 typed，本人真实自然事件的原稿有可接收形状且内容已改变时，先在同一原锁和原预算内核原 Candidate 目录中属于该 packet、执行代次及来源的完整摘要／provenance，再决定是否适配新稿；不能以旧 typed 本次是否被拒错判断过去有没有暂存。确认无该暂存事实才能重新适配更正稿；发现暂存、读取失败或预算不足时保留原 typed 并交付原因。已收回放、正常空卷和未变化观察不为此遍历历史，Main 的技术重试仍使用原 typed；不新增缓存、账本或作者手续。

相同专业内容的重试不重复产生 Candidate、Final、影响或交接。投影既有接收事实仍须保持原不可变产物、字节／摘要及来源关联；实际仍存在的私人原稿按原消费者合同核对，不能因同一路径曾成功就替改写后的内容签字。来源已按原合同清理时保留历史接收事实，不要求重建树或补填原稿；未接收而缺失的原稿不能冒充已收。

这不把真实漂移移出原检查：产品 HEAD／branch、证据、候选关联及当前集成资格仍在对应原消费／集成／检查边界核对；冲突须指出具体答案或绑定位置并交给实际接手者。普通已收观察只复核其适用的原事实，不重跑完整接收、全部日志散列、Gate 或 Main 集成来制造新进度。Targeted 的已收 blocked 重放和环境恢复后新尝试沿 §12.2 的独立合同。

阻塞仍属于当前责任也需要事实支持。答案、来源、合同、阶段或解决状态改变后，不能仅凭历史 blocker 存在继续声称当前有效。Main 通过原解决工序记录实际措施，Worker 继续本人缺失工作，原消费者区分旧尝试与新事实。

### 12.2 独立复验的环境恢复

正常 Targeted 入口识别已记录环境阻塞。环境确实恢复后，独立验证者在原题目保存新的实际结果，自然事件监听使用已有 Resume／Create／Commit 路线接收就绪答案形成新尝试；成功 Post／SubagentStop 兜底，相同 blocked 重放不改变 Runtime，也不假称已恢复。

需要显式恢复时，按系统给出的原 `targeted resume` 责任处理。恢复声明不是 PASS，必须有后续真实观察。原因或规格变化则使用调查／返工，不把这种变化塞进“环境已解决”。

### 12.3 恢复事实不接管平台生命周期

已接收且实际集成完成、工作树已清理后，迟到的 Stop 保留原接收事实，不重新创建工作区或重新让 Builder 填卷。缺失的未接收私人产物与已经保存的权威结果要分开判断，不能一律从 Main 借答案。

旧登记 execution 的 launcher、固定来源、bootstrap 或恢复坐标仅服务原记录；新原生职责不沿该链取得开工资格。Hook 不自动重启 Worker，不隐藏运行完整测试或合并，不用普通工具事件逐次执行 ACL、身份或全来源证明。

共同事务与修订边界见[状态转换核心](../l4-mechanisms/L4-state-transition-core.md)、[修订机制](../l4-mechanisms/L4-revision-usage.md)和[工作区治理](../l4-mechanisms/L4-worktree-governance.md)。这些机制维持事实与恢复，不替代专业原因、检查或批准判断。

## 13. 怎样观察修复是否在收敛

| 观察点 | 需要看见的事实 | 帮助判断的问题 |
| --- | --- | --- |
| Main 分工与简报 | 原合同、就绪单元、依赖、owner、准确工作资料和私人产物位置 | 尚未分工、尚未派发与平台执行问题 |
| 本人计划与前检 | 实际方法、pass/fail/blocked、预计改动与偏差 | 是否理解原因，还是只有泛化修复意向 |
| 自然接收与候选 | 当前准确责任、真实来源工作树、原卷与专业接收反馈 | 来源未识别、答案未就绪、接收缺口和候选待集成 |
| Main 集成与 Final | 实际贡献、所检版本、真实命令、检查点与 Final 记录 | 已接收但未集成、已合入未验证和实际完成 |
| 影响处置 | Final 批次变化、专业理由、失效／替代／保留与必要重验 | 旧 PASS 是否仍在替新实现背书，是否遗漏邻接面 |
| 独立定向与出口 | 必要断言、实际结果、未完成项、阻塞解决和 Handoff／种子 | 局部通过、完整定向通过和新 S7 入口的区别 |

当前原 `runtime repair status` 和相关图投射展示已有 Session／Plan、本人责任、候选、结果与下一动作。正常作者从系统给出的准确资料开始，不需要先研究底层恢复命令；状态查询也不能靠反复准备工作、求整个来源证明链或扫描目录来制造进度。

维护时应关注根因反复重开、漏做消费者或迁移、Main 待集成积压、修复后同类问题复发、合同在实施中大改以及无意义重复测试。指标用于定位设计或资料不足，不用工具调用数、修复文件数、造出的红测试数或人数配额替代质量。

## 14. 实现对应与当前边界

### 14.1 先确定职责层，再安排提示

S9→S7 的指引按以下层次维护。同一专业产物及其接收条件只在当前职责的准确题面定义；上层提供导航和责任边界，不能另写一套路径、字段包装或接收时机。原消费者决定事实是否成立，提示与角色名称不能代替实际批准、独立性或完成条件。

| 层次 | 内容与主要落点 | 不应承担的内容 |
| --- | --- | --- |
| 阶段规范 | 本文、`agent-protocol.md#s9`、原合法转换定义：输入、责任、完成和失败去向 | 把旧执行方式混入新原生工作的必经步骤 |
| 通用入口与方法 | AGENTS／loop 模板及手册、agent-dispatch／loop-orchestration／bug-resolution：识别当前职责并导航到准确材料 | 重复字段字典、另发报告、每次工具调用重证开工资格 |
| 角色能力 | backend／frontend／test Builder：实施能力与独立验证能力按实际派发区分 | 用 test-builder 名称推断必须实施，或禁止已明确派发的独立 Targeted 结论 |
| Main 工序 | 分工、Candidate 集成、ChangeImpact、独立复验派发的当前问卷与反馈 | 代写 Worker 观察、把分工登记说成平台已启动、让已完成 Worker 承担 Main 集成 |
| Worker 题面 | 原 native brief、Plan／Result 专业题干、Targeted 原请求：准确输入、作者字段、实际输出位置及来源关联 | Main 只读题面与私人输出混用；同时要求 flat JSON 和另一个 answer 信封 |
| Hook 接收与诊断 | Start 短提示、Pre 高频接收与成功 Post／Stop 兜底、Main 返回摘要：本次接收事实、未完成项、责任人与准确恢复入口 | 用普通工具保存增加 native Worker 激活手续、阻止真实未完成返回、因退出而制造完整结论 |
| 原出口与下一阶段 | Targeted 完成、Handoff、预算应用、S7 问卷准备和 Runtime next_action | 重交已接收专业结果、把 seed 当 PASS、让正常 S7 同时走保存与手动登记两条入口 |

Builder 以私人 brief 中的实际输出位置填写顶层专业字段，原题面只提供问题与来源；独立验证者以当前 Targeted 请求为唯一答题入口，不交 Builder PlanReport，不改产品来取得 PASS。Main 保存 ChangeImpact 时接收，native Worker 的 Plan／Result 或 Targeted 由 Pre 高频监听就绪原稿、成功 Post／Stop 兜底接收；“保存”与“接收”不能混写。

指引应优先交付当前动作所需的 Contract、已接收 Final／Impact 和相关证据引用；断言只展开一次，其他材料提供来源导航。不能用重复输出整个证据登记表替代明确当前责任，也不能为了缩短提示遗漏必要断言、停止条件或失败恢复。

### 14.2 收尾、未完成返回与恢复边界

首次 native 返回即使没有可接收的完整答案，也应将真实未完成工作与原拒绝原因交给 Main。原消费者继续拒绝不合格结果，不生成完成回执或推进；Hook 不以第二次 Stop、虚构 blocker 字段或凑齐结论作为退出条件。Main 摘要须关联准确 brief／answer、已接收事实与具体缺口，区分恢复来源、继续原专业责任和 Main 收尾，不只让 Main 猜测平台是否仍在运行。

Targeted 登记和 Handoff 完成是两个可能部分成功的步骤。最后一次有效 Targeted 在原完成出口尝试 Handoff；失败保留已接收 Targeted，返回可重入的原收尾入口及已有 Handoff 来源，恢复只补未完成交接，不重跑独立观察、不新增作者报告、不重复增加轮次。状态查询保持只读；Pre 仅在本次就绪原答卷接收后续原出口，不在无变化观察中反复重跑交接。

当前原恢复入口为 `runtime repair handoff complete --root <Main-root>`，反馈提供实际 Main 路径。它复用原 Handoff 消费者与 S7 准备；仅在真实未完成交接或准备失败时使用，不成为正常作者额外提交步骤。尚未通过的 Targeted 不获得这一交接指令；已登记 S7 计划也不因迟到返回再次要求填卷。

触及完整轮预算时，当前完成出口准备原预算应用并交付准确路径；空选择不授权扩额。原真实决策接收后续接未完成交接。若准备本身失败，明确其原恢复入口。成功 TR-012 后交付新 S7 seed 及原专业 ReviewPlan 问卷入口；正常 Main 填写并保存该问卷，不再要求另一条手工 review-plan 登记。

### 14.3 本轮缺口与分层修复验收

以下是本轮审查发现及修复验收项，不是额外的 agent 提交要求。修复必须同时检查相应提示来源、真实接线与失败反馈，避免只改文案留下旧 next_action。

| 已发现缺口 | 修复归属 | 验收观察 |
| --- | --- | --- |
| 独立 Targeted 使用 test-builder，但角色禁止独立结论并泛化要求 PlanReport | 角色／方法层 | 明确两类职责；独立验证只提交 Targeted，实施者仍履行 Plan／Result |
| 原 Plan 题干的 answer 包装／路径与 native 私人 flat 输出不一致 | 作者题面层 | 题干与 brief 对当前输出一致；冻结旧来源保持其原恢复兼容 |
| S9 首次不完整 Stop 仍阻拦，Main 只得到泛化诊断 | 接收／返回层 | 首次可返回，拒绝与具体缺口可追到原责任，绝不冒充完成 |
| brief／Start／恢复仍把返回路径当必需，或用 cwd 代替来源证明 | 原题面与自然事件来源层 | 不增作者完成信号；真实平台核子会话、cwd、原责任与 Stop 读取时序；无法证明时交 Main 准确缺口 |
| 已收 Result 的连续 Pre／Post／Stop 重复完整接收或覆盖固定 typed 输入 | 原消费者与恢复层 | 未收、固定未完成、已收分别沿原事实分流；计量公开事件耗时、读取及消费者调用次数，漂移仍在原边界拒绝 |
| Targeted 已登记但 Handoff 失败后缺少明确重入；预算应用未在该出口准备 | 收尾层 | 部分成功、重试、预算决定和准备失败均有原恢复；轮次只增一次 |
| Targeted 指引仍称 on save，S7 next_action 仍要求手动登记 | 动态提示／阶段边界 | 就绪 Worker 原稿由 Pre 监听接收，成功 Post／Stop 兜底；新 S7 原问卷沿原登记器接收 |
| 中断重试的 Changeset 引用多出查找 ID；S7 将机器种子指针误认成已登记专业计划 | 原引用／S7 入口层 | 首次和重试使用同一引用结构；种子副本可直接进入原专业登记，原种子保持不可变 |
| 断言重复展开、证据候选不分当前相关性 | 提示投射层 | 完整保留当前问题及来源，减少重复材料与无关证据扫描 |

本次边界复审继续按上述层次收敛，以下约束同时适用于正常提示和失败恢复：

| 复审缺口 | 责任边界与修正 | 验收观察 |
| --- | --- | --- |
| native flat 答案失败后收到内部 `/answer/...` 与旧包装修复指引 | native 接收适配层将原消费者字段反馈映射到本人实际私人文件；旧消费者及冻结执行保持原格式 | 缺字段时准确指向私人文件的顶层字段，不诱导添加 answer 包装；拒绝事实不变 |
| Worker 相对日志引用被提示层直接拼到 Main 根目录 | 原接收／证据交接保留实际来源工作区；本地引用明确基准，读取端不猜 Main 路径，历史未解析引用明确显示来源限制 | Worker 与 Main 存在同名不同内容日志时仍能定位本人观察；URI 保持原义，不以提示拼接冒充证据搬运 |
| 提前登记独立验证者后立刻要求启动，但 Targeted 尚未准备 | Main 分工提示按当前责任区分登记完成、等待 Final／Impact、题面准备缺口与可实际派发 | 只有已进入 Targeted 且原题面可用才要求现在派发；缺卷沿原登记保存恢复，不让验证者猜卷或空等 |
| S9 seed 等待完善时，普通 Pre／S7 status 隐式创建问卷或 guide | 普通投射只读；S9 完成出口与显式 handoff complete 拥有新 S7 准备 | 缺文件时查询不写文件、不推进 Runtime，显示准确恢复入口；完成出口仍能补齐原问卷和 guide；当前 TR-012 的计划指针缺失也不得回退生成通用 S7 草稿 |
| 通用角色实施要求、平台归因与 before save 说法覆盖实际责任 | 在要求所在句限定实施职责；Main checkout 边界是本工作流约定；Targeted 完整性由原消费者判断 | 独立验证不被要求另交计划或提交产品；普通保存可保留未完成工作 |

这些修正不增加作者报告、审批或工具禁令。已有真实证据由系统按来源交接；缺失材料明确由原责任恢复，不要求作者编造观察来满足结构。历史已接收产物保持不可变，不能为改善提示而重写其字节或把未搬运的旧引用宣称为已归档。

新 native Result 的顶层及逐观察 `evidence_refs` 中，本地文件以登记的 Builder 工作区为基准。推荐 `path:logs/check.txt`，也接受含 `/` 的相对文件路径及该工作区内绝对路径；单文件名写作 `path:check.log` 或 `./check.log`，没有路径标记或 `/` 的裸词保留为已有标识。原接收器将真实文件内容保存在 Main 的 `.claude/evidence/s9/`，在系统 provenance 中记录原引用、来源相对路径和归档摘要；D 保留原 Candidate 的本地 evidence capture 及 Final hash 核验边界，不以活树替代不可变证据；既有 C 锚按原合同承接已固定的同一内容。作者答案及其摘要不改写。URI 保持原义，缺失本地源明确反馈原工作区与恢复责任。普通提示只投射登记的来源和归档位置，不重复读取、散列全部日志。此交接不扩大到 Plan-only 引用、迁移／回滚依据或其他任意字符串；这些材料保留原字段语义。已登记冻结执行沿用原接收要求，不追加本地文件归档准入；其未归档引用保留原来源说明。

### 14.4 实现索引与保持的边界

| 环节 | 当前主要落点 |
| --- | --- |
| 合同、执行计划与责任 | [领域对象](../../internal/repair/model.go)、[Session／Plan 编译](../../internal/repair/session.go)、[Main 分工](../../internal/cli/repair_dispatch_questionnaire.go) |
| 原生简报与返回 | [native_s9.go](../../internal/cli/native_s9.go)、[本人 Plan／Result 题干](../../internal/cli/repair_result_questionnaire.go)、[阅读顺序](../../internal/cli/s9_execution_listener.go) |
| 私人答案纠错与证据交接 | [历史 C 不可变技术来源兼容读取](../../internal/repair/native_s9_source.go)、[native 字段反馈](../../internal/cli/native_s9_artifact_feedback.go)、[Result 本地来源保留](../../internal/repair/s9_local_evidence.go)、[来源记录](../../internal/runtime/repair_result_types.go) |
| 候选与 Final | [原结果消费者](../../internal/repair/issued_repair_result.go)、[交付接收](../../internal/repair/workspace_delivery.go)、[Result 记录](../../internal/repair/result.go) |
| 影响与定向 | [原 Main／Targeted 工序](../../internal/cli/repair_lead_questionnaire.go)、[独立分工](../../internal/cli/repair_verifier_dispatch_questionnaire.go)、[影响及复验产物](../../internal/repair/evidence.go) |
| 批次、退出与新轮 | [Final 变化归并](../../internal/repair/artifact.go)、[Runtime 消费](../../internal/repair/runtime.go)、[Handoff](../../internal/repair/handoff.go)、[S7 种子](../../internal/repair/s7_seed.go) |
| 原出口恢复与指引 | [收尾与 S7 准备](../../internal/cli/repair_exit.go)、[预算原申请](../../internal/cli/s7_budget_questionnaire.go)、[原生拒绝诊断](../../internal/cli/native_s9_diagnostic.go)、[Main 返回投射](../../internal/cli/native_main_return.go) |

使用入口为[主会话协议](../../docs/control/agent-protocol.md#s9)、[bug-resolution](../../skills/bug-resolution/SKILL.md)以及各实际 Builder／验证责任。计划与结果的专业字段分别见 [PlanReport 题目](../../internal/schema/assets/stage-form-s9-plan-template.json)和 [RepairResult 题目](../../internal/schema/assets/stage-form-s9-result-template.json)。

当前落地应保持以下区分：

- **真实工作与接收资格不同。** 不恢复通用 PLAN／Begin／激活、强制造红、无需迁移却强求双引用、工具 ACL 或全来源反复证明；结构仍需准确，未完成实际工作仍不能推进。
- **候选与 Final 不同。** 候选没有 Main 检查不等于 Final 可以缺检查；原 Final 接收要求当前被检提交与实际必要检查事实。普通 Hook 不代执行这些工作。
- **空变化与空履职不同。** 已完成整批 Final 可以得到空 Changeset／Impact，仍经原 Main 检查、专业处置、全部必要断言的独立复验及 Handoff；缺结果、检查或断言不会因空集合而成立。规范 S7 保留完整产品基线与 TASK／Claim 分母；旧非规范记录无产品变化时，种子钉住真实获批 Contract 作为规格来源，仍须由 Planner 按原规则形成专业计划，不能把这一兼容种子说成已经验证完整产品。
- **原事实来源与全目录扫描不同。** Impact／Handoff 沿用已接收 Final 的整批 last-wins 变化；不能把旧物理 Session 差异算法或目录缓存重新加成正常接收准入。规范 S7 基线仍有自己的完整来源读取责任。
- **专业独立与身份标签不同。** 消费者能识别已登记实现者冲突，实际合并者和平台上下文的独立性仍由 Main 正确安排；可选 continuity／scope 字段不代替这些事实。
- **计划编译与完整多合同协调不同。** 当前主要按原单元生成责任与处理本批事实，不能将高级合并、跨合同治理或自动修订正式 TASK 来源写成已经落地的能力。

D 仍须验收：完整现行安装下的 S7 Reviewer、独立 Targeted→fresh S7、真实 blocked／incomplete、正常 OS sandbox、项目／global ignore、Main／Worker 并发和取消、新旧 D／C／冻结责任共存，以及消费者与清理的非原子窗口和大 ignored 目录成本。默认平台保留树不等于临时 branch 已删除；长期阻塞的树、branch、磁盘与失败恢复须具名报告，不自动 GC 未授权资源。S6／S8／S10 原专业合同保持原范围，未因 D 获得新增覆盖。

本阶段是否完成由真实修复、集成、影响和独立必要复验及原交接共同决定。文档示例、Go 夹具、原生事件模拟、真实 Claude Code 运行与新 S7 产品验证分别具有自己的证据范围。

S9 → S7 的 `PLANNER-REFINE` 是未回答的专业字段，`assertion`、`oracle`、`method` 均须由 Planner 按真实材料完善。`cold_start` 表示覆盖起点，不代表必须做浏览器验证；每个适用 E2E Assignment 的 `behavior_surface` 必须按真实行为来源选择 `browser` 或 `non_browser`，真实不适用时保留有来源的 N/A Claim 并移除相应 Assignment。
