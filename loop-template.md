# Loop Wake-up Prompt (project `.claude/loop.md`)

> Template source. The release tarball installs this file at `.claude/loop.md`
> in the target project. It is the default Prompt that Claude Code's built-in
> `/loop` scheduler delivers on each wake-up.
>
> Source: `docs/control/agent-protocol.md`, `docs/control/loop-definition.json`, and
> `AGENTS-template.md`.

## What this file is — and is not

| Is | Is not |
|:---|:---|
| the Layer 2 Wake-up Prompt that recovers the Main-session Driver | the Engineering Loop itself |
| a short router back into `AGENTS.md`, the Runtime Bookmark, and the current stage of `agent-protocol.md` | a second copy of the Main Spine |
| consumed by Claude Code's built-in `/loop` scheduler | a REQ binder, a state machine, or a `/goal` replacement |

Hook events route actual saves and native returns to their original consumers.
Read-only planning observations show the current responsibility without
re-running its outlet. `/loop` is a fallback scheduler: it resumes the current
REQ responsibility and does not bind a REQ, grant approval or create another plan.

## Wake-up algorithm

If the latest Hook context already identifies the current responsibility,
accurate output path and next action, continue that work directly. A wake-up
alone is not a reason to re-read the library, reconcile Runtime or copy a REQ
hash. After a new session, compaction or contradictory context, recover as follows:

1. Start with the latest Hook responsibility. If it is unavailable, read the
   Runtime milestone and the relevant entry in `AGENTS.md`.
2. Follow the current stage anchor in `docs/control/agent-protocol.md` and its
   exact inputs/output references. Read only the direct upstream materials and
   primary Skill needed for the action. Do not reconstruct paths or machine
   fields from memory.
3. Continue the missing professional work through `DRIVE()` in
   `loop-orchestration`. Existing accepted facts remain valid unless the
   original consumer reports otherwise. Investigate source drift only when
   actual work or a diagnostic calls for it.
4. Follow the original completion boundary: Main saves its current answer.
   S7/S8/S9 PreToolUse receives ready original answers through the original consumer,
   with successful PostToolUse and Stop/SubagentStop fallback; reception does not
   wait for a final message or returned reference. Report actual results and
   unfinished work to Main when finished, without extra submit/done or activation.
   S5/S6 retain their original reference return and SubagentStop reception.
   Distinguish reception, stage transition and next-material preparation.
   A successful receipt with unfinished followup needs the named continuation,
   not a new answer or an unrelated tool call to trigger progress.
5. Continue across task completion, native return and recoverable failures.
   Stop at an actual human decision, explicit user stop, or a real external
   wait. Several valid implementations are not a reason to ask for a choice.

## Responsibility and recovery

- Reuse original responsibility and accepted results; do not duplicate TASKs,
  assignments or evidence after a wake-up.
- Pick a low-risk reversible implementation within the authorized requirement.
  An applicable existing human decision needs no second approval.
- A completed TASK or local repair does not complete the REQ. The stage contract
  and original consumers determine remaining review, integration and acceptance.
- Native work follows its original brief or questionnaire. Preserve actual
  accepted facts and continue unfinished work through the current responsibility.
- A Hook `warn`, `block` or missing item is not itself a human gateway. Follow
  its concrete cause and recovery. Ordinary tools remain available for repair;
  escalate only when the original domain actually requires a human decision.
- Normal work needs no `status`/`next`/`reconcile` ritual. Read-only diagnostics
  and supported original recovery commands remain available for a concrete
  technical gap. Do not refresh accepted sources or hand-push a transition.
- If Runtime is inactive with no bound REQ, follow `AGENTS.md`'s S0/S1 entry
  using actual authorization. This wake-up supplies no authorization of its own.

S7 由 Main 组织整轮覆盖，Reviewer 只完成本人原责任的实际 Claims/CASE。Pre／成功 Post／Stop 经同一原消费者单次核当前原卷的解析、必要字段、就绪和结论并保存。必要责任完成且无 Finding 时同次交 S10；有 Finding 时把已收发现与未完成事项交 S8，不另产 CleanRound／ObservationBatch 证明。S8 开始后已派发 S7 原责任仍可首次接收，晚到 Finding 交 Main 处理，不自动改既有批准。接收、转换与材料准备分别反馈；后继失败沿具名原入口只续未完成工序，保留已收工作，不重交原卷。S8 的 new_hypotheses 仅是建议；因果、未知与路线由 Main 判断，原真实批准及其对象保持独立责任。

## Reference

- Main Spine: `docs/control/agent-protocol.md`
- Loop Definition: `docs/control/loop-definition.json`
- Project Entry: `AGENTS-template.md`


## 临时 worktree 与阶段交付

S1 原绑定答卷记录实际开发分支和最终发布上游；不猜默认分支。显式 `req bind` 保留为兼容与恢复入口，不是正常问卷之外的另一项手续。项目根目录保存当前 REQ 权威事实。

S3 从原绑定及配置选定的 fileview 读取正式上游设计；真实来源缺失沿原反馈修复，不要求额外提交动作或 checkout 一致。其他阶段同样在实际来源合同要求时提交相关产物；不要自动提交用户无关变更。新 worktree 不包含主会话未提交或仅暂存的文件。

新原生 S6/S9 写入责任由 Main 携带准确 brief 通过 Claude Code Agent 派发，并使用原生 worktree 隔离。S6 子会话提交实际成果并返回原引用，由原消费者接收；Main 另行完成集成和检查；S6 原集成成功出口检查整批，满足 TR-006 后同次交付 S7 ReviewPlan/guide 的准确路径，不等待下一次无关工具动作。S9 原答卷按自然事件接收，Git commit、指定集成命令及 Final/proof 不作为 Impact 接收前置。S6 失败保留已成立事实，按反馈重入原 `runtime task-integrate` 恢复未完成工序或出口，原检查点防止重复合并/接收。Main 检查失败留下 preserved/blocked 检查点时，先解决原环境/检查原因，再按原 pending 命令带 `--retry-preserved` 重试，裸重入保留原失败；已成功集成仅续出口没有该重试前置。已迁移 S7 的材料缺口用原 `s7 draft` 恢复。已成立的本人交付和 Main 集成事实沿当前责任接续，具体恢复见 `docs/workspace-execution-recovery.md`。

正式文件、私人答卷、运行证据和 Runtime 各有来源合同，不能统一要求全部对象先入 Git。固定 Git 来源只接受已提交产物，明确磁盘来源读实际文件，Runtime 读权威快照。集成不等于 release；临时 worktree 不等于交付完成。清理提醒不新增 Stop 或普通工具硬门禁。

S9 先辨当前职责：Builder 在准确 brief 指定的本人路径保存原 PlanReport，独立接收后连续工作、自检，再保存原 Result；独立 Targeted 只答本人原题，对真实修复产品执行必要观察，不交 Builder 计划或代签结论。Pre／成功 Post／Stop 经原消费者一次接收，不要求返回引用、重存、flat→typed 作者副本或 Git／证据摘要资格。Main 完成真实集成与必要检查，填写原影响判断，再派发独立复验；Targeted 通过后准备完整新 S7，局部复验不是干净轮。blocked／未完成原样交准确责任，不能冒充完成。已收事实不依赖私人文件存续；后继失败保留接收并沿原入口只补缺口。清理是资源后继。旧发行路径和旧责任恢复保留，不倒灌新格式；新问题来源若暂不受准备器支持，交 Main/system 处理，不发明新协议。