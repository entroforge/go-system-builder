# 开发合同总览

> 需求：REQ-001
> Shared model policy: none
> Shared model reason: REQ-001 §A4-2 明确不做 API / 共享模型 / 事件；本需求没有任何跨边界的数据交换、请求响应类型或共享 schema 需要 JSON Schema 适配。唯一的受管交付物是 Git 仓库内一个只读文本文件，它不是请求/响应载荷，也不由任何消费者解析为结构化对象，因此不存在共享模型基线。
> 状态：locked
> （机器登记只认 locked——reviewed 仅人审中间态，PTR-PLAN-02 前须翻 locked；时机见 protocol #s3）
> 版本：v1.0.0
> PM / Architect：Main（PM / Architect）
> Contractor：Main（PM / Architect）
> 锁定日期：2026-10-21

> 锁定状态与依据见 runtime documents[] 与 journal（.claude/loop-events.jsonl）——文件内不再手填
>
> **本合同集属于 SIMULATION / test-only 夹具。** 它约束的是隔离测试中的一次模拟交付核对，不是真实产品接口合同。

## 文档链接

| 关系 | 文档 | 条款/用途 |
|:---|:---|:---|
| upstream | `docs/requirements/REQ-001.md` | locked REQ 条款（NFR-001 / AC-001 / §A4） |
| related | `docs/architecture/ARCHITECTURE-001.md` | 架构边界与锁定设计 |
| related | `docs/design/decisions/ADR-001.md` | 方向决策与背书的 N/A 清单 |
| related | `docs/design/prototypes/` | 不适用：UI impact=none 且全部 AC 只有 NFR 背书，无模块行为真相变化（§A4-3）；本目录仅保留模板 |
| downstream | `docs/dev/tasks/index-REQ-001.md` | 派生任务 |
| evidence | `docs/reports/review/REV-001.md` | 文档审核 |
| runtime | `.claude/loop-state.json` | 当前状态通过 Runtime identity和状态读取；机器关联由 Runtime/consumer 带出，不要求作者复制指纹或 revision |

## 合同锁定声明

合同 locked 后与其引用的共同设计基线构成 Builder 的执行依据。范围、接口、字段、错误码、状态机、副作用、验收标准变更必须走 change-control。

## 合同清单

| 合同ID | 类型 | 负责人 | 状态 | 版本 | 联调点 |
|:---|:---|:---|:---|:---|:---|
| BE-001 | 后端 | Builder-BE-01 | locked | v1.0.0 | N/A（无 SYNC 联调点） |

### 不适用合同族及依据

| 合同族 | 处置 | 依据 |
|:---|:---|:---|
| FE-001 | not_applicable | REQ-001 §A4-1：顶部 `UI impact=none`，不新增页面、组件、交互或可见状态；无前端责任 |
| SYNC-001 | not_applicable | REQ-001 §A4-2：不定义 API、事件或共享 schema；无跨端数据契约，也无 shared model policy |

## 联调点矩阵

| 联调点 | 前端合同 | 后端合同 | 契约文档 | 状态 |
|:---|:---|:---|:---|:---|
| N/A | N/A（无前端消费方） | N/A（无跨端数据契约） | N/A | N/A |

无联调点：本 REQ 不产生 SYNC 合同（§A4-2）。BE-001 的唯一产出是交付来源台账登记结论，由 S6 Builder Result 直接交给原消费者，不经过任何前端↔后端数据契约协商。

## UI 设计包输入

该表也登记服务行为输入：`UI impact=none` 且 AC 指向 FR 时填写模块七文件行为包
（五个源文件与两个生成文件，见 `docs/rules/scenario-model.md`），不要求 HTML/PATH。
服务 CASE 的验证来源是原服务入口的单元/集成检查；`browser_required:false` 只免除
浏览器 spec，不允许删除 CASE。不能仅凭 none 把模块与验证来源填成 N/A。

| REQ source_ref | UI impact | 模块当前真相包 | 合同影响 | E2E 来源 |
|:---|:---|:---|:---|:---|
| REQ-001/NFR-001 | none | N/A —— 依据不是"none 本身"，而是：`scenario bridge` 判定 REQ-001 的 1 条验收标准全部由已声明 NFR 背书（1 endorsed N/A）、无 FR 来源，且模块行为真相不变（§A4-3），因此本 REQ 不产生也不修改任何模块行为包 | BE-001（只读核对责任；无 FE / SYNC 影响） | N/A —— 本需求无浏览器表面、无 PATH、无浏览器 spec（§A4-1 / §A4-3）；S7 的 E2E Claim 以 `applicability=not_applicable` + `na_rationale` 处置，验证来源是 BE-001 §7 的文件内容核对 |

模块输入的机器指纹/登记关联由 Runtime 与原 consumer 使用，作者只填写实际模块、合同影响和验证来源，不拼接或抄写 fingerprint。

## 需求覆盖矩阵

条款列每条款一个 `{id} §{n}` 记号，同一条款可并列多个记号——本矩阵是条款宇宙唯一居所；每个 `§n` 必须与目标契约正文任一「本合同条款」列声明的条款号一致（contracts check 机检，数字精确匹配）；任务侧覆盖声明（TASK §3）由 `tasks check` 对此聚合对账。

| REQ source_ref | Rule → CASE → Story → PATH → Spec | FE 合同条款 | BE 合同条款 | SYNC 条款 |
|:---|:---|:---|:---|:---|
| REQ-001/NFR-001 | N/A —— 该 AC 无 FR 来源，只有 NFR-001 背书（`scenario bridge`：1 criteria, 0 reach FR→CASE, 1 endorsed N/A），因此不进入 CASE / Story / PATH / Spec 链 | N/A（FE not_applicable，§A4-1） | BE-001 §7 | N/A（SYNC not_applicable，§A4-2） |

## 合同变更记录

| 日期 | 版本 | 变更内容 | 申请人 | 审批人 | 影响合同 |
|:---|:---|:---|:---|:---|
| 2026-10-21 | v1.0.0 | 首次锁定（simulation/test-only）：只签发 BE-001；FE / SYNC 记为 not_applicable 并写明 §A4 依据 | simulation-human（test-only） | simulation-human（test-only） | BE-001 |

## Shared model baseline

先阅读 [共享模型规则](../../rules/shared-model-contracts.md)，从 [模型入口](../../architecture/data-model/MODEL-{id}.md) 定位现有权威定义。先共同收敛数据与 SYNC，再派生 FE/BE；不得按 REQ 复制模型。

**本 REQ 无共享模型基线。** `Shared model policy: none`，理由见文件顶部 `Shared model reason`（REQ-001 §A4-2：无 API / 共享模型 / 事件）。不涉及共享数据，因此按本节规则删除占位表，不伪造 Operation / Slot / Schema / Consumer 行，也不假装任何既有协议已被 JSON Schema 适配器验证。
