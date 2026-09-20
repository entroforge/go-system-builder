# 工作区绑定、执行与恢复

Main 始终是用户当前项目 checkout，目标分支取绑定时的当前分支。当前是 `test2` 就合回 `test2`；没有通用 `develop` 默认值，不自动 checkout。Main 本身可以是已有 linked worktree，Git common directory 只用于验证仓库身份。

## 主目录准备与执行

```sh
loop-harness runtime workspace bind --root /project
loop-harness runtime workspace prepare --root /project --assignment assignment-example --agent agent-example
loop-harness runtime workspace launch --root /project --assignment assignment-example --agent agent-example
```

prepare 消费已经登记的 assignment/owner，接受 building 或已进入 repairing 的 S9。S9 必须先在 Main 登记领域 PlanReport、批准的 Contract/Session/Plan，并执行 repair execution begin；通用 PLAN_REPORT 不能替代领域报告。审批、派发、S7 领域审核登记等仍走 Main 控制端，不向 Worker 开放审批权限。

派发前检查 staged、unstaged、untracked 输入，存在未提交源码/文档就列出文件并拒绝，不自动 add-all、commit 或 stash。派发记录精确 base_commit、输入摘要、scope、checks、owner 和执行代次；恢复不重新捕获当前 HEAD。先持久化 preparing，再创建 worktree、安装资产，最后 ready。

Worker 安装当前原生二进制、最小 Hook 设置、已安装的角色/技能及 `.claude/inputs` 下的冻结输入文档；不复制 Runtime、journal、凭据或整个 Main `.claude`。Bootstrap 回执记录文件摘要，改变资产后不能直接复用旧执行。

launch 只设置子进程 cwd，不切换主会话目录。启动前登记独立 Claude UUID，持有进程锁防止重复启动，通过安装的 `--agent` 角色执行，默认 30 分钟进程时限。仅放开 Read/Glob/Grep/Write/Edit/Bash，由 Hook 检查 Worker scope 和精确 adapter；没有 skip-permissions。需要其他原生工具的任务先保留现场调整已批准角色/执行方案，不能临时解除门禁。

重试用同一命令加 `--resume`，只恢复已登记 UUID。若进程在 Claude 创建会话前失败，resume 会如实失败；使用显式 replace 保留旧执行后重新派发。退出码 0 只代表 Claude 进程正常退出，不代表任务、S7 或发布完成。启动尝试日志/回执位于 Main `.claude/workspace-launch`。

## Worker 的受控操作

prepare 输出以下完整命令，直接使用，不追加管道、重定向或其他 shell 语句：

| 字段 | 输入与行为 |
| --- | --- |
| begin_command | `.claude/submissions/plan.json`，复用现有 PLAN_REPORT/activation 生命周期校验 |
| check_commands | 执行已声明检查的零基索引 |
| commit_command | `.claude/submissions/commit.json`，只暂存明确文件并正常 Git commit |
| report_command | `.claude/submissions/completion.json`，登记真实通用 completion_report |
| s9_delivery_command | `.claude/submissions/repair-result.json`，提出 S9 候选，不登记最终 PASS |

生命周期 JSON 使用既有 agent-message schema。提交请求示例：

```json
{"expected_head":"<当前 Worker 的完整 HEAD SHA>","message":"implement assignment","paths":["src/example.go","src/example_test.go"]}
```

commit 拒绝目录、路径越界、未声明暂存文件和 HEAD 漂移。仓库 Git hooks 保持启用，失败保留 index；相同请求可识别已完成提交，避免重复提交。仓库 hooks 属于受信任项目代码，提交入口不是 OS 沙箱；发现 hook 携带未声明文件时保留提交并报错。

check 当前支持 Linux amd64/arm64，要求 Bubblewrap、用户命名空间和 seccomp。检查在不超过 512 MiB 的副本中运行，排除 `.claude`/嵌套 `.worktrees`；Main 和 Git 元数据只读，禁止网络及 Unix socket、io_uring。声明命令需要缓存时使用沙箱内 `/tmp`。检查最长 5 分钟，日志/回执保存在 Main `.claude/evidence/workspace-checks`。不复制回构建产物，不把该回执代替实际合并检查。其他系统明确返回不可用，不降级成任意 shell。

Hook 将登记 Worker 路径映射到唯一 Main Runtime；独立启动会话还必须匹配 session UUID 和执行路径。Main 不能在 Worker 执行写操作，Worker 只能修改自己的 scope 和 submissions，不能修改 Runtime、Hook、角色、冻结输入或 Git 控制资产。符号链接按实际目标检查。这是 Hook 边界，不能视为 OS 级任意程序隔离。

## 合并、S9 与继续流程

```sh
loop-harness runtime task-integrate --root /project --assignment-id assignment-example
loop-harness runtime workspace pending --root /project
```

SubagentStop/SessionStart/PreCompact 提示 Main 消费显式待办，不在 Hook 中同步跑长合并。pending 从原报告/checkpoint 生成命令，不建立第二套调度状态。

S9 先完成通用报告，再 deliver：冻结 source SHA、Contract/Session/Plan、执行代次及 Git blob 变更摘要。主目录整合时重新检查授权、源提交、范围和冲突，正常非 squash 合并后执行必需检查，只有 verified 且 Main HEAD 仍等于 tested_head 时，调用原领域 SubmitRepairResult。Session 累计差异和独立复验门禁继续生效。后续仍是独立针对性复验 → fresh S7 → 人类发布授权，不自动发布。

失败保留源树/checkpoint。环境问题解决后使用 `--retry-preserved` 重验原候选。若需要修改代码，由 Main 执行 `workspace rework --root /project --assignment <id> --agent <owner> --reason <具体修正原因>`：仅接受未 verified 的失败 checkpoint，归档旧 checkpoint/候选，按实体生命周期把 reported owner/TASK review 退回 working/in_progress，并将原 completion evidence 标为 superseded；完成后 Worker 修改、commit、report，S9 再 deliver 新候选。不会覆盖旧候选或把失败改成 PASS。中断的返工也由 pending 投影恢复命令。已验证交付通过真实 completion_acknowledged 生命周期确认，同一 owner 的其他交付也必须验证。清理失败停在 cleanup_pending，重试继续清理，不强删、不全局 prune。已有 failed 领域结果要走原领域恢复路径，不能通过替换 worktree 抹掉失败。

## 显式迁移与恢复

所有恢复命令只能由 Main 执行，并要求 `--reason`。

- `workspace adopt --root /project --assignment <id> --agent <owner> --request historical-execution.json --reason ...`：请求是完整 Execution JSON，必须提供真实历史 base_commit、输入摘要、原 branch/path/target、runtime/baseline/execution generation、scope/checks。校验当前登记 assignment、仓库归属和历史差异；拒绝第二份 Runtime/journal。不会把当前 HEAD 猜成历史基线，缺失来源时需重新建立已批准基线。旧 PASS 不升级成新 verified。
- `workspace replace --root /project --assignment <id> --agent <owner> --execution-generation <旧代次> --reason ...`：要求没有活跃 launcher 锁、已登记 completion/candidate/integration checkpoint。保留旧树及 execution_history、原 owner 和冻结基线，新建代次/路径/分支。已有交付必须先走原集成/领域恢复；不能借替换绕过它。
- `workspace rebind --root /new/project --request relocation.json --reason ...`：先由 Git 完成 native worktree move/repair，再重绑 Harness。要求旧 Main 已不存在、明确 runtime/旧新 Main/旧新 common directory/branch/expected_head，以及每个活动和历史 Worker 的路径映射；拒绝仍活跃 launcher 及未完成交付/checkpoint。先保存 before/after 迁移意图，再 CAS 改绑定，最后迁移指针；相同请求可继续中断的指针更新。该入口不自动搬文件或修 Git 元数据，也不改变目标分支。

relocation.json 示例（路径映射必须完整）：

```json
{"runtime_id":"loop-example","old_main_root":"/old/project","old_common_dir":"/old/project/.git","new_main_root":"/new/project","new_common_dir":"/new/project/.git","branch":"test2","expected_head":"<当前完整 SHA>","paths":{"/old/project/.worktrees/assignment-example-g1-e1":"/new/project/.worktrees/assignment-example-g1-e1"}}
```

Runtime workspace 仍是 schema 1.1.0 的可选扩展，绑定前统一更新二进制；历史数据原样保留。执行新模式前做 Runtime/journal 配对备份，旧二进制不应写入不认识的扩展。回滚程序不能撤销已发生的 Git 合并。

## Hook 与指标维护

原生入口用外层子进程约束整个 Hook 为 8 秒，低于平台配置的 10 秒。完整结果返回前不输出部分 allow；超时/异常的潜在写操作返回阻断码 2，只读和生命周期通知保留诊断通道。中断的 Runtime 事务继续由原恢复协议处理。

计数型指标统一追加独立观察文件，不等待汇总锁。health/doctor、SessionStart 和 PreCompact 执行可恢复压缩：原子记录累计值和已消费文件，再删原文件，避免重复计数。普通 PreToolUse 不做压缩。持续长会话没有这些边界事件时仍应定期运行 health，指标不是业务恢复证据；不能对无限长、完全无维护的会话承诺磁盘恒定。

## 验收范围

临时真实 Git、进程、领域链路、故障恢复测试验证源码行为。真实 Claude S0–S11 和目标 Windows/macOS 运行验收需分别执行；未登录环境与交叉编译不能替代这些验收。检查隔离当前为 Linux 特性，不把其他平台的明确不可用说成支持。完整验证记录见优化方案的最新实施核验节。
