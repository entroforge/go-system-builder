# 联调合同：SYNC-{id}

> 状态：draft / reviewed / locked
> （机器登记只认 locked——reviewed 仅人审中间态，PTR-PLAN-02 前须翻 locked；时机见 protocol #s3）
> 版本：v0.1.0
> 前端合同：FE-{id}
> 后端合同：BE-{id}
> 锁定日期：YYYY-MM-DD

> 锁定状态与依据见 runtime documents[] 与 journal（.claude/loop-events.jsonl）——文件内不再手填

## 1. 文档链接

| 关系 | 文档 | 条款/用途 |
|:---|:---|:---|
| upstream | `docs/requirements/REQ-{id}.md` | FR/流程/验收条款 |
| related | `docs/design/prototypes/{module}/` | 模块当前真相：字段、错误码、状态、规则和交互 |
| related | `docs/contracts/FE-{id}.md` | 调用方行为 |
| related | `docs/contracts/BE-{id}.md` | 提供方行为 |
| downstream | `docs/tasks/TASK-{id}.md` | 联调任务 |
| evidence | `docs/reports/review/REV-{id}.md` | 契约和联调验证 |

## 2. 接口定义

### 上游需求

| REQ | 本合同条款 | 前端合同 | 后端合同 |
|:---|:---|:---|:---|
| REQ-{id} | SYNC-{id} §{n} | FE-{id} §{n} | BE-{id} §{n} |

> 「本合同条款」列是 SYNC 自己的条款号唯一声明居所——CONTRACTS 索引的 `SYNC-{id} §{n}` cell 必须与此列一致（contracts check 机检）。

### UI 设计包映射

| 模块当前真相文件 | 字段 / 错误 / 状态 / 权限 / 副作用 | 前端行为 | 后端行为 |
|:---|:---|:---|:---|
| `scenario-model.json` / `cases.json` / `stories.md` / `flows.md` / current `*.html` | {field/error/state/permission/side-effect} | {behavior} | {behavior} |

### Rule → CASE → Story → PATH → Spec → Evidence

| REQ source_ref | Rule / CASE | Story / PATH | Spec | contract assertion | Evidence |
|:---|:---|:---|:---|:---|:---|
| REQ-{id}/FR-{id} | BR-{id} / CASE-{id} | S-{id} / F-{id} / PATH-{id} | `web/e2e/{module}/*.spec.ts` | {wire shape, error, idempotency and state assertion} | REV/QA/E2E round {n} |

### 请求

```http
{METHOD} {PATH}
Content-Type: application/json
Authorization: Bearer {token}
X-Request-ID: {request_id}
```

```json
{
  "field": "value"
}
```

### 成功响应

```http
200 OK
Content-Type: application/json
```

```json
{
  "data": {}
}
```

## 3. 字段说明

| 字段 | 类型 | 必填 | 约束 | 说明 |
|:---|:---|:---|:---|:---|
| field | string | 是 | {约束} | {说明} |

## 4. 错误码

| 错误码 | HTTP 状态 | 场景 | 前端行为 |
|:---|:---|:---|:---|
| E1001 | 400 | 参数错误 | 展示字段错误 |

## 5. 状态机关联

| 实体 | 事件 | 当前状态 | 下一状态 | 文档 |
|:---|:---|:---|:---|:---|
| {实体} | {event} | {state} | {state} | `docs/design/state/{entity}.md` |

## 6. 幂等、限流和权限

| 项 | 规则 |
|:---|:---|
| 幂等 | {规则} |
| 限流 | {规则} |
| 权限 | {规则} |

## 7. 契约测试

| 用例 | 输入 | 预期 |
|:---|:---|:---|
| CT-001 | {输入} | {预期} |

## 8. 派生任务

| TASK | 路径 | 覆盖接口/事件 | 状态 |
|:---|:---|:---|:---|
| TASK-{id} | `docs/tasks/TASK-{id}.md` | `{METHOD} {PATH}` | pending |

## 9. 变更申请记录

| 日期 | 版本 | 变更内容 | 申请人 | 审批人 | 结论 |
|:---|:---|:---|:---|:---|:---|
| | | | | | pending/approved/rejected |
