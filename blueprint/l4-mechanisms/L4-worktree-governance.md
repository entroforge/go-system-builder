# L4 — Worktree 生命周期与项目根目录权威

> 层：第四层；版本：v1.1.0；设计基准：最新 Claude Code 官方机制。
>
> 上游：L1 D1（权威外置）、D2（自然路径）、D6（证据链）；L2 单一 REQ 生命周期。
>
> 本篇定义 REQ 分支绑定、临时 worktree 身份、交付集成与清理。文件来源和事务一致性由[状态机核心](L4-state-transition-core.md)定义，平台消息由[Hook 接线](L4-hook-platform-wiring.md)定义，责任与派发由[Agent 调度](L4-agent-dispatch-governance.md)定义。

## 1. 机制准入与不变量

本机制有独立对象（项目根目录、REQ 开发分支/发布上游、worktree、接收记录），被 S1 绑定、S2–S5 文档交付、S6 构建、S7 验证、S8 调查、S9 修复及 S10/S11 接收发布共同消费，具有独立的基线漂移、并发集成、资源遗留和丢失恢复失败面；对接 Git 与 Claude Code 原生隔离，有可验证的验收契约，符合 L4 准入五问。

全域不变量：

1. 项目根目录是当前 REQ 的唯一权威控制面；Runtime、journal、assignment 接收事实不随子工作目录复制或经 Git 合并。
2. REQ 绑定显式声明开发主分支和最终发布上游；无 develop/main/master 默认值，也不推断 Git tracking upstream。
3. 子 worktree 从开发主分支明确的已提交基线创建；成果在子分支提交后合并回根目录开发主分支，保留合并提交。
4. worktree 是暂存资源；子会话退出、提交或报告完成均不等于根目录已接收。
5. 集成不是 release。最终发布只消费声明的上游，仍遵循 S11 人工发布边界。
6. 未合并、分支偏离和清理积压给主会话警示，不新增工具 deny、Stop block 或循环续跑。
7. 主要阶段产出按上游契约提交后才可支撑阶段资格；这与回收软提醒不同。未合并的必要代码不能冒充开发主分支已有的产出。

## 2. 身份、绑定与唯一来源

| 对象 | 必需事实 | 所有者 |
| --- | --- | --- |
| REQ workspace binding | project_root、dev_branch、release_upstream、bound_commit | S1 绑定事务 |
| assignment worktree | assignment_id、owner、path、source_branch、base_commit、target_branch | 原有 Assignment Record |
| integration checkpoint | source_commit、target_branch、merge_commit、checks、ack、cleanup 状态 | 原有 Integrator |
| 平台观察 | session/agent 身份、实际 cwd、worktree 来源、活动状态 | Hook 观察并核对 |

分支记录采用明确引用；发布目标涉及远程时包含 remote。绑定 commit 是溯源信息，后续派发使用开发主分支当时的最新提交。target_branch 从 REQ 绑定派生，不与 manifest/sidecar 各维护一份可独立缺省的目标。

project_root 是本次项目的权威目录；如果主会话项目自身就是 linked worktree，不能擅自重锚定到 Git 的主 checkout。git common dir 用于仓库身份校验，不代替 project_root。

根目录切分支不改变 REQ 绑定。目标调整必须显式更新绑定；普通 Hook 只提醒差异，不自动 checkout、不自动创建或改名分支、不推断发布授权。历史未绑定分支的 Runtime 必须报告缺失并引导补全，不回退 develop。

## 3. 创建与输入交接

Claude Code 原生使用 Git worktree，而不是复制当前磁盘工作区。设置 head 也不携带暂存或未提交修改，且嵌套 worktree 的 HEAD 不是根目录 HEAD。派发服务必须解析 REQ 开发分支的实际 commit，不能仅依赖平台默认 fresh/head。

创建调用和手工 git worktree add 都进入相同身份核对和台账。WorktreeCreate 接线替代原生创建，若采用它，服务必须完成真实创建、按该事件协议返回路径，并承担环境准备/失败恢复；不能注册一个仅打印提醒的观察器。

S2–S8 的主要文档和代码先按阶段交付要求提交。S9 Builder 的输入分为：

- 代码/测试/正式文档：所声明 commit 中的内容；
- 明确不入 Git 的 evidence：根目录读取或按依赖清单交接的只读副本，记录来源与版本；
- Runtime/journal：根目录单一 Writer，子会话通过报告交给主会话或受控入口消费。

读取权威状态的 control root 与运行代码/测试的 execution root 必须显式区分。测试结果说明实际被测提交和文件内容，不能把 dirty 工作区测试结果当作干净提交的结果。不得 blanket 复制/软链接整个 `.claude`。

同 assignment 优先复用已有 worktree。创建记录和归属核对需并发安全。数量按项目并行预算观察，积压时提醒先集成/清理，不据此新增创建硬门禁。

## 4. 文件边界

统一 PathScope 识别项目根目录、执行目录与临时 worktree。识别依据包括 Git 注册信息、assignment 台账和约定的临时目录；处理真实路径、符号链接、绝对/相对路径和目录边界。

项目级扫描、产品差异、资产发现与 Hook 文件检查排除临时 worktree 文件。root 内 `.worktrees/`、`.claude/worktrees/` 与已登记自定义路径采用同一语义；外部 worktree 不被误当作根目录文件。合回后的内容按根目录路径参加检查。

不能因工具 cwd 是 worktree 就跳过整个 Hook。混合命令分别分类其目标；直接写根目录、发布动作及权威控制面仍适用各自规则。证据读取例外不能自动扩展到任意 gitignored 目录。

## 5. 接收状态机

```text
created → working → submitted → merged → verified → acknowledged → cleanup_pending → complete
```

复用既有 pending/ready/merged/verified/acknowledged/cleanup_pending/complete checkpoint 表达集成阶段，不新建平行状态机。失败保留精确阶段、source commit、merge commit 与原因；恢复从事实阶段继续，不把 preserved 当作不可恢复终态。

主会话负责（默认 pre-and-post 模式）：

1. 核对 assignment、仓库身份、开发目标、完整子分支提交和未跟踪文件；在 worker 工作树运行 manifest 声明的 `required_checks`，检查本次交付的真实输入与产物。
2. 串行合并到根目录绑定开发分支，生成普通合并提交；不自动切到 manifest 指定的另一分支。
3. 合并后在根目录重新运行 `required_checks` 作为联合校验，按控制面契约同步允许变化的 evidence 绑定。检查命令以当前执行目录为基准，无需自行猜测 worker/root 双路径。
4. 写入接收事实，及时清理 worktree。清理失败只重试清理，不重复合并。

显式例外 `post_merge` 只在已审阅的派发计划记录理由、成本与失败恢复后选择，不作为新任务的默认。它省略 Worker 动态 required_checks，不省略身份、范围、冻结输入和冲突检查；Main 合并树上的 required_checks 不得省略。Worker 与合并树不同，不能称为等价验证。检查失败时 Main 已包含候选，保留 merge/检查回执；环境失败 retry，代码失败 rework，未 verified 不 ack、不释放后继、不 cleanup，不自动 reset/stash。模式固定在不可变 manifest，变更走既有替换/返工流程。

注册执行、只读观察、能力预检与迁移事务的收益/成本及状态消费者见 [受限执行决策](L4-workspace-execution-profiles.md)。

开发主分支移动、根目录未提交修改、冲突、子目录新变更和校验失败均保留现场并给出恢复动作。不自动 stash/reset、强删未接收内容或提交无关变更。清理必须确认当前 source 仍是已接收版本，活动任务仍持锁时不删除。

合并重试以 source commit 的祖先关系和 merge receipt 核对，不能仅凭文件夹不存在或一条 completed 字符串推断接收完成。响应丢失后，从 Git 与持久化记录重建阶段；无身份归属的 worktree 可报告但不能自动删除。

丢失 merge receipt 时沿绑定开发分支的第一父历史查找普通双父合并，两个父提交必须按顺序分别等于 checkpoint 中的 target/source，且该合并仍可从当前开发分支到达；后续普通提交不妨碍恢复。`preserved` 同时保存 `resume_state`，清理失败从 `cleanup_pending` 恢复，不重复已经通过的联合检查。旧记录缺少恢复阶段时，只能保守地从已证明的 merge 阶段重新验证。恢复清理仍核对当前 worker 内容、HEAD 和 Git 活动锁。

已有 verified 或更后阶段的 receipt 在复用、确认和清理前同样需要核对当前绑定目标及 merge 可达性。目标历史重写使 merge 不再可达时，持久化保留现场并撤销后继派发资格；普通后续提交不使有效 receipt 失效。调度投影和实际 Integrator 消费同一接收条件。

## 6. 观察、提醒与容量

SubagentStop/报告返回记录待集成事实；主会话在 PostToolUse(Agent)、后续工具事件或 SessionStart 收到简短 additionalContext。后台 async_launched 仅代表启动，不能标记交付。

提醒至少包含 REQ、开发分支、assignment/worktree、缺失事实和下一动作。相同事实去重；来源提交、合并状态或积压变化才重新提醒。Stop 不因本项提醒被阻断，也不能用会续跑的 additionalContext 冒充静默通知。

Hook 仅处理有界读取和短事务；完整测试及重合并由主会话驱动的 Integrator 执行。任务退出后不依赖第二次 SubagentStop 才能触发接收与清理。

## 7. 验收契约

- 无默认分支：显式开发/发布绑定，worktree 创建和合并仅消费开发绑定。
- 正确基线：未提交正式产出不能支撑阶段通过；运行 evidence 的合法磁盘输入可用。
- 正确边界：约定/自定义/外部 worktree 排除；根目录真实变更不被隐藏。
- 正确交付：子分支提交后根目录生成合并提交，联合校验、ack、清理闭环。
- 正确恢复：并发集成、冲突修复、合并响应丢失、清理响应丢失不丢成果、不重复应用。
- 正确反馈：最新 Claude Code 上证明主会话实际收到提醒；未回收状态不新增工具/Stop 硬门禁。

## 变更记录

| 日期 | 版本 | 变更依据 |
| --- | --- | --- |
| 2026-09-18 | 1.0.0 | Worktree / Hook 问题复审及用户分支绑定、软提醒、上游输入来源要求；建立跨 Stage 唯一机制定义 |

2026-09-21：修复目录迁移 CLI 权限闭环，增加受限观察/能力预检；维持双阶段默认并限定 post_merge 显式例外。证据与取舍见受限执行决策。
