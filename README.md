# Vibe Coding Documentation System

Loop Harness 框架源码与可复用工程模板。此仓库用于开发、构建框架；安装后的业务项目有自己的指令、配置、Runtime 和交付文档。

## 从哪里开始

| 任务 | 入口 |
| --- | --- |
| 维护框架源码 | [源码维护指引](CLAUDE.md)、[目录与发布边界](packaging/README.md) |
| 了解设计 | [框架设计 L1–L4](blueprint/README.md) |
| 安装到新项目 | [安装指南](docs/guides/install.md) |
| 在已安装项目中使用 | [入门](docs/guides/getting-started.md)、[操作与恢复](docs/runtime-operations.md) |
| 查找模板与规范 | [文档导航](docs/README.md)、[文档地图](docs/DOCUMENT-MAP.md) |

## 源码目录

```text
cmd/、internal/       Go 入口、实现与包内测试
tests/               集成测试与合成夹具
skills/、agents/     安装到目标项目的 Skill 与子代理定义源文件
docs/                对外说明、协议、规则、空白模板与示例
blueprint/           框架本身的正式设计，不进入目标项目
packaging/、tools/   发布清单、安装投影、构建与维护工具
.local/              本机维护资料、验证记录、包与回退备份；Git 忽略
dist/、.claude/      本机生成物与本地安装状态；Git 忽略
```

`CLAUDE.md` 是维护本源码库的简短指令；`AGENTS-template.md` 是目标项目指令的模板，两者用途不同。根 `settings.json` 和 `loop-template.md` 也是安装源文件，`loop-harness.md` 由工具生成。

带日期的修复报告、原始日志和源码备份统一放在 `.local/framework-maintenance/`，从该目录的 `README.md` 查找。它们不进入发布包，也不作为常驻 Agent 指令。需要长期共享的审计材料应另存团队工件库；本地归档不是远端备份。

## Entry Points

1. `AGENTS-template.md`: project-local `AGENTS.md` source — Layer 1 entry (Main-session Driver). Step 0 checks Project Design Foundation before the first `UI impact=changed` REQ.
2. `loop-template.md`: project-local `.claude/loop.md` source — Layer 2 Wake-up Prompt.
3. `settings.json`: Hook registration — Layer 3 guardrail enforcement.
4. `docs/control/agent-protocol.md`: authoritative Main Spine (S0-S11 stage contracts).
5. `docs/README.md`: setup and usage order.
6. `docs/project-map-template.md`: template for project facts, stage, PM todo, and gates.
7. `docs/requirements/REQ-template.md`: requirement template.
8. `docs/rules/design-foundation.md` and `skills/design-foundation/SKILL.md`: F0–F6 before locking UI-changing REQs.
9. `docs/guides/getting-started.md`: main-session onboarding.

## Documentation and Installation

Start with the [documentation index](docs/README.md) and [document map](docs/DOCUMENT-MAP.md).
The [framework index](blueprint/README.md) separates L1 principles, L2 lifecycle,
L3 stages, and L4 mechanisms.
Root `blueprint/` documents the template itself; it is separate from target-project
`docs/` and is excluded from release packages.

Follow the [installation guide](docs/guides/install.md) to install a release into a new empty project.
Existing projects retain their matching release; this layout does not support overlay upgrades.

## Repository Boundary

This repository stores the reusable framework implementation, tests, build tools,
templates, rules and reference material.

See [packaging/README.md](packaging/README.md) for source and release boundaries,
[docs/guides/install.md](docs/guides/install.md) for installation, and
[docs/workspace-integration.md](docs/workspace-integration.md) for Main/Worker behavior.

Project-instance files such as `AGENTS.md`, `docs/project-map.md`, `docs/requirements/REQ-*.md`, requirement indexes, and reports are local to each target project and must not be committed back to this template repository.

## Core Gates

- No bound REQ, no requirement work (the machine-enforced gate; the PM todo block was removed from the REQ template).
- No stage check, no next stage.
- No locked requirement, no design lock, contract lock, task split, Builder dispatch, or feature branch.
- No UI Design Package Gate, no FE/BE/SYNC contract lock for UI-impacting requirements.
- No `loop-harness req bind` on a human-locked REQ, no Engineering Loop and no contract lock, formal task split, Agent Team, Builder dispatch, or implementation branch.
- Engineering Loop binding and Claude `/loop` are independent lifetimes. `/loop` only delivers the Layer 2 Wake-up Prompt; `req bind` supplies engineering authorization, so no separate `/goal` is required.
- No locked contract, no Builder execution.
- No approved task read-back, no sub agent activation.
- No Document Verifier approval, no Builder activation.
- No Delivery Verifier, QA, and E2E Browser evidence, no loop completion.
- No clean full-depth Delivery + QA + E2E review round, no release audit.
- Delivery Verifier / QA / E2E Browser findings enter S8 root-cause investigation before becoming accepted canonical BUGs for Builder repair.
- No human release approval, no squash merge to `master/main`.
