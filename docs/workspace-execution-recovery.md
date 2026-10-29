# 已登记执行恢复与底层运行参考

新原生责任先读[主指南](workspace-integration.md)。本参考保留已登记旧执行的完整操作，以及由原消费者调用的底层检查、集成和迁移边界，不创建新的派发流程。

| 下面的操作 | 何时适用 |
| --- | --- |
| workspace prepare/launch、Bootstrap、Claude UUID/session、旧 Team/readback、普通保存接收 | 原责任已冻结到该执行协议和来源；Main 核对原登记后恢复同一责任 |
| s6-v1.2、旧 Assignment context 的 PLAN/Result、旧 owner 修订与 workspace rework | 原 Record、execution 和 checkpoint 明确选择该协议；不能复制给 s6-native-v1 |
| Main 集成、检查 profile、可信 runner 回执、锁与 pending | 当前原记录实际提供该入口；它们也可供原生返回后的原消费者使用，不等于所有新 Agent 都要安装 Worker runner |
| adopt/replace/rebind | Main 显式恢复已存在的历史执行及 Git 坐标；不从当前 HEAD 补造来源 |

本参考中的旧 “native/teammate profile” 指原已冻结的 Assignment context／Team 接线，不是当前 Claude Code 原生 Agent 派发入口。所有命令均以原登记返回的精确参数为准，不从示例猜 owner、SHA 或路径。工具配置属于实际角色与平台，Hook 不对原始 Git、复合 shell 或一般工具建立 ACL；错误工程身份/来源仍由领域命令按原协议拒收。

## 已登记冻结执行：准备与启动

以下 prepare/launch 只用于原记录明确使用冻结 workspace 执行的责任；新 S6/S9 使用主指南的原生 brief，S5/S7/S8 使用原问卷。


```sh
loop-harness runtime workspace bind --root /project
loop-harness runtime workspace prepare --root /project --assignment assignment-example --agent agent-example
loop-harness runtime workspace launch --root /project --assignment assignment-example --agent agent-example
```

对已冻结的 S6 bound-workspace 责任恢复，Main 先用原 manifest 注册 Assignment，再对每个已登记
Assignment 运行 `workspace prepare --root <authority-root> --assignment <id> --agent <owner>`。
读取 prepare 返回的 `execution`、`assignment_context_ref` 和 `submission_paths` 中 PLAN/Result 的实际路径，
然后以同一 root、Assignment、owner 运行 `workspace launch`。Hook 为当前责任再给精确
view/answer 路径；作者照该路径填写专业答案并保存。prepare/launch 创建并登记原 Worker
checkout；这条已登记路径不再运行 `team launch`、导出 readback template 或手工建 worktree。
该目录的 JSON manifest 是显式旧协议 fixture，不是新原生派发模板；先核对原执行协议。

prepare 消费已经登记的 assignment/owner，接受 building 或 repairing 阶段的 S9 Assignment。S9 使用 Hook 给出的准确 PlanReport/RepairResult 路径保存专业答案，由原 consumer 投影生命周期和候选事实；Main 集成、必需检查、独立 TargetedReverification 与人类闸仍保留。不要把后文列出的低层兼容命令加成默认步骤。审批、派发、S7 领域审核登记等仍走 Main 控制端，原显式审批仍等待其人闸。

派发前检查 staged、unstaged、untracked 输入，存在未提交源码/文档就列出文件并拒绝，不自动 add-all、commit 或 stash。派发记录精确 base_commit、输入摘要、scope、checks、owner 和执行代次；恢复不重新捕获当前 HEAD。先持久化 preparing，再创建 worktree、安装资产，最后 ready。

Worker 安装当前原生二进制、最小 Hook 设置、已安装的角色/技能及 `.claude/inputs` 下的冻结输入文档；不复制 Runtime、journal、凭据或整个 Main `.claude`。Bootstrap 回执记录文件摘要，改变资产后不能直接复用旧执行。

launch 只设置子进程 cwd，不切换主会话目录。启动前登记独立 Claude UUID，持有进程锁防止重复启动，通过安装的 `--agent` 角色执行，默认 30 分钟进程时限。Worker 的可用工具由安装角色配置决定；没有 skip-permissions。PreToolUse 只监听当前问卷并提醒准确的 view/answer 绝对路径，不检查工具权限或 scope，也不拦截写入。需要其他原生工具的任务应按批准流程调整角色配置或执行方案。

重试用同一命令加 `--resume`，只恢复已登记 UUID。若进程在 Claude 创建会话前失败，resume 会如实失败；使用显式 replace 保留旧执行后重新派发。退出码 0 只代表 Claude 进程正常退出，不代表任务、S7 或发布完成。启动尝试日志/回执位于 Main `.claude/workspace-launch`。

## 已登记冻结执行：专业产物

### S5 独立文档审查

适用条件：原责任已登记到本节的冻结 workspace／Assignment context 协议。当前原生任务按[主指南](workspace-integration.md)执行，不新增本节启动或提交步骤。

新发行的 S5 packet 可包含内容寻址 `task_analysis`：固定解析器版本、正式
输入集合、TASK读集与粒度 advisory。prepare 将分析及其可读来源安装到
`.claude/inputs/` 并纳入原 Bootstrap 哈希。review-check 的 context/advice
读取原冻结字节，原机械 Problems/覆盖/DAG 仍独立求值。分析不签署 PASS、
不授权写入或下一阶段；来源漂移/原分析丢失须恢复或走原返工。旧已发行包
不会在重试时补分析字段；具名修订保留同一分析而不按新 parser 重算。

新分析可包含 `behavior_projection_version` / `behavior_examples`：仅投影 TASK
Manifest 明确引用的当前 CASE，经原 Scenario 源包与生成结果校验，保留真实
witness 输入、oracle、fixture、七个模块文件及实际校验的绑定 REQ 摘要。Mode、未知条件继续保留；
未决预期交回原规格责任。来源闭包也安装并冻结，漂移拒绝继续消费。这里没有
执行产品测试或浏览器，样例不是 PASS；旧分析不会追加此字段。

新分析还可冻结 `conventions_resolver_version` / `task_conventions`。只消费本人
scope 内有原 Rules 或登记文档证明的选择，保留例外及来源模式/摘要；registry
自称 approved 不构成批准。`candidate/conflict/stale/migration/superseded` 不
作为当前默认。prepare 核验实际安装输入等于冻结分析，接收继续检查原来源；
修订不刷新索引，旧分析不补字段。缺索引不会阻止普通授权工程选择，有影响
验收的决定缺口交原 Architect。参考投影不授予权限或替代专业结论。

Runtime v1.2/v1.3 Builder prepare 的 assignment-context 可包含 `conventions`：按原登记
TASK 与 Assignment 的共同范围求证，真实 Rule/registry/规范/示例摘要进入
原 frozen_inputs，Worker 从 `.claude/inputs/<原路径>` 读取。native 固定
context 同样列出实际来源和 input_read_order，登记锁内复核原提交视图。
S5 context 复用原已发行分析，旧已发行分析/ready execution/context binding
不补字段。该资料只说明执行开始时的参考；本人按原权限改变示例源码仍须
原 checks/report/Main verified integration。冲突/旧例子/未决范围交原责任，
不扩大 write_paths、授权 Skill 或专业结论。

当前问卷已由原消费者发行且 Runtime 启用问卷时，同一 `workspace prepare` 可准备只读 S5 document-review execution。新 install/init 默认 Runtime 1.3 且问卷开启，无需额外传 `--packet`；`--packet=false` 仅用于新 Runtime opt-out。已有 Runtime 或 pending 初始化沿用原冻结配置，不要手改或尝试原地切换。该执行绑定当前阶段、packet、owner 和独立目录代数；准备记录提交后，共享 Main 的同一答卷入口停用。

prepare/launch 给出完整命令及以下路径：

| 字段 | 用途 |
| --- | --- |
| work_packet_ref | `.claude/inputs/work-packet.json`，含原 packet 与不可变 execution view |
| frozen_input_root | execution view 的 `packet.draft.read[].path` 和 `packet.task_analysis.path` 从此目录下读取，题干中的权威相对路径保持原样 |
| answer_path | 本执行上下文给出的独占路径，与 view 的 `authoring_path` 一致；按原登记可为 `.loop-submissions/stage-answer.json` 或历史 `.claude/submissions/stage-answer.json`，只填写 answer |
| review_check_command | 原 TASK 语义检查的只读结果，不执行产品命令、不登记测试 PASS |
| receipt_command | 领域事实的只读回执 |

普通 `plan_checkpoint` S5 只有专业 answer 问卷：读取 `work_packet_ref`，在准确 `answer_path` 填 `answer` 并保存；原 PostToolUse consumer 接收并投影责任状态，不要求 generic PLAN、begin 或手工 answer command。只有当前 prepare 输出确实含 `plan_draft_path`/`begin_command` 的显式 `plan_approval_required` 或冻结 legacy execution 才按其原计划/审批流程继续；`answer_command` 是兼容入口，不是普通保存步骤。

成功 Write/Edit/MultiEdit 保存会返回正式领域接收回执。合法 Finding 经原接收者接受且本人检查满足后，将本人责任投影为 `done`；同事未交、系统 pending 或人工需求决定交给回执指明的 owner。Worker Stop 在 Controller 变更前检查本人责任，已接收或旧进入身份已被原路由取代时可结束。S5 execution 不提供产品 write scope、commit、Builder report、delivery 或 integration。view 和冻结资料计入 bootstrap 回执，答卷不计入其只读摘要；重放不得覆盖答案或 ready 资产。

### 构建与修复执行

适用条件：原责任已登记到本节的冻结 workspace／Assignment context 协议。当前原生任务按[主指南](workspace-integration.md)执行，不新增本节启动或提交步骤。

prepare 输出原 execution、准确 submission paths 和兼容命令。普通新路径按 Hook 给出的准确 view/answer 路径保存专业答卷；保存时由原 consumer 投影生命周期和领域事实。prepare/Hook 中出现的低层 begin/report/deliver 命令不是额外正常工序：仅在当前 Hook 明确给出恢复动作或 Runtime 明确走未发行的 legacy 路径时才调用。

| 字段 | 输入与行为 |
| --- | --- |
| `submission_paths` 中的 PLAN / Result 路径 | 当前 S6 assignment 的专业答卷路径；优先用 Hook 给出的 exact view/answer 路径保存，原 Writer 自动登记 begin/Result |
| check_commands | 执行已声明检查的零基索引；由当前 Result/检查问题要求时运行 |
| commit_command | 使用返回的 `submission_paths.commit`；只暂存明确文件并正常 Git commit，保留真实 source delivery |
| builder_receipt_command | S6 Builder 的只读当前交付/本人欠项/Main 待消费查询；S9 不使用此完成口径 |
| `begin_command` / `report_command` | 低层兼容入口：仅用于未发行 legacy Runtime 或 Hook 明确指示的恢复，不与已保存 PLAN/Result 重复调用 |
| `s9_delivery_command` | 未发行 S9 RepairResult packet 的 legacy 候选入口；packet-enabled 正常路径保存精确 RepairResult 答卷 |

在 packet-enabled S9 正常路径中，Hook 先给出 PlanReport 的准确问卷路径。Builder 填专业计划并保存；原 PlanReport Writer 投影既有 checkpoint/AgentBegin，图监听再给出下一步。Builder 随后在准确 RepairResult 路径填写专业结论和实际结果并保存；原 consumer 复核登记的实际 Git/source 与必要检查，接收候选并投影既有 completion。无需另交 generic `completion_report`、手工执行 `repair execution begin` 或运行 `workspace deliver`。Main 仍运行原候选集成和全部必需 Main checks；原 ChangeImpact、每个批准 Contract 一份独立 TargetedReverification、Handoff 和 fresh S7 保持。保存 PlanReport 本身不代表修复完成，保存 RepairResult 本身也不等于 Main 集成或独立复验通过。

未发行 typed RepairResult packet 的 legacy Runtime 可继续使用 `repair-result.json` 和 `s9_delivery_command` 原契约；只有该 legacy 路径明确要求时才提交原 generic completion。不要把 legacy 步骤带入 packet-enabled 新流程。

生命周期 JSON 使用既有 agent-message schema。普通 canonical S6 Builder 已在准确的
`submission_paths.commit` 提供原提交请求草稿，并带入起始 Worker HEAD；只填写
`message` 和实际源码文件 `paths`，然后运行返回的 `commit_command`。保存请求本身不会提交，
重新准备也不覆盖已有请求。旧实例的原请求格式如下；HEAD 漂移仍由原入口明确报错：

```json
{"expected_head":"<当前 Worker 的完整 HEAD SHA>","message":"implement assignment","paths":["src/example.go","src/example_test.go"]}
```

commit 拒绝目录、路径越界、未声明暂存文件和 HEAD 漂移。仓库 Git hooks 保持启用，失败保留 index；相同请求可识别已完成提交，避免重复提交。仓库 hooks 属于受信任项目代码，提交入口不是 OS 沙箱；发现 hook 携带未声明文件时保留提交并报错。

本节已冻结 S6 Builder 的操作顺序是：保存 Hook 提供的 PLAN answer → 按当前 TASK 实施并运行本人检查 →
按 `submission_paths.commit` 提交明确的源码改动 → 保存 Hook 提供的 Result answer →
Main 集成。原消费者根据保存的答卷登记对应生命周期；无需再运行 begin/report CLI。
源码变更须先按 `submission_paths.commit` 写明确请求，再执行原 `commit_command`；
Result 接收后，其交付源码快照已固定；后续修正走原返工或具名修订。漏提交时由 Main 保留原集成失败 checkpoint；
canonical S6 用原 `register-workgroup --revise-assignment` 具名修订形成新 attempt，
旧格式符合条件时沿原 `workspace rework`。不手改状态或直接 Git 提交。

check 当前支持 Linux amd64/arm64，要求 Bubblewrap、用户命名空间和 seccomp。检查在不超过 512 MiB 的副本中运行，排除 `.claude`/嵌套 `.worktrees`；Main 和 Git 元数据只读，禁止网络及 Unix socket、io_uring。声明命令需要缓存时使用沙箱内 `/tmp`。检查最长 5 分钟，日志/回执保存在 Main `.claude/evidence/workspace-checks`。不复制回构建产物，不把该回执代替实际合并检查。其他系统明确返回不可用，不降级成任意 shell。

Workspace registry 将 Worker 路径关联到原 Main Runtime，Assignment 的 write scope 是原派发契约。PreToolUse 不检查身份、权限或 scope，也不拒绝文件写入；原 PostToolUse/domain consumer 仍按各自语义校验是否接收已保存内容，并把拒绝原因作为反馈。Hook 不是 OS 级任意程序隔离。

### S6 显式 Assignment Record 与薄 PLAN

适用条件：原责任已登记到本节的冻结 workspace／Assignment context 协议。当前原生任务按[主指南](workspace-integration.md)执行，不新增本节启动或提交步骤。

已登记的 building 1.2 Builder manifest 可显式声明
`"assignment_record_contract": "s6-v1.2"`，并由原 `register-workgroup` 登记。
登记必须绑定当前锁定 TASK 与完整 reviewed dispatch plan；无标记沿用原流程。
实际角色、Skill、manifest 来自原安装路径，Schema/profile 来自 binary
嵌入字节，目标项目不需要 harness 的 `internal/` 源码目录。

在 `building` 阶段，PreToolUse 从当前 ready、非 DocumentReview 的 workspace
登记或原冻结 Assignment context 投影 PLAN/Result 问卷，并每次给出准确的
绝对路径。此 Hook 只监听并提醒，不检查工具权限或拦截写入。保存匹配的 PLAN
会调用原 `agent-begin`；保存匹配的 Result 会调用原 `task-complete`。原命令继续
校验专业答卷并推进自己的状态；Result 接收后，Main 的原 Inspect/Integrate 与
checks 仍需单独完成。常规 Worker 不手工运行 `stage-answer issue`/`consume`。

原登记的 Assignment/workspace context 回指同一 Runtime Record 的 ID、revision 和 SHA，
写入范围与检查要求由记录投影。读取 context 提供的 PLAN 草稿与 Schema，
本人填写 `objective`、`planned_paths`、`assertion_checks`、`dependencies`、
`risks_blockers` 五个专业字段。该显式分支不填 `steps`；依赖与风险的草稿
为 null，确认没有后才写 `[]`。禁止手改机器绑定或扩展有效范围。原
SendMessage(plan_ref)/`agent-begin` 激活链继续校验冻结来源；高风险模式
继续使用原审批闸。

canonical Result 通过原 `task-complete` 接收，填写 `summary`、
`assertion_evidence`、`risks_blockers`、`scope_deviations`。每个 assertion/oracle
须与已接收 PLAN 一致，并给出实际 evidence refs；不能填写机器 checks 或
changed_paths。原 Writer 保存接收时的耐久证据快照并返回 Result ref/SHA、
Result revision、AnswerDigest。相同答案重试保留 message_id，修正答案使用新
message_id；历史重放不改变当前 Result。副本损坏走原恢复，不覆盖旧事实。

此显式 S6 Builder 的 `done_when` 描述本人交付，须能在提交 Result 前验证。
接收回执在提交后证明 Result 注册；Main 的 `required_checks` 仍由集成者实跑。

#### 显式 S6 closing contract

适用条件：原责任已登记到本节的冻结 workspace／Assignment context 协议。当前原生任务按[主指南](workspace-integration.md)执行，不新增本节启动或提交步骤。

本协议的原 manifest 由 Main 明确选择 `closing_contract`：
`contract: "s6-closing-v1"`，并提供 `worker_checks` 数组；同一 assignment
要求同一 assignment 明确 `integration_check_mode: "post_merge"`；原来的 Main
`check_profiles` 必须是 `location: "main"`。原 Register 将这些条件冻结进当前
Assignment Record。`worker_checks: []` 表示原派发明确没有本人 runner checks；
缺少 closing contract 的旧 Record 不补字段，也不回溯成新契约。

closing contract 下，PLAN 的每项 `assertion_checks` 都要填非空、去重的
`done_when_indexes`，引用 Record 中 DoneWhen 的零基索引并覆盖每条 DoneWhen。
写 Result 时使用原 原登记的 Assignment/workspace context 提供的 `result_path` 和完整
`result_draft`，并查看 `result_questions`/`result_schema_ref`。Record、revision、
Runtime、owner 等机器字段已经由原 context 填好；不要手工重建或修改它们。只
填写草稿中的五个 null 专业字段：`summary`、`assertion_evidence`、
`risks_blockers`、`scope_deviations` 和 `closing_assessment`。

`closing_assessment` 要逐条判断每个 DoneWhen，格式与字段说明见 context：
`disposition` 为 `complete`、`incomplete` 或 `blocked`；这是专业评估结果值，不是责任状态；每个条件带零基
`done_when_index`、`satisfied`/`incomplete`/`blocked` 的 `judgment`、引用已接收
PLAN assertions 的 `plan_assertion_indexes` 和非空 `rationale`。complete 要求每条
判断都为 satisfied；声明风险/阻断或 scope deviation 时本人不能闭合。它是本人
专业判断，不是 runner PASS 或独立验收。

显式空 `worker_checks` 下，完整且来源有效的 complete 判断、无风险/偏差时，原
Builder receipt 可显示兼容字段 `work_complete=true`，将本人责任投影为 `done` 并将 Main integration 单独列为等待。
Main 仍须完成原合并与全部 Main `required_checks`。合法 incomplete/blocked、未决
风险/偏差或本人检查没有当前原 PASS 证明时，Main Inspect/Integrate 与 ACK 保持
阻断，TR-006 not ready。Main machine checkpoint 与本人专业闭合分别投影；
fresh S7 仍由原独立流程决定。

非空本人检查使用 context/prepare 返回的 `owned_check_commands`：workspace
命令选 `--owned-check <index>`，不能与 Main 的 `--check` 混用；native 使用
`runtime assignment-context check` 的冻结索引。命令与 profile 来自 Record，
检查必须经原隔离 runner 捕获，失败也保留原失败回执。缺隔离能力不会切到
未隔离执行。native 当前要求已固定 context、已激活 PLAN 及 manifest 冻结的
实际 worktree；后来 sidecar 中的坐标或审批模式尚不能借此获得授权。

Result 接收保存每项 `worker_check_facts` 和 `tool_closure_digest` 的机器事实及
receipt/log 耐久副本，专业草稿不能填写这些字段。同 message_id/答案重试使用
原耐久回执、零写；新 message_id 才收集当前工具事实。同答案但工具事实变化
会产生新 Result revision，重新等待 Main；内容相同的新键由原 Writer 记录
alias 后返回原 Result，之后 alias 重放也零写。搬迁或 native HEAD 变化后的
新接收需要 fresh 检查，历史 Result 重放无需恢复已清理的 worker。

本节的检查命令仅服务原已固定 context、PLAN 激活和实际执行来源。Assignment 修订保留 exact TASK/plan_checkpoint 的原约束；审批版本与 TASK 来源变更不能靠复用旧回执获得授权。宿主不可用时保留明确能力失败，不把源代码测试或交叉编译写成该宿主 PASS。

#### 原 Assignment 修订入口

适用条件：原责任已登记到本节的冻结 workspace／Assignment context 协议。当前原生任务按[主指南](workspace-integration.md)执行，不新增本节启动或提交步骤。

代码修正时，Hook 给出原重试和具名修订命令；也可用只读
`runtime workspace pending --root .` 查看当前完整指令。Main 通过原准备入口生成
fresh manifest，只选择新的执行人员和修订意图，不重抄原合同或猜清单路径：

```bash
loop-harness s6 prepare --root . --task TASK-001-01 \
  --role backend-builder --agent-id <new-agent> --platform-team-id <new-team> \
  --revise-assignment assignment-task-001-01 \
  --revision-intent-key task-001-01-charter-r2
```

准备返回 `.claude/workgroups/<REQ>/<TASK>/manifest-s6-r<N>.json` 的准确路径，
保留原范围、检查及履职条件，并带出原 Record ref/SHA 的登记命令。它不登记或
改 Runtime，也不覆盖已有草稿。Main 审核后执行返回的原 `register-workgroup`
命令，再按原 `workspace prepare → launch` 派发新 Builder。
直接登记的兼容格式如下：

```bash
loop-harness runtime register-workgroup --root . \
  --manifest .claude/workgroups/manifest-s6-r2.json \
  --task-id TASK-001-01 --task docs/dev/tasks/TASK-001-01.md \
  --revise-assignment assignment-task-001-01 \
  --revision-intent-key task-001-01-charter-r2
```

同意图重试保留 revision-intent-key；改动输入需要新键。当前首个切片要求
same Runtime/baseline、同 exact locked TASK ID/path/SHA 以及 plan_checkpoint。
原 Writer 分配下一 Assignment revision，调用者不能指定。若显式断言旧版本，
`--expected-record-ref`（RecordReference JSON）必须与 `--expected-record-sha256`
成对提供；缺省时由原 preflight 捕获当前前版，锁内再验证。原 Main workspace
锁与 Runtime Writer 锁串行化发行及集成，busy/stale 不产生新版本。

revision 输出额外提供 `assignment_revision_receipt`：原 Record ref/SHA、
revision、原 event/commit 与 current/replayed 标记。历史意图重放返回历史原
发行回执，不把当前 Runtime 提交序号当发行序号，也不回滚现有最高版本。
旧 Owner 的当前 PLAN/Result/ACK 失效，历史字节与源快照保留；新 Owner 重新
取得 context、填写 PLAN/Result，并等待自己版本的原 Main 检查。人闸版本
绑定与 TASK 来源版本变化仍未开放。

新源分支可从立即前版已合入的 source commit 继续提交，但原 Integrator 必须
先验证前版 Record 的完整源快照、精确 superseded Result、原发行 journal、
独立 checkpoint/原检查日志及真实双父 merge 关系。通过后只允许接续集成，
新版本仍生成自己的 merge 并运行全部 Main checks。缺来源、多个旧 merge
候选或没有新提交时保留 pending；可从当前 Main 建立新分支走原正常流程。
前版 checkpoint 和 ACK 不变成新版本的完成凭据。

#### 固定产品基线的共同来源

S7/S10 使用的 start..final、Main contribution、Record/Result、检查点及真实 Git 来源同时服务当前原生交付和显式旧记录，统一见[主指南的固定产品基线](workspace-integration.md#固定产品基线与-s7-接续)。冻结执行不会因为换 launcher 或恢复目录而另造产品基线，也不能回落作者自报的变化集合。

### S6 本人交付回执

适用条件：原责任已登记到本节的冻结 workspace／Assignment context 协议。当前原生任务按[主指南](workspace-integration.md)执行，不新增本节启动或提交步骤。

Builder 报告登记后，Worker 使用 prepare 返回的精确 `builder_receipt_command`；
Main 可查询：

```sh
loop-harness runtime task-receipt --root /project --agent-id agent-example --assignment-id assignment-example
```

回执只读原 TASK/Agent、报告索引/哈希、当前正式 TASK 指纹和原 integration
checkpoint。`delivery_recorded` 与 `accepted`、`work_complete` 分开；本人检查
失败/未运行/缺漏、scope deviation、缺 closing contract 明确列入 `own_pending`。
`checks_source=original_report` 表示读取原报告，不表示查询重新执行过命令。
本人合格但尚未集成时，兼容字段 `work_complete=true` 且 `waiting_on` 指向 Main；
当前原 receipt 与本人检查有效时，本人责任投影为 `done`；
`integrated=true` 另需原真实 merge/verified、当前报告绑定与目标分支可达性。

上述本人合格条件为原通用 completion 的已实现语义。显式 `s6-v1.2` 使用
`checks_source=main_checkpoint`，同时显示接收 Result/PLAN/Record 和耐久证据
引用，以及原 Main 实际 diff、完整 required check receipts。带新 closing contract
时，只有显式空本人检查集合、有效 complete 专业判断且无风险/偏差才可令
`work_complete=true`。合法 incomplete/blocked 或 Main 闭合阻断保持本人欠项；
Main verified 不能代替该判断。缺 closing contract 的旧 Record 保持
`professional_status=accepted_unassessed` 与 `professional_closing_contract_unproven`，
不追补或暗迁移。

登记证据丢失/被改或 TASK 输入改变时，回执失效并指向 Main 的恢复/原返工；不
让作者重复承担系统恢复。Main 经原 `workspace rework` 重新开放失败任务后，旧
结果引用清空，回执显示本次尚未提交，历史文件保留。查询不改变 Runtime、权限、
TR-006 完整批次条件、S7 独立性或发布人闸。S5 仍用 answer receipt，S9 仍用原
RepairResult；这个 S6 查询不能把通用修复报告当作修复完成。

## 合并、S9 与继续流程

本节保存原已登记执行/检查点的集成与恢复细节。新原生 S6 的派发和修订见[主指南](workspace-integration.md#s6-原生实现与本人交付)；共享 Main Integrator 仍按原记录处理实际候选。

```sh
loop-harness runtime task-integrate --root /project --assignment-id assignment-example
loop-harness runtime workspace pending --root /project
```

未指定 `integration_check_mode` 时保留 Worker 预检与 Main 合并后检查；普通任务不默认选择 `post_merge`。新 S6 closing contract 的首个空本人集合路径要求原 manifest 显式选择 `post_merge`，并把 Main profile 固定为 `location: main`；不把 Main 命令复制成 Worker checks。两种模式都检查工作区身份、范围和冻结文档，并且都必须完成 Main 合并后的必需检查。closing 未闭合时，原 Main Inspect/Integrate 保全检查点并阻止 ACK；canonical Result 的路径与 SHA256 一起绑定到回执，Result 改变须重新验证，不能复用旧 PASS。首次接收确认前目标 HEAD 改变也须重验；已经确认后的纯清理重试保留原验证回执。

Main 从原生返回回执及 SessionStart 的当前职责提示消费显式待办，不在 Hook 中同步跑长合并。PreCompact 保持静默，不代收答卷或另外创建阶段 checkpoint。pending 从原报告/checkpoint 生成命令，不建立第二套调度状态。

RepairResult 被原 consumer 接收后，Main 用原集成和必需检查完成候选接收；TargetedReverification 每个获批 Contract 一份，之后原 Handoff 返回 S7。canonical S6 generation 的 TR-012 会按原链路提供 S7 seed，Planner 仍须完成专业 Claim review；Session 累计差异、独立复验和 fresh S7 门禁继续生效。未发行 typed Result packet 的 legacy Runtime 保留原 `repair-result.json` / `s9_delivery_command` 契约。自动投影不代表 Main 集成、独立复验或人类发布授权通过；实际产品和宿主的完整运行验收需有独立证据。

typed S9 的 `completed` 答卷仍须全部 unit pass、无 scope deviation，并通过原候选与
Main 集成链。`blocked`、`contract_conflict`、`stale` 走原 blocker 接收，回执保持
`work_complete=false`，Worker 等待 Main 指定消费者。Main 的原 resolution 接收后，
可恢复项发行绑定前驱与 resolution 原事件的新 packet/answer；旧答案不恢复产品权限。
`contract_conflict` 仍回原 S8 人闸。相同解阻事件重放不重复接收，不能改填 pass。

已接收 typed Result 后、ACK 前的中断，重试同一 `workspace integrate` 会复验原回执并继续 ACK/cleanup，保留原检查事实。若 Main target 已离开原 TestedHead，命令在改写 checkpoint 前拒绝；先用 `runtime repair status` 和 `runtime workspace integration-status` 查看状态，由 Main 安排新的授权验证尝试。此情形的自动后继尝试尚未接线，不能用 `--retry-preserved` 或仅限未 verified checkpoint 的 `workspace rework` 绕过原回执。ACK 之后的清理重试仍允许 Main 后续合法前进。

失败保留源树/checkpoint。环境问题解决后使用 `--retry-preserved` 重验原候选。若需要修改代码，由 Main 执行 `workspace rework --root /project --assignment <id> --agent <owner> --reason <具体修正原因>`：仅接受未 verified 的失败 checkpoint，归档旧 checkpoint/候选，按实体生命周期把 reported owner/TASK review 退回 working/in_progress，并将原 completion evidence 标为 superseded；完成后 Worker 修改、commit、report，S9 再 deliver 新候选。不会覆盖旧候选或把失败改成 PASS。中断的返工也由 pending 投影恢复命令。已验证交付通过真实 completion_acknowledged 生命周期确认，同一 owner 的其他交付也必须验证。清理失败停在 cleanup_pending，重试继续清理，不强删、不全局 prune。清理完成后，由同一 durable checkpoint 将 ExecutionRegistry 投影为 complete；若 Runtime CAS 前中断，pending 会继续提示 Main 重试。complete 实例不能重新启动 Worker；未发行 typed Result 的兼容实例仍允许 Main 重复集成或报告重验，已接收 typed Result 保持原不可变验证事实。已有 failed 领域结果要走原领域恢复路径，不能通过替换 worktree 抹掉失败。

上述同 owner 的 `workspace rework` 用于旧格式及原 S9 候选。canonical S6 有显式
Assignment Record 时，修正代码／重新报告沿[原 Assignment 修订入口](#原-assignment-修订入口)：
Main 提供 fresh manifest、Team 和 Agent，以新 Record 形成下一 attempt。
原 checkpoint、Result、源树保留；不把 superseded Result 当作当前有效交付，
也不在同一 Record 内回退 reported owner。

## 显式迁移与恢复

所有恢复命令只能由 Main 执行，并要求 `--reason`。

- `workspace adopt --root /project --assignment <id> --agent <owner> --request historical-execution.json --reason ...`：请求是完整 Execution JSON，必须提供真实历史 base_commit、输入摘要、原 branch/path/target、runtime/baseline/execution generation、scope/checks。校验当前登记 assignment、仓库归属和历史差异；拒绝第二份 Runtime/journal。不会把当前 HEAD 猜成历史基线，缺失来源时需重新建立已批准基线。旧 PASS 不升级成新 verified。
- `workspace replace --root /project --assignment <id> --agent <owner> --execution-generation <旧代次> --reason ...`：要求没有活跃 launcher 锁、已登记 completion/candidate/integration checkpoint。保留旧树及 execution_history、原 owner 和冻结基线，新建代次/路径/分支。已有交付必须先走原集成/领域恢复；不能借替换绕过它。
- `workspace rebind --root /new/project --request relocation.json --reason ...`：先由 Git 完成 native worktree move/repair，再重绑 Harness。要求旧 Main 已不存在、明确 runtime/旧新 Main/旧新 common directory/branch/expected_head，以及每个活动和历史 Worker 的路径映射；拒绝仍活跃 launcher 及未完成交付/checkpoint。先保存 before/after 迁移意图，再 CAS 改绑定，随后迁移已完成 checkpoint 的坐标并更新活动 Worker 指针；相同请求可继续中断的更新。该入口不自动搬文件或修 Git 元数据，也不改变目标分支。

relocation.json 示例（路径映射必须完整）：

```json
{"runtime_id":"loop-example","old_main_root":"/old/project","old_common_dir":"/old/project/.git","new_main_root":"/new/project","new_common_dir":"/new/project/.git","branch":"test2","expected_head":"<当前完整 SHA>","paths":{"/old/project/.worktrees/assignment-example-g1-e1":"/new/project/.worktrees/assignment-example-g1-e1"}}
```

Runtime workspace 仍是 schema 1.1.0 的可选扩展，绑定前统一更新二进制；历史数据原样保留。执行新模式前做 Runtime/journal 配对备份，旧二进制不应写入不认识的扩展。回滚程序不能撤销已发生的 Git 合并。

## 完成报告的准确身份

原 Assignment 明确登记的 `CompletionRef` 是权威来源；缺失或不可读时集成与恢复均不能改用另一份报告。没有显式引用或原已绑定 inspection 时，仅从 `.claude/evidence/<current-runtime>/g<current-generation>/assignments/<assignment>/completion.json` 发现。Runtime/代际未知，或使用历史及其他布局时须明确恢复原来源；不扫描其他 Runtime 或选择第一个候选。原证据身份、摘要与质量门继续适用。

## Worker 观察与检查能力

本节仅针对已登记冻结执行的观察 adapter、Bootstrap 和原 runner 能力；不约束当前原生 Agent 的普通工具使用。

prepare 输出 `observation_commands`，包含 cwd、status、diff、staged、log 五个固定视图。
例如使用 prepare 返回的完整命令调用 `runtime workspace observe --view log`。
命令已经包含 Worker root、assignment 和 owner，不必自己构造身份。原始 `pwd` 可以使用；
原始 Git shell 被限制时，Hook 返回对应 observe 命令。观察不写完成证据，不改变 Runtime revision。
Git 调用禁用外部 diff/textconv、pager、fsmonitor 和惰性抓取，清除 Git 环境覆盖，最多输出 1 MiB、执行 10 秒。
不接受自定义 flags、重定向或复合 shell；源码文件可正常保存，commit adapter 仍按原规则校验显式暂存文件和提交范围。PreToolUse 不拦截源码写入；原 Assignment scope、consumer 校验与 Main 集成检查继续按各自流程生效。

`runtime workspace prepare --check-location worker` 是兼容默认，prepare/launch 会先探测实际
隔离能力和 512 MiB 检查树上限。网络依赖需预先准备离线缓存。
不适用时可在新执行实例上显式选择 `--check-location main`，由 Main 集成检查通道运行注册检查；
这不放开 Worker shell，不跳过 required_checks、独立验证或发布人闸。
已存在执行实例的 runner 不可切换；没有交付/checkpoint 的实例可按原 replace 约束替换。
replace 默认保留原 runner；可在满足原有无交付、无 checkpoint、无活动 launcher 的条件下显式指定 `--check-location main` 或 `worker`，新代次记录选择，旧代次保持历史。
macOS/Windows 用户应明确选择 Main runner，不能把 Claude 自带 sandbox 等同于此框架的 Linux 检查实现。

rebind 的迁移 intent 保存不可变源快照和请求，只允许路径坐标改变；Runtime/journal 沿用原提交恢复机制。
已完成交付可以迁移，保留 immutable candidate、报告哈希、source/merge/tested SHA 和检查凭据原文；绝对路径按显式映射更新，历史检查 CWD 保存在 checkpoint 中。未完成交付仍拒绝迁移。旧版 ready 条目若对应完整 complete checkpoint，可在迁移后通过独立 Runtime CAS 恢复终态，无需重建已清理 Worker。
同一请求可恢复 intent 已写入、Runtime 已写但 journal 未完成、checkpoint 尚未迁移、终态尚未投影及部分 Worker pointer 更新的窗口。迁移后可重复集成；重新提交 canonical Result 仍须重验，不能凭历史 PASS 放行。
普通 Writer 仍拒绝旧 authority 外的访问；遇到非迁移 pending 事务需恢复原路径处理该事务，不能 force 跳过。

观察 adapter 会在 Git 身份检查前拒绝继承的 `GIT_DIR`、`GIT_WORK_TREE`、`GIT_TRACE` 等 Git 环境覆盖变量；按提示从调用环境清除后再执行。pager 和 external diff 环境设置会被忽略，不能由观察命令启用。

### 冻结派发资料与计划支架

新 1.2 workspace prepare 返回 `context_mode=shadow` 和
`assignment_context_ref=.claude/inputs/assignment-context.json`。先读此文件：
它给出实际 owner/assignment/Team/TASK、职责、冻结资料、原技能、检查和收尾条件。
技能候选保留来源与摘要；当前 legacy 加载、角色 frontmatter 与激活权限均保持，
未宣称已经减载或实际加载完成。技术栈没有已批准机器来源时不从空模板猜测。

只有原 Assignment/prepare context 明确提供 `plan_draft_path` 时，才填写该 Plan
草稿的专业字段。S6 Builder 的 objective、steps、assertion_checks、risks_blockers
由本人判断；保存准确路径后，原 consumer 自动执行 PLAN/AgentBegin 校验与投影，
不再手工运行 prepare 返回的 begin。普通 S5/S7/S8 专业问卷不要求另造 generic
PLAN 文件；跟随 Hook 给出的 domain answer 路径。计划草稿在保存并被接收前不是
有效 PLAN_REPORT 或批准；显式 `plan_approval_required` 仍走原人闸。plan/answer
作者文件不进入只读 BootstrapReceipt，重试不覆盖作者字节。

技能、原协议和规则文件均需安装来源可读；缺失由 Main 修复，不回落工厂。
新冻结来源进入原 Execution.inputs/BootstrapReceipt，漂移拒绝继续使用；已有 ready
或 legacy preparing 资产不自动重写。显式 replace 保留原冻结来源/profile，重绑
execution generation；原目录与 context 保留。生命周期提示使用统一800 Unicode
字符预算，完整动作与读入口优先，长 scope 和提示不会挤掉它们。

### Backend execution 的 core profile

新1.2 S6 backend-builder 可由 Main 显式准备：

```sh
loop-harness runtime workspace prepare --root . \
  --assignment <actual-assignment> --agent <actual-owner> \
  --context-profile backend-core --check-location main
```

默认仍 shadow。返回 context_mode=enabled，固定 context 包含原 profile/hash 和
generated_role 的来源、目标及摘要。新 Worker 的 backend-builder.md 只预加载
四core；额外 selected Skills 由原 Read 工具读安装路径，allowedTools 不新增 Skill。
原角色 tools/model/permissionMode/disallowedTools、正文、manifest/activation
SkillRefs 和 required_checks 保持；Main 角色与安装回执不改。Main runner 的
选择仍走原检查通道，不跳过检查。

既有 ready/preparing 不能通过新选择换 profile；省略参数沿用已固定选择，
显式不同值拒绝。显式原 replace 保留同一生成字节和 profile，仅换 execution
代数。其他角色/阶段/旧1.1不能启用此首版。role/source/hash/symlink 冲突须
恢复原来源，不覆写现有资产。原 begin/commit/report/integrate、专业判断、
独立验证与发布人闸保持。宿主实际消费/token改善需要另行验收。

### Native/teammate 的注册资料视图

没有独立 execution 的已注册1.2 owner 可在 native lifecycle 或 registered
`team launch` 得到 `assignment_context_ref`（path/SHA256）。在原 Main 根读取，
视图列出实际 source_root、agent_state、原 manifest、资料顺序与安装 Skill 摘要。
计划支架和 Schema 与 workspace 使用同一生成逻辑；原 Agent 已 working 时复用
原接受的计划/激活，不因为支架出现就重新提交。已有作者文件不覆盖。

示例/未注册 launch 没有注册资料视图；注册 launch 的文档必须精确匹配原
manifest 引用。缺失、漂移、错误 owner 或 symlink 由 Main 修复。此视图是
shadow 资料，不是 ready、权限或完成证明。已经声明的独立 execution 始终走
原冻结副本，不回落 Main；旧1.1 不增加此资料入口。

### 固定 native/teammate profile 与生成候选

Main 运行 `runtime assignment-context prepare --root . --agent-id <actual-owner>`
将现有1.2注册责任的资料引用/代数/profile登记在原 Agent.context_binding。之后
lifecycle/readback复用原来源，不重新选择当前二进制的profile。原资料漂移或
损坏需要恢复匹配来源；实际Agent状态、计划/激活和完成仍来自当前Runtime，
不能把视图捕获的agent_state当当前授权。已声明独立execution继续原冻结副本。

S5具名修订仅允许 `--replace-generation <old>`，新卷必须指向已接收前驱，
原role/profile/Skill/Schema保持。成功后重试保持原代数，不覆盖作者文件。
绑定亦触发完整pair checkpoint；文档重建不能丢失固定profile后再选新版本。

`runtime assignment-context render-role --root . --role backend-builder` 给出
实际安装来源/SHA、核心Skill列表和生成的角色字节，enabled=false。它只减小
Skill preload块，不修改安装文件或Runtime；实际执行启用和宿主消费仍待接线。

## 检查能力与故障回执

本节是原已登记 runner 的能力和证据接口。原生 Agent 普通自检不因此要求 prepare/launch；Main/Worker 只有在当前原记录指定 profile/runner 时使用对应检查。

Main 检查模式在原 prepare/launch 前核验实际 shell 与简单字面命令的执行
入口；明确缺失工具不发布 ready，不替 Worker 自动改 runner。它不会执行
检查、安装或版本脚本来探测环境。复杂 shell/变量/控制流保留 legacy 语义，
明确标为 unverified；依赖、配置、fixture 与服务尚未完整 profile 化，因此
入口可用不等于环境全部 verified。

使用 prepare 给出的 `observation_commands["check-capabilities"]` 只读查看
当前环境；同一原 owner/assignment、路径和 Bootstrap 校验仍适用。Worker
观察会走原隔离预检，不能变成未隔离执行入口；支持平台/host 限制如实列出。

实际检查失败回执保留旧字段并附 failure：code/category/owner/check、观察、
恢复/重试条件及 receipt/log。非零退出只说明 process_exit，不能凭输出猜
assertion_failed。取消/超时/诊断 I/O 与 input_drift 归明确责任；原候选与
checkpoint 保留。没有完整环境/profile 键的旧检查，复用仍为 never，
Main 必需合并检查和独立 fresh S7 必须照原合同执行。

## 显式 CheckProfile

Runtime v1.2/v1.3 Assignment 的原 manifest 可选 `check_profiles`，由嵌入
`check-profile.schema.json` 校验。每个 profile 的 check_index/command 必须
逐字命中原 required_checks，不新增/替换命令；location 必须与新执行选择
一致，timeout_seconds 为1–300秒且更短的外层截止仍优先。旧 ready execution
不补字段，原 replace/adopt/rebind 保留原资料。依赖/fixture路径与摘要进入
原 frozen inputs，prepare/launch 核验真实来源和白名单环境，不运行安装脚本。

`inherited` 保持继承环境；`declared_only` 仅传 profile 的批准白名单事实，
null 表示必须 unset，空字符串不等于 unset。当前 worker/main 的实际回执
profile_observation 采集受测树、runner、环境、依赖/fixture指纹，check_binding
注明原Runtime/generation/Assignment/owner/责任。profile检查产生 v2 回执，
附日志摘要和 receipt_integrity；这只证明完整性，不证明领域批准或可信复用。

默认 shadow 会实际执行检查，reuse_eligible=false；仅下文明确启用的闭合静态 Worker 检查可以复用原可信成功回执。不得用自写回执、文件存在或
自重算完整性值替代原领域接收。Main必需合并检查、作者与独立责任、fresh S7
保持；非静态程序与未知环境不因存在旧回执而获得复用资格。

## 工具回执登记与 shadow 观察

新 profile 回执由实际 runner 捕获内存字节摘要；Worker 通过原 Writer/CAS
登记到当前 Execution，Main 通过原 integration checkpoint 保存可信引用。
登记含原责任绑定、profile、checkout、回执与日志摘要，不增加专业 PASS 或
完成事实。核验拒绝改写后重新封印的回执、日志漂移、未登记来源和责任变化。
旧回执继续供原领域消费，不能自动升级为新复用凭据。

prepare 返回的 `observation_commands["check-reuse"]` 是固定只读入口。
默认 shadow 决策均为 `action=run`；Main 必需检查、独立职责和 fresh 检查照常
执行。Worker 权限/隔离指纹未知或实际预检不可用时返回 unknown_permission_conditions；
即使纯函数 shadow 的完整同责任键匹配，也仍实际执行。原显式 rebind 仅移动
证据坐标，保留原 checkout、回执字节和摘要，移动后的 checkout 不自动命中。

新 Worker 观测记录实际 Bubblewrap、shell/动态库、seccomp/argv policy、
platform/kernel、uid/gid/groups 和原 scope/冻结输入/Bootstrap 摘要。采集与
原执行共用同一隔离参数和过滤器；真实预检失败不切换 runner、不启用复用。
旧回执没有 permissions 字段时仍按原领域消费，但不能作为新 shadow 命中来源。

首版闭合静态语法仅 `true`、`false`、`test -f <相对文件>` 和 `test -s <相对文件>`。
路径必须位于实际受测树内；任意 shell/外部程序不能仅凭 whole_tree 声明获得
命中资格。只读投影使用真实检查的临时副本算法，包含实际归一化目录模式。
缺省仍为 shadow，所有原检查继续实际执行。

## 显式静态复用与进行中诊断

新 manifest profile 可选 `reuse_mode: enabled`，仅允许原 Worker 的闭合静态
检查、same_input_same_responsibility、declared_only 和无副作用。缺省/旧 profile
仍 shadow；此字段属于冻结摘要，旧 shadow 回执不能升级。第一次仍实际执行，
之后只有当前实际输入、权限和原已登记成功回执完全一致时返回原引用。
不产生新 PASS；Main 必需检查、独立判断、S9/fresh 均实际重跑。

同责任/profile/源树请求通过进程锁串行。竞争请求返回原 run.json 诊断引用，
原 Main integration 锁和 Writer 接收仍保持。`observation_commands["check-running"]`
只读查看至多64项诊断；它不是存活证明、完成或接收凭据。进程死亡后重新
取得锁会保留 interrupted 记录，不从未完成文件推断 PASS。取得锁后重新读取
原 Runtime/执行/owner，再核对实际受测副本；损坏或缺失回执只能重跑。

## 冻结执行的真实会话验收

仅为原已登记 workspace/launcher 责任执行本组专项；新原生链路使用[主指南的验收安排](workspace-integration.md#实际验收与证据范围)。在隔离项目记录 Claude Code、Harness、产品提交与宿主版本：

1. 核对原 Assignment/owner/base_commit/source，按原 prepare/launch 恢复两个不同 scope 的冻结执行，确认 Main 目录/分支不变。
2. 在原 Worker 运行实际观察命令并核对冻结输入、Bootstrap、独立目录和已登记 Claude UUID。普通 Git 与复合 shell 不应因 Hook 的工具 ACL 被拒绝；领域命令仍拒绝错 owner、跨责任来源或漂移。
3. 核实项目所需离线依赖与真实检查能力。隔离能力/目录界限不足时保留原能力失败；入口检查不会推断所有依赖齐全，不自动安装或切换 runner。
4. 沿原冻结协议的 begin/commit/report/deliver 完成交付，再由 Main 实际集成与运行必需检查。这些动作不作为新原生责任的步骤。
5. 注入合并后失败，核对候选/checkpoint 保留且后继未放行；按原 retry/rework 继续，不自动 reset。
6. 停止实际 launcher 后迁移目录，先修复 Git worktree 链接，再按明确映射 rebind；中断重试核对 Runtime/journal 和 Worker pointer。
7. 对实际声明支持的 runner/宿主分别记录结果，不以交叉编译或未运行环境代替。
