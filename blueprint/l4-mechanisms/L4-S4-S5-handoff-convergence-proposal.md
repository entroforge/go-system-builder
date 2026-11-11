# S4→S5 完整交接与收敛设计

状态：**实施和验收中**，2026-11-10。本文按 [工作流收敛](L4-workflow-convergence.md) §3～§6，从专业责任推导整段交接；承接 [S4 接收方案](L4-S4-reception-wiring-proposal.md)，补足它尚未通过的 S5 出口及共享机械边界。本文的目标合同不表示当前实现已成立。

调查依据：仓库根下 `.local/framework-maintenance/fixture-replay-design-20261108/closure-20261109/s4-implementation-20261110/root-chain-investigation-20261110/FACTS-AND-DESIGN.md` 及其原始事件、快照；大报告 A/B 为 run `42773039390`。下列代码行号是调查快照定位，以符号及实际调用者为实施依据。

## 1. 已知事实、失败终点与设计取舍

`42771736757`、`42772280696` 均未通过整个 evidence 根占位的原自然验收。普通文件根的 S4 Post 已接受 S4、完成真实 TR-002 和 TASK/index 登记，Runtime 从 r7 到 r9、进入 S5；S4 报告在实际备用根，S5 formal inputs 可读。但 S5 register 的 `PreStageActivationEnvelope` 在固定 `.claude/evidence` 上 mkdir 失败，team/agent/packet 均未登记。默认 manifest 已产生而未登记；独立 issue 再次失败，不能抵销自然 Post 的失败，也不证明残留 manifest 的所有权。

符链根的自然 Post 已登记 team 和两题，且**实际向符链目标新增两份 activation**（319、306 字节）；原符链和 `unknown.txt` 未变。随后分别填写测试 PASS、调用生产 `SubagentStop` 的两次模拟事件，均因报告固定 evidence 路径含符链/非目录而拒收，无 S5 submission/evidence、无 TR-003。模拟 Stop 证明接线结果，不能证明平台启动或真实独立审查。普通文件例未进入两题发行，不声称该例实测过 Stop。`os.WriteFile` 可截断同名文件是源码可达风险；本次没有预置同名目标，**未实测覆盖**。准备失败的 run `42771505502` 没运行 Go，不算测试结论。

根因有三个不同职责：S5 原生审查不应依赖旧 generic plan activation；仍有实际消费者的共享 activation 家族须可靠发布并登记实际路径；S5 系统报告须在原 Writer 内接收并贯通全部实际路径消费者。不能只换一个目录或给固定根加重试。manifest 的作者意图与文件所有权又是独立问题，不能由报告修复顺带宣称解决。

选定方案先删除 S5 无消费者的旧接线，再复用原 Writer、原引用、原恢复和原领域消费者。保留专业检查、两位独立责任、真实批准及原出口判据；不新增状态表、根映射、摘要/Hash/OID、来源证明、作者提交、确认、交表或一律收紧权限。

## 2. 从责任到 S6 的唯一主线

| 层与所有者 | 本次职责及事实 | 消费者与下一责任 |
| --- | --- | --- |
| L3 主流程、模板、Skill | Main 形成真实 TASK、依赖、完成条件和 dispatch index；Reviewer 分别判断规范与任务。现有题目、方法和质量义务不变 | 本人原答卷表达专业结论；不把机器路径安全转成作者工作 |
| S4 自然保存与 Hook | 依据已登记 Main 原责任和真实成功保存事件调用 `SaveTaskPlanning`；不猜 Bash 文本、文件名或作者意图 | 原消费者校验答卷、TASK/index 与原正式来源；反馈 accepted、控制结果及后继准备分别成立到哪里 |
| Evidence Writer、Controller、TR-002 | 原锁内排他发布 S4 系统报告，登记实际 row.path；原 pending/state/journal 恢复；原 Controller 推进真实 TR-002 | TASK/index 登记与 S5 正式输入使用原已收事实，不重读私人答卷制造第二接收 |
| S5 manifest 准备与 register | 以实际 S4 报告和原 fileview 的正式输入构造两项独立责任；安全发布补全 manifest，原 team registration 登记实际 `manifest_ref` | 原 issuer 从登记事实准备两份 view/answer；manifest 存在不等于登记、登记不等于发行 |
| S5 issuer、指引 | 原 `s5_publication` 排他发布、登记 winner 和实际题卷路径；部分成功保留；给 Main 真实材料及派发责任 | Main 用平台原生能力派发；平台负责创建、运行、消息与继续。Hook 不执行通用调度，packet 不证明 worker 已启动 |
| S5 本人自然返回/Stop | 真实事件与原派发/packet 关联定位本人责任，读取原答案；保留现有结构、producer、subject/version 和适用真实授权检查 | 原消费者序列化 `document_review` 交同一个 Evidence Writer；Stop exit 0 不代表业务已收 |
| S5 接收与出口 | 锁内判断已有接收/赢家，首收排他发布并登记实际 evidence row；原 followup 调用真实 gate | 两份合格独立通过依 TR-003 进入 S6；fix_required 保留 `document_fix_required` 走 TR-004。报告、接收、阶段完成及 S6 工作准备分别报告 |
| S6 Main、平台与原执行链 | 接受原执行批次和真实阶段结果，沿现有 S6 native 责任准备、派发、工作及集成 | 不因 S5 成功就伪造 S6 启动/完成；S6 自身真实准备故障交 Main 原入口 |

一次合法自然 S4 保存应完成接收、TR-002、S5 manifest 登记及两份原题准备。真实系统故障时只承诺保留已成立事实、反馈准确缺项并由已有执行者续未完成部分；不能让 Main 重存、唤醒已履职 worker、执行无关 Read 来制造事件，也不承诺没有后续真实事件时无人续跑。

## 3. Manifest：登记事实优先，未知文件保留

自动准备先查当前责任已经登记的 `team.manifest_ref`，沿其实际路径、原 owner 和 team/entry 关系继续。不可读、损坏或原关联冲突如实反馈，不退回默认路径认领别的文件；已有 team 和已发 peer 不重新登记。尚无登记时，基于原 S4 正式输入自动准备，沿既有安全排他发布选择可用候选，将**实际路径**交原 register，并由成功 registration 成为引用权威。未登记孤儿、默认路径未知文件、同内容文件、目录、符链及未知祖先原样保留；它们不因存在而被读取、补全、覆盖或成为出题阻断条件。不新增 manifest 所有权账本或作者动作。

Main 的真实明确保存事件若已经明确指向原清单，且已有责任关联足以认定本次 Main 专业输入，则保留原专业表达（包括两项 scope），补全结果排他写独立安全路径，纳入同一原生准备/派发链，不原位改写原正文。事件只说明可消费本次专业输入，不授权覆盖任意同名文件。原 registered manifest、真实保存输入和系统自动补全分别有来源，不以正文像模板判断身份。

历史 `s5_manifest_questionnaire_e2e_test.go:314–341` 先写默认路径 scope，再以不携带路径的 `stage-answer issue` 采纳；没有明确保存事件和登记关系时，它与未知占位**无法自动区分**。选定安全取舍是不认领该未关联默认文件，自动准备仍可继续；所以这一历史行为可能改变，不能声称完全兼容或已取得作者意图。实施须用保留原文件及明确关联输入的用例表达这一取舍，不要求新增确认、交表、登记或重存来补证。若现有事件没有足够关联，保留未知，不凭角色、唯一候选或内容替作者选择。

并发自动准备可留下各自未登记候选；原 team registration 锁内以已登记同责任赢家为准，后到者沿赢家 manifest 和 packet 继续，不能覆盖、重复 team 或重复发行。失败、partial/pending 沿现有 registration/issuer 恢复；未登记失败候选保留，不按相同内容回收。`issue`、S4 handoff、发行、状态显示、诊断及原 hook 必须都展示/消费实际 `manifest_ref`，不能仍拼默认路径。

## 4. 只删除 S5 不必要 activation，修复仍被消费的共享机械

当前 S4→S5 两项原生 `plan_checkpoint`/`one_shot` 文档审查由真实 packet、owner、方法和原答案接收，不需 generic 预置 plan activation。登记这类明确 S5 原生责任时不再生成旧预置信封；新责任按原责任、dispatch 与 packet 关联分流。旧任务已经登记的 `activation_ref` 则是原在途路径的一部分：保留原 Hook 回退及原自动链，不能把旧引用一律改解释为新原生流程，也不能为了兼容旧任务而给新责任补 PLAN。S6 native 既有 skip 保持原义。

实施回归还发现，原 S5 测试辅助函数约 16 处主动执行 generic `agent-begin`。它不能作为新原生 S5 的必需动作；应逐项保留测试本来的责任：新原生流程通过实际 Start/答卷/Stop 验证，真正验证历史 activation、working 或已冻结执行的例子使用明确的历史在途 fixture。不能将 `begin` 全部改为空操作，也不能仅删失败断言。历史引用到旧 Hook 和新原生不创建 activation 两条链须分别验收。

这不删除全局 activation 生命周期。`PreStageActivationEnvelope` 两个直接生产调用为首次 `assignment/register.go:569`、非 native canonical revision 的 `assignment/record_revision.go:701–720`。通用 `register-workgroup`、S7 manifest/register、S8 `investigation_command.go`、S9 `repair_command.go`、legacy 及非 native revision 仍能经这两处进入共享机械。S7/S8 普通 plan_checkpoint 的专业直收不等于所有调用者均无需 activation；真正批准、旧执行/冻结工作区及实际消费者必须分别保留。上述共享路径受影响是源码分析，尚无本轮各阶段实测。

| 家族 | 作者/写入责任 | 原消费者及必须贯通的实际路径 |
| --- | --- | --- |
| 预置信封 | 原 register/revision 的 Writer/事务内排他发布；实际 ref 写入既有 agent | `hookctx/loader.go:629–642,2292–2305`、`assignment/auto_chain.go:139–160`、`plancheckpoint/recover.go:38–60,93–110` 与真实批准消费者 |
| activation message、work_start | 原 `auto_chain.go:248,287` 经 `activation_envelope.go:345–415` 发布；沿原 Writer/事务登记既有实际引用 | 原 activation/readback、自动链与恢复；真实批准不由预置信封或字段非空代替 |
| 失败清理、工作区和状态 | `register.go:580–603`、`runtime/store.go:677–754` 及原工作区责任 | 清理仅限能证明本次排他创建、当前仍安全且未被提交引用的对象；status、check、工作区分类/恢复按已登记实际 refs 识别 |

仍需发布的家族复用既有安全排他候选策略，不再 `MkdirAll/WriteFile` 跟随未知符链或截断文件；实际 path 贯通上述全部读者、原重放和 cleanup。候选路径/正文相同不证明归属，不能读未知占位来“复用”。旧已登记 ref 沿原位置读，不自动迁移、改写、删除或把未知目标认证为系统文件。原目标不可用时反馈真实故障，真实批准缺失时仍走原批准；不能要求重登记、重交原卷或为所有 worker 收紧工具权限。

`workspace/workspace.go:297–308` 当前 control-prefix 只列 `.claude/evidence/`；`integration/integrate_responsibility.go:350–460` 保留未知/私人材料。新增备用实际路径必须接入原 status/check、工作区分类、恢复及清理消费者；不能把所有 `evidence-alt-*` 一概认作系统所有或授权删除。共享机械修复必须覆盖原调用者，不能只在 S5 绕过后留下其余路径的同根风险。

## 5. S5 报告首收与完整出口

`submission/service.go:204–320` 保留原私卷读取、专业结构、当前责任和真实授权判断；删除 Writer 外固定 `.claude/evidence/submissions/<packet.ID>/document-review.json` 的 `Publish` 及读取未登记同内容候选的复用分支。序列化成功后以原 `EvidenceJSON` 合同传原报告；nil 与非 nil 空值保持既有含义，不靠时间戳、Hash 或额外作者字段认定同答。

`runtime/evidence.go` 与 `runtime/work_packet.go:260–344` 在同一原锁内先确定已有 submission/赢家；首收才排他发布并把实际 row.path 与 submission/followup 写入原 pending/state/journal。已登记同责任同答复用赢家；真正异答按原专业语义拒绝，不能覆盖。先完整读取、解析并验证赢家原关联，坏/不可读赢家报材料错误，不能统称异答冲突或用候选补证。并发、原重放与 Save 已收分支采用同一专业比较；未登记候选、取消残留及 orphan 均不认领。

以下每个消费者均从原 packet/submission/evidence 关系定位实际 row.path，不拼首选目录、不按文件名前缀授权读取。正式 REQ、TASK、contract、来源闭包仍沿原所选 fileview，不能将全部 `.claude` 或备用根声明为 disk 来掩盖断链。

| 实施落点 | 需要保持的一次消费与原语义 |
| --- | --- |
| `submission/receipt.go:198–225,270–297` | 本人/peer 回执、实际报告、待续事实一致；读取已收不依赖私人答卷 |
| `submission/service.go:251–274`、`runtime/work_packet.go:294–326` | 已收同答/异答及错误后重放读取登记赢家 |
| `cli/native_review_hooks.go:83–94,162–199,357–446` | 原本人 Stop/自然保存按责任和 packet 路由；实际失败输出/诊断不伪装已收 |
| `workpacket/document_review.go:34–52`、`qualitygate/evaluator.go:862` | fix_required 的 `requested_event=document_fix_required` 经实际报告进入原 TR-004 分流 |
| `qualitygate/evaluator.go:714–720,784,945,1064` | evidence qualification、producer 与独立性复读均读取当前已登记 S5 报告；保留两个不同独立责任及合格结论 |
| `transition/engine.go:645–653,767–825` | TR-003/TR-004 的 gate refs 与 required evidence 再读取同一实际报告，不能 gate 通过而执行失败 |
| `runtime/document_review_revision.go:433,512`、`ValidateCurrentDocumentReviewRevisions` | 修订报告、subject/version/current revision 与旧在途结果依原关联处理，不能冒充新结论 |
| `runtime/work_packet_issuer.go`、原 S6 入口 | 两合格结论后真实 TR-003 与执行批次保持；S6 准备和接手反馈核实际后继，不用直接写阶段代替 |

系统报告读取沿 S4 已选控制根实际路径操作复用低层职责，避免 runtime/transition/qualitygate 对 submission 的循环依赖。该操作只读已由调用者定位的登记报告，不做专业判断，不扫未知文件，也不访问 Git。其与旧 `workpacket.OpenParent`（拒所有符链、pin 目录/检测 inode）及生产 fileview（解析后根内、排除子 worktree）并不等价：沿 Writer 的 `safeEvidencePath` 解析后根内路径读，旧根内符链与登记子 worktree 报告可读；不再保证逐级无符链、目录 pin 或检测全部并发替换。解析后目标/祖先再被替换仍有读取变化内容甚至根外目标的窗口，特殊文件可能阻塞，不能承诺均能检测。原正式材料的 fileview/worktree 约束保持，发布仍拒绝未知占位。以上保证变化须与实际代码一致验收，不用裸 Disk 读法假称等价。

S5 题目当前将完整 packet ID 按 120 字节拆组件（`workpacket/packet.go:133–151`），报告却使用单组件；报告发布应复用原合法 ID 分段，不能只换根留下可能的 ENAMETOOLONG。现行 S5 Publish、replay/receipt 各有 1 MiB 限制，S4 已选择完整已登记报告读取，资源合同并不一致。本轮不能新增更严门槛；目标为原生成的合法 S5 系统报告可由全链完整消费，读写不以当前候选长度截断赢家，原作者答卷预算不变。真实 active 大 subject/version 是否超出现有限制必须先验：已测 4.5 MiB S4 的大版本来自 cancelled TASK，**没有证明合法 active S5 超限或长 ID 失败已经发生**。内存/特殊文件和运行成本按实际反馈，不用新字节 gate、延时或删字段获得通过。

## 6. 阶段状态与中断恢复

| 实际窗口 | 已成立事实 | 原恢复及显示责任 |
| --- | --- | --- |
| S4 专业校验/锁等待失败，或发布前取消 | 无新接收 | 原卷、未知文件及 Runtime/journal 保持；真实新保存按原入口处理 |
| 报告/manifest/题卷排他创建后、登记前中断 | 可有未登记残留；不证明接收或所有权 | 残留保留，下次选安全新候选；不靠内容认领或反向阻止准备 |
| pending 已写、state 未换；state 已换、journal 未追；journal 已追、marker 未清 | 原 Store 能识别的 candidate/pair/提交事实 | 原 pending/recovery 补未完成事务，保持实际赢家 path，不重复接收或推进 |
| S4 accepted，TR-002 pending/no_transition | 接收成立，控制未完成 | `receipt.Recover` 及成功 Main Post 只续原 followup，不读私人卷 |
| TR-002 advanced，S5 register/发行失败或一题已发 | S4/阶段成立，可能仅一 peer 已登记 | 同一次 handoff 报 Main consumer_pending/实际错误/实际材料；原 issue 与 issuer 只补未完成责任，不重收 S4 |
| 两题已发，平台未启动/worker 未返 | 出题成立；无审查结果 | Main 按原生平台实际派发/继续，Hook 不代启动或伪造 worker 完成 |
| 首位 S5 已收，peer 未收；已收后私卷删除或变更 | 首位实际 row/submission/followup 保留 | 后继沿已收报告；peer 按本人责任返回；真实新尝试保持原拒绝/冲突语义 |
| 两 S5 已收，TR-003/TR-004 或 S6 准备失败 | 原报告与已成立控制事实保留 | 原 Controller/Recover 续未完成后继；gate 真失败准确交 Main，不能改为 PASS |
| 旧已发、已收、已激活/显式批准、revision 或跨阶段晚到 | 原 owner、dispatch、packet/refs、授权、代际事实 | 沿原关联及原批准消费者接收/恢复，不把旧结果计作新责任，不撤销历史、不额外要求重交 |
| 控制根整体不可用或登记赢家坏/不可读 | 只能确认最后持久事实 | 交真实存储/材料错误与原入口，不迁移到另一控制根、不猜替代文件 |

现实现成功 Post 只自动续 S4 的 pending/no_transition；S4 已 advanced 后 S5 guidance 主要只读显示，不能声称任意自然事件已能自动修复。目标接线在当前 S5 Main 的**已有已收 S4 责任、原 team/entry、已登记 packet**下，成功自然 Post 对缺失的当前两题调用同一原准备器；不重开 S4，不新建 pending 表，不对普通 Pre/失败 Post 发行，不遍历无关历史。完整两题已登记则快速返回；partial 保留 peer。沿既有事件预算及当前责任约束尝试，失败用原诊断和 issue/recover 交 Main；无后续事件不承诺自动继续。此处是待实施的只读→有限续准备差异，必须单独验收预算、去重与旧在途，而非以提示文案冒充能力。

## 7. 大报告成本：先做同一事件等义解析

选定的首批优化是两处有严格等价条件的解析快路径：`sharedmodel.Section` 仅在原文不含三个连续反引号时跳过不可能匹配的 fence 正则；`semantic.loadTaskDocumentsWithFiles` 仅在原文不含 `Primary contract:` 时跳过不可能匹配的对应正则。含该字面量时仍执行原正则，不引入文件大小阈值、改变原检查或删除 cancelled TASK 的实际判断。可以在同一调用中复用已经解析的同一份内容，但跨锁、Runtime revision 或活动 branch 可能变化，不能把先前读取无条件缓存为当前写入资格，不新增 OID/hash、来源要求、全局缓存或预算延期。锁内仍以原当前状态和登记赢家判断，不用快路径替代并发判定。

run `42773039390` 的带采样单次顺序 A/B：normal `3.603s → 1.866s`；race in-process `13.156s fail → 10.061s pass`。正常外部 Harness `2.963s pass` 是另一种真实执行形态；race in-process 与外部子进程并不等价，不能声称外部子进程受相同 race instrumentation，或据单次、有采样及顺序效应保证所有环境均过预算。最终验收沿已有内部预算及外部 watchdog，不延长预算来修复。

Git Trace2 仅统计 Hook 时窗：normal baseline/candidate **各 373 次 Git 启动**（ls-tree 218、show 150、worktree 5）。候选解析快路径没有减少这些读取；race candidate 次数高于失败 baseline 因为候选走完全程，不可据此宣称总读取减少。重复正式来源读取仍是实际成本；活动分支未固定，后续若要减少，应按真实责任与读取时序另作设计，本轮不顺带跨锁缓存或另立来源状态。

本轮最终源码的 race 定向验收中，4.5 MiB 合法大报告在 S4 已收后多次因后继预算未完成；先前单次优化通过不足以证明收敛。进一步按原 guard 责任核对：`TaskBatchCompleteWithFiles` 只消费每个任务的相对路径和 Status，原完整 loader 却还计算条款、依赖、阅读材料与作用域；这些结果和阅读问题均未被该 guard 使用。允许在**同一 guard 调用内**复用同一目录发现、文件读取、REQ 归属和 Status 解析，仅跳过这个消费者不使用的计算；完整 `TasksCheck` 仍使用完整解析。阅读材料的多余只读调用会减少，不声称读取轨迹完全相同；不跨事务缓存、不延长超时、不新增门禁。等义用例需覆盖缺目录、读取失败、归属、缺/不同 Status 和取消任务。

该极端输入的容量验收直接调用原接收服务及控制器，核完整报告、TR-002、S5 消费与重放，不把 race 插桩下进程内 Hook 的耗时作为文件大小上限；常规材料的自然 PostToolUse 与原交接另有独立测试。**这不等于极端输入的一次 race Hook 已达成及时推进**：本机独占原 Hook 测试两次为 S4 已收、后继 pending，优化后仍有一次 pending；同一极端输入曾在普通外部 Harness 下约 3 秒通过，但不是所有机器的保证。完整来源读取的性能及预算仍须单列，不通过延长预算、删失败记录或重试后声称已修复。

## 8. 实施落点与按终点验收

### S6 原准备的补充接线与后续边界

实施后自然链仿真进一步确认：整个 `.claude/evidence` 被未知文件或符链占用时，S4 已收、两名模拟 reviewer 的 Stop 已收，TR-003 能真实进入 S6；但原 `s6 prepare → registerNativeS6Delivery → assignment.Register` 在 `stageRegisteredRecordSources` 内仍用旧根发布和探测 source snapshot，因而 S6 准备失败。两个 `fix_required → TR-004` 分支已经走通。该发现说明原清单遗漏了 S6 准备中的生产者，不能把 TR-003 成立描述成 S6 已准备。

本轮原定终点是 S6 **真实准备**。对应接线补全为：原 Assignment Writer 在首次登记及 revision 内对每份 snapshot 排他选择实际路径，并写回既有 `Record.SourceSnapshots[].Path`；原 Task、Manifest 与 ContextSource 仍是当前专业来源，snapshot 仅保留原有历史职责。未知文件、同内容候选和发布后未登记孤儿不认领、不覆盖、不按字节清理。`assignment_context.go` 的 native PLAN/Result 草稿发布与实际 `context` 中的引用也必须检查并在需要时接通备用根；原答卷、授权和正式 fileview 不变。S6 准备及原 brief 的验收必须经过原入口，不能直接写状态。

S6 随后的本人结果发布、Worker check 附件、Main checkpoint、检查及 TR-006 已发现独立的固定根接线。尤其 checkpoint 当前可变恢复使用固定坐标，若改为不确定的备用位置，需要解决已有责任如何在中断后找到真实赢家；不能凭文件存在或相同内容认领，也不能借此增加作者动作或收窄门禁。它们超出本文所承诺的 S6 准备终点：记录为后续机制设计，不得声称整个 S6→S7 在根占用下已通过，也不得直接把一次性报告发布器套到可变 checkpoint 上。`ControlFile` 虽只识别旧前缀，但当前全仓无生产调用，不能据此臆断实际工作区故障。

除 §4～§5 已逐个列出的生产消费者外，实施必须核对以下原入口及共享边界；不是要求改动每个文件，而是说明保留/删除的职责：`cli/run.go:2728–2754`、`questionnaire_observer.go:28–55,228–266`、`stage_answer.go:140–153,456–471` 的真实事件/issue；`s5_planning_handoff.go:19–71`、`s5_manifest_questionnaire.go:26–140,267–355` 的安全补全、登记和实际路径；`s5_guidance.go:49–109` 的 Main 状态/恢复；`submission/task_planning.go`、`runtime/evidence.go`、`runtime/store.go` 的 S4 已有 Writer 事务；`submission/service.go:102–201`、`s5_publication.go`、`runtime/work_packet.go:57–121` 的两题发行/赢家；`docs/control/loop-definition.json:2314–2325` 的来源声明边界（不扩大到全部备用根）。删除被替换的 Writer 外首收、S5 无用旧 activation 及仅为它们维持的测试，保留业务原卷和真实历史事实。

验收按以下顺序先证明最短整链和反例，再做共享/全仓；所有为待执行，不是本次结果。

1. **先补原失败的终点**：保留两原失败顶层，用同一最终源码经原 S4 Post→manifest/register→两题→各本人 Stop→实际 evidence→真实 TR-003→S6 原准备；另用真实 fix_required 走 requested_event/TR-004。fixture 的模拟答案/Stop 单列机制证据；另验真实平台分工与独立返回，不冒充专业审查质量。
2. **保护真实副作用边界**：普通根文件、内/外符链、未知目录、祖先与同名/同内容/异内容目标分别核前后字节和实际目标清单；S2/S3、原卷、未知文件及未登记孤儿保持。覆盖默认 manifest 未关联不采纳且不阻断、真实明确保存保留 scope、已登记 manifest_ref 优先、并发登记赢家、partial peer。未知 manifest 的保护不能靠测试只验证输出 PASS。
3. **全读者与门禁反证**：强制实际备用报告，分别到达 receipt、native Stop、重放、requested_event、qualification、independence、TR-003/TR-004 required refs、revision 及 S6；专业错误、不同 owner/主体/版本、非独立结果、缺真实批准仍准确拒绝。业务 REQ/TASK/contract 必须继续选定 Git/fileview；不能报告不可读而偶然拒绝后便称专业负例有效。验合法长 ID、active 大输入与旧读限，记录未实证边界。
4. **共享 activation 原调用者**：通用 register、S7、S8、S9、非 native revision 以及真实显式批准/legacy 各验必要家族发布、actual refs 的原读者、auto_chain 的 activation/work_start、readback、重放、status/worktree/check 和 cleanup。未登记目标不被认领，失败清理不删未知物；真实批准仍有效，S6 native skip 不变。不能只验新的低层 helper。
5. **中断与历史**：同答/异答并发、坏赢家、取消、发布后未登记、pending/state/journal/marker 全窗口、S4 已 advanced 后缺题的真实成功 Post、无后续事件、旧已发/已收/已激活/revision 晚到，核只续未完成且不丢事实、不重复接收。根内/越界符链、登记子 worktree、并发替换和特殊文件记录真实行为与读取保证边界。
6. **成本及最终同源码闭环**：大报告正常外部 Harness、原 race in-process、原预算/watchdog、采样开销与 Git 启动次数分别报告；等义解析负例核语义未变。再执行受影响定向测试、完整 CLI 六片、normal 全仓、非 CLI race、fmt/vet、doctor、staged release 安装、图检查及 validate/build，核 full=false/full=true、tag/发布依赖失败传播（不实际发布）。保存源码/toolchain、分片清单、退出码、skip/取消/准备失败及资源；不能选择重试覆盖原失败或借旧版绿灯关闭本轮。真实平台未执行及必需证据缺失单列待验。

完成标准是这段原入口交接、共享所有权、真实门禁和相应中断均成立，且有效指引没有反向增加作者工作。仅报告改用备用根、Hook exit 0、两题存在、辅助测试通过或本文完成，均不表示实施或验收完成；实施与验收结果应按已实际证明的终点报告。
