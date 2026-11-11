# 原生子代理与 Main 工作区集成

本指南是新工作的操作入口。Claude Code 负责创建子代理、前后台运行、消息、等待、worktree 隔离和恢复；Main 负责工程分工、准确资料交付、集成及阶段闭环。`SubagentStart` 补充有界上下文，S7～S9 的 Pre 观察当前原卷，成功 Post 和 Stop/SubagentStop 兜底交原消费者；S5/S6 保留原返回接收合同。Runtime Writer、领域接收器和原 Gate 保留工程事实与阶段决定。

[原生派发技能](../skills/agent-dispatch/SKILL.md)与[阶段协议](control/agent-protocol.md)给出角色和阶段约束。实际集成与中断接续见[恢复参考](workspace-execution-recovery.md)。

## 开始前先确认原责任

| 当前工作 | Main 提供的原来源 | 执行及接收 |
| --- | --- | --- |
| S2、S3、S4 Main 规划 | 当前阶段的准确 view/answer | Main 保存，由原 PostToolUse 消费；原 Gate 决定推进 |
| S5 文档审查 | 当前独立责任的 view/answer | 原生 Agent 独立审查，返回原答卷路径，Stop 接收 |
| S6 实现 | `s6 prepare` 返回的准确 brief | 原生 Agent/worktree，保存专业 PLAN/Result 并返回，Stop 接收 |
| S7 验证 | 当前 ReviewPlan、责任及准确 view/answer | 原生 Agent 按本人责任、波次和独立性执行，Pre／成功 Post／Stop 接收本人原卷 |
| S8 调查 | 原 Case/Hypothesis 派发和准确 view/answer | 原生 Investigator 保存本人原答卷，Pre／成功 Post／Stop 接收 |
| S9 修复／独立复验 | 原 RepairPlan/Contract 下准确 brief 或 Targeted 答卷 | Builder 用原生 worktree；独立 verifier 保留独立性；适用 Pre、成功 Post、Stop/SubagentStop 交原消费者 |

工程 owner 是责任身份，可以与实际平台 actor 不同。同一角色可以有多项责任，不能凭角色、actor 名、队列顺序或启动先后猜问卷。Main 必须真正派发原生 Agent；工程登记和准备成功本身不表示子代理已经运行。

Main 保持在用户当前项目 checkout，目标是需求绑定的开发分支；当前为 `test2` 就集成到 `test2`，没有通用 `develop` 默认值。已有 linked worktree 可以是 Main，Git common directory 仅验证仓库身份。原生 worktree 使用已提交的绑定开发分支 HEAD，不自动切 Main 分支或 add-all/stash 输入。

新 install/init 默认启用专业问卷，不必再传 `--packet`。`--packet=false` 是新 Runtime 的显式 opt-out；旧实例和 pending 初始化沿各自冻结设置继续。问卷路径由原准备入口与 Hook 提供，作者不手工 issue/consume，也不从 Runtime 猜路径。

责任状态使用 [pending/active/blocked/done/cancelled](rules/state-machine.md#21-loop-responsibility-status)。平台执行结束、领域接收、本人完成、Main 集成和阶段完成分别有自己的事实；不能互相代替。普通工具事件不激活或接收原生子代理，也不按工具、路径或身份建立准入门。

## Main 的 S2～S4 规划工作

### S2 Main 设计答卷

启用问卷的 v1.2/v1.3 Runtime（新 install/init 默认开启）在原 S2 责任准备时提供设计问卷，普通 PreToolUse 只读展示当前 `view_path` 与 `answer_path`。Main 读取 view，完成适用设计并锁定 `docs/architecture/ARCHITECTURE-*.md`，保留实际架构、适用 accepted ADR 和模块材料，在原 answer 的 `answer` 填写 `conclusion=pass` 与真实 `architecture_path` 并保存。S3 按原绑定及配置读取所选 fileview；所选来源确有材料缺失或不可读时准确反馈，不要求 checkout 一致或额外提交动作。成功保存由原 PostToolUse consumer 接收，不需要手工运行 `stage-answer issue` 或 `consume`；具体技术故障仍可使用受支持的诊断和恢复入口。

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
被 PTR-PLAN-02 登记。原消费者使用已有责任与实际材料，不以源文件摘要或目录成员认证作为接收资格。必需来源实际新增、删除或
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

原生上下文列出实际来源和阅读顺序。分析或必要来源缺失时如实交给 Main 处理，不要求作者补摘要。

### S5 具名修订

已接收判断不能就地修改。Main 在 S5 原进入身份、owner、Assignment 和冻结资料均不变时，以原回执的 `submission_key` 和具体原因调用：

```sh
loop-harness runtime stage-answer revise --root <Main> --previous <submission_key> --reason '<具体要重新审查的观察>'
```

同原回执、同原因重放返回同一新 packet；不同原因冲突。发行立即暂停旧证据资格，修订等待期间旧 PASS 不能推进阶段。新答案接收后原 Writer 原子登记替代关系，原 PASS/FIX/需求人闸路由保持。旧回执仍说明历史交付已接收；兼容字段 `responsibility_status=replaced`、`work_complete=false` 指向新 packet，当前 attempt 只有新答卷经原接收后才可能投影为 `done`。资料变化或已离开 S5 走原返工/需求流程。

新原生责任按新问卷的准确引用开展修订审查，Main 携带当前 view/answer 派发；旧 packet 的答案不能代交新 attempt。Claude Code 管理执行和恢复，不为新问卷增加 workspace prepare/launch。已接收结果和实际来源沿当前责任保留，不以旧答案代替新责任。

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

Result 按原草稿填写 `summary`、`assertion_evidence`、`risks_blockers`、`scope_deviations`、`closing_assessment`。每项 `assertion_evidence` 使用本人 Plan 的零基 `plan_assertion_index` 和非空 `evidence_refs`；系统从原 Plan 派生 assertion/oracle，不要求抄写。`evidence_refs` 可表达实际命令、观察或可用材料，不要求为接收创建证据文件；如实记录实际检查，Main 另行负责集成检查。

`closing_assessment.disposition` 为 `complete`、`incomplete` 或 `blocked`；每条条件填写 `done_when_index`、`judgment` 和具体 `rationale`；`plan_assertion_indexes` 可由原 Plan 映射派生，显式填写仍核对原关系。按当前原卷填写本人判断，不要求另建旧格式证明。本人以已有 disposition 和条件判断如实表达完成或未完成；风险、假设和范围说明保留原话交 Main，相关列表非空不自动阻止集成或表示偏差获批。Main 依据原 TASK、实际变化和自身责任处理真实范围问题。这是本人专业闭合判断，不能替代 Main 必需检查或 fresh S7。默认准备的原 closing contract 明确使用空 `worker_checks`、`post_merge` 和 Main 检查位置；Builder 仍执行 TASK 自检并如实报告，不能把空 runner 列表解释成不用验证。

真实阻塞可以发生在形成 Plan 前或之后。按 brief 在原 Result 路径保存最小 blocked Result：`summary` 说明进展与未完成工作，非空 `risks_blockers` 说明原因，`closing_assessment.disposition` 为 `blocked`。保留已有 Plan/真实证据，返回原 brief 与实际 Result 的绝对路径；不要编造检查、证据或计划。这是交 Main 恢复的未接收交接。已按完整报告合同填写的 blocked/incomplete Result 仍走原校验，接收不等于可集成，也不能原地改写已接收答案。需要后继修订时，若已有原返工检查点依据，直接沿下节具名修订；若尚无该依据，Main 先运行原 `loop-harness runtime task-integrate --root <Main> --assignment-id <assignment-id>`，由原消费者记录 closing blocker 检查点并拒绝 merge，再运行下节 `s6 prepare --revise-assignment`。保留已接收报告及真实阻塞，不把这次记录阻塞解释成合并或完成。

实际检查与中断接续见[底层检查参考](workspace-execution-recovery.md#本人结果与检查)。

### 原生 Assignment 返工

需要修改代码或报告时，Main 沿原具名修订入口续作：

```sh
loop-harness s6 prepare --root <Main> --task <TASK> --role backend-builder \
  --revise-assignment <assignment-id> --revision-intent-key <stable-key>
```

相同修订意图沿原责任接续；原 Writer 形成新版本并返回准确 brief，Main 再按当前原生责任派发。原 Result、checkpoint 和源树留存，旧 checks/ACK 不变成新版完成凭据。该入口使用同一 reviewed TASK 的实际 ID/path；TASK 规范变化回原规划与文档审查，不用摘要作资格。

如果只是原接收失败而专业产物已保存，先检查原回执、来源和 pending。同一责任仍有效且尚未接收时，恢复原生 agent 返回原产物；不用重做实现、另开责任或等待 Main 集成才能让 Builder 结束。

## 固定产品基线与 S7 接续

S6 的真实集成结果与必要检查提供进入 S7 的实际产品材料。Main 组织完整当轮覆盖，向各责任交付准确原题与可读来源。S7～S9 的答卷接收不递归重建 S6 journal、贡献、Git 差异或 frozen-subject 摘要来认证每份答案；实际集成和专业审查仍分别由 Main 与作者负责。S10 自身的审计合同不在此处改写。

TR-006 的 `builder_report_record` 是原 `completion_report` 的兼容候选 kind，不是另一份 Builder Result。原 Engine 仅在已登记的当前 Result ref 确属 `buildsurface.Read` 验证的当前 Main 贡献时接受；本人闭合、精确 TASK、共同起点和 Main 事实任一缺失都不能推进。旧 completion 保持原来源。

S9 完成实际集成、Main 影响判断和独立 Targeted 后，原出口准备新的完整 S7 及准确题面。Main 根据真实变化完善覆盖与分工；seed 不代表 S7 PASS，也不为接收增加基线摘要证明。

## S7 已发行专业答卷

Main 保存原 ReviewPlan，按实际责任、波次和独立性派发准确 view/answer。Reviewer 只填写本人原卷的 `answer`，执行分配的 Claims/CASE，保存一次。PreToolUse 观察当前原卷，成功 PostToolUse 和 Stop/SubagentStop 兜底，经同一原消费者单次核解析、必要字段、就绪和结论并保存；未填结论为草稿。不要求返回引用、重存、done 标记、另交报告，或重验 Git／证据／历史摘要。显式批准模式仍保留真实批准。

Main 负责整轮完整模块及受影响面的专业判断；作者如实说明通过、发现、blocked 和未执行项。普通 Finding 可继续剩余安全工作；真实 P0 或危险操作停止，保留实际观察与缺口。需要的脚本／夹具只写原分配验证空间，既有回归资产保留其原用途。

必要责任完成且无 Finding 时，原出口同次交 S10 并准备准确验收问卷；有 Finding 时将已收发现和未完成项交 S8，不另产 CleanRound／ObservationBatch 证明。S8 已开始后，已派发 S7 原责任仍可首次接收，晚到 Finding 交 Main 判断补充当前调查或后续处理，不重开 S7、不自动改批准。

## S8 已发行假设答卷

Main 沿原 Case/Hypothesis 准备准确 view/answer 并实际派发 Investigator。Investigator 阅读实际材料，填写本人方法、观察、引用、判别和结论，保存原卷一次；同样由 Pre／成功 Post／Stop 经原消费者接收。Main 判断因果、未知与去向，`new_hypotheses` 仅为建议，不自动登记责任或增加退出条件。原真实批准与其准确对象保留，答卷保存不构成批准。

已有发行题面和原路径继续使用。新问题若因当前来源形式尚未受原准备器支持而无法发行，作为 Main/system 的具名准备缺口处理；不臆造来源协议或索引别名。来源边界可用实际 Finding、决定或准确位置说明，不要求逐项机器验指针。真实 blocked／未完成项交 Main，不伪造结论。

## S9 真实实施与独立复验

Builder 按准确 brief 在实际 worktree 先保存原 PlanReport，独立接收后连续实施与自检，再保存原 RepairResult。Pre／成功 Post／Stop 经各原消费者接收，不增加 flat→typed 作者副本、Git／证据摘要门或返回证明。Main 对已收贡献执行真实集成与必要检查，填写原影响判断；不强制指定 `workspace integrate`、Git commit、Final 或 proof 作为 Impact 接收前置。空变化、空处置均合法，可选说明不成为隐藏必填。独立 Targeted verifier 只填写自己的原答卷，对实际修复产品执行必要观察；不能由 Builder 或 Main 代签。

blocked、未完成或真实失败如实保存在原卷并交准确责任。Targeted 通过后原出口准备完整新 S7；局部复验不是干净轮。已收事实不依赖私人文件仍存在或字节不变；后继准备失败保留接收，并沿反馈的原入口只续未完成部分，不重存、不重验历史。清理是资源后继，不撤销已收工作。

## Main 集成、必需检查与继续流程

子代理返回后，Main 先查当前原回执或已接收交付，再消费原待办：

```sh
loop-harness runtime workspace pending --root <Main>
```

对于当前已接收、可集成的 Assignment，使用原待办给出的准确命令。S6 的原集成入口为：

```sh
loop-harness runtime task-integrate --root <Main> --assignment-id <assignment-id>
```

Main 在绑定开发分支集成实际贡献、执行全部原必需检查，再由原检查点和 Writer 确认接收。Main 按真实合并和检查事实处理实际变化；不为答卷接收或阶段出口登记、比较摘要或 Git OID。中断或失败后允许按原反馈重入同一 `runtime task-integrate` 命令，恢复尚未完成的合并、检查、确认或出口；原检查点避免重复合并和接收。Main 检查失败留下 preserved/blocked 检查点时，先解决原环境/检查原因，再按原 pending 的具名命令带 `--retry-preserved` 重试；裸重入会保留原失败。旧 closing 误挡可能覆盖合并结果未知的状态，不能只凭错误码自动重合并；已有 Main 合并事实沿已知边界续接，否则保留未知，由 Main 理解现场。已成功集成仅需接续出口时，不增加该重试前置。已迁移 S7 后的材料准备缺口沿反馈给出的原 `s7 draft --root <Main> --out <原路径>` 恢复。已有确认后的纯清理重试保留原验证事实，不因为 Main 后续合法推进而重新要求 Builder 工作。Hook 不同步执行长合并；PreCompact 不代收答卷或另建阶段 checkpoint。

本节 `runtime task-integrate` 适用于原 S6 集成；S9 沿上节原答卷合同接收。两种检查位置沿原登记选择，不能为省时把 Worker 自检抄成 Main PASS。本人真实未完成或缺实际必需检查时，不把工作当成完成；风险或范围说明非空本身不阻止 ACK 或阶段转换，也不意味着 Main 已批准偏差。S6 完成进入 S7 后，Main 仍要完成专业 ReviewPlan；S9 Main 集成、独立 Targeted 与原 Handoff 成立后进入 fresh S7，不能由候选接收跳过。

环境问题解决后按原 pending 重验保全候选；确需修代码时，原生 S6 使用上节具名 Assignment 修订，S9 使用原领域返工/解阻路径。ACK 中断续作及清理细节见[集成恢复参考](workspace-execution-recovery.md#main-集成与继续流程)。该入口不覆盖旧候选、不自动 reset，也不把失败改成 PASS。

## 统一提交回执

原接收入口和 `stage-answer receipt` 使用同一个 `submission-receipt.schema.json`。原生子代理在 Stop 接收，Main 规划在保存时接收；不能因接口相同就把两者的触发事件合并。

`submission_key` 是原领域的内部接收身份；S7 Result、S8 Case 和 S9 PlanReport/candidate/Result 沿各自已有责任关联。`current_stage` 表示查询当时的阶段，不标识历史回执的来源阶段。`submission_key`、`domain_refs` 和状态均为只读投影，不由作者修改或提供摘要。

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
原 ACK 沿已有责任和实际报告引用确认，cleanup 失败单列恢复，不延长已确认结果的反压；不要求报告 SHA。
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

Runtime 的真实事务中断沿当前提交和 journal 接线恢复；保留已接收事实与实际用户文件。旧 fingerprint 标记不再表示当前事务，也不阻挡正常 Writer；不要删除未知文件来换取推进。

先运行 `runtime recover inspect --root . --req <locked-REQ-path>`，从输出取得
`latest_checkpoint`，然后生成可审核的原 pair 计划：

```bash
.claude/bin/loop-harness runtime recover plan --root . \
  --req <locked-REQ-path> --dev-branch <bound-development-branch> \
  --release-upstream <bound-release-destination> --checkpoint <latest_checkpoint>
```

核对 `checkpoint_restoration` / `original_pair`、实际候选、责任与激活、修订及
失效记录、原 cursor 与迁移事件后，按原人闸执行
`runtime recover apply --root . --plan <plan-path> --approved-by <human-operator>`。
原合法 pending 优先于计划；半替换后重复同一 apply，不能重新生成接收键。
已处理接续保持已处理，未交的新 attempt 保持未交，旧 PASS 不复活。
计划应用拒绝仍持 launch lease 的 Worker、较新事实、资料漂移和已关闭 Runtime。
必要原材料缺失时如实保留未知，不从文件存在猜测专业签字。

## Hook 与指标维护

原生入口用外层子进程约束整个 Hook 为 8 秒，低于平台配置的 10 秒。完整结果返回前不输出部分 allow；超时/异常的潜在写操作返回阻断码 2，只读和生命周期通知保留诊断通道。中断的 Runtime 事务继续由原恢复协议处理。

计数型指标统一追加独立观察文件，不等待汇总锁。health/doctor、SessionStart 和 PreCompact 执行可恢复压缩：原子记录累计值和已消费文件，再删原文件，避免重复计数。普通 PreToolUse 不做压缩。持续长会话没有这些边界事件时仍应定期运行 health，指标不是业务恢复证据；不能对无限长、完全无维护的会话承诺磁盘恒定。

S5 答卷保存消费者的调用在 health/doctor 显示
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
提示。默认不启用提醒。
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
2. S5/S6 保留原产物交接合同；S9 在实际 worktree 保存原 PlanReport/Result，独立 Targeted 保存本人的原答卷。核对 Start 上下文、适用自然事件及原领域回执；S7/S8/S9 PreToolUse 观察就绪原答卷，成功 PostToolUse 和 Stop/SubagentStop 兜底，普通工具无需激活或工具准入。
3. 验证保存了但尚未接收、同答重放、来源缺失及并发中断时的原路恢复；不能把收到完成通知解释成领域已接收。
4. 对写责任运行真实自检、集成与必需 Main 检查；注入检查失败，确认候选/checkpoint 保全、后继未放行，恢复沿原待办而不改旧 PASS。
5. 按实际目标平台分别记录能力和结果；runner 隔离不可用、依赖缺失或未执行项如实说明。源码测试、CLI fixture、交叉编译和真实产品/浏览器运行分别证明各自范围。

实际集成与恢复见[恢复参考](workspace-execution-recovery.md)。版本、平台和实际流程证据保存在维护记录；本指南不充当完整 S0～S11 或产品验收报告。
