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
   S7/S9 PreToolUse receives ready original answers through the original consumer,
   with successful PostToolUse and Stop/SubagentStop fallback; reception does not
   wait for a final message or returned reference. Report actual results and
   unfinished work to Main when finished, without extra submit/done or activation.
   S5/S6/S8 retain their original reference return and SubagentStop reception.
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
- Native work follows its original brief or questionnaire. Only an already
  registered frozen execution retains its named readback/approval/activation
  recovery contract; do not copy those steps into new native work.
- A Hook `warn`, `block` or missing item is not itself a human gateway. Follow
  its concrete cause and recovery. Ordinary tools remain available for repair;
  escalate only when the original domain actually requires a human decision.
- Normal work needs no `status`/`next`/`reconcile` ritual. Read-only diagnostics
  and supported original recovery commands remain available for a concrete
  technical gap. Do not refresh accepted sources or hand-push a transition.
- If Runtime is inactive with no bound REQ, follow `AGENTS.md`'s S0/S1 entry
  using actual authorization. This wake-up supplies no authorization of its own.

S7 由 Main 组织整轮覆盖，Reviewer 只完成当前原责任的 Claims/CASE，采用实际浏览器、API、命令或状态方法。最后有效 Result 接收形成 CleanRound 后，同次完成出口接续原 Gate/TR-009，返回 S10 验收问卷准确位置；不等待下一次普通工具事件。接收、迁移和材料准备分别反馈。失败沿原 `loop-harness runtime review-continue --root <Main>` 复核当前干净出口；已进 S10 的缺失材料沿 `loop-harness runtime audit-prepare --root <Main>` 恢复，只补未完成工序并保留既有结果和报告，不重交 Reviewer 答案或手工复制 clean_round 绑定。

## Reference

- Main Spine: `docs/control/agent-protocol.md`
- Loop Definition: `docs/control/loop-definition.json`
- Project Entry: `AGENTS-template.md`


## 临时 worktree 与阶段交付

S1 原绑定答卷记录实际开发分支和最终发布上游；不猜默认分支。显式 `req bind` 保留为兼容与恢复入口，不是正常问卷之外的另一项手续。项目根目录保存当前 REQ 权威事实。

S2 在最终答卷前将正式上游设计提交到绑定开发分支，使 S3 能读取真实已提交来源。其他阶段同样在实际来源合同要求时提交相关产物；不要自动提交用户无关变更。新 worktree 不包含主会话未提交或仅暂存的文件。

新原生 S6/S9 写入责任由 Main 携带准确 brief 通过 Claude Code Agent 派发，并使用原生 worktree 隔离。子会话提交实际成果并返回原引用，由原消费者接收；Main 另行完成集成和检查；原集成成功出口检查整批，满足 TR-006 后同次交付 S7 ReviewPlan/guide 的准确路径，不等待下一次无关工具动作。S6 失败保留已成立事实，按反馈重入原 `runtime task-integrate` 恢复未完成工序或出口，原检查点防止重复合并/接收。Main 检查失败留下 preserved/blocked 检查点时，先解决原环境/检查原因，再按原 pending 命令带 `--retry-preserved` 重试，裸重入保留原失败；已成功集成仅续出口没有该重试前置。已迁移 S7 的材料缺口用原 `s7 draft` 恢复。已有冻结执行保留其具名 `runtime worktree-create`、原登记及集成恢复流程，详见 `docs/workspace-execution-recovery.md`，不推广为新责任的启动前提。

正式文件、私人答卷、运行证据和 Runtime 各有来源合同，不能统一要求全部对象先入 Git。固定 Git 来源只接受已提交产物，明确磁盘来源读实际文件，Runtime 读权威快照。集成不等于 release；临时 worktree 不等于交付完成。清理提醒不新增 Stop 或普通工具硬门禁。

S9 先辨当前职责：实施／测试 Builder 按准确 brief 在实际 worktree 保存私人 flat PlanReport／Result；独立 Targeted（包括被这样派发的 test-builder）只答原 Targeted 请求，不交 Builder 计划或改产品。普通保存不接收 native Worker 专业事实，返回原引用后由 SubagentStop 接收；新版本 completed nochange 由系统先在该 Stop 保全原字节、所需证据和准确来源，再进入原消费者。已登记技术保源不等于接收，树清理后仍须复核；未保全或保全失败不能恢复丢失原字节，旧冻结合同不升级。Main 自己保存 ChangeImpact。具体字段与方法只读当前题面和 bug-resolution，不在唤醒时另建报告。Targeted 已接收而 Handoff 未完成时，沿反馈给出的原收尾入口只补缺口；普通 Pre／状态不触发交接。成功 TR-012 后 Main 填写并保存原 S7 ReviewPlan 问卷，seed 不等于 PASS，也不另做手工 review-plan 登记。旧已登记冻结来源保持原恢复。
