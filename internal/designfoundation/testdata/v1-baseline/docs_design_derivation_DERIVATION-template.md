# Design Derivation — REQ-{id}

> Foundation：docs/design/DESIGN.md@vX.Y.Z
> Surface：consumer / operations / {profile}
> Experience role：探索 / 理解 / 判断 / 承诺 / 等待 / 兑现 / 恢复
> Posture：inherit / extend / exception

每个 `UI impact=changed` 的 REQ 在 S2 画模块包之前填写本文件。不要复制整份 Foundation。
开工前先读 `DESIGN.md` §0 Next-agent card。不要把上一页的库默认主色或 hex 写成 inherit。

## Active laws

- LAW-0X：{本 REQ 如何表现这条法则}

## Must not

从 Next-agent card 抄来并具体化到本页。换手失败几乎都发生在这一节空着的时候。

- 不得出现的 CTA / 承诺：{例：本页不得放购买；不得并排两个同等主按钮}
- 不得使用的气氛色：{例：数字与「更优」不用红/绿；不把库 Primary 当品牌锁}
- 不得从施工笔记升级的值：{hex、组件库默认蓝、上一页的按钮文案}

## Macro composition

{先写一页的信息顺序：证据 → 判断 → 行动，或当前 Surface 的对应顺序}

## Stress state

{高密度 / 长内容 / 错误 / 权限中至少一项如何处理}

## New language

{无 / 新增 Pattern 或 Token 候选。不得在此直接改 DESIGN.md}

## Exception

{无 / `decisions/EX-{id}.md` (legacy `exceptions/EX-*.md` 仍兼容)}

## Proof

{本 REQ 用哪个页面、状态或流程证明推导成立}
