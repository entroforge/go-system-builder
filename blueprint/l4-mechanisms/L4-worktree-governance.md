# L4 Worktree、交付集成与恢复

## 1. 隔离工作区解决什么问题

多位执行者同时修改产品时，需要分别保留本人改动、检查与失败现场；Main 又必须把这些贡献组成一份真实、可验证的交付。若大家直接写同一目录，难以区分来源和冲突；若把子工作区提交或退出当成完成，Main 中可能仍没有成果，也没有检查过合并后的树。

Worktree 提供隔离的 Git 工作目录。工程机制在其上明确来源、目标、接收和恢复，让一份贡献经历“本人交付 → Main 集成 → 必要检查 → 确认 → 清理”。这些事实分别发生，不能用一条 completed 消息相互替代。

本机制服务 S1 的目标绑定、S6／S9 的写入贡献及 Main 集成，也支撑 S7 读取真实产品基线。它不管理 Claude Code 进程，不给原生 Worker 增加启动许可；原生派发与两端接入见[子代理工程衔接](L4-agent-dispatch-governance.md)。

设计依据是 [L1](../l1-principles/L1-design-principles.md) 的外置权威、自然路径观测和独立背书。集成检查与独立专业验证分别承担责任：合并树上的命令通过，不自动等于需求已兑现、完整审查通过或获得发布授权。

## 2. 三种位置与四类目标怎样区分

### 2.1 控制根、执行根与 Git 公共目录

| 位置 | 保存什么、承担什么 | 如何确定 |
| --- | --- | --- |
| 项目权威控制根 | 当前 REQ 绑定、Runtime、journal、原责任、接收与集成事实 | 原 REQ 的 workspace binding |
| 本人执行根 | 隔离产品、测试、私人专业计划／结果及现场 | 平台实际 worktree 或已登记冻结来源 |
| Git 公共目录 | 多个 checkout 共享的仓库对象与引用 | 用于核对是否同一仓库，不决定工程控制根 |

Main 自身可以是 linked worktree。此时它仍是本次绑定的控制根，不能因为 `git common dir` 指向另一 checkout，就将 Runtime 或集成目标搬过去。

Worker 不复制 Runtime／journal 形成第二写者，也不靠 Git 合并活跃控制状态。私人计划和结果可以先保存在执行根，再由原消费者持久化需要的工程事实。文件来源与 Writer 的权威边界见[事务核心](L4-state-transition-core.md)。

### 2.2 需求开发目标与最终发布目标

原绑定明确声明 `project_root`、`dev_branch`、`release_upstream` 和 `bound_commit`。开发贡献集成到 `dev_branch`；最终发布目标由 `release_upstream` 表达；绑定提交记录当时来源，不是此后所有任务永远使用的起点。

开发分支没有 develop／main 等默认值，也不从 tracking upstream 推断发布目标。涉及远端时，需要明确实际远端与引用；保存目标字符串不等于已验证外部发布环境。

当前目录切换、Main 临时换分支或 manifest 出现另一目标，都不会自动改变绑定。集成前 Main 要回到正确目标，保留当前本地工作；集成器核对实际分支，不自动 checkout、stash 或替用户重命名。

### 2.3 一份交付需要多种提交坐标

| 坐标 | 它说明什么 | 不应与什么混同 |
| --- | --- | --- |
| `bound_commit` | REQ 绑定时的开发来源 | 每项任务的实际起点 |
| S6 原 TR-003 起点 | 本代实现开始前的共同 Main 基线 | 当前 HEAD、最早单任务基线 |
| 任务准备／实际创建基线 | 本次材料与本人工作区的来源 | 本人最终成果提交 |
| `source_head` | 被接收和集成的本人实际成果 | Main 被测树 |
| `target_head` 与 `merge_commit` | 合并前目标及实际集成结果 | 单纯平台返回或完成文字 |
| `tested_head` | Main 必要检查实际覆盖的提交 | 任何后来 HEAD 或 Worker 自检树 |

保留这些区别，是为了回答“测过什么、合入什么、还能否复用”，而不是要求作者手抄哈希。系统从真实 Git 和原记录维护坐标；作者解释实际工作及偏差。

## 3. 原生创建怎样取得正确输入

### 3.1 平台创建，工程固定材料

新 S6／S9 写入责任由 Main 携带准确 brief 原生派发，Claude Code 创建本人 worktree。当前仓库设置 `worktree.baseRef: head`；Main 仍须确认自己在绑定开发分支，必要输入已经提交，实际 HEAD 包含本次任务所需材料。

设置 head 不会携带 Main 未提交或仅暂存的内容，也不能纠正 Main 站在错误 checkout／分支上的问题。嵌套工作区的 HEAD 可能与绑定 Main 不同，因此“来自 HEAD”必须结合实际目录与工程起点解释。

S6 准备固定实际 Main 源提交并发布 brief，返回后关联实际 worktree 和分支。S9 还处理准备后依赖集成推进 Main 的情况：首次返回观察实际共同 Git 基线，确认它包含准备起点，再保留该来源供重试与集成使用。

新 S9 native Builder 的 D 目标合同使用默认平台 worktree 与真实普通原稿：准确 `.loop-submissions/native-s9/<原Plan packet>/plan.json`、同目录 `result.json`，系统在新 source pin／brief 固定版本及坐标，原 packet／digest 不变。两份原有专业产物各只填写一次，不新增留存文件。原始 Git 须看见准确 `??`；系统检查项目／info／global ignore 和 index／base／产品范围冲突，不修改用户 ignore，也不把全目录排出产品检查。任意规则都可能使候选被忽略，故普通路径并不保证保树。此处是先行 Blueprint 合同，当前实现与各项验收须分别记录。

产品消费者按原责任与原字节投影准确合格原稿；原 PlanReport 回执与 source pin 还确定本责任私人运行产物区，其未追踪检查输出和中间产物不算产品变化。平台仍看见原始 Git 状态，真实 diff、scope、Main 实际检查和 Final 保持原要求。原稿留到来源后继结束且原 verified／ACK／cleanup_pending 授权成立，不在 Plan／Candidate 接收或 Stop 内提前删除最后一份原稿；私人运行产物按第 9.2 节随原授权工作区回收。真实原稿未产生、被 ignore／删除、无有效 Stop 或后继执行者时，不能承诺保留及自动完成。树与 branch 保留具有资源成本，长期 blocked／取消现场仍归原责任，不以目录存在补造完成。

既有 C source-v1／Git ref 及冻结任务按原 pin、原字节和原 resolver 读取；新 D 不默认另建 C 包／ref，不改旧 packet、brief、source.json 或旧来源坐标。旧合法 nochange 已收候选仍可按原 Git 对象和目标历史恢复集成，不要求重新造稿；未收且无可靠字节的来源不能借 Main 替代或重建同名 worktree。活树存在时仍核来源 HEAD／branch、原稿与证据摘要，保树不证明不可变性或 actor 所有权。

真实 Claude Code 2.1.311 正常 acceptEdits 的隔离合成 changed／nochange 已走到原 Final、Impact 准备与清理，OS sandbox 实际未启用；完整安装、S7／Targeted、blocked／异常及并发仍待验证。该有限证据不能写成所有默认平台生命周期永久保留保证。现行设置不接管 WorktreeCreate／Remove；不能要求作者空提交、无关修改、seal 或额外动作换取保树。

### 3.2 正式输入与运行证据分别交接

代码、测试与按阶段要求提交的正式文档，从声明提交中读取。明确不入 Git 的运行证据可以按原依赖引用读取，或在支持的边界交付只读副本，说明来源与适用性；不能据此复制或软链接整个 `.claude`。

Main 派发前整理本次所需输入，不能自动提交用户无关改动来制造干净状态。缺上游时，反馈准确材料与准备责任；Worker 不应从 Main 未提交文件猜出一份未声明基线。

### 3.3 创建能力不是第二执行生命周期

原有 `runtime worktree-create`、冻结 workspace 及其准备／恢复能力各有既定用途；它们不是新原生 Worker 必须先走的路线。当前 settings 没有注册 WorktreeCreate 来替代平台创建，不能声称框架已经接管每次 worktree 的创建。

如果采用创建替代 Hook，必须真实创建并按平台协议返回路径，同时承担失败恢复；只输出提醒不能代替该事件的创建义务。现行新任务继续使用平台原生创建，工程记录只关联真实输入与返回来源。

## 4. 目录与文件边界怎样保持一致

### 4.1 临时工作区不混入项目扫描

项目级资产发现和文档扫描需要排除临时工作区，否则同一份文档或代码会被发现多次，生成错误分母、重复职责或无关差异。排除依据包括约定目录、Git 已登记路径及实际目录边界。

当前 `pathscope` 排除根内 `.worktrees/`、`.claude/worktrees/` 和 Git 登记的其他工作区，支持根内自定义目录及外部路径；符号链接与尚不存在的输出路径也解析其真实祖先。路径 `scratch/worker` 不应误伤 `scratch/worker-other`。

指标摘要原子发布使用保留的 `.claude/.loop-metrics-v1-<32 位小写十六进制>.tmp`，旧发布器遗留的 `.claude/.loop-metrics-<uint32 十进制>.tmp` 也按此暂存处理。这里只识别准确文件：父目录必须存在且不是符号链接，现存叶必须是普通文件；状态观察后仅叶消失可以是 rename。各消费者先保护 HEAD、暂存区、冻结对象或已有基线成员，只把符合其未跟踪条件的文件视为控制面暂存；不扩大到邻居、子目录或相似名称。这个内部命名不是所有权证明，同名未跟踪普通文件可能被当作暂存，识别不授权删除或回收文件，也不改变 Worker 工作区回收条件。

根本身是 linked worktree 时，扫描不能把自己排除。成果合入 Main 后，按 Main 产品路径参与检查，不因它最初来自临时目录而隐藏。

### 4.2 扫描、干净检查和产品差异各有目的

扫描排除不等于“整个目录内任何变化都无关”。集成器的干净检查使用 Git 状态、明确控制投影以及确切接收的私人文件；产品基线计算则读取实际提交差异及其产品路径规则。不同消费者不能仅因都谈文件，就共用一条任意忽略规则。

S9 Candidate 与 Inspect 使用同一原 PlanReport 回执、source pin 和登记执行，确定该责任唯一的 `.loop-submissions/native-s9/<原Plan packet>/` 私人运行产物区。除准确原稿外，区内未追踪内容按本责任私人运行产物处理；是否被引用、文件内容、名称或扩展名不另作 clean 前置，也不要求逐文件登记或冻结。这是已绑定责任的私人区，不是对整个 `.loop-submissions` 或其他 packet 的通配排除。准确原稿继续按原身份和已固定字节核对；专业结论实际引用的本地证据继续按原来源捕获和校验。tracked／staged 产品变化、实际提交差异、scope、HEAD／branch 和必要检查不因这个投影消失。

例如，S6 原生私人 Plan／Result 已在 Main 接收，仍存在时可按原专业内容比对；内容变化或多出其他文件不能被当作原交付。合法缓存缺失也不要求作者重造一份答案。旧安装覆盖层按其准确 pinned receipt 处理，不扩成任意 ignored 目录豁免。

当前 Git 产品面还有固定路径排除，不能由 `pathscope` 的存在推导全部消费者都已统一排除所有自定义目录。维护新的扫描或差异读取器时，需要核对它真正采用的来源与边界。

这些检查放在输入、交付、集成或扫描消费者中，不在新原生 Worker 普通工具事件恢复逐工具 ACL、首写门或通用激活。

## 5. Main 怎样接收一份实际贡献

### 5.1 先看原领域交付，再安排集成

S6 Stop 将本人专业 Plan／Result 交原完成消费者，S9 Stop 交原 PlanReport／RepairResult 消费者。接收只说明这些原事实已经成立；后续实际合并、Main 检查和确认仍需 Main 执行。

Main 应读取原责任与回执，核对真实成果来源、未完成事项、目标和必要检查。不能只凭 Worker 说“完成”、子分支存在或平台已退出就清理现场，也不能让本人等待 Main 后续集成才结束原生执行。

### 5.2 S6 与 S9 的范围消费不同

S6 的原 Assignment Result 与集成链依据正式 TASK、有效写范围和真实差异检查交付；计划路径只是预测，最终事实来自 Git。必要只读来源与冻结材料也按原契约核对。

S9 的 typed 专业交付按批准修复意图、原候选和领域消费者接收。当前通用 Integrator 的 domain delivery 分支不会再把 S6 通用路径 ACL 当成 S9 Main 集成硬门；真实差异和偏差仍须供原修复消费者及 Main 判断，必要锁与前置以该链实际配置为准。

不能因此统一宣称“所有阶段都逐路径硬拒”，也不能把没有这道通用门误解成允许修复任意扩张。专业范围约束与机器已实现的拒收范围需要分别说明。

### 5.3 无变化也是一种需要准确解释的交付

某些专业交付确实只做验证、无需产品差异。支持的 domain delivery 分支可保留实际 Main HEAD，执行必要 Main 检查而不制造空 merge 或假产品提交。

这不是所有路径都支持的通用豁免。canonical S6 首次集成仍要求真实 source 提交；修订和前驱已合并情形按原 Main 来源证明处理。S9 的完整变化集另有原完成条件，全批没有差异仍不能自动闭环。具体边界见 [S6](../l3-stages/L3-S6-build.md)与 [S9](../l3-stages/L3-S9-repair.md)。

S6 已接收的 checkout 缺失时，原诊断给出准确路径、分支和 accepted SourceHead，由 Main 恢复该 Git 来源再沿原 task-integrate 接续。原消费者已经保留 Plan／Result，不需要重造私人作者缓存，更不能补一个新提交来“恢复”已接受来源。精确恢复后仍按 S6 实际提交范围和必要检查判断；资源恢复不自动扩展为零提交范围支持，也不授权伪造产品变化。

## 6. 检查点怎样把集成拆成可恢复事务

### 6.1 集成阶段不是平台运行状态

```text
pending → ready → merged → verified → acknowledged → cleanup_pending → complete
```

这是原 Integrator 的持久化工序，不是另造一组工程责任状态。`blocked`／`preserved` 保留失败与已完成边界，`resume_state` 说明从哪一步继续；它们不等于成果失效或不可恢复终态。

| 工序 | 持久化的事实 | 仍未发生什么 |
| --- | --- | --- |
| ready | 当前交付坐标已准备进入合并 | 不代表 Git 已合并 |
| merged | 记录实际 merge 或受支持无变化交付的目标 | 不代表必要 Main 检查通过 |
| verified | 实际检查及被测树、原 Result 关联 | 不代表原领域已确认和资源已清理 |
| acknowledged | 原确认工序成立 | 不代表目录删除已完成 |
| cleanup_pending | 已准备回收确切已接收资源 | 允许清理失败后单独重试 |
| complete | 本项集成与原回收闭合 | 不等于整批通过或发布完成 |

检查点保存 source／target／merge／tested 坐标、Result 关联、检查引用、错误与恢复位置。采用明确 Assignment Record 的 S6，检查点还按不可变责任 revision 分槽，后继不能借前驱的一份 PASS 或 ACK。

### 6.2 Git 操作与检查点写入不能伪装成同一原子动作

Git 合并、文件系统操作、检查点 CAS 与 Runtime 确认分别持久化。进程可能在合并后、写入 merge receipt 前中断，也可能删除目录后尚未写 complete。

机制以原坐标与阶段重建已经发生的事实，而不是通过重做所有动作恢复。未知事实保留未知；一个“幂等键”或目录消失都不足以证明接收。Runtime 事务原子性见事务核心，不能把它扩大为 Git、测试与清理的跨系统原子事务。

## 7. Inspect 与实际合并怎样配合

### 7.1 Inspect 提供可复核的当前观察

Main 原入口核对责任、实际来源、原 Result、工作区是否干净、source／target、merge base、实际差异、必要锁定路径、冲突和本模式适用的检查。结果包含具体 blocker，帮助 Main 判断缺少什么。

Inspect 不靠自报路径推断差异，也不把清空写范围当成一份新授权。S6 canonical 读取原 Record／Result，验证其专业 closing 条件；S9 走其原候选验证器。各分支保证的范围取决于真正调用的消费者。

### 7.2 同一 Main checkout 串行变更

实际合并、Main 检查和清理使用原 Main 工作区锁串行化。锁归正在变更的 checkout，即便检查点另存，也不能按证据目录分出两个并发 Git 写者。目录别名解析到同一锁根，同一操作通过 context 复用锁。

当前锁竞争有短等待预算，忙时反馈重试同项原集成。只读锁观察报告 busy／available／unknown 等瞬时资源事实，不认证 PID、Assignment、完成或检查通过，也不创建另一集成进程。

Inspect 是快照；Integrator 在锁内重新核对当前来源、目标、差异及适用原关联。Main 后续提交、source 移动、实际差异改变或冻结路径命中，都需要按真实条件重新判断，不能仅凭先前 Ready 合并。

### 7.3 合并到绑定开发分支并保留真实历史

改变产品的正常集成使用 `git merge --no-ff`，保留本人提交和普通合并关系。目标必须是绑定的开发分支，Main 工作区保持干净；集成器不 squash、自动切分支、提交无关内容或代做 release。

冲突与未提交修改保留现场。解决问题需要 Main 或原专业作者作实际判断，不能用 reset／stash／强制覆盖来把检查结果凑成成功。检查点记录已经发生的 merge，重试不能另造第二次相同合并。

## 8. 必要检查怎样绑定真实被测树

### 8.1 本人自检、预合并检查与 Main 检查分别表达

Worker 按 TASK／修复责任完成真实自检，并如实给出命令与结果。Main 的集成检查验证合并后的实际树；二者不能互相代替。

原兼容 manifest 未声明检查模式时，保留预合并 Worker 树与合并后 Main 树的两处检查；显式 `post_merge` 可省去该预合并命令重复，但仍保留本人的专业自检与必要静态检查，且必须有实际理由和恢复安排。

S6 canonical Record 的 `required_checks` 当前由 Main post-merge 承担，原生准备也明确生成 `post_merge` 配置；它不把 Main 命令复制成一份 Worker runner 合同。Worker 仍按真实 TASK 自检，Result 如实引用。这与兼容 manifest 的默认双位置行为需要分开解释。

### 8.2 命令从原责任或真实 Main 选择取得

S6 Main 检查消费原 Record 及其 Main CheckProfiles。S9 Main 按修复目标与实际变化选择必要检查，可在原 `workspace integrate` 使用重复 `--check-command` 参数；专业前检是已经发生的事实，不自动冻结未来 Main 命令。

当前 S9 原集成入口没有必要 Main 命令就保持待办，不记录最终 PASS。底层通用 Integrator 本身允许空检查集合，不能仅凭 `verified` 字符串断定所有入口都执行过非空真实测试；要读本领域配置与原检查回执。

检查执行能力、环境、日志、复用和超时的详细机制见[执行环境与检查](L4-workspace-execution-profiles.md)。本篇关注这些检查实际覆盖哪个 Main 树及如何被确认。

### 8.3 检查前后核对实际树与 Result

Main 记录实际命令、退出结果、日志引用及 `tested_head`。检查期间 HEAD、分支或工作区发生变化，即使命令返回 0，也不能认证原候选；必要专业 Result 在检查中被修改也必须拒绝复用原结论。

S6 canonical Main facts 将责任、原 Result、Git 差异、检查及被测 HEAD 关联。后续读取这些事实时，继续验证其原适用性，而不是把一条文字 PASS 看作永久凭据。

Main 检查不能覆盖全部专业语义：真实覆盖、独立验证、风险判断与下一完整验证轮仍需相应角色履行。

## 9. 确认、依赖释放与清理怎样衔接

### 9.1 确认只能消费已验证贡献

S6 Main 确认消费对应 Record／Result 与 verified 检查点；同 owner 承担多项原责任时，原确认逻辑还会检查其余交付，不能确认第一份就关闭全部本人责任。

S9 typed 候选经 Main 合并与必要检查后，由原领域接收最终 Result；这项正式接收就是领域完成确认。原入口核对已接收回执及未改变的准确 Main 检查事实，再沿原检查点 CAS 将 `verified` 接续为 `acknowledged`，继续回收。Runtime 接收与检查点确认分别持久化，中间中断时从原回执恢复，不重交 Worker 答案，也不把两次写入假称跨系统原子事务。它不同于通用 Worker 激活或额外的人类批准。原完成事实和工作状态由各自消费者投影。

依赖释放消费工程 done 与 verified integration；只有提交、接收或单独 Main 合并都不够。清理积压不产生新 Worker 工具门，实际候选与资源顺序仍由 Main 安排。

### 9.2 清理只处理确切已接收资源

清理前核对原 checkpoint、source HEAD、目标仍包含原合入关系或无差异来源，以及本人目录是否有新变化。未能归属该责任的未跟踪文件、已改私人卷、新提交或 Git worktree 锁应保留现场；不能强删未接收成果。本责任私人运行产物在产品 clean 视图中的分类不是回收授权。

原 verified／ACK／cleanup_pending 对该责任工作区的适用授权成立后，由原 PlanReport 回执与 source pin 确定的本人私人运行产物区可以随该工作区回收，不要求作者先引用、提交或手删其中的检查输出，也不要求为每个中间文件另建摘要或清单。该区以外的其他责任和未知邻项不因此获得回收资格。本人原 Plan／Result 仍按准确原字节及来源合同保护，不能因 Plan／Candidate 接收或 Stop 提前删除；已引用证据按原接收和后继合同保存与核验，不能因所在区域被回收而丢失必要事实。

对于确切 receipt 支持的旧安装覆盖层、系统 typed 文件和原已接收私人文件，继续按各自原合同区分并核对准确路径、字节与适用后继。删除本人原稿前仍需核对原授权和原稿／证据，辨认不属于已授权私人运行产物区的 ignored 内容；未知 ignored、已改写原稿、未收第二责任或未知邻居保树及原稿，维持具名 `cleanup_pending`。不能借日志字段、普通目录名称或 clean 投影猜出其他文件的清理资格，也不跟随链接回收区外内容。

原 source HEAD、目标包含原合入关系或无差异来源、原稿与引用证据的适用来源和必要后继仍须成立；中断后依原 checkpoint 续回收，不另建 GC／accepted 账本、不改 index、不全局 prune，也不以 force remove、git clean 或递归删除清理未知内容。最后 Final 成立并进入影响工序后，原出口须先准备 ChangeImpact，再独立 ACK／cleanup；失败反馈同时交后继材料与资源 pending，不把 Final 说成 Worker 尚未交付。

框架 Main 锁只约束协作者，清理与外部并发写入不具跨进程原子性。已观察到原稿、引用证据或其他责任发生冲突时保留现场并给出具体原因；不把证明任意外部进程均已静止变成常规回收前置。并发新增／替换、特殊文件和大目录扫描成本须实测列明，不能从小 fixture 推成任意并发安全。临时 branch 处置与容量另按原授权报告，树已删不表示 branch 已回收。

### 9.3 清理中断不改变原检查含义

清理失败保留已验证事实及恢复阶段，解决具体文件或资源问题后只继续回收，不重复合并，不把新的清理时间当作新的验证时间。

目录已被移除时，依据原 `cleanup_pending` 及准确 Git 登记完成剩余清理。目录不存在本身不授予接受；没有归属或缺失检查点的孤立资源可报告，不能自动删除。

## 10. 失败与响应丢失怎样继续

| 情形 | 应保留什么 | 原恢复方向 |
| --- | --- | --- |
| Main 或 Worker 有未提交修改、source 新提交 | 实际目录、差异和原交付 | 查明内容归属，按原修订／重新检查处理 |
| 合并冲突 | 原 source／target、冲突现场、检查点 | Main 处理真实冲突，不自动 reset／stash |
| 合并后检查失败 | 已合入树、命令／日志、失败工序 | 环境问题重验原候选，代码问题返工产生真实新交付 |
| 已接受而响应丢失 | 原接收、检查点与 Git 坐标 | 查原事实，不重复交付和合并 |
| 删除目录后 complete 未写 | 原 cleanup_pending、来源与目标关系 | 确认确切回收后补完工序 |
| 目标历史被重写 | 原 merge、检查与当前不可达事实 | 保留并撤回后继资格，不沿用旧 PASS |
| 控制根／执行根移动或来源丢失 | 原绑定、登记和已有持久产物 | 使用具名迁移／恢复能力，不从同名目录猜回 |

`preserved` 保留的是精确失败边界。检查点有 `resume_state` 时按原已完成工序继续；需要修代码时，S6 走原 Assignment revision，S9 走其原未验证候选返工／领域恢复。不能直接把旧失败改成 PASS，也不能通过换 worktree 丢掉原失败。

## 11. 两类恢复边界必须明确区分

### 11.1 已有 merge receipt 与丢失 merge receipt

已有 receipt 时，原恢复会检查 merge 是否仍可从当前目标到达、其父提交是否符合原 source，并还原真正合并前的范围。正常后续提交不自动否定这项历史集成；目标重写导致 merge 不可达则不能再用。

若 Git 合并已经发生，但 checkpoint 仍为 `ready` 且没有 merge receipt，系统只读辨认原执行意图：沿当前绑定目标的第一父历史，最多检查 256 个提交，找到恰好两个父提交、第一父等于原 `target_head`、第二父等于原 `source_head` 的准确合并。第一父表示 Main 的直接演进链；仅从另一条侧枝可达、父序反转、不同 source／target 或八爪鱼合并，均不属于这份原意图。当前目标分支和原 source 也须可准确对应，不能由提交消息、时间或 reflog 猜测。

后继普通提交不否定这项已发生的合并，但辨认不授予验证通过。Integrator 先只读取得原合并关系，让规范 S6 的 Record／Result 与源范围校验仍使用原合并前的准确来源，而不是把已经集成导致的 merge base 变化误当成新责任；随后才用原 CAS 将 `ready` 接续为 `merged`，在当前实际 Main HEAD 执行原必要检查。若 ready 中已有准确 ResultBinding、完成字段尚未落盘，则从原已验证来源恢复相同字段，仍保原文件与摘要检查。原生 S9 的候选、Final、确认与清理也沿同一原链继续。

原 Integrator 与 Main 控制入口复用这一只读判定，不各写历史搜索或新建合并账本。新的 Inspect 若仅因原 source 已集成而报告没有新提交，可以由准确原合并解释；目录变脏、来源变化或其他真实 blocker 仍阻断。原责任、代次、Record、差异、检查及确认边界都不能靠“找到了一个 merge”绕过。

搜索在原 target 之前停止，并以有限额外一条记录辨认截断。范围内没有准确匹配、观察不唯一、Git 对象或环境不可用、超过 256 个第一父提交时，保留原 checkpoint 和 Worker，明确原 target／source 及当前缺少的证明，交 Main 核对原意图与目标历史。不能扩大为无限扫描，也不能为了继续运行重做合并、篡改检查点或让作者再交同一份成果。

### 11.2 确认前与确认后，目标前进的含义不同

S6 原 Integrator 在首次确认前发现目标已离开 `tested_head`，会要求检查当前目标，保留原 merge 而不重复合并。已经确认后的正常后续提交与纯清理重试，则保留原历史验证，不无限重验每位已完成作者。

S9 要以原领域接收为界。Final 尚未接收时，目标前进仍需原 Integrator 在当前目标实际重验；旧 HEAD 的检查保留为历史，Final 只读取当前受检提交的有效检查。Final 已正式接收时，即使检查点确认尚未落盘，原 `workspace integrate` 也先核回执和原 Main 证明，补完原确认后按历史贡献继续安全清理。此时不改原验证时间、受检提交或检查集合，不另建新的验证尝试记录，也不把后续目标变化拖回已经完成的作者。

已收贡献的确认不会覆盖后续变化的验证责任：新贡献的 Main 检查和完整 S7 必须针对其实际对象执行。原 merge 不能从目标到达、来源变化、检查记录损坏或新文件阻碍清理时，仍按原检查点保留现场；没有有效 Final 的候选不能用这个恢复分支跳过当前检查。

这些差异来自原接受和不可变结果的边界。设计需要准确区分历史贡献与当前验证，不应把同一恢复口号套到所有消费者。

## 12. 如何交给下一完整验证轮

S6 的共同产品起点来自原 TR-003，而非当前 HEAD 或各任务最早提交。S7 应看到这个起点到最终 Main 的完整真实变化，包括删除、失败尝试残留、后继修订及可证明的历史贡献。

当前 `buildsurface` 从原 Runtime、journal、准确 TASK 锁集、Assignment 链、原 Result 和 Main receipt 重建产品面，核对绑定开发分支及最终 Main。只拼当前完成者的路径或当前成功 diff，可能漏掉实际仍留在产品中的变化。

### 12.1 同一变化面在计划、冻结与覆盖中的表达

产品快照、`frozen_subjects` 和 `coverage_inventory` 是同一来源的不同投影，不能分别定义删除是否属于本轮。原产品快照负责状态和 Git 证明；冻结集合给出本轮准确审查对象；覆盖库存要求 Main 为每个实际变化形成可判定命题。它们由系统产生，Main 只补专业命题、适用性和分工。

| 原对象 | 冻结集合中的表达 | 实际核验与后继 |
| --- | --- | --- |
| 原锁定 TASK | `kind=task`，原路径和锁定字节 SHA | 与原 TR-003 锁集一致；同路径产品变化不能覆盖 TASK 身份 |
| added/modified 产品路径 | `kind=changed_surface`，最终 Main 的该路径字节 SHA | 原 Git 来源和适用的工作区漂移检查；覆盖库存仍含实际路径 |
| deleted 产品路径 | `kind=deleted`，该路径及原产品快照保存的删除前态 SHA | 原快照的 `preimage_commit`、`deleted_by_commit`、blob 和最终不存在证明；当前工作区不得悄悄重现该产品路径；覆盖库存仍含实际路径 |

因此删除进入 `frozen_subjects`，但其 SHA 表示原删除前态，不能要求“当前文件 SHA”。这里固定的是准确审查对象，不宣称删除后仍有当前文件。原快照已有足够的删除来源字段，不新建删除证明文件、第二种路径库存或作者 SHA 字段。正式 TASK 既要求仍为原锁定来源又被声明删除，属于来源冲突，不能通过跳过删除或保留一半记录处理。

同一投影规则贯穿首次 Plan 草稿、原 Plan 登记/精确集合检查、覆盖检查、结果源摘要、CleanRound、S9 后 fresh S7 以及 S10 原登记基线读取。fresh S7 重新生成实际对象及本轮摘要；S10 读取该轮已登记证明，不从另一份当前目录清单缩小分母。生成与校验必须共用原投影语义，不在两个门各加特例。

Main 的近端 guide 须列明机器不可改部分（产品快照、冻结对象、覆盖库存和派生摘要）及专业可填部分（Claims、实际分工、适用性、环境和资产安排）。缺 Claim 时指向命题及 `source_refs`；机器投影不一致时由原准备/消费者纠正，不指示作者手工补 `frozen_subjects` 或重算摘要。非浏览器行为依真实产品适用性决定，不能从 UI 标志推导 N/A。

此前 S7 冻结投影排除 deleted，而覆盖门和原登记基线 Reader 要求完整路径，曾造成同源分母不一致。生成与校验应统一消费本节的投影规则；“跳过删除”的正向测试不能作为架构验收证据。

### 12.2 保留历史来源与正式规格边界

前驱证据失效不自动证明其 Git 内容已撤销；反过来，历史内容存在也不使失效 PASS 重新当前有效。读取原历史贡献只保留可证明的来源与范围，不恢复其质量资格。共享基线与独立验证方法详见 [S7](../l3-stages/L3-S7-verification-round.md)。

集成检查可能在取得 source、target 或 merge base 之前失败。原 Writer 按当时已知的坐标保存 `preserved`，不补造尚未取得的 Git 事实。Reader 通过准确 Record、代际、任务、目标和失败状态识别这种未集成尝试；没有 merge、检查或完成证明的记录只表示“尚未验证”，不能当作成功贡献。当前责任仍被它阻断；只有原 Assignment 后继链已合法成立，且当前修订有真实 Main 验证时，历史读取才能跳过前驱的这份未验证贡献。旧失败不改写，已合并内容、失败检查、成功历史贡献和产品起点到最终 Main 的实际差异仍分别核验。

当前正式 TASK 源仍要求首次 TR-003 的精确锁集，Assignment revision 只服务保留该 TASK 的执行纠偏。正式规格问题归 Main 的原规划／审查责任，同代 TASK 替换尚未接通完整来源与后继执行，不由本机制自动重锁，也不要求作者构造额外证明或刷新 hash 绕过。

原生准备重放先比对当前准确 TASK 锁定身份与 Main 的该任务源字节，再返回旧 brief；未改变 TASK 的 Main 后续提交不会重置原准备基线。源已经改变时应明确交 Main 处置，不能以成功返回原 brief 冒充接入新规格。

## 13. 观察与提醒如何帮助 Main

| 观察位置 | 要回答的问题 | 行动归属 |
| --- | --- | --- |
| 当前绑定与实际分支 | 是否站在正确控制根与开发目标 | Main 保留现场后显式纠正 |
| 原领域回执与待集成投影 | 哪份真实交付已接收、哪项仍缺 | Main 选择原集成入口 |
| 检查点与实际 Git | 合并、验证、确认和清理分别到了哪里 | 原 Integrator／领域消费者恢复 |
| Main 检查记录 | 实际测过什么、在哪个提交、为何失败 | Main 或原专业返工责任 |
| 原工作区锁及资源积压 | 当前是否忙、有哪些已验证待回收资源 | Main 安排顺序，诊断只读 |
| 原返回与 Hook 诊断 | 本人缺项还是系统准备／后继问题 | 明确 owner，避免重复催已交付 Worker |

提醒应给准确 REQ、责任、来源、目标、缺项和下一动作。相同事实可去重，交付／合并／积压变化再反馈；未回收不是继续运行 Worker 的理由，后台启动也不是待集成完成事实。

新原生 Stop 负责接收已有产物和短事务，不运行长合并、检查或清理。Main 使用实际可见返回、原回执与当前指引接续，不依赖再触发一次 Stop。当前 native 成功诊断并不保证每条父会话消息已到达，不能仅因 Hook 输出过一句提醒就宣布反馈闭环成立。

## 14. 当前实现与验证边界

| 机制责任 | 当前落点 |
| --- | --- |
| 原绑定与同仓库 checkout 核对 | [workspace/binding.go](../../internal/workspace/binding.go)、[workspace.go](../../internal/workspace/workspace.go) |
| 原生来源与本人产物关联 | [native_s6.go](../../internal/cli/native_s6.go)、[native_s9.go](../../internal/cli/native_s9.go)、[native_delivery.go](../../internal/integration/native_delivery.go) |
| 真实 Inspect、集成、检查点与锁 | [inspect.go](../../internal/integration/inspect.go)、[integrate.go](../../internal/integration/integrate.go)、[checkpoint.go](../../internal/integration/checkpoint.go)、[workspace_lock.go](../../internal/integration/workspace_lock.go) |
| 合并／清理恢复与内容投影 | [cleanup_recovery.go](../../internal/integration/cleanup_recovery.go)、[git_worktree.go](../../internal/integration/git_worktree.go) |
| Main 确认和 S9 领域集成入口 | [integration_ack.go](../../internal/cli/integration_ack.go)、[workspace_delivery.go](../../internal/cli/workspace_delivery.go) |
| 临时目录扫描边界 | [pathscope/worktrees.go](../../internal/pathscope/worktrees.go) |
| 当前完整产品面与历史贡献 | [buildsurface](../../internal/buildsurface)、[mainreceipt](../../internal/mainreceipt) |
| 原工作区观察和使用说明 | [observation.go](../../internal/integration/observation.go)、[workspace-integration](../../docs/workspace-integration.md) |

部分 Git 规则和公共说明仍把手工 worktree-create、旧激活或全双位置检查写成普遍路径；应与当前原生与各领域合同核对，不能将其恢复为新任务前置。已有迁移、Bootstrap、launcher 和受控检查能力仅按它们的实际适用范围解释，详见执行环境篇。

验证需要分别覆盖正确绑定、linked Main、源提交、真实差异、目录排除、并发集成、检查前后漂移、Result 变化、响应丢失、确认与清理分离，以及目标重写和源丢失。检查点单元测试证明原事务行为；隔离 Git 测试证明实际合并与恢复；真实 Claude Code 才能证明平台创建、返回与 Main 接续。模拟专业答案和历史报告都不能代替当前全场景产品验收。
