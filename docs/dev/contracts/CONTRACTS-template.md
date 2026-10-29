# 开发合同总览

> 需求：REQ-{id}
> Shared model policy: json-schema-v1
> 状态：draft / reviewed / locked
> （机器登记只认 locked——reviewed 仅人审中间态，PTR-PLAN-02 前须翻 locked；时机见 protocol #s3）
> 版本：v0.1.0
> 规划责任：{Main / 实际专业责任人；本阶段不预建 Builder}
> 作者维护本合同的真实状态与专业版本；登记时间、摘要和锁定关联由原消费者记录到 Runtime documents[] 与 journal，不另填机器元数据。

## 文档链接

| 关系 | 文档 | 条款/用途 |
|:---|:---|:---|
| upstream | `docs/requirements/REQ-{id}.md` | locked REQ 条款 |
| related | `docs/design/{path}.md` | 设计约束或 N/A |
| related | `docs/design/prototypes/{module}/` | 适用 UI 或服务行为的模块当前真相包 |
| downstream | `docs/dev/tasks/index-REQ-{id}.md` | 派生任务 |
| evidence | `docs/reports/review/REV-{id}.md` | 文档审核 |
| related | {当前 view 提供的实际架构 / ADR 路径} | 冻结设计来源；机器身份与登记关联由系统带出 |

## 合同锁定声明

文档 `locked` 是作者对本批专业内容就绪的声明，不单独成立系统接收或冻结。当前 S3 尚未接收的输出可连续修正、自审，完成所需 Git 交付后保存原答卷；不因文档已写 locked 就禁止修正。已接收的合同与其引用的共同基线构成后续执行依据，其范围、接口、字段、错误码、状态机、副作用或验收标准的实质变化，以及已冻结上游的变化，沿原正式返工 / change-control 处理，不能覆盖已接受事实或刷新指纹。

## 合同清单

| 合同ID | 类型 | 负责人 | 状态 | 版本 | 联调点 |
|:---|:---|:---|:---|:---|:---|
| FE-001 | 前端 | {实际前端合同负责人} | draft | v0.1.0 | SYNC-001 |
| BE-001 | 后端 | {实际后端合同负责人} | draft | v0.1.0 | SYNC-001 |
| SYNC-001 | 共同操作协议 | {实际协议合同负责人} | draft | v0.1.0 | SYNC-001 |

只列真实适用契约；FE/BE/SYNC 分别判断，不适用类型说明依据并删除占位文件行。服务场景无 FE 时不创建 FE 占位；仅 FE 本地行为或仅 BE 内部交付可不适用 SYNC，实际共享边界也可只有 SYNC 而无独立 FE/BE 合同。三类均不适用且实际交付为空时，当前接收器尚不支持该阶段适配；报告具体缺口，不能虚构 BE 或谎报 required。

## 联调点矩阵

仅实际共享操作填写；SYNC 不适用时删除示例行并在此写明依据，不创建占位合同。合同清单「联调点」列和覆盖矩阵各不适用合同列同样填写有据 N/A，保留所有实际条款与 CASE。

| 联调点 | 实际消费方 | 实际提供方 | 契约文档 | 状态 |
|:---|:---|:---|:---|:---|
| SYNC-001 | {API 调用者/worker/适用 FE 合同} | {实际提供方合同} | `SYNC-001.md` | draft |

## UI 设计包输入

该表也登记服务行为输入：`UI impact=none` 且 AC 指向 FR 时填写模块七文件行为包
（五个源文件与两个生成文件，见 `docs/rules/scenario-model.md`），不要求 HTML/PATH。
服务 CASE 的验证来源是原服务入口的单元/集成检查；`browser_required:false` 只免除
浏览器 spec，不允许删除 CASE。不能仅凭 none 把模块与验证来源填成 N/A。

| REQ source_ref | UI impact | 模块当前真相包 | 合同影响 | 适用验证来源 |
|:---|:---|:---|:---|:---|
| REQ-{id}/FR-{id} | none | `docs/design/prototypes/{module}/`（七文件服务行为包） | {实际 BE/SYNC 等合同} | CASE / F → {实际服务单元/集成/契约测试入口} |
| REQ-{id}/FR-{id} | changed | {适用 UI 行为包，含实际页面} | {实际 FE/BE/SYNC 合同} | CASE / PATH → {实际浏览器 Spec} |

模块输入的机器指纹/登记关联由 Runtime 与原 consumer 使用，作者只填写实际模块、合同影响和验证来源，不拼接或抄写 fingerprint。

## 需求覆盖矩阵

条款列每条款一个 `{id} §{n}` 记号，可并列多个（如 `BE-001 §2, BE-001 §3`）——本矩阵是条款宇宙唯一居所；每个 `§n` 必须与目标契约正文任一「本合同条款」列声明的条款号一致（contracts check 机检，数字精确匹配）；任务侧覆盖声明（TASK §3）由 `tasks check` 对此聚合对账。

目标声明使用带表头、分隔行的普通 Markdown 管道表格；「本合同条款」列可写 `§2` 或目标自身的 `BE-001 §2`。其他列、正文提及和代码围栏示例不能补声明。机检只核编号结构，承诺含义和覆盖充分性仍需 S3 作者及 S5 独立审查，不另填条款证明。

| REQ source_ref | Rule → CASE → Story / Flow → 适用 Spec | FE 合同条款 | BE 合同条款 | SYNC 条款 |
|:---|:---|:---|:---|:---|
| REQ-{id}/FR-{id} | BR-{id} → CASE-{id} → S-{id} → F-{id} → {实际服务测试；仅浏览器分支加 PATH/浏览器 Spec} | FE-{id} §{n} | BE-{id} §{n} | SYNC-{id} §{n} |

## 合同变更记录

| 日期 | 版本 | 变更内容 | 申请人 | 审批人 | 影响合同 |
|:---|:---|:---|:---|:---|:---|
| | | | | | |


## Shared model baseline

先阅读 [共享模型规则](../../rules/shared-model-contracts.md)，直接引用项目实际权威定义；已有模型导航时可用其定位，不为本模板另建 MODEL 文档。仅实际共享操作适用时，先共同收敛适用的数据模型与 SYNC，再派生参与方 FE/BE；FE、BE、SYNC 分别判断，不得按 REQ 复制模型。

| Operation | Slot | Schema | Consumers | Valid example | Structural negative |
|:---|:---|:---|:---|:---|:---|
| {operationId} | {request/response-200} | [权威定义](../../architecture/data-model/{schema}.json) | {仅列实际消费者的 Markdown 链接，如 [后端](BE-{id}.md) [协议](SYNC-{id}.md)} | [合法数据](../../architecture/data-model/{valid}.json) | N/A |

不涉及共享数据时改为 `Shared model policy: none` 并添加具体 `Shared model reason`，删除占位行。既有其他协议不得假装已被 JSON Schema 适配器验证。
