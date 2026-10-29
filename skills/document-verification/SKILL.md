---
name: document-verification
description: Use when assigned one S5 document review — save the exact issued professional answer; existing legacy records retain their original recovery boundary
category: methodology
version: 2.2.0
---
# Document Verification


## Native S5 / S7 / S8 professional work

Main carries the exact original view and answer references into Claude Code native Agent. Read its actual frozen subjects, perform only your independent professional responsibility and save the original answer. For S5/S8, return the original reference for SubagentStop reception. For S7, PreToolUse listens for the ready original answer; successful PostToolUse and Stop/SubagentStop provide fallback through the same original consumer. No extra submit, done or activation is required. Reception does not wait for a final message or returned reference. S5/S8 ordinary tools retain their original reception contract. Ordinary tools need no activation or receipt. No Team, opaque platform ID, workspace prepare/launch, generic readback/PLAN or begin is required for new work. Main retains professional division, ReviewPlan waves, Case routing and stage closure; real approval modes and actual independence remain. Frozen registered workspaces keep their original sources and recovery; never fall back to Main for a lost frozen execution. S5 checks the independent document responsibility; S7 answers its Claims/lens; S8 investigates its Hypothesis without product writes.

## Authority
You produce an independent professional review; the original S5 consumer and gate evaluate it. Runtime authority lives in `docs/control/loop-definition.json`; the S5 stage contract lives in `docs/control/agent-protocol.md #s5`. The REV template explains professional review content and historical-format boundaries. It is not an envelope-authoring tutorial or a second mandatory answer.

## Role Contract

You are one of two independent reviewers of the frozen spec chain. The current
Assignment and Hook identify your responsibility. You verify exactly one —
`DV-SPEC-CONSISTENCY`（规格链自洽吗？）or
`DV-TASK-EXECUTABILITY`（Builder 照着能干吗？）. You never repair what you review.

## Entry Conditions
- The Loop is in `document_verification` and the Hook supplies your current responsibility's exact questionnaire view/answer paths. An explicit `plan_approval_required` assignment must first follow its original approval path; an absent path for expected packet work is a Main/system preparation gap, not a legacy fallback.
- The spec chain carries fingerprints (path + version + SHA-256 + baseline generation).

## Required Inputs

Hook-supplied view and exact answer path, the locked spec chain (REQ → design → contracts → tasks), and the
applicable module current-truth package resolved through the original accepted
S3/S4 frozen source closure and its document refs. If UI impact is `none` but
an AC targets an FR or service behavior truth changes, the required package is
the seven-file service behavior package: `scenario-model.json`,
`fixture-contract.json`, `cross-matrix.json`, `stories.md`, `flows.md`,
generated `cases.json`, and generated `scenario-coverage.json`. Its service
branches use `browser_required:false`, so HTML, PATH, and browser specs are not
required. Base the signed judgment on the frozen inputs named by the original refs;
if a required bound ref or file is unavailable or its fingerprint differs, report the source-closure defect
and stop signing that subject. Do not replace it with a same-path live file
found by Glob.

## Procedure — 审查过程

先检查 Hook 是否给出当前责任的准确 view/answer 路径。有卷时：读冻结资料 → 做第 2 步专业审查 → 只填写并保存该卷 answer → 保存本人原答卷。S7 由 Pre 高频监听就绪答案，成功 Post／Stop 兜底；S5／S8 仍返回准确引用，由 SubagentStop 接原消费者。普通 `plan_checkpoint` 不另写 generic PLAN_REPORT、readback 或 begin，也不复制 subject/hash 或手工 issue/consume；原 Writer 从登记责任/冻结输入生成 envelope 和 receipt。`plan_approval_required` 仍走原审批。旧完整信封仅保留已记录格式的读取和原恢复，不能作为新作者缺卷时的替代填写步骤。缺路径或机器关联时报告 Main/system，不改走手建信封。

新原生 S5 view 是本次签发的 packet：读集在 `draft.read`，答案位置在 `answer_path`。按 Main 带入的准确来源根解析这些相对路径；`task_analysis` 是引用，先打开 `task_analysis.path`，再读取该分析文件内的 `structural_check`、`task_contexts`、`granularity_advice` 及适用投影。`methods` 给出本责任的专业方法路径。只编辑独占 answer 文件里的专业答案，保存后返回准确引用，由 SubagentStop 调原消费者。Main 根下的独占私人答卷是合法原生布局，不能与共享或他人的答卷混用。

1. **读取当前原题目**：确认独占答卷、责任、准确输入和阅读目的。来源、身份、版本、摘要和输出位置由系统提供；缺项交 Main／系统恢复，不拼接机器元数据或新建证据信封。
2. **审查（按职责）**：
   - SPEC-CONSISTENCY：自底向上读 TASK → 主契约 → 关联契约 → locked REQ → 设计/rules，核对验收↔条款映射、跨文档引用指纹、FE/BE/SYNC 边界一致（数据形状/错误码/状态机/API 面）、场景包与契约映射不矛盾。FR-targeted/service-behavior changes require checking all seven service-package files through the frozen refs even when UI impact is `none`; `browser_required:false` skips only browser artifacts. 三项深挖：
     - **需求覆盖端到端**：抽 2-3 条 AC 走完最后一公里——AC → 契约条款 → TASK §3 声明 → 收尾契约 assert 行，全程指名到位；任何一跳"对不上号"即 finding（机器只保到 AC↔CASE，这最后一公里归你）；服务 CASE 继续参与 AC 覆盖分母，不因没有 PATH 或 browser spec 而免检；
     - **NFR 落地追踪**：REQ 非功能表的每一行有没有落进某张契约的条款（`NFR-{id}` 被 CONTRACTS 索引引用或条款显式声明）？NFR 是最易静默掉队的——没落地的每一行都是 finding；
     - **负向与错误路径对账**：契约错误码表 ↔ 场景包负向分支 ↔ 权限拒绝用例三方对得上吗？REQ 流程表声明了权限列的，场景包里必须有"无权限被拒"的负向分支。
   - TASK-EXECUTABILITY：先消费 `task_analysis.path` 指向的冻结分析文件中的 `structural_check`（覆盖与 DAG 结果由原 S4 checker 提供，不重算、不重复运行），再审机器判不了的**五问+半问**（代入 builder 视角——"我拿到这个任务单能顺利干完吗"）。仅当明确的旧 packet 缺少此摘要且原始冻结输入仍可用时，才用提供的 `review_check_command` 作为兼容补充；缺少输入时报告 Main/system。该命令也可用于具体问题诊断，但不是普通审查步骤：
     1. **单一职责**：检查任务是否具有清楚的交付目标和独立完成边界，结合真实实现耦合、依赖和验收判断是否需要拆分。跨层涉及多个真实交付物时分析其关系，不能仅凭涉及多层或断言行数认定拆分；收尾契约使用足够表达真实完成条件的断言。
     2. **上下文与耦合可行**：消费同一冻结分析文件中的 `task_contexts` 与 `granularity_advice`；对新 packet 以原分析字节及 parser 版本为准。若该冻结资料缺失或漂移，报告 Main/system 走原恢复，不自行重跑后改写分析。按必要读集、未知条件、明确共享写入、依赖和独立 oracle 判断当前任务是否可执行。目录未展开和无法定位的 anchor 是具体资料缺口；字节数不是 token 或拆分门槛。保留源 TASK/条款/路径并回答建议，结合真实 compact/返工观察判断，不能仅凭一次 compact 或可拆分就认定设计缺陷；
        `behavior_examples` 仅包含该 TASK Manifest 明确引用且经原 Scenario 校验的当前 CASE，保留 oracle/fixture/来源 hash 和读集 Mode/未知条件。新冻结分析有此字段时消费冻结字节，旧分析不补样例。缺引用、过期 CASE 或源包未验证给 unresolved，由原合同/设计责任补齐；样例存在不代表产品执行通过、不扩大覆盖、不替代独立 S7。
        `task_conventions` 有字段时消费原冻结投影和来源；检查默认是否在原规范/决定段落、适用 scope 与例外是否对应本人任务、示例是否真正吻合。candidate/conflict/stale/migration 不当当前默认，缺索引不构成自动缺陷。原登记/hash/版本只证明来源，不替代语义判断；偏离按原计划/结果和变更控制处理，不扩批准范围。
     3. **语义连贯**：地基（类型/schema/迁移）先于依赖者？有没有 B 用 A 产物却没声明依赖的缺失边？有没有复制粘贴来的假边？
     4. **自包含**：任务单是否给出明确的交付范围、验收判据与准确来源（路径+条款号）？若 Builder 必须靠搜索猜测要交付什么或怎样验收，记录具体缺项；沿已给 TASK 做正常代码定位属于实现工作；
     5. **可测性前向**：收尾契约要求的证据在 S7 三角度（DV/QA/E2E）下真的能产出吗？验收标准本身可判定吗——"响应快""体验好"这类无指标的表述，到 S7 只能靠猜，finding；
     批次节奏（附加半问）：DAG 机器已判无环——再看一眼关键路径：有没有一串任务全是单链依赖（串行瓶颈拖死并行分派）？有没有本可并行的任务被假依赖串起来了？
3. **Save the domain answer.** When the independent review can form a conclusion, choose it in the exact answer path, add findings only when needed, then save and return that exact reference for SubagentStop reception. Choose `pass` when this responsibility finds no issue and keep the prefilled `findings: []`. Choose `fix_required` when the locked REQ is clear and correcting design, contracts or TASKs can satisfy it. Choose `req_change_required` when the REQ itself is ambiguous, conflicting or missing a necessary decision; a human must clarify or change it, and this answer does not authorize that change. For each issue, fill only `path` (a supplied reviewed source, optionally with an anchor), `expected` (what should be present), and `observed` (the actual gap). Do not copy identity, fingerprints or machine-check summaries. Those structured findings are the normal finding record; do not create a second Markdown report. The original S5 Writer creates and registers the `document_review` envelope and receipt. Do not hand-create an envelope, choose/copy evidence IDs, or run `runtime evidence add`/`stage-answer issue/consume`. 旧已记录完整信封沿原消费者读取／恢复，不能要求新作者补造身份、摘要、证据 ID 或登记流程；没有准确原记录时由 Main／系统恢复。不要手动调用 transition。
专业答卷已包含结构化发现，不重复写 Markdown。已存在的历史专业报告保留实际发现、优先级、证据及 N/A 理由供恢复阅读，但不向新作者索取第二份报告。

## Triggered Deep-Dives（按冻结输入的适用条件审查）

**归属**：三项全归 **TASK-EXECUTABILITY 审查者**（数据迁移任务在批里、外部依赖看 SYNC 契约、critical 看 REQ——都是任务/REQ 视角）。根据当前冻结输入逐项判断下表条件，完成适用的专业检查，并把发现写入同一份答卷。问卷给出检查要求，不自动判断哪些条件命中。

| 触发条件 | 专项审查 |
|:--|:--|
| BE 契约含数据模型/schema 变更（新增/删除/改型字段、建表、迁移） | **迁移与破坏性变更处置**：按当前 REQ、契约及其引用的已批准决定检查兼容或断裂选择，不由本方法预设策略。核对选择的理由、影响面和责任；若保留兼容，检查相关决定/债务记录中的移除路径及责任人，例如该变更已有 ADR 的决策、影响与后续动作。破坏性变更的数据迁移任务是否进入 TASK 批？缺少必要决定或记录时，在原答卷定位对应 REQ/契约及缺口，由原规划责任补齐。REQ §A4 是负空间，不作为债务记录位置 |
| SYNC 契约存在，或 REQ/契约声明外部依赖（第三方 API/服务） | **外部集成韧性**：每个外部调用点有没有超时、失败重试策略、降级行为的声明？SYNC 契约的字段映射把外部错误翻译成本地错误码了吗？没有声明的调用点 = 生产环境的第一批故障源 |
| REQ 绑定的场景包 `coverage_profile: critical`（或 REQ 风险声明为高） | **风险触发验证就位**：S7 会按风险加派验证维度——现在预检它的"座位"在不在：场景包 required 分支覆盖是否声明到位；有浏览器的 CASE 检查 E2E PATH 座位，服务 CASE（`browser_required:false`）检查其服务 oracle 和对应的 S7 行为验证安排，不要求虚构 PATH。负向分支齐备性归职责 A 深挖项，不在此重复；座位缺失在 S5 补 |

触发项的检查结论写入当前专业答卷的 conclusion/findings；原 Writer 负责信封和机器元数据。

## Outputs

有已发行的 S5 packet 时，只在该 owner 的确切答卷路径填写 `answer.conclusion`/`answer.findings`，保留机器题干；普通 `plan_checkpoint` 不另交 generic PLAN_REPORT、readback 或 activation。显式 `plan_approval_required` 仍等待原审批。新原生责任在 Stop 由原领域 Writer 派生和登记信封；现有回执 `work_complete=true` 是兼容投影，只有原答卷接收有效且当前，才把本人审查责任投影为全局 `done`。合法 finding 不会撤销本人的已接收交付；同事未交、系统 pending 或需求人闸归回执指定 owner，不重新打开本人的责任。此模式不另抄 subjects、选择 evidence ID、写重复 Markdown 或手工登记第二份信封；已接收判断不能就地覆盖，修订或规格变化由 Main 走相应原路由。旧信封仅按已记录格式及原来源恢复；缺卷、缺关联交 Main/system，不得手改 Runtime 启用，也不得让作者补摘要。

具名修订必须读取当前 packet 的 `binding.revision_reason`，重新审查具体观察及完整冻结资料；旧回执 `replaced` 不完成新 attempt。新答案只保存到当前派发的确切路径。新原生修订由 Main 将新的原问卷引用交原生上下文，旧答案/view 保留；只有已登记旧独立 execution 按原退役/prepare 恢复，缺失旧来源不能借 Main 路径或手工 evidence add 提交。

- packet-enabled 必产物：Hook 答卷路径中的专业答案；原 Writer 派生/登记证据信封和 receipt。
- 历史兼容：保留已记录信封及原登记的读取和恢复；不能把内部完整格式要求转成新作者填写任务，缺原来源时由 Main／系统恢复。

## Exit Conditions

- 专业结论及 findings 已由当前责任的原答卷/Writer 接收；冻结 subjects、独立性和原 gate 检查仍必须满足。

## Stop Conditions

If the exact supplied sources are readable and sufficient to locate a specification defect, missing layers, contradictory documents or excessive task scope are review findings. Save `fix_required` when the locked REQ is clear, or `req_change_required` when its meaning needs clarification or change. These defects do not by themselves prevent a review conclusion.

Return a real blocker only when you cannot form the independent judgment: for example, independence is lost, or a required bound source is unreadable or has drifted. Name the unavailable source or independence problem and the original responsibility reference for Main; leave an unformed conclusion unanswered. Main restores the exact sources or reassigns the independent review. Do not refresh old hashes or replace missing frozen inputs with live workspace files. A technical recovery needs no new human decision by itself; a changed locked REQ/business meaning follows the original human Gateway.

## Registered frozen execution recovery

Only an already registered frozen S5 execution reads `.claude/inputs/work-packet.json` as an execution view. Its original packet is under `packet`; read `packet.draft.read` and the file referenced by `packet.task_analysis.path` beneath the supplied `frozen_input_root`. Write the original execution view’s `authoring_path`, matching the execution context’s supplied `answer_path`. Use its original save consumer or supplied `answer_command` for recovery. If those original paths are missing or inconsistent, Main/system restores them; do not substitute a new native packet or Main files. An explicitly older packet without an analysis summary uses only its supplied `review_check_command` and available original frozen sources. These are recovery instructions, not prerequisites for new native S5 work.

## Non-Goals

- Do not repair the reviewed documents — your fix_required routes them back to planning.
- Do not lock the batch or activate Builders — TR-003 does that after both reviewers pass.
- Do not treat missing coverage as N/A, and do not accept an unverifiable closing contract as executable.

## Production entry and transaction feasibility

TASK-EXECUTABILITY must inspect the actual entry and persistence path for every promised capability/slot family, not just document references. In existing TASK/contract traceability rows require: user action or external caller → registered entry → authoritative service/transaction → response or persisted state → owning TASK → necessary write paths → falsifiable check. Verify permission registration, UI mounts and shared types where applicable. A valid scope subset is not proof that all necessary files were declared. Keep changes that must be atomic in one transaction; do not split them merely to increase parallelism. Missing wiring ownership or a required transaction file is fix_required before dispatch. SPEC-CONSISTENCY retains business-semantic review; do not repeat its whole review or run the future product suite in S5.
