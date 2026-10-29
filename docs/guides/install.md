# 安装与版本边界

文档布局采用 layout v2：控制文件在`docs/control`，REQ 在`docs/requirements`，合同与任务在`docs/dev`，技术架构在`docs/architecture`，发布审计在`docs/reports/release-audits`。

`docs/product/` 属于不兼容的旧布局。Harness 拒绝该遗留目录（包括空目录）和当前 Runtime 的旧需求引用；显式 `--state` 同样检查。旧布局问题由原布局校验反馈；PreToolUse 监听进度和提示工序，不拦截工具操作。

## 全新安装

解压发布包，选择本机二进制文件：Linux amd64为`.claude/bin/loop-harness-linux-amd64`，macOS arm64为`.claude/bin/loop-harness-darwin-arm64`，Windows amd64为`.claude/bin/loop-harness-windows-amd64.exe`。不支持的架构应从源码构建匹配版本，不执行其他架构的二进制文件。

```bash
/path/to/extracted-release/.claude/bin/loop-harness-linux-amd64 install \
  --source /path/to/extracted-release \
  --root /path/to/new-empty-project
```

`--root`必须不存在或为空目录，含`.git`的非空目录也不会被覆盖；可在安装成功后执行`git init`。安装器先校验发布资产，再在同级临时目录组装、初始化和验证，全部通过才通过重命名发布目标目录。复制失败不会覆盖目标内容，重试可使用新的空目录；异常中断留下的`.loop-install-*`目录不参与驱动，确认无安装进程使用后可以删除。

首次绑定前，按项目惯例完成本地 Git 初始化与配置，并明确选择开发分支和最终发布上游。`req bind` 从所选开发分支的已提交树读取 REQ，所以锁定后的 REQ 必须先提交到该分支；若文件仅在工作区或暂存区，先在该本地分支提交它。绑定必须显式提供 `--dev-branch` 和 `--release-upstream`（远程目标包含 remote），两者都没有默认值。配置本地 Git 身份和例行命名本地分支不需要新的审批，也不授权推送或发布；审批人必须来自本次锁定/绑定授权，不能由 Git `user.name` 推断。

安装器将 Skills/Agents 放到`.claude`，将`AGENTS-template.md`变成`AGENTS.md`，将主机二进制文件和生成的 Manual放在`.claude/bin`，并初始化唯一的inactive 状态的 Runtime。所有平台二进制文件与 shell/PowerShell 启动器一并保留；`.claude/bin/loop-harness` 自动选择 Linux/macOS 的本机版本，Windows 使用 `loop-harness.ps1`。不要用原生二进制覆盖启动器。安装前校验 `release-manifest.json` 的完整文件清单，安装后 `.claude/framework-installation.json` 记录转换后的文件摘要和原始清单摘要；这些摘要证明内容一致性，不替代发布来源认证。

安装后运行：

```bash
.claude/bin/loop-harness doctor --root .
.claude/bin/loop-harness validate --all --root .
.claude/bin/loop-harness release-graph validate --installed --root .
.claude/bin/loop-harness docs check --root .
```

填写`docs/project.yaml`的项目事实与技术栈，以及`docs/project-map.md`；模板内的占位内容需要由目标项目完成。随后阅读[入门](getting-started.md)和[阶段协议](../control/agent-protocol.md)。

## Runtime 版本与阶段答卷

全新 install/init 默认使用 Runtime `1.3.0` 和阶段问卷；Agent、TASK 与 S7 ReviewAssignment 的工作状态采用共享五态。正常使用无需额外开关：listener 给当前准确填写路径，Main 规划答卷保存时由原消费者接收；原生子代理保存并返回实际产物，由 SubagentStop 调用原消费者。初始化不会批准 REQ 或替代原人类决定。旧模式仅用于兼容，可在全新项目中显式选 `--packet=false`；不要修改 state JSON 或对活跃项目重复 init 切换模式。

S5/S7/S8 使用本责任的专业问卷，S6/S9 使用原生 Agent/worktree 中的实际专业产物；不另交重复通用 PLAN，显式审批仍按原图执行。当前启动、返回、Main 集成与具名修订见[原生工作区指南](../workspace-integration.md)；已登记旧执行的 launcher、独立输入安装及目录迁移见[冻结执行恢复参考](../workspace-execution-recovery.md)。运行验收应说明实际版本、平台与执行范围，不能用安装成功或机制测试代替全阶段及产品验收。

初始化回执 `.claude/runtime-initialization.json` 绑定最初的 Schema、optimization 配置与 state 摘要；当前状态仍以 `.claude/loop-state.json` 为准，回执不能替代审批或完成。中断初始化重试保留原 marker 的版本/模式；显式 `--packet` 与 marker 不一致时拒绝。已有 1.1/1.2 Runtime 的兼容读取及原格式恢复不做原地迁移；新 Runtime 1.3 不会覆盖或重签旧状态。

原人闸的 rollover/unbind 继续归档整个旧 state/journal 并清空新 inactive；新实例保留源 Schema 和 optimization 配置，不能借重置切换模式。已有发卷/接收索引时，文档导入无法恢复唯一前驱、激活或接续事实；当前拒绝这种 RR-A 重建，须保留原配对与 pending、恢复匹配的原事实。该限制不阻止有效 pending 事务原样恢复。

## 存量项目与回退

安装器不支持活跃 Runtime 的原地升级。保留匹配的旧二进制文件、Definition、policy、Skills、模板和 Manual 继续工作；新二进制文件检测旧布局或混合布局时，在写入前返回`layout migration required`。不能用`cp -R`覆盖已有 docs，不能用 fingerprint 刷新重签旧基线，也不能批量修改历史 path/SHA。

需要采用新版本时，保留原项目与版本备份，在新目录完成安装与验证，再在明确的需求/基线切换窗口采用。既有证据保留其原 commit/path/hash；新需求重新绑定并重新验证。回退使用原完整资产组和其原 Runtime，不把新旧文件混合。

## 发布边界

安装包只含[文档地图](../DOCUMENT-MAP.md)中的执行资产与模板、明确的示例、Skills/Agents、工具和二进制文件。模板仓库的 L1–L4 设计文档不安装。包根`INSTALL.md`和`prelude.md`只是指向本目录的入口；Manual 来自 Definition 与编译后的 guard registry，不能手工维护第二份权威。

## 显式 S6 接收反压配置

全新 loop-harness install 可传
`--dispatch-max-unreceived-results 3 --dispatch-resume-unreceived-results 1`。
默认不启用；max >= 1，0 <= resume < max。配置写入新 1.3 Runtime 初始化
回执并冻结；不能覆盖已有 Runtime 或通过 reset 调整。启用后原 S6 Builder
注册在未接收结果达到 max 时暂缓，原 Main 接收到 resume 后解除。cleanup
不占已确认结果的预算，所有 required 职责继续保留。详情见
[工作区集成说明](../workspace-integration.md)。
