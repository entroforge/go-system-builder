# 重构后测试缺口全面审计 — `go-system-builder` @ `d0d948a`

> 上一版:[CI-AUDIT.md](./CI-AUDIT.md)(重构前的 CI 速度审计)。
> 本版是**新的一轮重构之后**的测试缺口审计,由 11 个 sub-agent 分两阶段完成。
>
> **本报告的三份附件**:
> - [`TEST-GAP-AUDIT-SEAMS.md`](./TEST-GAP-AUDIT-SEAMS.md) —— 跨 stage 接缝与系统层 agent 的完整原始报告(11 张表,含 26 条逐条失败清单与 12 项接缝矩阵)
> - [`TEST-GAP-AUDIT-FAILURES.md`](./TEST-GAP-AUDIT-FAILURES.md) —— 失败根因归组 agent 的完整原始报告(13 个根因组,385 条失败实体的完整清单表)
> - [`CI-AUDIT.md`](./CI-AUDIT.md) —— 上一版的 CI 速度审计(本报告 §7 沿用其结论)

**基线提交**:`d0d948a` — `refactor: converge S7-S9 on natural-event reception and drop hash-proof machinery`
(分支 `codex/hook-handoff-wiring-20261025`,1569 文件,+64080 / −103729 行,净删 69k 行)

**方法**:逐 stage 审查(7 个 agent,按 stage 切片分工)+ 跨 stage 审查(3 个 agent:接缝/系统层、契约与不变量、CI 车道适配)+ **失败根因归组(1 个 agent,385 个失败实体 → 13 个根因组)**。共 **11 个 sub-agent**。
每个 agent 的硬性指令:**发现失败测试不停止,记录后继续,直到整个 stage 覆盖完**;统一 `-p 2 -timeout=20m`(宽松超时,避免多 agent 并发时的假超时);
**每个失败都在父提交 `9bf73ca` 上复跑对照**,从而把「本次重构引入的回归」与「既有失败」严格分开。

**标签**:`[M]` 实测 · `[S]` 静态推断 · `[U]` 未验证。

---

## 0. 执行摘要

### 0.1 结论先行

**这轮重构把测试套件从「红」变成了「更红,但红得更精确」。** 绝大多数包是干净的;真正的破坏集中在 `internal/cli`(409 个测试文件)和 `internal/investigation` 两处。

**而且现在 CI 连「跑起来」这一步都做不到** —— 这比测试失败更靠前:

| # | 硬事实 | 实测位置 |
|---|---|---|
| **1** | **`make fmt-check` 必挂** —— `gofmt -l cmd internal tests` 在 `d0d948a` 返回 **3 个文件**(父提交 `9bf73ca` 是 0 个)。`ci-verify` 的第一步就死,后面所有测试都跑不到 | §7.1 |
| **2** | **`make ci-verify-full` 必挂** —— `internal/cli` 整包在 `-timeout=30m` 处被 `panic: test timed out` 强杀(1800.097s) | §9.1 |
| **3** | **`make test-ci-smoke` 必挂** —— 52 个 smoke 测试里 `TestNativeS5AmbiguousReferencesDoNotReceiveEitherAnswer` 失败 | §9.4 |
| **4** | **`internal/cli` 的 1197 个测试,CI 只跑 52 个(4.3%)** —— `test-ci-core` 用 sed 把 `cli`/`integration`/`investigation` 三个包**整体剔除**,它们只靠 smoke 正则的少数名字被覆盖 | §7.3 |
| **4b** | **`tests/system/*` 的 110 个端到端验收测试(98 req039 + 12 recovery)完全不在 CI 里** —— 被 `test-ci-core` 剔除后没有任何车道接管 | §7.3 |
| **5** | **smoke 正则有 4 个死条目** —— 指向已不存在的测试名,`go test -run` 静默跳过,**CI 绿灯不代表它们在跑** | §7.4 |

| 层次 | 结果 |
|---|---|
| **包级**(100+ 个包,含 `internal/runtime` 679、`internal/review` 399、`internal/qualitygate` 160、`internal/workpacket` 131、`internal/submission` 109、`internal/scenario` 166、`internal/schema` 192) | **几乎全绿**。唯一有包级失败的是 `internal/investigation`(**21 个**) |
| **`internal/cli` 整包实测** | **247 个顶层失败 + 138 个唯一子测试失败**(768 通过 / 1021 启动 / 176 从未启动)。绝大多数是**本次重构引入的回归** |
| **接缝/系统层实测**(`internal/integration` + `tests/system/*`) | **26 个顶层失败 + 12 个子测试失败**,其中 **22 个是回归**(父提交 PASS → HEAD FAIL),4–5 个是既有失败。**`internal/integration` 的 32 个测试是被删掉的(115→83),不是跑挂的** |
| **FLAKY-ORDER** | **0**。所有失败在 `-p 1 -count=1` 单独重跑下依然失败 —— 全部是确定性缺陷,不是并发假象 |
| **契约漂移** | `INV-013`、`INV-017` 陈旧;`forbidden_events` 的运行时消费者被**本次重构删除** |
| **HIGH 级产品缺陷** | **17 条**。H1–H9(逐 stage)+ **H10 S7 出口被吞**、**H11 恢复命令对损坏 Runtime 不可用**、**H12 S6 范围校验被删**(接缝)+ **H14 派发改写 TASK 事实**、**H15 S9 schema 拒历史记录**、**H16 Inspect 缺 Worktree/Branch**、**H17 审计行重复**、**H18 review-plan 路径为空**(根因归组)+ MED: H13、M1–M3 |

### 0.2 最重要的结论:失败几乎全是「重构引入」,不是「测试过时」

各 agent 用父提交 `9bf73ca` 做基线对照,结果高度一致:

| Stage | CLI 切片失败 | **本次重构引入** | 既有失败 |
|---|---|---|---|
| S0–S2 | 34 | **23** | 4 |
| S3–S4 | 17 | **16** | 1 |
| S5–S6 | 41 | **30** | 11 |
| S7 | 34 | **34** | 0 |
| S8–S9 | 97(+21 包级) | **90** | 7 |
| S10–S11 | 13 | **12** | 1 |

**约 205 个 CLI 失败是这轮重构砸出来的**,只有 24 个是既有缺陷。这改变了处理策略:**先修代码,不要先删测试。**

> 父 agent 的整包实测把这条结论从「切片推断」升级为「整包事实」:247 个失败里,S9 一族(`TestS9*` 50 + `TestNativeS9*` 37 = **87 个,占 35%**)是最大的失败簇。
> **但归因 agent 的实测修正了直觉**:这 87 个**并非**都指向 H9 —— H9(R03)只命中 10 个实体,其余分散在 R02/R10/R11/R12 等根因。**S9 是「症状簇」,不是「一条根因」。**

### 0.3 这轮重构的主线,以及它砸到了什么

重构主线是**移除"机器证明"**:hash/digest 证明、capture/local-evidence 接线、frozen-diff、fingerprint、S7 预算、clean-round 机器证明、reentry 预算、observation-batch / readback schema。
同时把 S7/S8/S9 的原生答案接收统一为 **PreToolUse 监听 → 成功 PostToolUse → Stop/SubagentStop 兜底,经 original consumer 流转**。

方向是对的。但**"删除证明"和"删除检查"被混为一谈**了 —— 有 **17 处**把与 hash 无关的保护一起删掉了(见 §3 的 H1–H18)。

### 0.4 【新】失败面高度集中:坏的是接线层,不是领域逻辑

整包实测显示一个贯穿全局的规律 —— **除 `internal/investigation` 外,所有包级失败都只发生在 `internal/cli`**:

- `internal/transition` 316 测试全绿、`internal/review` 399 全绿、`internal/runtime` 679 全绿、26 个基础设施包 1363 测试 0 失败;
- `internal/cli` 247 个失败,涉及 **132 个测试文件**;
- 失败最密集的是 `s9_handoff_wiring_test.go`(15)、`native_s9_recovery_test.go`(9)、`ct_test.go`(7)、`run_test.go`(7)。

**结论:领域模型基本完好,坏的是 CLI 的接线/路由/兜底接收。** 这决定了修复顺序 —— 修 17 条 HIGH 缺陷能一次性解锁大部分失败,而不是逐个改测试。

### 0.5 【新】比失败更危险的:三条主接缝现在一个测试都没有

接缝 agent 对 12 个跨阶段交接点逐条判定(完整表见 §6.4),其中三条**完全没有覆盖**:

| 接缝 | 状态 | 后果 |
|---|---|---|
| **S8→S9**(investigation → repair) | **NOT COVERED** | 没有任何测试驱动这一跳 |
| **S9→S7 再验证**(完整闭环) | **NOT COVERED** | 父提交的 `TestCT03914_FindingToBugRepairFullReview` 被删、`req039fixtures.SeedSealedObservationBatch` 被删 → **主闭环断了** |
| **S9 修复后的后继发布** | **NOT COVERED** | 父提交 4 个交付/复查测试文件**全部被删** → 整条 S9 交付链**丢掉了 L4 层** |

而 `tests/fixtures/native-stage-simulation` 其实只给 **Python 驱动 `tools/claude-full-stage-simulation.py`** 用,**不在 Go 测试套件里**。

> **也就是说:这轮重构之后,S7→S8→S9→S7 这个主闭环,在 Go 套件内没有任何一个测试能跑通它。** 这比「247 个测试红了」严重得多 —— 红着的测试至少会报警。

此外,还有一批**被静默删除的连续性保证**(§6.5):`internal/runtime/checkpoint_indices.go` 从 **271 行砍到 17 行,26 项数据完整性检查全没了**,而这 26 条错误串在 HEAD **和**父提交的任何测试里都**零出现** —— 所以它被删得**毫无阻力**。

---

## 1. 工作区冻结记录

```
d0d948a  refactor: converge S7-S9 on natural-event reception and drop hash-proof machinery
         1569 files changed, 64080 insertions(+), 103729 deletions(-)
```

- 提交前 `go build ./...` **退出 0**、`go vet ./...` **退出 0**(995 个已跟踪 + 70 个新增测试文件全部类型检查通过)——**所有失败都是断言层失败,不是编译错误**。
- 唯一额外改动:`.gitignore` 增加 `*.lock.process`(运行时锁文件 `internal/submission/.lock.process` 此前未被忽略,下一次 `git add -A` 会把它带进去)。
- `integration.test`(41MB)与 `.local/`(8.4GB)均为 gitignore 的构建产物,未进入提交。
- 提交后工作区干净。

---

## 2. 失败总账

### 2.1 包级:唯一有失败的是 `internal/investigation`

`internal/investigation` **21 个失败,全部是本次重构引入的回归**(父提交只有 4 个失败,且那 4 个现已修复)。
`internal/repair`(93)、`internal/repairpolicy`(2)、`internal/diagnostic`(2)、`internal/diagnosticstorage`(3)、`internal/bugprojection`(4)、`internal/missingtokens`(6)、`internal/evidence`(7)、`internal/targetedauthoring`(2)全部通过。

21 个失败集中在三类(详见 §3):

| 类别 | 数量 | 说明 |
|---|---|---|
| STALE-TEST | 14 | 断言了蓝图**明确移除**的机器门(`L3-S8:299`/`:307`「不新增机械必填门」)→ 应改写 |
| PRODUCT-BUG | 5 | 正向要求仍然存在,代码却违反了 → 真缺陷 |
| STALE-FIXTURE | 2 | fixture 仍写已删的 SHA 字段 / 重复 journal event id |

### 2.2 `internal/cli`:伤害集中地

12 个 stage 的 agent 分别跑了自己的切片,全部失败按 §0.2 分类。几个规模的切片:

| 切片 | 选中 | 失败 |
|---|---|---|
| S0–S2 给定正则 | 68 | 8(另补跑漏选的 118 个 → 再得 26) |
| S3–S4 | 97 | 17 |
| S5–S6 | 156 | 41 |
| S7 | 118 | 34 |
| S8–S9 | 270 | 97 |
| S10–S11 | 53 | 13 |

> ⚠️ 切片之间有重叠,且**切片 ≠ 全包**。父 agent 的整包实测给出权威数字:**247 个顶层失败 + 138 个唯一子测试失败**(§9)。切片加起来是 210,差的 37 个落在切片的正则缝隙里 —— 这本身就证明了 §2.3 的结论。

> ⚠️ **切片之间有重叠**(例如 S7 的正则会命中部分 S8 测试),不能简单相加。完整 `internal/cli` 包的唯一失败数以本文末尾的整包运行结果为准。

### 2.3 方法论级发现:按正则切 stage 不可靠

S0–S2 agent 实测发现我给它的正则 `^(Test)?(S0|S1|S2|Design|...)`:
- **多选 31 个** —— `S1` 前缀匹配到了 `S10`/`S11`;
- **漏选 118 个** —— 真正的 S0/S1/S2 测试大量用 `TestREQ*`、`TestEntryQuestionnaireS0*`、`TestContract*`、`TestUnbind*` 命名,不带 `S0/S1/S2` 前缀。

agent 自己补跑了漏选的 118 个,否则该 stage 报告会严重不完整。

**这直接证明:仓库里 `internal/cli` 的 409 个测试文件没有一致的命名约定,任何基于测试名的 stage 切片都不可靠。** 正确的切分单位是**文件**或**构建标签**,不是函数名正则。

### 2.4 接缝与系统层(接缝 agent 独立测得)

接缝 agent 在**独立克隆**上跑完了 `internal/integration` + `tests/system/*` + `tests/policy` + `tests/assignment`,每个包都在 HEAD 与父提交 `9bf73ca` 上各跑一遍对照。

| 包 | HEAD | 父提交 | 时长(HEAD) |
|---|---|---|---|
| `internal/integration` | 83 测试 → **2 失败** | 115 测试 → **0 失败** ✅ | 166.2s |
| `tests/system/req039` | 98 测试 → **19 失败** | 114 测试 → 45 失败 | 83.1s |
| `tests/system/recovery` | 12 测试 → **5 失败** | 12 测试 → **0 失败** ✅ | 6.6s |
| `tests/policy` | 20 → 0 | 20 → 0 | 0.17s |
| `tests/assignment` | 6 → 0 | 10 → 0 | 0.89s |

合计 **26 个顶层失败 + 12 个子测试失败 = 38 个失败事件**,其中 **22 个是父提交通过 → HEAD 失败(回归)**,**4–5 个是既有失败**。

**两个必须记下的事实:**

1. **`internal/integration` 的 32 个测试是被删掉的,不是被跑挂的** —— 父提交 115 → HEAD 83。16 个测试文件在本轮重构中被删除,**没有替代断言**。
2. **`tests/system/req039` 的父提交基线本身就是坏的(45 个失败)** —— 所以这里的「回归」判定用的是最严格的信号:**父提交 PASS → HEAD FAIL**。接缝 agent 特意在**独立克隆**(非 linked worktree)里复跑父提交,排除了 worktree 伪影,确认这 45 个失败是**真实既有失败**。

**修正一条我此前的说法**:我此前以为父提交 `internal/integration` 也有 `hook output is not JSON: EOF` 这类失败 —— **错**。那些错误来自父提交的 `tests/system/req039`,`internal/integration` 在父提交上是干净的 `ok`。

### 2.5 【新】13 个根因组 —— 385 个失败实体的归因总账

归因 agent 把 **385 个失败实体**(247 顶层 + 138 子测试)按错误签名 + 修复落点聚成 **13 组**,并对每组做父提交 `9bf73ca` 对照:

| 组 | 规模 | 判定 | 修复落点 | 该改什么 |
|---|---|---|---|---|
| **R01** quality_gate 投影丢失 | **32**(8 顶层 + 24 子) | **既有失败** —— 父提交同错(5/5 代表验证) | `internal/cli/run.go:2961` | **先定 quality_gate 与 graph 的设计优先级**,再决定改测试还是改代码 |
| **R02** 4 个 CLI 子命令被删 | 7 | **回归**(父提交 PASS,2/2) | `internal/cli/run.go:1855` | **改测试** —— 子命令入口已被蓝图取代 |
| **R03** 原生 Result 解不回 Plan | 11 | **回归**(父提交 PASS) | `internal/cli/native_s9_rcovery.go:115` | 改产品(= H9) |
| **R04** Inspect 缺 Worktree/Branch | **16** | **回归**(父提交 PASS) | `internal/integration/inspect_responsibility.go:20-22` | 改产品 → **H16** |
| **R05** Stop 收不到兜底答案 | 15 | **回归**(父提交 PASS) | `internal/cli/native_review_hooks.go:553-569` | 改产品(= H1) |
| **R06** 绑定 owner 改写 TASK 事实 | 11 | **回归**(父提交亦失败但**原因不同**) | `internal/assignment/register.go:472-475` | 改产品 → **H14** |
| **R07** schema 拒历史 hash 字段 | 7 | **回归** —— **真缺陷,不是陈旧测试** | `internal/schema/assets/repair-workspace-candidate.schema.json:4` | 改产品 → **H15** |
| **R08** 同事件重试重复审计 | 6 | **回归**(父提交 PASS) | `internal/cli/hook_audit.go:13` | 改产品 → **H17** |
| **R09** WorkerChecks → `null` | 4 | **回归** | `internal/cli/assignment_context.go:339` | 改产品(= H7) |
| **R10** review-plan 空路径 | **14** | **回归** | `internal/cli/s7_status.go:425` | 改产品 → **H18** |
| **R11** 原 S9 路径未保留 | 5 | **回归** | `internal/cli/s9_impact_observer.go:20` | 改产品(= MED-1) |
| **R12** 缺当前 Record 投影 | 3 | **回归**(待二次确认) | `internal/cli/integration_ack.go:127` | **改 fixture** —— legacy-Assignment 形态(= 接缝 #12) |
| **R99** 长尾 | **254** | **混合**,需按测试文件分片 | 分散 | 逐片做父提交对照(需 100+ 次重跑,超出本次预算) |

**三条元结论:**

1. **除 R01 与 R02 外,R03–R12 全部应改产品代码。** 只有 R01(既有失败,且设计意图待定)和 R02(子命令入口已被蓝图取代)属于「改测试」。
2. **最大的那组(32 个)恰恰是既有失败** —— 这纠正了直觉:「`QUALITY_GATE` 投影丢失」看着最像重构砸的,实际父提交同样坏。归因 agent 在组内 5 个代表上逐一验证,结论一致。
3. **176 个从未启动的测试与 R01–R12 的特征零交集**(4 项关键词检索全部 0 命中),抽样重跑 5 个**全部 PASS** → 判定为超时所致,大概率不会失败。**但只覆盖了 2.8%**,余下 171 个理论上仍可能个别落在 R99 长尾里。

---

## 3. HIGH 级产品缺陷清单(最重要交付)

下面 **17 条 HIGH**(H1–H12、H14–H18)+ 1 条 MED(H13)是**这轮重构砸出来的真缺陷**。每条都有 file:line、蓝图依据和实测证据。它们不是"测试过时",是代码行为被改坏了。

> H1–H9 来自逐 stage 审查;**H10–H13 来自跨 stage 接缝审查**(§2.4、§6.4);**H14–H18 来自失败根因归组**(§2.5)。后两批都是本轮审计后期才浮现的 —— 分层审查与根因归因缺一不可。

### H1 — S7 的 Stop/SubagentStop 兜底接收**完全失效** `[M]`

| | |
|---|---|
| 位置 | `internal/cli/native_review_hooks.go:553-569`(`nativeReviewTaskText`) |
| 现象 | 新观察器链的第三条腿(Stop/SubagentStop 兜底)**收不到 Reviewer 的答案** |
| 根因 | 新构建器只读 `in.AgentTranscriptPath`,读不到就返回 `""`。而平台 SubagentStop 载荷把精确引用放在 **`last_assistant_message`**,不在 transcript 里。父提交 `nativeTaskTextDiagnostic`(`internal/cli/native_task_transcript.go:242-254`)有 `LastAssistantMessage` 回退,**被重构删掉了** |
| 精确引用选择 | 因此找不到候选 → 兜底永不观察答案 |
| 证据 | **同一份探针代码在两个提交上对跑**:父提交 `received=true`(owner id 与外来 agent id 均收到);`d0d948a` 为 `received=false`,外来 agent id **连诊断输出都是空的** |
| 蓝图依据 | `L3-S7:221`「`PreToolUse`、成功 `PostToolUse`、`SubagentStop` 使用同一接收路径观察自然保存的原答卷」;`L3-S7:219` |
| 影响 | S7 的 3 个测试全红(其中 1 个是本提交新增的)。Worker 在最后一次 PostToolUse 之后才保存答案的场景,**该答案永远收不到** |
| 修法 | 恢复 `LastAssistantMessage` 回退分支 |

### H2 — S8 人工审批权威门被静默删除 `[M]`

| | |
|---|---|
| 位置 | `internal/investigation/hypothesis_result_receipt.go:257`(`validateS8ApprovalMode`) |
| 现象 | **`plan_approval_required` 模式的 S8 Investigator 现在可以不经人工审批直接提交并被接受** |
| 根因 | 该函数在父提交有 **2 个调用者**(preflight + CAS `validateCurrent`)。重构后 `ReceiveIssuedHypothesisResult`(`:41`)不再调用它,新的 `validateS8ResultResponsibility`(`:363`)只检查 case/status/assignment。**全仓 grep 确认 0 调用者** |
| 证据 | `internal/investigation/hypothesis_result_registration_test.go:174` 失败,错误信息正是 `S8 answer from the unapproved plan_approval_required Investigator was accepted` |
| 蓝图依据 | `L4-agent-dispatch-governance.md:113`「原责任明确要求真正批准时,必须满足其原批准条件;任务指引不能把这些要求隐去」;`:83`「真正批准仍须真实发生,Start 和保存不能代签」 |
| 影响 | **这是本次审计最严重的发现之一**。人工审批权威被静默削弱,没有任何一条测试在正常路径上拦截 |
| 注意 | 同包 `TestTechnicalReviewCannotMasqueradeAsHumanApproval`(`delegation_test.go:213`)**仍然通过** —— 被删的是**另一条** S8 专属的审批门,delegation 路径的权威断言完好 |

### H3 — S8 因果闭合链整体成为死代码 `[M]`

| | |
|---|---|
| 位置 | `internal/investigation/case_workflow.go:850`(`causalClosureDefect`)、`:1167`(`validateCausalClosure`)、`:1123`(`requireCompetingHypothesisClosure`) |
| 现象 | `requireCompetingHypothesisClosure` 及整条因果闭合链 **0 调用者**(穷举 grep 确认) |
| 后果 | `UpdateCaseRoute` 现在接受**无效的** `competing_hypothesis_assessment`(未知/不确定/重复的 hypothesis id)。`TestCompetingAssessmentUsesCurrentConclusiveDiscriminator` 失败:`invalid assessment authorized repair` |
| 蓝图依据 | `L3-S8:160`「assessment 必须引用由**当前结论性结果**区分的可信备选」 |
| 影响 | Main 可以把**未解决的**备选声称为已关闭,并据此路由到 S9 修复 —— S9 合同建立在一个虚假的因果声明上 |
| 同批死代码 | `requireCurrentHypothesisResults`(`:1027`)、`validateBlastRadius`(`:1075`)、`validateDetectionGap`(`:1099`)。**注意**:这三者的"移除"是蓝图 `:297`/`:299` 明确要求的,只是函数没删干净 —— **不要重新接上** |

### H4 — S6 的 TR-003 共同基线校验被删除 `[M]`

| | |
|---|---|
| 位置 | `internal/buildsurface/read.go`(重构删除了 `RequireCurrentS6MainStartContext` 调用;父提交 `read.go:57`) |
| 现象 | **没有 TR-003 共同 Main 基线时,S6 集成仍能跑通**,回执宣称 `integration verified; completion_ack recorded` |
| 蓝图依据 | `L3-S6-build.md:46`「正常入口为 `building`,前提是 TR-003 已实际提交」;`:38`、`:58` |
| 影响 | 阶段出口条件已不可能满足,回执却宣称集成已验证 → **门被静默拆掉** |
| 证据 | `internal/cli/s6_handoff_e2e_test.go:174` `TestS6IntegrationWithoutMainStartKeepsOriginalBatchGate` 失败 |

### H5 — S3 契约包的权威根绑定校验被误删 `[M]`

| | |
|---|---|
| 位置 | `internal/runtime/contract_authority.go:70-79`(`ValidateContractPacket`) |
| 现象 | 父提交此处有 `fileview.ValidateAuthority(root, state)` —— **与 hash 无关**。重构把它连带删掉了。结果:**任何目录都能对 S3 契约包授权** |
| 蓝图依据 | `L4-state-transition-core.md:31`「权威根由 `bound_req.workspace.project_root` 确定…不能复制一对 Runtime 文件就成为第二个控制面」;`:339` |
| 影响 | 多控制面风险。同一次 diff 还删掉了两个**非 hash** 的结构校验:`reflect.DeepEqual(binding, p.Binding)` 与 `BuildContracts` 冻结包比对 |
| 证据 | `internal/cli/design_answer_e2e_test.go:312` 失败:`other root granted S3 authority` |
| 备注 | 该函数的文档注释仍自称"checks supplied frozen inputs against original facts",**已失真** |

### H6 — S7 的 static/behavior 波次门被整体删除 `[M]`

| | |
|---|---|
| 位置 | `internal/assignment/review_readiness.go:15-19`;`internal/assignment/review_bind.go`;`internal/cli/s7_status.go`;`internal/review/manifest_draft.go` |
| 现象 | `reviewDispatchPrerequisite` 丢掉了 `staticSettled` 参数,`register-workgroup` 不再拒绝 behavior 波次的 assignment,`s7 status` 丢失 `wave readiness:` / `B-admission` |
| 后果 | `review.StaticClaimsSettled` / `RemainingStaticClaims` 成为**死代码** |
| 蓝图依据 | `L3-S7:149`「交付与 QA 属于 `static` 波次,行为执行属于 `behavior` 波次。**静态工作先取得完整处置,再进入行为执行**」 |
| 重要区分 | `L4-workflow-convergence:503` 只禁止**在接收端认证波次**,不禁止**派发端设门** —— 所以这不是"移除机器门"的一部分 |
| 证据 | 2 个测试失败:`s7_guidance_test.go:62`、`s7_native_remaining_test.go:108` |

### H7 — `assignment_context.go:339` 的 nil/空切片 bug `[M]`

| | |
|---|---|
| 位置 | `internal/cli/assignment_context.go:339` |
| 现象 | `copyOf.WorkerChecks = append([]assignmentrecord.WorkerCheck(nil), in.ClosingContract.WorkerChecks...)` → 空列表变成 **nil** → JSON 输出 `null` |
| 后果 | schema `native-assignment-context.schema.json:569` 要求 `"worker_checks": {"type":"array"}` → **S6 assignment-context 校验直接失败,上下文不可交付** |
| 证据 | `internal/cli/s6_closing_context_e2e_test.go:39`、`s6_record_context_e2e_test.go:319` 失败:`validate data: jsonschema … worker_checks: got null, want array` |
| 备注 | `copyContextDispatch` 是本轮重构**新增**的函数 —— 经典 nil/空切片 bug |
| 修法 | 改用 `append([]assignmentrecord.WorkerCheck{}, ...)` 或 `slices.Clone` |

### H8 — S8 的来源集合从「相等」放宽成「子集」 `[M]`

| | |
|---|---|
| 位置 | `internal/investigation/case_workflow.go`(`subsetOf` 取代 `sameStringSet` 用于 `source_finding_ids`) |
| 现象 | 已固定的 Case **可以扩大**自己的来源集合 → 问题可以借用 Main **没有选中**的 Finding |
| 蓝图依据 | `L3-S8:101`「现有实现依赖原批次固定来源集合」;`L3-S8:93` |
| 证据 | `internal/investigation/finding_source_handoff_test.go:166` 失败(3 个子测试):`borrowed source from unselected Finding` |
| 影响 | 溯源被破坏,`explains_finding_ids` 变得不可验证 |

### H9 — S9 的新路由层无法把原生 Result 解回 Plan 包 `[M]`

| | |
|---|---|
| 位置 | `internal/cli/native_s9_routing.go:15`(`nativeS9Routes`)+ `internal/runtime/repair_plan_report_routing.go:23`(`CurrentRepairPlanReportRoutesContext`) |
| 现象 | 这两个函数**是本轮重构新增的**(父提交 0 命中)。以 `native Result path has no exact current original Plan packet` 失败(`internal/cli/native_s9_recovery.go:115`) |
| 蓝图依据 | `L3-S9:253`「自然事件从已有当前任务和原责任坐标准确选责」;`L4:145`「S5／S6／S7／S8／S9 复用同一有界任务读取器」 |
| 影响 | 原生 S9 Plan/Result 的接收与恢复路径**大面积失效** —— 归因实测命中 **10 个失败实体(5 顶层 + 5 子测试)**,归为根因组 R03(见 §2.5)。**注意:早前估算的「22 个」偏高,实测是 10 个;S9 一族的 87 个失败里,大部分来自其它根因。** |
| 相关 | `internal/cli/repair_lead_questionnaire.go:546` 的 `pointer["questionnaire_root"]` 为空,导致另外 6+ 个测试失败(`the original S9 questionnaire path was not retained`) |

### H10 — S7 Stop/SubagentStop 兜底接收被「已接受即静默返回」分支吞掉 `[M]`

> **接缝 agent 发现,父 agent 独立复核后并入。** 与 H1 **是两个不同机制**,必须一起修。

| | |
|---|---|
| 位置 | `internal/cli/native_review_hooks.go:358-372`(新增分支)+ `:396`(被跳过的诊断) |
| 现象 | 新增的「已被接受 ⇒ 静默 no-op」分支在 **`received by the original consumer` 诊断之前**、**在 TR-009 晋级之前**就 `return true, 0` |
| 后果 | ① `original consumer` 诊断永远不打印;② **S7→S10 的出口不再触发** —— `lifecycle.state` 卡在 `verification`,干净的一轮 S7 **停滞在阶段出口** |
| 证据 | `internal/integration` 的两个 S7 issued-result 测试在 HEAD 失败、在父提交通过:`active S7 Stop did not report original reception: stdout= stderr=`(`internal/integration/s7_issued_result_external_test.go:433`) |
| 蓝图依据 | `L4-workflow-convergence.md:437`「适用必要责任完成且无 Finding 时经 **TR-009** 准备 S10」;`:392`「不能要求 Main 另用普通工具触发 TR-008／TR-009」;`L3-S7-verification-round.md:384` |
| 静态证据 | `s7SuccessorPending` / `reviewPacketAppliesToCurrent` 在父提交**不存在** → 是本轮新增 |
| 严重度 | **HIGH** —— 这是**阶段出口**级别的断裂,让整条 S7→S10 链变得不可观测 |

### H11 — `runtime recover apply` 对「损坏的 Runtime」本身不可用 `[M]`

| | |
|---|---|
| 位置 | `internal/runtime/recovery.go:522-526` → `internal/cli/recovery_checkpoint.go:180` → `internal/runtime/checkpoint.go:550` |
| 现象 | 恢复流程现在把**恢复候选**与**仍然存在的、已损坏的活动 Runtime**做比对 → 报 `LOOP_RECOVERY_INPUT_DRIFT: recovery input drift: checkpoint disagrees with current Runtime identity/baseline/REQ` |
| 根因 | 父提交只对**已索引的候选**调用该守卫(`internal/runtime/recovery.go:370-375`,在 `if hasIssuedIndices(state)` 之内),所以损坏的 Runtime 会先被隔离;本轮重构把这个条件**去掉**了 |
| 证据 | **实测**:在 HEAD 上给 `checkpoint.go:550` 打点,看到 `current={"runtime":"corrupt-idempotency"} pair_runtime_id=loop-REQ-039`;在父提交上 `ValidateCheckpointRestore` **根本不会被调用**。把 HEAD 的调用重新按 `hasIssuedIndices` 加条件后,`TestRuntimeRecoveryApplySamePlanIsIdempotent` **通过**(在一次性克隆里验证) |
| 影响 | 恢复命令的**主场景**(恢复损坏的 Runtime)整个不可用。`tests/system/recovery` 12 个测试 **5 个失败**(父提交 0 失败) |
| 次生 | `internal/runtime/recovery.go:648` 新增了 `RecoveryConflictError`("current Runtime has later or different committed progress"),父提交无对应物 |
| 蓝图依据 | `tests/system/recovery/runtime_recovery_test.go:75-80` 自己写明是「RR-002, RR-004, RR-008, RR-010 的 RED 契约」 |

### H12 — S6 交付阶段的 `planned_paths` 范围校验被删除 `[M]`

| | |
|---|---|
| 位置 | `internal/assignment/completion_canonical.go:682-689`(HEAD)vs 父提交 `:700-717` |
| 现象 | 父提交有 `err = plancheckpoint.Validate(root, snapshot, agentID, planRef)`(以及 `plancheckpoint.ActivatedRef`),**HEAD 读了 plan 但从不校验它** |
| 后果 | **超出登记范围的 S6 Plan 会被接受** —— 即「扩张 TASK」 |
| 次生使能因素 | 旧的 legacy-Assignment 回退分支被移除:父提交 `internal/cli/integration_ack.go:131-134` → HEAD `:126-128` |
| 蓝图依据 | `L3-S6-build.md:188`「planned_paths **必须落在登记范围,不能扩张 TASK**」;`:226`「当前原生接收…**能拒绝范围**…」 |
| 为何没被发现 | 唯一能抓到它的系统层测试 `TestS4AuditOutputPathsCannotExpandReviewedScope`(父提交 `tests/system/req039/audit_dispatch_registration_test.go:152`)**在同一个提交里被删掉了** —— 删测试的同时删掉了唯一的守卫 |
| 严重度 | **HIGH** —— 蓝图明文禁止的行为,机器校验被删 |

### H13(MED) — 未知 schema 的 pending-commit 标记从「可隔离」变成「硬中止」 `[M]`

| | |
|---|---|
| 位置 | `internal/cli/recover_command.go`(apply)+ `internal/runtime/store.go:1803` |
| 现象 | `unsupported pending runtime commit schema ""` → 报「original pending Runtime operation is unresolved; preserve the existing files」,**没有任何隔离路径** |
| 父提交行为 | 父提交**故意**让无效标记「fall through so the approved recovery plan can quarantine it」 |
| 后果 | 一个陈旧标记会**永久阻塞**恢复,且用户无法自助脱困 |
| 严重度 | **MED** |

### H14 — 派发 Builder owner 时改写了已有 TASK 的状态事实 `[M]`

> **归因 agent 发现,父 agent 独立复核确认。** 这是「重构自己违反自己注释」的典型。

| | |
|---|---|
| 位置 | `internal/assignment/register.go:472-475` |
| 现象 | 新增了 `workstate.RecordTaskActivity(state, row, "in_progress", mutationEvent)` 调用 |
| 位置关系 | 这行**正好在**一段注释下面:`// ... Record the explicitly dispatched Builder owners in this same CAS; **keep all existing TASK facts and prior owners**.`(`:454-455`) |
| 后果 | `RecordTaskActivity` 会改写该 TASK 的 `state` / `last_lifecycle_event` —— **与它自己上面的注释直接矛盾**。父提交在这同一分支里**没有**这个调用(父提交的 `RecordTaskActivity` 只在 `:404` 新建分支和 `:1182` 出现) |
| 失败表现 | `binding a dispatched owner changed other TASK facts`(11 个失败实体,根因组 R06) |
| 判定 | **回归** —— 父提交同样有 11 个失败,但**失败原因不同**(父提交是 S6 基线/Report 问题),所以这 11 个是本轮引入的**新**失败 |
| 修法 | 移除 `:472-475` 的 `RecordTaskActivity` 调用,或改成不触碰 `state`/`last_lifecycle_event` 的记录方式 |

### H15 — S9 repair-workspace-candidate 的闭合 schema 拒绝整份历史记录 `[M]`

| | |
|---|---|
| 位置 | `internal/schema/assets/repair-workspace-candidate.schema.json:4`(`"additionalProperties": false`) |
| 现象 | 重构把 schema 的属性集换掉了(父提交 `required` 里有 `source_head`、`base_commit`;HEAD 换成了 `execution_generation`、`baseline_generation`),但 **`additionalProperties: false` 保留了下来** → **旧记录里的历史散列字段让整份记录被拒** |
| 蓝图依据 | **`L3-S9-repair.md:15` 明写「旧记录中已有散列值**只作为历史原字段读取**」** —— 这不是「可以拒绝」,是「必须能读」 |
| 判定 | **真缺陷,不是陈旧测试** —— 这是本轮审计里最明确的一条「schema 未随 hash-proof 删除同步更新」 |
| 关联 | 与我早前单独标记的 `stage-answer-s7.schema.json` / `verification_artifact_digest` 缺口是**同一类问题的两个实例**(S7 一个、S9 一个) |
| 修法 | schema 保留历史字段为**可选属性**(或加 `historical` 子 schema 承载),而不是靠 `additionalProperties: false` 一刀切 |

### H16 — Inspect 责任装载丢了 Worktree/Branch `[M]`

| | |
|---|---|
| 位置 | `internal/integration/inspect_responsibility.go:20-22` |
| 现象 | Inspect 出来的责任**缺 Worktree / Branch 信息** |
| 判定 | **回归**(父提交 PASS)—— 16 个失败实体(根因组 R04) |
| 修法 | 在责任装载时回填 Worktree/Branch |

### H17 — 同一事件的重复重试会重复写审计行 `[M]`

| | |
|---|---|
| 位置 | `internal/cli/hook_audit.go:13` |
| 现象 | 同一事件重试时**重复追加**审计记录 |
| 判定 | **回归**(父提交 PASS)—— 6 个失败实体(根因组 R08) |
| 修法 | 审计行按事件标识去重 |

### H18 — `s7_status.go` 的 review-plan 路径计算为空 `[M]`

| | |
|---|---|
| 位置 | `internal/cli/s7_status.go:425` |
| 现象 | review-plan 的路径算出来是**空字符串** → 下游 `runtime review-plan` 调用直接失败 |
| 判定 | **回归** —— 14 个失败实体(根因组 R10) |
| 关联 | 与 R02(4 个 CLI 子命令被删)是两回事:R10 是**路径算错**,R02 是**入口被删** |

### 附:三个 MED 级 + 一条既有行为澄清

| # | 位置 | 问题 |
|---|---|---|
| M1 | `internal/cli/s9_impact_observer.go:20` + `internal/cli/run.go:2696` | `observeMainS9Impact` **0 测试引用**,却插在 `observeMainS9Brief`(`:2699`)与 Stop preflight 链(`:2783`+)之前并 `handled=true` 短路 —— 一个完全无防护的观察器可能吞掉 Main 的 Stop 事件。**父 agent 已复核:`internal/cli/s9_impact_observer.go` 是 `d0d948a` 新增的文件,父提交 `git grep` 命中 0 次** → 确实是本轮引入 |
| M2 | `internal/cli/hook_reader.go:211-219` + 新文件 `internal/cli/hook_reader_s5.go:39-46` | 新增的 `reader.SelectionIssue` 字段(父提交 0 命中)让 `hookReaderObservation` 在 `SelectionIssue != ""` 且事件不是 `SessionStart`/`SubagentStart` 时返回 `true, 0` → **事件在 `runControlCycleForHook`(`internal/cli/run.go:3425`)渲染图诊断(`:3446`,`LOOP_RUNTIME_INVALID`)之前就被吞掉**。新文件 `hook_reader_s5.go` 会设 `reader.Kind="Worker"` + `reader.SelectionIssue`,把 S5 不可选中的情形接进这条短路 |
| M3 | `internal/cli/native_s6.go` | 「中断后合并结果未知」路径返回 1 并要求人工看 worktree,而 `L3-S6-build.md:352` 要求按原责任接续 |

> **【更正】我此前把 M2 写成「`internal/cli/run.go:2691`(`reader.Kind != "Worker"` → `return 0`)→ PreToolUse 在审计前提前返回」—— 这条描述是错的,父 agent 复核后撤回:**
> - `run.go:2691` 的那个 `reader.Kind != "Worker" → return 0` 位于 **`if renderHook && request.Event == "PreCompact"`** 分支内(`:2673`),**是 PreCompact,不是 PreToolUse**;父提交同一分支(`:3124-3126`)**逐字相同** → **既有行为,不是本轮回归**。
> - `hookReaderObservation` 里的 `if reader.Kind != "Worker" { return true, 0 }` 也**在父提交就有**(父提交 `hook_reader.go:203-205`)→ 同样是既有行为。
> - **本轮真正新增的只有 `reader.SelectionIssue` 这条新分支和 `hook_reader_s5.go` 这个新文件** —— M2 已按上面改写。
>
> 顺带一提:`internal/cli/run.go:2961` 的 `!graphObserved` 跳过 quality_gate 投影,在父提交 `run.go:3197` 有**同样结构** → 归因 agent 判定根因组 R01(32 个失败,最大组)**是既有失败**,这条也一并纠正了。

### 3.10 【新】父 agent 对关键缺陷的独立复核

不依赖 agent 转述,我在 `d0d948a` 与父提交 `9bf73ca` 上直接对比源码:

**H2(S8 人工审批门被删)** —— 用 `grep` 数出现次数:

```
HEAD  : grep -rn "validateS8ApprovalMode" internal/
  internal/investigation/hypothesis_result_receipt.go:257:func validateS8ApprovalMode(...) error {
  → 1 处 = 只有定义,0 个调用者

父提交: git grep -n "validateS8ApprovalMode" 9bf73ca -- internal/
  9bf73ca:internal/investigation/hypothesis_result_receipt.go:127:  if err := validateS8ApprovalMode(root, snapshot.State, request.OwnerAgentID); err != nil {
  9bf73ca:internal/investigation/hypothesis_result_receipt.go:189:  if err := validateS8ApprovalMode(lockedRoot, state, request.OwnerAgentID); err != nil {
  9bf73ca:internal/investigation/hypothesis_result_receipt.go:244:func validateS8ApprovalMode(...) error {
  → 3 处 = 2 个调用者 + 定义
```

**结论坐实:两处调用是本轮重构删掉的。** S8 Investigator 的审批权威门在 `d0d948a` 上**没有任何路径会执行**。

**H7(空切片变 nil)** —— `internal/cli/assignment_context.go:339`:

```go
copyOf.WorkerChecks = append([]assignmentrecord.WorkerCheck(nil), in.ClosingContract.WorkerChecks...)
```

`WorkerChecks` 为空时,`append(nil, ...)` 的结果是 `nil` → `encoding/json` 序列化成 `null`。
而 `internal/schema/assets/native-assignment-context.schema.json:563-573` 明确要求:

```json
"worker_checks": { "type": "array", "uniqueItems": true, "items": {"$ref": "#/$defs/worker_check"} }
```

且它在 `"required"` 列表里(`:563`)。→ **`null` 违反 `"type": "array"`,S6 上下文不可交付。** 修法一行:`append([]assignmentrecord.WorkerCheck{}, ...)`(或 `slices.Clone`)。

---

## 4. 契约漂移:文档说的和代码做的对不上

### 4.1 `forbidden_events`:**本次重构删掉了唯一的运行时消费者** `[M]`

这是本次审计**推翻了前几轮结论**的一条。

| | 父提交 `9bf73ca` | 当前 `d0d948a` |
|---|---|---|
| `enforceForbiddenEvents()` | 存在于 `internal/transition/engine.go:448-490`,在 Apply 时被调用(`:176`),fail-closed,并在错误里点名事件 | **被本次重构删除**(`git diff` 全是 `-` 行,无 `+`) |
| 证明测试 | 3 个(`TestForbiddenEventsBlocksACCWithoutCleanRound` `:99`、`TestForbiddenEventsBlocksReleaseAuditWithoutACC` `:143`、`TestForbiddenEventBarrierPassesWithValidCleanRound` `:167`) | **3 个全被删除** |
| `run_release_audit_without_acc` | **ENFORCED-AND-TESTED** | **NO-GUARD** |
| 其余 7 个事件 | DECLARED-ONLY(父提交同样如此) | DECLARED-ONLY |

**结论:这不是"既有缺陷",是这轮重构把一个能工作的屏障变成了装饰性表格。**
当前 `internal/transition/engine.go` 中 `forbidden`/`Forbidden` 出现 **0 次**。
`internal/transition/conformance_test.go:247-249` 只断言 `fe.Enforcement != ""` —— 一个叫 `enforcement`、值是字符串 `strong_block`、但**没有任何代码读它**的字段,是最坏的一种状态。

**最小诚实化修复**(来自契约 agent):
1. 在 `internal/transition/guards.go` 新增 `ForbiddenEventGuard(name) (GuardFn, bool)` 注册表,每个事件一个真实函数体;
2. 把 `conformance_test.go:247` 从「字符串非空」改成「必须能查到守卫」,查不到就 `t.Fatalf`;
3. 把 `enforceForbiddenEvents` 加回 `engine.go`(插入点 = 父提交 `:176`,**在构造 mutation 之前**,拒绝时不写 journal 行);
4. 恢复那 3 个证明测试,并按**当前**的 S10 载体(current review-plan identity)重写;
5. 更省事的替代方案:把 `enforcement: "strong_block"` 字段从 8 个条目里删掉,显式标注为 `declared_only` —— 让 JSON 不再承诺引擎从不做出的保证。

### 4.2 陈旧的不变量与状态描述

| 位置 | 现状 | 应改为 | 严重度 |
|---|---|---|---|
| `docs/control/loop-definition.json` INV-013 | 「ACC and release audit require a valid clean round.」 | 守卫 `clean_round_still_valid` 已从 TR-015/TR-017 移除,clean-round 要求现在**只在 TR-009** | **STALE** —— 而且 `internal/transition/guards_test.go:90-109` **明确断言了相反的事**(TR-015/TR-017 必须**不**带该守卫) |
| `docs/control/loop-definition.json` INV-017 | 「59 guards / 48 actions … 81 transition declarations … 58 unique guards / 33 unique actions」 | 实测:**59 guards ✅ / 42 actions ❌ / 98 declarations ❌ / 54 unique guards ❌ / 28 unique actions ❌**。5 个已注册但未声明的守卫、14 个已注册但未声明的 action | **STALE(5 个数字错 4 个)** —— 数字既不匹配父提交也不匹配当前,描述的是重构中途的某个状态 |
| `docs/control/loop-definition.json` `states.acceptance.description` | 「ACC is assembled from the locked baseline and **one valid clean round**.」 | 它自己的 `entry_condition` 已经改对了 → **内部自相矛盾** | STALE |
| `docs/guides/engineering-loop.md:625` | 「Forbidden outcomes」里仍列着 `create_acc_without_clean_round` | 该事件**已被本轮重构从 JSON 里删除** | STALE |
| `docs/guides/engineering-loop.md:640` | 「acceptance or release preparation before a clean round」列为 warn-and-self-correct | ACC/release 前置条件已不存在 | STALE |
| `docs/guides/engineering-loop.md:501`、`:514-526`、`:563` | 「a clean round requires all three to PASS in the same review round」「AND all blocking BUGs are closed AND no referenced evidence is invalidated」 | 实际闸门是 `internal/review/completion.go:7-66`:适用责任全部完成且被消费、verdict 为 `pass`、且无 Finding。**「所有阻塞 BUG 已关闭」「无失效证据」两个条件在 S7 出口已不再计算** | STALE |
| `blueprint/l3-stages/L3-S0-requirement-design.md:450` | 「当前实现**仍登记所读内容指纹**…**相关接线尚需落地**」 | 实测**已经落地**:重构删除了 `crypto/sha256` 导入、`req.SHA256` 的全部登记、`sha256Hex` helper、暂停检查点的文档重哈希 | STALE |
| `internal/transition/guards.go:343` | 注释仍称「must still match the file」 | 实现已不比对 | STALE |
| `internal/cli/projection.go:45` | 仍向用户显示 "current clean round" | 该机器证明已被移除 | STALE |

### 4.3 未变更但需注意的

- `INV-012`("clean round … **without a Finding**")—— **措辞准确且两处独立强制**(`guards.go:281-286` + `qualitygate/review_round_gates.go:12-19`),「without a Finding」半边也确实被强制(`guards.go:282` 的 `len(review.RoundFindings(state)) > 0` → 拒绝)。
- `INV-009` —— 措辞已正确迁移(从 sealed ObservationBatch → accepted Finding)。
- `INV-014`/`INV-015` —— S11 人工网关**端到端健全**:`human_release.go` 与 `guards.go` 都 import `internal/acceptance` 且无循环依赖;`latestReleaseAuditID`(`human_release.go:95`)把决定绑定到**当前** release-audit 身份,`engine.go:281-283` 拒绝过期 audit 的决定。
- `forbidden_events` 的 `reason` / `enforcement` 字符串**没有被改写** —— 8 条全部与父提交**逐字节相同**。前一轮"重构改写了 reason 字符串"的判断是错的。

---

## 5. 陈旧测试:可删 / 可改写

### 5.1 可整体删除的测试文件:**没有**

各 stage agent 的一致结论:**没有任何测试文件「只断言被删除的行为」而可整文件删除**。
陈旧断言集中在**单个测试函数**上,需要**改写**而非删文件。

> 唯一的例外方向:`internal/cli/s7_budget_questionnaire_test.go` 里的 `TestS7BudgetLegacyNumbersDoNotPrepareHumanApplications`(`:51`)是删除 S7 预算的**直接守卫**,但它**不在 CI 正则里**,只在 release lane 跑 —— 这是要**加入**不是删除。

### 5.2 陈旧测试改写清单(按 stage)

| Stage | 文件:行 | 测试 | 原先断言什么 | 现在该断言什么 |
|---|---|---|---|---|
| S0–S2 | `design_answer_e2e_test.go:268` | `TestDesignAnswerOriginalPairRecovery` | `journal.last_event_id` 与 `revision` 逐字节相等 | 排除 `revision`/`journal` 后比较,并断言 `revision` 递增(范式见已更新的 `contract_answer_e2e_test.go:213-228`) |
| S0–S2 | `planning_rework_e2e_test.go:114`、`:216` | `TestPlanningReworkCLIRejectsInvalidRequests` / `...RejectsFrozenDesignDrift` | digest 比对拒绝 REQ/设计变更 | 版本/坐标类不变量,或按 §6 补新行为测试 |
| S0–S2 | `bind_rollover_test.go:19` | `TestREQBindArchivesControllerTouchedInactiveRuntimeAndStartsAtRevisionZero` | 回执含 `source_journal_sha256` | 删除该断言,保留归档 journal 完整性(`bytes.Equal`) |
| S3–S4 | `internal/cli/planning_rework_e2e_test.go` 同上 | 同上 | 同上 | 同上 |
| S5–S6 | `s6_prepare_e2e_test.go:25,126,240` | 3 个 | `-agent-id` / `--platform-team-id` / `--revision-intent-key` | 从 args 删除这三个已删 flag(owner 改由 `s6_prepare.go:115-122` 推导) |
| S5–S6 | `native_s6_task_source_test.go:22-104` | 2 个 | 依赖已删的 `source.json` | 改读现存 brief/manifest 坐标 |
| S5–S6 | `native_s6_source_resume_test.go:20-40, 95-110` | 2 个 | 断言已删的 `nativeS6SourceAlignment` 文案 / 依赖 Git OID 比较 | 拒绝依赖 OID 是蓝图要求 → **按蓝图删除该保证**,但负向半边要留 |
| S5–S6 | `frozen_task_analysis_e2e_test.go:60-80` | 1 个 | 靠摘要识别损坏 | 蓝图说机器不证明材料未被外部修改 → 按蓝图改写 |
| S5–S6 | `s5_manifest_questionnaire_e2e_test.go:146-175, 220-240, 325-345` | 3 个 | 清单 SHA 钉 / 源漂移拒绝 | 改为"无证明仍可接收"的正向断言 |
| S5–S6 | `task_conventions_e2e_test.go:77` | 1 个 | 断言 `uncommitted inputs must be resolved` | 文案改为 `uncommitted tracked inputs must be resolved`(行为仍正确) |
| S5–S6 | `workgroup_help_test.go:54` | 1 个 | help 含 `runtime workspace bind --root` | 改为断言现行 `s6 prepare` flag 集 |
| S5–S6 | `revision_boundary_e2e_test.go:40` | 1 个 | 依赖原源快照指纹 | 按蓝图改写 |
| S7 | `s7_handoff_test.go:130` | `TestS7OriginalExitRefusesMissingCleanRoundEvidence` | S7 出口需要 CleanRound 机器记录 | 该机器证明已不存在 → 改为 `internal/review/completion.go` 的责任完成语义 |
| S7 | `s7_lifecycle_listener_test.go:96-99` | deny 断言块 | StopIdle 拒绝未完成责任 | 改为断言 notice(deny→notice 是本轮有意变更) |
| S7 | `work_progress_s7_actor_test.go:38-52` | 4/5 模式 | `projectWorkProgress` 缺目录/坏上下文时返回错误 | 重写为"unknown native actor ⇒ 静默",保留该模式 |
| S8–S9 | `case_workflow_test.go:101,120,186` | 3 个 | `evidence_refs` 非空 / hash 漂移 / follow-up `assignment_id` | 按 `L3-S8:307` 改写;schema 仍强制 `minItems` 的部分改为断言 schema 错误 |
| S8–S9 | `current_results_test.go:90,108,151` | 3 个 | 逐问题/逐 Finding 闭合门 | 按 `L3-S8:269`/`:307` 改写 |
| S8–S9 | `human_case_test.go:131,286` | 3 个 | changed-source / changed-baseline 拒收 | 按 `L3-S8:83`/`:377` 改写 |
| S8–S9 | `controller_test.go:1816` | `TestS9RepairNextActionKeepsNativeCandidateWithMain` | 断言一句已删的引导文案 | 按 `L3-S9:253` 改写 |
| S10–S11 | `internal/transition/product_baseline_test.go` | — | **已被正确更新**(父提交的 `clean_round` fixture 已替换为责任形态) | 无需动作 |

### 5.3 陈旧 fixture 字段

| 文件:行 | 已删字段/路径 | 改法 |
|---|---|---|
| `internal/investigation/receipt_machine_metadata_external_test.go:56` | `manifest_sha256`/`packet_sha256`/`task_sha256`/`answer_sha256`/`assignment_sha256` | 从合成的 `hypothesis_results[0].receipt` 删掉这 5 个键 |
| `internal/investigation/hypothesis_result_cas_test.go:~30` | journal event id `evt-s8-unrelated-observation` 已存在 | 每子测试用唯一 id(如 `fmt.Sprintf("evt-s8-unrelated-%d", time.Now().UnixNano())`) |
| `internal/cli/questionnaire_observer_s7_test.go:28-30` | 缺 `binding.review_plan_id` 与 `review.round_entry.plan_id` | 补上;`questionnaire_observer.go:472-477` 比对身份 |
| `internal/cli/questionnaire_observer_e2e_test.go:186` | 旧路径 `…/g1/S7/r1/review-plan.json` | 新路径 `…/g1/S7/<planID>/review-plan.json`(`internal/review/plan.go:PlanQuestionnairePath`) |
| `internal/cli/s7_manifest_draft_test.go:234` | 旧 manifest 路径 | 新身份目录 `.claude/workgroups/plans/<b64(planID)>/assignments/<id>/manifest.json` |
| `internal/cli/review_plan_draft_baseline_e2e_test.go:88-90`、`s7_native_handoff_test.go:148` | `review.round_entry.plan_id` 缺失 ⇒ `PlanQuestionnairePath` 返回 `""` | 先 stamp round entry |
| `internal/cli/s7_late_original_test.go:61` | 装下一个 plan 时未更新 `review.round_entry.plan_id` | 仿 `internal/repair/runtime.go:1141` |
| 7 个文件 | `runtime review-plan` 命令已删除 | 改用问卷式 plan 接收(`internal/cli/review_plan_pre_listener.go`) |
| `internal/cli/simulation_recovery_test.go:48` | `runtime review-plan` 恢复帮助 | 改为 `runtime review-continue --help` |
| `internal/cli/s7_planning_materials_e2e_test.go:48` | `-revision-intent-key` 已删 | 改写该步骤 |
| `internal/cli/s6_record_context_e2e_test.go:470-485` | schema 不再含 `sha256` 机器字段 | 负向样例改用仍在 schema 内的字段 |

### 5.4 【新】接缝层的陈旧 fixture 与已被重构删掉的测试文件

#### (a) 接缝层陈旧 fixture(需改写,不能删)

| 文件:行 | 缺失/已删字段 | fixture 改法 |
|---|---|---|
| `tests/system/req039/task_integrate_test.go:21-48`(`seedIntegrableAssignment`) | `assignment-ti` 没有规范的 `assignmentrecord.Record` → `internal/cli/integration_ack.go:126` 拒绝 | 造一个规范的 Assignment Record(`Record` + `AssignmentRecordSHA256` + `AssignmentRevision`),或迁到 `internal/assignment` opt-in |
| `tests/system/req039/audit_sharedmodel_integration_test.go`、`ct_integration_resume_test.go`、`final_audit_delivery_test.go`、`worktree_governance_test.go`、`audit_dispatch_result_binding_test.go` | 同上,legacy-Assignment 形态 | 同一处修,或抽一个共享的 `req039fixtures.SeedCanonicalAssignment` |
| `tests/system/req039/s11_human_decision_branches_test.go:19,52,105,281,321` + `s11_human_decision_test.go:30` | 当前 baseline/round 缺 `release_audit` 证据行 | 播一行 `release_audit` 证据(守卫 `internal/transition/guards.go:290+` → `acceptance.ValidateCurrentS10Evidence`) |
| `tests/system/req039/ct_l4_precondition_test.go:82`、`s2_to_s11_hook_driven_test.go` | 缺 `builder_responsibility:TASK-039-01/02` 引导行 | 在 TR-006 PreToolUse 之前登记 S6 builder 责任行 |

> 这些 fixture 失败是 **STALE-FIXTURE**,不是产品缺陷 —— 但**掩盖**了 H12 这类真缺陷。修 fixture 时要意识到这一点。

#### (b) 本轮重构删掉的 `internal/integration` 文件(32 个测试,无替代断言)

| 被删文件 | 原来守的能力 | 现在由谁覆盖 | 判定 |
|---|---|---|---|
| `canonical_predecessor.go`(+ 2 个测试文件) | 规范前驱执行代次(来自原工作区历史) | **无** —— `CanonicalPredecessor` / `CanonicalPredecessorFor` 在 HEAD **零命中** | **静默接缝漏洞(HIGH)** |
| `result_binding.go`(+ `result_binding_test.go`) | 把规范 Result 绑定到 Integrate 消费的 Main facts | `internal/workpacket` 绑定 + `internal/assignment/completion_canonical.go:626-709` | **接缝处能力丢失** —— 这正是 H12 的根因 |
| `check_receipt.go` | 每检查的机器回执绑定到集成检查点 | `internal/mainreceipt/reader.go:90-91` 只读投影 | **部分** —— 存储还在但**是空的** |
| `cleanup_ignored.go` | 忽略但未跟踪的工作必须不被静默清理 | HEAD `cleanup_ignored_test.go:11`(1 个)+ `cleanup_nohash_test.go` | **覆盖从 3 缩到 1** |
| `worker_private_area.go`(+ 1 个测试文件) | 观察 worker 私有区域(无跨边界写) | `inspect_responsibility.go` / `integrate_responsibility.go` / `main_private_files_external_test.go` | **部分** —— 改名了,3 条专属断言没了 |
| `native_delivery.go` | native-delivery 契约接线 | `internal/submission/repair_result.go:121`、`repair_result_resolution.go:139` | **别处已覆盖** |
| 另 9 个测试文件 | `audit_scratch_projection`、`canonical_dirty_pending`、`canonical_revision_external`、`canonical_surface_failed_revision_external`、`captured_log_projection`、`cleanup_recovery`、`frozen_paths`、`git_worktree_projection`、`metrics_temporary`、`ready_merge_history{,_external}`、`retry_real` | — | **32 个顶层测试被移除(115→83),零替代断言** |

#### (c) 接缝 agent 判定可安全删除/改写的陈旧测试

| 文件:行 | 测试 | 为什么过时 | 原来断言什么 | 现在覆盖情况 |
|---|---|---|---|---|
| `tests/system/req039/audit_authority_e2e_test.go:92` | `TestRuntimeAuthorityProtectsRecoveryAndStorageCoordinates`(5 个场景) | `runtime fingerprint` 命令已删 | 复制出来的 Runtime 的权威拒绝 | **无** → 见 §6.5 第 4 条 |
| `tests/system/req039/audit_sharedmodel_spine_test.go:280` | `TestAuditSharedModelHookSpineCommittedSource` | content-hash 证明被删 | 共享模型内容哈希 | n/a(不是蓝图保证) |
| `tests/system/req039/audit_sources_e2e_test.go:112` | `TestAuditStageSourcesUseOriginalConsumerBoundaryE2E` | SHA 被删 | `S3 frozen input differs from original checkout SHA` | n/a |
| `tests/system/recovery/runtime_recovery_test.go:68` | `TestRuntimeRecoveryInspectAndPlanSurviveMalformedOrBOMState` | plan 内容寻址被删 | plan 保留损坏字节 | `internal/cli/recovery_checkpoint.go:66-90`(只保留大小) |
| `tests/system/recovery/runtime_recovery_test.go:123` | `…RebindsREQAndQuarantinesCorruptRuntime`(digest 那一半) | `bound_req.sha256` 被删 | **按 digest** 重绑 | 按 id/path/version 重绑(`internal/cli/recovery_checkpoint.go:36-38`) |
| `internal/integration/s7_issued_result_external_test.go:433` | 两个 S7 issued-result 测试(stderr 那一段) | 不再重新播报 | stderr 含 `original consumer` | 持久回执在 `:428` —— **但 H10 的 TR-009 晋级是另外坏掉的** |

---

## 6. 覆盖缺口(UNENFORCED-NEW):重构放宽了,但没有任何测试断言新的宽松行为

这是"删减与适配"里**最重要的新增工作** —— 重构删掉了大量检查,但**几乎没有任何测试断言"现在必须允许这些情况"**。不补这些,下一次有人把检查加回来也不知道。

| # | 被放宽的行为 | 蓝图依据 | 为什么现在没人抓 | 建议测试 |
|---|---|---|---|---|
| U1 | `RegisterHypothesis`/`SubmitHypothesisResult` 接受空 `evidence_refs`/`method`/`observed`/`counterfactual` | `L3-S8:241`、`:307` | schema `minItems` 挡了 `evidence_refs`,但 `method`/`observed`/`counterfactual` 无人断言宽松 | `TestRegisterHypothesisAcceptsMinimalProfessionalFields` @ `internal/investigation` |
| U2 | `ApproveContract` 不再校验因果闭合证据与合同基线完整性 | `L3-S8:377` | 无测试断言"空 `causal_model` 必须被拒" | `TestApproveContractRequiresCausalModelAndRootCause` @ `internal/investigation` |
| U3 | `requireRepairReadyCase` 不再要求 `route == "s9_repair"` / 非空 `route_reason` / 空 `unexplained_finding_ids` | `L3-S8:307` | 现有测试断言的是**相反** | `TestApproveContractAcceptsNonRepairRouteWithoutUnexplainedSet` @ `internal/investigation` |
| U4 | `validateS8ApprovalMode` 不再被调用 → `plan_approval_required` Investigator 可无审批提交 | `L4:113` | 见 H2 | 先修 H2,再补 `TestApprovalModeInvestigatorStillRequiresWorkingAuthorization` |
| U5 | hash/SHA 证明从证据/回执/合同/审批中删除 | `L3-S8:377`、`L3-S9:382` | 只有负向测试红;**没有**正向测试断言"无证明的输入仍被接受" | `TestUnattestedEvidenceStillReceivesProfessionalResult` @ `internal/investigation` |
| U6 | `readback_request` 不再是合法的 agent-message 类型 | `internal/schema/validator.go:227` | schema 会拒,但**无测试**断言它被拒 | `TestAgentMessageRejectsRemovedReadbackRequest` @ `internal/schema` |
| U7 | S7 答案体仍容忍旧机器键 `verification_artifact_digest` | `stage-answer-s7.schema.json:133` | 实测容忍;无测试断言 | `TestS7AnswerBodyToleratesLegacyMachineKey` @ `internal/schema` |
| U8 | `internal/audit` 决策索引改用「日志大小 + 解析路径」而非整份 SHA-256 | `internal/audit/decision_index.go:31` | 现有测试覆盖大小变化与路径变化,**没覆盖同路径同大小就地改写** | `TestDecisionIndexAcceptsSameSizeInPlaceRewrite` @ `internal/audit` |
| U9 | `WithReviewResultReadCache` 变成空操作 | `internal/runtime/review_result_read_cache.go:13-18` | 3 个外部调用者仍在调它;无测试断言"不发生缓存" | `TestReviewResultReadIsNeverCached` @ `internal/runtime` |
| U10 | S2 `no_transition` 语义被收窄为仅 `pending`,`validNoTransitionPlanningStamp` 被删 | `S2:47`、`:572` | 5 处调用点仍按 `no_transition` 分支,但**无任何测试覆盖 S2 的 `no_transition`** | `TestS2NoTransitionReportIsNotWorkComplete` @ `internal/cli` |
| U11 | rework 路由不再检测 REQ 变更 | `loop-definition.json:202`(PTR-PLAN-03)、`S1:297`/`:341`、`S8:408` | 守卫退化为只检查 `bound_req.path` 非空;无测试断言新宽松行为 | `TestPlanningReworkRejectsChangedReqVersionRoutesToAmendment` @ `internal/cli` |
| U12 | `req workspace` 不再比对 REQ 内容 | `S1:347`「若需求内容也变了,必须回到 amendment」 | `internal/cli/worktree.go` 已无 digest 检查 | `TestReqWorkspaceRoutesChangedRequirementToAmendment` @ `internal/cli` |
| U13 | S5 准备后不再钉清单/材料摘要,源漂移不再拒收 | `L3-S5:84`、`:140`、`:402` | 旧断言全失效,替换后的正向语义无人覆盖 | `TestS5PreparationRecoveryAcceptsSourceDriftWithoutAttestation` @ `internal/cli` |
| U14 | 源提交 OID 不再登记/核对 | `L3-S6:135`/`:163` | 同上 | `TestNativeS6ResumeAcceptsWithoutSourceAttestation` @ `internal/cli` |
| U15 | `cleanup_pending` 不撤销接收/检查/ACK,也不阻挡 TR-006 | `L4-workflow-convergence:435` | 现有测试只覆盖"未知文件"路径,**没覆盖"已修改的已跟踪产品文件"** | `TestS6CleanupPendingKeepsModifiedTrackedProductFile` @ `internal/cli` |
| U16 | PreToolUse 可静默退出 0 | `L4-hook-platform-wiring:172` | 无正向用例断言"空 stdout + 0 是合法协议结果" | `TestHookPreToolUseSilentSuccessIsProtocolValid` @ `internal/cli` |
| U17 | S7 答案体 `site_lost` 仍是 schema 必填,但重构意图是"同答卷里解释" | `L3-S7:242`、`:338` | 唯一用 `capture_gaps` 的测试仍传 `site_lost: []` | `TestS7AnswerAcceptsP0WithoutSiteLostField` @ `internal/review` |
| U18 | `CompareIdentity` 对缺失 PlanID 返回 `Unknown`,观察器因此**静默丢弃**答案 | `L3-S7:219` | 无测试构造空 `review_plan_id` 的 S7 包 | `TestS7ObserverKeepsAnswerWithUnknownPlanIdentity` @ `internal/cli` |
| U19 | 旧 review 预算键为可选兼容数据、无默认值 | `internal/schema/legacy_budget_compat_test.go:40` | ✅ **已覆盖** | — |

### 6.1 零测试引用的新生产面(完全无防护)

| 符号 | 位置 | 风险 |
|---|---|---|
| `observeMainS9Impact` | `internal/cli/s9_impact_observer.go:20` | **HIGH** —— 短路 `run.go:2696` 之前的整条链,0 引用 |
| `observeMainS9Brief` | `internal/cli/native_s9_brief_observer.go` | MED |
| `nativeS9RoutingAvailable` | `internal/cli/native_s9_routing.go` | MED |
| `receiveNativeS9OriginalBuilder` | `internal/cli/native_s9_original.go:21` | MED —— 仅被生产代码调用(间接覆盖),而该间接路径当前正是坏的 |
| `internal/cli/native_s9_pin_prepare.go` 全部 | — | MED |
| `internal/acceptance/review_identity.go` 全部 | `CurrentReviewIdentity`/`CurrentReviewArtifact`/`StampReviewIdentity` | MED —— 包里有 4 个测试文件却**没有它的**;被 `internal/transition/human_release.go:159` 调用 |
| `nativeS8Responsibility` 的 11 个函数中 9 个 | `internal/cli/native_s8_responsibility.go` | LOW —— 只有 2 个有覆盖 |

### 6.2 零测试包

| 包 | 内容 | 风险 | 判定 |
|---|---|---|---|
| `cmd/loop-harness` | 15 行 CLI 入口,行为全在 `internal/cli` | 无 | **可接受** |
| `internal/runtimemeta` | 8 行,一个 Snapshot DTO | 无 | **可接受** |
| `internal/builderbatch` | 46 行,一个纯函数 `HasDispatchPlan`;重构从 `gate.go` 删了 **714 行** | LOW | **真实但小的缺口** —— 纯函数、12 行逻辑,零断言 |
| `internal/processtree` | `process_unix.go`/`process_windows.go`:进程**组** `Setpgid` + `SIGKILL`、`cmd.WaitDelay`、退出时杀子孙 | **MED** | **真实缺口** —— 进程组 kill 语义正是 `internal/integration` 间接覆盖的东西 |

### 6.3 10 个死历史资产

`internal/schema/assets/` 下 **102 个文件在磁盘上,但只有 90 个被 `go:embed` 嵌入**(glob 排除了 `-before-`/`-legacy`)。
**12 个未嵌入**,其中 10 个是历史模板,**零消费者**(本轮重构删掉了 `internal/workpacket/repair_template_history_test.go`,并收窄了 embed glob):

- `stage-form-s7-template-before-{answer-shape,boundary-guidance,natural-observation,normal-authoring,s7-s10-guidance,safety-stop}.json`
- `stage-form-s9-{plan,result}-template-before-natural-observation.json`
- **`stage-form-s7-template-before-convergence.json`** —— **本轮重构自己新增的 49 行文件**,是重构前 S7 模板的逐字节副本(仍含已删的 `verification_artifact_digest` 机器题),**未嵌入、未被任何代码/文档/测试引用**。重构删掉了兼容机制,却新增了一份死的兼容快照。

**同时**:重构**删掉了**「历史模板能重建其原始发行」这一能力的证明测试(`repair_template_history_test.go` 146 行 + `review_result_test.go` 的 `TestHistoricalReviewResultTemplateOnlyRestoresKnownFrozenQuestions`),而 10 个模板文件还留着 —— 未来的编辑者会以为它们是活的。

> **同时被标记为 HIGH 的 schema 缺口**:重构前的 S7 答案文件(**必然**带 `questions.verification_artifact_digest`)现在会被闭合 schema 拒绝(实测:`at '/questions': additional properties 'verification_artifact_digest' not allowed`)。而 `L4-workflow-convergence.md:104` 要求「已发行的 S7、S8、S9 原责任在阶段变化后真实晚到或本人自然继续,**仍沿原关联接收**」——**重构前保存答案、之后晚到的专业者,其答案将永久无法被接收**。这条与 `:118`「不保留旧版本 schema、兼容解码」存在真实的蓝图张力,需要 owner 裁定。

### 6.4 【新】12 项跨 stage 接缝检查矩阵

接缝 agent 对 12 个「跨阶段交接点」逐条判定。这是本轮审计里**最贴近「重构是否把系统接坏了」**的一节。

| # | 接缝 | 判定 | 证据 | 缺口 |
|---|---|---|---|---|
| 1 | S6→S7(TR-006 Builder 完成) | **BROKEN(既有)** | `tests/system/req039/ct_l4_precondition_test.go:105`;`s2_to_s11_hook_driven_test.go:60` | `builder_responsibility:*` 从不落地;`GATE-BUILDER-BATCH-READY` 一直是 `not_ready` |
| 2 | S6 范围强制 | **BROKEN(产品缺陷=H12)** | 父提交 `internal/assignment/completion_canonical.go:711` 被删;`internal/plancheckpoint/validate.go:227` 变成不可达 | H12 |
| 3 | S7 接收(Reviewer 答案 → S7) | **BROKEN(产品缺陷=H10)** | `internal/cli/native_review_hooks.go:358-372` | H10 |
| 4 | S7→S10 出口(`RequiredResponsibilitiesComplete` → TR-009) | **经 #3 破**;出口本身**在系统层被测到** | `internal/review/completion.go`;`internal/transition/guards.go:282`;由 `tests/system/req039/ct_verification_chain_test.go:22` 与 `s2_to_s11_hook_driven_test.go:15` 驱动;**`internal/integration` 层没有覆盖**(它唯一的 S7→S10 驱动是坏的) | 缺集成层的出口覆盖 |
| 5 | S7→S8(Finding → investigation) | **PARTIAL** | 父提交 `ct_bug_correction_test.go:14`(TR-008)被删;HEAD 的 `ct_bug_correction_test.go:12` 只播种一个既有的定向复查失败;`internal/cli/ct_11_24_test.go:135` 只断言「没有 deny」 | **没有系统层测试把真实 Finding 推进 S8** |
| 6 | S8→S9(investigation → repair) | **NOT COVERED** | `ct_rework_test.go` 只覆盖 TR-023/TR-024 | **没有任何测试驱动这一跳** |
| 7 | S9→S7 再验证(完整闭环) | **NOT COVERED** | 父提交 `TestCT03914_FindingToBugRepairFullReview`(TR-008→PTR-BUG-01→02→04→05→…)被删;`req039fixtures.SeedSealedObservationBatch` 被删 | **没有任何测试驱动完整 S7→S8→S9→S7 闭环**;每一跳都只在播种状态下孤立测过 |
| 8 | S9 修复后的后继发布 | **NOT COVERED** | 父提交 `recheck_delivery_e2e_test.go:22`、`branch_delivery_regression_test.go:18`、`recheck_repair_hooks_e2e_test.go:21`、`native_workspace_chain_test.go:25` **全部被删** | 整条 S9 交付/复查链**丢掉了 L4 层** |
| 9 | S9 Impact 观察(`observeMainS9Impact`) | **UNTESTED** | `internal/cli/s9_impact_observer.go:20`(零测试引用);短路 `internal/cli/run.go:2696` | M1 |
| 10 | Runtime 恢复(损坏 Runtime → apply) | **BROKEN(产品缺陷=H11/H13)** | `tests/system/recovery/runtime_recovery_test.go:153,367,478`;`internal/runtime/recovery.go:522-526` | H11 / H13 |
| 11 | Review identity(S7 plan_id → S10 acceptance/release_audit → S11 决定) | **OK —— 不是缺口** | `internal/cli/audit_identity_hook_e2e_test.go:14 TestUnnumberedS10AndS11ConsumeTheirOriginalSource`(HEAD 存在,断言 `review_plan_id` 传播 +「旧 PASS 不得成为当前」);新面 `internal/reviewmeta/identity.go:16-58`、`internal/acceptance/review_identity.go:7-21` | 无(静态判定;该包运行由并行的 `internal/cli` 审查员负责,已记录为通过) |
| 12 | Task-Integrate 接缝(Main 集成 worker 分支) | **BROKEN(陈旧 fixture,6 个测试)** | `internal/cli/integration_ack.go:126-128` vs 父提交 `:131-134` | legacy-Assignment fixture 需迁移(见 §5.4a) |

> **第 6、7、8 条是本次审计最重要的新发现之一** —— 这三条接缝(尤其是 **S9→S7 完整闭环**)现在**一个测试都没有**。而 `tests/fixtures/native-stage-simulation` 其实是给 **Python 驱动 `tools/claude-full-stage-simulation.py`** 用的,**不在 Go 测试套件里**。也就是说:**S7→S8→S9→S7 这个主闭环,目前只在 Go 套件之外被跑过。**

### 6.5 【新】被静默删除的连续性保证(比失败更危险)

失败会报警;**被静默删除的守卫不会**。接缝 agent 逐条比对了「保证 → 现在的强制 → 现在的测试」:

| 被删的保证 | 删在哪 | 现在的强制 | 现在的测试 | 判定 |
|---|---|---|---|---|
| 检查点/接收链一致性(**26 项检查**:journal 尾链、重复 packet/evidence/reception、冻结绑定、身份、领域证据、答案完整性、原始发行/journal/控制提交、转换绑定、取代/复活、旧审查失效) | `internal/runtime/checkpoint_indices.go` **271 → 17 行** | **无** | **无** —— 这 26 条错误串在 HEAD **和**父提交的**任何**测试文件里都**零出现** | **静默丢失** |
| Runtime 检查点契约(192 行) | `internal/runtime/contract_checkpoint.go` **整个删除** | 无 | 无 | **静默丢失** |
| S8 假设结果连续性(「一个未答包的来源在兄弟 Case 修订间保持不变」) | `internal/runtime/hypothesis_result_continuity.go` 删除 | **无** —— `VerifyHypothesisResultContinuity` 在 HEAD **零引用** | 无 | **静默丢失** |
| S7 审查结果控制路径校验(漂移扫描内) | `internal/runtime/review_result_control_paths.go` 删除(调用点是父提交 `internal/review/workspace.go:502`) | 无(整个漂移扫描被删) | 无(`internal/review/workspace*_test.go` **5 个文件、705 行**被删) | **丢失 —— 按 `L4-workflow-convergence.md:260` 是有意的,但现在无人断言** |
| 审查结果读缓存(memoization + 「缓存的 JSON 不得绕过结构检查」守卫) | `internal/runtime/review_result_read_cache.go` 209 → 76 行;`WithReviewResultReadCache` 变成空操作 | 结构检查总是执行 | `internal/runtime` 全绿 | **未丢失**(纯性能) |
| 跨阶段 Result → Main facts 绑定 | `internal/integration/result_binding.go` 删除 | `internal/workpacket` + `internal/assignment/completion_canonical.go:626-709` | `internal/integration/result_binding_test.go` 删除 | **接缝处丢失**(= H12 的根因) |
| 规范前驱执行代次 | `internal/integration/canonical_predecessor.go` 删除 | **无** | 无 | **静默的接缝漏洞(HIGH)** |
| Main 复查检查点 / 合并回执恢复 | `canonical_revision_external_test.go`、`ready_merge_history*_test.go` 删除 | 部分由 `internal/integration/target_recovery_regression_test.go` | 缩减 | **缩水** |

**建议**:第一条(26 项检查点一致性)是最值得立刻补测试的 —— 它保护的是**数据完整性链**,而且在父提交上也**同样没有测试**(这解释了为什么它能被无声删掉)。建议新增 `TestCheckpointIndexRejectsForgedReceptionChain` @ `internal/runtime`。

### 6.6 【新】CT-039 层级覆盖矩阵

接缝 agent 逐个 CT 编号比对了「进程内层」(`internal/cli`,`controller.RunControlCycle`)与「系统层」(`tests/system/req039`,真实 hook 路径):

- **只有进程内层覆盖**(系统层的完整版本被删):**CT-039-14** ⚠⚠(父提交 `ct_bug_correction_test.go:14 TestCT03914_FindingToBugRepairFullReview` 是**完整闭环**测试,HEAD 只剩一个弱化版)、**CT-039-04**、**CT-039-09** ⚠、**CT-039-20** ⚠
- **两边都红**:CT-039-12、CT-039-17
- **谁都不覆盖**:**CT-039-22** ⚠⚠
- **按设计只有进程内层**:CT-039-19
- 其余 17 个 CT 两层都有

另外 `tests/system/req039/helpers_test.go:8` 的文件头注释**仍声称** `ct_bug_correction_test.go` 包含 CT-039-14 和 CT-039-22 —— 实际它只剩 CT-039-23。**注释与现实不符,是接手者的陷阱。**

---

## 7. CI 车道适配

> 本节的覆盖数字由父 agent **独立复核**(直接解析 `Makefile` 的 `-run` 正则 + `go test -list` 的真实测试清单),不依赖任何 agent 的转述。

### 7.1 【新】`fmt-check` 是 `ci-verify` 的第一步 —— 它现在必挂

```
$ gofmt -l cmd internal tests
internal/cli/audit_review_required_test.go
internal/cli/s9_native_listener_guidance_test.go
internal/investigation/repair_plan_report_hook_test.go
```

父提交 `9bf73ca` 上是 **0 个文件** → **这 3 个文件是本轮重构引入的**。共 **7 处一行式 `if` 语句**(`gofmt` 要求展开成花括号块):

| 文件 | 处数 | 位置 |
|---|---|---|
| `internal/cli/audit_review_required_test.go` | 2 | `:268-278` |
| `internal/cli/s9_native_listener_guidance_test.go` | 1 | `:21-27` |
| `internal/investigation/repair_plan_report_hook_test.go` | 4 | `:127-145` |

> `Makefile:47` `fmt-check` 的判定是 `@test -z "$$(gofmt -l cmd internal tests)" || { gofmt -l cmd internal tests; exit 1; }`。
> `ci-verify: fmt-check vet test-ci test-ci-race doctor validate build` —— **`fmt-check` 是第一个 target**。
> **所以在 `d0d948a` 上,CI 会在跑任何一个测试之前就红。** 修复成本:`gofmt -w` 三个文件,零风险。

### 7.2 重构对 CI 配置的影响极小

`Makefile` 只改了 **2 行**(smoke 正则里两个测试名),`.github/workflows/` **完全未改**。
上版报告的所有结论依然成立:CI 自 2026-09-21 起未运行、`main` 37 天没动、仓库历史只有 1 个 PR、macOS 腿连续 2 次 901s 排队取消。

已被本轮重构部分适配的部分:`Makefile:70` 的 smoke 正则里
- `S2MissingQuestionnaireRestoresOriginalFilesOnMaterialChange` → `S2MissingQuestionnaireRestoresViewWithoutInventingPrivateAnswer`(实测存在,`internal/cli/design_guidance_recovery_test.go:79`)✅
- `S7BudgetNormalApplicationRecordsTwoActualHumanChoicesAndReadOnlyReplay` 被移除(实测源码里也已消失)✅

### 7.3 【新】三个核心包根本不在 `test-ci-core` 里 —— 覆盖率塌陷

`Makefile:59-64` 的 `test-ci-core` 用 sed 把三个包**整体剔除**:

```
sed -E '\@/internal/(cli|integration|investigation)$@d; \@/tests/system(/|$)@d'
```

实测(77 个包 → core 车道只跑 **72** 个,剔除 3 个核心包 + `tests/system/*`)。
于是 `internal/cli`、`internal/integration`、`internal/investigation` **只能靠 `test-ci-smoke` 的 52 / 21 / 16 个名字的 `-run` 正则**被覆盖。

**而且 `tests/system/*` 被整体剔除后,没有任何其它车道接管它。** 实测:

| 包 | 顶层测试数 | 任何 CI 车道是否跑 |
|---|---|---|
| `tests/system/req039` | **98** | ❌ 完全没有 |
| `tests/system/recovery` | **12** | ❌ 完全没有 |

`grep -n "tests/system" Makefile .github/workflows/*.yml` → **只有 `Makefile:62` 那一处 sed 排除**,smoke 与 race 车道都没提到它们。

这 110 个测试恰恰是**跨 stage、真实 Git、端到端**的验收场景(例如 `TestL4AuditRealGitClosedLoop`、`TestS3AuditFrozenModelCannotMergeFromWorker`、`TestAuditSharedModelHookSpineCommittedSource`、`TestRuntimeRecoveryApplyRebindsREQAndQuarantinesCorruptRuntime`)——**最该拦住这次重构那类接缝回归的测试,一条都不在 CI 里**。

用 `go test -list '.*'` 拿到真实顶层测试数,再用 Makefile 里的正则逐条比对:

| 包 | 顶层测试总数 | smoke 正则命中 | **任何 CI 车道都不跑** | 占比 |
|---|---|---|---|---|
| `internal/cli` | **1197** | 52 | **1145** | **95.7%** |
| `internal/integration` | 83 | 21 | **62** | **74.7%** |
| `internal/investigation` | 85 | 16 | **69** | **81.2%** |
| `tests/system/req039` | **98** | 0 | **98** | **100%** |
| `tests/system/recovery` | **12** | 0 | **12** | **100%** |

> `runtime-portability.yml:22` 额外跑 `go test ./internal/cli -run 'TestBuildInfoDoesNotNeedProject|TestInitDerivesHookMetadataAndCreatesJournals'` —— 这 2 个名字**已包含在 smoke 的 52 个里**,净增为 0。
> 也就是说:**`internal/cli` 的 1197 个测试里,CI 一次只跑 52 个(4.3%)。** 而这 52 个里面还有 4 个是**死条目**(见 §7.4),实际有效覆盖只有 48 个。

### 7.4 【新】smoke 正则里有 4 个死条目(实测 `go test -list` 确认)

| 包 | 死条目 | 后果 |
|---|---|---|
| `internal/cli` | `S3RegistrationPreservesAcceptedSourceClosure` | 该测试已不存在 → 正则永不命中 |
| `internal/cli` | `NativeS9CanceledPreparationPreservesOriginalSource` | 同上 |
| `internal/integration` | `CanonicalSurfaceEarlyInspectionFailureDoesNotBlockSuccessfulRevision` | 同上 |
| `internal/integration` | `IntegrationPreservesChangesWrittenByChecksAndResumesWithoutRemerge` | 同上 |

`Makefile` 那次修正只对了 1 个(`S2MissingQuestionnaireRestoresOriginalFilesOnMaterialChange`)。**这 4 个死条目不报错**(`go test -run` 对不存在的名字静默跳过),所以 CI 绿灯并不代表它们在跑 —— 它们只是**静默失效**。

另有 4 个通配条目(`NativeS5.*`、`NativeReviewResume.*`、`CheckpointStore.*`、`Integrate.*`、`ApproveContract.*`、`DelegatedApproval.*`)会随源码增删**自动漂移**:今天覆盖 12 个 `NativeS5`/`NativeReviewResume` 测试,明天重构删掉一个,CI 依然绿灯但少跑一个。

### 7.5 【新】触发器机制精确核实

`git ls-tree main -- .github/workflows` → **`main` 上只有 `ci.yml` 和 `release.yml`**;`runtime-portability.yml` **只存在于 feature 分支**。

GitHub Actions 的取文件规则:
- `pull_request` 事件 → 使用 **base 分支**上的 workflow 文件 → `main` 上没有 `runtime-portability.yml` → **该 workflow 对 PR 永远不触发** ✅(与上版结论一致)
- `push` 事件 → 使用 **被推送 ref** 上的文件 → 每次推 feature 分支都会触发 portability ✅

而 `ci.yml` 的触发器是 `pull_request: branches: [main, integration/loop-next]` + `push: branches: [main]` → **往 `codex/hook-handoff-wiring-20261025` 推代码,`ci.yml` 根本不会跑**。

> **净效果:本分支上,PR 检查套件(`make ci-verify`)从来没有被触发过。** 这是 §8 第 0 步要先修的第 1 件事。

### 7.6 仍然缺失的 CI 选择

`internal/cli/s7_budget_questionnaire_test.go:51` `TestS7BudgetLegacyNumbersDoNotPrepareHumanApplications` —— 删除 S7 预算的**直接守卫** —— **不在 CI 正则里**,只在 release lane 跑。

### 7.7 【新】`make test` 的 30 分钟预算跑不完 `internal/cli`

见 §9。`Makefile:5` `TEST_TIMEOUT ?= 30m`,`test`/`test-race`/`ci-verify-full` 都用它。实测整包在 **1800.097s** 处被 `panic: test timed out` 强杀 → **`make ci-verify-full` 在本轮重构后不可能跑完**。

---

## 8. 删减与适配执行顺序

### 第 0 步:让 CI 先能跑(不依赖任何测试改动)

**这 5 件事必须先做,否则后面所有修复都得不到验证。** 按此顺序:

| # | 动作 | 理由(全部实测) |
|---|---|---|
| 0.1 | **`gofmt -w` 那 3 个文件** | `gofmt -l cmd internal tests` 返回 3 个文件 → `fmt-check` 是 `ci-verify` 的第一步 → CI 在跑任何测试之前就死。**这一条不改,后面全是白做。** 文件:`internal/cli/audit_review_required_test.go`(2 处)、`internal/cli/s9_native_listener_guidance_test.go`(1 处)、`internal/investigation/repair_plan_report_hook_test.go`(4 处) |
| 0.2 | **`ci.yml` 的触发器覆盖 feature 分支** | `pull_request: branches: [main, integration/loop-next]` + `push: branches: [main]` → 往 `codex/hook-handoff-wiring-20261025` 推代码,`ci.yml` **根本不会跑**。CI 自 2026-09-21 起未运行(`main` 最后一次提交 `60c3440`) |
| 0.3 | **`runtime-portability.yml` 合并进 `main`** | `git ls-tree main -- .github/workflows` → `main` 上只有 `ci.yml` + `release.yml`。GitHub 的 `pull_request` 用 **base 分支**的 workflow 文件 → 该 workflow 对 PR **永远不可能触发**(而 `push` 用被推 ref 的文件 → 每次推 feature 分支都触发,行为不一致) |
| 0.4 | **`ci.yml` `timeout-minutes` 15 → 30** | 重构前实测热缓存 PR lane 就要 16.5 min;重构后 `internal/cli` 整包已超 30 分钟(§9.1),**15 分钟连想都不用想** |
| 0.5 | **`TEST_TIMEOUT` 30m → 60m,或把 `internal/cli` 拆成多个测试包** | `make ci-verify-full` 现在必然超时被杀。**更根本的解法是拆包**:`internal/cli` 有 409 个测试文件,拆成 4–6 个子包(如 `internal/cli/s0s4`、`internal/cli/s5s7`、`internal/cli/s8s9`、`internal/cli/e2e`)既能并行又能各自设超时,还能顺手解决 §2.3「按正则切 stage 不可靠」的问题 |
| 0.6 | macOS 腿加 `continue-on-error: true` | 连续 2 次 901s 排队取消(0 steps),是 GitHub arm64 容量问题,不是测试失败 |

> 0.1 之所以排在最前,是因为它**一行命令、零风险、且是 CI 的第一道门**。

### 第 1 步:先修 HIGH 级产品缺陷(17 条,见 §3)

**不要先删测试。** 205+ 个 CLI 失败和 22 个接缝回归是这轮重构砸出来的,先删测试会把真缺陷永久掩盖。

**最高性价比的 6 条**(按「解锁失败数 / 改动行数」排序,归因 agent 实测):

| 序 | 缺陷 | 解锁 | 改动量 | 判定依据 |
|---|---|---|---|---|
| 1 | **H15** S9 schema 容忍历史 hash 字段 | 7 | **3 行** | 蓝图 `L3-S9-repair.md:15` 明文要求「旧记录中已有散列值只作为历史原字段读取」 |
| 2 | **H9** `make([]T,0,len)` 预分配(= H7) | 4 | **1 行** | `append([]T(nil), ...)` 空列表 → `nil` → JSON `null` |
| 3 | **H14** 移除 `register.go:473-475` 的 `RecordTaskActivity` | 11 | **3 行** | 该调用违背自己上一行的注释「keep all existing TASK facts」 |
| 4 | **H10** S7 出口:让诊断与 TR-009 晋级先于静默返回 | 2 + 整条 S7→S10 链 | 小 | 阶段出口级断裂 |
| 5 | **H11** 恢复命令按 `hasIssuedIndices` 加条件 | 5 | 1 处条件 | **实测已验证**:加回条件后 `TestRuntimeRecoveryApplySamePlanIsIdempotent` 即通过 |
| 6 | **H16** Inspect 责任回填 Worktree/Branch | 16 | 中 | 父提交 PASS → HEAD FAIL |

**其余按序**(完整表):

| 序 | 缺陷 | 解锁面 | 改动风险 |
|---|---|---|---|
| 7 | **H2**(S8 人工审批门) | 恢复 `validateS8ApprovalMode` 的两处调用 | 低 |
| 8 | **H1**(S7 Stop 兜底) | 恢复 `LastAssistantMessage` 回退 | 低 |
| 9 | **H5**(S3 权威根)+ **H4**(S6 TR-003 基线)+ **H3**(S8 因果闭合链) | 恢复被误删的保护 | 中 |
| 10 | **H12**(S6 `planned_paths` 范围校验) | 恢复父提交 `plancheckpoint.Validate` 调用 | 中 —— **必须同时**把被删的 `TestS4AuditOutputPathsCannotExpandReviewedScope` 补回来,否则同样的坑会再踩一次 |
| 11 | **H17**(审计行去重)+ **H18**(review-plan 路径) | 6 + 14 | 低 |
| 12 | **H6**(S7 波次门) | 恢复派发端设门 | 中 |
| 13 | **H9/H3 组**(S9 路由) | 10 | 高 |
| 14 | **H8**(S8 来源集合)+ **H13**(pending 标记,MED) | — | 低 |

> **H12 有一个特别提醒**:它在同一个提交里**同时**删掉了机器校验和唯一能抓到它的测试。修的时候**只补校验不补测试 = 白修**。
> **H15 同理**:schema 收紧和「旧记录必须可读」是蓝图里的两条要求,本轮只做了前一半。

### 第 2 步:改写陈旧测试(§5.2 + §5.4,约 30 个函数)

全部是**函数级**改写,不是删文件。范式已由重构自己给出:`internal/cli/contract_answer_e2e_test.go:213-228`(排除 `revision`/`journal` 后比较)。

### 第 3 步:补 UNENFORCED-NEW 测试(§6,约 18 条)

这是"适配"的核心 —— 断言重构后的新宽松行为,防止有人把检查悄悄加回来。

### 第 3b 步:【新】补三条「一个测试都没有」的主接缝(§6.4)

**这是整份清单里优先级最高的一步** —— 因为失败的测试会报警,而这三条接缝**完全静默**:

| 序 | 要补的测试 | 放哪 | 参照 |
|---|---|---|---|
| 1 | **S8→S9 跳转**(investigation → repair) | `tests/system/req039/` | 仿 `ct_rework_test.go` 的 TR-023/TR-024 写法,把终点换成 TR-023/024 之后的一跳 |
| 2 | **S9→S7 完整再验证闭环**(TR-008→PTR-BUG-01→02→04→05→…) | `tests/system/req039/ct_bug_correction_test.go` | 父提交 `TestCT03914_FindingToBugRepairFullReview` 被删了 —— **把它恢复回来**(父提交 `9bf73ca` 里可直接取),并补回 `req039fixtures.SeedSealedObservationBatch` |
| 3 | **S9 修复后的后继发布/复查链** | `tests/system/req039/` | 父提交 4 个文件全被删:`recheck_delivery_e2e_test.go:22`、`branch_delivery_regression_test.go:18`、`recheck_repair_hooks_e2e_test.go:21`、`native_workspace_chain_test.go:25` |

**另外必做**:把 `tests/system/req039/helpers_test.go:8` 的文件头注释改对 —— 它现在还声称 `ct_bug_correction_test.go` 含 CT-039-14 和 CT-039-22,实际只剩 CT-039-23(§6.6)。

**优先级说明**:这三条不补,「重新删减和适配全部测试」这个目标本身就是建立在**一个没有被验证过的闭环**之上 —— 你无法确认删掉测试后系统仍然是对的。

### 第 4 步:修契约漂移(§4.2,约 10 处文本)

纯文本编辑,零风险,让 JSON / 蓝图 / 指南与代码重新自洽。

### 第 5 步:补零防护新面(§6.1,7 个符号)+ 零测试包(§6.2,2 个)

### 第 6 步:清理死代码与死资产(§6.3,10 个资产 + §3 提到的死函数)

死函数清单(确认 0 调用者,删掉即可,**不要重新接上**):
- `internal/investigation/case_workflow.go`: `requireCompetingHypothesisClosure`(`:1123`)、`validateCausalClosure`(`:1167`)、`causalClosureDefect`(`:850`)、`requireCurrentHypothesisResults`(`:1027`)、`validateBlastRadius`(`:1075`)、`validateDetectionGap`(`:1099`)
- `internal/investigation/hypothesis_result_receipt.go`: `validateS8ApprovalMode`(`:257`)—— **除非按 H2 接回去**
- `internal/runtime/review_result_legacy_fixture_test.go`:`sha256Bytes`、`reviewPacketProfessionalDigest`(0 调用者,整文件可删)
- `internal/buildsurface/read.go:111-114`:`Verify`、`VerifyContext`(导出但 0 生产调用者)
- `internal/cli/work_progress.go`:`projectMilestoneReadSources`(`:111`)、`progressDirectoryRef`(`:74`)、`progressOwnedCode`(`:145`)+ `work_progress_tools.go:78` `actualProgressRead` —— ⚠️ **需 owner 裁定**:重构彻底删了 `internal/evidence/local_path.go`,却留下这四层死代码。若是有意移除则删;若是接线遗漏则恢复 `projectWorkProgress` 里的调用(否则读取归因永久失效)
- `internal/review/StaticClaimsSettled` / `RemainingStaticClaims` —— ⚠️ 除非按 H6 接回去
- `internal/cli/projection.go` 里的 clean-round 投影、`internal/review/submit.go:654`、`internal/acceptance/review_identity.go:26` 的 `review.clean_round` 字段写入 —— 残留的机器证明概念

### 第 7 步:CI 车道提速(沿用上版报告 §5,加上本轮的新数据)

`internal/cli` 从 333 → **409** 个测试文件;`internal/runtime` 在 `-race -short` 下 382s,**已超过 5 分钟的 `CI_TEST_TIMEOUT`**;`internal/recovery` 零并发却仍在 `CI_RACE_PACKAGES` 里(3.8s 的 race 成本换零价值)——**建议摘掉**。

**本轮新增的三条硬数据(见 §7.3 / §9.1):**

1. **覆盖率塌陷比「慢」更严重。** `internal/cli` 1197 个测试 CI 只跑 52 个;`tests/system/*` 110 个端到端验收测试**一个都不跑**。提速之前先想清楚:**绿灯只覆盖了 4.3% 的 CLI 测试和 0% 的系统验收测试** —— 这种绿灯没有意义。
2. **整包 30 分钟跑不完**(实测 1800.097s 超时被杀)。**拆包是唯一正解**,不只是提速:`internal/cli` 单包 409 个测试文件、1197 个测试函数,拆成 4–6 个子包后,`-p 2` 能真正并行,每个子包也能各自设超时。**顺带解决 §2.3 的正则切分不可靠问题** —— 拆包后每个包就是天然的 stage 单元。
3. **4 个 smoke 死条目 + 4 个通配条目**(§7.4)。通配条目(`NativeS5.*`、`NativeReviewResume.*`、`CheckpointStore.*`、`Integrate.*`、`ApproveContract.*`、`DelegatedApproval.*`)会随源码增删静默漂移 —— 建议把通配改成**显式名字清单**,并加一条 CI 自检:**把 smoke 正则里的每个字面名与 `go test -list` 求差集,差集非空就失败**。这样死条目不会再悄悄溜过去。

---

## 9. 整包运行结果(父 agent 在干净克隆上实测,非切片)

### 9.1 `internal/cli` 整包 —— 30 分钟跑不完,并被超时强杀

**命令**(干净克隆 `/tmp/fullcli` @ `d0d948a`,`GOCACHE` 独立):

```bash
go test -json -p 2 -timeout=30m ./internal/cli
```

**结果**:`FAIL github.com/entroforge/go-system-builder/internal/cli 1800.097s` → `panic: test timed out after 30m0s`

| 指标 | 数值 |
|---|---|
| 顶层测试**总数**(`go test -list '.*'`) | **1197** |
| 本次**实际启动** | **1021**(85.3%) |
| 本次**通过** | **768** |
| 本次**失败** | **247** |
| 唯一**子测试**失败 | **138**(另有 701 个子测试通过) |
| **从未启动**(被超时吃掉) | **176** |
| 失败涉及源文件 | **132** 个 `_test.go` |

**⚠️ 这意味着 `make ci-verify-full`(`Makefile:116`,`-timeout=30m`)在本轮重构后不可能跑完。** 上一版报告里 `internal/cli` 只是「慢」,现在是**超时即死**。

### 9.2 挂起点与连带损伤

超时 panic 的 goroutine dump 给出了确切调用链:

```
internal/runtime/checkpoint.go:302   (*Store).publishCheckpointLocked   → f.Sync()  ← 阻塞点
  ← store.go:1965  (*Store).clearCommitMarkerLocked
  ← store.go:1351  (*Store).applyMutation          (持锁中)
  ← store.go:986   (*Store).UpdateCurrent
  ← task_planning_submission.go:168  RecordTaskPlanningPacket
  ← submission/task_planning.go:58  (*Service).IssueTaskPlanning
  ← submission/design.go:362         preparePlanningOutlet
  ← submission/contracts.go:213      (*Service).SaveContracts
  ← cli/stage_answer.go:367          consumeRegisteredStageAnswer
  ← cli/stage_answer.go:166          runStageAnswer
  ← cli/run.go:1042                  runRuntime
  ← cli/run.go:209                   Run
  ← cli/stage_answer_e2e_test.go:53  stageCLI
  ← cli/task_planning_answer_e2e_test.go:41  newTaskPlanningAnswerFixture
  ← cli/s5_rework_e2e_test.go:22     currentS5ReworkFixture
  ← cli/s5_rework_e2e_test.go:65     TestS5ReworkRetiresOriginalWorkersAndPreservesReceivedReplay
```

**连带损伤**:`TestCT03910_ConflictDirtyPreservePins`(`internal/cli/ct_11_24_test.go:93`)在 `t.Parallel()` 上**阻塞了 11 分钟** —— 串行阶段不结束,并行阶段就永远不会被释放。所以 1 个测试挂住 → **整包 176 个测试陪葬**。

**这不是确定性死锁**:单独跑该测试 `ok ... 17.504s` 通过。本机 8 核 / 7.9GB / **无 swap**,且同时有 3 个 agent 在跑测试 → 属于**资源竞争型挂起**。

> **但这恰恰是最该记的结论**:CI 的 GitHub runner 是 **2 核 / 7GB**,比本机更弱;`internal/cli` 在 `-p 2` 下已经用满 30 分钟。**无论挂起是不是偶发的,`make test` 的 30 分钟预算对 `internal/cli` 已经不够用了。**

### 9.3 247 个失败的分布(按前缀)

| 前缀 | 失败数 |
|---|---|
| `TestS9*` | 50 |
| `TestNativeS9*` | 37 |
| `TestCT*` / `TestAC*`(验收场景) | 18 |
| `TestNativeS6*` | 12 |
| `TestS7*` | 12 |
| `TestNative*`(其它) | 12 |
| `TestS8*` | 8 |
| `TestS1*` | 6 |
| `TestS5*` | 4 |
| `TestS4*` | 3 |
| `TestS3*` | 2 |
| `TestNativeS5*` | 1 |
| 其它(跨 stage 集成/回归类) | 82 |

**失败最密集的源文件(前 10)**:

| 文件 | 失败数 |
|---|---|
| `internal/cli/s9_handoff_wiring_test.go` | 15 |
| `internal/cli/native_s9_recovery_test.go` | 9 |
| `internal/cli/ct_test.go` | 7 |
| `internal/cli/run_test.go` | 7 |
| `internal/cli/controller_test.go` | 5 |
| `internal/cli/native_s9_reception_split_test.go` | 5 |
| `internal/cli/ac_test.go` | 5 |
| `internal/cli/native_execution_return_test.go` | 5 |
| `internal/cli/review_plan_pre_listener_e2e_test.go` | 5 |
| `internal/cli/policy_ref_test.go` | 5 |

**S9 是重灾区**(`TestS9*` 50 + `TestNativeS9*` 37 = **87 个失败**,占 35%):与 H9(新路由层无法把原生 Result 解回 Plan 包)直接对应。

### 9.4 CI smoke 车道在这 247 个失败里会踩到几个

| 项 | 数值 |
|---|---|
| smoke 正则选中的 `internal/cli` 测试 | 52 |
| 其中**失败** | **1** |
| 其中通过 | 51 |
| 其中**因超时从未启动**(状态未知) | 1 |

唯一被踩到的:

```
FAIL github.com/entroforge/go-system-builder/internal/cli TestNativeS5AmbiguousReferencesDoNotReceiveEitherAnswer
```

→ **`make test-ci-smoke` 在 `d0d948a` 上是红的。** 这与 CI 车道 agent 的独立实测一致(冷跑 `test-ci-smoke` 259.5s,失败原因就是这个测试的诊断文案变更)。
→ 也就是说:如果 CI 真跑起来,它会在 `fmt-check` 之后、在 smoke 车道上**第一次变红**。

### 9.5 其它包(父 agent 复核)

| 包 | 结果 |
|---|---|
| `internal/investigation` | **21 个失败**(唯一一个包级有失败的包),全部是 `internal/cli` 之外唯一的失败集中点 |
| `internal/transition` | 316 测试全绿(父提交 347 也全绿)—— **但重构删掉了 `enforceForbiddenEvents()`**,见 §4.1 |
| 26 个基础设施包 | **1363 测试 0 失败**(三轮跑:全量 / `-race` / CI 等价) |
| `internal/integration` | HEAD **83 测试 / 2 失败 / 166.2s**;父提交 **115 测试 / 0 失败 / 406.0s** —— **32 个测试是被删的** |
| `tests/system/req039` | HEAD **98 / 19 失败 / 83.1s**;父提交 **114 / 45 失败** —— 父提交基线本身是坏的 |
| `tests/system/recovery` | HEAD **12 / 5 失败 / 6.6s**;父提交 **12 / 0 失败** —— 5 个全回归(H11/H13) |
| 其余 stage 包 | S0–S2 / S3–S4 / S5–S6 / S7 / S10–S11 全部包级全绿,失败全部集中在 `internal/cli` 切片 |

**一条贯穿全局的规律**:除 `internal/investigation` 外,**所有包级失败都只发生在 `internal/cli`**。这说明重构砸坏的**不是领域逻辑,而是 CLI 接线层**(S9 路由、S7 兜底接收、S6 上下文交付、S3 权威绑定、S6 范围校验、Runtime 恢复)。

**一个值得注意的时间信号**:`internal/integration` 在 HEAD 上从父提交的 **406s 降到 166s(-59%)** —— 这不是优化,是**断言被删掉了**。跑得更快不等于跑得更好;这正是 §5.4b 与 §6.5 要记的账。

---

## 10. 附录:本轮审计推翻的几条前版结论

| 前版结论 | 裁定 | 依据 |
|---|---|---|
| 「`forbidden_events` 一直就没有运行时消费者,属于既有缺陷」 | **推翻** | 父提交**有** `enforceForbiddenEvents()` + 3 个证明测试。**是本轮重构删的** → 回归 |
| 「重构改写了 `forbidden_events` 的 `reason` 字符串」 | **推翻** | 8 条 `reason`/`enforcement` 与父提交**逐字节相同**;唯一变化是删除了第 9 条 |
| 「`internal/review` 有 30 个失败」 | **推翻** | 干净克隆里 `internal/review` **399 测试 0 失败**。那 30 个来自**脏工作树的 8.4GB `.local/`** |
| 「S8 的 reentry budget 删除是未覆盖的能力缺口」 | **推翻** | 删除是**有意且已被守卫**:`case_workflow_test.go:738` 设 limit=1 后连做 7 次 `investigate_more`;`legacy_budget_compat_test.go` 证明旧键可选无默认 |
| 「S9 引用等价性的负向半边没测」 | **推翻** | `internal/cli/native_s9_reference_acceptance_test.go:49-59` 完整覆盖 basename / `../` / `prefix` / 绝对根 / URL / 环境变量等拒绝场景 |
| 「`site_lost[]` 声明已移除」 | **部分推翻** | `site_lost` **仍是** `stage-answer-s7.schema.json` 的必填字段;只移除了 `verification_artifact_digest` |
| 「`runtime-portability.yml` 没有 Go 缓存」 | **推翻** | setup-go v5 的 `cache` 输入**默认 true**(`action.yml`);显式写 `cache: true` 是空操作 |
| 「`internal/verification` 包存在」 | **推翻** | 本提交**已删除**该包(`clean_round.go` 一起删) |
| 「父提交 `internal/integration` 也有 `hook output is not JSON: EOF` 这类失败」 | **推翻** | 接缝 agent 实测:父提交 `internal/integration` 是**干净的 `ok`(115 测试 0 失败,405.978s)**。那些 EOF 错误来自父提交的 **`tests/system/req039`**(45 个失败) |
| 「`internal/integration` 的测试是跑挂的」 | **推翻** | 它的 32 个测试是**被重构删掉的**:115 → 83,16 个测试文件删除,**零替代断言**(§5.4b) |
| 「`internal/acceptance/review_identity.go` 零覆盖是缺口」 | **推翻** | `internal/cli/audit_identity_hook_e2e_test.go:14` 端到端覆盖了 review identity 链(`review_plan_id` 传播 + 「旧 PASS 不得成为当前」),且在 HEAD 通过(§6.4 第 11 条) |
| 「`internal/cli` 整包只是慢」 | **推翻** | 实测在 `-timeout=30m` 处 **`panic: test timed out`(1800.097s)** 被强杀,176 个测试从未启动 → `make ci-verify-full` 必然超时(§9.1) |
| 「`test-ci-core` 覆盖了绝大多数包」 | **推翻** | 它用 sed 把 `internal/cli`、`internal/integration`、`internal/investigation`、`tests/system/*` **整体剔除** → 这三个包 CI 只跑 52 / 21 / 16 个名字,`tests/system/*` **一个都不跑**(§7.3) |
| 「`internal/cli/run.go:2691` 是 PreToolUse 提前返回 → 工具调用不留痕」 | **撤回并更正** | 该处位于 **`if renderHook && request.Event == \"PreCompact\"`** 分支内(`:2673`),**父提交 `:3124-3126` 逐字相同** → **既有行为**。本轮新增的只有 `reader.SelectionIssue` 分支与新文件 `hook_reader_s5.go`(M2 已改写) |
| 「hook 输出丢失 `QUALITY_GATE` 投影是本轮重构砸的(32 个失败,最大组)」 | **推翻** | 归因 agent 在 R01 组内 5 个代表上逐一对照父提交,**全部同样失败** → **既有失败**。`run.go:2961` 的 `!graphObserved` 跳过结构在父提交 `run.go:3197` 同样存在 |
| 「H9 占 22 个失败」 | **更正为 10 个** | 归因实测:R03 组 = 5 顶层 + 5 子测试。S9 一族 87 个失败里大部分来自其它根因 |
| 「`observeMainS9Impact` 是零引用新面」 | **确认** | `internal/cli/s9_impact_observer.go` 由 `d0d948a` **新增**(`git log --diff-filter=A` 确认),父提交 `git grep` 命中 0 次 → 确实是本轮引入 |

---

*本报告由 **11 个并行 sub-agent** + **父 agent 独立实测**汇总。父 agent 的独立实测部分:整包 `internal/cli` 运行(§9)、CI 车道覆盖率的正则×`go test -list` 逐条比对(§7.3–§7.4)、`gofmt` 复核(§7.1)、`tests/system/*` 测试计数(110 个端到端测试全不在 CI)、H2/H7/H14/H15/M1/M2 的父提交对照复核(§3.10)、H9 影响面的实测更正(§2.5)。*
*每个 agent 的原始输出含逐条 file:line 与决定性错误摘录;接缝 agent 与归因 agent 的完整报告已作为附件随本报告提交(见文首)。*
*所有结论均在 `/tmp` 下的干净克隆(`d0d948a`)与父提交 worktree(`9bf73ca`)上实测,原仓库 `/home/sebs/go-system-builder` 除本报告外**未被修改**。*
