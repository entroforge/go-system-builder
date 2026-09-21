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

未指定 `integration_check_mode` 时保留 Worker 预检与 Main 合并后检查；仅在派发计划明确记录理由、成本取舍和失败恢复时选择 `post_merge`，它省略 Worker 动态预检；Worker 树和合并树不同，不能把它称为等价重复检查。普通任务不默认选择此模式。两种模式都检查工作区身份、范围和冻结文档，并且都必须完成 Main 合并后的必需检查。canonical Result 的路径与 SHA256 一起绑定到回执；恢复时 Result 改变须重新验证，不能复用旧 PASS。首次接收确认前目标 HEAD 改变也须重验；已经确认后的纯清理重试保留原验证回执。

SubagentStop/SessionStart/PreCompact 提示 Main 消费显式待办，不在 Hook 中同步跑长合并。pending 从原报告/checkpoint 生成命令，不建立第二套调度状态。

S9 先完成通用报告，再 deliver：冻结 source SHA、Contract/Session/Plan、执行代次及 Git blob 变更摘要。主目录整合时重新检查授权、源提交、范围和冲突，正常非 squash 合并后执行必需检查，只有 verified 且 Main HEAD 仍等于 tested_head 时，调用原领域 SubmitRepairResult。Session 累计差异和独立复验门禁继续生效。后续仍是独立针对性复验 → fresh S7 → 人类发布授权，不自动发布。

失败保留源树/checkpoint。环境问题解决后使用 `--retry-preserved` 重验原候选。若需要修改代码，由 Main 执行 `workspace rework --root /project --assignment <id> --agent <owner> --reason <具体修正原因>`：仅接受未 verified 的失败 checkpoint，归档旧 checkpoint/候选，按实体生命周期把 reported owner/TASK review 退回 working/in_progress，并将原 completion evidence 标为 superseded；完成后 Worker 修改、commit、report，S9 再 deliver 新候选。不会覆盖旧候选或把失败改成 PASS。中断的返工也由 pending 投影恢复命令。已验证交付通过真实 completion_acknowledged 生命周期确认，同一 owner 的其他交付也必须验证。清理失败停在 cleanup_pending，重试继续清理，不强删、不全局 prune。清理完成后，由同一 durable checkpoint 将 ExecutionRegistry 投影为 complete；若 Runtime CAS 前中断，pending 会继续提示 Main 重试。complete 实例只允许 Main 重复集成或报告重验，不能重新启动 Worker。已有 failed 领域结果要走原领域恢复路径，不能通过替换 worktree 抹掉失败。

## 显式迁移与恢复

所有恢复命令只能由 Main 执行，并要求 `--reason`。

- `workspace adopt --root /project --assignment <id> --agent <owner> --request historical-execution.json --reason ...`：请求是完整 Execution JSON，必须提供真实历史 base_commit、输入摘要、原 branch/path/target、runtime/baseline/execution generation、scope/checks。校验当前登记 assignment、仓库归属和历史差异；拒绝第二份 Runtime/journal。不会把当前 HEAD 猜成历史基线，缺失来源时需重新建立已批准基线。旧 PASS 不升级成新 verified。
- `workspace replace --root /project --assignment <id> --agent <owner> --execution-generation <旧代次> --reason ...`：要求没有活跃 launcher 锁、已登记 completion/candidate/integration checkpoint。保留旧树及 execution_history、原 owner 和冻结基线，新建代次/路径/分支。已有交付必须先走原集成/领域恢复；不能借替换绕过它。
- `workspace rebind --root /new/project --request relocation.json --reason ...`：先由 Git 完成 native worktree move/repair，再重绑 Harness。要求旧 Main 已不存在、明确 runtime/旧新 Main/旧新 common directory/branch/expected_head，以及每个活动和历史 Worker 的路径映射；拒绝仍活跃 launcher 及未完成交付/checkpoint。先保存 before/after 迁移意图，再 CAS 改绑定，随后迁移已完成 checkpoint 的坐标并更新活动 Worker 指针；相同请求可继续中断的更新。该入口不自动搬文件或修 Git 元数据，也不改变目标分支。

relocation.json 示例（路径映射必须完整）：

```json
{"runtime_id":"loop-example","old_main_root":"/old/project","old_common_dir":"/old/project/.git","new_main_root":"/new/project","new_common_dir":"/new/project/.git","branch":"test2","expected_head":"<当前完整 SHA>","paths":{"/old/project/.worktrees/assignment-example-g1-e1":"/new/project/.worktrees/assignment-example-g1-e1"}}
```

Runtime workspace 仍是 schema 1.1.0 的可选扩展，绑定前统一更新二进制；历史数据原样保留。执行新模式前做 Runtime/journal 配对备份，旧二进制不应写入不认识的扩展。回滚程序不能撤销已发生的 Git 合并。

## Hook 与指标维护

原生入口用外层子进程约束整个 Hook 为 8 秒，低于平台配置的 10 秒。完整结果返回前不输出部分 allow；超时/异常的潜在写操作返回阻断码 2，只读和生命周期通知保留诊断通道。中断的 Runtime 事务继续由原恢复协议处理。

计数型指标统一追加独立观察文件，不等待汇总锁。health/doctor、SessionStart 和 PreCompact 执行可恢复压缩：原子记录累计值和已消费文件，再删原文件，避免重复计数。普通 PreToolUse 不做压缩。持续长会话没有这些边界事件时仍应定期运行 health，指标不是业务恢复证据；不能对无限长、完全无维护的会话承诺磁盘恒定。

## 验收范围

临时真实 Git、进程、领域链路、故障恢复测试验证源码行为。真实 Claude S0–S11 和目标 Windows/macOS 运行验收需分别执行；未登录环境与交叉编译不能替代这些验收。检查隔离当前为 Linux 特性，不把其他平台的明确不可用说成支持。发布验收应保存所用版本、平台、操作结果和失败恢复证据。

## Completion report identity

An assignment's explicit `CompletionRef` is authoritative: missing or unreadable
reports block integration and recovery. Neither operation substitutes another
report. Without an explicit reference or a previously bound inspection, discovery
uses only `.claude/evidence/<current-runtime>/g<current-generation>/assignments/<assignment>/completion.json`.
Unknown Runtime/generation and historical or alternate layouts require explicit
recovery; the framework does not scan other runtimes or select the first candidate.
The normal evidence identity/hash and quality gates still apply.

## Worker 观察与检查能力

prepare 输出 `observation_commands`，包含 cwd、status、diff、staged、log 五个固定视图。
例如使用 prepare 返回的完整命令调用 `runtime workspace observe --view log`。
命令已经包含 Worker root、assignment 和 owner，不必自己构造身份。原始 `pwd` 可以使用；
原始 Git shell 被限制时，Hook 返回对应 observe 命令。观察不写完成证据，不改变 Runtime revision。
Git 调用禁用外部 diff/textconv、pager、fsmonitor 和惰性抓取，清除 Git 环境覆盖，最多输出 1 MiB、执行 10 秒。
不接受自定义 flags、重定向或复合 shell；源码写入继续使用受 scope 约束的文件工具及 commit adapter。

`runtime workspace prepare --check-location worker` 是兼容默认，prepare/launch 会先探测实际
隔离能力和 512 MiB 检查树上限。网络依赖需预先准备离线缓存。
不适用时可在新执行实例上显式选择 `--check-location main`，由 Main 集成检查通道运行注册检查；
这不放开 Worker shell，不跳过 required_checks、独立验证或发布人闸。
已存在执行实例的 runner 不可切换；没有交付/checkpoint 的实例可按原 replace 约束替换。
replace 默认保留原 runner；可在满足原有无交付、无 checkpoint、无活动 launcher 的条件下显式指定 `--check-location main` 或 `worker`，新代次记录选择，旧代次保持历史。
macOS/Windows 用户应明确选择 Main runner，不能把 Claude 自带 sandbox 等同于此框架的 Linux 检查实现。

rebind 的迁移 intent 保存不可变源快照和请求，只允许路径坐标改变；Runtime/journal 沿用原提交恢复机制。
已完成交付可以迁移，保留 immutable candidate、报告哈希、source/merge/tested SHA 和检查凭据原文；绝对路径按显式映射更新，历史检查 CWD 保存在 checkpoint 中。未完成交付仍拒绝迁移。旧版 ready 条目若对应完整 complete checkpoint，可在迁移后通过独立 Runtime CAS 恢复终态，无需重建已清理 Worker。
同一请求可恢复 intent 已写入、Runtime 已写但 journal 未完成、checkpoint 尚未迁移、终态尚未投影及部分 Worker pointer 更新的窗口。迁移后可重复集成；重新提交 canonical Result 仍须重验，不能凭历史 PASS 放行。
普通 Writer 仍拒绝旧 authority 外的访问；遇到非迁移 pending 事务需恢复原路径处理该事务，不能 force 跳过。

观察 adapter 会在 Git 身份检查前拒绝继承的 `GIT_DIR`、`GIT_WORK_TREE`、`GIT_TRACE` 等 Git 环境覆盖变量；按提示从调用环境清除后再执行。pager 和 external diff 环境设置会被忽略，不能由观察命令启用。

### 发布前真实会话验收

在隔离的新项目中，使用已登录的 Claude Code，记录 CLI 版本、操作系统和 Harness 提交：

1. 在自选开发分支（例如 `test2`）提交输入，绑定 Main；准备并启动两个不同 scope 的 Worker，确认 Main 的目录/分支不变。
2. 在 Worker 执行 `pwd` 以及 prepare 返回的全部观察命令；原始 Git 命令被拒绝时，确认 Claude 能读取 recovery 并改用观察 adapter。
3. 验证本项目所需离线依赖可用、声明检查成功；缺少 bwrap/用户命名空间或检查树超限时，确认派发前失败且没有新增执行/会话记录。通用预检不会推断任意项目的依赖是否齐全；应在采用 Worker runner 前验证项目声明的依赖条件。
4. 完成编辑、begin/commit/report/deliver 与 Main 集成；错误 owner、跨 Worker 和复合 shell 应拒绝。
5. 注入合并后检查失败，确认保留候选和 checkpoint、未放行后继；环境修复后 retry，代码失败采用 rework 新候选，不自动 reset。
6. 停止 launcher 后迁移项目，先修复原生 Git 链接，再按显式映射 rebind；中断并重试，检查 Runtime/journal 与各 Worker pointer 一致。
7. Linux Worker runner、macOS/Windows Main runner 分别记录原生执行结果；未执行项标记待验收，不能以交叉编译代替。
