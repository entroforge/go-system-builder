# 原生子代理与 Main 工作区集成

本指南是新工作的操作入口。Claude Code 负责创建子代理、前后台运行、消息、等待、worktree 隔离和恢复；Main 负责工程分工、准确资料交付、集成及阶段闭环。`SubagentStart` 补充有界上下文，`SubagentStop` 将实际专业产物交给原消费者。Runtime Writer、领域接收器和原 Gate 保留工程事实与阶段决定。

[原生派发技能](../skills/agent-dispatch/SKILL.md)与[阶段协议](control/agent-protocol.md)给出角色和阶段约束。已登记的旧 workspace／Team 执行使用[冻结执行恢复参考](workspace-execution-recovery.md)；其中 prepare/launch、bootstrap、Claude UUID 与旧保存提交命令只恢复原责任，不能成为新原生工作的前置条件。

## 开始前先确认原责任

| 当前工作 | Main 提供的原来源 | 执行及接收 |
| --- | --- | --- |
| S2、S3、S4 Main 规划 | 当前阶段的准确 view/answer | Main 保存，由原 PostToolUse 消费；原 Gate 决定推进 |
| S5 文档审查 | 当前独立责任的 view/answer | 原生 Agent 独立审查，返回原答卷路径，Stop 接收 |
| S6 实现 | `s6 prepare` 返回的准确 brief | 原生 Agent/worktree，保存专业 PLAN/Result 并返回，Stop 接收 |
| S7 验证 | 当前 ReviewPlan、责任及准确 view/answer | 原生 Agent 按 Claims、波次和独立性执行，Stop 接收 |
| S8 调查 | 原 Case/Hypothesis 派发和准确 view/answer | 原生 Investigator 返回原答卷，Stop 接收 |
| S9 修复／独立复验 | 原 RepairPlan/Contract 下准确 brief 或 Targeted 答卷 | Builder 用原生 worktree；独立 verifier 保留独立性；Stop 交原消费者 |

工程 owner 是责任身份，可以与实际平台 actor 不同。同一角色可以有多项责任，不能凭角色、actor 名、队列顺序或启动先后猜问卷。Main 必须真正派发原生 Agent；工程登记和准备成功本身不表示子代理已经运行。

Main 保持在用户当前项目 checkout，目标是需求绑定的开发分支；当前为 `test2` 就集成到 `test2`，没有通用 `develop` 默认值。已有 linked worktree 可以是 Main，Git common directory 仅验证仓库身份。原生 worktree 使用已提交的绑定开发分支 HEAD，不自动切 Main 分支或 add-all/stash 输入。

新 install/init 默认启用专业问卷，不必再传 `--packet`。`--packet=false` 是新 Runtime 的显式 opt-out；旧实例和 pending 初始化沿各自冻结设置继续。问卷路径由原准备入口与 Hook 提供，作者不手工 issue/consume，也不从 Runtime 猜路径。

责任状态使用 [pending/active/blocked/done/cancelled](rules/state-machine.md#21-loop-responsibility-status)。平台执行结束、领域接收、本人完成、Main 集成和阶段完成分别有自己的事实；不能互相代替。普通工具事件不激活或接收原生子代理，也不按工具、路径或身份建立准入门。

## Main 的 S2～S4 规划工作

### S2 Main 设计答卷

启用问卷的 v1.2/v1.3 Runtime（新 install/init 默认开启）在原 S2 责任准备时提供设计问卷，普通 PreToolUse 只读展示当前 `view_path` 与 `answer_path`。Main 读取 view，完成适用设计并锁定 `docs/architecture/ARCHITECTURE-*.md`，将架构、适用 accepted ADR 和模块材料等正式上游提交到原绑定选定的开发分支，再在原 answer 的 `answer` 填写 `conclusion=pass` 与真实 `architecture_path` 并保存。S3 读取已提交来源并与磁盘核对；Hook 不替作者提交文件。成功保存由原 PostToolUse consumer 接收，不需要手工运行 `stage-answer issue` 或 `consume`；具体技术故障仍可使用受支持的诊断和恢复入口。

接收、迁移和下一材料准备分别成立。未答完的答案仍为草稿；完整专业答案可被接收并登记 `planning_design` 和 pending followup，但架构未 locked 或 UI impact=unknown 等条件仍会使原门拒绝 `PTR-PLAN-01`。需求澄清沿原人闸处理。S2 接收不以开发来源摘要相等为资格；正式来源问题会影响后续 S3 准备，不能误报为答卷未接收。原转换动作登记架构，随后准备 S3；准备失败时修复反馈指出的实际来源并提交，不改写已经接收的结论。同内容重放返回原回执，未完成出口沿原 continuation 继续，不重复登记。Main 阶段回执不等于项目可结束，也不替代后续独立验证；旧实例保留原兼容合同。

`architecture_path` 是当前报告审查的架构，不把登记范围缩成单个文件。系统给出实际架构登记批次和未锁定项，包括兼容规则纳入的无明确需求归属文档；作者处理这些真实缺口，不手填批次清单，也不把未采用方案伪标为 locked。S3 正式来源包括锁定 REQ、已登记架构、`docs/design/prototypes/` 下全部非模板模块的完整文件集合、当前 REQ 的 accepted ADR 及真实引用指向的条件依赖。全项目模块消费与原桥一致，不要求本次重新设计无关模块，也不要求删历史资料或拆需求来缩小来源集合。

### S3 Main 契约答卷

在实际 cursor 为 `planning.contracts` 时，Hook 自动确保 S3 答卷；在
`planning.design` 时仍提供 S2 问卷。每次 PreToolUse 按当前 `status`/原 Hook
cursor 返回准确 view/answer 绝对路径。Main 读取冻结资料，只在 answer 的 `answer` 填入：

| S3 字段 | 填写内容 |
| --- | --- |
| conclusion | 明确的专业自审结论 `pass` |
| index_path | 实际的 `docs/dev/contracts/CONTRACTS-<id>.md` 索引路径 |
| contract_paths | 本次完整且适用的 FE/BE/SYNC 合同文件路径数组 |
| applicability | `FE`、`BE`、`SYNC` 各自为 `required` 或 `not_applicable`，并给出与 REQ/locked design 对应的具体 rationale |

`not_applicable` 不是机器推断：必须说明该端为何不适用；`required` 必须与
交付的同类合同相符，整体仍须有至少一份 locked 合同。不要填写 S2 的
`architecture_path`，也不要把 S2 的 UI impact 或设计结论复制成 S3 字段。作者
不填写 producer、source refs、证据 ID、摘要或接收键。

Hook 发行绑定实际 `planning.contracts` 的 Main/runtime/generation/stage-entry；
答卷读取原 locked REQ 和 PTR-PLAN-01 已登记的 S2 design，并冻结按原引用闭合的
模块真相、ADR/共享模型和条件来源。S3 的 CONTRACTS/FE/BE/SYNC 是本阶段的
交付候选；它们要由本次原检查和 PTR-PLAN-02 消费、登记，不能假定发行时已经
被 PTR-PLAN-02 登记。源文件摘要及目录成员清单由发行端从上游原登记与引用解析
重建，不由答卷中的 `contract_paths` 自证完整。必需来源新增、删除、漂移或
stage/baseline 变化时，尚未接收的旧答卷不能覆盖新事实，应按原规划/修复路径
重发。

Main 保存只完成接收请求；原 `contracts check` 和 `scenario bridge` 仍是 S3
机械检查，原 `GATE-PLANNING-CONTRACTS-COMPLETE` 与 PTR-PLAN-02 仍决定推进，
并由 `register_locked_contracts` 登记原合同与 shared inputs。答卷的 `pass` 不
等于 gate PASS。缺条件输入、合同集合不完整、检查失败或需改变 REQ 时，修复
相应源并重跑原检查；涉及 REQ/breaking 决策继续走原 human gateway，不能用
S3 自审批准。PTR-PLAN-02 后的 S5 双审仍独立。

同一个 packet/attempt 的相同答案重放返回原领域回执；不同内容冲突。成功接收
后中断由原恢复流程续作 pending followup，不重复登记 `planning_contract`
或转换。需要查询回执或恢复时，Main 可使用原 `receipt`/`recover` 诊断入口；
手工 `issue`/`consume` 是兼容操作，不属于常规作者步骤。回执说明本次 Main 责任、原 evidence refs、等待 owner/系统动作和下一步；
它不代表 S3 gate 已通过，更不授予结束项目资格。实际模型与宿主的运行证据须说明版本和执行范围。

### S4 Main TASK 规划答卷

实际阶段为 `planning.tasks` 时，Hook 自动确保 S4 问卷并提供当前 view 与
answer 的准确绝对路径。读取 view，只在 answer 填写 `conclusion=pass`、精确当前
REQ 的 `index_path` 和本次有效任务的完整 `task_paths`。取消事实由原索引与受控投影收集，不要求作者为取消项补写新的路径、摘要或交付。依赖、条款覆盖、
写范围和波次在正式 TASK/索引中声明，原 `tasks check` 校验这些事实。保存登记
原 `planning_task` 与 pending followup；原门和 `TR-002` 决定进入 S5。需要查询或
恢复时由 Main 使用 `receipt`/`recover` 诊断入口，手工 `issue`/`consume` 仅保留
兼容路径，不属于正常作者流程。pending 恢复不修改已经接收的专业判断；原完整
状态恢复保留三个子类型。

S4 首版要求可验证的原 S3 接收和登记来源；旧实例没有这些来源时按原流程
恢复，不从磁盘补造历史或自动迁移。机制已通过定向验证；专业运行的验收范围须单独记录。

## S5 原生文档审查

Main 保留原独立审查分工与冻结资料，将当前 view、私人 answer 的准确路径带入原生 Agent。Reviewer 读取真实资料、执行分配的专业视角，只填写原答卷中的 `answer`，保存后在原生返回中指出准确引用。`SubagentStop` 调用原 S5 接收器；普通保存没有逐次领域接收的含义。普通 `plan_checkpoint` 不要求再交一份通用 PLAN；显式 `plan_approval_required` 继续等待原批准。

只读审查可以使用 Main 下的私人问卷，不需要 execution、bootstrap 或独立 Claude session。若这项责任原来已登记独立来源，就保留原路径；来源丢失交 Main 恢复，不能换成 Main 来源。Finding 被合法接收且本人检查满足后，本人责任可为 `done`；同事未交、系统接续或人类需求决定由回执指定的 owner 继续。

当前原生 packet 的 `draft.read` 给读集，`answer_path` 给私人答案，`task_analysis.path` 给单独的分析文件；相对路径按 Main 带入的准确来源根解析。以下只读资料中，行为与惯例投影在分析文件内，不是 packet 的顶层字段；它们不代替审查结论：

| 原资料 | 提供什么 | 使用边界 |
| --- | --- | --- |
| `task_analysis.path` 指向的分析文件 | 正式 TASK 输入、读集、粒度建议和固定解析器版本 | 读取原冻结分析；机械 Problems、条款覆盖与 DAG 仍独立检查；不签署 PASS |
| `behavior_projection_version` / `behavior_examples` | TASK Manifest 明确引用并经场景包验证的 CASE、witness、oracle、fixture 及实际来源摘要 | 阅读模式和未知条件保留；这是可执行预期的参考，不是产品或浏览器已运行的证据 |
| `conventions_resolver_version` / `task_conventions` | 原 Rules 或登记规范支持的选择、例外及来源 | registry 自称 approved 不是批准；candidate/conflict/stale 等不能当当前默认；有影响的决定缺口交原 Architect |

原生上下文列出实际来源和阅读顺序；冻结执行如何安装 `.claude/inputs` 由[恢复参考](workspace-execution-recovery.md#s5-独立文档审查)说明。不要把安装副本步骤加给原生审查。旧已发行分析不在重试时追加字段或按今天的解析器重算；具名修订保留同一冻结分析。来源漂移或分析丢失须恢复原来源或走原返工，不能靠重新生成摘要续作。

### S5 具名修订

已接收判断不能就地修改。Main 在 S5 原进入身份、owner、Assignment 和冻结资料均不变时，以原回执的 `submission_key` 和具体原因调用：

```sh
loop-harness runtime stage-answer revise --root <Main> --previous <submission_key> --reason '<具体要重新审查的观察>'
```

同原回执、同原因重放返回同一新 packet；不同原因冲突。发行立即暂停旧证据资格，修订等待期间旧 PASS 不能推进阶段。新答案接收后原 Writer 原子登记替代关系，原 PASS/FIX/需求人闸路由保持。旧回执仍说明历史交付已接收；兼容字段 `responsibility_status=replaced`、`work_complete=false` 指向新 packet，当前 attempt 只有新答卷经原接收后才可能投影为 `done`。资料变化或已离开 S5 走原返工/需求流程。

新原生责任按新问卷的准确引用开展修订审查，Main 携带当前 view/answer 派发；旧 packet 的答案不能代交新 attempt。Claude Code 管理执行和恢复，不为新问卷增加 workspace prepare/launch。已经冻结的独立执行仍保留其来源，按[冻结执行恢复参考](workspace-execution-recovery.md#已登记冻结执行准备与启动)建立后继执行，不能退回 Main 来源或改写旧目录。

Main 可只读恢复历史回执：

```sh
loop-harness runtime stage-answer receipt --root <Main> --agent <原owner> --packet <原packet_id>
```

旧 Worker 根已退役，不再接受提交或执行命令；历史读取由 Main 查询原领域记录。当前已发行责任不通过手工 `runtime evidence add` 登记另一份审查。新 install/init 默认打开问卷 consumer；`--packet=false` 是新 Runtime 的明确 opt-out，旧实例和 pending 初始化不暗迁移。版本、初始化回执与恢复边界见[安装指南](guides/install.md)。

## S6 原生实现与本人交付

新实现使用 `s6-native-v1`。Main 选择已经审查、当前可执行的 TASK 和角色：

```sh
loop-harness s6 prepare --root <Main> --task <TASK> --role backend-builder
```

将返回的准确 brief 交给 Claude Code 原生 Agent，并使用原生 worktree 隔离写入。准备会复用原 Register Writer 登记工程责任，不要求 Team ID、预选平台 Agent ID 或 workspace launcher。具体示例见 [S6 Builder](examples/s6-builder/README.md)。

Builder 按 brief 的固定源提交，在本人 worktree 读取其中列明的 TASK、规格与产品相对路径；准确 brief 和方法入口可以位于 Main 控制根，但 Main 后续变化不替换本责任来源。在自己实际 worktree 的私人 `.claude/assignments/native/` 路径先保存专业计划，再实现、自检、提交真实产品改动、保存 Result。私人草稿不进入 Git，不写回 Main。返回准确 brief 和 Result 的绝对路径；保存不代表激活或接收，Stop 才通过原适配器提交实际产物。不要增加手工 begin、task-complete、report/deliver 步骤。

当前私人 Plan 沿 brief 填写 `objective`、`planned_paths`、`assertion_checks`、`risks_blockers`。每项 assertion 包含 `assertion`、`oracle`、`done_when_indexes`，覆盖 Done when 的零基索引。登记依赖由系统带入 canonical Plan，不在私人 Plan/Result 填写 `dependencies`；无风险时 `risks_blockers` 填空数组。

Result 按原草稿填写 `summary`、`assertion_evidence`、`risks_blockers`、`scope_deviations`、`closing_assessment`。每项 `assertion_evidence` 使用本人 Plan 的零基 `plan_assertion_index` 和非空 `evidence_refs`；系统从原 Plan 派生 assertion/oracle，不要求抄写。`evidence_refs` 是实际 worktree 内、本人写范围或已列阅读范围中的精确存在文件路径，不带行号、说明或命令。命令及观察写入 summary 或 rationale，不能虚构日志文件引用。

`closing_assessment.disposition` 为 `complete`、`incomplete` 或 `blocked`；每条条件填写 `done_when_index`、`judgment` 和具体 `rationale`；`plan_assertion_indexes` 可由原 Plan 映射派生，显式填写仍核对原关系。历史完整 assertion/oracle 证据行及显式 closing 映射仅作接收兼容，仍严格核对原 Plan；同一证据行不混用 compact index 和全文字段，不把旧格式当当前必填。complete 要求所有条件 satisfied，且没有未决阻断或范围偏差。这是本人专业闭合判断，不能替代 Main 必需检查或 fresh S7。默认准备的原 closing contract 明确使用空 `worker_checks`、`post_merge` 和 Main 检查位置；Builder 仍执行 TASK 自检并如实报告，不能把空 runner 列表解释成不用验证。

真实阻塞可以发生在形成 Plan 前或之后。按 brief 在原 Result 路径保存最小 blocked Result：`summary` 说明进展与未完成工作，非空 `risks_blockers` 说明原因，`closing_assessment.disposition` 为 `blocked`。保留已有 Plan/真实证据，返回原 brief 与实际 Result 的绝对路径；不要编造检查、证据或计划。这是交 Main 恢复的未接收交接。已按完整报告合同填写的 blocked/incomplete Result 仍走原校验，接收不等于可集成，也不能原地改写已接收答案。需要后继修订时，若已有原返工检查点依据，直接沿下节具名修订；若尚无该依据，Main 先运行原 `loop-harness runtime task-integrate --root <Main> --assignment-id <assignment-id>`，由原消费者记录 closing blocker 检查点并拒绝 merge，再运行下节 `s6 prepare --revise-assignment`。保留已接收报告及真实阻塞，不把这次记录阻塞解释成合并或完成。

原登记中的非空 runner 检查、回执耐久副本与静态复用条件见[底层检查参考](workspace-execution-recovery.md#显式-checkprofile)。仅在当前原来源确实提供对应命令时使用，不把冻结执行的命令复制给新责任。

### 原生 Assignment 返工

需要修改代码或报告时，Main 沿原具名修订入口续作：

```sh
loop-harness s6 prepare --root <Main> --task <TASK> --role backend-builder \
  --revise-assignment <assignment-id> --revision-intent-key <stable-key>
```

相同修订意图保留 stable key；原 Writer 形成新版本并返回准确 brief，Main 再按当前原生责任派发。原 Result、checkpoint 和源树留存，旧 checks/ACK 不变成新版完成凭据。该入口保留同一 reviewed TASK 的 ID/path/SHA；TASK 规范变化回原规划与文档审查，不通过刷新摘要或反复修订绕过。

如果只是原接收失败而专业产物已保存，先检查原回执、来源和 pending。同一责任仍有效且尚未接收时，恢复原生 agent 返回原产物；不用重做实现、另开责任或等待 Main 集成才能让 Builder 结束。

## 固定产品基线与 S7 接续

显式 Assignment 的薄 Result 不提供机器 changed paths。S7 的固定产品来源由原 TR-003 的 Main 起点回执、journal、Assignment Record/Result、版本化 Main checkpoint、完整 runner/tool receipts 和真实 Git 差异共同重建。`controlproof` 验证原 journal/start receipt，`mainreceipt` 验证单项 Main contribution，`buildsurface.Read` 汇总当前与历史 Record 的合法贡献及 start..final 全路径，`ReadRegistered` 再检查原 typed ReviewPlan 的完整 `frozen_subjects`。这些来源也服务当前原生交付，不是旧 launcher 的专属能力。

基线包括删除路径和未归属某个 Builder 的实际 Main 变化，不能退回作者的 `changed_paths`、`scope_refs` 或遗漏集合。S10 显式 `affected_paths=all` 表示审查范围全量，仍须逐项覆盖机器 `changed_path` 分母和验证来源。缺失或漂移是不可验证，不是“无变化”。

TR-006 的 `builder_report_record` 是原 `completion_report` 的兼容候选 kind，不是另一份 Builder Result。原 Engine 仅在已登记的当前 Result ref 确属 `buildsurface.Read` 验证的当前 Main 贡献时接受；本人闭合、精确 TASK、共同起点和 Main 事实任一缺失都不能推进。旧 completion 保持原来源。

S9 的 TR-012 seed bridge 复用同一产品基线，发布 seed 前和原 Writer 变更状态前复验来源，保留完整任务/路径覆盖、DV 和六项 QA 骨架。Main 仍要完善 assertion、oracle、method 与专业分工；seed 不判定 E2E 适用性。机器来源和 seed 成立不代表完整产品或真实模型验收，实际运行证据单独记录。

## S7 已发行专业答卷

Main 完成原 ReviewPlan 与专业登记，原准备入口确保当前 dispatched Assignment 的 `s7-review-result-v1` 问卷。波次和依赖保留；未就绪责任不提前取得专业结论。Main 将准确 view/answer 路径带入原生 Agent，Reviewer 阅读真实冻结材料，独立执行其 Claims/lens，填写原 `answer` 并返回准确引用。SubagentStop 调用原 `review.SubmitIssuedResult`，校验当前 Plan/round、owner、来源、Claim exact set、证据与原批准模式。普通工具事件不提供逐保存回执，不要求 generic PLAN/activation。

计划草稿、登记与修订共用绑定 REQ 和当前冻结 TASK 的明确模块范围，读取其中必需浏览器 CASE。TASK 仅以原 `Module current truth` 和 `Module Impact` 的 `Modules touched` 声明选模块，背景阅读及说明链接不扩大范围。每个已知 CASE 在 E2E Claim 的 `source_refs` 使用完整 `cases.json#CASE` 引用，安排实际验证或用原理由/检查表明确 N/A；笼统 `ui_impact=none` 不代替逐 CASE 处置。缺明确来源交 Main 恢复，不全库兜底；Main 仍须分析未显式记录的消费者影响。

新只读责任可直接使用原 Main 根下私人问卷，不创建 execution。已登记独立 Worker 使用其确切来源，缺失时不能回落 Main；cold-start E2E 必须如实报告实际执行来源，`verification_artifact_digest` 由原消费者自动固定并验证，作者保留原卷机器预填或 null，不计算、复制摘要。显式 `plan_approval_required` 等待原审批。

Main 组织整轮完整模块和受影响面，Reviewer 只执行本人分配的 Claims/CASE；浏览器、API、命令和状态型行为各使用实际入口与方法。S7 读取锁定场景/流程及契约，缺项反馈 Main，不重写规格或 FE 契约。cold_start 所需新脚本/夹具写入原 `verification_artifact_workspace`（`e2e-workspace/` 下），命令指向实际文件和适用 runner/config；既有 `web/e2e/<module>/` 资产在原路径只读执行。正常负向 PASS 的状态、副作用、拒绝和恢复写 `ClaimResult.observed` 与真实证据；失败才产生 Finding。普通 Finding 继续其余安全检查，真实 P0／危险操作停止并保留观察及采集缺口。

最后有效 Result 接收形成 CleanRound 后，原完成出口在同次交接检查原 Gate/TR-009，准备并返回 S10 当前验收问卷准确路径。原接收、迁移与材料准备是分别成立的事实；门或迁移失败按原 `loop-harness runtime review-continue --root <Main>` 复核当前干净出口，已进 S10 的缺稿按 `loop-harness runtime audit-prepare --root <Main>` 恢复当前缺失材料，保留已有报告。Main 不重交 Reviewer 答案、不手工复制 clean_round 绑定，也不靠下一次普通 Read/Bash 制造事件；旧 PreToolUse 自动转换只保留兼容恢复。

相同已接收答卷重放不追加 Result/Finding，不同答案沿原冲突/具名修订处理。`runtime stage-answer receipt --agent <owner> --path <issued-path> --root <Main>` 只读恢复事实；普通工作不手动 issue/consume。原 explicit/冻结 teammate 保存入口继续服务其原兼容责任。专业验收须记录实际产品、平台和执行范围。

## S8 已发行假设答卷

Main 沿原 Case/Hypothesis 与 `runtime investigation dispatch` 登记工程责任，原准备入口确保准确 view/answer。Main 携带该引用原生启动或恢复 Investigator；逻辑 owner 不要求等于实际平台 actor，登记不代表已经启动。Investigator 阅读真实 encounter/材料，填写本人方法、观察、证据、判别与结论，保存原答卷并返回引用。SubagentStop 调用原 HypothesisResult/Case Writer，普通工具不激活、不接收。

新只读责任不要求独立 execution/session。已登记独立目录沿原来源校验，缺失时不得换用 Main。普通 `plan_checkpoint` 无 generic PLAN；显式批准模式保留原批准。原 Writer 同 CAS 保存结果与回执，无关兄弟结果追加不使有效问卷失效，自身来源/派发/路由变化仍拒收。source boundary 使用实际 `finding:<source-id>` 或 encounter pointer；不编造 ref。

新 Hypothesis/Assignment 由原领域生成，Main 仍须真正派发；答案不批准 route、Contract 或 S9 修复。重复 Stop 沿原幂等接收，改写已接收答案仍冲突。只读 receipt、冻结旧保存入口和原人闸保留。S8 当前实际 lifecycle 为 `bug_resolution/investigation`，残留 S9 pointer 不得抢占它。真实原生模型专业验收另记。

## S9 原生修复与独立复验

原批准的 Case/Contract、Session、RepairPlan 与实际修复派发确定责任。Main 将原准备入口给出的准确 S9 brief 交给原生 Agent，Builder 使用原生 worktree 完成实际预检，先保存专业 PlanReport，再实施修复、自检和 Result。存在产品变化才提交真实改动；合法无变化结果不制造空提交。Main 根下的问题是只读输入，专业输出留在实际 worktree 私人路径。

Builder 返回准确 brief 和 Result 的绝对路径。Stop 将实际 PlanReport 专业内容导入原问卷，调用原 PlanReport 与 RepairResult 消费者；不由 Main 代填，不要求普通保存时先投影 AgentBegin，也不新增通用 PLAN_REPORT 或 workspace deliver。`native_delivery` 仅保存实际返回的 Git 坐标，供原 Result、Main 集成及检查使用；它没有 bootstrap、平台会话或启动租约，不能交 launcher 启动。

独立 Targeted verifier 得到准确的原 Targeted 答卷，检查修复后的实际树，不写产品；返回后由 Stop 调用原消费者。每个获批 Contract 保留其独立复验义务，之后原 Handoff 返回 fresh S7。PlanReport 被接收、修复候选被接收以及 Main 最终结果分别有自己的回执，均不代替后续门禁。

`completed` 仍要求原 unit 条件成立且无范围偏差；`blocked`、`contract_conflict`、`stale` 如实交原 blocker/resolution 路径。缺准确来源或接收失败交 Main 处理，不让 Builder 猜 source、刷新 SHA 或运行旧命令冒充完成。冻结 workspace／Main-root 保存问卷只沿[原执行恢复](workspace-execution-recovery.md)消费其历史责任。

## Main 集成、必需检查与继续流程

子代理返回后，Main 先查当前原回执或已接收交付，再消费原待办：

```sh
loop-harness runtime workspace pending --root <Main>
```

对于当前已接收、可集成的 Assignment，使用原待办给出的准确命令。S6 的原集成入口为：

```sh
loop-harness runtime task-integrate --root <Main> --assignment-id <assignment-id>
```

Main 在绑定开发分支集成实际贡献、执行全部原必需检查，再由原检查点和 Writer 确认接收。Result 的路径、摘要及实际受测 HEAD 一起决定回执适用性；产品、结果或受测 HEAD 变化不能复用旧 PASS。中断或失败后允许按原反馈重入同一 `runtime task-integrate` 命令，恢复尚未完成的合并、检查、确认或出口；原检查点避免重复合并和接收。Main 检查失败留下 preserved/blocked 检查点时，先解决原环境/检查原因，再按原 pending 的具名命令带 `--retry-preserved` 重试；裸重入会保留原失败。已成功集成仅需接续出口时，不增加该重试前置。已迁移 S7 后的材料准备缺口沿反馈给出的原 `s7 draft --root <Main> --out <原路径>` 恢复。已有确认后的纯清理重试保留原验证事实，不因为 Main 后续合法推进而重新要求 Builder 工作。Hook 不同步执行长合并；PreCompact 不代收答卷或另建阶段 checkpoint。

两种检查位置沿原登记选择，不能为省时把 Worker 自检抄成 Main PASS。原 closing 未闭合、有风险/偏差或缺实际必需检查时，不放行 ACK 或阶段转换。S6 完成进入 S7 后，Main 仍要完成专业 ReviewPlan；S9 Main 集成、独立 Targeted 与原 Handoff 成立后进入 fresh S7，不能由候选接收跳过。

环境问题解决后按原 pending 重验保全候选；确需修代码时，原生 S6 使用上节具名 Assignment 修订，S9 使用原领域返工/解阻路径。旧格式与冻结执行的 `workspace rework`、ACK 中断续作及清理细节见[集成恢复参考](workspace-execution-recovery.md#合并s9-与继续流程)。该入口不覆盖旧候选、不自动 reset，也不把失败改成 PASS。

## 统一提交回执

原接收入口和 `stage-answer receipt` 使用同一个 `submission-receipt.schema.json`。原生子代理在 Stop 接收，Main 规划在保存时接收；不能因接口相同就把两者的触发事件合并。

`submission_key` 是原领域接收身份，可以是通用提交摘要、S7 Result ID、S8 Case commit key 或 S9 PlanReport/candidate/Result key，不一定是 SHA256。`current_stage` 表示查询当时的阶段，不标识历史回执的来源阶段。`submission_key`、`domain_refs` 和状态均为只读投影，不由作者修改。

`accepted` 只表示原接收已经成立；`work_complete` 还需要当前 owner 本人责任的证明。S9 PlanReport 可以 accepted 而本人仍 active；`candidate_accepted` 在检查满足时可以将本人投影为 done，同时列出 Main integration 待办。`main_result_accepted` 也不代替后续 Gate。历史 `main_result_accepted_rework_required`、`main_result_accepted_owner_status_unavailable` 保持 `work_complete=false`，按原回执的具体恢复动作处理。尚未接收的候选返回 `not_accepted`，不能用空状态冒充完成。

`consumer_owner` 与 `consumer_pending` 成对出现，表示后续责任归谁。Main 的待办不自动重开已完成 Builder 的责任，也不授权下一阶段。原生执行已返回但接收未成立时，先由 Main 排查来源与并发事务；责任和输入未变才恢复原 agent 返回原产物，不重复专业工作。

查询当前事实只使用当前实际引用，例如：

```sh
loop-harness runtime stage-answer receipt --root <Main> --agent <owner> --path <issued-answer-path>
```

### Main 怎样向用户反馈完成情况

Main 在原生返回后核对当前回执，再用已有事实说明进展，不要求子代理另交一份汇总报告：

| 当前原回执事实 | 可以怎样解释 | 接下来由谁做 |
| --- | --- | --- |
| `accepted=true`，本人 `work_complete=true`，`consumer_pending=true`、`consumer_owner=main` | 本人交付已接收，后续处理尚未完成 | Main 沿 `waiting_on` 和 `next_action` 接续；无需让已完成子代理继续等待 |
| S9 `candidate_accepted`，本人条件与检查已满足 | 修复候选已接收，本人责任可以结束；尚不能说整个修复阶段完成 | Main 集成、必需检查，随后原独立复验及 fresh S7 |
| PlanReport 已 accepted，但 `work_complete=false` | 计划已接收，不能由此宣称实现或修复完成 | 按当前责任与回执继续实际工作 |
| `not_accepted` 或原来源/事务接收失败 | 执行已返回，领域交付尚未成立 | Main 恢复接收来源/事务；同责任未变化才恢复原 agent 返回原产物 |

例如确认了第二行的真实回执后，可以反馈：“修复候选已接收，Builder 本人工作完成；Main 还需集成和执行必需检查，独立复验与新 S7 尚未完成。本次已验证的内容为……，尚未验证的内容为……。”省略没有事实支持的阶段或测试结论，保留实际等待 owner、原引用和下一步。`waiting_on` 指向人类决定时如实说明原决定，不另加批准步骤。

## S6 接收优先建议

Main 可运行 `loop-harness s6 status --root . --advice --capacity 3 --json`；
`3` 应替换成平台实际总槽位。输出保留原看板，并追加 `reception_advice`。
它从原 Assignment、完成报告与 integration checkpoint 重建待接收动作，
优先接收能释放更多直接消费者的结果，同分按 TASK/Assignment ID 排序。
`current_action.argv` 是原 pending 的具体入口；依然由原集成检查和 Writer
核实当前权限与结果。收到返工问题时先填写实际纠正理由。

省略 capacity 时预算为 null；显式零表示当前无槽位。
`--max-unreceived-results 1` 只收紧 `recommended_tasks` 的建议预算，
不改 `next`、注册门或 required 集合。未接收、失败、verified 待确认、cleanup
分别计数；cleanup 不计入这个上限。该单次建议参数不改变 Runtime 持久配置或注册反压门；后者只在初始化显式选择时成立。Runtime 在读取期间变化会要求刷新；建议仍可能在执行前过时，
执行原命令时继续复核。checkpoint 状态不证明进程存活。

## 新 Runtime 的显式持久反压

初始化或全新安装时可选择：

```sh
loop-harness init --root . --dispatch-max-unreceived-results 3 --dispatch-resume-unreceived-results 1
```

install 接受相同两个参数。max 至少 1；resume 必须小于 max，缺省为 0。
配置随原初始化回执冻结，已有 Runtime、普通更新与 reset 不可修改它。
默认不启用；单次 status 的上限仍只用于建议。

原 Assignment 的未接收结果达到 max 时，新的 S6 Builder 注册暂缓，看板
候选显示 queued；已登记作业照常交付。迟滞持续到原 Main 接收量降至 resume。
在途交付可能超过触发值，不能据此取消作业或缩小 required 覆盖。
原 ACK 登记确认的报告路径与 SHA；匹配当前 checkpoint 才释放，新报告清除
旧 ACK 绑定。cleanup 失败单列恢复，不延长已确认结果的反压。
源 manifest 无法解析时先恢复原登记来源，不能把未知接收量当零。

## 原 Main 集成锁的只读观测

`loop-harness runtime workspace integration-status --root .` 读取原工作区锁的
瞬时状态：busy、available、not_created 或 unknown。它不创建锁或诊断文件，
不改 Runtime、journal 或 checkpoint。锁文件存在不表示正在运行；busy 来自
实际 OS 锁探测，而不是 PID 或旧记录。输出遵循 integration-observation Schema。

在实际 busy 期间，`s6 status --advice` 的当前动作为 observe_integration，给
完整只读入口，继续保留待接收结果与兼容候选。这个信号没有权限或 PASS，
不证明具体 Assignment/进程身份；观察后状态可能立即变化。unknown 给来源
诊断，真正执行仍由原锁和 Writer 校验。普通工具和 Worker Stop/Idle 不新增
扫描或门，Main 只在原指定检查点刷新。

## 已发行答卷的 Runtime 恢复

1.2 Runtime 发行工作包后，原 Writer 会在清除提交/指纹 pending 前保存完整
state/journal checkpoint（包含旋转段），索引只保留最新两份。它是恢复证据，
正常门禁仍只读原 Runtime。快照失败会保留原 pending，修复报告的问题后由原
Writer 重试；不要删除 marker 或把答卷重新登记。

先运行 `runtime recover inspect --root . --req <locked-REQ-path>`，从输出取得
`latest_checkpoint`，然后生成可审核的原 pair 计划：

```bash
.claude/bin/loop-harness runtime recover plan --root . \
  --req <locked-REQ-path> --dev-branch <bound-development-branch> \
  --release-upstream <bound-release-destination> --checkpoint <latest_checkpoint>
```

核对 `checkpoint_restoration` / `original_pair`、候选摘要、责任与激活、修订及
失效记录、原 cursor 与迁移事件后，按原人闸执行
`runtime recover apply --root . --plan <plan-path> --approved-by <human-operator>`。
原合法 pending 优先于计划；半替换后重复同一 apply，不能重新生成接收键。
已处理接续保持已处理，未交的新 attempt 保持未交，旧 PASS 不复活。
计划应用拒绝仍持 launch lease 的 Worker、较新事实、资料漂移和已关闭 Runtime。
现存 Worker 冻结资料与原计划仍需完整可验证；缺少可信完整来源时，不从文件存在猜测
专业签字。单份快照 state 上限16 MiB、完整 journal 上限64 MiB，超限明确保留 pending。

## Hook 与指标维护

原生入口用外层子进程约束整个 Hook 为 8 秒，低于平台配置的 10 秒。完整结果返回前不输出部分 allow；超时/异常的潜在写操作返回阻断码 2，只读和生命周期通知保留诊断通道。中断的 Runtime 事务继续由原恢复协议处理。

计数型指标统一追加独立观察文件，不等待汇总锁。health/doctor、SessionStart 和 PreCompact 执行可恢复压缩：原子记录累计值和已消费文件，再删原文件，避免重复计数。普通 PreToolUse 不做压缩。持续长会话没有这些边界事件时仍应定期运行 health，指标不是业务恢复证据；不能对无限长、完全无维护的会话承诺磁盘恒定。

已冻结 S5 保存消费者识别答卷的调用在 health/doctor 显示
`loop_submission_save_total` 与 `loop_submission_save_duration_ms`。
草稿/无效答卷记为 incomplete，完整答卷的领域拒收另记 complete_rejected；
原 Writer 持久接收的 pass、fix_required、req_change_required 分别显示。
计数包含重复保存，不能用它计算唯一交付数或首交成功率；耗时是本次
Save 调用耗时，包含同步接续，不能当作跨进程等待时间或模型用量。
统计写入失败不改变原回执，指标本身不决定权限、完成或阶段转换。

## 有界进展观察

Main 的 PostToolUse 监听 Read/Glob/Grep，Worker 沿用原 matcher。
原 Runtime/派发/答卷/收尾事实与必要资料的实际读取范围相同时，连续
5 次支持的 Read/status 观察记录一次 typed stall。默认 shadow，只保存到
诊断缓存；维护者显式 hook --progress-mode notify 才追加 WORK PROGRESS
提示。安装与冻结 Worker Bootstrap 不默认启用提醒。
新资料范围、专业字段或责任范围代码变化重置计数；原运行锁、人闸、
外部责任等待和已接收交付抑制提示。未知范围、未适配工具、缓存损坏、
锁忙或资源超限只标观察不足，不作为空转事实。

缓存位于 .claude/hook-notices/work-progress，每个 owner/session 一个槽，
每槽最多64KiB、64个资料范围。缓存故障时 Main 可核对并移除指定槽后
重试；它不是恢复或业务接收证据。提示只给当前缺口和恢复 owner，
不改变阶段、授权、required 检查或本人完成状态。

当前覆盖 S5 发行答卷、原 Assignment 收尾事实及 Main 原 milestone；
其他阶段的专业缺口 adapter 和真实模型轨迹仍须分别验收。

## 实际验收与证据范围

原生机制验收在隔离项目使用实际可用的 Claude Code，记录 CLI 版本、操作系统、Harness 提交及产品提交：

1. 在绑定开发分支准备具体责任并真正派发原生 Agent，核对准确资料引用和独立性；Main 目录及分支保持绑定事实。
2. S5/S7/S8 返回原答卷，S6/S9 返回实际 worktree 的专业产物；核对 Start 上下文和 Stop 原领域回执，确认普通工具与保存不负责激活、接收或工具准入。
3. 验证保存了但尚未接收、同答重放、来源缺失及并发中断时的原路恢复；不能把收到完成通知解释成领域已接收。
4. 对写责任运行真实自检、集成与必需 Main 检查；注入检查失败，确认候选/checkpoint 保全、后继未放行，恢复沿原待办而不改旧 PASS。
5. 按实际目标平台分别记录能力和结果；runner 隔离不可用、依赖缺失或未执行项如实说明。源码测试、CLI fixture、交叉编译和真实产品/浏览器运行分别证明各自范围。

冻结 launcher/目录迁移的专项验证见[恢复参考](workspace-execution-recovery.md#冻结执行的真实会话验收)。版本、平台和实际流程证据保存在维护记录；本指南不充当完整 S0～S11 或产品验收报告。
