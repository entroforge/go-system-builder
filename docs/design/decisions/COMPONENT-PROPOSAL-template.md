# CP-{id} — {component or pattern name}

> 状态：proposed / accepted-local / promoted / rejected
> 日期：YYYY-MM-DD
> Surface：consumer / operations / {name}
> 范围：module-local (`prototypes/{module}/`) / promotion-candidate
> Foundation：docs/design/DESIGN.md@vX.Y.Z

## Semantic role

{Which Grammar role does this serve: action.promise, evidence.support, status.blocking, recovery, …}

## Why existing components fail

{Name the live component or library default and the Law it violates. If Storybook MCP is available, cite `docs-show` output. “It looks nicer” is not a reason.}

## Duplicate check

{Output of `loop-harness design-foundation check` for near-name collisions, or “none”.}

## Do / Don't

- Do:
- Don't:

## Proof

{Anchor or stress screen where this is the only honest expression.}

## Promotion condition

{What second occurrence would justify a global Grammar/Token change. If never, say so.}

## Feedback receipt（仅当本 CP 承载一条反馈事务时填写；否则保留为空，checker 不要求）

| 字段 | 填写 |
|:---|:---|
| Source observation | {Finding ID / counterevidence 行 / 视觉回归 / REQ-* PROOF-* 路径} |
| Affected constraint IDs | {被违反或被证伪的 LAW-*/ANTI-*/INV-*/GR-*/ROLE-*/PAT-*/SUR-*/EX-*} |
| Classification | module Pattern-CP |
| Changed edges | {模块 Pattern / CP 文件 + 受影响 Grammar / Token / 组件 / Proof} |
| Replay evidence | {受影响边的 design-foundation check 结果 + 一个最小回放路径} |
| Status | open / closed |

未通过前保持 `open`；收据字段必须可解析。
