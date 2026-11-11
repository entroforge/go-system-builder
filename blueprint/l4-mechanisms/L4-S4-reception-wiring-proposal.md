# S4 原接收、系统报告与 S5 出口实施设计

状态：**实施中，整根占位与自然大报告验收未通过**。2026-11-10 的专项复测中，普通模式自然生成 4,507,031 字节报告并通过；race 模式该场景在 S4 接收后处于 pending。整个 evidence 根为文件时 S4 已 advanced 但未准备出两份 S5 原题；根为符链时，未知目标保留的复合断言失败。日志未记录底层具体控制错误或符链目标变化的明细，不能据此推断根因；证据见 `.local/framework-maintenance/fixture-replay-design-20261108/closure-20261109/s4-implementation-20261110/natural-acceptance-42748141944/`。这些结果与本设计的完整出口／保留承诺冲突，不能宣称该方案已验收；应先按原阶段链调查并重新确定接线，再继续相关修复。

实施依据为 [工作流收敛](L4-workflow-convergence.md) 的原责任、原消费者、Writer、事实保留与后继边界，及 `.local/framework-maintenance/fixture-replay-design-20261108/closure-20261109/NEW-FAILURES-INVESTIGATION.md`、`NEW-FINDINGS.md` 和其中 `s4-draft-20261110/incomplete-s4.patch`。行号为设计审查快照定位，实施时核对符号。

后续证据已确认 S5 分工登记、本人报告接收及 TR-003 仍有同源断点；完整阶段、共享 activation、旧在途责任与性能取舍以 [S4→S5 完整交接设计](L4-S4-S5-handoff-convergence-proposal.md) 为准。本文保留 S4 原接收与报告读取的细化合同，不单独宣称整个交接已跑通。

## 1. 问题、专业责任和选定方案

Main 从原设计、原契约和锁定 REQ 拆分 TASK、依赖、完成条件及派发索引，在本人已登记 S4 原答卷表达专业结论。原消费者执行既有 TASK 检查和批次解析，原 Writer 保存接收，TR-002 登记任务后交 S5 独立审查。报告生成、接收、转换和 S5 准备是不同事实。

当前 `internal/submission/task_planning.go:163–172` 先在 Writer 外发布报告，之后才接收。取消可留下未登记报告；重试生成新时间戳，再以新报告长度读取旧文件，产生 `packet artifact exceeds ... bytes`。不能通过增加上限、固定时间戳、删除报告或要求作者重存掩盖生命周期分裂。

选定方案：S4 准备好的报告经 `SubmissionRequest.EvidenceJSON` 交原 Evidence Writer；Writer 在原锁内判断接收并排他发布，沿现有根级备用路径策略，登记实际胜出路径。**已登记的 S4 系统报告从控制根磁盘读取，业务材料继续沿原 fileview。** 不扩大整个 `.claude` 的来源声明，不修改 Git 来源规则。

不采用“所有备用报告限制在 `.claude/evidence/` 内”的方案。现有 `PublishS9Questionnaire` 在 `internal/workpacket/s9_questionnaire_publication.go:30–36` 已能选择 `.claude/evidence-alt-N/...`；原根被未知文件、目录内符链等占用时，根内方案会放弃此能力，并新增另一套候选循环和根可用性判断。S2/S3 已有系统报告直接磁盘读取先例，三处 S4 报告读取的局部修正成本更低，也保留原 Writer 能力。整个 `.claude` 或控制根不可用仍如实失败，不承诺绕过原控制根。

## 2. 原接线、真实来源和修改归属

| 环节 | 现行责任文件与位置 | 实施合同 |
| --- | --- | --- |
| 原发行与实际路径 | `submission/task_planning.go:18–70`，`:73–96` | 保留原 packet 和已登记 view/answer；发行并发读取登记赢家，未登记候选不冒充原题 |
| 答卷接收与专业检查 | 同文件 `:99–159`、`:259–271` | 保留原答卷结构、当前责任、来源判断、`TasksCheckWithFiles`、实际 TASK/index 批次；不新增 Authorize 或来源证明 |
| 报告构造 | `workpacket/task_planning.go:266–315` | 原 REQ、sources、contracts、实际 artifacts 和本人专业答案；created_at 不作答卷身份 |
| 报告交 Writer | `submission/task_planning.go:163–172` | 删除 Writer 外 PublishBounded；处理 JSON 序列化错误，将原报告字节交 EvidenceJSON |
| 锁内接收与发布 | `runtime/evidence.go:84`、`:188–275` | planning_task 纳入原 planning 发布分支；先判断原接收，再沿现有排他发布取得实际 path，再写 evidence/submission |
| 锁内同答/异答 | `runtime/task_planning_submission.go:41–55` | 从已登记赢家读取原答案，不要求 row.path 等于当前候选路径，不比较候选时间戳 |
| 错误后重放 | `runtime/evidence.go:293–305` | S4 与 S3 一样，确认实际 submission 已成立才解析接收重放；无已收事实不得在错误后运行首收逻辑 |
| 控制与持久化 | `runtime/store.go:977–994`、`:1204–1375` | 原锁、原 pending、原 state/journal 提交与恢复；不增加旁账或事务层 |
| 原后继 | `controller/planning_followup.go:63–94`、`:216–263` | 原 followup 与当前入口关联；已收只续原 TR-002，不重收本人卷 |
| TR-002 登记 | `transition/actions.go:570–652`、`:737–791` | 从已收答案和实际批次登记索引、complete TASK；cancelled 仍属原检查批次但不成为执行 TASK |
| S5 正式输入 | `runtime/s5_formal_inputs.go:193–252` | 最新实际 accepted/advanced S4 tuple 与原 REQ/代际；不能拼默认报告路径 |
| S5 责任准备 | `cli/s5_planning_handoff.go:19–71` | 原 S4 接收与 TR-002 成立后准备原 S5 责任；失败保留已有事实及已发行材料，沿原 issue 续剩余部分 |

以下三处当前通过 fileview 读取系统报告，须仅把该次报告读取改成控制根实际 path 读取；其业务材料读取不改：

1. `internal/qualitygate/task_planning_packet_subjects.go:64`，控制根为 `input.Root`。
2. `internal/transition/actions.go:629`，控制根为 `actionRoot(state, ctx)`。
3. `internal/runtime/s5_formal_inputs.go:231`，控制根为原 `root`。

**TR-002 通用证据投影也属于同一出口。** 联合验证发现，`qualitygate/evaluator.go` 在 S4 专用事实投影后，仍会通过 `unauthorizedProducerConflicts` 和 `qualifiedEvidence` 使用产品 fileview 读取同一报告；只改上面第一处仍会报 `evidence:<ID>:unreadable`。S4 原消费者已定位的当前接收和 evidence row 是这次投影的唯一路径：仅将该**实际登记报告路径**的读取接入相同控制根报告操作，通用投影其余 producer、conclusion、subjects 等原有判断保持；其他证据和正式材料继续使用原 fileview。不能全局改变证据来源、依据文件名推断当前报告，或借此增加新关联条件。

`qualifiedRequestedEvents` 只处理带 `requested_event` 的报告；现行 S4 原报告不生成该字段。`evidenceEnvelopesByID` 用于独立文档审查等其他 gate，不是本次 TR-002 的 S4 读取入口。本轮不为未发生的用途改动它们。反例测试须确认是选定 Git TASK/契约的实际问题，而不是报告不可读导致的任意失败；成功路径须先真实到达对应检查。

已有直接磁盘读取先例是 `qualitygate/contract_packet_subjects.go:171`、`transition/actions.go:504`、`runtime/task_planning_artifacts.go:345`。这些是 S3 已登记报告的读取，不是允许从 checkout 代读 REQ、TASK、契约或其他原 Git 来源。S4 自身已有直接磁盘读点包括 `submission/design.go:469–476` 的重放读取、`controller/planning_followup.go:251` 的原后继读取、`submission/contracts.go:479–515` 的交付指引读取；锁内重放新增读取也须进入同一 S4 报告读取合同。

## 3. S4 已登记报告读取合同：不新增字节或路径门禁

当前上限并不一致：controller 固定 1 MiB（`planning_followup.go:251`），重放固定 4 MiB（`submission/design.go:473`），交付指引使用 `TaskPlanningEvidenceReadLimit(p)`（`submission/contracts.go:503`），该动态值是序列化 Inputs 长度加 1 MiB（`workpacket/planning_limits.go:25`）；三处 fileview 报告读取没有显式大小截断。简单统一成动态 limit 或 `max(4 MiB, dynamic)` 仍会收窄原无显式上限的消费者，不能宣称保持能力。

本轮统一：**S4 已登记报告从控制根读取完整当前字节，不新增 byte-size 接收门或读取截断。** 在既有低层文件读取职责中提供一个共用的 S4 报告读取操作，供三处报告消费、controller、S4 重放、S4 指引和锁内重放使用。该操作不判断 receipt、来源或专业质量；调用方沿原关联取得实际 row.path。S3 共用的 `readPlanningEvidence` 不能为此全局放宽其他阶段，应在已确定 S4 packet 的调用处采用 S4 读取语义。原作者答卷预算不变；发布 helper 的 len(data) 只核本次待发布数据，不作读取旧报告的预算。

路径安全须作真实差异分析，不能直接把 `workpacket.ReadFile` 去掉 limit 就声称等价：其 `OpenParent`（`files.go:19–66`）拒绝路径中任意符链，末端读取也拒绝非 regular；现行 `fileview.View.diskPath`（`view.go:168–177`）经 EvalSymlinks 后允许控制根内且不在排除工作树中的目标，`ReadFile:179–193` 再读取。裸 `fileview.Disk.ReadFile`（`contracts.go:18–23`）没有同等边界检查，不能直接代替生产 View。

选定实现沿原 Evidence Writer 的控制根路径语义：将 `safeEvidencePath(root, path)` 已有的归一、符链解析和控制根范围判断提取为内部共用计算，返回规范相对路径及解析后的绝对路径；原 `safeEvidencePath` 仍只返回原来的相对路径，供其他 Writer 保持原行为。新增 S4 报告读取操作使用同一次计算所得的解析后绝对路径执行 `os.ReadFile`，不重新解析原符链。原调用方仍从 Runtime evidence row 取得实际 path、负责原责任关联与专业判断；读取操作不扫描文件、不另认领未知内容、不访问 Git、不设大小上限或缓存，也不用于 TASK、契约、REQ 或来源闭包。已有调用上下文取消时沿原调用路径传递或检查，不为此新建独立后台等待。

不选每次新建纯 disk `fileview.NewContext`：虽然它不要求 Git ref，实际 `declaredView` 会调用 `pathscope.New`，后者每次用独立的 Background／2 秒预算枚举 Git worktree；多个报告消费者会重复产生额外子进程及等待。只从已登记位置读取系统报告，无需重复枚举开发工作区。也不取现有读法中最严规则的并集：`safeEvidencePath` 接受解析后仍在控制根内的符链，拒绝绝对／越界路径及解析后离开控制根的目标，和现有 Evidence Writer 的路径规则一致。

此选择有明确的保证变化，不能描述成三种旧读法完全等价：三处旧 fileview 报告读取原先会排除控制根内登记的子 worktree，现在对原 evidence row 已登记的系统报告不再额外做该排除；原 `workpacket.ReadFile` 的消费者不再对报告要求逐级无符链、目录 pin 和打开前后 inode 一致。两者均只影响系统报告，不影响正式材料的来源和 worktree 排除。报告登记时原 Writer 仍按现有排他发布规则处理符链及未知占位，不因读法变化覆盖或认领文件。旧已收报告沿原 row 的实际可读路径接续，不迁移、不要求重存。

读取解析后绝对路径保留原 fileview 在原符链被直接切换时的行为；解析后的最终目标或其祖先仍可能在读取前被替换，读取可能成功但取得变化后的内容甚至根外目标。因此不声称获得 OpenParent 的 pin、检测所有路径替换或跨文件原子性。读取目标若实际成为 FIFO 等特殊文件，既有无截断读取可能阻塞且 `os.ReadFile` 无法在读取中被 context 中断；大文件也会消耗实际内存。记录这一取舍，不将其掩盖为保证能自动报错，也不在本轮新增文件类型、路径稳定性或大小资格。真实读取错误须如实反馈并保留事实。验收分别覆盖根内及越界符链、控制根内子 worktree 的已登记报告，以及正式材料仍受原 fileview 限制；路径替换测试记录实际结果，不声称所有替换均被检测。

实际报告缺失、不可读、路径越界、解析损坏或目标变化须准确反馈；原接收事实不撤销，不猜另一个文件，不要求作者清理或补交。读取完整文件有实际内存/运行成本，需报告，但本轮不得用新的字节 gate 替代已承诺的读取；如将来改变资源合同，应独立设计而不是暗中夹入修复。

## 4. 并发、赢家与事实保留

Writer 在原锁内先处理已有接收。首收才发布报告；成功返回的实际路径进入 evidence row，submission/followup 通过原 evidence ID 关联，journal 保存同次接收。报告存在本身不证明已收、所有权或阶段完成。

同责任同答并发：只接收一次，后到者读取已登记赢家的专业答案；允许 TASK paths 次序规范化，不以时间戳、JSON 排版或候选位置制造冲突。合法异答必须按原专业比较返回 ErrSubmissionConflict，不覆盖赢家。赢家报告坏或不可读与真正异卷不同：先完整读取并解析、验证赢家原关联，损坏返回具体报告错误；只有赢家有效且双方专业答案确实不同才返回异卷冲突。不能把所有 ValidateTaskPlanningEnvelope 失败压为 conflict。

实施时先从赢家报告提取其原专业答案，用该答案执行现有 `ValidateTaskPlanningEnvelope`，确认报告自身可理解及原关联；再按该函数既有的 conclusion、index、TASK paths 语义与本次合法答案比较。这样复用现有判断即可区分报告错误和真实异答，不增加 subject 全文、时间戳、作者额外说明或历史字节相等检查。

锁内 `prepareTaskPlanningSubmission` 与 `SaveTaskPlanning` 的已收分支必须使用同一判断；当前 Save 把所有 envelope 校验错误统一映射为 conflict，不能只修锁内分支。报告本身缺失、损坏、关联不可理解时返回实际报告问题并保留原已收事实；报告有效而新答案不同才是 conflict，不能静默转成成功或要求作者重新证明历史来源。

`EvidenceJSON` 的 nil 表示原有外部报告入口；正常 S4 consumer 只在序列化成功后交付完整非 nil 报告。Writer 冻结请求时必须保留 nil 与非 nil 空字节的区别：如果调用方明确给了空报告，应由现有报告验证反馈不可理解，而不是由于复制将其转成 nil 后意外读取首选路径的未知占位。此区分不新增作者字段、报告格式或资格。

首选报告及祖先被未知同内容、异内容、不可解析文件、目录或符链占用时，沿现有 helper 选择 sibling 根，不读占位正文推导身份。整个 evidence 根占用同样保留原状并使用 fallback。所有未知候选、失败者产物和未登记残留均保留，不增加清理授权。

历史已收事实沿原责任、代际和 superseded/replaced 规则保存。旧代/旧入口不重开 TR-002、不占当前恢复预算；真实新保存尝试被拒绝时，现有 `task_planning.go:218–225` 仍返回原 Accepted 事实和本次 rejected，不把尝试失败改写成从未接收。

## 5. 中断矩阵与原恢复

| 窗口 | 持久事实及恢复 |
| --- | --- |
| 首收前取消、专业检查失败、锁等待失败 | 不发布新报告，不接收；原 Runtime/journal 与未知文件不变 |
| 锁内发布后、pending 前取消/候选校验失败 | 可留未登记报告；Runtime pair 不变，无 pending；重入原消费者选择安全新路径，不认领旧残留 |
| pending 已保存、state 未替换 | 原 pending/Store 恢复 candidate 和事件，不重新生产接收 |
| state 已替换、journal 未追加 | 原恢复补全 pair，保留赢家 path 与 submission |
| journal 已追加、marker 未清 | 原恢复识别已成立事实并清原 marker，不追加第二接收 |
| 接收成立、TR-002 未完成 | 保留 Accepted 与原 pending followup，原 Recover 只续控制 |
| TR-002 成立、S5 准备中断 | 保留 S4、阶段和已发行 S5；原 issue 续未完成部分，不重收 S4 |
| 已收后私卷改动、损坏或删除 | 不撤销原事实；真实新尝试可拒绝，后继不依赖私卷 |
| 已登记赢家坏或不可读 | 准确报告系统材料错误，保留接收，不以候选或未知文件补证 |
| 整个控制根/.claude 不可用 | 准确存储/读取故障，保留可确认的最后事实，不向别的控制根迁移或伪造成功 |

复用 `submission/receipt.go:310–360` 的原 Recover：它直接继续 followup，不读取私人答卷；`cli/questionnaire_observer.go:228–266` 的成功 Post 已自动调用此入口，再续 S5 handoff。保留其当前责任筛选、历史跳过和有限本次恢复职责，不另建生命周期。

备份 patch 中“私人答卷 ReadFile 失败自动 continueTaskPlanningOutlet”的新增分支不实施；它不解决发布生命周期，且把私人读取错误与恢复混合。删除该草案部分，保留原 Save 的尝试判断及原 Recover。可以整合重复内部调用，但不得引入第二种恢复语义或要求用户增加动作。普通 Pre、成功 Post、具名原恢复沿现有合同继续；失败 Post 不冒充保存，没有真实后续事件不承诺无人触发完成。

## 6. 输入、指引、模板、Hook 与答卷边界

原 L3、TASK/index 模板、S4 原 questionnaire/schema、Main 本人答卷和已安装专业指引的职责无需改变：专业题目、必需结构及实际来源未变。Hook 负责按已知 Main 原责任观察原卷、调用同一消费者及反馈接收/后继；不新解析 Bash 命令猜保存位置，不要求补一次编辑、重存、Stop 或专用提交。`cli/stage_answer.go:460–471` 原接收后继续 S5；现有自动恢复与 S5 准备保留。

本轮只核对这些投影未残留相反承诺，不为系统报告位置或机器读法新增作者字段、模板段落、done/ready、注册动作、授权、hash/SHA 或工具许可。报告 actual path 是系统原 Writer 的保存责任，不交作者抄写。S5 两个独立责任与实际平台派发继续由原流程承担，系统准备不等于平台启动。

## 7. 验收及同源码 CI 闭环

先完成有明确反例的测试，再扩大到最终版本验证；以下均待执行，不是本文结果。

1. 原 `TestMainStageReceptionCanceledBeforePendingPreservesOriginalPair/S4` 保留 Runtime/journal 字节不变、无 pending、原卷重试成功。确定性构造合法长时间戳旧残留和短候选，记录字节长度、赢家路径、原残留；不靠循环碰概率。
2. 原接收入口同答/异答并发，候选时间戳和路径不同；只一条 evidence、submission、followup、接收 journal，后到同答使用赢家，异答拒绝，坏赢家与异答错误可区分。更新 `runtime/task_planning_submission_light_test.go` 的实际报告 fixture，保留不得重跑 Authorize/来源证明的断言。
3. 首选文件及相关祖先占位、整个 evidence 根被未知文件/符链占用、同内容未知报告、目录和不可解析占位全部原样保留；至少一个 evidence-alt-N 实际路径贯通原 declared fileview 环境、TR-002 门/实际 TASK 登记、S5 formal inputs 和两份独立原题准备。整个 .claude/控制根故障准确失败。
4. 覆盖报告大于旧 controller 1 MiB、旧 replay 4 MiB及动态 limit 的合法机器来源和 TASK 批次内容；所有 S4 报告消费者完整读取，不因新字节门失败。另可单独构造含未知扩展的已登记报告，验证读取与解析兼容，不能把扩展当作当前生成器自然产出的字段。专业答卷原预算不变。测试根内符链和控制根内子 worktree 的已登记报告、越界目标以及路径替换的真实错误；同时确认正式材料仍受原 fileview 与工作树排除约束，不能以原 OpenParent 与 fileview 等价的假设替代测试。
5. 覆盖表中发布、pending、state/journal 和 marker 窗口，原 Store/ApplyRecovery 及重复恢复不丢事实、不重复接收；已收后 TR-002/S5 失败、私卷删除/变更，原 Recover 和自然成功 Post 只续剩余部分。验证 cancelled TASK、旧代替换、历史报告保留及实际业务材料仍读原 fileview 的反例。
6. 共享 Evidence Writer 的 S2/S3 原接收、同/异答并发和实际路径回归，确认本轮没有改变它们的报告格式、专业检查或来源合同。原相关测试先普通后 race，外部 Hook 保留真实普通 Harness 与 watchdog；不把它说成外部子进程也受 race instrumentation。
7. 在同一最终源码、toolchain 和 runner 配置执行两原失败顶层、受影响定向测试、完整 CLI 六片，保存清单、JSON、退出码、skip 原因和资源。任一失败、缺片、取消、超时、准备失败或损坏结果均失败，不用选择性重试抵销。
8. 同一最终源码完成 normal 全仓、非 CLI 全仓 race、fmt/vet、doctor、staged release 安装及图验证、validate/build；核 full=false/full=true、tag 与发布依赖失败传播，不实际发布来证明拒绝发布。只有所有职责实际通过才称完整 CI 闭环；真实平台未执行单列待验。

## 8. 变更清单与无关范围

生产实施责任限于 S4 consumer→Evidence Writer、S4 锁内重放、S4 已登记报告统一读取及列出的现有调用点；原 Store、Recover、TR-002/S5 专业业务读取作为组合验收，不另造状态。低层读取实现归已有文件读取职责，避免放入 submission 导致 runtime/transition/qualitygate 循环依赖，也不新建报告台账。实际修改清单由上述符号和直接测试确定。

不扩查或修改本轮 NEW-FINDINGS 中 Fresh、catalog、S9 返回或其他独立问题；Worker 空日志问题由其原测试责任另行处理。既有 dirty 文件不属于本文修改。设计实施后应以实际差异复核删去 Writer 外发布、草案私卷错误旁路和错误的固定字节依赖，保留历史业务事实；不能只增加一个 helper 而让旧入口继续运行。
