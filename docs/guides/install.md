# 安装与版本边界

文档布局采用 layout v2：控制文件在`docs/control`，REQ 在`docs/requirements`，合同与任务在`docs/dev`，技术架构在`docs/architecture`，发布审计在`docs/reports/release-audits`。

`docs/product/` 属于不兼容的旧布局。Harness 拒绝该遗留目录（包括空目录）和当前 Runtime 的旧需求引用；显式 `--state` 同样检查。PreToolUse 对可识别的旧目录写入路径提前拒绝，并提示改用 `docs/requirements/`。

## 全新安装

解压发布包，选择本机二进制文件：Linux amd64为`.claude/bin/loop-harness-linux-amd64`，macOS arm64为`.claude/bin/loop-harness-darwin-arm64`，Windows amd64为`.claude/bin/loop-harness-windows-amd64.exe`。不支持的架构应从源码构建匹配版本，不执行其他架构的二进制文件。

```bash
/path/to/extracted-release/.claude/bin/loop-harness-linux-amd64 install \
  --source /path/to/extracted-release \
  --root /path/to/new-empty-project
```

`--root`必须不存在或为空目录，含`.git`的非空目录也不会被覆盖；可在安装成功后执行`git init`。安装器先校验发布资产，再在同级临时目录组装、初始化和验证，全部通过才通过重命名发布目标目录。复制失败不会覆盖目标内容，重试可使用新的空目录；异常中断留下的`.loop-install-*`目录不参与驱动，确认无安装进程使用后可以删除。

安装器将 Skills/Agents 放到`.claude`，将`AGENTS-template.md`变成`AGENTS.md`，将主机二进制文件和生成的 Manual放在`.claude/bin`，并初始化唯一的inactive 状态的 Runtime。所有平台二进制文件与 shell/PowerShell 启动器一并保留；`.claude/bin/loop-harness` 自动选择 Linux/macOS 的本机版本，Windows 使用 `loop-harness.ps1`。不要用原生二进制覆盖启动器。安装前校验 `release-manifest.json` 的完整文件清单，安装后 `.claude/framework-installation.json` 记录转换后的文件摘要和原始清单摘要；这些摘要证明内容一致性，不替代发布来源认证。

安装后运行：

```bash
.claude/bin/loop-harness doctor --root .
.claude/bin/loop-harness validate --all --root .
.claude/bin/loop-harness release-graph validate --installed --root .
.claude/bin/loop-harness docs check --root .
```

填写`docs/project.yaml`的项目事实与技术栈，以及`docs/project-map.md`；模板内的占位内容需要由目标项目完成。随后阅读[入门](getting-started.md)和[阶段协议](../control/agent-protocol.md)。

## 存量项目与回退

安装器不支持活跃 Runtime 的原地升级。保留匹配的旧二进制文件、Definition、policy、Skills、模板和 Manual 继续工作；新二进制文件检测旧布局或混合布局时，在写入前返回`layout migration required`。不能用`cp -R`覆盖已有 docs，不能用 fingerprint 刷新重签旧基线，也不能批量修改历史 path/SHA。

需要采用新版本时，保留原项目与版本备份，在新目录完成安装与验证，再在明确的需求/基线切换窗口采用。既有证据保留其原 commit/path/hash；新需求重新绑定并重新验证。回退使用原完整资产组和其原 Runtime，不把新旧文件混合。

## 发布边界

安装包只含[文档地图](../DOCUMENT-MAP.md)中的执行资产与模板、明确的示例、Skills/Agents、工具和二进制文件。模板仓库的 L1–L4 设计文档不安装。包根`INSTALL.md`和`prelude.md`只是指向本目录的入口；Manual 来自 Definition 与编译后的 guard registry，不能手工维护第二份权威。
