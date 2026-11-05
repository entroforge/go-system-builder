# DEFECT 落地方案(69 条真缺陷 / 38 个根因组)

> 本轮方案已获授权进入**生产修复**阶段:21 个工作包按文件级互斥切分并行落地,每包一份「已固定改动」简报作为权威工单。
> 改动范围仍受 §0.1 判据与红线约束 —— 只恢复结构性事实,不引入任何散列门禁。
>
> 本轮硬约束:**revision / sha / hash / digest / fingerprint / checksum / round / CAS 一律不得作为门禁**;目标是统一减负、退役这些门禁。
> 判据是 blueprint,不是代码现状。要恢复的是**结构性事实**:权威根、原责任坐标、原来源存在性、原题面有效性、原责任是否未完成、原阶段是否正确。
>
> 历史散列/revision 字段:**继续原路径读取,不重算、不作为资格、也不因存在而拒收**。schema 解法是「显式列退役字段为允许项」,**不是** blanket `additionalProperties: true`。

---

## 0. 方案总览

### 0.1 判据与红线

| 规则 | 内容 |
|---|---|
| 蓝图为唯一判据 | 测试/生产现状只是事实,不是规范 |
| 结构性事实优先 | 权威根 / 原责任坐标 / 原来源存在性 / 原题面有效性 / 原责任未完成 / 原阶段正确 |
| 散列退役 | revision、sha、hash、digest、fingerprint、checksum、round、CAS 不得作门禁;也不得「为了转绿」重新引入 |
| 历史值只读 | 历史散列/revision 字段继续按原路径读取,不重算、不作资格、不因存在而拒收 |
| schema 解法 | 显式列出退役字段为允许项,不是放开 `additionalProperties` |
| 断言不得削弱 | 不删断言、不放宽阈值、不把 `t.Fatal` 改 `t.Log`、不加 `t.Skip`、不删文件;鼓励加反向断言 |
| 落点纪律 | 每个修复点给出 `file:line`、判据条款、定向验证命令、风险 |

### 0.2 方案分档

| 档 | 含义 | 组 |
|---|---|---|
| A 档 | 恢复结构性判据(权威根/题面/就绪/坐标),不引入任何散列 | G01-A、G01-B、G03-A、G04-A、G04-B、G06-A、G06-B、G06-C、G12-A、G12-B、G12-F、G12-G |
| B 档 | 退役/放宽已退役的机器门(逐断言覆盖、证据存在性、复用分级、阶段/输入适配) | G02-C、G03-B、G05-A、G05-B、G06-D、G06-E、G07-B、G07-C、G08-A、G08-B、G08-C、G09-A、G09-B、G09-C、G10-A、G11-B、G11-C、G11-D、G11-E、G12-D |
| C 档 | 幂等/静默吞事件/发布尾一致性等行为修正 | G07-A、G10-B、G10-C、G10-D、G12-E |
| D 档 | 本轮现场新发现(原 36 组之外) | G13、G14 |

### 0.3 批次与文件冲突

四批次的文件重叠见 §6。**跨组共享同一生产位置时必须合并成一个修复点**,否则两组同时改同一行必然冲突。

---

## 1. D1 组落地方案(接收权威与题面来源)

### G12-A — S3 契约包校验丢失权威根与冻结输入身份

**问题**:`runtime.ValidateContractPacket` 把 `root` 和 `files` 两个参数完全闲置,任何 checkout 都能通过 S3 契约包校验 —— 复制一对 Runtime 文件即可冒充第二个控制面;契约包冻结的输入集合与 S2 登记的设计关联从未被比较。

**蓝图依据**:`L4-state-transition-core.md:31`「权威根由 REQ 的 `bound_req.workspace.project_root` 确定……不能复制一对 Runtime 文件就成为第二个控制面」;`:339`「权威根或 state／journal 坐标不符 → 回绑定工程的准确根,不复制 Runtime 继续」。

**解法**:S3 契约包的接收资格只由**权威根**与**原责任坐标**决定,不由散列决定。契约包必须证明 (1) 当前校验位置就是 REQ 绑定的权威根;(2) 责任坐标与当前登记的 S3 责任一致;(3) 冻结输入集合与 S2 登记的设计关联(ID/Path/Version)一致,且原来源在**声明的来源视图**里仍然存在(只判存在与可读)。不逐字节比较设计正文,不比对任何 digest,���比较 live 目录清单。

**具体改动**:
- `internal/runtime/contract_authority.go:71` — `CurrentContractBinding` 第二个返回值由 `_` 改为 `designs` 并保留(当前登记的设计集合原本被直接丢弃)。
- `internal/runtime/contract_authority.go:70-79` — 在 `samePlanningResponsibility` 之前插入 `fileview.ValidateAuthority(root, state)`(`internal/fileview/contracts.go:42-61`,纯 canonical 根比较,不含散列);权威根不符即拒收,消息指向 REQ 绑定的根。
- `internal/runtime/contract_authority.go:75-77` — 追加冻结输入身份比较:把 `designs` 与 `p.Inputs.Designs` 按 ID/Path/Version 对齐;用 `workpacket.BuildContracts(binding, p.Inputs)`(`internal/workpacket/contracts.go:53`)重算契约包并比较身份字段(ID/TemplateID/Binding/Inputs/ViewPath/AnswerPath)。重算只在内存中做身份比较,不写任何文件。
- `internal/runtime/contract_authority.go:78` — 追加「原来源是否存在」:对每个登记设计路径用 `files.ReadFile(path)` 试读;读不到即拒收并带上路径。只判存在与可读,**不比较内容**。
- `internal/runtime/contract_authority.go:70` — 补 `files == nil` 守卫,文案沿用同族先例 `internal/runtime/contract_batch.go:39-41`「S3 requires original authoritative file view」。
- `internal/runtime/contract_authority.go:67-69` — 注释改写为「权威根 + 原责任坐标 + 冻结输入身份 + 原来源存在」,并明确声明不计算、不登记、不校验散列。
- **不恢复**:`liveContractDirectoryEntries` / `sameDirectoryEntries`(live 树已不存在,恢复会打破 `internal/runtime/contract_sources_test.go:109`、`:134`);逐文件 digest 比对与「磁盘 vs 视图逐字节相等」一律不恢复(`internal/runtime/contract_batch_test.go:251`、`:261` 已确立「checkout 差异不得成为门禁」)。

**验证**:
- 定向:`TestContractAuthorityUsesOriginalS2RegisteredDesign`(其中 `:399-401` 权威根断言由本组修复)、`TestResolveContractInputsClosesScenarioAndLocalDesignDependencies`、`TestResolveContractInputsPreservesIssuedPacketWhenScenarioRootAppears`、`TestResolveContractInputsUsesSelectedGitSourcesDespiteCheckoutDifferences`、`TestContractArtifactsReadsOriginalSourceViewWithoutCheckoutEqualityGate`、`TestContractArtifactsUsesBoundBranchWhenCheckoutHasOtherContent`。
- 命令:`go test ./internal/cli/ -run 'TestContractAuthorityUsesOriginalS2RegisteredDesign'`;`go test ./internal/runtime/ -run 'TestResolveContractInputs|TestContractArtifacts'`;`go test ./internal/fileview/`。

**风险**:
- `internal/cli/design_answer_e2e_test.go:402-408` 的第三条断言(「设计正文被改写后 S3 校验必须失败」)在本组内无法满足,需 owner 裁定:退役散列后没有可用的结构性事实(`workpacket.Reference` 已不携带 digest;唯一历史字节记录是 `state["documents"][*].sha256`,而 `L4-workflow-convergence.md:281` 规定无 hash 不得宣称可识别篡改;设计正文位于 `docs/**`,属 `git_tree` 来源,磁盘改写只是 checkout 差异)。**建议拆出单独成例**,替换为蓝图版断言:(a) 权威根不符必须拒收;(b) 登记的设计关联(新版本/新 ID)变化必须使旧契约包失效;(c) 登记设计文件不存在必须拒收;(d) 正文漂移不构成机器门,按 `L4-state-transition-core.md:345` 交回原 owner。
- 冻结输入身份比较用「当前登记的设计」对比「包内冻结的设计」,而 `internal/runtime/planning_rework.go:131` 的返工路径校验的是**旧**签发的 S3 包;若设计在签发后被重新登记,该比较会阻断返工。落地前必须实跑 `internal/runtime` 全包确认返工路径语义。
- `files.ReadFile` 的存在性检查依赖调用方传入正确的来源视图;若某条调用链传的是 `fileview.Disk` 而磁盘缺文件,会把 checkout 差异误判为缺来源,需按调用点逐个确认。

### G12-B — S5 独立复审接收链丢失原题面来源校验

**问题**:Worker 的复审答卷来源被替换后(Worker 视图文件被清空、冻结输入被改写)仍被接收 —— `validateSource` 的 `!accepted` 分支现在只剩一条生命周期游标检查,原题面来源校验整段消失。

**蓝图依据**:`L4-hook-acceptance-checklist.md:15`「无散列验收同时检查有效题面/指引、schema……实际 Hook 路径不计算、登记或验证任何散列……历史值可按原记录读取但不重算、重验或登记到新事实」;`L4-workflow-convergence.md:281`「测试可记录既有历史数据但不得以散列作为验收前置」。

**解法**:恢复接收链上的**结构性**来源校验,并且只用「题面/schema 有效性」与「原责任坐标」,不用字节相等、也不用散列。Worker 视图必须能解析、必须通过 `work-packet-execution.schema.json`,其身份字段(执行根、世代、owner、作者路径、包坐标)必须与登记的执行和包一致;执行登记本身必须仍与 REQ 绑定权威一致;冻结输入只校验**位置与结构**,不校验历史字节。

**具体改动**:
- `internal/submission/source.go:150-156` — `!accepted` 分支恢复结构校验:重新取得执行登记(`workspace.Decode(state)`),依次执行 (1) 执行登记对 REQ 绑定权威的校验(`internal/workspace/workspace.go:682` `ValidateAuthority`,纯根/分支比较);(2) 冻结输入的结构与位置校验(`internal/workspace/workspace.go:459` `ValidateInputs`,其注释即「does not authenticate historical input bytes」);(3) Worker 视图的题面/schema 有效性校验(`internal/workspace/document_review.go:107` `ValidateDocumentReviewView`)。
- `internal/workspace/document_review.go:124` — 把 `bytes.Equal(data, append(expected, '\n'))` 的**逐字节相等**换成题面/schema 有效性判定:解析 Worker 视图 JSON,用 `internal/schema/assets/work-packet-execution.schema.json` 校验(`internal/workpacket/execution.go:63` 是同一 schema 的既有用法),再比对身份字段 —— `execution_root` == `e.Path`、`execution_generation` == `e.Generation`、`owner` == `e.AgentID`、`authoring_path` == 该执行的答卷路径、内嵌 `packet` 的 ID 与绑定坐标 == 登记的包 `p`。
- `internal/workspace/document_review.go:116` — 重算出的 `expected` 不再用于字节比较,仅用于取出身份坐标;`:127-129` 的既有注释(输入路径只是位置,历史 digest 不构成授权条件)保留并扩展到视图本体。
- `internal/submission/source.go:171` — 保持现状:父提交此处曾带 `e.BootstrapSHA256 == ""` 这一**散列门**,已退役,**不得**回填。

**验证**:
- 定向:`TestIndependentReviewRejectsCorruptSourceWithoutReception`(子例 `view` 由本组修复)、`TestExecutionInputsUseOriginalLocationsWithoutHistoricalDigests`、`TestChangedInputDoesNotRequireContentAuthentication`、`TestExecutionRegistryCannotOverrideRequirementAuthority`、`TestNativeContextBindingCheckpointPreservesFixedFactsAndRejectsMissingSources`。
- 命令:`go test ./internal/cli/ -run 'TestIndependentReviewRejectsCorruptSourceWithoutReception'`;`go test ./internal/workspace/ -run 'TestExecution|TestChangedInput|TestWorkerControlRoot'`;`go test ./internal/submission/`。

**风险**:
- `internal/cli/stage_answer_e2e_test.go:666-672` 的 `frozen input` 子例(改写 `.claude/inputs/<path>` 正文并要求拒收)在本组内无法满足,需 owner 裁定:冻结输入只有**位置**没有可对照原内容。建议按蓝图判为 STALE,拆出为独立子例改述为「冻结输入缺失/位置逃逸必须拒收」,或由 owner 裁定保留为红;**不得以引入 digest 比对来转绿**。
- `ValidateDocumentReviewView` 还有两个调用方 `internal/cli/workspace_answer.go:64` 与 `internal/cli/workspace_launch.go:85`;字节相等降级为 schema+身份后这两处拒收强度下降,必须实跑 S5/S6 用例确认没有放行应当拒收的来源。
- `internal/workspace/workspace.go:499` 的 `ValidateExecution` 会 `git rev-parse` Worker 工作区;接收时工作区可能已被清理,会把正常接收误判为失败。**不建议恢复**。
- 恢复 `workspace.Decode` 与登记校验会新增一次状态读取,需确认接收路径没有因此多写 journal 事件(`internal/cli/stage_answer_e2e_test.go:690-693` 断言被拒来源不得追加领域事实)。

### G12-G — S9 候选登记无条件化 + 兼容入口诊断丢责任引用

**问题**:S9 Worker 交回的 Result 没有逐单元结果时,Stop 钩子仍登记了 workspace 候选并写入 `DeliveryRef`,专业遗漏被当成完成;随后 `runtime task-integrate` 落到一条既不点名责任也不说明是谁的检查选择消息。

**蓝图依据**:`L4-hook-acceptance-checklist.md:52`「C03:责任由已有任务与登记坐标准确定位……缺字段指向本人实际路径与字段,未知就报告未知」;`L4-hook-platform-wiring.md:124`「未就绪保留原稿,具体结构错误给准确位置,同一暂缺不反复刷屏……已有可理解专业结论或本卷既有必要表达即接收」;`:126`「非关键缺项不阻止接收,不逐条比对题面或断言集合增加覆盖门……也不能从所有 checks 成功推算 PASS」。

**解法**:分两件独立的事。**(a) 完成位不得推算** —— 候选的完成位必须来自作者自己的单元记录,不能硬编码;否则 `RepairResultWorkComplete` 无法区分「完成」与「未表达」,等于从 checks 推算 PASS。**(b) 兼容入口必须点名原责任** —— 无论走「候选缺失」还是「候选已登记但专业工作未完成」,诊断都必须点名 assignment、agent 与原 packet。

**具体改动**:
- `internal/repair/issued_repair_result.go:696` — `RepairResultRequest.Result: "pass"` 是硬编码完成位;改为由作者自身的单元记录推导(全部单元为 `pass` 才是 `pass`;没有单元记录时不得断言 `pass`)。这是「不推算 PASS」的唯一落点。
- `internal/repair/issued_repair_result.go:566-580` — `ensureRepairResultCompleted` 目前**无任何调用点**(死代码);接回候选登记前的就绪判定,并补齐「verdict=completed 却没有任何单元记录」这一自相矛盾情形。判定只用答卷自身的 `verdict` 与 `unit_results`,**不**拿原 Plan 的单元集合做逐条比对。
- `internal/repair/issued_repair_result.go:519` — `createReceivedRepairCandidate` 之前接入就绪判定:就绪才登记候选并写 `DeliveryRef`(`:544-550`);不就绪则保留原稿、不登记候选,诊断指向本人实际答卷路径与字段(`/answer/unit_results`)。
- `internal/submission/repair_result.go:164-167` — 与「不就绪」路径统一:答卷原样保留,收据如实记录未就绪与字段位置,不写候选、不写领域事实;钩子不阻塞(返回 0)。
- `internal/cli/s9_handoff_projection.go:151-166` — `summary()` 目前只输出 owner 与 assignment(`:159`/`:161`/`:164`),**不输出原 packet ID**;补上原 PlanReport packet ID(`p.Plan.id`)与 Result packet ID(`p.Result.id`),并改为输出该执行自身的 assignment/owner。
- `internal/cli/workspace_delivery.go:92-95` — 「候选缺失」分支已走 `handoff.summary(*root)`,路由不变;确认输出同时含 assignment/agent/packet。
- `internal/cli/workspace_delivery.go:128` — 「候选已登记但专业工作未完成」的消息必须同样点名 assignment/agent/packet,格式沿用同文件 `:102`/`:106` 已有的 `owner=%s assignment=%s` 样式。
- `internal/cli/workspace_delivery.go:445` — 「S9 Main check selection is unavailable」必须点名原责任。
- `internal/cli/run.go:1629-1633` — 兼容入口的 `--assignment`/`--agent` 转交未丢坐标,丢失在消息层,无需改转发参数。

**验证**:
- 定向:`TestS9TaskIntegratePreservesAcceptedPlanWhenCandidateIsMissing`、`TestRepairResultStatusStopsOnAnyNonPassResult`、`TestS9CandidateUsesOriginalResponsibilityWithoutDigest`、`TestS9HandoffStoresOriginalPathsWithoutHistoricalDigests`、`TestS9ArtifactPathComparisonDoesNotUseHistoricalDigests`、`TestS9RepairNextActionKeepsNativeCandidateWithMain`。
- 命令:`go test ./internal/cli/ -run 'TestS9TaskIntegratePreservesAcceptedPlanWhenCandidateIsMissing'`;`go test ./internal/repair/ -run 'TestS9|TestRepairResult'`;`go test ./internal/submission/`。

**风险**:
- `internal/cli/s9_task_integrate_handoff_test.go:35-37`(「不得登记候选」)与 `L4-hook-platform-wiring.md:124` 存在真实张力,需 owner 裁定:两种读法 —— (i)「verdict=completed 但零单元记录」是**答卷自身**的结构矛盾,不登记候选;(ii) 它是「可理解的专业结论」,应照常接收,则断言改为「候选已登记但完成位不得被推算为 pass」,生产修复退化为只做 (a)+(b)。无论选哪条,(b) 的诊断点名都是必须项。
- 完成位从硬编码改为推导后,`internal/repair/issued_repair_result.go:820` 的 `RepairResultWorkCompleteWithContext` 会对缺单元记录的结果返回「未完成」,改变 `internal/cli/workspace_delivery.go:123-129` 的分流,影响 `internal/cli` 多条 S9 集成用例,必须实跑逐条比对。
- 「就绪判定」接回后与 `internal/submission/repair_result.go:164` 的既有 verdict 门叠加,需避免同一暂缺重复提示(`L4-hook-platform-wiring.md:124`「同一暂缺不反复刷屏」)。
- 诊断新增 packet ID 后,`internal/cli/s9_task_integrate_handoff_test.go:62` 的禁止串「invalid packet artifact path」「through PostToolUse」必须仍不出现;新增文案不得复用这两处历史措辞。

### G04-B — 已绑定重放路径只做 verifyFixedContext,不重新核验声明的原始来源

**问题**:`runtime assignment-context prepare` 在已有绑定时走重放分支,只复核固定上下文自身,**不重新核验它声明的原始来源是否还在**;已安装的 Skill 文件被删除后,重放仍报告就绪。

**蓝图依据**:`L4-hook-acceptance-checklist.md:52`(C03);`L4-state-transition-core.md:345`「专业材料漂移或缺失 → 交原 owner 处理来源、影响或返工,不维护成自动通过」。

**解法**:重放与替换分支都必须重新核验**声明来源是否存在**(只判存在与可读,不比对内容、不重算散列)。固定上下文登记的每一个冻结输入(`frozen_inputs`)都必须在权威来源根下仍然可读;缺失即拒收并点名该路径,交回原 owner。

**具体改动**:
- `internal/cli/assignment_context_binding.go:124-126` — 在 `verifyConventionView` 之后追加独立校验:遍历 `c.Inputs` 的每个键(`internal/cli/assignment_context.go:38` 声明,`:205-207` 由 `selection.Selected` 的 `skill.Source` 填入),逐个在权威来源根下试读;不可读即返回错误并带上该路径。错误文案沿用同族先例 `internal/cli/context_conventions.go:138`「convention source is unavailable: %s」。
- `internal/cli/context_conventions.go:87-90` — `verifyConventionView` 在 `b == nil` 时直接返回 nil,导致**声明来源核验整体被跳过**;新增核验不得依赖约定包,必须独立于 `c.Conventions` 是否存在。
- `internal/cli/assignment_context_binding.go:253-259` — 重放分支保持调用 `verifyFixedContext`,由上面的新增校验统一承担;该分支在任何写操作之前失败,天然满足「不改写作者答卷」与「Runtime 字节不变」。
- `internal/cli/assignment_context_binding.go:263-268` — `--replace-generation` 分支同样走 `verifyFixedContext`,自动获得同一校验。
- `internal/cli/assignment_context_binding.go:80-161` — `verifyFixedContext` 注释补一句:它核验「已登记事实 + 声明来源的存在」,不重新认证来源正文。
- `internal/cli/assignment_context.go:205-207` — 确认 `inputs[skill.Source] = skill.Source` 登记的是**来源路径**,新增核验因此天然是路径级判定。

**验证**:
- 定向:`TestExplicitAssignmentContextRejectsMissingSourcesWithoutAuthorOverwrite`、`TestNativeContextBindingFreezesAcrossWorkingStateAndRejectsSourceDrift`、`TestNativeContextBindingCheckpointPreservesFixedFactsAndRejectsMissingSources`、`TestExplicitAssignmentContextDeliversSourcesAndPreservesLegacyDraft`。
- 命令:`go test ./internal/cli/ -run 'TestExplicitAssignmentContextRejectsMissingSourcesWithoutAuthorOverwrite'`;`go test ./internal/cli/ -run 'TestNativeContextBinding'`。

**风险**:
- 冻结输入里可能包含**曾经存在、后来被合法删除**的来源路径;新增核验会让这类重放从「就绪」变成「拒收」。落地前必须确认 `frozen_inputs` 的语义是「本次仍然依赖的来源」而非「曾经读过的来源清单」;若语义不同,应改为只核验 `c.ReadingOrder`(`internal/cli/assignment_context.go:39`)所列的当前必读来源。
- 读取来源要走声明的来源视图:`docs/control/loop-definition.json` 的 `file_sources` 把 `.claude/skills` 声明为 `disk`、其余默认 `git_tree`;`.claude/skills/**` 删除即不可读(符合预期),`docs/**` 走 git 树(磁盘删除不可见,属既有 checkout 差异立场)。两种语义要分开表述。
- 新增的是**存在性**,不是**内容认证**;实现时不要顺手加内容比对(`internal/cli/assignment_context_binding_test.go:207-250` 已确立「恢复不重新认证冻结来源的散列」)。

---

## 2. D3 组落地方案(专业字段级校验、完成度与领域投影)

### G06-A — Result/PlanReport 专业字段级校验被放宽,原消费者不再对 null/缺失/未知字段产出字段错误

**问题**:Worker 把私人 Result 里的 `migration_ref` 写成 `null`、写进结构之外的未知键、或把 PlanReport 的 `red_checks[].result` 写成 `pass|fail|blocked` 之外的值,S9 接收照样成功并生成 Candidate,诊断里既没有字段指针也没有实际文件路径,Worker 无从知道该改哪个字段。

**蓝图依据**:`L3-S9-repair.md:238`「字段缺失时保留原稿,反馈实际文件与字段」;`:198`「`migration_ref`、`rollback_ref` | 实际需要的已有依据;不适用时可为空字符串,不强制成对」;`:171`「`red_checks` 是保留的字段名,实际结果可为 `pass`、`fail` 或 `blocked`」;`L4-hook-acceptance-checklist.md:52`「缺字段指向本人实际路径与字段」。

**解法**:专业答卷按「声明即存在」核对 —— 凡结构体里声明为 string 的字段,原 JSON 中必须出现且不得是 `null`,空字符串仍表示「不适用」并被接受;不在结构体中声明的键按未知字段拒收;`red_checks` 的 `result` 取值限定 `pass|fail|blocked`。所有拒绝都以字段级错误(带 `/answer` 前缀的指针)返回,不吞字段、不转交作者重做,也不退化为整份答卷的笼统失败。

**具体改动**:
- `internal/workpacket/repair_result.go:297` — 宽松解码 `decodeOneJSON` 换成严格解码 `decodeOneStrictJSON`(定义在 `internal/workpacket/review_result.go:540-551`),让未知键与类型错配立刻变成 `/answer` 字段错误;`decodeOneJSON`(`internal/workpacket/review_result.go:528-538`)保留给仍需宽松语义的调用点。
- `internal/workpacket/repair_result.go:295-341` — 解码后补一次原 JSON 形状核对:对声明为 string 的 `verdict`/`migration_ref`/`rollback_ref`,若原 JSON 中缺失或为 `null`,返回 `/answer/<字段名>` 字段错误;空字符串合法,不得据此拒收;数组字段(`before_fix_results`/`unit_results`/`detection_gap_artifacts`/`evidence_refs`/`scope_deviations`/`residual_risks`)不做存在性要求。
- `internal/workpacket/repair_plan_report.go:273` — 同样换成 `decodeOneStrictJSON`。
- `internal/workpacket/repair_plan_report.go:271-281` — 补 `red_checks[i].result ∈ {pass, fail, blocked}` 的枚举核对,指针 `/answer/red_checks/<i>/result`;保留现有 `/answer/plan` 空值检查不变;`plan` 同样适用「不得为 null/缺失」规则。
- `internal/workpacket/repair_result.go:267-270` — `issue()` 生成的 Repair 文案仍写「填写 answer …」(G06-B 要翻译的旧信封文案),本组只保证 Pointer 一律带 `/answer` 前缀。

**验证**:
- 定向:`TestNativeS9PublicAuthorCorrectsRejectedResult`、`TestNativeS9FlatValidationFeedbackUsesPrivateFileFields`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestNativeS9PublicAuthorCorrectsRejectedResult|TestNativeS9FlatValidationFeedbackUsesPrivateFileFields'`。

**风险**:
- 「缺失」这一半比「null」严:只写部分键的手写私人答卷会被拒。框架自身写出的私人文件走 `persistRepairLeadQuestionnaire`(`internal/cli/repair_lead_questionnaire.go:937-943`)直接 marshal 结构体,九个字段与三个 PlanReport 字段都带 `json` tag 且无 `omitempty`,必定齐全;外部/历史手写样本若缺 `migration_ref` 会被拒 —— 这是「声明即存在」的代价,应与蓝图 `:198` 的「空字符串可用」保持一致,不得放宽成「缺失也接受」。
- 严格解码会让 `before_fix_results` 之类嵌套结构里多余键也报错;仓库内唯一手写原始 JSON 样本(`internal/runtime/repair_result_blocker_reader_test.go:32`)与 `RepairResultBeforeFixResult`(`internal/workpacket/repair_result.go:107-111`)兼容。
- 本组不单独让测试转绿:字段指针必须带 `/answer` 前缀,才能被 G06-B 的映射器翻译;若本组直接产出 `/migration_ref`,G06-B 的 `else { continue }` 会跳过,诊断重新漏出「填写 answer」。

### G06-B — 扁平私人文件字段反馈映射未接线,nativeS9FlatArtifactError 成为死代码

**问题**:S9 消费点把原消费者的字段错误原样抛出,诊断里出现旧信封指针「/answer」与「填写 answer 中…」文案,既没有本人私人文件的实际路径,也没有扁平答卷里的字段指针,Worker 只看到「S9 answer /answer: …」。

**蓝图依据**:`L4-hook-acceptance-checklist.md:52`;`L4-agent-dispatch-governance.md:167`「原字段错误沿接收器传递」;`L3-S9-repair.md:238`。

**解法**:原消费者的字段错误原样保留其成因,但在扁平私人文件边界翻译成「实际文件路径 + 扁平字段指针 + 本人可执行的修改指引」;不重写原消费者的判定、不换字段名、不把工作转交作者重做。

**具体改动**:
- `internal/cli/native_s9.go:514` — `return err` 改为 `return nativeS9FlatArtifactError("PlanReport", filepath.Join(source, plan), err)`(`plan`/`source` 是同函数 `:43`/`:25` 的作者相对路径与根目录)。
- `internal/cli/native_s9_original.go:110` — `return err` 改为 `return nativeS9FlatArtifactError("Result", filepath.Join(source, result), err)`。
- `internal/cli/native_s9_artifact_feedback.go:18-35` — 映射器本体不改(死代码只因无调用点):确认 `:25-28` 把 `/answer`→`/`、`/answer/*`→`/*`,`:32` 的 Repair 文案保留「keep professional fields at the top level」与实际路径,`:34` 保留原错误在链上。

**验证**:
- 定向:`TestNativeS9FlatValidationFeedbackUsesPrivateFileFields`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestNativeS9FlatValidationFeedbackUsesPrivateFileFields'`。

**风险**:
- 接线后诊断里会同时出现「actual native Result at <path>」标签与扁平指针,任何依赖旧「S9 answer /answer」字样的现存断言或日志解析会失配;改前需确认没有别处按 `/answer` 前缀做文本匹配。
- 该测试的 forbidden 列表含「/answer/」「 /answer:」「填写 answer」「professional Result received」,接线只解决前三项;第四项来自 `internal/cli/native_s6_received_guidance.go:28`(S6 专用投影),若扁平答卷被错误接收后触发 S6 路径,说明 G06-A 未生效,两组必须一起验。
- `nativeS9FlatArtifactError` 只在错误是 `*workpacket.AnswerError` 且 `len(Fields)>0` 时才翻译(`:19-22`);来源/Writer 级错误原样返回,这是预期行为,不要为统一文案吞掉因果链。

### G01-B — 完成度判定忽略真实 unit_results[].status,只信顶层 verdict + 硬编码 pass

**问题**:Worker 交的 Result 里 `unit_results[0].status="fail"`,系统仍把它投影成 `work_complete:true` / `responsibility_status:"candidate_accepted"`,Main 被当成只剩「集成义务」,`workspace integrate` 直接放行,未完成的单元被当成已完成。

**蓝图依据**:`L3-S9-repair.md:238`「正常保存、工具成功、Stop 或 completed 字样本身不等于完成」;`:195`「`unit_results` | 原 unit ID、真实 pass/fail/blocked」;`L4-revision-usage.md:141`「Runtime revision 只是审计坐标,不证明专业正确」。

**解法**:完成度只从本人答卷的真实单元状态得出:顶层 `verdict=completed` 只说明作者声明完成,必须同时所有 `unit_results[].status` 为 `pass` 才算本人工作完成;否则如实投影为「已接收但未完成」,并把具体未完成单元登记为待办,由 Main 按原遗漏继续安排;不得替本人把未完成说成完成,也不得以任何散列/版本/轮次作为完成或拒收条件。

**具体改动**:
- `internal/repair/issued_repair_result.go:696` — 提案里写死的 `Result: "pass"` 改为从本次真实单元状态推导(全 `pass` → `pass`;有 `fail` → `fail`;否则有 `blocked` → `blocked`),取值落在 `repair-result.schema.json` 已有枚举 `pass|fail|blocked|scope_changed` 内;同函数 `:662-680` 已有的 `units` 构造就是真实单元来源,直接复用。
- `internal/repair/issued_repair_result.go:820-822` — `RepairResultWorkCompleteWithContext` 保持「verdict != completed 或 Result != pass 即未完成」的判定式不变,改的是提案里的 `Result` 不再恒为 `pass`;不引入散列/轮次判定。
- `internal/repair/issued_repair_result.go:946` — 恢复守卫调用点:`provenance := *candidate.Provenance` 之后调用 `ensureRepairResultCompleted(provenance.SubmittedAnswer)`(`internal/repair/issued_repair_result.go:566-580`,当前无调用点的死代码),使最终 Result 不得覆盖未通过的单元;只读本人原始答卷,不比较任何散列。
- `internal/repair/issued_repair_result.go:994` — 最终 Result 的 `proposal.Verdict, proposal.Result = "completed", "pass"` 同样受该守卫约束,确认守卫位置早于此处生效。
- `internal/submission/repair_result.go:528-538` — `result.WorkComplete` 由 `RepairResultWorkCompleteWithContext` 得出后,`:535-538` 的「本人已完成、Main 接手集成」分支只在真完成时走;`:539-584` 的未完成分支负责登记 `WaitingOn`(如 `repair unit <id>: fail`)与「无需重复提交已接收内容」,该分支已是正确表达,本组不改文案。
- `internal/cli/native_main_return.go:604-609` — `receipt.ResponsibilityStatus == "candidate_accepted" && receipt.WorkComplete` 的分支在未完成时不再把 NextAction 换成「已完成」文案,保持回执自身给出的未完成 NextAction;不改判定条件,只依赖上游 `WorkComplete` 变正确。
- `internal/cli/s9_handoff_projection.go:342-351` — `s9ObservedCandidateProgress` 不能只看顶层 `verdict`;需一并读 `unit_results[].status`,任一单元非 `pass` 即返回 `s9CandidateUnfinished`,只有全部 `pass` 才返回 `s9CandidateComplete`,从而不再让 `s9ObservedHandoffAction`(`:378-400`)给出「已完成」的 Main 义务文案。
- `internal/cli/workspace_delivery.go:122-130` — 集成门禁继续以 `RepairResultWorkCompleteWithContext` 为准(未完成即拒收并输出「actual professional work remains unfinished」),本组不改门禁位置,只让判定值变正确。

**验证**:
- 定向:`TestS9HandoffPartialCandidateDoesNotBecomeIntegrationDuty`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestS9HandoffPartialCandidateDoesNotBecomeIntegrationDuty'`。

**风险**:
- 恢复守卫后,任何以「verdict=completed 但含非 pass 单元」的 candidate 去写最终 Result 的路径都会被拒;仓库内其它现存测试若依赖这种写法会开始失败,需逐条复跑 `internal/repair` 与 `internal/cli` 的 S9 用例。
- `unit_results[].status` 的取值域必须与 `workpacket` 侧一致(`pass|fail|blocked`,见 `internal/workpacket/repair_result.go:317-319`);出现第四种值时应落到「未完成」而不是「完成」。
- 推导规则不能反向放宽:空 `unit_results` 不得被当成「全部 pass」。当前 `ensureRepairResultCompleted`(`:574-578`)对空单元是放行,若单元为空即视为完成,会重新引入「没有真实单元也判完成」的漏洞。
- 与 G06-A 叠加:G06-A 只能拒「字段形状错误」,不能拒「单元失败」,否则 candidate 不生成,G01-B 的测试前提(`DeliveryRef` 非空)失效。

### G03-A — Builder 回执的原领域投影被删成桩,checks_source 由 original_report 改成 main_checkpoint

**问题**:S6 Builder 的 `task-receipt` 返回 `WaitingOn:[{Owner:main,Reason:assignment_unavailable}]` 的空桩,本人是否交付、是否被接收、是否完成、本人自检来源全部丢失,Main 无从知道 Builder 到底做了什么、哪一步还没做完。

**蓝图依据**:`L4-worktree-governance.md:174`「Worker 按 TASK／修复责任完成真实自检,并如实给出命令与结果。Main 的集成检查验证合并后的实际树;二者不能互相代替」;`:178`「Worker 仍按真实 TASK 自检,Result 如实引用」;`:182`「S6 Main 检查消费原 Record 及其 Main CheckProfiles,按原领域配置记录所检提交和回执」;`L3-S6-build.md:242-243`「Builder 本人检查 | 本人改动和任务条件 | 实际命令、受测内容、结果、未执行项及原因」;`:246`「Main 的重跑不替 Builder 自测,Builder 的通过也不代表合并树通过」;`L4-workflow-convergence.md:138`。

**解法**:legacy(非 canonical)Builder 回执恢复原领域投影:本人自检来源如实标为「本人完成报告」,Main 检查点来源只出现在 canonical 侧;交付、接收、完成、待办、等待项全部由原责任坐标推出 —— 注册关系、交付事实、当前活动状态、原 TASK 文档是否存在、本人完成报告信封里的任务/作者/主题是否对得上、Main 检查点里的责任/任务/世代/报告路径是否对得上。判定只看这些结构性事实;散列、指纹、版本、轮次一律不算、不比较、不登记。

**具体改动**:
- `internal/qualitygate/builder_receipt.go:93` — legacy 分支的 `ChecksSource` 从 `"main_checkpoint"` 改回 `"original_report"`(与同文件 `:66-69` 既有写法一致),不再让 Main 检查点冒充本人自检来源。
- `internal/qualitygate/builder_receipt.go:79-96` — 用原领域投影替换桩:依次判定注册关系(唯一 task、唯一 agent、`agent.prompt_ref` 对应 assignment、`task.owner_agent_ids` 含 owner、`agent.task_ids` 含 task)、交付事实(`req.CompletionRef` 与 `task.completion_report_ref`/`agent.completion_reported_ref` 一致才置 `DeliveryRecorded`)、活动状态(`workstate.TaskActivity`/`workstate.AgentActivity`,`internal/workstate/activity.go:141-142`)、当前 TASK 文档(`currentDocuments` 唯一命中,`internal/qualitygate/evaluator.go:1154-1176`)、本人完成报告信封(`evidenceEnvelope` 的 `TaskID`/`ProducerAgentID`/`SubjectRefs` 与原文档 path/version 对齐);据此产出 `Accepted`、`ResponsibilityStatus`(`not_submitted`→`accepted_incomplete`→`complete`)、`DomainRefs`、`OwnPending`、`WaitingOn`。
- `internal/qualitygate/builder_receipt.go:79-96` — 散列一律不算:不比较 `sha256Hex(data) != doc.SHA256`、不比较证据行的 `sha256`、不比较 `cp.ReportSHA`;`documentFact`(`internal/qualitygate/evaluator.go:257-265`)与 `subjectRef`(`:267-270`)在 d0d948a 后已无散列字段,按 path/version 判主体即可;原来源是否存在(文件可读、信封可解析、checkpoint 责任/任务/世代/报告路径匹配)才是判定依据。
- `internal/qualitygate/builder_receipt.go:79-96` — 集成判定改用 `internal/integration` 的 `Checkpoint` 结构字段(`AssignmentID`/`TaskID`/`BaselineGeneration`/`CompletionReportPath`/`State`,`internal/integration/types.go:28-41`),沿用 `SubmitIssuedRepairResult` 已有的判定式(`internal/repair/issued_repair_result.go:955` 的 `integration.StateVerified|StateAcknowledged|StateCleanupPending|StateComplete`),不引入任何散列比较;`builderbatch.VerifiedCheckpointState`、`qualitygate.exactSubjects`、`failingEnvelopeChecks` 已被 d0d948a 删除,需用现结构字段重写。
- `internal/qualitygate/builder_receipt.go:12-14` — `BuilderReceiptRequest` 补回 `RequiredChecks []string` 与 `DoneWhen []string`,供 legacy 侧判「本人检查缺失」「缺收口条件」。
- `internal/cli/task_receipt.go:62` — 把 `a.RequiredChecks`/`a.DoneWhen` 传回 `qualitygate.BuilderReceiptRequest`;这两个字段在 `hookctx.AssignmentContext` 仍存在(`internal/hookctx/types.go:48`、`:52`),legacy 与 canonical 行都已填充。
- `internal/qualitygate/canonical_builder_result.go:397` — canonical 分支的 `"main_checkpoint"` 保持不动;两处字面量必须由「本人自检来源 / Main 检查点来源」显式区分,不要继续共用同一个未加注释的字面量。

**验证**:
- 定向:`TestActualMainCheckBusyObservationDoesNotStartAnotherIntegration`、`TestActualMainIntegrationBarrierCleanupAfterEarlyReturn`、`TestCheckProfileActualMainIntegrationCapturesOriginalResponsibility`、`TestBuilderConventionsOwnedExampleSurvivesActualVerifiedIntegration`、`TestConfiguredPressureOriginalReportWriterRegistrationAndActualMainIntegration`、`TestBuilderReceiptOriginalFailureReworkAndCurrentResubmission`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestActualMainCheckBusyObservationDoesNotStartAnotherIntegration|TestActualMainIntegrationBarrierCleanupAfterEarlyReturn|TestCheckProfileActualMainIntegrationCapturesOriginalResponsibility|TestBuilderConventionsOwnedExampleSurvivesActualVerifiedIntegration|TestConfiguredPressureOriginalReportWriterRegistrationAndActualMainIntegration|TestBuilderReceiptOriginalFailureReworkAndCurrentResubmission'`。

**风险**:
- 当前 6 条测试全部在第一个回执断言处致命失败,后续断言不可达;恢复投影后可能暴露此前从未执行过的断言(如 `WorkComplete=true`/`Integrated=true`、`DomainRefs[0]` 指向真实文件路径、返工后 `ResponsibilityStatus=="not_submitted"`),必须逐条重跑到全部断言通过,不能只看首条转绿。
- `WaitingOn.reason` 与 `ResponsibilityStatus` 的取值必须落在 schema 允许的枚举内(`internal/schema/assets/builder-receipt.schema.json`:`waiting_on.reason` 含 `integration_pending`/`assignment_unavailable`/`integration_receipt_invalid`/`task_input_changed`/`result_evidence_invalid`;`responsibility_status` 含 `not_submitted`/`invalid`/`accepted_incomplete`/`complete`;`checks_source` 同时允许 `original_report` 与 `main_checkpoint`)—— schema 不用改。
- 恢复的是 legacy 侧,canonical 侧(`canonical_builder_result.go:397`)不得跟着改成 `original_report`,否则破坏 canonical 路径「Main 检查点来源」的语义;若两边共用同一常量,改一侧必然连带另一侧。
- `internal/qualitygate/builder_receipt.go:22-23` 的既有契约注释「Check results retain their original report source.」与 `:93` 直接冲突,本组正是消除这个冲突;注释不需要改。

---

## 3. D5 组落地方案(门禁降级、静默吞事件与 S6/S7 链)

### G10-A — hook 入口静默吞掉无法归属的事件(非 Main/Worker),不报「未知」

**问题**:载荷带未登记的 `agent_id`(或 payload 无 `agent_id`)时,`hookReaderObservation` 直接 `return true, 0`,stdout/stderr 全空,调用方看不到任何「这条事件我不知道是谁发的」的事实,事件被当成已处理。

**蓝图依据**:`L4-hook-acceptance-checklist.md:52`「C03:责任由已有任务与登记坐标准确定位,不跨根、owner、责任或轮次借卷,不按角色、最近文件、唯一候选或目录扫描猜选;缺字段指向本人实际路径与字段,未知就报告未知」;`L4-workflow-convergence.md:250`。

**解法**:无法归属到 Main 或已登记 Worker 的自然事件,必须产出一条显式的「未知」诊断(写 stdout),指明事件名、平台携带的身份字段、以及缺失的登记事实;不得静默返回成功,也不得以任何散列/修订号判定归属。原责任坐标只按已登记事实确定,不得用扫描或唯一候选猜选。

**具体改动**:
- `internal/cli/hook_reader.go:208-210` — 把 `if reader.Kind != "Worker" { return true, 0 }` 改为:产出「未知归属」诊断后返回 `(true, 0)`(不改变退出码),诊断内容取 `in.Event`、`in.EffectiveAgentID()`、`reader.Kind`,并说明缺哪一个登记事实(已登记 Agent / 当前 Assignment 记录)才可归属。
- `internal/cli/hook_reader.go:203-207` — 复用已有 `reader.SelectionIssue` 通道承载该诊断文案,避免新增第二条诊断路径;`reader.Kind == "Main"` 的早退保持不变。
- `internal/cli/run.go:2693` — 不改调用点;`hookReaderObservation` 的 `(true, 0)` 语义保持「已处理但无 Controller 动作」,由新诊断承担可见性。
- `internal/cli/ct_11_24_test.go:418-425`(本轮已按 STALE 适配为 `MainPreToolUseBody(root, …)`)—— 不回退。

**验证**:
- 定向:`TestCT03911_DualPassDocumentVerificationCommitsTR003`、`TestCT03915_S11StopsAutoAdvance`、`TestCT03920_BuilderCompletionDurableRef`、`TestCT03924_SameAgentDualDVLabelsBlocksTR003`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestCT03911_DualPassDocumentVerificationCommitsTR003|TestCT03915_S11StopsAutoAdvance|TestCT03920_BuilderCompletionDurableRef|TestCT03924_SameAgentDualDVLabelsBlocksTR003'`。

**风险**:
- CT-039-11/15/24 的夹具原用 `PreToolUseBody`(`agent_id: agent-1` 未登记),本轮已判 STALE 并把夹具改为 `MainPreToolUseBody`;**不要**顺带把 `hook_reader.go:208-210` 改成「按 payload 强制当 Main」——那会违反 C03 的「不按角色猜选」。
- CT-039-20 断言 stdout 含 `completion` 或 `integration`,因此「未知」诊断必须引用载荷里真实存在的原责任坐标(assignment_id / completion_ref),而不是只说「不认识」;退出码必须仍为 0。
- 本组位置与 D6 的 G09-D(a) 是同一处(`hook_reader.go:208-210`),需合并为一次改动。

### G08-A — S7 Stop 接收被「已接收 ⇒ return true, 0」早退吞掉,TR-009 出口不触发

**问题**:S7 的 Stop 事件命中「该责任已接收」分支后被静默 `return true, 0`,原消费者诊断(`… received by the original consumer`)永不产生,TR-009 的 S10 出口也不准备,生命周期停在 `verification`。

**蓝图依据**:`L3-S7-verification-round.md:384`「没有实际 Finding、没有未完成或 blocked 的必需责任,且原结论明确完成时,沿 TR-009 自动进入 S10 验收与审计」;`L4-workflow-convergence.md:437`「适用必要责任完成且无 Finding 时经 TR-009 准备 S10」。

**解法**:「已接收」只表示不重复接收,不表示无事可做。该分支必须仍产出原消费者诊断并继续走 S7 出口准备;是否已准备出口由「原阶段是否正确 + 后继是否待办」这一结构性事实判断,不用修订号/散列/轮次判定。

**具体改动**:
- `internal/cli/native_review_hooks.go:358-361` — 把 `if stage != "S7" || !s7SuccessorPending(root, snap.State) { return true, 0 }` 改为:不再静默返回;「不重复接收」通过跳过 `SaveReviewResultFrom` 实现,随后仍打印原消费者诊断并落入 `:441` 的 S7 出口块;`s7SuccessorPending` 只保留为「出口是否已准备」的幂等护栏(用于 `s7_issued_result_external_test.go:147` 的重放不写 Runtime 断言)。
- `internal/cli/native_review_hooks.go:362-370` — 「已接收」分支里 `received = receipt` 之后补上与 `:396` 同形的原消费者诊断(阶段前缀 + `receipt.NextAction`)。
- `internal/cli/native_review_hooks.go:308-310` — `len(selected)==0` 时不得静默 `return false, 0`;至少要把「未能定位到原责任」作为可见诊断交给下游。
- `internal/cli/native_review_hooks.go:267-284`、`internal/cli/native_review_hooks.go:553-569` — S7/S8 的文本来源目前只读 transcript,平台载荷里的**已登记原答案路径**到不了候选选择,于是 `selected` 为空、事件进不了 S7 路由;必须让「载荷中出现的、指向已登记答案的精确路径」参与定位(与 D6 的 G09-D 同一处)。

**验证**:
- 定向:`TestS7IssuedResultPacketUsesOriginalDispatchAcceptanceReceiptAndCleanRound`、`TestS7IssuedResultColdHookProcessesUseOriginalAcceptanceReceiptAndCleanRound`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/integration/ -run 'TestS7IssuedResultPacketUsesOriginalDispatchAcceptanceReceiptAndCleanRound|TestS7IssuedResultColdHookProcessesUseOriginalAcceptanceReceiptAndCleanRound'`。

**风险**:
- **本组单独落地不足以让测试转绿**:实测 stdout/stderr 全空的直接原因是 `native_review_hooks.go:555` 只读 transcript → `selected` 为空 → `:308-310` 静默 declines → `hook_reader.go:208-210` 吞掉;早退 `:358-361` 只是第二层吞点。必须与 D6/G09-D(`native_review_hooks.go:555`)一起合入。
- `s7SuccessorPending` 判定用的是 `lifecycle.state==verification && phase∈{clean,observation_sealed}`;误改成「总是早退」会打破重放断言(`s7_issued_result_external_test.go:147` 要求 Runtime 不变),完全删掉护栏则重放会重复触发 TR-009。
- `reviewPacketAppliesToCurrent` → `review.IssuedResultAppliesToCurrent` 是内容/身份比较,符合蓝图;**不要**改成散列比较。

### G08-B — 原责任未完成/未知的 Stop 检查从 deny 降级为 warn

**问题**:S7/S8/S9 的 SubagentStop / TeammateIdle 事件遇到「原责任未完成」或「原责任绑定未知」时只给 warn,平台不会阻止本次收尾,未完成的原责任被静默放过。

**蓝图依据**:`L4-hook-platform-wiring.md:162`「删除旧 TeammateIdle 和固定 execution 的收尾接线……`stop_hook_active` 防循环不授予完成资格」;`L4-workflow-convergence.md:225`「S7/S8/S9 共用 Pre 观察、成功 Post 和 Stop 兜底的接收语义;只定位当前责任与原卷、返回接收或具体缺项」。

**解法**:收尾检查的判据是「原责任是否真的已完成/是否可归属」这一原责任事实。原责任未完成或绑定不可确定时,必须给出拒绝性决定并附恢复路径,而不是一个不阻断的提醒;是否完成只看原收据/原责任是否在场,不比较修订号、散列或轮次。

**具体改动**:
- `internal/hook/stopidle.go:144-157` — 把新增的 `Decision: "warn", RuleID: "original_responsibility_unfinished"` 分支改回拒绝性决定(`deny`),理由文本仍说明「原责任未完成、Main 需按实际待办工序处理」,并补 `Recovery`(继续该原责任的原 PLAN/Result 问卷路径)与 `Retry: policy.RetryAfterRecoveryValidation`,使平台在保存后重试而不是直接放行。
- `internal/hook/stopidle.go:678` — `s7_original_responsibility_unknown` 的通知改回 `deny`,`RuleID` 恢复为 `RuleSubagentStopMissingResult`,并补 `Recovery` 与 `Retry`。
- `internal/hook/stopidle.go:686` — 同上,`RuleID` 恢复为 `RuleTeammateIdleResumeAssignment`。
- `internal/hook/stopidle.go:694` — S9 分支同样恢复为 `deny`(`RuleID` 沿用父提交 `reviewAssignmentBindingBlock`/`hypothesisResultAssignmentBlock`/`repairResultAssignmentBlock` 的取值)。
- `internal/hook/stopidle.go:158-166` — 保留 `stopIdleBlock` 作为通用拒绝路径;若恢复后出现两套拒绝文案,以 `stopIdleBlock` 的阶段化为准。

**验证**:
- 定向:`TestS7LifecycleListenerProjectsPathsWithoutCompletingResponsibility`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestS7LifecycleListenerProjectsPathsWithoutCompletingResponsibility'`。

**风险**:
- `internal/hook/idle_budget.go:19` 的 TeammateIdle 重试预算按 `decision.RuleID == RuleTeammateIdleResumeAssignment` 计数;若恢复 deny 却保留新造的自定义 RuleID,空闲预算会失效、平台可能无限重试。RuleID 必须回到父提交取值。
- `Retry` 与 `Decision` 必须配套:保留 `Retry: "not_applicable"` 同时改成 `deny` 会让平台既阻断又不重试,与「拒绝后按恢复路径续接」矛盾。
- 本组不得引入任何修订号/散列比较;「原责任未完成」只按原收据是否在场判断(`CurrentResponsibility(...).ExecutionMayEnd` 已提供该事实)。

### G08-C — S7 指引只给 `s7 manifest-draft`,缺原 `register-workgroup` 入口;static/behavior 波次门被删

**问题**:S7 指引对已存在的(被编辑过的)专业 manifest 找不到实际路径,只打印一条 `s7 manifest-draft` 命令,Main 拿不到原 `runtime register-workgroup` 入口;另外 behavior 波次的 Assignment 在必需静态 Claim 未结论时被当成可派发,给出了 manifest 准备指令。

**蓝图依据**:`L3-S7-verification-round.md:205`「原计划接收后,系统为已声明且已解锁的原 Assignment 提供准确的 manifest 准备路径和原 `register-workgroup` 入口……不要求 `review.round`、轮次计数、SHA／hash、Git OID 或额外作者字段;历史摘要不计算、复制、登记或认证为新材料的资格」;`L4-workflow-convergence.md:437`。

**解法**:指引必须同时给出「准确的 manifest 准备路径」与「原 register-workgroup 入口」,并用该 Assignment 的实际 REQ/TASK 路径与业务版本;登记入口只按已登记事实给出,不要求轮次计数,不校验 manifest 里的历史 sha256。派发前置必须恢复 static/behavior 两波次序:必需静态 Claim 未全部结论时,behavior 波次 Assignment 只能报告「等待」,不得进入 manifest 准备。

**具体改动**:
- `internal/cli/s7_guidance.go:111-113` — manifest 路径解析不能只认新 identity 目录;除现有路径外,再尝试该 Assignment 的历史登记布局 `.claude/workgroups/manifest-g<generation>-r<round>-<id>.json`(文件名中的代次/轮次只作定位约定,不作通过或拒收条件)。
- `internal/cli/s7_guidance.go:120-122` — 命中既有 manifest 时,继续输出 `s7ManifestRegistrationGuidance` 的原 `runtime register-workgroup` 命令(该函数在 `internal/cli/s7_manifest_draft.go:178-199`),并保持「不覆盖专业已编辑的 manifest」;命中判定只用 `readS7ExistingManifest` 的内容事实(runtime_id、baseline generation、原计划身份、claim 集合、lens)。
- `internal/cli/s7_guidance.go:127-128` — 未命中既有 manifest 时,除 `s7 manifest-draft` 外也给出原 `runtime register-workgroup` 入口的形状。
- `internal/assignment/review_readiness.go:22-37` — 在 `reviewDispatchPrerequisite` 内恢复 behavior 波次前置:该 Assignment 的 `ExecutionWave == "behavior"` 且 `review.StaticClaimsSettled(state, plan)` 为假时返回等待理由(依据 `internal/review/plan.go:194`,该函数目前无生产调用者)。
- `internal/cli/s7_guidance.go:71-75` — 等待分支已有 `S7 assignment=%s waits: %s` 文案,恢复波次门后即可命中,不改文案。

**验证**:
- 定向:`TestS7DispatchGuidanceUsesActualPrerequisitesAndSource`、`TestS7ExistingEditedManifestRecoversRegistrationWithoutOverwrite`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestS7DispatchGuidanceUsesActualPrerequisitesAndSource|TestS7ExistingEditedManifestRecoversRegistrationWithoutOverwrite'`。

**风险**:
- `readS7ExistingManifest`(HEAD)在 `plan.Revision > 1` 时走 `review.ManifestAssignmentUnchangedSince`(内容比较,注释已声明不计算历史摘要);**不得**新增轮次或 sha256 比较。测试夹具里的 manifest 带有 `"sha256": "bbb…"`,按本轮约束只能原样读取、不得重算、不得作为资格。
- 路径定位若改为「目录扫描挑唯一 manifest」会违反 C03;必须按已登记布局定位。
- 恢复波次门会让当前正在跑 `s7` 的 Main 看到新的「waits」输出,属预期行为变化,但依赖「manifest-draft 总是被打印」的既有断言会受影响。

### G09-A — TR-006 当前批次复检失去 S6 Main 起点校验,缺原 TR-003 共同 Main 基线时门被放行

**问题**:缺少原 TR-003 共同 Main 基线的 Runtime 仍能通过 `runtime task-integrate`,TR-006 被提交、批次被判为集成完成,而不是报「没有原 TR-003 共同 Main 基线」。

**蓝图依据**:`L3-S6-build.md:46`「正常入口为 `building`,前提是 TR-003 已实际提交」;`L4-revision-usage.md:141`「Runtime revision 只是审计坐标,不证明专业正确」。

**解法**:TR-006 的当前批次复检必须以「当前代际存在原 TR-003 共同 Main 基线」为前提;这个前提是结构性事实(原阶段正确、原来源存在),不是修订号比较、不是散列、不是轮次计数。缺失时按准确事实报错给 Main,不得以「历史起点可推断」放行。

**具体改动**:
- `internal/buildsurface/read.go:23` — `ReadContext` 在进入当前批次复检前,恢复对原 TR-003 共同 Main 基线的要求:读取该 Runtime 的事件日志后调用 `controlproof.RequireCurrentS6MainStartContext`,失败即返回「当前代际没有原 TR-003 共同 Main 基线」这一事实性错误(文案沿用 `internal/controlproof/s6_main_baseline.go:196`)。
- `internal/transition/engine.go:508-512` — 不改调用点;TR-006 通过 `buildsurface.ReadContext` 取当前批次,恢复基线校验后该 transition 自然在提交前失败。
- `internal/controlproof/s6_main_baseline.go:176-196` — 保持为唯一实现点,不新增第二套判定。

**验证**:
- 定向:`TestS6IntegrationWithoutMainStartKeepsOriginalBatchGate`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestS6IntegrationWithoutMainStartKeepsOriginalBatchGate'`。

**风险**:
- `RequireCurrentS6MainStartContext` 内部(`internal/controlproof/s6_main_baseline.go:154-158`)会比对原 TR-003 事件自身的 before/after revision 与 sequence。按本轮「revision 不得作为门禁」的严格口径,这属于修订号比较;更保守的写法是只取「原 TR-003 转移存在 + 转移身份 + 阶段游标正确」三条结构事实,并**不要**顺带恢复父提交 `internal/buildsurface/read.go:61` 的 `ValidateAssignmentRevisionReceiptsContext`(按 `record.Revision > 1` 判定,更接近修订号门禁)。
- 该校验会被 `buildsurface.Read` 的所有调用方继承;若别处依赖「无基线也能读当前批次」的旧行为,需一并评估。
- 本组与 G09-B 同在 S6 接收链,但一处管「起点是否存在」、一处管「写域是否越界」,不要合并成同一次改动。

### G09-B — 原生 S6 接收链缺少 planned_paths 的 Assignment 写域校验

**问题**:原生 Stop 接收路径把专业 PLAN 的 `planned_paths` 直接并入 PLAN 并发布,随后完成写入只核身份绑定、不核写域,越界路径(`outside/proof.txt`)照样被接受并完成收尾,退出码 0。

**蓝图依据**:`L3-S6-build.md:188`「| `planned_paths` | 实际准备改动的产品或测试位置 | 必须落在登记范围,不能扩张 TASK |」;`L4-workflow-convergence.md:250`。

**解法**:原生接收链与其它接收者用同一把尺子:专业 PLAN 的每个 `planned_paths` 必须落在该 Assignment 的登记写域内;越界即以准确事实拒绝,不计算也不比较任何散列来判定是否越界。

**具体改动**:
- `internal/assignment/completion_canonical.go:682-699` — 在「原责任未完成」判定里补上写域校验:先按登记坐标解析该 owner 的当前计划引用(`plancheckpoint.ActivatedRef`),确认待写 PLAN 就是该原责任的那份,再调用 `plancheckpoint.Validate(root, snapshot, agentID, planRef)`,让 `validateCanonicalPlan` 的登记范围检查(`internal/plancheckpoint/validate.go:224-229`)生效。
- `internal/assignment/completion_canonical.go:626` — `resolveCanonicalResultBinding` 返回结构里的 `PlanSHA`/`RecordSHA`(`:618`、`:620`)是上一版散列门禁的残留,**不得**作为资格条件;建议同时删除这两个字段与相关赋值,避免下轮被当成「已有散列门禁」。
- `internal/assignment/completion_canonical.go:243` — 校验必须发生在 Writer 事务之前,保证越界被拒时 Runtime 修订号不变(测试断言 `Revision` 不变)。
- `internal/cli/native_s6.go:502`、`:543`、`:563` — 原生链发布的 `received-plan.json` 必须就是被校验的那份原计划(同一路径),并在 `CompleteTaskWithReceipt` 之前完成校验;`internal/assignment/completion_canonical.go:411` 负责把该引用登记为该 agent 的 `plan_reported_ref`,两者必须一致。

**验证**:
- 定向:`TestTaskScopeOriginalS4SaveThroughNativeAssignmentAndBrief`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestTaskScopeOriginalS4SaveThroughNativeAssignmentAndBrief'`。

**风险**:
- **不要**恢复父提交同位置的 `planSHA`/`RecordSHA256` 比较 —— 那是本轮要退役的散列门禁;只恢复身份绑定与登记写域两条结构事实。
- `plancheckpoint.Validate` 在非 canonical 分支会比对 `assignment_revision` 与 `ptr.Revision`(`internal/plancheckpoint/validate.go:69-89`);canonical 记录的 `assignment_revision` 比对是「PLAN 绑定当前原 Assignment 身份」,属原责任坐标而非专业正确性证明,应保留但需在评审中说明。
- 校验接在 completion Writer 上会同时影响原生与非原生两条完成路径;非原生路径若历史上依赖「不校验也能写」,需单独确认。

### G09-C — 集成 Inspect 只把脏工作树当瞬时观察,不进 gate/checkpoint

**问题**:Builder 工作树里有未提交/未跟踪材料时,Inspect 只把它写进一段不落盘的观察文本,集成检查点 `Blockers` 为空、`FailureReason` 只记 check 失败,Main 无法从检查点回答「是不是 dirty」,脏工作树也不构成保留现场的依据。

**蓝图依据**:`L3-S6-build.md:352`「| Inspect | 正确当前 Result、实际 source/target、工作区状态、提交、真实差异、锁定路径及冲突 | 原成果与具体未满足条件 |」;`L4-workflow-governance.md:168`「冲突与未提交修改保留现场。解决问题需要 Main 或原专业作者作实际判断,不能用 reset／stash／强制覆盖来把检查结果凑成成功。检查点记录已经发生的 merge,重试不能另造第二次相同合并」。

**解法**:工作区脏状态要成为可持久检查的事实,而不是瞬时提示:Inspect 读一次 Builder 工作树状态并把它记进检查点;不可观测(unknown)与脏(dirty)必须分开 —— unknown 不阻断也不记录为脏,dirty 则在保留现场的检查点里成为一条明确 blocker。判据是工作树里是否真有未提交/未跟踪材料,不比较散列、修订号或轮次。

**具体改动**:
- `internal/integration/types.go:97-98` — `WorktreeObservation` 保持瞬时(`json:"-"`,注释「never a gate or checkpoint field」需改写),另加一个可落盘字段承载工作区脏事实(如 `WorktreeDirty []string`,带 json tag),使「工作区状态」成为检查点字段而非提示。
- `internal/integration/inspect_responsibility.go:96-98` — 把 `observeWorktreeCandidates` 的一次读取结果同时分成两份:提示文本(维持现有措辞)与脏材料清单(写入新字段);不可观测的工作树不得产生脏条目。
- `internal/integration/inspect_responsibility.go:16`、`:36` — `block(...)` 目前只被 scope/locked/target 类失败调用;脏工作树不通过它直接阻断,而是通过检查点 blocker 落盘。
- `internal/integration/integrate_responsibility.go:198` — `next := cleanCheckpoint(current)` 之后,把 Inspect 观测到的脏材料转成一条 blocker 文案(用 `ErrDirtyWorktree` 的既有措辞,含 "dirty")写入 `next.Blockers`,使 `:213`、`:218` 两条 `preserveAfterCAS` 路径写出的保留检查点都带上该 blocker。
- `internal/integration/integrate_responsibility.go:155-160` — `!in.Ready` 的保留路径同样把 `in.Blockers` 复制进检查点 `Blockers`(目前只写进 `FailureReason`),并按 blocker 性质选择 `ErrDirtyWorktree` 而非 `ErrMergeConflict` 作为哨兵。

**验证**:
- 定向:`TestCanonicalS6WorkspaceReworkRequiresNamedAssignmentRevision`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestCanonicalS6WorkspaceReworkRequiresNamedAssignmentRevision'`。

**风险**:
- **不能把脏工作树直接做成阻断条件**:`internal/cli/s6_integration_observation_e2e_test.go:16`(`TestNativeS6IntegrationReportsWorktreeCandidatesWithoutGating`)要求带 staged/unstaged/untracked/renamed 的工作树仍然集成成功、`result.State == "integrated"`;`internal/integration/inspect_test.go:75` 与 `internal/integration/s6_worktree_observation_test.go:31-37` 同样要求只观察、不阻断、且观察文本不落盘。脏 → 阻断 会打破这三处;脏 → 记录进检查点 blocker 才是与蓝图一致的最小解。
- 检查点 `Blockers` 目前在保留路径上恒为空,只改 Inspect 不改 `integrate_responsibility.go` 无法满足断言。
- 测试断言重做必须以 `--revise-assignment` 形式拒绝,且不得出现 `--expected-record-sha256`;任何以 sha256 作为输入条件的实现都与本轮约束冲突。

---

## 4. D6 组落地方案(进度投影、授权门与旧接线摘除)

### G05-A — projectWorkProgress 不再投影 milestone 与责任范围,停滞诊断永不可达

**问题**:采样器永远拿不到「当前缺什么/该读什么」,因此重复读同一范围 5 次也不会产生任何 WORK PROGRESS 提示,影子记录里的 notice 恒为 null。

**蓝图依据**:`L4-procedure-reduction.md:193`「重复读取同一范围、只查询状态而缺项不变,可以形成停滞提示」;`:197`「当前相同缺口的重复 Read／status 达到五次时可产生一次诊断」;`L4-hook-anchor-catalog.md:148`「未知、等待、已完成或读取范围不足不能被强判停滞」。

**解法**:让 `projectWorkProgress` 重新投影两类结构性事实 —— Main 侧的当前 milestone(缺口事实 → Gap,声明的必要读范围 → Sources),Worker 侧的原责任坐标(当前 S5 责任 → Gap 与其实际输入范围 → Sources,以及责任自身是否已完成/在等接收方 → Waiting/Complete)。投影只用既有责任坐标、实际文件内容和 Runtime 阶段事实,不计算、不登记、不校验任何散列,不比较 Git OID,不以 revision 判定。

**具体改动**:
- `internal/cli/work_progress.go:236-240` — 删掉 Main 的无条件早退,改为按父提交 `d0d948a^:internal/cli/work_progress.go:544-578` 的 milestone 分支:读 `snap.State["milestone"]`;`milestone["lifecycle_state"] != life["state"]` 时置 `f.Known=false` 并返回(读取范围不足不得强判停滞);否则 `f.Gap = progressText(milestone["action"]) + ": " + progressJSON(milestone["missing"])`,`f.Waiting` 追加 `milestone["human_required"]==true`。
- `internal/cli/work_progress.go:236-240` — 同一分支内把 `milestone["read"]` 经 `internal/cli/projection.go:369` `resolveBoundREQRead` 解析后交给 `internal/cli/work_progress.go:111` `projectMilestoneReadSources` 投影进 `f.Sources`(目录型范围走 `internal/cli/work_progress_tools.go:145` `progressOwnedCode`,文件型走 `internal/cli/work_progress.go:40` `addProgressSource`;值只写 "present" 或实际内容,不写摘要)。
- `internal/cli/work_progress.go:241-261` — 把现在的「按 entities.agents 行匹配」扩成父提交 `d0d948a^:362-372` 的责任投影:用 `internal/hookctx/loader.go:253` `hookctx.FromSnapshot` 取 `PolicyContext.Agent`/`Assignments`,再用 `internal/hook/responsibility.go:168` `hook.CurrentResponsibility` 得到 `hook.ResponsibilityFacts`,并调用 `internal/cli/work_progress.go:24` `applyResponsibilityProgress`(现无生产调用者)。
- `internal/cli/work_progress.go:241-261` — 再补父提交 `d0d948a^:382-419`/`:446-478` 的 S5 责任分支:用 `runtime.FindWorkPacket` 找当前 `s5-document-review-v1` 包,`f.Gap` 取该责任的真实答复/字段缺口,`f.Sources` 用 `internal/cli/work_progress.go:62` `projectProgressSourceInputs` 投影 `current.Inputs`(根为 `execution.Path/.claude/inputs`)。
- `internal/cli/work_progress.go:241-261` — 补父提交 `d0d948a^:479-543` 的「有原 Assignment 的 Worker」分支:`f.Gap` 说明当前关闭契约未交付,`f.Sources` 投影 `own.ReadPaths`,`f.Waiting` 取报告状态与生命周期活动。
- `internal/cli/work_progress.go:227-229` — 保留 inactive/paused/bound_req 早退;在 `:229` 后补父提交 `d0d948a^:233-235` 的观察者身份守卫(`hookReaderKey` 的 Kind 既非 Main 也非 Worker → 事实保持未知),使未登记 actor 仍然沉默。
- `internal/cli/work_progress.go:224-262` — **不得恢复**以下父提交片段:`:305-361`(Git tree + `workpacket.Digest`)、`:421-445`(`workpacket.Digest(data) != row["sha256"]` 比对)、`:513`/`:527`(`progressProfessionalArtifact` 摘要)、`:521`(`execution.BaseCommit`)。

**验证**:
- 定向:`TestMainOrdinaryReadAndNonCheckpointStatusProgressWire`、`TestDefaultProgressShadowRecordsWithoutContext`、`TestS5WorkerReadProgressAndReceivedPeerWaitWire`、`TestMainS2S5MilestoneDirectoriesProjectActualReadSources`、`TestS7OptionalProgressUnknownNativeActorIsSilent`、`TestProgressProjectionIgnoresRevisionClockAndControlBytes`、`TestProgressIgnoresOtherTypedPacketsButKeepsS5Diagnostics`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestMainOrdinaryReadAndNonCheckpointStatusProgressWire|TestDefaultProgressShadowRecordsWithoutContext|TestS5WorkerReadProgressAndReceivedPeerWaitWire|TestMainS2S5MilestoneDirectoriesProjectActualReadSources|TestS7OptionalProgressUnknownNativeActorIsSilent|TestProgressProjectionIgnoresRevisionClockAndControlBytes|TestProgressIgnoresOtherTypedPacketsButKeepsS5Diagnostics'`。

**风险**:
- 父提交里 Worker/责任分支依赖 `hookctx.FromSnapshot` 的加载错误语义(缺 CWD 报 "registered owner/execution directory unavailable"、坏 agents 行报错);当前代码已丢掉这些错误,恢复时必须一并接回,否则 `TestS7OptionalProgressUnknownNativeActorIsSilent` 的四个子例仍红。
- `f.Semantic` 必须保持稳定(父提交把 `semantic["sources"]=f.Sources` 也算进去),否则重复计数每次都被重置,提示永不出现。
- 过度恢复 `f.Waiting`(例如把「有活动 Assignment 未报告」也算进去)会直接抑制提示,因为 `internal/hook/work_progress.go:195` 要求 `!facts.Waiting`;但 `internal/cli/work_progress.go:24` `applyResponsibilityProgress` 在 ConsumerPending 时置 Waiting,必须保证接收后场景仍能判定为已完成。
- 与 G05-B 共用同一函数,先合 G05-B 的早退/半已知修复再接本组投影。

### G05-B — actor 可解析前就写 f.Owner/f.Binding,S9 专业阶段早退被删

**问题**:未匹配的 platform actor 拿到「半已知」事实(Owner/Binding 已写、Known=false),S9 专业阶段也会写出进度缓存,采样器越权覆盖别人的责任。

**蓝图依据**:`L4-procedure-reduction.md:195`「它只在实际支持的成功 PostToolUse 与可解析责任上工作。未知观测……会使它不能充当全阶段活性判据」。

**解法**:事实必须在「责任可解析」之后才成立。未登记/不可解析的 actor、S9 专业阶段、以及非 Main/Worker 的观察者,一律返回完全未知的事实(Owner/Binding 皆空),不得落任何进度缓存;已知事实的 Owner/Binding 只在解析成功后才写。

**具体改动**:
- `internal/cli/work_progress.go:234-235` — 把 `f.Owner, f.RecoveryOwner = owner, "Main"` 与 `f.Binding = progressJSON(...)` 从「写 owner 之前」移到「责任解析成功之后」;在 `:229` 与 `:230` 之间插入解析步骤。
- `internal/cli/work_progress.go:230` — 在解析之前插入父提交 `d0d948a^:240-245` 的 S9 早退:`runtime.PacketEnabled(snap.State)`(`internal/runtime/work_packet.go:148`)&& `life["state"]=="bug_resolution"` && `s9ProfessionalPhase(progressText(life["phase"]))`(`internal/cli/s9_execution_listener.go:10`)→ 直接返回空事实,S9 的进度由本人专业消费者负责。
- `internal/cli/work_progress.go:253-255` — 未匹配到该 actor 时不要保留已写的 Owner/Binding:返回前把 Owner/RecoveryOwner/Binding 清空,保持 `Known=false` 的「完全未知」语义。
- `internal/cli/work_progress.go:236-240` — Main 分支保持 `f.Known=true` 与稳定的 Binding/Semantic,但不得把 S9 态当成普通 Main 采样(由上面的 S9 早退先拦截)。

**验证**:
- 定向:`TestProgressPacketS9SkipsLegacyCoverageWithoutCache`、`TestS7OptionalProgressUnknownNativeActorIsSilent`、`TestProgressProjectionIgnoresRevisionClockAndControlBytes`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestProgressPacketS9SkipsLegacyCoverageWithoutCache|TestS7OptionalProgressUnknownNativeActorIsSilent|TestProgressProjectionIgnoresRevisionClockAndControlBytes'`。

**风险**:
- 断言点很细:测试要求 `facts.Owner==""` 且 `facts.Binding==""`,而不只是 `Known=false`;清空动作必须发生在早退路径上,不能在 hook 层事后补偿。
- S9 早退依赖 `runtime.PacketEnabled` 与 phase 词表;phase 词表变动会让早退失效(回到「S9 也写缓存」的旧症状)。
- 与 G05-A 同一函数:早退位置在投影之前,顺序不能颠倒。

### G07-B — S9 接收路径 Result 侧用 json.Valid 短路,绕过原消费者字段校验

**问题**:作者把 Result 写成坏 JSON 时,只收到「未提交或未完成」,拿不到「哪个文件、哪一段 JSON 非法」;Plan 侧却能给出字段级作者错误,两侧不对称。

**蓝图依据**:`L3-S9-repair.md:238`「文件不存在、草稿未就绪或字段缺失时保留原稿,反馈实际文件与字段」;`:240`「Result 的真实 blocked／contract_conflict 可以独立先接收,不能因为 Plan 缺失、未就绪或存在非关键缺项而跳过 Result」。

**解法**:Result 侧与 Plan 侧对称 —— 文件存在但内容不是合法 JSON 时,按原消费者的校验口径判定,把「实际文件 + 原校验器给出的解析问题」作为作者责任反馈出去(原稿保留,不生成完成事实);文件确实不存在时才回到「未提交/未完成」。

**具体改动**:
- `internal/cli/native_s9_original.go:80` — 去掉 `resultErr == nil && !json.Valid(raw)` 这一路直接 `pendingResult()` 的短路;文件存在但字节不是合法 JSON 时,改走与作者错误同型的返回:用 `internal/workpacket/planning.go:94` `workpacket.ValidateUniqueJSON(raw)` 取得原校验器的解析问题,再用 `internal/cli/native_s9_artifact_feedback.go:11` `nativeS9ArtifactAuthorError("Result", filepath.Join(source, result), err)` 包成作者责任错误。
- `internal/cli/native_s9_original.go:81` — `os.IsNotExist(resultErr)` 保留为「未提交/未完成」分支(文件不存在 ≠ 内容非法),不得与内容非法合并。
- `internal/cli/native_s9_original.go:86-88` — 该处 `json.Unmarshal` 失败时同样应保留为作者责任错误(实际文件 + 解析位置),不得退化为通用错误。

**验证**:
- 定向:`TestS9ReceptionFeedbackNewExecutionKeepsOldFailureHistorical`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestS9ReceptionFeedbackNewExecutionKeepsOldFailureHistorical'`。

**风险**:
- 反馈文本必须包含 `workpacket.ValidateUniqueJSON` 的原文(测试用同一个原校验器构造期望串),换成 `json.Valid`/`json.Unmarshal` 的措辞会不匹配。
- 失败分类必须落进 `nativeAuthorError`,否则 `internal/cli/native_s9_attempt.go:158` 会把 FailureKind 记成 "unknown" 而非 "author"。
- 作者正在写文件时的半截 JSON 会从「pending」变成「作者错误」;这正是蓝图 `:238` 的要求,但需与作者提示语一致(同一文件、同一字段口径)。
- 不要把 `blocked`/`contract_conflict` 的独立先接收路径(同文件 `:68`、`:119`)一并短路。

### G07-C — S8 人工批准模式门被摘除,validateS8ApprovalMode 零调用者

**问题**:没有工作授权的 plan_approval_required Investigator 的答卷照样被 S8 接收方接受并写进 Case。

**蓝图依据**:`L3-S8-finding-investigation.md:215`「已有匹配授权时原批准消费者记录并应用既存真实决定,不能由 Hook 自行作出批准」;`:81`「确需批准时记录原批准主体的真实决定」。

**解法**:接收路径在写入之前先确认原责任人的真实授权状态:连续 plan_checkpoint/one_shot 派发按原口径直接放行;批准模式派发必须通过原激活/读回链(`agentauth.ValidateWorkingAuthorization`)才允许其答卷进入 Case。授权检查是「本人是否真在工作/是否真被激活」的结构性事实,不引入 revision、散列或 CAS 判定。

**具体改动**:
- `internal/investigation/hypothesis_result_receipt.go:73` — 在 `:71-73` 的「已接受回执直接返回」之后、`:74` `hypothesisResultSourceRoot` 之前插入 `validateS8ApprovalMode(root, snapshot.State, request.OwnerAgentID)` 调用,失败即返回错误,保证未授权者的答卷不被读取、不被写回、不产生任何 Runtime 变更。
- `internal/investigation/hypothesis_result_receipt.go:257` — `validateS8ApprovalMode` 保持现有实现(已通过 `internal/agentauth/working.go:16` `ValidateWorkingAuthorization` 复用原激活链);不新增 revision/散列比较,不新增 CAS。
- `internal/investigation/hypothesis_result_receipt.go:120-125` — 晚期结果分支(`receiveLateHypothesisResult`)必须落在上面那道授权门之后,避免未授权者绕过「保留原稿」路径写入 Case。

**验证**:
- 定向:`TestS8GenerationTwoOriginalHookReceiptReplaySiblingAndGates`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/investigation/ -run 'TestS8GenerationTwoOriginalHookReceiptReplaySiblingAndGates'`。

**风险**:
- `ValidateWorkingAuthorization` 要求 Agent 处于原 working 状态并持有原激活链;若晚期/重放场景的 Agent 活动状态已不是 "working",会被误拒。插入点必须在「已接受回执直接返回」之后,避免破坏重放静默语义。
- `internal/investigation/hypothesis_result_receipt.go:113-114` 仍有一处 revision 比对(`ErrStaleRevision`)。授权门必须放在它之前,不能依赖它通过;本组不建议顺手改这处(超出本组范围),但也不要把它当作本组的门禁。
- 该函数现在只有这一个调用点;若 S7/S9 有同形接收路径需另案处理,不要在此处泛化。

### G08-D — 独立验证者在 Targeted 未就绪时就被派发

**问题**:hook 直接输出「Registration is complete; Main arranges actual integration and independent verification through the original Impact responsibility.」—— Targeted 原卷还没就绪,独立验证者的派发入口就已经出现。

**蓝图依据**:`L3-S9-repair.md:72`「独立复验与 S7 后继 | Main 按实际工程进度安排独立验证者」;`L4-workflow-convergence.md:213`。

**解法**:独立验证者的「可派发」由两件结构事实决定:当前阶段确实处在修复阶段(权威根仍在本阶段),且当前状态确实处在「准备 Targeted 原卷」的阶段;未到该阶段时,只保留「已登记、等本人集成与变更影响后再派发」的等待语义,不得给出 spawn/resume 入口;Targeted 原卷未就绪时,反馈必须是「原登记责任重新落盘 + 原责任准备其答卷」,而不是「另存一份分工」。

**具体改动**:
- `internal/cli/repair_verifier_dispatch_questionnaire.go:25-31` — 恢复阶段守卫:在 `repairVerifierDispatchCurrent` 里加回 `lifecycle.state == "bug_resolution"` 判定(父提交 `9bf73ca` 同位置用 `mapFieldCLI(state,"lifecycle")["state"]`,该 helper 仍在 `internal/cli/investigation_command.go:354`),非修复阶段不产生任何独立验证者分工路径。
- `internal/cli/repair_verifier_dispatch_questionnaire.go:30` — 准入状态集合收回为 `planning`/`reproducing`/`targeted_reverification`,移除 `repairing` 与 `ready_for_full_review`(这两个阶段还没有 Targeted 原卷)。
- `internal/cli/repair_verifier_dispatch_questionnaire.go:56-58` — 把「Main arranges actual integration and independent verification through the original Impact responsibility.」改回等待语义(父提交 `9bf73ca:57` 同文)。
- `internal/cli/repair_verifier_dispatch_questionnaire.go:69` — 把「Main restores the named source/filesystem failure without resaving the division.」改回:先点名原分工文件路径,要求恢复命名的来源/文件系统故障后在原分工路径重新落盘,原登记被复用、由原 Targeted 责任准备其答卷(父提交 `9bf73ca:69-71` 同文)。

**验证**:
- 定向:`TestS9VerifierRegistrationWaitsForTargetedReadiness`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestS9VerifierRegistrationWaitsForTargetedReadiness'`。

**风险**:
- 守卫用 `lifecycle.state` 时,任何 fixture/真实状态里该字段缺省会直接把分工路径判掉(整段上下文变空);需确认读取路径与 `internal/repair/runtime.go:206` `setLifecycle(state,"bug_resolution","planning")` 一致。
- 收紧状态集合会让 `repairing`/`ready_for_full_review` 阶段不再出现独立验证者分工问卷;若 S9 其它流程依赖该问卷存在,需同批检查 `internal/cli/repair_lead_questionnaire.go` 的调用方。
- 与 G12-E、G11-E 同根因(准入集合放宽/lifecycle 守卫被删),应合并为一个修复点。
- 文案里的 "Final integration and ChangeImpact" 是工程集成事实,不是散列/版本门禁;不要再引入 Final/Changeset 产物作为额外拒收条件。

### G08-E — 旧团队 PLAN_REPORT 收尾门仍拦原生 S9 Builder

**问题**:原生 S9 Builder 收到 TeammateIdle 就被 exit 2 拦住,现场提示要求「SendMessage message_type=plan_report」补一个旧 checkpoint,而不是让它交本人原卷。

**蓝图依据**:`L4-hook-platform-wiring.md:162`「删除旧 TeammateIdle 和固定 execution 的收尾接线,不为旧分支要求当前原生任务额外 Plan 或完成报告」;`L4-hook-anchor-catalog.md:54`「保留已有旧团队及固定责任的收尾语义,新原生流程不依赖它开工或收卷」;`L3-S9-repair.md:248`「Worker SubagentStop兜底本人原卷并交接真实结果或未完成事实」。

**解法**:旧团队的 PLAN_REPORT 收尾语义保留在旧路径上,但原生责任不得被它收尾:持有原包责任(审查/调查/修复)或原 Assignment 记录的 actor,由原生分支判定(未完成即「本人责任未完成,交 Main」,不 deny);TeammateIdle 不得再作为当前原生流程的收尾接线。

**具体改动**:
- `internal/hook/stopidle.go:591-592` — `questionnaireAssignment` 补上 `agent.RepairResultAssignment`,使原生 S9 责任与 S5/S7/S8 原生责任同等对待。
- `internal/hook/stopidle.go:596-602` — 删掉 TeammateIdle 的旧 PLAN_REPORT deny 分支(以及 `:603-630` 的 TeammateIdle 续段),按蓝图 `:162` 把旧 TeammateIdle 收尾接线从当前原生流程摘掉。
- `internal/hook/stopidle.go:631-637` — 通用 `!planRecorded` deny 不得作用于原生责任:已在本组第一处被判定为原生责任的 actor 应走 `internal/hook/stopidle.go:144-157` 的原生分支。
- `internal/hook/stopidle.go:77-83` — 若上述改动后 TeammateIdle 仍会落到旧矩阵(即 actor 未被标记为原生责任),按蓝图 `:162` 让 TeammateIdle 不再进入 exit2 收尾接线(旧团队语义保留在 SubagentStop 路径)。

**验证**:
- 定向:`TestPublicS9StopIdlePreservesOriginalResultState`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestPublicS9StopIdlePreservesOriginalResultState'`。

**风险**:
- 实测该 fixture 的 S9 Builder 在 `internal/hookctx/loader.go:830-866` 的标记条件下**没有**被置为 `RepairResultAssignment`,因此 `internal/hook/stopidle.go:127`/`:130`/`:144` 全部不触发,才会掉到旧矩阵;只改 `:591` 不足以过测,必须同时确认标记来源,否则要靠 `:77-83` 的 TeammateIdle 旁路兜底。
- 直接删 `:596-602` 而不处理 `:631` 的通用 deny,TeammateIdle 会改判成「subagent 停止前未记录任何 PLAN_REPORT 或 Result」,仍是 exit 2。
- 旧团队的 TeammateIdle 语义若被其他测试依赖,删除前需确认 `internal/hook` 的既有 stop/idle 用例。

### G09-D — 平台事件被提前短路 / 身份信息被丢弃,接收方拿不到准确对象

**问题**:(a) 非 Main/Worker 的 PreToolUse 被整类吞掉,S6 委派预检里程碑从未写入;(b) S7/S8 原生 Stop 在缺 `agent_transcript_path` 时识别不出责任,Stop code 0、诊断为空,未接收的责任没有交 Main。

**蓝图依据**:`L4-agent-dispatch-governance.md:167`「默认直接交 Main,保留原诊断、准确对象与已有产物。接收失败不能冒充完成」;`L4-agent-guidance.md:279`「未知 actor、只读探索或其他责任不得收到无条件实现/commit 指令」;`:288`「相同责任和内容去重;缓存可丢弃,失效只能多提示,不能吞错误、阻止接收或改变阶段」;`L4-runtime-control-plane.md:237`。

**解法**:平台事件不得被身份猜测提前吞掉:观察者身份只用于「要不要跑 Main Controller」,不用于「要不要观察这次事件」;责任识别按蓝图 8.3 的顺序 —— 先按实际派发 prompt 的准确引用查原责任,prompt 没有准确关联时才用平台返回文字中的引用,仍有多项真实责任歧义时如实交 Main,绝不静默返回。

**具体改动**:
- `internal/cli/hook_reader.go:208-210` — 把「Kind 既非 Main 也非 Worker ⇒ 吞掉整类事件」收窄为只排除嵌套的 Helper(`internal/cli/hook_reader.go:192` 赋的 "Helper");顶层未分类读者(`internal/cli/hook_reader.go:71` 初始 "Unknown")应继续走普通观察路径,让委派预检与里程碑照常写入。
- `internal/cli/hook_reader.go:190-196` — 观察者分类只服务于「是否运行 Main Controller」;不得据此丢弃事件本身(与 `internal/cli/controller.go:340-342` 的 Main 判定、`internal/cli/controller.go:827` 的委派预检保持一致)。
- `internal/cli/native_review_hooks.go:553-569` — `nativeReviewTaskText` 保留平台回退文本源:平台 transcript 打不开时,退回 `internal/cli/native_task_transcript.go:286-298` 已有的平台回退(`in.LastAssistantMessage`),而不是返回空串;仍保持「取最早那条派发任务」的口径,不改成「取最后一条」,以免把本人最终文本当成新派发。
- `internal/cli/native_review_hooks.go:308-311` — S7/S8 下识别不出责任时不再静默 `return false, 0`;按 `internal/cli/native_review_hooks.go:312-315` 的既有分支如实交 Main(诊断写明「未解析的原责任」),让接收方拿到准确对象。
- `internal/cli/native_feedback.go:65` — 保留「S7 reception incomplete; Main owns recovery for <owner> at <answer>」这一既有口径:识别成功后,接收失败按本人责任对象点名交 Main,不得冒充完成。

**验证**:
- 定向:`TestS6UnchangedPreToolUseReusesObservation`、`TestNativeMainS7ReturnKeepsSubmissionDiagnosticWhenOwnBodyFails`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestS6UnchangedPreToolUseReusesObservation|TestNativeMainS7ReturnKeepsSubmissionDiagnosticWhenOwnBodyFails'`。

**风险**:
- 放开 `:208` 后,顶层未分类 actor 会进入 Main Controller;必须确认控制器仍按原责任判定,不会给未知 actor 无条件实现/commit 指令(`internal/cli/controller.go:827` 的预检只给只读问题与读序,不是实现授权)。
- 平台回退文本源只解决「prompt 无准确关联」的兜底;若平台连返回文字都没有,仍需走 `internal/cli/native_review_hooks.go:312-315` 的交 Main 路径,不能猜绑 actor。
- 同一处 `internal/cli/hook_reader.go:208` 也是 G10-A 的落点,必须合并为一个修复点。
- S8 同形的原生 Stop 责任识别在 `internal/investigation` 包内另有实现,需一并核对是否也有「只读 transcript」问题。

---

## 5. D2 组落地方案(schema 严格性与退役散列字段)

### G07-A — 历史散列字段在 schema 层变成「拒收资格」

**问题**:S9 的原 Candidate 与 S8 的原 Case 回执只要带着历史散列字段(`contract_sha256`/`source_head`/`base_commit`、`packet_sha256`/`answer_sha256`/`assignment_sha256`/`task_sha256`/`manifest_sha256`),原消费者就直接拒收整份历史记录,原责任无法继续。

**蓝图依据**:`L4-workflow-convergence.md:250`「历史记录内已存在的散列值只作为旧记录的原始数据保留,继续原责任时不重算或以其作资格」;`:281`「schema、状态、日志……均不生成或登记散列,也不以旧散列作匹配/拒收。……测试可记录既有历史数据但不得以散列作为验收前置」;`L4-stage-form.md:56`「机器字段格式不能拒绝可理解的本人原答卷」;`L3-S9-repair.md:15`「旧记录中已有散列值只作为历史原字段读取,不重新计算、登记或作为继续资格」。

**解法**:schema 层把「是否携带退役散列字段」从资格条件降为普通原始数据。做法是**保持其余字段严格**(顶层 `additionalProperties: false` 不动),只把退役的散列/Git 坐标字段**逐个显式列为允许项**:可选、无类型/格式/长度限定(接受 null、字符串、数字、数组、对象任意历史值)、仅作为旧记录原字段读取,不重算、不登记、不作资格。**不得用 `additionalProperties: true` 顶替**,也不得用任何散列/revision 比较决定通过。

**具体改动**:
- `internal/schema/assets/repair-workspace-candidate.schema.json:52` — 在 `properties` 末尾(`provenance` 之后)补列 3 个退役字段 `contract_sha256`、`source_head`、`base_commit`,一律声明为可选、无限定(空 schema `{}`,仓库先例见 `internal/schema/assets/loop-state-v1.3.schema.json:2104` 的 `"base_sha": {}`);`additionalProperties: false`(`:4`)与 `required`(`:5-15`)保持不变。
- `internal/schema/assets/hypothesis-result-receipt.schema.json:117` — 在 `successor_case_revision` 之后补列 6 个退役字段 `packet_sha256`、`answer_sha256`、`assignment_sha256`、`task_sha256`、`manifest_sha256`、`review_round`,同样可选、无限定;`additionalProperties: false`(`:7`)与 `required`(`:8-27`)保持不变。
- `internal/schema/assets/repair-workspace-candidate.schema.json:47` — `proposal` 维持自由对象(不加 `additionalProperties: false`),否则 `proposal.contract/session/plan/plan_report` 里的历史 `sha256` 会被二次拒收。
- **明确不做**:不新增任何 sha/hash/digest/revision 的比较、校验或「重算后一致才通过」的分支;`internal/runtime/repair_result_checkpoint.go:70` 与 `internal/runtime/hypothesis_result_checkpoint.go:118` 的校验入口不改。

**验证**:
- 定向:`TestNativeS9OriginalCandidateReadsHistoricalMachineValues`、`TestOriginalHypothesisReceiptIgnoresHistoricalMachineFields`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestNativeS9OriginalCandidateReadsHistoricalMachineValues'`;`go test ./internal/investigation/ -run 'TestOriginalHypothesisReceiptIgnoresHistoricalMachineFields'`。
- 现状复现(已实测):S9 报 `additional properties 'contract_sha256', 'source_head', 'base_commit' not allowed`;S8 报 `at '/hypothesis_results/0/receipt': additional properties 'task_sha256', … not allowed`。

**风险**:
- 退役字段若带 `type`/`pattern` 限定,历史 `null`/数字/对象值仍会被拒 —— 必须是无限定声明。
- 若顺手把这两个 schema 改成 `additionalProperties: true`,会同时放过真正的非法字段,与 G12-D 的严格性目标直接冲突。
- `internal/schema/assets/review-investigation-case.schema.json:458` 用 `$ref` 引用本 receipt;该 Case 文件的顶层 `additionalProperties` 已被 X-01 放宽为 true,本组不动它,但 X-01 后续若恢复该文件严格性,必须同步给出 Case 顶层退役字段清单。
- S8 回执解码走 `internal/runtime/hypothesis_result_checkpoint.go:41` 的宽松 `json.Unmarshal`,未知字段被忽略;补字段后无需改 Go 结构体。

### G12-D — 状态索引 schema 顶层被放宽成 true,非法字段不再被拒

**问题**:状态索引行(`entities.work_packets[]` 的外部 body 索引)里混入非法字段(如 `title`)时,`Snapshot()` / `ObservationSnapshot()` 返回 nil 错误,Runtime 不再拒绝被外部改写的索引,主控也拿不到「快照无法核验」的诊断。

**蓝图依据**:`L4-hook-acceptance-checklist.md:15`「无散列验收同时检查有效题面/指引、**schema**、所有自然事件的处理器及可达消费者、原 Writer、状态/journal/pending/receipt/原产物、恢复与下一阶段……历史值可按原记录读取但不重算、重验或登记到新事实」。

**解法**:索引行是「原责任 + 定位符」的封闭结构,必须恢复结构严格性:顶层与 `body_ref` 回到 `additionalProperties: false`,非法字段(如 `title`)被拒。同时把已退役的散列/size 字段**显式列为允许项**(可选、无限定),使历史索引行仍可被读取 —— 严格性来自「字段清单」,不是来自「什么都能过」。

**具体改动**:
- `internal/schema/assets/review-result-packet-index.schema.json:6` — 顶层恢复 `additionalProperties: false`;在 `properties`(`:18-67`)补列退役字段 `template_sha256`(可选、无限定)。
- `internal/schema/assets/review-result-packet-index.schema.json:53` — `body_ref` 恢复 `additionalProperties: false`;在其 `properties`(`:58-66`)补列退役字段 `sha256`、`size_bytes`(可选、无限定);`required` 保持 `["format","path"]`(`:54-57`)。
- `internal/schema/assets/review-result-packet-index.schema.json:72` — 内联 `$defs/binding` 恢复 `additionalProperties: false`;在其 `properties`(`:83-123`)补列退役字段 `review_plan_sha256`、`review_round`、`assignment_sha256`、`subject_digest`、`verification_artifact_digest`(可选、无限定);`required`(`:73-82`)保持不变。
- `internal/schema/assets/repair-result-packet-index.schema.json:6` — 顶层恢复 `false` + 补列 `template_sha256`。
- `internal/schema/assets/repair-result-packet-index.schema.json:51` — `body_ref` 恢复 `false` + 补列 `sha256`、`size_bytes`。
- `internal/schema/assets/repair-plan-report-packet-index.schema.json:6` — 顶层恢复 `false` + 补列 `template_sha256`。
- `internal/schema/assets/repair-plan-report-packet-index.schema.json:54` — `body_ref` 恢复 `false` + 补列 `sha256`、`size_bytes`。
- 跨文件配套(这两个 binding 定义在别处,归 X-01,本组只给字段清单):`internal/schema/assets/repair-plan-report-work-packet.schema.json` 的 `$defs/binding` 退役 9 项 `review_round`、`case_sha256`、`contract_sha256`、`session_sha256`、`plan_sha256`、`repair_assignment_sha256`、`manifest_assignment_sha256`、`task_sha256`、`manifest_sha256`;`internal/schema/assets/repair-result-work-packet.schema.json` 的 `$defs/binding` 退役 3 项 `plan_report_sha256`、`execution_sha256`、`rework_sha256` —— 两者要么维持 `true`,要么恢复 `false` 并按此清单补列。

**验证**:
- 定向:`TestNativeSnapshotFailureReachesMainAndRestoresOriginalRoute`;回归 `TestNewPacketStorageUsesOriginalBodyAndLeavesLegacyRowsInline`、`TestReviewResultReceptionPreservesOriginalBodyAndCheckpoint`、`TestOriginalPacketBodyPreservesBusinessFieldsAndResponsibility`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestNativeSnapshotFailureReachesMainAndRestoresOriginalRoute'`;`go test ./internal/runtime/ -run 'TestNewPacketStorageUsesOriginalBodyAndLeavesLegacyRowsInline|TestReviewResultReceptionPreservesOriginalBodyAndCheckpoint'`;`go test ./internal/packetbody/ -run 'TestOriginalPacketBodyPreservesBusinessFieldsAndResponsibility'`。
- 现状复现(已实测):`fixture did not cause the expected Snapshot failure: <nil>` —— 注入 `title` 后 `Snapshot()` 无错误。

**风险**:
- 新写入的索引行由 `internal/packetbody/body.go:184`(投影)与 `:206`(`body_ref` 只写 `format`/`path`)产生,字段集与严格 schema 一一对应;改动前若投影字段有变,新行会被自己的 schema 拒收。
- 这三个 schema 被 `internal/schema/assets/loop-state-v1.3.schema.json:4645`/`:4648`/`:4651` 与 `loop-state-v1.2.schema.json:4606`-`4612` 的 `workPacket` `oneOf` 引用:恢复 `false` 后,旧 Runtime 状态里的 `template_sha256`/`body_ref.sha256` 必须靠上面的退役字段清单才能继续匹配,漏列一项 = 整个 Runtime 状态不可读写。
- `review_result_reception` 必须保持可选(`:19-21` 不在 `required` 内):`internal/runtime/review_result_reception_test.go:71`-`:75` 明确覆盖「旧 Runtime 没有该字段」。
- `internal/runtime/packet_body.go:404` 的报错文本是 `packet body index <id>: … additional properties 'title' not allowed`,含 `title`,满足 `internal/cli/native_snapshot_failure_external_test.go:57` 的断言;不要改这条错误包装。
- **本轮生产修复中实测发现的一处配套改判(已执行)**:恢复顶层 `additionalProperties: false` 后,`internal/runtime/repair_packet_body_test.go` 的 `TestS9ExternalBodyRejectsWrongRootDriftFormatsAndCancellation/unknown-index-field` 子用例转红。追溯该用例来历:它**在父提交 9bf73ca 就存在**,当时 `index["future_field"] = true` 走通用断言 `if err == nil { t.Fatalf("unsafe %s accepted") }` —— 即**父提交要求 foreign field 必须被拒**。是重构提交 `d0d948a` 在放宽索引 schema 顶层 `additionalProperties`(false→true)之后,反向新增了一个「必须容忍 foreign field」的特例分支以便测试转绿。该特例断言与 G12-D 同源、方向相反,属**改判 STALE**,已按父提交口径改写回「必须被拒」,用例转绿。教训:凡「生产放宽 + 测试反向改写」成对出现处,两者应一并按 blueprint 复判,不能只改生产。

### G04-A — 「已声明但为空」的集合被拷成 nil slice,context.json 里写成 null

**问题**:`runtime assignment-context prepare` 在检查集为空时退出 1(发布出的 `context.json` 把空集合写成 `null`,而生产 schema 要求 array),上下文根本发不出去。

**蓝图依据**:`L3-S6-build.md:149`「识别到的任务检查装配为 Main 合并后检查及其 CheckProfiles,采用整树输入、禁止复用,**fixtures／dependencies 文件集合为空**,不要求作者为正常准备补文件 SHA。**Worker 声明空的 runner 检查集**。」

**解法**:区分「未声明」与「已声明为空」两种集合事实,并让已声明的集合在发布物里始终表现为**空数组**而不是 null;不声明的集合仍保持不声明(不得替 Main 伪造「明确不派 runner 检查」)。集合语义用「责任与事件身份」表达,不引入任何散列/revision 门禁。

**具体改动**:
- `internal/cli/assignment_context.go:331` — `Dependencies` 拷贝改为「源非 nil 时拷入非 nil 空切片」(`append([]checkexec.FileRef{}, …)`),使「文件集合为空」发布为 `[]`;写法对齐 `internal/workspace/check_profile.go:29`。
- `internal/cli/assignment_context.go:332` — `Fixtures` 同上(对齐 `internal/workspace/check_profile.go:30`)。
- `internal/cli/assignment_context.go:339` — `WorkerChecks` 拷贝改为「源非 nil 时拷入非 nil 空切片」(`append([]assignmentrecord.WorkerCheck{}, …)`),使「Worker 声明空的 runner 检查集」发布为 `[]`;写法对齐 `internal/assignmentrecord/record.go:487`。
- `internal/cli/assignment_context.go:329` — 外层 profiles 拷贝改为 `make`+`copy`(对齐 `internal/assignmentrecord/record.go:1143`-`:1144`),消除该处的 nil/空歧义。
- `internal/cli/assignment_context.go:313` — 自校验关口保持原样(它是问题的暴露点,不是修复点)。
- 不改 `internal/schema/assets/native-assignment-context.schema.json`(`:1067` `dependencies`、`:1073` `fixtures`、`:569` `worker_checks` 要求 array 是正确的生产契约;该文件在 d0d948a 未被放宽)。
- (可选)`internal/cli/assignment_context.go:331`-`:332` 同一函数 —— `Environment` 目前未拷贝、与源共享底层数组,与函数注释「不共享可变切片」不符;补一次拷贝不改变 JSON 输出,但可消除隐患。

**验证**:
- 定向:`TestNativeProfileContextExposesFrozenOriginalProfile`、`TestS6ClosingContractCLISeparatesOwnedCompletionFromMain`、`TestS6NativeClosingContextKeepsAssessmentEditableAcrossRetry`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestNativeProfileContextExposesFrozenOriginalProfile|TestS6ClosingContractCLISeparatesOwnedCompletionFromMain|TestS6NativeClosingContextKeepsAssessmentEditableAcrossRetry'`。
- 现状复现(已实测):`at '/check_profiles/0/dependencies': got null, want array`、`at '/dispatch/closing_contract/worker_checks': got null, want array`。

**风险**:
- 不能把「未声明」与「已声明为空」合并:按 `internal/assignmentrecord/record.go:96`-`:99` 的注释与 `:447`-`:449`(`worker_checks must be an explicit JSON array`),`WorkerChecks=nil` 表示 Main 未提供收尾契约;无脑写成 `[]` 会伪造「Main 明确不派 runner 检查」。注册入口 `internal/assignment/record_registration.go:247` 已保证已登记契约的 `WorkerChecks` 非 nil,所以本处只需「保留显式性」。
- 不能反向把 schema 改成接受 null:那会让「未声明」也变成合法集合,与蓝图「声明空的 runner 检查集」相反。
- 源 profile 的 `environment`/`dependencies`/`fixtures` 已由 `internal/cli/assignment_context.go:191` 的 `checkexec.ValidateProfiles` 保证是数组(`internal/schema/assets/check-profile.schema.json:78`-`:89` 要求 array),所以只在拷贝层修,不要动 profile 构造或 `checkexec.Profile` 结构。
- `copyContextProfiles` 返回的 `out` 若为空会得到 nil,`nativeAssignmentContext.CheckProfiles` 带 `omitempty`(`internal/cli/assignment_context.go:28`)会整段省略;`check_profiles` 不在 schema 的 `required`(`internal/schema/assets/native-assignment-context.schema.json:7`-`:21`),无需额外处理。

---

## 6. D4 组落地方案(S9 坐标、回收清理与诊断存储)

### G01-A — 原生 S9 交付执行记录丢失源分支 → integration.Inspect 入口即拒

**问题**:Main 接手原生 S9 Candidate 后做集成检查时,`integration.Inspect` 直接返回 `assignment worktree and branch are required`,11 条 S9 交接测试全部死在共享夹具 `internal/cli/repair_integration_recovery_test.go:41-44`,各自的断言体一行都没执行。

**蓝图依据**:`L4-worktree-governance.md:33`「S6 集成器核对实际分支」;`:119`「S6 已接收的 checkout 缺失时,原诊断给出准确路径、分支和 accepted SourceHead,由 Main 恢复该 Git 来源再沿原 task-integrate 接续」;`L4-workflow-convergence.md:138`「Hook 所在的接收、Writer、事务恢复和后继均不计算、登记或校验散列……原事务采用已有责任、实际内容和内部版本协调,不以散列作为写入配对或恢复条件」。

**解法**:原生交付责任的执行记录必须带**真实的源分支坐标** —— 它是该责任作者实际 checkout 的分支,由 Git 实际解析得到后写进责任记录(可核对、可持久读回),解析不到就如实报错、不落空串。分支只是可核对的实际坐标,不是散列、不是版本证明,不参与任何内容资格判定;已接收的专业事实不因分支取值而被拒收或重收。

**具体改动**:
- `internal/cli/native_s9_original.go:169` — 把 `workspace.Execution{... Branch: "", ...}` 改成先解析作者 checkout 的实际分支再写入:`git branch --show-current` 取值,解析失败或为空时按父提交语义如实报错("native source needs its actual Git branch"),绝不落空串。
- `internal/cli/native_s9_original.go:159-163` — 已存在同责任执行记录时:若记录的分支为空,用同一次真实解析补齐;已有非空分支不重算、不覆盖(责任坐标一旦存在即为权威)。
- `internal/cli/native_s9_original.go:97` — 唯一的登记调用点,解析失败会沿原错误路径返回(不吞错、不降级为空串)。
- `internal/cli/native_s9_original.go:169` 的同行字面量中 **不要** 恢复父提交同处的 `BaseCommit` 与 `e.Inputs[...]=SHA256` —— 那两项属本轮要退役的散列门禁,HEAD 已正确移除。

**验证**:
- 定向:`TestS9HandoffTypedCandidateCannotLoseProvenanceOrSource`、`TestS9HandoffFinalProjectionRetainsOriginalCleanupResponsibility`、`TestS9HandoffTypedSelectorPreservesChangedAttemptFence`、`TestS9HandoffDamagedTypedCandidateStopsBeforeMerge`、`TestS9HandoffSameOwnerNeighborCannotSupplyOrBreakOriginalCandidate`、`TestS9HandoffFinalReceiptSurvivesMissingPlanIndex`、`TestS9HandoffAcceptedFinalCleanupUsesFrozenSessionProtection`、`TestS9HandoffCanceledFinalAcknowledgementPreservesOriginalProof`、`TestS9HandoffSpentProgressPreservesIndexesWithoutNewDocumentReads`、`TestNativeS9OriginalFinalRecoveryDoesNotReopenLivePlanOrPrivateInput`、`TestNativeS9UnrecordedMergePreservesUnknownAndWorktree`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestS9Handoff|TestNativeS9OriginalFinalRecoveryDoesNotReopenLivePlanOrPrivateInput|TestNativeS9UnrecordedMergePreservesUnknownAndWorktree'`。

**风险**:
- 恢复「取不到就报错」会把非 Git 作者根变成硬失败。现有三个调用方(`internal/cli/native_s9.go:601`、`internal/cli/native_s9_recovery.go:127`、`internal/cli/native_s9_original.go:97`)传入的 source 都是 Worker worktree 根,夹具在 `internal/cli/native_s9_test.go:42` 用 `git worktree add -b native-s9-test` 建立,故能解析出 `native-s9-test`;但将来若出现非 worktree 作者根,这里会拒绝而非降级 —— 这是有意的语义(拿不到准确分支就不该登记)。
- 设计包原文引的父提交位置有误:`9bf73ca` 并不存在 `internal/cli/native_s9_original.go`(该文件是 `d0d948a` 新增),真实父提交代码在 `9bf73ca:internal/cli/native_s9.go:1165-1168`(解析 + 报错)与 `:1182`(`Branch: branch`)。落地时按后者对照,不要按不存在的文件行号改。
- 波及面:`internal/cli/s9_final_writer_concurrency_test.go:48` 现在在测试里自己跑了一次 `git branch --show-current` 绕过这个缺陷;修好后这行可删(可选,不删也不影响)。
- 连带:`internal/cli/native_s9_main_continuation_test.go:147` 用 `e.Branch` 组装 `worktreeBranch` 返回值;`e.Branch` 从空串变成真实分支后,「missing-branch」/「wrong-branch」两个场景的判别依据从「空串」变成「真实分支不匹配」,语义更准,但该测试需重跑确认。

### G02-A — 原生 Result 反向选责用目录基名匹配完整 packet ID,长 ID 分段后恒不命中

**问题**:Main 跑 `stage-answer consume` 恢复原生 S9 Result 时一律报 `native Result path has no exact current original Plan packet`,8 条原生恢复测试全部复现同一错误串。

**蓝图依据**:`L4-agent-dispatch-governance.md:135`「S7～S9 的原任务和已有责任关系提供准确卷位置与实际作者根,不能只按 cwd、角色或同名文件猜选,也不要求返回绝对路径来取得接收资格」;`L3-S9-repair.md:134`「新责任沿已有准备入口给出唯一作者路径,例如 `.loop-submissions/native-s9/<原Plan packet>/plan.json` 和 `result.json`;占位路径不能当本次任务路径」。

**解法**:反向选责必须用「原 Plan packet 推导出的唯一作者目录」这一个坐标判定,而不是目录基名、文件名或 cwd 的猜测。责任 ID 被分段时,判定必须复用**生产者写盘时的同一个推导**,使「这条 Result 属于哪条原责任」由原责任坐标唯一确定;多责任歧义仍保留检测。

**具体改动**:
- `internal/cli/native_s9_recovery.go:87` — 去掉 `filepath.Base(filepath.Dir(path)) != route.ID` 这个「基名 vs 完整 ID」的筛选(分段后基名只是尾片段,恒不等)。改为:用 `repair.NativeS9PrivatePaths(route.ID)`(生产者写盘用的同一函数,`internal/repair/native_s9_private.go:25-31`)算出该责任自己的私有 Result 相对路径,再要求传入路径以它结尾。
- `internal/cli/native_s9_recovery.go:94-101` — 现有的后缀判定保持原样(它已经是对的),改完后它成为对同一坐标的二次确认;**不得引入第二套分段算法**。
- `internal/cli/native_s9_recovery.go:109-110` — 多责任歧义检测保留:不同 route 的 packet ID 不同 ⇒ 私有目录不同 ⇒ 正常只有一条命中,该分支退化为防御性断言,**不得删**。
- `internal/cli/native_s9_recovery.go:115` — 命中不到仍以同一错误串拒收,不放宽、不按「最近一个责任」猜选。

**验证**:
- 定向:`TestNativeS9OriginalFlatConsumeReceivesAndProjectsReplay`、`TestNativeS9OriginalConsumeDoesNotReimportAcceptedPlan`、`TestNativeS9ExplicitStopChangeStillConflictsAfterRecovery`、`TestNativeS9OriginalConsumePendingPrecedesMissingTypedAndChangedFlat`、`TestNativeS9PendingRecoveryReportsChangedPrivateOriginal`、`TestNativeS9OriginalConsumeMissingUnadaptedDraftCanAdaptExplicitFlat`、`TestNativeS9OriginalConsumeAcceptedPlanSurvivesMissingDisplayBrief`、`TestNativeS9AcceptedDRecoveryPreservesMissingButRejectsChangedOriginals`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestNativeS9OriginalFlatConsumeReceivesAndProjectsReplay|TestNativeS9OriginalConsumeDoesNotReimportAcceptedPlan|TestNativeS9ExplicitStopChangeStillConflictsAfterRecovery|TestNativeS9OriginalConsumePendingPrecedesMissingTypedAndChangedFlat|TestNativeS9PendingRecoveryReportsChangedPrivateOriginal|TestNativeS9OriginalConsumeMissingUnadaptedDraftCanAdaptExplicitFlat|TestNativeS9OriginalConsumeAcceptedPlanSurvivesMissingDisplayBrief|TestNativeS9AcceptedDRecoveryPreservesMissingButRejectsChangedOriginals'`。

**风险**:
- `internal/repair/native_s9_private.go:26` 的 `^packet-[A-Za-z0-9._-]+$` 必须放行 route ID:实际 ID 是 `"packet-" + base64url(责任键)`(`internal/workpacket/repair_plan_report.go:203-204` + `:234-241`),字符集落在该正则内;旧的 64 位十六进制 packet ID 同样落在内,因此旧记录无需迁移、无需重算任何散列。
- 分段算法必须只有一处实现:`internal/repair/native_s9_private.go:13-23` 是唯一分段点。若在 cli 侧另写一份分段,两个坐标会漂移,回归会以同样的错误串复发。
- 反例不得被放过:`internal/cli/native_s9_recovery_test.go:134` 同时喂入主根下的正确路径与 `.claude/submissions/native-s9/wrong-plan/result.json`,两者都必须被拒 —— 前者由 `internal/cli/native_s9_original.go:29-31`(作者位置缺失)拒,后者由后缀不匹配拒。改法不得把其中任一条变成接收。
- 潜伏项(本次未修):`internal/schema/assets/repair-result-candidate-provenance.schema.json:45` 的 pattern 仍是 `^\.loop-submissions/native-s9/packet-[A-Za-z0-9_-]+/(plan|result)\.json$`,不含分段形状。该字段 `private_submissions` 目前生产侧从不填充(`internal/runtime/repair_result_types.go:29` 为 `omitempty` 且非 required),故现在是休眠的;但若将来填充,分段路径会在 schema 层被拒 —— 需 owner 决定是否同批放宽。

### G12-F — 已回收的 worker worktree 上无条件执行 git,git 128 直接终止清理重试

**问题**:Worker checkout 被回收后,Main 重试清理直接死在 `git branch --show-current: exit status 128: fatal: cannot change to ...`,检查点卡在 `StateCleanupPending`,已就绪的后继被堵住。

**蓝图依据**:`L4-workflow-convergence.md:212`「…接收不证明集成或检查完成;工作区清理单独处理,不撤销接收或堵住后继」;`L4-worktree-governance.md:115`「…worktree 已回收不撤销已接收专业事实」;`L3-S9-repair.md:275`「…资源清理独立处理,清理 pending 不截断已就绪专业后继。准备失败保留已有 Final、原 ACK 与原稿,清理重试不重收 Final、不重跑已完成检查、不覆盖 Impact」。

**解法**:清理重试要区分两件**结构性事实**:(a) 原 checkout 已被回收 —— 这是资源事实,不是专业失败,不撤销已接收的 Candidate/Final/ACK,Main 沿既有检查点继续把资源退休做完(`StateCleanupPending → StateComplete`),保留原 `VerifiedAt` 与已完成检查,不重收 Final、不重跑检查;(b) 原 checkout 还在 —— 才需要重新解析实际分支并做活体检查。没有可续检查点时仍必须拒收,并如实报告「原来源已不可用,Main 需恢复该精确来源」,不得凭空取得清理权。判定依据是「是否已有可续的检查点」,不是 revision、round 或任何散列。

**具体改动**:
- `internal/cli/workspace_delivery.go:150-153` — 不再把 `workspace.Git(ctx, e.Path, "branch", "--show-current")` 当成无条件前置。改为:仅当「即将做活体 Inspect」(无检查点,或检查点尚未进入合并后状态)时才解析作者实际分支;已有可续检查点时不对已回收的 checkout 执行这次活体解析,也不 `return fail(err)`。
- `internal/cli/workspace_delivery.go:156-169` — 把检查点加载与坐标核对(`Load` → `found` → `cp.WorktreePath/TargetBranch/BaselineGeneration` 核对)**提到** `:138` 的条件块之前,使 `:150` 能依据「是否已有可续检查点」决定要不要活体分支。只调顺序,不改判定内容。
- `internal/cli/workspace_delivery.go:195-196` — 已有可续检查点时,检查点即事实来源(`integration.InspectionFromCheckpoint(cp)`),不重跑活体 Inspect、不重跑已完成检查。
- `internal/cli/workspace_delivery.go:140-147` — Main 自身的 checkout 绑定核对与 `b.CommonDir` 赋值保持无条件执行,不要因为跳过作者分支解析而一并跳过。
- `internal/cli/workspace_delivery.go:202` — `a.WorktreePath, a.Branch, a.TargetBranch` 的赋值只服务于活体 Inspect 分支,保留在 else 分支内,不要上移。

**验证**:
- 定向:`TestNativeS9MissingWorkerCleanupResumesOriginalCheckpoint`(两个子用例 `candidate-only` 与 `accepted-final` 都必须过)。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestNativeS9MissingWorkerCleanupResumesOriginalCheckpoint'`。

**风险**:
- `candidate-only` 子用例(`internal/cli/workspace_missing_private_test.go:59-63`)要求**仍然失败**("unverified missing Worker acquired cleanup authority")。因此不能简单地把 `:150` 改成「解析失败就忽略」—— 那样活体 Inspect 会带着空/旧分支继续走,可能错误地取得清理权。必须让「是否活体 Inspect」与「是否已有可续检查点」绑定。
- `accepted-final` 子用例还要求 `recovered.VerifiedAt == original.VerifiedAt`(`:69`)且不重建 Worker(`:72-74`)。清理侧的 `internal/integration/integrate_responsibility.go:326-332` 已能容忍 checkout 不存在(只退休 Git 登记),本组不需改它。
- 清理重试会再次走到 `internal/cli/workspace_delivery.go:220-250` 的「未 registered」提交分支;`internal/repair/issued_repair_result.go:917-927` 的 `existingIssuedRepairResult` 幂等保护会让它重放既有 Final 而不重新收卷,满足「清理重试不重收 Final」。**若另一组把 `registered` 判定修对,本组的症状会自行消失,但「对可能已回收的 checkout 无条件跑 git」这一脆弱点仍在,本组改动仍应落地。**
- 检查点存储内部用 `CompareAndSwap`(`internal/integration/cleanup_recovery.go:36`)属「内部版本协调」,本轮不触碰,也不要改成别的门禁。
- 顺序调整会改变「Main checkout 绑定核对」与「检查点坐标核对」同时失败时的报错先后,属可接受的诊断顺序变化。

### G06-C — 诊断存储识别规则未随无散列诊断布局更新

**问题**:`diagnosticstorage.Recognizes` 对生产侧真实写出的诊断文件一律返回 false,原生诊断记录既不被识别为受控显示存储、也在 `internal/repair/artifact.go:238-241` 的清单里被当成产品文件。

**蓝图依据**:`L4-hook-acceptance-checklist.md:161`「…诊断存储识别同步核对;…诊断与内部暂存不转成作者新文件或接收资格;未知邻居不自动删除,也不因其存在阻止原卷接收」;`L4-workflow-convergence.md:250`。

**解法**:诊断存储的识别规则必须与生产者**实际写出的无散列布局**一致:路径按生产者分段后的形状识别(可逆编码,不是散列),正文按生产者的字段形状校验;`scope` 不再被要求是 64 位散列,而是接受生产者写出的坐标 JSON 数组,旧记录里既有的散列形状按形状继续识别、不重算、不作资格。识别仍须精确:未知邻居文件继续是产品文件,诊断与内部暂存不转成作者新文件或接收资格。

**具体改动**:
- `internal/diagnosticstorage/storage.go:18` — 路径正则扩到生产者的三种实际布局:(a) `.claude/diagnostics/planning/<name>.json`,`<name>` 要么是旧记录的 64 位十六进制名,要么是 base64url 且解码后为坐标 JSON 数组(生产者 `internal/submission/planning_diagnostics.go:20-25` 的可逆编码);(b) `.claude/diagnostics/native-s9/responsibility/<分段...>/observation.json`(生产者 `internal/cli/native_s9_diagnostic.go:45-59`);(c) 旧的 `.claude/diagnostics/native-s9/<64hex>.json`。
- `internal/diagnosticstorage/storage.go:19` — 临时文件正则同步覆盖新布局:`.diagnostic-<32hex>.tmp` 可出现在 planning 目录、旧 native-s9 目录,以及新的 responsibility 分段目录(生产者 `internal/cli/native_s9_attempt.go:202-212` 在最终文件同目录写临时文件)。
- `internal/diagnosticstorage/storage.go:78` — 去掉 `len(text("scope")) != 64` 这条散列形状门:scope 改为「非空字符串,且是旧记录的 64 位十六进制形状或新的坐标 JSON 数组形状」,不重算、不比对任何散列。
- `internal/diagnosticstorage/storage.go:73-76` — planning 正文判定保持 `{message, at}` 两字段 + RFC3339Nano 不变。
- `internal/diagnosticstorage/storage.go:22-28`、`:29-31` — 发布锁特例与路径总闸保持不变(只放宽路径/正文形状,不放宽「必须是普通文件、不得是符号链接」的约束)。

**验证**:
- 定向:`TestNativeS9ConsumerCauseSurvivesPrerequisiteAndConcurrentPublication`(目标)、`TestDisplayStorageKeepsUnknownNeighborsAndSymlinksVisible`、`TestNativeS9RecognizesOnlyExactOldNewAndTemporaryShapes`、`TestNativeS9PublishLockRecognitionIsExact`(回归)。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/diagnosticstorage/ -run '.'`;`go test ./internal/cli/ -run 'TestNativeS9ConsumerCauseSurvivesPrerequisiteAndConcurrentPublication'`。

**风险**:
- 收紧方向不能反:planning 的新文件名是 base64url 编码的 scope,`internal/diagnosticstorage/storage_test.go:24` 要求 `unknown.json` 这类产品文件判为 false。因此 planning 规则**不能**放松成「任意 `<name>.json`」,必须用「解码后是坐标 JSON 数组」来区分。
- 旧记录必须继续可识别:`internal/diagnosticstorage/storage_test.go:19` 与 `internal/diagnosticstorage/native_s9_attempt_test.go:14,33` 仍用 64 位十六进制文件名 + 64 字符 scope 并期望 true。旧形状只能按**形状**识别,不得重算散列、也不得以散列作资格。
- 波及面:`internal/repair/artifact.go:238-241` 用 `Recognizes` 判定某文件是否属控制面。修好后真实诊断文件才会被判为控制面而不是产品文件 —— 这是修正,但此前被算进产品清单的诊断文件会从清单里消失,依赖清单条数/内容的断言需要复核。
- `internal/cli/native_s9_attempt.go:183` 用 `previous.Scope == a.value.Scope` 复用旧消费者原因;scope 改成坐标 JSON 数组后,同一责任的两次尝试仍产出同一坐标数组,这条不受影响。
- **越界提示**:`internal/cli/native_s9_consumer_retention_test.go:85-103`(`TestNativeS9OriginalStopCauseReachesMainAfterSourceCleanup`)当前也失败。**本轮已复核**:该失败有两个成因 ——(a)「只写 3 条 before_fix_results」这一**逐断言覆盖门**已退役(本轮已在测试侧改判 STALE 并改为「不可用 assertion ID」这一仍存的结构性拒绝);(b) 改判后仍红,真实成因是**新发现的缺陷 G13**(见 §8),不属于本组。

---

## 7. D7 组落地方案(残余守卫、恢复漂移与命名规范)

> D7 的每一组方案都已在一次性克隆(`/tmp/adapt/probe`,基于 `df87fde`)里落地并实测通过,下列「已验证」即指该次实测。

### G09-F — 责任歧义未与「未知」分别报告,多重精确匹配被折叠

**问题**:Main 的 PostToolUse 回执里,当一次提示同时精确引用两份 S5 原卷时,只输出通用文案「Some original responsibilities remain unknown」,调用方无法区分「引用歧义」与「真未知」,也无法判断系统是否已停止猜选。

**蓝图依据**:`L4-agent-guidance.md:297`「准确责任与未知/多重引用分别覆盖」;`L4-agent-dispatch-governance.md:131`「不能按路径出现顺序猜选」。

**解法**:把「多重精确引用」与「未知」拆成两个彼此独立的结果并分别如实报告。多重精确引用时清空已选集合(不按路径出现顺序挑一个),但保留「歧义」这一事实,不得降级成「未知」;未知仍走原未知文案。不引入任何散列/版本/轮次判定。

**具体改动**:
- `internal/cli/native_main_return.go:174-181` — 现有 `selected, unknown = nil, true` 改为把结果写入新增的独立 `ambiguous` 变量:多重精确匹配且命中非 S7/S8 类责任时清空 `selected` 并置 `ambiguous`,不再污染 `unknown`。
- `internal/cli/native_main_return.go:215-217` — 在原 `unknown` 文案之前新增 `ambiguous` 文案,必须包含小写字面量 `multiple exact`,并写明「不按路径顺序选取」;原 `unknown` 文案保持原样。
- `internal/cli/native_main_return.go:146`/`:171` — 不改;`len(selected)==0 && !unknown` 的早退在歧义分支之前求值,保持原语义。

**验证**:
- 定向:`TestNativeMainReturnReadsS5ReceiptAcrossStageWithoutReceivingAnswers`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestNativeMainReturnReadsS5ReceiptAcrossStageWithoutReceivingAnswers'`。
- 已验证:一次性克隆按上述改法落地后该用例 PASS;同包 `TestNativeMainReturn*` 未出现回归。

**风险**:
- 测试用 `strings.Contains(text, "multiple exact")` 匹配,**必须是小写**;写成 "Multiple exact" 会不通过。
- 该文件里 `unknown` 还被 `internal/cli/native_main_return.go:146` 的早退消费;若顺手把 `unknown` 改回 true,「无匹配」与「多重匹配」又会合并,回归同根因。
- 只改文案不改选择逻辑会让系统继续猜选;清空 `selected` 这一步必须保留。

### G10-B — recovery apply 无条件执行 ValidateRestore,与仍存在的损坏 Runtime 比对而中止

**问题**:`runtime recover apply` 面对一个仍然损坏(但可解析)的活动 Runtime,直接报 `LOOP_RECOVERY_INPUT_DRIFT: checkpoint disagrees with current Runtime identity/baseline/REQ` 并中止 —— 「恢复损坏 Runtime」这个首要场景完全不可用;根因是拿**恢复候选**去和**正在被替换的损坏字节**比身份,等于把损坏物当权威。

**蓝图依据**:`L4-runtime-control-plane.md:72`「checkpoint 恢复和具名历史继续按故障模型检查实际保存材料、版本和事务配对…恢复承诺实际需要的材料缺失或冲突时,保留准确系统故障,不跳过或改用相似对象」;`L4-state-transition-core.md:196`;`L4-revision-usage.md:141`「Runtime revision 只是审计坐标,不证明专业正确」;`L4-revision-usage.md:29`。

**解法**:把「恢复前的比对」对象从*损坏的活动 Runtime*换成*计划本身与仓库当前实际保存材料*。三件事同时成立:(1) 原 Main 恢复守卫**继续无条件执行**(它守的是 Worker 租约/launch 所有权,不是散列),但它内部那条「候选 vs 活动 Runtime 身份/基线/REQ」的比对只在活动 Runtime 是**自洽 Runtime** 时才成立 —— 损坏投影不是权威,不能据此拒收;(2) 损坏投影也不能主张「更晚的进度」,只有自洽 Runtime 才能占住恢复槽位,否则冲突;(3) 计划陈旧性改用**结构性清单**判定:重新 Inspect 仓库,逐条比对计划记录的输入路径/种类/大小与当前实际保存材料,缺项/新增项/变更项一律 `LOOP_RECOVERY_INPUT_DRIFT`,并复核 REQ 绑定的 id/path/status/version。全程不比较 revision/sha/digest/指纹,不做 CAS。

**具体改动**:
- `internal/cli/recovery_checkpoint.go:180` — `runtime.ValidateCheckpointRestore(root, pair)` 这一「候选 vs 活动 Runtime」比对改为:仅当活动 Runtime 自洽时才执行;新增同文件辅助 `recoveryActiveRuntimeCoherent(root)`(读 `.claude/loop-state.json`,用 `semantic.RuntimeCandidateValidator` 判自洽),放在 `checkpointRecoveryGuard`(同文件 `:131`)之前。
- `internal/runtime/recovery.go:636-649` — `stateNewer` 主张在 `runtime_id` 不同时必须由自洽性佐证:活动投影若不是自洽 Runtime,不能主张「更晚的进度」,恢复继续(自洽时维持原 `LOOP_RECOVERY_SOURCE_CONFLICT` 语义)。
- `internal/cli/recover_command.go:278` — 在 `checkpointSource = document.CheckpointPath` 之前新增 `verifyRecoveryPlanInputs(root, document)` 调用;新函数与 `recoveryPlanDocument` 同文件,比较 `document.REQ` 的 id/path/status/version 与 `document.Inputs` 的 path/kind/size 对 `recovery.Inspect(root, document.REQ.Path)` 的当前清单。
- `internal/cli/recover_command.go:267-278` — 调用点必须落在同文件 `:267` 那次「已应用探测」**之后**:否则第二次 apply 会在已应用、Runtime 已变后误报计划陈旧。
- `internal/runtime/checkpoint.go:550-556` — 本组不改(该函数保留给 checkpoint restore 路径);本组不新增任何 revision/sha/散列门禁。

**验证**:
- 定向:`TestRuntimeRecoveryApplyNeverRevivesActiveAgents`、`TestRuntimeRecoveryApplySamePlanIsIdempotent`、`TestRuntimeRecoveryApplyRejectsREQInputDriftWithoutMutation`、`TestRuntimeRecoveryApplyRejectsNewArtifactAfterPlan`、`TestRuntimeRecoveryApplyRejectsActiveStateCreatedAfterMissingSourcePlan`、`TestRecoveryGuardProtectsActiveWriterAndResumesPending`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./tests/system/recovery/`;`go test ./internal/runtime/`;`go test ./internal/cli/ -run 'Recovery|Checkpoint|Recover'`。
- 已验证:一次性克隆里按上述三处改法落地后,`tests/system/recovery` 整包 ok、`internal/runtime` 整包 ok;`internal/cli -run 'Recovery|Checkpoint|Recover'` 只剩 2 条**在 HEAD 上同样失败**的既有用例(`TestCheckpointRejectsOlderPairAndForgedReceptionFacts`、`TestMainOrdinaryReadAndNonCheckpointStatusProgressWire`),属别的根因组。

**风险**:
- **只按前轮方向把 `ValidateRestore` 门控到 `hasIssuedIndices` 会打坏 3 条原本通过的漂移测试**(实测:`TestRuntimeRecoveryApplyRejectsREQInputDriftWithoutMutation` / `RejectsNewArtifactAfterPlan` / `RejectsActiveStateCreatedAfterMissingSourcePlan` 全部变成「apply 成功」)。父提交是靠计划里的 sha256 检出计划陈旧的,d0d948a 删掉散列机器后那条路消失,因此**必须同时补结构性清单比对**,否则「改了 REQ / 新增产物 / 计划之后才出现的活动 Runtime」会被静默接受。
- 清单比对必须跳过 `recovery.InputKindCheckpoint` 输入:checkpoint 目录会被 Runtime 正常裁剪,逐条比对会误报「计划输入已不存在」(实测 `TestTaskPlanningAnswerPendingFollowupRecovery` 因此失败)。
- 清单比对用 path/kind/size,size 只是弱代理;蓝图要求「检查实际保存材料」,更严格的做法是把 REQ 原件字节随计划冻结后逐字节比对(不产生任何散列),但要改计划文件格式,超出本组范围。
- `internal/runtime/checkpoint.go:553-556` 仍是 revision CAS 门禁(被 `internal/runtime/checkpoint_checks_test.go:37-59` 锁定)。本组不新增 revision/sha 门禁,但该 CAS 属于本轮「退役 revision 门禁」的目标,建议后续单独处理并同步改那条单测。
- 另一条同样偏严的门禁是 `internal/runtime/recovery.go:761` 的 `haveRev > wantRev`;它同时承担「已应用」与「更晚进度」的区分,退役前需与 `completePendingRecovery` 的自洽规则一起改。

### G11-A — PreToolUse 在 Runtime 不可读时被原生路由提前返回,跳过质量投影渲染

**问题**:Runtime 快照读不到时,PreToolUse 的输出只有一条纯文本 `additionalContext`,没有 `QUALITY_GATE` 行、没有 `LOOP RECOVERY` 包、也没有恢复动作,Agent 看不到质量投影,只能自己猜下一步。

**蓝图依据**:`L4-hook-platform-wiring.md:391`「适用的质量投影与原 Hook 输出共同来自相应控制结果」;`:184`「质量投影保留原 `status`、候选、观察 revision、缺项、冲突与 `transition_committed`」。

**解法**:原生快照失败路径也要经过唯一产出 `QUALITY_GATE` 行的渲染器。以「游标不可知」的控制结果(status=unknown、观察 revision=0、不写任何 Runtime)配一份与不可知游标一致的指引,渲染成 PreToolUse 输出:保留 `QUALITY_GATE` 行 + `LOOP RECOVERY` 包 + 与其它未知游标同样的恢复动作。不产生 deny,不造观察 revision,不写 Runtime。

**具体改动**:
- `internal/cli/native_hook_routes.go:256-257` — `modelContext` 分支拆出 PreToolUse 专属路径:构造 `policy.Decision`(decision=allow、additionalContext=保留现有诊断文案、guidance 的 stage/skill/action 取自既有投影函数的默认分支)与 `controller.ControlResult`(QualityGate.Status=unknown、ErrorCode 标识「运行时快照不可用」),交给 `hook.PreToolUseWithQualityGate` 渲染;渲染出错时回退到原纯文本分支,不让 Hook 失败。
- `internal/cli/native_hook_routes.go:11-17` — 补 `internal/controller` 与 `internal/hook` 两个 import。
- `internal/cli/run.go:535-597` — 不改;指引文案复用 `projectNext` 的既有默认分支(stage=cross-stage、action=`recover the runtime checkpoint`),不新造字符串、不另起一份恢复口径。
- `internal/hook/pretooluse.go:38-68` — 不改(渲染器已是唯一 `QUALITY_GATE` 产出点)。

**验证**:
- 定向:`TestUnavailablePolicyDoesNotDenyAndKeepsGraphRecoveryVisible`、`TestUnavailablePolicyDoesNotBlockLifecycleNotifications`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestUnavailablePolicy'`。
- 已验证:一次性克隆里按上述改法落地后 22 个子用例全 PASS,生命周期通知用例仍 PASS,`.claude/loop-state.json` 未被创建。

**风险**:
- 渲染器固定输出 `hookEventName="PreToolUse"`,所以只对 PreToolUse 生效;PostToolUse / SessionStart / SubagentStart 仍走原纯文本分支(它们的事件名不能被改写)。
- 输出形状从「纯文本 additionalContext」变成「`QUALITY_GATE` 行 + 包 + systemMessage」,任何断言旧形状的既有用例都要复核(实测 `TestUnavailablePolicy*` 与 saved/stage/design 组仍通过)。
- `projectNext("", "", root)` 会先走 `runtime.StageFor`;必须确认它不写盘(该用例已断言不创建 Runtime)。

### G11-B — S9 Targeted 的 checkout/来源绑定守卫调用点被删,守卫成死代码 ⇒ 隔离 checkout 可借走 Main 原卷

**问题**:`guardNativeS9TargetedSource` 被掏空成 `return contextForHook(in).Err()`,`nativeSharedCheckoutSourceError` 成了死代码,隔离 worktree 的 SubagentStop 能直接消费 Main 的原 Targeted 问卷;但把守卫原样恢复,另一条**同一提交新增**的用例(另一个 worktree 的 PreToolUse/PostToolUse 必须被接收)会失败。

**蓝图依据**:`L4-hook-acceptance-checklist.md:52` C03「责任由已有任务与登记坐标准确定位,不跨根、owner、责任或轮次借卷」;`L4-agent-dispatch-governance.md:57`「S7 Reviewer、S8 Investigator、S9 独立 Targeted 使用实际安排的独立上下文和各自原卷,不因独立性另加机器身份、worktree 注册或来源证明」「S7～S9 接收也不核 Git／ignore 状态」。

**解法**:恢复守卫本体,并把「共享 checkout 借用」这条禁令**限定在兜底消费路径**(Stop/SubagentStop:agent 自己的消息被当成它的答案)。自然事件路径(PreToolUse/PostToolUse)的原责任由登记坐标准确定位,不拿 cwd/Git 状态当来源证明 —— 这与 listener 用例自己的注释「A tool event's cwd alone does not prove which product it verified.」以及 C05「接收不访问 Git 状态」一致。两条用例因此同时成立,不需要 owner 改判任何一条。

**具体改动**:
- `internal/cli/native_s9.go:530-532` — 恢复 `guardNativeS9TargetedSource` 本体:空 cwd 直接放行(缺失 cwd 不是来源证明);按 `repairTargetedSelections` 取本人原卷路径,文本精确命中且 `repair.UsesSharedTargetedAuthoring` 为真时,调用 `nativeSharedCheckoutSourceError` 拒绝无来源绑定的共享 checkout。
- `internal/cli/native_s9.go:411-413` — 在 `receiveNativeS9Targeted` 之前插入守卫调用,且**只在兜底路径**(`natural == false`,见 `internal/cli/native_s9.go:241`)调用;自然事件路径不调用。
- `internal/cli/native_shared_source.go:13-51` — 不改;守卫恢复后不再是死代码。

**验证**:
- 定向:`TestNativeS9SharedTargetedCannotBeBorrowedByIsolatedStop`、`TestNativeS9TargetedListenerActualPlatformPreAndPost`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestNativeS9SharedTargetedCannotBeBorrowedByIsolatedStop|TestNativeS9TargetedListenerActualPlatformPreAndPost'`。
- 已验证:一次性克隆里按上述改法落地后,guard 用例 PASS,listener 的 PreToolUse/PostToolUse × main-checkout/other-checkout 四个子用例全 PASS(未限定事件类型时 other-checkout 两例 FAIL,即设计包所述冲突)。

**风险**:
- 两条用例按 cwd 对立(d0d948a 同一提交新增了 listener 的 other-checkout 用例,同时删掉了守卫调用点)。本方案按事件类型切分,已实测两者同时通过;若 owner 裁定以 listener 为准、彻底退役 Git 探测,则 guard 用例要同步退役,`internal/cli/native_shared_source.go:13` 变成可删死代码 —— 那是 C05 的方向,但等于删掉一条现有契约,需 owner 拍板。
- `nativeSharedCheckoutSourceError` 内部用 `git rev-parse` 判 checkout,与 C05「接收不访问 Git 状态／ignore／HEAD／baseline」抵触;限定在兜底路径只是缩小暴露面,不是消除。

### G11-C — S9 阶段交接文案使用泛称 "Plan" 而非蓝图规范名 "PlanReport"

**问题**:`runtime investigation contract approve` 的 stderr 阶段交接只写泛称 "Plan",不含蓝图规范名 "PlanReport";按规范名检索阶段交接的调用方检索不到,合同与交接虽然都已落盘,交接文本却无法被机器对齐。

**蓝图依据**:`L3-S9-repair.md:70`「Builder 在原位置分别保存本人 PlanReport 与 Result」;`L4-agent-dispatch-governance.md:115`「S9 brief 给出本人三字段 PlanReport…」;`L3-S8-finding-investigation.md:410`「原 S9 合同批准同时保存权威合同与阶段交接」。

**解法**:阶段交接文案统一使用规范名 PlanReport;只改文案措辞,不改交接行为、不新增字段、不新增判定。

**具体改动**:
- `internal/cli/controller.go:515` — `Builders save their own Plan with real prechecks and their Result` → `Builders save their own PlanReport with real prechecks and their Result`(无 owner 时的准备文案分支)。
- `internal/cli/controller.go:554` — `Each Builder saves its own Plan with real prechecks before implementation and its Result after work` → 同名替换为 `PlanReport`(有 owner 时的正常路径分支)。
- `internal/cli/investigation_command.go:1087` — 不改(合同与交接的落盘点已正确,只缺规范名)。

**验证**:
- 定向:`TestRuntimeInvestigationContractApproveCLIHandsOffToS9`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestRuntimeInvestigationContractApproveCLIHandsOffToS9'`。
- 已验证:一次性克隆里按上述两行改法落地后该用例(含两个子用例)全 PASS。

**风险**:
- 同包的 S9 阶段投影文案(`internal/cli/run.go:572`、`:574`、`:576`)仍用泛称 "Plan"/"Plan/Result";本组只修交接路径,若后续出现按规范名检索阶段文案的用例,需一并统一,否则会出现两套叫法。

### G12-C — 非 Worker/teammate 载荷跳过原消费者,未签发路径得不到 fail-closed 收据

**问题**:带 `teammate_name` 的 PostToolUse 载荷被判成 nested → `reader.Kind="Helper"` → 原消费者**根本没被调用**,未签发的 S8 答案路径既不报错也不拒收,静默无输出、调用方无法判断是「无人认领」还是「已被接收」。

**蓝图依据**:`L4-runtime-control-plane.md:237`「S7～S9 原生 Worker 的 PreToolUse 监听当前本人原卷,成功 PostToolUse 与 SubagentStop／Stop 兜底,均调用同一适用原消费者」;`L4-hook-acceptance-checklist.md:52` C03。

**解法**:非 Worker/teammate 载荷也进入原消费者路由,由原消费者按精确登记路径选卷:路径没有当前登记签发时,如实给出 fail-closed 收据(「无当前登记签发」)并保持 Runtime 不变;路径确有登记时照常走原接收。这类载荷仍不触发 Main Controller。

**具体改动**:
- `internal/cli/hook_reader.go:208-210` — `reader.Kind != "Worker"` 不再直接早退:PostToolUse 先走 `receiveSavedQuestionnaire`(与 Worker 同一条原消费者路由),原消费者未处理时才维持原来的 `(true, 0)` 返回;Main Controller 仍不由此路径触发。
- `internal/cli/hook_reader.go:76`/`:190-196` — 不改(Helper 判定保留,只改早退之后的路由)。
- `internal/cli/stage_answer.go:736-750` — 不改;`saveHypothesisResultHook` 已是 fail-closed 收据的生产点(`:749`「S8 answer path has no current registered issuance」),本组只是让这条路径真的被走到。

**验证**:
- 定向:`TestSavedHypothesisResultHookRejectsUnissuedPathWithoutRuntimeWrite`、`TestSavedHypothesisResultHookDoesNotTreatUnissuedTeammatePathAsCurrent`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestSavedHypothesisResultHook'`。
- 已验证:一次性克隆里按上述改法落地后两条用例全 PASS;同包的 saved/stage/design/planning 组仍 ok。

**风险**:
- Helper 载荷现在会真的进原消费者,凡是靠「Helper 一律不处理」来避免副作用的地方都会开始触发;必须复核「普通工具不得出现 SUBMISSION RECEIPT」这类断言(`internal/cli/planning_tool_change_test.go:62`,实测通过)。
- `saveHypothesisResultHook` 开头有 `mainQuestionnaireViewer` 早退(`internal/cli/stage_answer.go:737`),Main 载荷仍归 SubagentStop 路径,本组不改变这条既有分工。
- 收据内容依赖 `submission.Service.HypothesisResultSourceForPath` 的精确登记匹配;该选择逻辑本身没改,本组只是让它对 teammate 载荷也可见。

### G12-E — 独立验证者派发准入集合被放宽,S9 lifecycle 守卫被删

**问题**:在 S8 调查阶段(还没有 S9 合同)、以及 Builder 实施 / 影响处置 / 已就绪全审阶段,Main 仍然看到「独立验证者派发」入口,阶段责任被提前暴露,Main 可能据此去派发一个并不存在的 S9 独立复验责任。

**蓝图依据**:`L3-S9-repair.md:72`「独立复验与 S7 后继 | Main 按实际工程进度安排独立验证者,交原合同、实际产品、必要断言和本人 Targeted…」;`L4-hook-acceptance-checklist.md:53` C04。

**解法**:恢复 S9 lifecycle 守卫(只有 `bug_resolution` 才进入 S9),并把准入集合收回 blueprint 规定的阶段(准备 / 复现 / 定向复验);`session_id` 判定保留,它是原责任坐标,不是版本/散列门禁。

**具体改动**:
- `internal/cli/repair_verifier_dispatch_questionnaire.go:26-28` — 恢复 `lifecycle.state == "bug_resolution"` 守卫,置于 `session_id` 判定之前(S8 调查态因此不再出现派发入口)。
- `internal/cli/repair_verifier_dispatch_questionnaire.go:30` — 准入集合从 `planning|reproducing|repairing|targeted_reverification|ready_for_full_review` 收回为 `planning|reproducing|targeted_reverification`。

**验证**:
- 定向:`TestS9RepairVerifierDispatchQuestionnaireOriginalPhasesAndResponsibility`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestS9RepairVerifierDispatchQuestionnaireOriginalPhasesAndResponsibility'`。
- 已验证:一次性克隆里按上述两处改法落地后该用例 PASS(六个状态子用例全绿)。

**风险**:
- `session_id` 判定是 d0d948a 新增、父提交没有的;保留它意味着「S9 阶段但尚未开 RepairSession」时派发入口同样不出现 —— 与 blueprint「独立复验由 Main 按实际工程进度安排」一致,但若有用例依赖「只有 session 缺失才挡」需复核。
- 收窄准入集合会让 `repairing` / `ready_for_full_review` 阶段不再产出派发入口;若下游把该入口当作阶段推进信号,需改读原 responsibility 指针而不是入口文案。
- **与 D6 的 G08-D 是同一处生产位置**(`internal/cli/repair_verifier_dispatch_questionnaire.go:25-31`),必须合并为一次改动。

---

## 8. 本轮新发现的缺陷(G13 / G14)——不在既有 36 组内,现场发现

### G13 — `nativeS9ConsumerAttempt` 的消费者阶段白名单未随重构改名更新,专业拒绝原因在后续前置失败时被丢弃

**问题**:S9 原生 Result 消费点用 `EnterConsumer("original_result_consumer")` 标记消费者边界;而 `nativeS9ConsumerAttempt` 用**阶段名字符串白名单**判断「上一条尝试是不是消费者尝试」。白名单里仍是重构前的旧名(`flat_result_consumer` / `plan_report_consumer` / `typed_result_consumer` / `targeted_consumer` / `typed_result_saved`),**没有** `original_result_consumer`。于是:专业答卷被拒(作者字段错误)→ 记录一条 stage=`original_result_consumer` 的尝试 → 之后一次技术性前置失败再次发布 → 发布合并逻辑判定「上一条不是消费者尝试」→ **保留的消费者原因被置空** → Main 收到的 `ConsumerFailure` 为 nil,真实的「哪个文件、哪个字段」被静默吞掉。

**发现路径**:本轮 Lane 1 适配 `internal/cli/native_s9_consumer_retention_test.go` 时,退役掉「3/6 逐断言覆盖」门后,该测试仍红;追报错得到 `ConsumerFailure:<nil>`。

**蓝图依据**:`L4-hook-acceptance-checklist.md:52`「缺字段指向本人实际路径与字段」;`L3-S9-repair.md:238`「文件不存在、草稿未就绪或字段缺失时保留原稿,反馈实际文件与字段」;`L4-agent-guidance.md:288`「缓存可丢弃,失效只能多提示,不能吞错误、阻止接收或改变阶段」。

**解法**:消费者边界是**结构性事实**(谁在消费本人的原卷),不是阶段显示名。把这个事实**显式持久化**,不要再用阶段名反推:在 attempt 记录里增加一个消费者边界标记字段,由 `EnterConsumer` 置位、`ConsumerSucceeded` 清位;`nativeS9ConsumerAttempt` 改判这个字段。阶段名退回它本来的职责(诊断标签)。

**具体改动**:
- `internal/cli/native_s9_attempt.go:21-30` — `nativeS9Attempt` 增加 `ConsumerBoundary bool` 字段(带 json tag),显式记录「这条尝试停在消费者边界上」。
- `internal/cli/native_s9_attempt.go:56` — `EnterConsumer(stage string)` 同时置 `a.value.Attempt.ConsumerBoundary = true`。
- `internal/cli/native_s9_attempt.go:57-61` — `ConsumerSucceeded()` 同时置 `a.value.Attempt.ConsumerBoundary = false`(与既有 `a.consumer = false` 同步)。
- `internal/cli/native_s9_attempt.go:63-72` — `nativeS9ConsumerAttempt` 改为读 `a.ConsumerBoundary`;阶段名白名单作为**旧记录的兼容分支**保留(旧记录没有该字段时仍按旧名判定),避免历史诊断文件失去可读性。
- `internal/diagnosticstorage/storage.go:98-147` — `recognizesAttempt` 当前要求 attempt 字段集合精确匹配且值为字符串;新增的 bool 字段必须同步纳入识别(要么加进白名单并允许 bool,要么把标记编码成字符串 `"1"`/`""` 以避免改动识别器的值类型假设)。**推荐用字符串编码**,这样 `internal/diagnosticstorage` 的识别器改动最小。
- `internal/cli/native_s9_attempt.go:177-191` — 发布合并逻辑不变(它已经正确地按「上一条是不是消费者尝试」决定是否继承原因),只是判据从阶段名换成结构字段。
- `internal/cli/native_s9.go:510`、`internal/cli/native_s9.go:675`、`internal/cli/native_s9_original.go:95` — 三个生产消费边界的 `EnterConsumer` 调用点不改(阶段名继续用于诊断显示)。
- **最小替代方案**(若 owner 不接受改诊断记录形状):`internal/cli/native_s9_attempt.go:68` 的白名单补 `"original_result_consumer"`,一行修复;但必须同时补一条**防回归测试**,枚举本包所有 `EnterConsumer(...)` 实参并断言都在白名单内,否则下次改名会原样复发。

**验证**:
- 定向:`TestNativeS9OriginalStopCauseReachesMainAfterSourceCleanup`(本轮已把退役的覆盖门改判 STALE,剩余红即本缺陷)、`TestNativeS9ConsumerCauseSurvivesPrerequisiteAndConcurrentPublication`。
- 命令:`export GOCACHE=/tmp/adapt-cache && go test ./internal/cli/ -run 'TestNativeS9OriginalStopCauseReachesMainAfterSourceCleanup|TestNativeS9ConsumerCauseSurvivesPrerequisiteAndConcurrentPublication'`;`go test ./internal/diagnosticstorage/ -run '.'`。
- 新增回归测试(建议):`internal/cli` 内一条表驱动用例,断言 `nativeS9ConsumerAttempt` 对本包每个 `EnterConsumer` 实参都返回 true。

**风险**:
- 诊断记录形状变化会牵动 `internal/diagnosticstorage` 的识别器与 `internal/repair/artifact.go:238-241` 的控制面清单判定;若采用字符串编码方案,历史记录仍按旧形状识别,不需要迁移。
- 若只补白名单不改结构,同类缺陷在别的阶段改名时仍会复发 —— 这正是本组存在的意义,推荐结构性改法 + 字符串编码。
- 本组与 G06-C 共享 `internal/diagnosticstorage/storage.go`,两者必须串行落地(G06-C 先)。

---

### G14 — S8「同身份异文不覆盖已接受内容」的冲突判据缺失:已接受回执早退在读私有答卷之前,精确重放与异文重放不可区分

**问题**:S8 接收链在 `internal/investigation/hypothesis_result_receipt.go:71-73` 的「已接受 ⇒ 直接返回原回执」早退块,位于**读取本人私有答卷之前**。于是「同一份答卷的精确重放」与「同身份但内容已改写的重放」在生产代码里不可区分 —— 下游本可拒收的判据(`:119-120` 的 `bytes.Equal(existingReceipt.Answer, answerJSON)`、`:136` 原题面已关闭时拒收晚期答卷)对**结论性**已接受结果不可达。

原实现靠 `existingReceipt.AnswerSHA256 != answerSHA` 这一散列比较区分二者。该散列门禁已随重构下线,退役后**结构化判据没有补位**,冲突判据随之静默丢失 —— 这正是本轮「散列退役」必须配套「结构性事实补位」的典型反例。

**发现路径**:W18-s8-approval 工作包落地 G07-C(S8 人工批准模式门)后,定向测试 `TestS8GenerationTwoOriginalHookReceiptReplaySiblingAndGates` 的失败点从 `:189` 前移到 `:289`。G07-C 自身的断言(`:188-190` 未批准答卷不再被接受、`:191-193` 不产生 Runtime/journal 变更、`:261-262` 不写进 Case)**已全部通过**,撞上的是另一条蓝图条款。

**蓝图依据**:`L3-S8-finding-investigation.md:235`「重复事件复用原接受事实;同身份异文不覆盖已接受内容,真正新的判断由原修订工序处理」;`:445`「Case 已因本人题意或来源变化而失效 → 新内容不能冒充旧答卷重放」。

**解法**:补一个**无散列**的结构化冲突判据 —— 区分「私有答卷文件**缺失**」与「私有答卷文件**存在但内容与已接受答卷不同**」:前者必须重放成功(答卷文件被删除后仍能重放是既有契约),后者必须拒收。

**具体改动(待 owner 裁定后落地)**:
- `internal/investigation/hypothesis_result_receipt.go:71-73` — 早退块是「精确重放 / 异文重放」不可区分的根源,但**移动它**会破坏重放静默语义(见风险)。
- `internal/investigation/hypothesis_result_receipt.go:119-120` — `bytes.Equal(existingReceipt.Answer, answerJSON)` 是现成的无散列内容判据,只需让它对**结论性**已接受结果可达。
- `internal/investigation/hypothesis_result_receipt.go:136` — 原题面已关闭时拒收晚期答卷,是「同身份异文」的第二道结构性判据,同样因早退而不可达。

**三条候选路径(需 owner 拍板)**:
1. **裁定「重放不得依赖私有答卷」(维持现状)** → `:283-290` 属散列时代遗留的 STALE 断言,由 owner 改写测试。`:235` 的「不覆盖已接受内容」**仍然满足**(现码不写、复用已接受事实),争议点只在「把这次投递**报告**为 accepted」。**建议采纳**。
2. **裁定「同身份异文必须拒收」(`:283-290` 胜出)** → 另两个测试需由 owner 改写,且需新增无散列判据。硬阻塞:「答卷文件被删除」的重放**必须**成功,判据不能是「与已接受答卷内容不同」,必须额外区分「文件缺失」与「文件内容不同」—— 现码有意把两者合并。属**新组**,须新开工作包。
3. **按入口分流** —— 直接 API `SubmitIssuedHypothesisResult` 保持重放静默,Hook 入口 `ObserveHypothesisResultFrom` 拒收异文。可同时满足现有三个测试,但改变了对外 API 语义,且 `:445` 未限定入口,需要蓝图背书。

**验证**:
- 定向:`export GOCACHE=/tmp/adapt-cache && go test ./internal/investigation/ -run 'TestS8GenerationTwoOriginalHookReceiptReplaySiblingAndGates'`。
- 回归:`TestNativeS8StopReceivesOriginalCaseAnswerWithoutGenericPlanOrActorAlias`、`TestS8OriginalCaseReceiptRecoversSealedPendingCommit`、`TestS8IssueUsesActualGenerationTwoInvestigatorRegistration`。

**风险**:
- 同包三个测试对**完全相同**的场景(`result := "supported"` 同一结论性结果、同一条 `ReceiveIssuedHypothesisResult(..., continueUnresolved=true)`)要求**相反**结果 —— 任何单边取舍都会打破另外两个。这不是实现难度,是**契约冲突**,只能由 owner 裁定。
- 本组与 G07-C 同在 `internal/investigation/hypothesis_result_receipt.go`,两者必须串行落地(G07-C 先);G07-C 已落地并生效。
- 本组是**新开的归属缺口**:`internal/submission/hypothesis_result.go` 在本方案中无任何组,若最终走路径 1 或 3,该文件可能需要新开工作包。

---

## 9. 跨组冲突与落地批次

### 9.1 文件级重叠(必须合并成一个修复点)

| 生产文件 | 涉及组 | 处理 |
|---|---|---|
| `internal/cli/hook_reader.go` | G10-A、G09-D、G12-C | **合并为一次改动**:既收窄 Helper 早退范围(D6/D7),又让无法归属的事件产出「未知」诊断(G10-A),又让 teammate 载荷进原消费者(G12-C) |
| `internal/cli/native_review_hooks.go` | G08-A、G09-D | **合并为一次改动**:先恢复文本来源(G09-D),再把「已接收 ⇒ 早退」改为「不重复接收但仍诊断+出口准备」(G08-A) |
| `internal/hook/stopidle.go` | G08-B、G08-E | **合并为一次改动**:三处 warn/deny 恢复与旧 TeammateIdle 接线摘除同处一文件 |
| `internal/cli/repair_verifier_dispatch_questionnaire.go` | G08-D、G12-E | **合并为一次改动**:同一个 lifecycle 守卫 + 同一个准入集合 |
| `internal/cli/work_progress.go` | G05-A、G05-B | **合并为一次改动**:先落早退/半已知清空(G05-B),再落投影(G05-A) |
| `internal/cli/native_s9_original.go` | G06-B(:110)、G07-B(:80-88)、G01-A(:169) | 同文件三个不同 hunk,可同批但要分行提交 |
| `internal/cli/native_s9.go` | G06-B(:514)、G11-B(:411-413、:530-532) | 同文件两个不同 hunk |
| `internal/cli/workspace_delivery.go` | G12-G(:92-95、:128、:445)、G12-F(:150-169、:195-202) | 同文件两个不同 hunk |
| `internal/repair/issued_repair_result.go` | G12-G(:696、:566-580、:519)、G01-B(:696、:820、:946) | **强耦合**:`:696` 的硬编码 `Result:"pass"` 同时是两组的核心落点,必须一次改完 |
| `internal/submission/repair_result.go` | G12-G(:164-167)、G01-B(:528-538) | 同文件两个不同 hunk |
| `internal/cli/s9_handoff_projection.go` | G12-G(:151-166)、G01-B(:342-351) | 同文件两个不同 hunk |
| `internal/cli/native_main_return.go` | G01-B(:604-609)、G09-F(:174-181) | 同文件两个不同 hunk |
| `internal/diagnosticstorage/storage.go` | G06-C、G13(可选) | **串行**:G06-C 先,G13 后 |

### 9.2 落地批次(按文件不重叠切分,保证可二分)

| 批次 | 组 | 主要包 |
|---|---|---|
| B1 接收权威与 schema | G12-A、G12-B、G04-B、G07-A、G12-D、G04-A、G03-A | `internal/runtime`、`internal/submission`、`internal/workspace`、`internal/cli`、`internal/schema`、`internal/qualitygate` |
| B2 S9 交付坐标 | G01-A、G02-A、G12-F、G06-C、G06-B、G07-B、G11-B | `internal/cli`、`internal/repair`、`internal/diagnosticstorage` |
| B3 门禁与观察者 | G10-A+G09-D+G12-C、G08-A+G09-D、G08-B+G08-E、G08-C、G09-A、G09-B、G09-C | `internal/cli`、`internal/hook`、`internal/buildsurface`、`internal/assignment`、`internal/integration` |
| B4 字段与完成度 | G06-A、G12-G+G01-B、G05-A+G05-B、G07-C、G08-D+G12-E、G09-F、G10-B、G11-A、G11-C | `internal/workpacket`、`internal/repair`、`internal/cli`、`internal/investigation`、`internal/runtime` |
| B5 本轮新缺陷 | G13 | `internal/cli`、`internal/diagnosticstorage` |

批次内部仍需遵守「先落被依赖方,后落依赖方」:
- G12-C → G11-B(先扩大路由面,再收紧兜底条件)
- G09-D → G08-A(先能定位原责任,再去掉早退)
- G05-B → G05-A(先定早退,再定投影)
- G06-C → G13(先改识别器形状,再加消费者标记字段)
- G10-B 的结构性清单比对是「计划陈旧性」的**唯一**判定点,其它组若要判「输入变了」只能复用它,**不得**另起散列/摘要式门禁。

### 9.3 需 owner 裁定的事项(方案已给出默认建议,但需蓝图所有者确认)

| # | 事项 | 涉及组 | 本方案默认建议 |
|---|---|---|---|
| 1 | 「设计正文被改写后 S3 校验必须失败」在散列退役后无任何可用的结构性事实(`workpacket.Reference` 已不带 digest) | G12-A | 判为 STALE,改述为「权威根不符 / 登记关联变化 / 登记源文件缺失必须拒收;正文漂移交回原 owner」 |
| 2 | 「冻结输入正文被改写必须拒收」同上 —— 冻结输入只有位置没有原内容 | G12-B | 判为 STALE,改述为「冻结输入缺失/位置逃逸必须拒收」 |
| 3 | 「verdict=completed 但零单元记录」是否登记候选 | G12-G、G01-B | 判为**作者结构矛盾**,不登记候选;完成位不得从 checks 推算 PASS |
| 4 | 两条按 cwd 对立的用例(共享 checkout 不得借卷 vs 另一个 worktree 的自然事件仍应接收) | G11-B | 按事件类型切分,两条同时成立(已实测);若 owner 要彻底退役 Git 探测,则 guard 用例同步退役 |
| 5 | `internal/runtime/checkpoint.go:553-556` 的 revision CAS、`internal/runtime/recovery.go:761` 的 `haveRev > wantRev` | G10-B | 属本轮退役目标,但需同步改 `internal/runtime/checkpoint_checks_test.go:37-59`;本轮 B4 不动,建议单开一轮 |
| 6 | `internal/schema/assets/repair-result-candidate-provenance.schema.json:45` 的 pattern 不含分段形状(字段目前休眠) | G02-A | 暂不动(生产侧从不填充);若将来填充需同批放宽 |

---

## 10. 验证纪律

1. **绝不整包跑** `internal/cli`(39–47 分钟)。永远 `-run` 精确限定,并 `export GOCACHE=/tmp/adapt-cache`。
2. 每批落地后按「本批组的定向用例 → 同包相关回归 → 受影响包的单测」三层跑;`internal/cli` 的回归只跑本批相关用例集合。
3. `gofmt -l` 与 `go vet ./internal/cli/` 必须干净;`go vet` 通过后再跑测试。
4. 每次改动后跑 `git diff 9bf73ca..HEAD -- internal/` 的红线自查:**生产代码改动必须逐条对应本方案的 `file:line`**,不得夹带。
5. 禁止的「转绿」手法一律不用:删断言、放宽阈值、`t.Fatal`→`t.Log`、加 `t.Skip`、删测试文件。
6. 涉及散列退役的组,验收时必须显式确认:**没有新增任何 sha/hash/digest/revision/round/CAS 的比较、登记或资格条件**,历史散列字段只被原路径读取。

---

## 11. 本轮未闭环项

### G01-C —— S9 失败交接的跨请求 deadline 链:测试侧屏障已结构性死亡

**状态**:本轮**未适配**,测试保持红,已在测试文件头写明阻塞原因。

**原因**:该测试靠一个 `git` shim 作为屏障,当原 Plan 落进 journal 时让 `git` 阻塞 30 秒,从而让 8 秒的工作截止在「Plan 已接收」之后到期。d0d948a 把 `prepareSource` 里的 `merge-base`/`rev-parse HEAD` 一并退役(它们产出的是 base commit/散列坐标),**接收链里已不存在任何 git 调用**,屏障无法再触发。实测:父提交 `9bf73ca:internal/cli/native_s9.go:1165-1168` 的分支解析与 `:1182` 的登记,连同 `:765-774` 的 git 调用一起消失;HEAD 的 `internal/cli/native_s9.go` 与 `internal/cli/native_s9_original.go` 全链无 `workspace.Git`。

**为什么不能靠测试侧绕过**(三条路都实测过):
| 尝试 | 结果 |
|---|---|
| 请求边界已过期的工作上下文 | `internal/cli/native_hook_routes.go:264-267` 的 `nativeRoutingSnapshot` 先做 `ctx.Err()`,hook 直接 `return false, 0`,事件根本没进接收链 |
| 持有 Runtime 写锁制造阻塞 | `internal/runtime/store.go:604-608` 的 `Store.Snapshot()` 同样持锁,hook 的第一读就卡在锁上,拿不到 DeadlineExceeded |
| 用 FIFO/大文件制造慢读 | `internal/workpacket/files.go:104-111` 明确拒绝非普通文件;且读 Result 发生在 `ctx.Err()` 检查**之前**的 `internal/cli/native_s9_original.go:61`,阻塞点早于「Plan 已接收」 |

**需要的决定(生产侧,本轮不落)**:给接收链一个**确定性的、非散列**的阻塞/可观测点,让「工作截止在 Plan 已接收之后到期」这一状态可被跨请求复现。可选形态:
(a) 在 `internal/cli/native_s9_original.go:77` 的 `ctx.Err()` 检查之前引入一个**已登记责任身份**驱动的可注入等待点(不是 git、不是 delay、不是 revision);
(b) 让发布尾(`internal/cli/native_s9_attempt.go:125` 的 `filelock.Acquire` 路径)成为可观测的截止边界,并允许测试在**不持 Runtime 锁**的前提下冻结该尾段;
(c) owner 裁定该用例改述为「绝对截止点已过」的请求边界场景(蓝图 G26 已列此案),并相应放弃「精确输入坐标」与「flat consume 命令」两条断言。

在 owner 选定之前,本组保持红并如实标注,不以任何形式削弱断言。

---

## 12. 附:X-01 跨文件退役字段清单(W09 落地时实测核对)

G12-D / G07-A 恢复严格性时,另外三个 schema 的顶层 `additionalProperties` 仍被 `d0d948a` 放宽为 `true`。X-01 若要恢复它们的严格性,**必须同时给出下面的退役字段允许清单**,否则历史记录会被整体拒收(直接违反「不以旧散列作匹配/拒收」)。

| 文件 | 位置 | 需显式列出的退役字段(可选、无限定) |
|---|---|---|
| `internal/schema/assets/repair-plan-report-work-packet.schema.json` | 顶层 | `template_sha256` |
| 同上 | `$defs/binding`(现 `:59` 为 `true`) | `review_round`、`case_sha256`、`contract_sha256`、`session_sha256`、`plan_sha256`、`repair_assignment_sha256`、`manifest_assignment_sha256`、`task_sha256`、`manifest_sha256` |
| `internal/schema/assets/repair-result-work-packet.schema.json` | 顶层 | `template_sha256` |
| 同上 | `$defs/binding`(现 `:60` 为 `true`) | `plan_report_sha256`、`execution_sha256`、`rework_sha256` |
| `internal/schema/assets/review-investigation-case.schema.json` | 顶层(现 `:7` 为 `true`) | `baseline_digest`、`canonical_case_sha256`、`observation_batch_id`、`observation_batch_ref`、`observation_batch_sha256`、`repair_contract_sha256` |

> 清单由 W09 在落地时用 `git show d0d948a^`(放宽前版本)逐项核对得出,与本方案 §5 的 G12-D 交叉清单一致。
> **注意**:索引 schema 顶层的 `template_sha256` 指的是**索引行自身**的字段,与 work-packet / Case 顶层的 `template_sha256` 是不同对象,不重复计数。

---

## 13. 本轮裁定台账(总纲:L4-workflow-convergence.md:61)

> **总纲**(`L4-workflow-convergence.md:61`):「『保持质量要求』不等于永久保留实现它的每一道内部手续……重复校验、兼容封套和派生状态应删除或合并;**削减机器核验会改变系统可证明的保证,应明确交给谁判断,不能宣称保证完全不变**。」
>
> **本轮目标**:统一减负、退役 revision/sha/hash/digest/fingerprint/checksum/round/CAS 门禁。凡属**机器核验**的门禁一律退役,保证变化按该条交给「实际责任人 + 原专业复验」承担(`:281`);凡属**结构性事实**(权威根、原责任坐标、原来源存在性、原题面有效性、原责任是否未完成、原阶段是否正确)必须恢复。
>
> **裁定取向**:优先选「**生产侧一处收窄 ⇒ 既有断言自动转绿**」的方案(成本最低、减负最大);只有当断言本身编码了已退役门禁、且不存在等价的结构化判据时,才改写断言;legacy/frozen 接线按 `:71`「删除已被现行责任链取代的旧入口、适配、消费者、格式和**测试**」直接退役。

### 13.1 退役类:机器核验门禁一律退役

| 组 | 争议点 | 依据 | 裁定 | 落地动作 |
|---|---|---|---|---|
| G12-A / G12-B | 正文漂移的字节级比对 | `:281`「无 hash 不再单凭原路径和版本独立证明字节未被外部改动;实际责任人和原专业复验承担判断」;`:279`「不计算、登记或比较散列、Git OID、整树内容或逐文件字节来补证」 | **退役**正文漂移比对;只恢复权威根/原责任坐标/原来源存在性 | W08 已落地;相关漂移断言改判 STALE |
| G10-B | `checkpoint.go:553-556` revision CAS、`recovery.go:761` `haveRev > wantRev` | `:138`「不以散列作为写入配对或恢复条件」 | **退役**;改判「原阶段是否正确 / 原责任是否未完成 / 恢复来源是否存在」 | W15 落地;`internal/runtime/checkpoint_checks_test.go:37-59` 改判 STALE |
| G11-B | Targeted 的 checkout/来源绑定守卫(`nativeSharedCheckoutSourceError` 用 `git rev-parse` 判 checkout) | `:225`「**去掉按 Git 状态**、hash、旧 C/D、typed 或额外事件认定能否接收」;`L4-agent-dispatch-governance.md:57`「S7～S9 接收也**不核 Git／ignore 状态**」 | **完全退役**守卫(不是按事件类型切分)。C05 方向即正解 | W01:移除 `native_s9.go:411-413` 调用点与 `native_shared_source.go` 死代码;guard 用例改判 STALE;listener 用例保留为正解 |
| G02-C | 已收私人原卷的**字节漂移**检测(`accepted private original bytes changed`)**与 symlink 根替换检测**(`completed replay trusted a replacement symlink root`) | `L3-S9-repair.md:437`「系统**不靠每次回读旧原稿发现改写**或保证实时漂移完整性」;`:281` | **两者一并退役** —— 字节漂移与「回读旧原稿发现坐标已换」同族(`:437` 的「回读旧原稿发现改写」同时覆盖二者)。「已收后修改属于原专业修订或新尝试」由原修订工序承担 | **无需新增生产代码**。`internal/cli/workspace_missing_private_test.go:85-88,93-98` 与 `internal/cli/native_s9_recovery_test.go:381,695` 共 4 处断言改判 STALE,改为断言「已收事实不被覆盖、不产生新投递」(同文件 `repairDispatchAssertUnchanged` 已在断言该事实)。保留「文件缺失仍能重放」一半(`:100`「读取已收事实时不依赖私人文件仍存在」) |
| G12-G / G01-B | 零单元记录 | `:100`「阶段分流仅使用当前已接收答卷中直接表达的结论」 | 零单元**不构成完成证据**,也**不构成拒收理由**;完成度记为未知并交 Main | 完成度投影侧改为「未知」而非硬编码 pass |
| G02-A | `repair-result-candidate-provenance.schema.json:45` 的 path `pattern` | `:250`「历史记录内已存在的散列值只作为旧记录的原始数据保留」;`:154`「不得把已有真实观察变成拒收理由」 | `pattern` **不得作拒收资格**;显式列出允许的两种布局(flat `packet-<id>` 与分段 `packet-<p>/<seg>/…`) | 父任务直接改该 schema |

### 13.2 保留类:结构性判据必须恢复

| 组 | 裁定 | 依据 |
|---|---|---|
| G12-D / G07-A | 索引行是「原责任 + 定位符」封闭结构,顶层 `additionalProperties:false` **保留**;退役字段以空 schema 显式列为允许项 | `L4-hook-acceptance-checklist.md:15`「schema」纳入无散列验收 |
| G08-B | S7/S8/S9 原生责任未完成 ⇒ **deny + Recovery + Retry**,不降级为 warn | `:225`「S7/S8/S9 共用 … Stop 兜底的接收语义;只定位当前责任与原卷、返回接收或具体缺项」;`L4-hook-platform-wiring.md:162`「stop_hook_active 防循环不授予完成资格」 |
| G08-C | 静态波次未取得完整处置前,behavior 波次**不得**解锁 | `L3-S7-verification-round.md:149`「静态工作先取得完整处置,再进入行为执行」 |
| G05-A / G05-B | 进度投影与 actor 早退**必须**恢复 | `:136`「成本与可执行验收」;`L4-agent-guidance.md:288`「不能吞错误、阻止接收或改变阶段」 |
| G01-A / G06-B / G07-B / G11-A / G12-A / G12-B / G12-C / G12-E / G08-D / G08-E / G09-A / G09-B / G09-C / G09-D / G10-A / G04-A / G04-B / G03-A / G13 / G02-A / G07-C | 各组「问题→解法→改动」按 §1–§8 原文落地,本次不改判 | 各自条款已在 §1–§8 列明 |

### 13.3 测试侧处置:哪些改写、哪些退役、哪些不动

| 断言组 | 编码了什么 | 处置 | 依据 |
|---|---|---|---|
| `internal/runtime` `unknown-index-field` | d0d948a **反向新增**的「必须容忍 foreign field」特例(父提交要求必须被拒) | **已改判 STALE 并改写**为父提交口径(必须被拒) | 与 G12-D 同源、方向相反;`:71` |
| `internal/hook/responsibility_test.go` 7 处 `!= "warn"` | d0d948a 的 deny→warn 降级 | **改写**为父提交口径 `!= "deny"` | `:225` |
| `internal/hook/responsibility_test.go` 4 条 d0d948a 新增「不得阻断」用例 | 与降级同向 | **改写**为断言 deny + Recovery 路径 | `:225`;`L4-hook-platform-wiring.md:162` |
| `internal/cli/s7_guidance_test.go` 等 4 条 | 断言「波次门不存在」 | **改写**为断言波次门存在(静态未结算时 behavior 不解锁) | `L3-S7:149` |
| `internal/cli/native_s9_recovery_test.go` 2 条漂移断言 | 依赖已退役的 SHA256 字节比较 | **改写**为「已收事实不被覆盖」 | `L3-S9:437`;`:281` |
| W11 的 6 条 legacy 用例 | 依赖 legacy task-integrate / legacy 接收板 | **退役**(不是改写) | `:71`「删除 … 和**测试**」;`:293`「删除 frozen／legacy 的启动、观察、接收及恢复接线,而非增加兼容层」 |
| `internal/investigation/s8_late_same_case_stage_test.go:54-57` setup 段 | 要求「所有 owner 的答卷都被接受」,含 `plan_approval_required` 派发的 `g2-03` | **改写**(仅 setup 段):审批模式 owner 在真实合同批准前的答卷**必须被拒**(即 G07-C 断言),非审批模式 owner 仍必须被接受;测试真正要断言的后半段(`:98-122`:真实批准后迟到答卷被接受、Case/批准/生命周期未变、迟到结果已记录、重放幂等)**一条不动** | `L3-S8:215`「已有匹配授权时原批准消费者记录并应用既存真实决定」;`L3-S8:81`「确需批准时记录原批准主体的真实决定」;§13.2 保留 G07-C |
| `internal/workpacket` 3 条「未知键 / 缺声明字段不产生字段错误」 | 编码了「未知键拒收」「缺键拒收」 | **不动测试** —— 改生产使其满足(见 13.4) | `:154`「允许无关扩展」;`:240`「Result 的真实 blocked／contract_conflict 可以独立先接收」 |

### 13.4 最高性价比裁定:G06-A 收窄(生产一处改,3 条既有断言自动转绿)

W21 的实现把「未知键」与「缺声明键」都当成拒收理由,与两条蓝图条款冲突:

- `L4-workflow-convergence.md:154`「**允许无关扩展**……可选字段、字段拼法和枚举不得把已有真实观察变成拒收理由」
- `L3-S9-repair.md:240`「Result 的真实 `blocked`／`contract_conflict` 可以**独立先接收**,不能因为 Plan 缺失、未就绪或存在非关键缺项而跳过 Result」

**裁定**(`internal/workpacket/repair_result.go` + `repair_plan_report.go`):

| 情形 | 处置 | 依据 |
|---|---|---|
| 字段值为 `null` | **拒收**,给 `/answer/<字段>` 字段级指针 | `:238`「反馈实际文件与字段」;`:232`「结构错误仍需修正」;W21 的空字符串约定 |
| 枚举越界(`red_checks[].result`) | **拒收**,给 `/answer/red_checks/<i>/result` 指针 | `:232` |
| **未知键** | **不拒收** | `:154`「允许无关扩展」 |
| **缺声明键** | **不拒收**(字段级指针机制保留,供 null/枚举两条路径用) | `:240` blocked 独立接收;`:238`「不要求补 … 其他必填项」 |

> **⚠ 本条只适用于 canonical schema 解码层(`internal/workpacket`),不适用于 flat 私人作者文件入口。**
>
> 父提交 `9bf73ca` 的口径是**两套严格性并存**,不可混为一谈:
> - `internal/workpacket` 的 canonical Result/PlanReport 解码:**宽松**(无 `DisallowUnknownFields`)—— 未知键属于「无关扩展」,不得成为拒收理由(`:154`)。
> - `internal/cli/native_s9.go` 的 **flat 作者文件入口**:**自带严格解码**(`git show 9bf73ca:internal/cli/native_s9.go:891-896` 有 `decoder.DisallowUnknownFields()`),父提交注释写明理由是「Only author-owned professional fields are transferred into the original questionnaire. **No platform identity or result metadata is model-authored.**」—— 顶层只允许**作者自己的专业字段**,平台身份/结果元数据不是模型写的。
>
> 因此:canonical 解码放宽后,**flat 入口必须自己补回严格解码**,否则 `internal/cli/native_s9_artifact_feedback_test.go` 的 `unknown Result field` 子用例(来自 `82a7146`,**早于**重构,编码的是父提交契约)会失去被拒路径。
> 落点:`internal/cli/native_s9.go:514`(PlanReport)、`internal/cli/native_s9_original.go:121`(Result)—— 两处都在 W01 的独占文件内。
> 判据:**未知键 ⇒ 拒收并给「keep professional fields at the top level」反馈;类型错配 ⇒ 交 canonical 校验器给精确 `/answer/<字段>` 指针。**

**成本收益**:生产侧收窄一处 ⇒ `internal/workpacket` 3 条既有断言(未知键 `checks`、缺 `migration_ref`、`{"verdict":"blocked"}`)**自动转绿,一条测试都不用改**;同时保留 G06-A 的真正价值(null/枚举的字段级指针),而那正是 G06-B 映射器所需的形态。

### 13.5 新增组(本轮现场发现)

- **G14**(`internal/investigation/hypothesis_result_receipt.go:71-73` 早退块):实现 `L4-workflow-convergence.md:112` 的**专业语义比较** —— 以同一原消费者的专业语义比较当前原卷与其当前已收对象,不用字节、散列、时间、ID 或 Runtime revision 判作新判断;文件缺失 ⇒ 复用已收事实(`:100`「读取已收事实时不依赖私人文件仍存在」);专业语义不等价 ⇒ **不覆盖已接受内容**(`L3-S8:235`),按 `:342`「真实新表达仍沿原业务处理」交原修订工序。目标是让 3 条互相矛盾的测试同时成立。
- **G17**(写路径 CAS 族 —— **按蓝图 §4 分类,不整体退役**):全仓 `ExpectedRevision` 分布 **68 个非测试文件、224 处**,实参多为真实快照值(`snapshot.Revision` / `resolvedRevision` / `commitRevision` 等),**不是常年 `-1` 的哑管道**,是活门禁。但**不得整体退役**,`L4-revision-usage.md` 已把 CAS 的适用边界写死:「CAS 是比较并交换…**它适合有意关联旧快照的外部集成、恢复或必要的内部边界,不是业务完成本身**」(`:85`)、「普通专业保存,没有要求整个 Runtime 保持原提交 → 用支持该语义的**当前值入口**,检查原业务对象与责任」(`:89`)、「高级命令可以保留 `--expected-revision`,正常入口应把它作为**可选断言**」(`:79`)、「底层 `Update` 的**负值请求也进入当前值路径**」(`:71`)、「显式调用者主动要求的**全局 CAS 保持原语义**」(`:241`)。⇒ 裁定为**四分类**:①**业务完成路径**上的 CAS(普通专业保存/接收/完成,调用方并不真需要「整份 Runtime 保持原提交」)⇒ **B 档,退役为当前值入口**;②外部集成有意绑定旧快照 ⇒ A 档保留;③恢复/中断接续 ⇒ A 档保留;④角色不明者 ⇒ C 档上报。**退役方向不是删检查**,而是按 `L4-state-transition-core.md:193`「原 Writer 锁内核 Runtime、原责任和修订;**旧快照重读当前事实**」把保证落到锁内重读 + 按当前事实重新判定。
  - **关键前置(本轮新发现)**:许多调用点的**事件 id 与幂等键里嵌了 revision**(如 `internal/repair/runtime.go:335/865-867`、`internal/investigation/hypothesis_result_receipt.go:176/246-250`、`internal/assignment/lifecycle.go:302/306`、`internal/assignment/completion.go:218/222`、`internal/assignment/task_lifecycle.go:88/92`、`internal/assignment/register.go:933/937`、`internal/runtime/evidence.go:174/178`、`internal/runtime/change.go:58/62`、`internal/review/supplement.go:206/210`、`internal/investigation/contract.go:256/260`、`internal/investigation/intake.go:200`、`internal/transition/engine.go:213`)。id 依赖**预分配的序号** ⇒ 调用方必须先知道 revision ⇒ **这正是逼着写路径用 CAS 的根因**。所以「业务完成路径退役 CAS」与「事件 id / 幂等键不再嵌入写计数」是**同一个改造的两面**,必须一起做,否则改不动。
  - **本轮处置**:立案记账 + 给出口径与四分类 + 记下这个前置依赖;**不改实现**(68 文件 224 处 + 事件 id 形状变更会波及全部幂等查找与历史兼容,搭进已改 60+ 文件的批次会失控;按 `L4-workflow-convergence.md:61`「削减机器核验会改变系统可证明的保证,应明确交给谁判断」,写路径并发保证的变更单独交给实际责任人裁定)。**下一轮专项**,且必须先出「id 形状迁移 + 历史事件兼容读取」的方案再动。
- **G16**(S8 消费侧缺少「不可变 Case 世系连续性」判据):`internal/investigation/hypothesis_result_registration_test.go:396` 要求消费侧错误信息含 `continuity`,`:402` 要求签发侧同样拒收;而签发侧 `internal/investigation/hypothesis_result_packet.go:514-531` 的 `verifyHypothesisResultPacketCurrent` **自身注释**即写明「必须证明完整不可变 Case 世系,不能只比当前投影」—— 该机制在生产中**不存在**(现实现只比当前投影)。全仓无任何 S8 错误信息含 `continuity`。裁定:**本轮立案记账、不实现** —— 它需要签发侧与消费侧两处联动的新机制,签发侧不在任何工作包的独占文件内;且与 `L3-S8:408`「旧轮晚到结果沿原责任接收」直接张力,实现口径(能否用非散列的 Case 坐标表达世系)需 owner 裁定。该断言按 DEFECT 处置(只记录、保持红)。

- **G15**(`internal/hook/stopidle.go` `ReviewAssignmentBlocked`):d0d948a 把「原来源证明」改写成「manifest 行 `activity == blocked` 状态串」,并删除 `reviewSiteLostBlocker` ⇒ 已验证的**原始** blocker 不再使 S7 Reviewer 责任成立。裁定:恢复原来源判定(原始 `work_blocked` 事件 / S7 `site_lost` blocker 事务),不在 manifest 状态串上判定。
- **W05 的 frozen 场景**(`internal/cli/native_stop_pending_test.go`):S9 绑定通知的 deny 触发条件**收窄**为「绑定本身不可确定」(`repairAssignmentID == "" || agent.RepairAssignmentID != repairAssignmentID`),使「绑定已知、仅旧 execution/source-pin 证明缺失」的场景不落进绑定未知分支。依据 `:342`「删除旧 C／D、frozen …… source pin 的并行接收、来源证明及恢复接线」;`:300`「删除仅为旧 frozen execution 准备的 commit／适配及其兼容恢复接线」。此收窄**不是**弱化生产行为,而是把 deny 限定在真正无法确定绑定时。
- **W18 的授权门**(`internal/investigation/hypothesis_result_receipt.go`):授权门应按「**授权事实存在**」判定,而非「当前仍处于 working 态」,避免误拒「曾合法激活、工作过、之后离开 working 态」的 Investigator 的晚期答卷;同时不得把晚期路径重新开放成未授权可写。

### 13.6 本轮对 worker 提问的裁定(已定,无需再问)

| 提出方 | 问题 | 裁定 |
|---|---|---|
| W05 | `:105` 分支(agent context 不可用)用 `repairAssignmentID == ""` 作为「绑定本身不可确定」的等价形式,是否认可 | **认可**。该分支没有 `agent` 可比较,`repairAssignmentID == ""` 是同一判据的等价形式;「绑定已知、仅旧 source-pin 证明缺失」不落进绑定未知分支,与 `:342`「删除旧 source pin 的来源证明接线」一致 |
| W05 | frozen 场景新增的 `repairResultAssignmentUnfinishedNotice` 是本包唯一一处 `warn` | **认可**。这不是 G08-B 的缺陷(绑定真正不可确定的场景仍 deny),而是 `:342`/`:83` 要求的「退役旧门 + 仍报诚实事实」:绑定已知但旧 source-pin 证明已退役时,系统仍须如实报出「尚未被确立为已接收/已完成」,但不阻断。**非阻断事实投影不构成降级** |
| W05 | `reviewProofJournalHasSiteLost` 保留 `baseline_generation` 坐标是否要一并去掉 | **保留**。`baseline_generation` 是**结构性基线坐标**(绑住「该 journal 事务属于哪个基线」),不属 revision / sha / hash / digest / fingerprint / checksum / round / CAS 任一退役门禁家族,也不参与资格判定。去掉它会失去「原阶段是否正确」这一判据 |
| W05 | artifact 认证由父提交的 `sha256(artifactBytes) == evidence["sha256"]` 改为 artifact 自身 envelope 字段对账 | **认可**。散列门禁退役后,原件认证改由**原件自身携带的结构性字段**与证据行对账(evidence_id / kind / runtime_id / assignment_id / producer_agent_id / finding_id / reason / queue_reason / scope_refs),这正是「恢复结构事实」而非「恢复旧门禁」 |
| W03 | G12-G 的登记门用「completed + 至少一条单元」而非「全部单元 pass」 | **认可**。全部单元 pass 是**完成**判据,不是**接收**判据;若用作登记门会拒收「单元 fail」的候选,与 G01-B 自身要求(「候选照常登记,完成位推导为 fail」)及既有断言「不得登记候选」的语义直接冲突。严格守卫保留在最终 Result 写入点 |
| W03 | `internal/cli/workspace_delivery.go:338-340` `currentRepairResultPacketForDeliveryContext` 返回 `typed=false` + 「original S9 result association is unavailable」,断言要求 `typed=true` + 含「no legacy fallback」 | **已复核为误判,按 W24 改判。** 追父提交 `9bf73ca:512-521` 发现:父提交在 `candidate.Provenance == nil` 时**委派 typed 选择器**,并返回 `(packet, true, error("…no legacy fallback"))` —— 即「**已确定**该责任有 typed 包,但其原始 provenance 不可用」⇒ 明确拒绝 + 明说不退回 legacy。断言方向**与蓝图一致**,错的是 d0d948a 删掉委派后的笼统硬失败。W24 已按父提交口径恢复委派,两条验收 PASS。**该处不是 STALE 测试,是生产缺口。** |
| W21 | canonical 层与 flat 入口的严格性边界 | **已定**(见 §13.4 的加注):canonical `internal/workpacket` 解码**宽松**(恢复父提交),flat 私人作者文件入口**自带严格解码**(恢复父提交 `:891-896`)。两套严格性并存,不可混为一谈 |

### 13.7 本轮未采纳的「为了变绿而恢复门禁」提案(留档)

| 提案 | 不采纳理由 |
|---|---|
| 为让 `internal/runtime` `unknown-index-field` 通过而把索引 schema 顶层改回 `additionalProperties: true` | 与 G12-D 相反。父提交 `9bf73ca` 即为 `false`,且该用例在父提交就要求 foreign field 被拒 —— 是 d0d948a 放宽 schema 后反向改写了断言。已按父提交口径改写断言 |
| 为让 `unknown Result field` 通过而把 canonical Result 解码改回严格 | 与父提交相反。父提交 canonical 层本就宽松、严格性在 flat 入口。已改为**在 flat 入口补回严格解码**(W01 落地) |
| 为让 `TestNativeStopPendingFrozenS9RetainsSamePublicStopGate` 通过而恢复旧 source-pin 证明门 | 与 `:342`「删除旧 source pin 的来源证明接线」相反。已改为收窄 deny 触发条件为「绑定本身不可确定」 |
| 为让 G02-C 的漂移断言通过而恢复 SHA256 比对 | 与退役规则正面冲突。已按 `:437` 退役 |
| 为让 W11 的 legacy 用例通过而恢复 legacy task-integrate 接线 | 与 `:71`/`:293` 相反。已改为退役该批用例 |

---

## 14. 全 stage 计数器 / revision 残留门禁审计台账

### 14.1 审计方法

- **目标**:排查「除本轮已退役家族外,各 stage 还残留哪些计数器 / revision 门禁」。
- **分片**:按 stage 切成 10 片(S0/S1、S2/S3、S4/S5、S6、S7、S8、S9、S10/S11、`internal/cli` 全域、写路径 CAS 总账),每片一名只读审计员,共用协议 `audit-work/fix-waves/briefs/AUDIT-revision-counter-protocol.md`。
- **分类权威**:`L4-revision-usage.md:15-26` 的「几种关联轴分别回答什么」轴表。判档 **A 保留(结构性事实)/ B 残留门禁(退役候选)/ C 上报(需父任务裁定)**。
- **口径修正**:协议 §3 明确 —— **schema 字段存在性要求不是比较门禁**;**历史散列字段只按原路径读取、原样记录**是允许的;**`sequence`/`generation`/`phase_revision`/对象 revision 之间的身份绑定**是 A 档。
- **本轮已退役家族(W29a/W29b/W15/W23/W27/W05/W12/W13/W14/W17/W18/W21/W24-W28)不在审计范围内,但各片仍需复核「是否已退役」并找漏网**。

### 14.2 写路径 CAS 总账(四类分布,父任务裁定)

| 类 | 规模 | 代表位置 | 裁定 |
|---|---|---|---|
| **① 业务完成路径** | ≈45 比较点 + ≈49 纯回灌点 | `review/submit.go:337`、`review/revise.go:82`、`review/register.go:139/309`、`investigation/{intake.go:60,contract.go:75,route_consume.go:36,case_workflow.go:564,hypothesis_result_receipt.go:145}`、`assignment/{auto_chain.go(8),completion_canonical.go:189,452,571,lifecycle.go:478}`、`submission/*`(9)、`cli/*`(≈24)、`repair/{issued_*(4),runtime.go(4)}` | **B 档 → G18 专项(下一轮)** |
| **② 外部集成有意绑定旧快照** | ≈18 | `cli/controller.go:1846/1894`、`integration/types.go:141-147/260`、`cli/human_release_questionnaire.go:220`、`runtime/store.go:944-962/1192-1211` | **A 档保留**(`:90`/`:241`) |
| **③ 恢复 / 中断续接** | ≈16 | `runtime/store.go:958/975`、`runtime/relocation.go:186`、`cli/recover_command.go:740`、`review/submit.go:109-124`、`investigation/hypothesis_result_receipt.go:229-230`、`controller/cycle.go:307/560` | **A 档保留**(`:85`/`:198`) |
| **④ 语义不明 / 可疑** | 6 | `cli/run.go:1926`、`cli/investigation_lead_contract.go:245`、`review/supplement.go:81`、`repair/issued_repair_result_blocker.go:94`、`issued_repair_result_resolution.go:150`、`repair/runtime.go:360` | **C 档 → 本轮修(§14.6)** |

**负值(当前值路径)实际使用比例**:负值 ≈31 处 / 真实快照值 ≈49 处 ⇒ **当前值路径仅约 38%,约 62% 仍传真实 revision**。全部 15 个 CLI flag 站点默认 `-1` ✓ 已合规。

**替代判据(不是删检查)**:退役 CAS 后「不覆盖已收事实」由三件套承担 ——(a) 领域级「已收 / 已消费」行检查(`runtime/evidence.go:267-285`、`review/submit.go:154-159,307-311`);(b) `EventID`+`IdempotencyKey` 去重(`runtime/store.go:1220-1224`);(c) 对象版本断言(`ExpectedCaseRevision`、Plan 指针 revision、`ExpectedRecord`、`ExpectedRuntimeID`)。蓝图 `L4-revision-usage.md:73`「不能用宽松 Update 接受过期内容」⇒ **替代判据必须是「当前值入口 + 原对象/来源条件」,不是裸 `UpdateCurrent`**。

**正确范式(库内已有)**:`internal/review/submit.go:109-124` `continueIssuedResultAfterPeer` —— stale 后由系统重读当前事实续未完成部分,**不改号、不让作者猜**。

### 14.3 逐 stage 结论

| stage | 域 | B 档 | C 档 | 结论 |
|---|---|---|---|---|
| S0/S1 | `transition` / `workspace` / `fileview` / runtime 绑定解绑 | 0 | 2 | A 档为主。`engine.go:213` 幂等键只有 `transition+revision`、无责任维度 → 上报(撞键风险) |
| S2/S3 | `submission` / `workpacket` / `semantic` / `sharedmodel` | 6(`repair_result_blocker.go:35/47`、`repair_result_resolution.go:92/104`、`repair_plan_report.go:115`、`repair_result.go:181` —— 全是**适配层回灌** `snapshot.Revision`,传 `-1` 即可) | 1 | `phase_revision` / `StageEntry` / `RevisionPacketID` 全部 A 档保留;`phase_revision` 是阶段入口(单调轴),`RevisionPacketID` 未被当通用许可 |
| S4/S5 | `review` / `hookctx` / `assignmentrecord` | 6(`submit.go:337`、`revise.go:82`、`register.go:139/309`、`plan_reception.go:41`、`result_packet.go:101`) | 0 | 退役需与 `runtime` 锁内重校配套 |
| S6 | `assignment` / `integration` / `plancheckpoint` 等 | 12(事件 id 族 6 + CAS 3 + 计数连续性 1 + `lifecycle.go:484`) | 3 | 事件 id 族统一入 G18 |
| S7 | `reviewmeta` / `hook` / `submission/review_result` | **0** | 0 | **本域无残留门禁**;仅轮数语汇命名遗留(`review_readiness.go:75/93`、`stopidle.go:383`) |
| S8 | `investigation` | 6(全部是「CAS + 事件 id 嵌写计数」成对出现) | 0 | 两件事同一改造的两面,必须同批 |
| S9 | `repair` + `runtime/repair_result_*` | 1(W30 已修) | 3 | 极性反转三处本轮修 |
| S10/S11 | `acceptance` / `qualitygate` / `controller` / `controlproof` | **0** | 2 | **本域无残留门禁**;S11 发布门本就不靠 revision 判申请有效性(按证据记录 + 代际 + 计划身份) |
| 全域 | `internal/cli` | 18 | 3 | 事件 id/幂等键族 ≈20 处 + CAS 回灌 ≈24 处 |

### 14.4 父任务复核:推翻 / 采纳审计员判定

1. **推翻** —— `internal/hook/hypothesis_result.go:122` 与 `internal/cli/s8_questionnaire_listener.go:122` 的 `receipt.SuccessorCaseRevision > binding.CaseRevision`。审计员判「方向反了,应验证指向当前 Case 版本」。**父任务复核后不采纳**:生产侧 `internal/investigation/case_workflow.go:679` 把后继 Case 递增为 `nextRevision`,而 `internal/runtime/hypothesis_result_checkpoint.go:68` 在结果落地**前**记 `SuccessorCaseRevision = p.Binding.CaseRevision` ⇒ `>` 表达的是「后继 Case 必须**严格晚于**绑定版本」这一既定关系,属 Case 对象版本轴上的**关系约束(A 档)**,不是新鲜度门;且同一条表达式已用 `PacketID`/`AssignmentID`/`TaskID`/`ManifestRef` 把收据钉死在本 packet,旧收据混不进来。改方向会直接破坏「S8 结果已推进 Case」这一事实的判定。**判 A 档保留。**
2. **纠正审计员** —— CAS 总账报「`:193` 行号与所引原文不符」。该审计员核的是 `L4-revision-usage.md:193`(实为「实际验证计划或代际改变」条);而 `:193` 引自**另一个文件** `L4-state-transition-core.md:193`「并发提交或重复事件 | 原 Writer 锁内核 Runtime、原责任和修订;**旧快照重读当前事实**;领域消费者复用已收内容 | 不覆盖已收事实,不以散列键判同答」—— **引用无误**。为免后续 wave 引错行,本轮统一口径为 **`L4-state-transition-core.md:193`(并发保证怎么落地)+ `L4-revision-usage.md:191`(无关提交增加 → 支持当前值的原入口继续,不让作者猜数字)+ `:198`(状态与事件不成对或有 pending → 沿原 Writer/恢复流程处理,不把它当普通 CAS 冲突)**。
3. **采纳** —— S9 审计员报的漏网:`internal/runtime/repair_result_blocker_checkpoint.go:437` 仍判 `before < 0 || after != before+1` ⇒ **W30 已修**(父任务亲手)。
4. **采纳** —— S0/S1 审计员报的 `internal/runtime/checkpoint.go:204/237/500-509` 的 manifest `revision` 一致性比较残留 ⇒ 入 §14.6 复核。
5. **采纳** —— W29c 上报的 `internal/runtime/relocation.go:128` `pending.PreviousRevision != before.Revision` ⇒ B 档,同一条 `if` 内已有幂等键 + 目标内容一致性兜底 ⇒ 可退役。

### 14.5 G18 立项:业务完成路径 CAS → 当前值入口 + 事件 id/幂等键去写计数

- **范围**:①类 45 比较点 + 49 回灌点 + 事件 id/幂等键族 ≈35 处(跨 `assignment` / `cli` / `investigation` / `repair` / `runtime` / `submission` / `transition`)。
- **核心发现**:**写计数外泄成领域身份,是逼着写路径必须用 CAS 的根因**。事件 id 依赖「预分配的序号」⇒ 调用方必须先知道 revision ⇒ 必须传 CAS。⇒ 这两件事是**同一个改造的两面**,必须一起做,否则改不动。
- **执行风险(必须整体做,不可逐点退役)**:事件 id 去掉 `r%d` 若只改一半,会造成**幂等键碰撞**(同一语义不同数字 = 重复提交);`internal/runtime/store.go:1223` 的 `event_id already exists` 唯一性检查会出现断层。
- **迁移顺序**:① 先纯回灌层(`submission/*` 9 处 + `cli/*` ≈24 处,改传 `-1`,不动语义)→ ② 再把领域比较点换成「已收行 + 事件去重 + 对象版本」三件套 → ③ 最后改事件 id/幂等键形状(**新增事件用语义坐标,历史 id 按原路径只读、不重算、不改写**)。
- **约束**:`L4-revision-usage.md:192`「原对象新增真实修订 → 沿原版本／修订路径重新交付或重判,**不能只替换全局序号**」—— 不得用「换一个序号」冒充接续。

### 14.6 本轮立即修的清单

| 编号 | 位置 | 判档 | 处置 |
|---|---|---|---|
| W30 | `internal/runtime/repair_result_blocker_checkpoint.go:437` | B | **已修**(父任务亲手):退役 `before < 0`、`after != before+1`;保留字段存在性(schema 形状)、`sequence`、`generation`、证据引用 |
| W31a | `internal/repair/issued_repair_result_blocker.go:94`、`issued_repair_result_resolution.go:150`、`internal/repair/runtime.go:360` | C | 极性反转:负值(=省略)不再被判 stale;显式非负仍是具体断言 |
| W31b | `internal/cli/run.go:1926`、`internal/cli/investigation_lead_contract.go:245` | C | 适配器越权:省略保持省略,不擅自回灌真实 revision |
| W31c | `internal/review/supplement.go:81` | C | 零值语义统一为 `>=0`(对照 `:78`) |
| W31d | `internal/cli/recover_command.go:634` | C | `ExpectedRevision: 0` 的语义裁定并写明注释 |
| W32 | `internal/runtime/relocation.go:128` | B | 退役 `pending.PreviousRevision != before.Revision` |
| W33 | `internal/runtime/checkpoint.go:204/237/500-509` | 待判 | manifest `revision` 一致性比较复核 |
| W34 | `internal/cli/task_receipt.go:106` 的 `bytes.Equal(before, after)` | B | 字节比较属 `L4-workflow-convergence.md:281` 退役家族 |

### 14.7 C 档上报与逐条裁定(裁定见 §14.11)

| 位置 | 疑点 | 不做的理由 |
|---|---|---|
| `internal/assignmentresult/accepted.go:325-332/266` | Result 版本要求**连续无缺口** + `ResultRevision < 1` 下界 | **已裁定:保留** —— `ResultRevision` 由接收端派生(`len(history)+1`),不是作者传入的期望值,不构成写路径 CAS;守的是「已收历史无静默丢失 + 提交不重复」。原顾虑「须先补 submission_key 锚点」已满足。详见 §14.11 C-A |
| `internal/plancheckpoint/validate.go:111-113` | `assignment_revision != 1` 即拒收 | **已裁定:保留检查、修正注释** —— 与 ReviewPlan 指针 revision **是同一轴**(全仓四处一律是 Assignment Record 的域版本);字面量 1 的依据是 manifest Assignment 登记即 `Revision: 1`。真正的两轴混用在注释措辞。详见 §14.11 C-B |
| **`internal/transition/engine.go:207`、`:213`** | 事件 id `evt-<transition>-r<commitRevision+1>` 与幂等键 `runtime:<transition>:<commitRevision>` —— 写计数写进领域身份 | ①**本轮新证据(W32)**:业务保存路径的 CAS 退役后,`commitRevision` 取**接收端自己锁前快照**的值,并发窗口内它可能**低于真正提交的 `after_revision`**,编号与 `after_revision` 错开;已核实全仓**没有任何**按 `-r<N>` 与 `after_revision` 做等值校验的代码(`grep '"-r%d"'` 无命中),journal 的权威坐标是 `sequence` + `after_revision`(`internal/runtime/store.go:1250-1262`、`:1568-1569`),故**当前不构成功能破坏**,但记录本身在声称一个可能不准确的序号。②**周期重置风险**:`L4-revision-usage.md:29`「周期身份重置有自己的协议,不都表现为『同一个数字加一』」⇒ 计数器重置后 `runtime:<transition>:<commitRevision>` 可能与重置前的事件撞键。③处置:与 §14.7 的 `result_packet.go:735` 一并归 **G18 第 3 步**(事件 id/幂等键形状)——**语义幂等键必须同时带上责任坐标与周期坐标**,否则既会撞键、也会让编号错开 |
| `internal/hookctx/loader.go:1056` | `document.Revision < CaseRevision` 方向 | **已裁定:保留,方向 `<` 正确** —— Case 在飞行中可正当修订,必须是下界而非等值;第二段等值是「活文件 ↔ 活索引」自洽性核对。两处守的是「原题面有效性」。详见 §14.11 C-C |
| **`internal/controller/replay.go:210`、`:219-224`** | 恢复重放用 **revision 递增**判「无进展」(`after.Revision <= snapshot.Revision`),并用 `seenRevisions` 判「状态重复」 | **已裁定并本轮已改** —— `:210` 的 revision 判据**冗余**(同处 `TransitionCommitted` 已证明写过、游标判据已证明生命周期动了)⇒ 退役;`:219-224` 的 `seenRevisions` **可证明不可达**(同一 revision 只对应一个状态、状态决定游标,而游标检查先执行)⇒ 整支退役、行为不变。游标去重本来就已在生效(`:217-221`),故「重放不产生第二次恢复状态机」(`:105`)的保证**未被动摇**。配套新增守卫用例 `TestRecoveryReplayAdvancesTheCursorAndNeverRepeatsIt`。详见 §14.11 C-D |
| `internal/cli/s6_observation.go:26` vs `controller.go:1251` | 里程碑绑定写计数 vs「observed revision 不是状态变更」自相矛盾 | **已裁定** —— 矛盾在**注释与实现的说法不一致**,不在行为:内容语义身份(`stableMilestoneProjection`)本就排除写计数;journal 幂等键必须带写计数是**承重**的(S6 观察失效时内容不变、必须重写,只有写计数能把两次写入分开)。本轮把两处与实现不符的注释改回真话;把写计数换成结构身份(journal 末事件 id)归 **G18 第 3 步**。详见 §14.11 C-E |
| `internal/qualitygate/review_round_gates.go` 门名含 `ROUND`;`internal/assignment/review_readiness.go:75/93`;`internal/hook/stopidle.go:383` | 轮数语汇命名遗留 | 语义已无轮次,非功能门禁;命名清理归入 G18 之后的整洁批次 |
| `internal/cli/recover_command.go:634` 的 `0` | 零值是具体断言还是省略 | 归 W31d 裁定 |
| **`internal/review/result_packet.go:735`** | `result.ResultID = fmt.Sprintf("%s-attempt-%d", p.ResultID, snapshot.Revision+1)` —— 用**全局 Runtime revision** 派生结果身份后缀 | 与 `L4-workflow-convergence.md:242`「每次 Hook 重算的可变 Runtime revision…不能代替稳定 Plan 身份」正面冲突:同一逻辑结果在无关提交后会拿到不同 ResultID,幂等去重随之分叉。这是 G18 的一个具体实例。退役需要设计一个**稳定坐标**(原接收事实 / 已记录的重新交付次数字段),不能只删这一行 —— 改法未设计前不动 |
| **`internal/review/result_packet.go:735`** | `result.ResultID = fmt.Sprintf("%s-attempt-%d", p.ResultID, snapshot.Revision+1)` —— 用**全局 Runtime revision** 派生领域身份后缀 | 违反 `L4-workflow-convergence.md:242`「固定转换标签、每次 Hook 重算的**可变 Runtime revision** 或改名后的数字**都不能代替稳定 Plan 身份**」;且改成当前值路径后,这个序号可能**从未被分配过**,记录会声称一个不存在的 revision。**本轮不改生产代码** —— 新身份方案必须同时满足三条约束:①不嵌入任何 Runtime revision(`:242`);②不累计轮次序号(`L4-revision-usage.md:20`「删除旧 `review.round` 生产、消费及兼容读取,不生成或累计轮数」、`:123`);③**不得与上一次交付的身份或其答卷保存路径相撞** —— `ResultID` 决定答卷落在哪个文件,同 id 会**覆盖已收原卷**(违「不能覆盖已收事实」)。**关键证据(供 G18 第 3 步裁定)**:`internal/review/submit.go:525` 的 **journal 事件 id 已经带了 Runtime 在锁内分配的序号**(`evt-review-result-<resultID>-r<commitRevision+1>`),所以领域后缀**对唯一性是多余的**,唯一性已由事件坐标承担;缺的只是「这次重交」的一个**稳定领域判别式**。候选:收据里记录一次**领域交付序号**(不是轮次、不是 revision);或领域 `ResultID` 保持 plan 派生的稳定身份、由事件坐标区分两次交付。`L4-workflow-convergence.md:242` 明说「具体事务接线和中断恢复仍须核实,**不在此宣称已实现或锁定某种事件身份方案**」⇒ 本项归 **G18 第 3 步**(事件 id/幂等键形状)一并裁定,不得就地造一个 ad-hoc 方案 |
| **`internal/runtime/planning_rework.go`**(`validateReworkFrozenDesign` / 新增 `originalS3Responsibility`) | 返工冻结源按「`StageEntry` 数值最大且 < 当前 entry」选 S3 原件 | **已裁定并已改(本轮 W34)** —— 改为「按已记录的接收 + 转换事件关系选」:接收须 `advanced`、带 `TransitionEventID`、未被修订/取代、owner 为 Main,且与 packet 的 runtime/代/REQ/stage/阶段入口逐项对上;缺口**点名报错**而非回退到更早责任。详见 §14.12 |
| `internal/runtime/planning_rework.go` 的返工 transition CAS | 返工 transition CAS | **仍未动,归 G18 第 3 步** —— 若退役具体断言,需 `internal/transition` 支持「以当前 entry 派生幂等键」,否则损 `L4-revision-usage.md:117`「同一意图重试保持原关联」;且幂等键形状与 `transition/engine.go:207/:213` 是同一批,必须一起改 |
| `internal/workpacket/packet.go:28 Attempt` | `Attempt` 参与 packet 身份,`Attempt==1` 表「首次不得声明修订」 | 不是累计轮次,但读法接近「第几次」;退役会丢首次/修订身份区分 ⇒ 登记为 C 档观察,保留 |
| `internal/semantic/runtime_reachability.go:193` | 用数值 max 判 S5 manifest「当前/历史」 | 阶段入口是单调轴(A 档),但 `:169` 的原意是「已成立的接续关系」;若要收敛,改用 `origin_entry`/`transition_event` 关系,不减断言 |

### 14.8 已确认无残留的域

`internal/acceptance/`(revision 比较 0 处)、`internal/qualitygate/`(无 CAS、无轮次计数、无散列)、`internal/controlproof/`(只剩 Assignment 对象版本前后继绑定 + `sequence`)、`internal/fileview/`(零命中)、`internal/reviewmeta/` 与 `internal/hook/`(无数值轮次、无 sha/digest)、`internal/investigation/`(无 sha/hash/digest 残留)、`internal/review/`(无 `review.round` 生产/消费)、`internal/runtime/recovery.go` 与 `internal/runtime/checkpoint.go` 与 `internal/controlproof/**`(ExpectedRevision / ErrStaleRevision 零命中)。

### 14.9 本轮已知保持红(逐条归属,不得靠「恢复门禁」变绿)

| 用例 | 归属 | 性质与处置 |
|---|---|---|
| `internal/investigation/hypothesis_result_registration_test.go:396` 的 `continuity` 断言 | **G16** | 断言要求「不可变 Case 世系连续性」判据。签发侧 `internal/investigation/hypothesis_result_packet.go:514-531` 自身注释就写明「必须证明完整不可变 Case 世系」,而该机制不存在 —— 需签发侧与消费侧联动,**签发侧不在任何独占文件内**,本轮不实现,**断言维持红** |
| `internal/cli/native_s9_recovery_test.go:163` `TestNativeS9ExplicitStopChangeStillConflictsAfterRecovery` | 总账 **D2 桶**已知 DEFECT | 生产侧 `internal/cli/native_s9_original.go:37-44` 在 `DeliveryRef != ""` 时直接短路并返回「已接收」,**不做专业语义比较**;蓝图 `:112` 要求「以同一原消费者的专业语义比较当前原卷与其当前已收对象」。**不迁就、不改断言** |
| `internal/cli/workspace_cleanup_test.go:410` `TestLastFinalPreparesImpactBeforeIndependentCleanup/impact-before-cleanup` | **owner 仲裁已裁(本轮)** | 该变体自带仲裁注记「states the blueprint behaviour, so this variant stays red until the owner decides which reading holds」。**父任务裁定(蓝图优先)**:①`L4-worktree-governance.md:210`「**不以 force remove、git clean 或递归删除未知内容制造完成**」⇒ `--force` 移除路径必须删除;②`L4-worktree-governance.md:208`「**私人目录…都不是删除授权**」+ `L4-workflow-convergence.md:302`「未知、未跟踪、忽略或**私人材料保留为 cleanup_pending**」⇒ 私人答卷材料一律保留、保留即 `cleanup_pending`;③`:302`「cleanup_pending **只与保留材料绑定**」+ `L3-S9-repair.md:271`「Main 可准备原 ChangeImpact 而不使其成为完成门」⇒ 清理**不得**被未决的 ChangeImpact 问卷卡住。**实现侧三条已全部落地**(本轮:删除 `--force` 路径、私人材料改判为保留并命名为 `private answer material`、接收侧脏列表跳过私人答卷区)。该变体断言 `State == complete` 且要求工作区被移除 —— 在**保留材料位于同一工作区**时与第 ② 条**互斥**(不可能既移除该目录又保留其中私人答卷)。⇒ **维持红,但红的原因变了**:断言须按保留语义改写(断言 `cleanup_pending` + 已核实事实不变 + 私人字节保留 + 无强制移除 + **不依赖清理的阶段后继不被阻挡**),**下一轮测试侧执行**;不得为了让该变体变绿而恢复 `--force` |
| `internal/cli/s9_handoff_wiring_test.go:542` `TestS9HandoffAcceptedFinalCleanupUsesFrozenSessionProtection` | **owner 仲裁已裁(本轮)** | W26 的验收断言写成「状态必须是 `complete`」,与本轮删除 `--force` 后的保留语义互斥(工作区里留着作者的私人答卷 ⇒ 按 `:302` 保留为 `cleanup_pending`)。**父任务裁定**:`:302`「未知、未跟踪、忽略或**私人材料保留为 cleanup_pending**,**不能撤销本人接收、Main 检查或 ACK**」⇒ 清理挂起**不等于**接收被撤销,状态可以是 `cleanup_pending`。断言已改写为**更强**形式:①状态 ∈ {`complete`, `cleanup_pending`};②若为 `cleanup_pending`,阻塞原因**不得**指向缺失的旧 Plan(这正是本用例要证明的「旧 Plan 不阻塞」);③必须报出被保留的工作树路径**且该工作树确实还在**(私人材料没被删);④已接收的 Final 字节不变。**不得**为了让断言回到 `complete` 而恢复强制移除 |
| `internal/cli/integration_observation_e2e_test.go:23` `TestActualMainCheckBusyObservationDoesNotStartAnotherIntegration` | legacy 接线族 | 该 helper 走 legacy `runtime task-integrate` / legacy 接收板路径,与 B4 退役族同源,但**不在** §0.6 退役名单内 ⇒ 按「不得顺手删别的用例」保留并上报 || `internal/cli/s9_handoff_wiring_test.go:265` `TestS9HandoffTypedSelectorPreservesChangedAttemptFence` | W25 | 生产侧 `internal/runtime/repair_result_packet.go:214-244` 执行围栏已落地;该用例断言的 attempt fence 形态仍与围栏口径不一致,待下一轮统一 |
| `internal/cli/s6_observation_test.go:115` `TestS6UnchangedPreToolUseReusesObservation`、`TestNativeS6NonblockingNotesReachS7WithoutReanswer` | **G09-A 夹具缺口 → 已修** | 生产侧行为**正确**(W12 验收 `TestS6IntegrationWithoutMainStartKeepsOriginalBatchGate` PASS):当前批次复检必须以「当前代际存在原 TR-003 共同 Main 基线」为前提,缺失即按事实报错。根因**不在**基线守卫上——压缩夹具缺 canonical Assignment Record,闸门一直 not-ready,所以基线错压根未浮现;且 canonical 架构下唯一提交 TR-006 的是 Main 的集成出口(`internal/cli/s6_handoff.go:16`),PreToolUse 钩子只能**评估**批次闸门。**已修**:夹具用真实 TR-003 Writer 建立基线(`internal/cli/s6_observation_test.go:27` `newS6MainBaselineFixture`),新建批次改由原责任原生交付 + Main 真实集成建模,闸门评估在最终钩子前**重新采样**以证明钩子确在变化事实上执行。**断言逐字未动**,未碰生产代码,未改 `tests/fixtures/**` |



### 14.10 本轮抽样新浮出的红(总账未登记,归属重构期遗留,待补录)

`internal/cli` 定向抽样(`-run 'TestS7|TestS8|TestS9|TestCT039|TestNative|TestPublic|TestS6'`)命中 18 条顶层红,其中 **12 条总账已登记**(1 DEFECT / 3 STALE / 8 未决,红是设计内的);**另 6 条总账无记录**,经查全部由**重构提交或更早的修复**引入,**早于本轮**,不是本轮回归:

| 用例 | 文件 | 引入提交 |
|---|---|---|
| `TestNativeS9ActualWorktreeStopReceivesWithoutLauncherOrPlatformIdentity` | `internal/cli/native_s9_test.go` | `7f38047` |
| `TestNativeS9CandidatePreparesImpactWithoutRecordingMainCompletion` | `internal/cli/native_s9_impact_delivery_test.go` | `d0d948a` |
| `TestNativeS9IntegratesChangedDeliverySourcesWithoutHashMachineProof` | `internal/cli/native_s9_preintegration_test.go` | `7f38047` |
| `TestNativeS9PlanOnlyStopPreservesReceiptAfterPrivatePlanRemoval` | `internal/cli/native_s9_plan_only_stop_test.go` | `d0d948a` |
| `TestNativeS9PublicCorrectsHistoricalUnstagedResult` | `internal/cli/native_s9_reception_split_test.go` | `2b70155` |
| `TestNativeS9BlockerResumesThroughOriginalResolutionWithoutPrepare` | `internal/cli/native_s9_test.go` | `7f38047` |

**本轮进展**:其中 `TestNativeS9BlockerResumesThroughOriginalResolutionWithoutPrepare` 与 `TestNativeS9IntegratesChangedDeliverySourcesWithoutHashMachineProof` **已转绿**(根因:集成侧把代理**自己的私有答卷目录**归为可删除的「原物质」并用 `git worktree remove --force` **强制删掉**,从而制造「集成完成」;违反 `L4-worktree-governance.md:208`/`:210`)。**本轮已修**:`internal/integration/integrate_responsibility.go` 删除 `--force` 移除路径、私人材料改判为保留(`private answer material`)、`internal/integration/inspect_responsibility.go` 的接收侧脏列表跳过私人答卷区;顺带让 `TestNativeS9IntegratesChangedDeliverySourcesWithoutHashMachineProof` 由红转绿。

⇒ **待办**:其余 4 条逐条补录进复核总账(给出判档与归属),再按蓝图逐条改写或退役;`TestNativeS9PublicCorrectsHistoricalUnstagedResult` 的失败与本条无关(报文逐字相同),归下一轮。**其中 `TestNativeS9BlockerResumesThroughOriginalResolutionWithoutPrepare` 的根因已查明**:集成检查把代理**自己的私有答卷目录**(`?? .loop-submissions/`)判成 worktree dirty 并列为阻塞项 —— 违反 `L4-agent-dispatch-governance.md:57`「S7～S9 接收也不核 Git／ignore 状态」与 `L3-S9-repair.md:136`「原稿是否 tracked、staged、ignored 或 Git `??` 均不属于答卷接收资格」;**本轮已在修**。

### 14.11 C 档逐条裁定(本轮补完,附蓝图依据)

§14.7 上报的 C 档点在本轮逐条核实完毕。结论:**两点保留**(判据是结构性事实,不是写计数比较)、**两点本轮已改**。保留不等于放过 —— 每条都写明它守的是哪一个结构性事实。

#### C-A `internal/assignmentresult/accepted.go:266`、`:325-332` —— Result 版本连续性:**保留**

- `ResultRevision` 的来源是 `internal/assignment/completion_canonical.go:286` 的 `resultRevision := len(history) + 1` —— **接收端自派、派生自已收历史**,不是作者传入的期望值。因此它**不构成写路径上的 CAS**,不会触发「写计数外泄成领域身份 ⇒ 逼着写路径必须用 CAS」这条根因链。
- 角色 = **记录身份 + 阶段坐标**(该 Assignment 结果历史里的第几版结果)。按「revision 按角色三分」:作记录身份者保留 ⇒ **保留**。
- `:325-332` 的「版本连续 + `SubmissionKey` 唯一」是**已收历史完整性**:已收结果没有静默丢失、同一次提交没有重复接收。按最高判据,这类「已收行完整性」正是要**恢复**的结构性事实,不是要退役的门禁。
- §14.7 原先的顾虑「退役会削弱 `main_facts.go:53` 的 `Binding.ResultRevision` 唯一定位锚点;须先补 submission_key 锚点」—— 该前提**已满足**:submission_key 锚点本就存在(`internal/assignment/completion_submission_keys.go:36`/`:45`/`:53`,以及 `:329` 的 `keys[result.SubmissionKey]`)。两个锚点都在位,退役既无必要也无收益。
- **裁定:两处都保留,不改。**

#### C-B `internal/plancheckpoint/validate.go:111-113` —— `assignment_revision != 1`:**保留检查,修正轴标注**(本轮已改)

- 疑点是「与 ReviewPlan 指针 revision 轴是两轴」。**核实结论:不是两轴,是同一轴。** 全仓四个站点的 `assignment_revision` 一律是 **Assignment Record 自身的域版本**:
  - `:69` / `:87` —— 与 ReviewPlan 指针 `ptr.Revision` 比对(「声明 vs 已登记」);
  - `:212` —— 与 `record.Revision` 比对;
  - `:111` —— 与字面量 1 比对。
- 字面量 1 有结构性依据:manifest Assignment **登记**时就是 `Revision: 1`(`internal/assignmentrecord/record.go:405`),而 revision > 1 需要一次显式 `RevisionIssuance`;manifest 派发不做修订,故其登记版本恒为 1。
- **真正的「两轴混用」在注释里**:原文写「its generic **checkpoint revision** is deliberately 1」,把 Assignment 的域版本说成了 checkpoint 的写入次数 —— 用 checkpoint 轴的措辞解释 assignment 轴的事实。本轮已把注释改回真轴(Assignment 阶段坐标),并写明它既不是 checkpoint 计数也不是 Runtime 写计数:声明它 = 把 checkpoint 绑定到派发时的原责任坐标。
- **裁定:检查保留,注释修正。**

#### C-C `internal/hookctx/loader.go:1056` —— Case 版本方向:**保留,方向 `<` 正确**

- `document.Revision < packet.Binding.CaseRevision`:答卷所依据的 Case 文档版本**不得低于**该 packet 绑定的 Case 版本。Case 在飞行中可以被正当修订(存在 `case_revised` 操作),所以这里必须是**下界**而非等值 —— 用等值会让每一次正当的 Case 修订都误杀在飞的责任。假设题自身的绑定(hypothesis id + assignment id + status open)在下面单独核对。
- `currentCase && document.Revision != integerFact(pointer["revision"])`:当该 packet 的 Case 就是当前 Case 时,读的是**活文件**,这个等值是**活文件 ↔ 活索引的自洽性**(完整性)核对,不是写门禁。
- 两处都落在「原题面有效性」这一列 —— 而原题面有效性正是最高判据要求**恢复**的结构性事实之一。
- **裁定:两处都保留,不改。**

#### C-D `internal/controller/replay.go:210`、`:219-224` —— 恢复重放进度判据:**本轮已改**

- `:210` 的 `after.Revision <= snapshot.Revision` 把 Runtime 写计数当进度信号。**它是冗余的**:同一处的 `TransitionCommitted` 已证明暂存对被写过,而 `snapshotCursorString(after) == beforeCursor` 已证明生命周期动了。写计数器在这里不承担任何独立职责 ⇒ **退役**。
- `:219-224` 的 `seenRevisions` 把写计数当状态身份。**它可证明不可达**:暂存 Runtime 里一个 revision 只对应一个状态,状态决定游标;而游标检查(`:217-221`)对**同一个 `after`** 先执行。所以「revision 重复」必然先被「cursor 重复」拦下,`seenRevisions` 分支永不触发 ⇒ **整支退役,行为不变**。
- §14.7 原先的顾虑「改用游标去重前必须先确认游标在同一恢复链上确实唯一」:核实结果是**游标去重本来就已在生效**(`:217-221`)。本次不是新增游标判据,而是删掉一条与它重复的写计数判据。因此「不得为了减负而放弃『重放不产生第二次恢复状态机』」(`:105`)这条约束**没有被放弃** —— 该保证由游标检查独占承担。
- **遗留事实(不在本轮范围)**:游标在语义上**并不**全局唯一 —— 返工重开 contracts 会让 `(planning, contracts)` 合法重现。今天的游标检查会把这种合法重现报成循环,这是**既有行为**,不是本轮引入;要彻底修需要换成真正的结构身份(如 journal 末事件 id),归后续批次。
- **配套守卫用例(本轮新增)**:`internal/controller/replay_test.go` 的 `TestRecoveryReplayAdvancesTheCursorAndNeverRepeatsIt` —— 断言每个 advanced 步**必须推进游标**,且同一条恢复链里**没有任何游标出现两次**。这两条断言直接钉住「游标是进度与身份信号」,也是退役 revision 判据后仍然成立的根据。
- **裁定:两处 revision 用法均已退役。**

#### C-E `internal/cli/s6_observation.go:26` vs `internal/cli/controller.go:1251` —— 里程碑观察身份:**本轮改注释,结构性替换归 G18 第 3 步**

- 三处相关代码,各自的职责必须分清:
  1. `internal/cli/controller.go:1209` `stableMilestoneProjection` —— 里程碑的**内容语义身份**:比较「已记的里程碑是否还在说同一句话」。它**故意排除** `source_revision`、`updated_at`、`event`、`instruction` 和 gate 的 `observed_revision`,理由写在注释里:「观测到的 Runtime 修订是诊断遥测,不是状态变更;把它算进来会让每个 hook 都刷新自己的里程碑」。
  2. `internal/cli/controller.go:971` —— **journal 幂等键** `milestone:<runtime_id>:r<revision+1>`。**这里的写计数是承重的**:S6 观察相对已移动的 Runtime 失效时,里程碑的**投影内容不变但仍必须重写**(否则 `:959` 的早退分支永远命中、观察永远陈旧),只有写计数能把这次写入与上一次分开。
  3. `internal/cli/s6_observation.go:26` —— S6 PreToolUse **复用**已缓存的 gate 观察。失效方向永远是「缓存不适用 ⇒ 退回真 Controller 重算」,**不可能**拒绝任何专业完成 ⇒ 它是缓存新鲜度判断,不是门禁。
- **「自相矛盾」的准确位置在注释,不在行为**:原注释称 `stableMilestoneProjection` 是「noop 比较与 journal 幂等键的**唯一**语义身份」,而真正的 journal 键(`:971`)并不由它构成、且带写计数;`milestoneIdempotencyWithGate` 的注释则称它「hashes the stable milestone projection」并称「仅观测的修订变化保持幂等」—— 它既不 hash,键里也**带着**修订 ⇒ 两处都是**假陈述**。
- 本轮已把两处注释改回真话(不改行为):写计数在 journal 键里的承重性、它与内容语义身份的区别、以及替换方向。
- **结构性替换(归 G18 第 3 步)**:把里程碑观察锚定到 **journal 末事件 id**(记录身份,按「revision 按角色三分」属**保留**类),从而让写计数彻底退出观察身份。届时 `source_revision` 也应随之替换或退役。
- **裁定:行为不改,注释已改;替换归 G18 第 3 步。**

### 14.12 W34:返工冻结源改按真实接续关系选(已落地)

`internal/runtime/planning_rework.go` 的 `validateReworkFrozenDesign` 原先在 `entities["work_packets"]` 里挑 `StageEntry` **数值最大**且小于当前 entry 的 S3 packet 作冻结源。数值大小**替代了真实接续关系**:它可能挑到一个**只签发、从未接收**的 packet —— 那是一份材料,不是一份责任。本轮(W34)改为按**已记录的接收 + 转换事件关系**选。

#### 判据(逐条照抄 `internal/runtime/task_planning_artifacts.go:298-322` 的同包正确范式)

选中源必须同时满足:接收记录 `control_status == "advanced"`、`transition_event_id != ""`、`revision_packet_id == ""`、`superseded_by == ""`、`owner == "Main"`;`packet_id` 一致;`origin_state/origin_phase/origin_entry` 与接收一致;packet 侧 `runtime_id`/代/锁定 REQ/stage=S3/owner/阶段入口与接收侧逐项对上;该 packet 的契约提交唯一。

#### 失败时的行为(蓝图 `:169`「说明具体未知……不退回较早责任冒充当前来源」)

- 没有任何成立接续 ⇒ 报错点名:`no original S3 reception advanced the S3 continuation before entry N`;
- 比选中源更新的接收找不到对应 packet ⇒ 报错点名 entry 与 packet id(**不回退**到更早责任);
- 选中 entry 上 packet 缺失 / 重复 / 接收重复 ⇒ 各自点名。

#### 蓝图依据(逐条)

- `L4-revision-usage.md:169`「先在原 packet/submission 索引上核对 owner、阶段、`origin_entry`、`advanced` 与准确转换关联;取最近一次**真实完成该接续**的 S3 阶段入口;这里比较的是原单调阶段入口及**已成立的接续关系**,不是 packet ID、文件时间、全局 Runtime revision,或『哪份证据碰巧还 valid』」;
- `L4-state-transition-core.md:89`「**接续尚未建立时,应明确当前覆盖受阻和恢复责任**」;
- `L4-agent-guidance.md:190`「contracts 目标继续校验原 S3 的冻结 S2 输入与目录成员,**不能把上游漂移重新冻结为新事实**」。

#### 行为面变化(须知情,不是回归)

1. **从未建立过 S3→S4 接续的链上,contracts 返工被拒**(此前允许拿一个只签发的 packet 开新条目)。按 `L4-state-transition-core.md:89` 这是应有行为:接续未建立时必须明确「当前覆盖受阻」而不是冒充更早责任。
2. **上游 S2 设计漂移后,contracts 返工被拒** —— 冻结源解析到的仍是原 S3,而原 S3 冻结的 S2 身份已不等于当前登记的设计(`L4-agent-guidance.md:190`)。要修正上游须走 `--target design`。
3. **漂移的可见性只到「身份」为止**:冻结 S2 输入按 `ID/Path/Version` 核对,**不核字节**(`L4-revision-usage.md:171` 不逐文件算摘要)⇒ 纯字节改动不会被发现,contracts 返工仍被放行,且登记的设计身份不变。这是「Hook 不能声称发现所有变更」(`L4-agent-guidance.md:183`)的正确结果,已单独用例钉住。

#### 配套夹具改写(3 条红 → 绿,断言改得更强)

- `TestPlanningReworkCLIOriginalRoutes/S3_contracts`:由「返工成功并路由到 contracts」改为**显式拒绝**并点名缺失事实与恢复责任,并追加:拒绝不开入口、不改 Runtime 修订与 lifecycle/documents/evidence/bound_req/baseline、历史答卷字节未变、**拒绝本身没有补出那条缺失的接收**、重复拒绝仍拒绝。
- `TestPlanningReworkCLIPreservesAcceptedHistory`:夹具改为经真实 consume + 落地 Main followup 让接收真正 `advanced`;followup 断言由「任何 err != nil」收紧为 `errors.Is(err, runtime.ErrSubmissionFollowupProcessed)`;bounded Recover 断言改述为「已收历史不占用恢复预算」。
- `TestPlanningReworkCLIRejectsFrozenDesignDrift`:先建立真实接收,再让上游 S2 **声明版本**漂移并由原 S2 出口重新登记;断言 contracts 返工被**拒绝**且诊断点名漂移、Runtime 修订不变、新 S3 责任带**实际**设计身份而原冻结身份保留为历史。

#### 验收

`go build ./...` / `go vet` / `gofmt` 全过;`go test ./internal/runtime/` 整包绿(86.6s);`internal/cli` 返工族 + 回归集定向抽样绿(143.3s)。

### 14.13 G18 第 2/3 步:一次性执行的拆包与波次裁定

#### 根因(一句话)

事件 id 与幂等键由 Runtime 写计数拼出 ⇒ 调用方必须先知道 revision 才能构造 id ⇒
写路径被迫传 CAS ⇒ 「写计数外泄成领域身份」⇒ 领域层被迫做门禁式比较。

#### 统一键形状契约(唯一权威)

`/home/sebs/audit-work/fix-waves/briefs/G18-EVENT-IDENTITY.md`。六个 worker 全部照这一份实现,
禁止各造各的形状。要点:

- **事件 id** = `evt-<事件域>-<语义坐标…>`;**幂等键** = `runtime:<事件域>:<语义坐标…>`;
  形状里不得出现来自 `snapshot.Revision` / `state["revision"]` / `commitRevision` 的数字。
- 语义坐标取用优先级:`runtime_id`+`baseline_generation` → 原责任标识 → 责任承担者 →
  阶段坐标 → 意图键。仓内**已有合规先例**照抄:`internal/cli/worktree.go:356`、
  `internal/review/result_receipt.go:90`、`internal/runtime/contract_submission.go:188`、
  `internal/controlproof/assignment_revision_receipt.go:29`。
- 一次责任内**会合法发生多次**的事件(执行开始、目标复验、影响协调)必须换用**域内**区分量
  (产物路径 / 提交键 / 域内尝试号 / 更细的事件名),**不得**直接删数字 —— 否则 journal 的全局
  `event_id` 唯一性会把第二次写入当重复拒掉。
- 幂等键必须**至少含「责任坐标 + 意图键」**:同一意图重试 ⇒ 同键(幂等);不同责任 ⇒ 不同键(不撞)。
  现状 `runtime:<域>:<T>:<revision>` **没有责任维度**,这正是 `transition/engine.go:213` 的撞键根因。
- **历史兼容硬约束**:旧 journal 的事件 id 按原路径只读;任何扫描/比对旧 id 的代码必须**同时**
  接受旧形状与新形状(`internal/hook/stopidle.go:468`/`:530`、
  `internal/runtime/repair_plan_report_checkpoint.go:209`)。**不得重算已保存身份再比较**。

#### 拆包(按**耦合关系**切,不按目录切;文件级互斥)

| 片 | 包/文件 | 耦合要点 |
|---|---|---|
| W-A | `internal/transition/{engine.go,actions.go}` + `internal/reviewmeta/identity.go` + `internal/review/{register,revise,supplement,manifest_revision}.go` | 根因所在:`mutationEventID` + `runtime:<T>:<rev>` 幂等键 + `NewPlanID` 的种子;`engine.go:447` 的「已应用」判定必须改成比对**已保存身份**,不得重算 |
| W-B | `internal/investigation/{intake,case_workflow,contract,route_consume,hypothesis_result_receipt,hypothesis_result_packet}.go` | `intake`/`case_workflow`/`hypothesis_result_receipt` **共用** `evt-investigation-case-<caseID>-r<N>`,今天只靠数字区分 —— 必须区分操作或补坐标 |
| W-C | `internal/assignment/*` + `internal/runtime/{repair_result_blocker_checkpoint,repair_result_resolution}.go` + `internal/hook/stopidle.go` | 生产者/消费者**必须同步**:`assignment/lifecycle.go:302` 产出 `evt-agent-<id>-<event>-r<N>`,`runtime` 侧两处**重算比对**,`hook/stopidle.go:530` **前缀扫描** |
| W-D | `internal/repair/*` + `internal/runtime/{change,evidence,work_packet,packet_body,store}.go` | `evidence.go` 的**重复接收可观测结果不得变**(journal 唯一性检查在 Apply 之前);`store.go` 只改迁移键 |
| W-E | `internal/review/{submit,result_packet,result_receipt}.go` + `internal/cli/{audit_review_required,repair_lead_questionnaire,native_s6,builder_questionnaire,s6_prepare_revision,assignment_context,assignment_context_binding,investigation_command,repair_delegation_application,integration_ack}.go` | **结果身份三条硬约束**:不嵌 Runtime revision、不累计轮次、不得与上次交付的身份或答卷保存路径相撞 |
| W-F | `internal/cli/{controller,run,batch_scope_repair,evidence_binding_repair,workspace_launch,workspace_rework,workspace_check,workspace_recovery,workspace_command}.go` | 里程碑刷新必须能「内容不变仍重写」(S6 观察失效)⇒ 锚到 **journal 末事件 id**(记录身份) |

#### 两波执行的裁定(第 2 步与第 3 步不并行)

第 2 步(领域比较点换「已收行 + 事件去重 + 对象版本」三件套)与第 3 步**文件重叠**,
且第 2 步三件套里的「**事件去重**」正是第 3 步产出的新幂等键 ⇒ **第 3 步必须先落地**。

- **第一波(本节执行)**:第 3 步 —— 事件 id / 幂等键去写计数。
- **第二波(第一波落盘后执行)**:第 2 步 —— 退役领域侧 `ExpectedRevision >= 0 && … != …` 门禁
  (实测 ≈30 处,分布见 `internal/transition/engine.go:142`、`internal/review/{submit,revise,register,plan_reception,supplement}.go`、
  `internal/investigation/{hypothesis_result_receipt,case_workflow,contract,intake,route_consume}.go`、
  `internal/runtime/store.go:713`、`internal/assignment/{lifecycle,completion_canonical}.go`、
  `internal/repair/{issued_plan_report,issued_repair_result,runtime}.go`),
  逐处换成「已收行 + 事件去重 + 对象版本」。

#### 退役判定式(各 worker 自查用)

自己的文件里不得再出现形如 `Sprintf("...-r%d", <revision 来源>)` / `Sprintf("...:%d", <revision 来源>)`,
其中 `<revision 来源>` 指 `snapshot.Revision` / `state["revision"]` / `commitRevision` /
`commitSequence` / `expectedRevision` / `before.Revision` / `after` / `current.Revision`。
**注意**:`%d` 用于**域内**计数(阶段号、修订号、代际、尝试号)是允许的 —— 判断标准是**来源**,不是格式。

#### 第一波执行结果(G18 第 3 步)

**六片全部交付,60 个文件落地。**关键落点:

| 簇 | 落点 |
|---|---|
| 转换引擎(根因) | `internal/transition/engine.go` 的 `mutationEventIdentity`:键 = 责任坐标(`runtime_id`+代)+ 意图键(转换 + 来源游标 + 阶段入口 `phase_revision`)。`runtime:<tr>:<rev>` **补上了责任维度**,同阶段不同轮次靠阶段入口区分(阶段入口是域坐标,不是写计数) |
| 「已应用」判定 | 不再**重算** plan id 比对。改为从 journal 读**已保存的**事件(转换 id + `before_revision` == 调用方的 CAS 修订)→ 取其**已保存的** event id → 与**已保存的** plan id 比对。历史 `evt-tr-012-r10` 形状仍匹配 |
| 三站点共用事件名 | ingest 改**事件名**(`…-case-ingested-`);case 更新与假设接收**共用 Case 的域修订**坐标(`case-revision-<caseID>-r<caseRev>`,其中 `caseRev` 来自 Case 自身指针,不是 Runtime 修订) |
| 生产者/消费者同步 | `assignment/lifecycle.go` 产出 `evt-agent-<owner>-<kind>-<message_id>`;`runtime` 侧两处**重算比对**改为比对**已保存身份**;`hook/stopidle.go` 的前缀扫描同时接受 `-r<N>`(旧)与 `-<意图>`(新) |
| 重复接收 | `internal/runtime/evidence.go` 镜像 Apply 的顺序做**预检**:已登记 ⇒ 先跑 `prepareSubmission`(保留 `ErrSubmissionConflict` 语义),再返回**同一句** `evidence %s is already registered`,两边都不写状态或 journal 事实 |
| 结果身份(三条硬约束) | `<base>-attempt-<revision+1>` → `<base>-redelivery-<16 hex>`,区分量来自**上一次交付已记录的** `submission_key`+`result_ref`。不嵌 Runtime 修订、不累计轮次、且与上次交付的身份及其答卷保存路径**天然不同**(⇒ 旧原卷永不被覆盖);历史 `-attempt-N` 仍可只读 |
| 里程碑 | 锚到 **journal 末事件 id**(记录身份)⇒ S6 观察失效时可「内容不变仍重写」,重试仍同键 |
| 会合法发生多次的事件 | 执行开始 / 目标复验 / 影响协调 / 交接:改用**域内**区分量(会话 id + 已登记的产物路径 / 计划报告引用),不再靠写计数编号 |

**验收(实测)**:
- `go build ./...` / `go vet ./internal/...` / `gofmt`(60 文件)全过。
- 包级回归:`transition` `reviewmeta` `review` `assignment` `assignmentrecord` `repair` `runtime` `hook` `controlproof` `qualitygate` `submission` `integration` **全绿**;`investigation` 仅 `TestS8GenerationTwoOriginalHookReceiptReplaySiblingAndGates` 一条红(总账已登记的未闭环 DEFECT)。
- **写计数残留普查:真命中 0**(审计时 ≈35 处)。唯一 grep 命中是 `internal/repair/runtime.go:1158` 的 `writer.Update(current.Revision, …)` —— 那是 store 的 CAS **机制参数**(A 档保留),不在事件 id / 幂等键里。
- 新增 **22 条反向断言**(同一意图重试同 id、不同责任不同 id、历史形状仍可读、事件 id 不含写计数)。
- `internal/cli` 定向回归:22 条新增断言全 PASS;9 条红经 `go test -overlay` 把 60 个改动文件整体回退到 HEAD 后**逐字同样红** ⇒ **G18 第 3 步零新增红**。

#### 第二波执行结果(G18 第 2 步)

**三片全部交付,17 处 ①类比较点退役。**第一波的键形状落地后,第二波把「领域层拿写计数当门禁」逐处换成三件套。

| 片 | 包 | 退役的 ①类比较点 |
|---|---|---|
| W-G | `internal/review` | `plan_reception.go:41`(接收拒绝)、`submit.go:337`(提交前比计数)、`submit.go:180`(截止期恢复拒绝移动的计数)、`revise.go:83`(修订比计数)、`register.go:139`(登记比计数)、`supplement.go:83`(陈旧追赶守卫拒绝重试)—— 共 6 处 |
| W-H | `internal/investigation` | `intake.go:60`、`contract.go:75`、`case_workflow.go:585`、`hypothesis_result_receipt.go:145`、`route_consume.go:36` —— 共 5 处 |
| W-I | `internal/assignment` + `internal/repair` | `completion_canonical.go:189`(陈旧追赶守卫的写计数分支)、`:241`(`commitRevision` 解析)、`register.go:314`(签发路径的 CAS)、`issued_plan_report.go:589`、`issued_repair_result.go:503`、`issued_repair_result.go:984` —— 共 6 处 |

**关键设计(不可省的一步)**:退役 CAS 绑定会顺带**丢掉并发冲突检测**。W-H 发现后补上了「以当前值为准的并发重试」——
`hypothesis_result_receipt.go:259` 在检测到并发提交时按**当前值**重试并重新证明,而不是拿计数拒绝专业动作。
同理 W-I 把 `completion_canonical.go:189` 的陈旧追赶改成「陈旧 CAS 一律追赶」,W-G 把 `supplement.go:83` 改成「总是重读并重试」。
**门禁退役 ≠ 保护消失**:三件套(已收行 + 事件去重 + 对象版本)必须逐点补齐,否则就是把安全性一起删掉。

**测试侧改写(不放宽)**:唯一一条因退役而变红的用例是
`internal/cli/s9_final_writer_concurrency_test.go:157`(原名 `TestS9NativeFinalOrphanAfterCASRetry`)——
它断言 `SubmitIssuedRepairResult` 用陈旧 `ExpectedRevision` 返回 `runtime.ErrStaleRevision`,而那正是被退役的
`issued_repair_result.go:984`。**改写方向是换更强的断言,不是删断言**:
- 原断言(必须被写计数拒绝)⇒ 改为 **反向断言**:Runtime 因无关原因移动后,这个专业动作**必须仍然完成**;
- 并**加断言**:移动确实发生了(`advanced.Revision != request.ExpectedRevision`)、记录的是**新**的交付引用
  (`ref.Path != firstRef.Path`)、**同一个** `ResultID`(同一次专业动作)、未登记的孤儿文件**字节未变**、
  收据可读回、重放**幂等**、journal 里 `S9-RESULT-SUBMIT` 事件**恰好 1 条**。
- 用例改名 `TestS9NativeFinalOrphanAfterUnrelatedRuntimeMove`(名实相符)。

**残留裁定(第一波之后全仓普查,17 处 grep 命中,逐处归类)**:

| 命中 | 归类 |
|---|---|
| `internal/transition/engine.go:426/453`、`internal/runtime/{evidence,store}.go:681/950`、`internal/review/supplement.go:214`、`internal/investigation/hypothesis_result_receipt.go:261`、`internal/repair/runtime.go:1058` | **遗漏标记**(`< 0` = 未断言 ⇒ 走当前值路径),是退役的**实现形态**,不是门禁 |
| `internal/assignment/{auto_chain,activation_envelope}.go:216/267/405` | **记录/转递**:把已读到的坐标原样带下去,不比较、不拒绝 |
| `internal/review/result_packet.go:103`、`internal/investigation/hypothesis_result_packet.go:55`、`internal/repair/issued_{plan_report,repair_result}.go:112/157/107/129`、`internal/repair/issued_repair_result_{resolution,blocker}.go:154/98` | **A 档保留**:S9 packet 发行 / 阻塞与解决答复绑定**旧快照**;只有**显式非负**绑定才比较,未断言则走当前值路径 |
| `internal/assignment/lifecycle.go:494` | **A 档保留**:消息内 `ExpectedRuntimeRevision` 与 `request.ExpectedRevision` 的**自洽性**检查(§3.3 KEEP) |
| `internal/review/register.go:359` | **当前值路径**:陈旧即改用当前值重试,不拒绝 |
| `internal/cli/{task_receipt,assignment_context}.go`、`internal/cli/controller.go:992` | **观察一致性**:读取期间 Runtime 变了就重读重算,失效方向永远是「重算」,不可能拒绝专业完成 ⇒ 不是门禁 |
| `internal/review/{submit,revise,register,plan_reception}.go`、`internal/investigation/case_workflow.go`、`internal/cli/s6_observation.go:26` 等 | **域对象版本 / 观察锚**:比的是 Plan 指针修订、Case 域修订、Assignment Record 修订,**不是** Runtime 写计数 |

⇒ **①类真命中 0**(事件 id / 幂等键里的写计数同:真命中 0)。

**验收(实测)**:
- `go build ./...` / `go vet ./...` / `gofmt -l internal/ cmd/` **全过**(`gofmt` 空输出)。
- 包级整包:`review` ✅(141.8s)、`assignment` ✅(135.9s)、`repair` ✅(9.3s);`investigation` 仅
  `TestS8GenerationTwoOriginalHookReceiptReplaySiblingAndGates` 一条红 —— 用 `go test -overlay` 把 **43 个改动文件整体回退 HEAD**
  后**逐字同样红** ⇒ 非本轮引入。
- `internal/cli` 定向回归 **两批共 62 条**:批 1(S9/review/assignment 簇)1 条红
  `TestIndependentReviewRejectsCorruptSourceWithoutReception/frozen_input`;批 2(investigation/S7/S8 簇 + 已知红名单)10 条红。
  其中**两条此前未登记**(`TestMinimalAnswerFlowOrdinaryFindingEntersS8AndReceivesAlreadyDispatchedPeer`、
  `TestIndependentReviewRejectsCorruptSourceWithoutReception/frozen_input`)均经 overlay A/B **逐字同样红** ⇒ **零新增红**;
  其余 9 条与总账已登记红**逐条对上**。
- 新增反向断言:review 3 组(`plan_first_identity_test.go`、`result_receipt_test.go`、`supplement_test.go`)、
  investigation 4 组(`intake_test.go`、`case_workflow_test.go`、`contract_test.go`、`route_consume_test.go`)、
  assignment/repair 3 组(`completion_canonical_test.go`、`record_revision_test.go`、`issued_plan_report_context_test.go`)、
  CLI 1 组(`s9_final_writer_concurrency_test.go` 改写)。

#### 第 3 步的遗留回归(第一波埋下、第二波 CI 暴露并已修)

`make test-ci-core` 暴露 `tests/assignment` 的 `TestRegisterTaskDuplicateIDRejected`:
期望 `*ErrDuplicateTaskID`,实际得到 `mutation event_id "evt-register-task-TASK-100" already exists in runtime journal`。

**根因**:第 3 步把 TASK 登记的事件 id 从 `evt-register-task-<id>-r<N>` 改成 `evt-register-task-<id>`(去掉写计数)。
**副作用**:重复登记同一 TASK 时,事件 id 相同 ⇒ **journal 的全局 event_id 唯一性检查先于 `Apply` 触发**,
而域内的「TASK id 已登记」拒绝原本住在 `Apply` 里 ⇒ **域内判断被 journal 的键冲突顶掉**。
这是「事件 id 去写计数」与「journal 全局唯一性」之间的新交互,不是写计数门禁的问题本身。

**修法(与第一波 `internal/runtime/evidence.go` 同一手法)**:在 mutation 之前**镜像 `Apply` 的判定顺序**做预检 ——
`internal/assignment/register.go` 新增 `taskRegistered(...)`,重复登记直接返回 `&ErrDuplicateTaskID{TaskID: ...}`;
`Apply` 内的原判定**保留**(它是并发下的权威判定),预检只负责让**域内错误面**优先于 journal 键冲突。
两条路径都**不写** Runtime 状态,也**不写** journal 事实。

**同类回归排查**(全仓静态扫描:域身份形事件 id + 同一函数内的域内拒绝):
- `internal/runtime/evidence.go:46` —— 第一波已用同样的预检手法修过(`evidenceRegistered`)。
- `internal/investigation/intake.go:146`(`rejectExistingInvestigation`)、`internal/repair/runtime.go:123`(活跃 S9 RepairSession)、
  `internal/cli/newassignment_context_check.go:296` —— 域内拒绝都在 mutation **之前**,不会被顶掉。
- 其余命中(`internal/review/submit.go:1270` 的 pause checkpoint、`internal/review/submit.go:1373` 与
  `internal/scenario/atomic.go:374` 的文件级拒绝、`internal/workspace/recovery.go:100`)分别是文件系统级或 mutation 前的检查。
- `internal/review/submit.go:891`/`:991`(Finding 不可变、evidence id 冲突)由提交前的 `replayResultRequest` 预检与
  收据自带事件 id 覆盖;不同责任复用同一 Finding id 时事件 id 本就不同。
⇒ **仅此一处真回归**,已修。

**新增反向断言**:`internal/assignment/register_task_identity_test.go`
「重复登记保持域内结果且不写任何东西」—— 断言重复登记返回 `*ErrDuplicateTaskID`(且**不是** journal 键冲突文案)、
状态里仍只有 1 个 TASK、Runtime 修订仍为 1、journal 里 `ENTITY-REGISTER` 事件**恰好 1 条**且事件 id 为
`evt-register-task-TASK-100`(不含写计数)。**临时撤掉预检即红**(实测:报 `mutation event_id ... already exists in runtime journal`)。

#### 第三波:事件 id 与 journal 键冲突的整族治理(整包 CLI 暴露)

**触发方式**:第一次跑 `internal/cli` **整包**(1200 个测试函数,此前从未整包跑过)。定向抽样只覆盖 62 条,
漏掉的回归由整包跑一次全部翻出来:63 条失败。

**归因方法(可复现)**:用 `go test -c -overlay=...` 把**会话基线 `22bb385`** 的内容编译成测试二进制 ——
overlay 把会话改过的 99 个 `.go` 文件逐个映射回基线版本,**不触碰工作树**,因此基线跑不受并发编辑污染。
基线跑出 **1192 例 / 53 红**(50 条总账已登记 + **3 条此前从未登记**,因为 internal/cli 从未整包跑过),
**53 条在 HEAD 仍全部红** ⇒ 基线零污染伪影。会话引入的回归**正好 10 条**,逐条 A/B + 二分到引入提交:
- `a18e7e3`(第 3 步,事件 id 去写计数)引入 5 条:3 条**陈旧断言**(仍钉 `runtime:<T>:<写计数>` / `evt-<T>-r<写计数+1>`)、
  1 条 `evidence.go` **锁外预检**把「已接收」变成错误(并发下重放被拒)、1 条 work packet 事件 id 碰撞。
- `ed15364`(本会话早前的锁外预检)引入 3 条:预检放在 CAS **之前**,错误顺序错了。

**根因(整族)**:事件 id 改成「责任 + 意图」后,**同一意图的重试必然撞同一个 id**;而 journal 的全局
`event_id` 唯一性检查**先于 `Apply` 触发** ⇒ 住在 `Apply` 里的域内拒绝被 journal 的键冲突顶掉,
调用方收到 `mutation event_id %q already exists in runtime journal` —— **存储机制泄漏成领域错误面**。
这是「事件 id 去写计数」与「journal 全局唯一性」之间的新交互。

**通用机制**:`runtime.Mutation` 新增可选 `Precheck func(state map[string]any) error`。
store 在**锁内**、**revision CAS 之后**、**state/journal 配对校验之后**、**journal 键冲突检查之前**调用它;
其错误原样返回,失败后**不跑 `Apply`**;`Precheck` **不写** Runtime 状态、**不写** journal 事实。
⇒ 域内判定优先于键冲突,且顺序与第一波之前一致(先 CAS、后域内)。

**合法重复的域内区分量**(不是写计数):
| 事件 | 区分量 | 论证 |
|---|---|---|
| 转换(PTR-PLAN-01 返工) | **出现锚** = 该责任上一次提交的转换 id | 第一次设计出口跟在 bind 之后,返工跟在把责任打回的触发之后;Runtime 自己的游标证明**中间必有提交**(`phase_revision` 单调) |
| work packet(S5 问卷) | **准备次数** = journal 记录的该问卷发放次数 | 是**域内出现序号**:S5 夹具里两次发放是 1 和 2,而 Runtime 修订是 5 —— 与写计数相去甚远。内容派生的令牌不可行:两次发放的是**同一份**问卷,内容哈希相同 |
| agent completion | **交付身份**按已接收提交逐级升级(`-r2`/`-r3`) | 重投是新交付(合法重放),同一身份出现两次才是冲突 |
| plan 报告观察 | Worker + Assignment/其修订 + **规范化计划路径**的令牌 | 规范化后同一份计划的两种写法**共用一个身份**而不是分叉;令牌来自已登记的计划路径,不来自 Runtime 状态或写计数 |

**已修站点**(11 处,含第一波遗留的 4 处):
`internal/assignment/register.go`(TASK 登记 + workgroup 登记)、`internal/runtime/evidence.go`(S3 接收)、
`internal/assignment/completion.go`(legacy 完成)、`internal/assignment/lifecycle.go`(agent 生命周期)、
`internal/assignment/completion_canonical.go`(canonical 完成)、`internal/transition/engine.go`(转换)、
`internal/runtime/work_packet.go`(work packet)、`internal/cli/run.go`(plan 报告观察者)。

**陈旧断言改写(一律改强,不是改弱)**:
- `internal/cli/planning_followup_read_cycle_e2e_test.go` 的 `assertPlanningTransitionJournalEvent`
  从「钉住写计数形状」改成:把长度前缀意图**解码成 5 段**并逐段断言(代 / 游标 state / 游标 phase / 阶段入口 / 出现锚)、
  断言锚**不是裸数字**、断言**恰好只有 2 个数字段**(⇒ 写计数无法夹带)、断言被退役的两种形状**不存在**。
- `internal/assignment/event_identity_test.go` 里那条**原本有意钉住**「already exists in runtime journal」的断言
  改成钉**域内拒绝的精确文本** + 断言键冲突文案**不存在** + 保留并加强身份断言(反向断言的计数改由 `next.Revision` 推导,
  且对**两个身份**分别断言)。
- `internal/cli/s5_s6_guidance_delivery_test.go`、`internal/cli/tasks_e2e_test.go` 追加:准备次数**不跟随** Runtime 写计数;
  PTR-PLAN-01 的重放**不二次提交**(journal 恰好 1 条、修订不动)。

**新增反向断言**:11 个新测试文件/测试组,每条都做过「撤掉修复即红」的变异校验,
其中并发用例一律用**确定性夹具**(validator 闸门 / Writer 进程锁 / 真实 auto-chain 写入),不靠撞竞态。

**总账**:371 → **374**(补登 3 条此前未登记的既有红:`TestNativeS6CompactResultPassesTheOriginalIntegrationGate`、
`TestS7NativeListenerReportsSelectedUnreadableBody`、`TestS7NativeListenerReceivesAssignedOriginalOnNaturalToolEvent`)。

**残留(如实记录,未粉饰)**:
- 转换锚在「游标被手工改回 + 阶段入口被钉死 + 中间无提交」时,第三次连续出现会与第二次撞同一 id
  ⇒ 由 journal 拒绝。**裁定:这是诚实的** —— 那种情形下域内确实没有可区分的事实。
- `internal/assignment/record_revision.go` 的 `stopRevisionOwner` 会抹掉 `plan_reported_ref`,
  于是「该计划已被登记」只能由 journal 里的域内事实回答(仍是域内拒绝,不是键冲突);
  想让被取消的尝试保留自己的事实需要新增 agent 字段,而 agent schema 是 `additionalProperties: false` ⇒ 需要改 schema,未做。

#### 残留(留给下一波):
- `internal/cli/s6_observation.go:26` 仍以 `milestone["source_revision"]` 与当前修订比对来决定是否复用已缓存的 S6 观察 —— 这是 §14.11 C-E 记下的那一处写计数判据。**裁定:它不是门禁**(失效方向永远是「缓存不适用 ⇒ 退回真 Controller 重算」,不可能拒绝任何专业完成),故按最高判据**不强制退役**;但替换设计已算清,留给下一波:
  - 里程碑对象在状态 schema 里是**开放对象**(`additionalProperties` 未设 false)⇒ 换字段不需要改 schema;
  - 替换物 = **journal 末事件 id 的锚**(记录身份,与 `milestoneRefreshIdentity` 同一套 `journalEventAnchor` + `domainEventToken`);
  - **关键细节**:里程碑记的必须是**它自己那次写入之后**的锚(即 `domainEventToken("evt-milestone-refreshed-<写入前的锚>")`),而不是写入前的锚 —— 否则每次刷新后当前锚都等于刚写入的事件、而里程碑记的是更早的锚,观察**永远不会被复用**。旧语义记的正是「写入后的修订」(`snapshot.Revision+1`),两者等价。
  - `stableMilestoneProjection` 的排除清单要把 `source_revision` 换成 `source_anchor`(内容比较语义不变)。
- `internal/assignment/lifecycle.go:500` 的 `agent["activation_revision"]` 仍把 Runtime 修订存进域行字段。**裁定:保留为审计投影**,字段旁注明它是「激活发生时的审计坐标」,不是断言、不是门禁,不参与任何身份构造。
- 领域侧 `ExpectedRevision >= 0 && … != …` 门禁(①类)已在**第二波**退役完毕(17 处,真命中 0),详见上节「第二波执行结果」。
  判定规则留档:`/home/sebs/audit-work/fix-waves/briefs/G18-DOMAIN-GATES.md` §3.1(按**保护对象**分档:专业完成 ⇒ 退役;调用方绑定自己刚读到的快照 ⇒ 保留)。

### 14.14 总账 374 条的闭环现状(实测,基线对照)

**测量方法(可复现)**:把会话基线 `22bb385` 用 `git archive` 导出到 `/tmp/base-src`(**不触碰工作树、不写 git 元数据**),
在基线树与当前树两侧各编译一次测试二进制,只跑**总账涉及的 345 个测试**(374 条里 345 条的主体测试在基线仍存在;
另 29 条已在基线之前的适配波次被删除)。基线侧 59 红、当前侧 59 红,**逐条同名,双向差集为空**。

**整包失败数(当前 HEAD `8fa862a`;两次运行分别始于 11-04 19:07 与 11-05 01:50,均晚于最后一次提交 11-04 18:17)**:

| 包 | 失败数 |
|---|---|
| `internal/cli`(整包 1204 例) | 53 |
| `internal/investigation` | 1 |
| `tests/system/req039` | 5 |
| **合计** | **59** |

59 条**全部**在总账 374 条内有登记,**未登记 0 条**。

**374 条的闭环拆分**:

| 分类 | 条数 | 含义 |
|---|---|---|
| 已闭环 · 主体测试仍在代码中且当前通过 | 286 | 断言已按蓝图改写 |
| 已闭环 · 主体测试已在基线之前被删除 | 29 | `5d1f0a3` / `8144ba7` / `c728b42` / `b87abe4` / `e3a7ae0`(均为基线祖先)删掉陈旧断言 |
| **未闭环** | **59** | 与当前 59 条失败 **1:1** 对应 |
| 合计 | 374 | |

按判档拆:STALE 247 = 220 + 20 + 7;DEFECT 72 = 55 + 8 + 9;未决 48 = 8 + 1 + 39;CONFLICT 7 = 3 + 0 + 4。

未闭环 59 条的桶分布:L4 17 / L1 8 / D3 7 / L3 7 / SYS 5 / D2 4 / D1 2 / L2 2 / L5 1 / INV 1 / J06 1 / J08 1 / J13 1 / J15b 1 / J17b 1。

**本会话(G18 三波,18 提交)对复核总账的贡献 = 0 条红转绿**。这不是缺陷,而是**两本账不同**:
G18 的对象是 §14.2 的**写路径 CAS 总账**(①类业务完成路径的比较点,以及事件 id/幂等键里嵌写计数),
成果体现在**实现里不再有写计数门禁**,而不是任何一条复核总账用例由红转绿 —— 那些用例的红各自由自己桶的裁定文本决定
(例如 `internal/cli/native_s9_original.go` 缺专业语义比较、`internal/investigation` 的 Case 世系连续性机制不存在),
与写计数无关。59 条未闭环中只有 4 条的裁定文本触及 revision/计数/事件 id 轴(J08、L2、L4 ×2),且都不是被 G18 退役的那类门禁。

**下一批治理对象** = 上表 59 条未闭环(与 59 个仍红的测试 1:1 映射),按桶推进;
已给出逐条归属与「不得靠恢复门禁变绿」约束的条目,仍以 §14.9 为准。
