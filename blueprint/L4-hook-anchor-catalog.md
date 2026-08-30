# L4 — Claude Code Hook 锚点全图与选点审查（Platform Anchor Catalog）

> 层：第四层｜性质：**平台侧事实库 + 选点审查纪律**。本文不定义我们的策略——它忠实收录 Claude Code 平台全部 Hook 锚点的触发时机、可阻断性与通信契约，并规定"何时允许为一个 stage 挂上新锚点"的审查流程。
>
> 权威来源：官方 Hooks Reference（https://code.claude.com/docs/en/hooks ，全文核对日 **2026-08-28**，文中标注的 v2.1.x 行为以官方页面为准）；owner 知识库 `/Users/lisonghao/SebsVault/LLM/Hook/Layer1-Hook.md` 提供了最初九段生命周期框架。两者冲突处以官方为准，已知差异登记在 §6。
>
> 四家族分工下的位置：[Hook 与平台事件接线](L4-hook-platform-wiring.md)回答"**我们这边**怎么收发"（payload 输入、envelope 输出、失败态度）；本文回答"**平台上有哪些格子**、每个格子什么时候响、响的时候能干什么、以及凭什么决定占用哪个格子"。接线篇的十类锚点是从本目录选出的消费子集，不是平台的边界。

## 0. 为什么这份目录必须是独立的 L4

选点错误是整类失效的源头：挂晚了（不可逆动作之后才发现）、挂错了（用了不允许阻断的锚点当门）、挂重了（每工具调用的常驻税）、漏挂了（以为没有格子可挂就自造轮子模拟平台）。此前系统里没有任何一处能回答"一共有哪些格子"；本目录补上这一层后：

- 各 L3 提出执法需求时，先到 §1 找天然格子，找不到才考虑发明机制；
- wiring 篇 §2 注册表的每次增删都能对照本目录给出依据；
- 平台升级带来的新锚点/新语义有一个明确的核对入口（§6 复核条款）。

## 1. 锚点总目录（31 个，按生命周期分组）

消费状态图例：●已接线｜◐候选（进入评审即可论证）｜○观察（暂无对应失控）｜△明确不采用（理由见备注）。

### 一・会话初始化

| 锚点 | 触发时机 | 能否阻断 | matcher 对象 | 状态 |
|:--|:--|:--|:--|:--|
| `SessionStart` | 新会话 / resume / clear / fork | 否（stderr 仅调试可见；可注入 additionalContext / 设 watchPaths / CLAUDE_ENV_FILE 落环境变量 / reloadSkills） | source（startup/resume/clear/fork） | ● |
| `Setup` | 仅 `--init-only` 或 `-p --init/--maintenance`（CI 单次准备） | 否 | trigger（init/maintenance） | ○ |
| `InstructionsLoaded` | CLAUDE.md / .claude/rules 加载（启动急载 + 会话中懒载） | 否，纯观测；输出字段被丢弃 | load_reason | ○ |

### 二・用户输入段（每一 turn）

| 锚点 | 触发时机 | 能否阻断 | matcher 对象 | 状态 |
|:--|:--|:--|:--|:--|
| `UserPromptSubmit` | 用户提交 prompt、Claude 处理之前 | 是（decision:block / exit 2）；明文 stdout 即注入上下文；**默认超时仅 30s** | 无（每条都触发） | ◐ |
| `UserPromptExpansion` | 斜杠命令/MCP prompt 展开为正式 prompt 前——覆盖"直敲 `/skill` 绕过 PreToolUse(Skill)"的那条路 | 是 | command_name | ○ |
| `MessageDisplay` | 助手文本分批渲染到屏幕时 | 不能拦，但可用 displayContent **改写显示内容**（transcript 与 Claude 所见不受影响；适合脱敏显示） | 无 | ◐ |

### 三・工具调用段（agentic loop 内）

| 锚点 | 触发时机 | 能否阻断 | matcher 对象 | 状态 |
|:--|:--|:--|:--|:--|
| `PreToolUse` | 工具参数生成后、执行前；**唯一能在事前拦工具的点** | 是：permissionDecision `deny>`defer`>`ask`>`allow`（最严优先）+ `updatedInput` 改参；exit 2 无视任何 JSON 强制拦 | tool_name（当前模板覆盖内置写工具、Task/Agent 与 `mcp__<srv>__<tool>`；PowerShell 见 B11） | ● |
| `PermissionRequest` | 即将对需要授权的动作弹权限框（或对无法弹窗的场景自动拒前） | 特殊：只有返回 decision 对象才生效（behavior allow/deny + updatedPermissions）；**exit 2 而 decision 缺席时不改变流程** | tool_name | ◐ |
| `PostToolUse` | 工具成功完成之后 | 不能撤销动作；可 updatedToolOutput 改写结果回传 Claude、可 additionalContext、可 classifierContext 给 auto 分类器递条子（v2.1.236+） | tool_name | ● |
| `PostToolUseFailure` | 工具开始执行后失败（抛错/MCP isError） | 不能；可给纠错反馈与告警。权限被拒/参数校验失败而根本没执行的不触发 | tool_name | ● |
| `PostToolBatch` | 同批并行工具全部落定、下一轮模型调用之前 | 是（decision:block 或 continue:false 停住循环等待下一轮） | 无；输入携带整批 tool_calls 数组 | ◐ |
| `PermissionDenied` | **仅 auto 模式**：分类器拒绝工具调用后 | 不能翻案；唯一输出是 hookSpecificOutput.retry=true 允许模型换法重试 | tool_name | ○ |

### 四・子代理与团队任务

| 锚点 | 触发时机 | 能否阻断 | matcher 对象 | 状态 |
|:--|:--|:--|:--|:--|
| `SubagentStart` | 经 Agent 工具召唤子代理 | 否；可向子代理注入 additionalContext | agent_type | ● |
| `SubagentStop` | 子代理完成、即将交卷 | 是（decision:block + reason 使其继续干；exit 2 同效；受 stop_hook_active 与连续 8 次 block 上限约束） | agent_type | ● |
| `TaskCreated` | TaskCreate 创建任务时 | 是（exit 2 或 decision:block → 任务删除，reason 回给创建者） | 无 | ◐ |
| `TaskCompleted` | 任务被标 completed（显式 TaskUpdate 或 teammate 带 in-progress 任务收工） | 是：exit 2 = 打回不关；continue:false = 整个 teammate 停机 | 无 | ◐ |
| `TeammateIdle` | 团队成员一轮结束将 idle | 是：exit 2 = 带着 stderr 继续干；continue:false = 彻底停机 | 无（输入 teammate_name/team_name） | ● |

### 五・回合结束与会话终止

| 锚点 | 触发时机 | 能否阻断 | matcher 对象 | 状态 |
|:--|:--|:--|:--|:--|
| `Stop` | 主会话 agent 说完话准备收工（用户打断不触发；API 故障走 StopFailure） | 是；未派发责任或未消费 Result 时 exit 2 继续同一 Main 回合 | 无 | ● |
| `StopFailure` | API 错误导致的异常收工 | 完全无决策权；除 terminalSequence 外输出被丢弃 | error 类型 | ○ |
| `SessionEnd` | 会话终止（clear/resume 切换等）；**默认超时仅 1.5s**，预决算上限 60s 可经环境变量覆盖 | 否 | reason | ○ |

### 六・环境、工作树、通知、压缩与弹窗

| 锚点 | 触发时机 | 能否阻断 | matcher 对象 | 状态 |
|:--|:--|:--|:--|:--|
| `CwdChanged` | `cd` 等导致工作目录变化 | 否；可动态设 watchPaths / 写环境变量 | 无 | ○ |
| `DirectoryAdded` | 会话中期 `/add-dir` 或 SDK 注册新根（启动期 `--add-dir` 不算） | 否；后台异步跑 | source | ○ |
| `FileChanged` | 被 watch 的文件**无论被谁**改写（工具/Bash/仓库外进程） | 否；价值在于覆盖工具途径之外的变更 | 双角色：watch 清单（`\|` 分隔的字面文件名）+ 生效过滤器 | ◐ |
| `WorktreeCreate` | 创建隔离工作树（--worktree / isolation / 后台会话）；**配置即替换默认 git 行为**，须在 stdout 末行交回路径 | 是（失败=建树失败；官方做符号链接安全筛） | 无（输入 slug 名） | ◐ |
| `WorktreeRemove` | 隔离树移除（清理审计） | 否，失败仅 debug 日志 | 无（输入 worktree_path） | ○ |
| `Notification` | 平台通知发出（permission_prompt 约延迟 6 秒去抖） | 否；terminalSequence 仍有效 | notification_type | ○ |
| `PreCompact` | 上下文压缩前 | 是（manual 压缩报给人；自动恢复型压缩被拦则底层错误浮出） | trigger（manual/auto） | ● |
| `PostCompact` | 压缩完成后（携带 compact_summary） | 否 | trigger | ○ |
| `ConfigChange` | 会话中设置类文件变动 | 只审计；policy_settings 层不可拦，不把观察器伪装成门禁 | source（settings 文件类别） | ● |
| `Elicitation` | MCP server 请求弹窗填表前 | 是（程序化代答或 exit 2 拒绝） | mcp_server_name | ○ |
| `ElicitationResult` | 弹窗填完、结果回传 server 前 | 是（可改写或置 decline） | mcp_server_name | ○ |

---

## 2. 全体锚点共享的平台机制

这些语义属于每一个格子，挂任何锚点前都要过一遍：

**输入面**：公共字段（session_id / transcript_path / cwd / hook_event_name，子代理上下文附 agent_id / agent_type）之外各锚点另有专属字段——关键的记 here：PreToolUse 三件套 tool_name/tool_input/tool_use_id 且**文件工具的 file_path 保证绝对路径、Windows 反斜杠**；Stop 家族携带 stop_hook_active、last_assistant_message、background_tasks、session_crons；SessionStart 携带 source 并可拿 model 字段。

**输出面三层**：通用字段 `continue / stopReason / suppressOutput / systemMessage / terminalSequence`（多数事件接受但部分丢弃，见目录"能干啥"列）；顶层 `decision(+reason)` 用于块式控制（非 PreToolUse 家族）；`hookSpecificOutput` 富控对象（permissionDecision、updatedInput、updatedToolOutput、additionalContext 等，hookEventName 必填）。字符串统一 10000 字符封顶。**首字符决定解析**：stdout 以 `{` 开头按 JSON 解析，否则当明文（三类事件会把明文喂进上下文）。

**退出码三档**：0 成功（JSON 生效）；2 阻断（**无视同批 JSON 的 allow**——语义上不存在"JSON 说放行但 exit 2"的中间态；PreToolUse 同义于 deny）；其余非零一般是非阻塞错误继续执行（per-event 例外：WorktreeCreate 任何非零即建树失败）。schema 校验失败的 JSON 也是非阻塞错误——**这也是静默失效的主要来源**：拼错字段名的门等于没装，handler 上线必须配 wire 级测试而非只测业务逻辑。

**并行与优先级**：同一事件的全部命中 handler 并行执行；跨 settings 层的同名 handler 去重，插件副本不去重；决策取最严（deny > defer > ask > allow）。一对孪生含义：多道屏障不会互相跳过，但也意味着一道宽松的新 handler 不可能稀释既有 deny；反过来，任何新 handler 都要假设自己可能与全系统所有旧 handler 同时命中。

**Matcher 语法陷阱**：逗号分隔要求 v2.1.191+；连字符纳入精确匹配集要求 v2.1.195+，之前的 `code-reviewer` 这类名字会被当**未锚定正则**误伤同名前缀——精确名一律手写 `^…$` 锚定；`FileChanged` / `StopFailure` 只接受字母数字下划线与 `|`。`if` 字段按权限规则语法对工具名+参数联合过滤（尽力而为，Bash 解析不了时 fail-open——**硬门禁止放在 if 里**）。MCP 工具命名 `mcp__<server>__<tool>`，服务器通配必须带 `.*` 后缀；插件内置 server 使用带插件名的 scoped 前缀。

**Handler 五类型与事件支持矩阵**：command（shell/exec 两种形态）、http（POST body 进、响应体出；**仅 2xx+JSON 体可产生决策**，连接失败一律继续）、mcp_tool、prompt（单轮 LLM 判定 `{ok}`）、agent（≤50 轮带工具的验证者，实验性）。全部五类的只有十三个（八成是决策型锚点：Pre/Post 族、Stop 族、Permission 族、Task/Teammate/Prompt 入口族）；SessionStart/Setup 只支持 command+mcp_tool——**想在会话起点放 prompt 型判定目前不成立**。

**超时预算**：默认 600s（command/http/mcp_tool），特例：UserPromptSubmit 30s、MessageDisplay 10s、SessionEnd 1.5s。超时=取消+无决策：**PreToolUse 的 command 家族超时不会拦工具，会落入正常权限流**——不能指望慢门当闸（Agent SDK callback 形态除外，其超时会拦）。

**异步**：仅 command 型可 `"async": true`；后台跑、下一 turn 交付 additionalContext/systemMessage，期间不具备任何控制力（exception：`asyncRewake` 形态 exit 2 可唤醒空闲会话）；`-p` 会话结束时被杀。

**权限与信任**：hooks 无沙箱、以宿主进程 OS 权限运行（知识库警示原文："像审查生产代码一样审查 hook 脚本"）；workspace trust 先于 settings 类 hook 装载（`-p` 视为信任，仓库自带 .claude/settings.json 会直接生效——审别人的仓库前用 `disableAllHooks` 或 `--bare`）；settings 合并而非覆盖、managed 层不可被下层关闭；企业可 `allowManagedHooksOnly` 收口；HTTP 白名单 `allowedHttpHookUrls` 适用于包括 managed 在内的全部来源。子代理继承 settings/plugins/skills 的 hooks（tool 事件同样触发，输入带 agent 身份）。skills/agents frontmatter 可携 hooks：subagent 的只在存活期有效（其 Stop 会被转换为 SubagentStop），skill 的注册后全会话有效（可 `once:true`）。

## 3. 当前消费组合的选取理由（为何恰是这十类）

现役十类锚点各自对应一条 D2 自然路径：PreToolUse=一切写的必经之门（gate/屏障/gate 引擎投影全在这）；PostToolUse(SendMessage)=计划回执与 blocker/completion 的必经信道（观察者）；PostToolUseFailure=原生失败信号；SubagentStop+TeammateIdle=Worker 收工的两个出口；Stop=Main 收工门；SessionStart/PreCompact=跨会话连续性的两个端点；SubagentStart=派发瞬间的简报注入；ConfigChange=治理资产变更的第二只眼。它们的共同点是责任边界清晰、没有引入第二套状态机，且每个占用都有可验证收益。

其余锚点未被接线不是因为平台不给，而是各自的理由见目录状态列：要不尚无对应失控（○），要不是明确收益但要过评审（◐），要不与治理模型冲突需要 owner 先裁（PermissionRequest：它替用户行使批准权，触到人闸模型的边界——这正是当初迁移模板把它列入禁词的动机；解禁前任何人不得接线）。**十类是一个快照，不是天花板。**

## 4. 候选锚点评估备忘（◐ 项的既知收益与前置条件）

> 各候选与现役锚点的问题应在目标项目本地的 `docs/bugs/` 登记册中操作化——推进任一候选时以对应条目为工单底稿，完成后回写本目录状态列；本模板仓库不保存具体项目的缺陷登记册。

| 锚点 | 既知收益 | 前置条件 / 风险 |
|:--|:--|:--|
| `Stop` | **● 已接线**：主会话有未派发责任或未消费 Result 时阻止收工；活跃后台 Worker 不误拦 | 受 stop_hook_active 与平台连续 block 上限约束；判定直接读已有 review assignments |
| `PostToolUseFailure` | **● 已接线**：补平台原生失败信号，作为 wrapper/probe 的第二信号源 | 只是 audit 信号；冻结窗口的语义仍归控制面篇 §9 |
| `FileChanged` | locked artifact / frozen subjects 目前靠主动 fingerprint 刷新发现漂移；此锚点可让**仓库外进程**的篡改也被看见 | matcher 即 watch 清单（字面文件名）；列表须动态化（配合 SessionStart/CwdChanged 下发 watchPaths） |
| `ConfigChange` | **● 已接线**：治理资产（loop-definition / hook-policy / settings）运行期被改的第二只眼 | audit-only；policy_settings 层拦不住，不能伪装成 enforce |
| `UserPromptSubmit` | Main 会话指令入口的投影/护栏（当前恢复包只在 SessionStart；会话中途的方向修正没有承载格） | 30s 默认超时短；每 turn 高频 |
| `PostToolBatch` | 批次级跨工具校验（如 S9 修复中的双写一致性、批量导入后的总量核对） | block 会停在下一轮模型调用前——确认这不是想要的粒度再用 |
| `TaskCreated` / `TaskCompleted` | Team task 生命面的两道合规门（依赖存在性；completion 必须对准调度篇 canonical Result 的 consumed 事实） | TaskCompleted 情况二（teammate 带活任务收工）会被 stopId le 重叠覆盖——分工须在调度篇划清；迁移模板当前把两者列入禁词，接线前先走演化协议解禁 |
| `MessageDisplay` | 显示面脱敏（补足证据面已有 capture 脱敏的最后一块可见性缝隙） | 仅影响渲染不改 transcript——不能冒充真正的 secret scrub |
| `WorktreeCreate` | 接管 S6/S9 隔离树的创建以复用登记逻辑；官方符号链接安全筛免费获得 | 配置即整体替换默认 git 行为；`.worktreeinclude` 失效，抄送逻辑须自己做 |
| `PermissionRequest` | 代表用户自动批准已白名单化的只读操作，减少弹窗噪声 | **触碰人闸模型**（谁有权说 yes）；此前列入迁移禁词。解禁条件：先修订控制面篇 §7.2 人闸契约，界定机器可代批的范围并由 owner 批准 |

## 5. 选点审查规则（新锚点准入的必答题）

任何一个 stage 或机制想占用新格子（或在既有格子上加 handler），提出者在动 settings 之前回答六问，答案写入 PR 描述并同步本目录状态列：

1. **格子责任问**：这个失控有没有已经被占用的天然格子承担？（D2 不重复设岗——同一时刻别造两道同义门。）
2. **时序有效性问**：锚点的判定时刻是否在要防的动作之前？事后格子（Post 族）只能善后不能设防；混合需求拆两个格子。
3. **失败方向问**：新 handler 属 fail-open 还是 fail-closed？该锚点的退出码/JSON 能力支持这个方向吗？（例：PermissionRequest 的 exit 2 是空转的；PreToolUse 的 command 超时是不拦的。）方向与 wiring 篇 §5 矩阵一致吗？
4. **常驻成本问**：触发频率 × 超时预算是什么量级？落在 30s/10s/1.5s 特例区间里的有没有重估？
5. **并行干涉问**：与全系统现存 handler 同事件并发时，最严优先原则会让它放大还是架空谁？跨域重叠区（Task 族 vs 调度篇；PreCompact vs checkpoint 协议）拿到归属文档背书了吗？
6. **三写同步问**：settings 注册、migration 白名单、本目录状态列是否同一提交更新？

连答六问全绿才可接线。退出一律反向走（先摘 settings，再清理白名单，最后改状态列并在 §5.1 注销理由）。

### 5.1 已注销锚点（防空位幽灵）

（空——首个注销发生时在此登记锚点名、日期、注销理由与残留清理项。）

## 6. 复核条款与来源差异

- **复核触发**：Claude Code 版本 bump、接线清单变更、或任一评审依据到龄（>6 个月）时，重新抓取官方页 diff 本目录；diff 结果记入变更记录。
- **2026-08-28 核对时的已知差异**（知识库 Layer1 → 官方现状）：①"其他非零退出码终止整个会话"已不成立——现行语义多为非阻塞错误继续执行（WorktreeCreate 除外）；②permissionDecision 共四值 deny/defer/ask/allow，其中 defer 仅在 `-p` 集成方有效；③知识库未收录 `DirectoryAdded`；④prompt/agent 型 handler 已成体系并有自己的分事件 ok:false 语义，不只是 KB 所述两种返回形态。
- 本文所有平台行为的最终解释权在官方页面；发现矛盾先修本文件再谈接线。

## 变更记录

| 日期 | 版本 | 变更 | 依据 |
|:--|:--|:--|:--|
| 2026-08-28 | v0.2.0 | 将 Stop、PostToolUseFailure、ConfigChange 从候选转为已接线，并同步十类消费组合；明确 PostToolUseFailure/ConfigChange 仅补审计信号，不创建第二套状态机 | HOOK-B03/B07/B09 实装与契约测试 |
| 2026-08-28 | v0.1.0 | 初版：收录平台 31 个 Hook 锚点（九段生命周期分组 × 触发/阻断/matcher/消费状态）、全体共享机制（输入输出三层/退出码/并行优先级/matcher 语法陷阱/五类 handler 支持矩阵/超时与异步/权限信任）、现役七锚点选取理由、十个◐候选评估备忘、六问选点审查流程与注销登记制度 | owner 指示：文档不得把现用七锚点表述成边界；读 SebsVault/LLM/Hook 知识库并对照官方最新文档建立锚点全图供后续迭代对照审查 |
