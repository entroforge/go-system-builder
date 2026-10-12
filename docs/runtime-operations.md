# S7 / Repair 工件提交与重试

S7 的 ReviewPlan 登记、修订、ReviewResult、site-lost 阻断件、FindingSupplement 使用 Runtime 的同一条工件提交链。领域校验与 CAS 仍决定是否接受；工件文件存在本身不授予证据资格。

## 可重试的 S7 操作

以下命令支持可选的 `--operation-id`：

```bash
loop-harness runtime review-plan --file plan.json --operation-id plan-r1
loop-harness runtime review-plan revise --file plan-v2.json --source-ref runtime:source-evidence --affected-surface src --operation-id revise-r1
loop-harness runtime review-result submit --assignment-id assignment-qa-1 --result result.json --operation-id result-qa-r1
loop-harness runtime operation --id result-qa-r1
```

操作 ID 在一个 Runtime 内唯一。保留它并用完全相同的逻辑输入重试，提交成功后会返回原回执，不再次消费结果、批准或预算。JSON 排版和输入文件位置不影响身份；内容、capture 内容、修订依据、范围、显式时间与执行身份属于输入。CAS revision 是并发断言，不属于逻辑输入。复用已提交的 ID 改输入会报冲突，应先检查原回执，再为确实不同的工作使用新 ID。

回执保存在既有 Runtime journal 的 `action_results` 中，包含 `operation_id`、`input_sha256`、Runtime/actor/kind、原始提交时间和 revision、event ID、工件 path/hash。响应中的 `current_revision` 可能已更大；不能把重试时刻当作新的执行或审批。site-lost 的回执只证明阻断状态已登记，重试仍返回未消费结果的阻断响应。

`runtime operation` 查询当前 Runtime，返回 `committed`、`not_found` 或 `recovery_required`。它不恢复、不改 state/journal；使用进程锁获得一致读取。`not_found` 不证明历史 Runtime 从未执行过此 ID。存在 pending 时，不推断成功或失败，先使用支持该 pending 版本的 Writer 恢复。

未提供 `--operation-id` 的旧调用继续使用重复拒绝语义。FindingSupplement、workgroup、revive 及下表未列出的 Repair 入口尚未提供稳定操作 ID，不要将上述重试保证外推到这些命令。

workgroup activation 仍使用原有的可修改草稿写入链，尚未接入此工件事务。失败时保留已写出的草稿，不能按调用方旧引用列表删除；没有已提交的 Agent/activation_ref，不授予这些草稿执行权限。其独立 staging/恢复改造仍待完成。

## 可重试的 Repair 操作

以下五个入口支持 `--operation-id`，返回 `operation_receipt` 和当前 `revision`：

| 命令 | 本次提交的工件 |
|---|---|
| `runtime repair session open` | RepairSession |
| `runtime repair plan compile` | RepairPlan |
| `runtime repair plan-report submit` | PlanReport |
| `runtime repair result submit` | RepairResult |
| `runtime repair handoff commit` | 下一轮 S7 ReviewPlan 种子 |

操作 ID、逻辑输入、执行身份和显式时间必须保持一致。入口在检查当前阶段之前查询原回执；重试发生在之后的阶段也能返回原提交，不再次增加 review round。返回的历史结果不表示当前产品文件再次通过验证。新旧请求共用 ID 却改变输入会报冲突；未提交的 CAS 失败可以用同一 ID 重新准备。原工件缺失或 hash 改变时，查询和重试均拒绝成功。

Session、Plan、PlanReport、Result 的新物理路径包含 Runtime/Session 摘要，领域 ID 和内容 hash 各自保留。S9 种子 ID 同时包含 Runtime/Session 摘要和轮次。读取旧路径仍依照已记录的 path/hash；不会重命名历史件。`repair changeset compute --session-id ...` 默认从当前 Runtime 指针解析 Session，归档的新路径可显式传 `--session-ref <path>`。旧的独立创作平面路径继续可读。

公开的独立工件创作 API 使用私有完整文件、文件 fsync、无覆盖发布和目录 fsync；它不自动提交 Runtime，也不具有上述操作回执语义。CLI 的 PlanReport 提交已组合为同一 Runtime 事务。ChangeImpact、TargetedReverification、RepairHandoff 本体的独立创作、派单 manifest/task 和 activation 尚未全部组合为事务，不可外推为完整 W5 已完成。

## 提交与恢复顺序

1. 校验领域输入；将整个工件组写入本操作私有的 `.claude/operations/staging/<随机身份>/` 并 fsync。
2. 获取 Runtime 进程锁，核对实际准备 revision、权威与引用，验证候选状态。正式目标若已存在，拒绝覆盖。
3. 写入并同步 `.claude/loop-state.json.commit-pending.json`。S7 工件组版本为 `2.0.0`；包含 Repair 路径的组使用单独的 `2.1.0` schema，记录暂存路径、正式路径和摘要，并由 journal action 摘要关联。它不是 Runtime/ReviewPlan 的 v2 协议。
4. 使用无覆盖的 hard link 发布完整工件，再同步目录；依次写 state、追加 journal。
5. 清除并同步 pending marker；只清理本操作的私有暂存。正式工件不可在普通失败路径删除。

同一文件系统内的 hard link 是此发布方式的要求；跨文件系统或平台不支持时保持失败，不降级为覆盖或部分写入。暂存和正式输出都限制在项目控制面内，拒绝 symlink 路径。该机制不防御拥有同一宿主账户、能直接改写控制面文件的任意进程。

恢复先验证完整的 state/journal 连续性、候选状态、manifest 及全部工件字节，再补齐缺失发布。已存在且摘要一致的成员可复用；任何缺件、漂移、路径/版本冲突都会保留 pending 和暂存并拒绝继续。读取接口不会隐式修复。marker 写入返回错误也可能已完成 rename，因此从尝试写 marker 起保留暂存。

旧 `1.0.0`、`2.0.0` pending 继续兼容读取，旧 schema 原样保留。修复前一批实际二进制已验证拒绝 `2.1.0` pending，并保留 state/journal/marker/工件字节。不能给 Repair 工件组改标签为 `2.0.0`。已核对的旧 Writer 拒绝 `2.0.0` pending，不能使用旧二进制恢复新工件事务；其他历史 Writer 的兼容性需分别验证。已经完成的事务仍使用原 Runtime/journal schema。这不是旧项目升级许可，仍遵守 [升级政策](framework-upgrade.md)。

## 清理边界

旧式 `runtime recover plan/apply` 只重建受支持的旧协议。识别到新 Runtime
格式、durable invocation 或不支持的 pending 格式时，返回
`LOOP_RECOVERY_PROTOCOL_UNSUPPORTED`，不生成重建计划、不替换 state/journal，
并保留原工件。apply 在 Runtime 锁内重查；恢复已中断的 apply 还会检查隔离区
的源文件，不能因当前 state 已被旧 Writer 换成 v1 就继续降级恢复。
`runtime recover inspect` 仍可盘点并记录原始 SHA。

这项保护只属于含修复的二进制。已固定实测的旧安装二进制仍能通过其 recovery
入口把合成 v2 状态重建为 v1；迁移前必须停止并隔离它，不能只依赖 schema。
本修复没有启用 Runtime/Review v2，也没有提供旧项目迁移。严重损坏、无法辨认
协议声明的输入仍须凭匹配的发行件和完整恢复集判断，不能当作已确认兼容。

普通错误不能依据旧 revision 或调用方的旧引用列表删除正式工件。失败注册可能留下未被计划授权的空 verification workspace，后续权限仍由 Runtime 计划决定。canonical GC 尚未实现；保留当前、归档及 pending 可能引用的证据，不手工移动或删除以腾出 ID。新事务只在确定未尝试持久化 pending，或已完成提交并清除 marker 后清理私有暂存。

## Hook deadlines and timing diagnostics

The Hook evaluation passes one remaining deadline through its primary context
load, control cycle, Runtime snapshot/CAS, evidence refresh, automatic transition,
Git file view, PreToolUse milestone and audit append. Runtime OS-lock and legacy
sentinel waits share a five-second cap bounded by that deadline; they do not each
start another five-second wait. Callers of the Go API can use `Store.WithContext`,
`transition.ApplyContext`, `fileview.NewContext` and `Outbox.AppendContext`.
Existing APIs retain their default budgets. The outer native Hook process still
buffers output and enforces its 45-second process-tree deadline.

Cancellation before a durable pending marker rejects the proposed commit and
cleans only its private staging. Once pending exists, cancellation or a lost
response requires operation lookup and recovery; it is not proof that no commit
occurred. Pending and published artifacts are retained. A cooperative context
cannot interrupt every filesystem call or a validator that ignores context.
Lifecycle reconciliation, observer/activation helpers and all other non-Hook
commands have not yet been fully converted to one caller deadline; the outer
process deadline remains necessary.

PreToolUse audit records with measured phases use diagnostic schema `1.2.0`;
records without that instrumentation retain `1.1.0`. The original strict 1.1
schema is unchanged. The embedded validator dispatches by the declared version,
and fresh installs include both schemas. This is an audit-format change, not a
Runtime/Review protocol migration or authorization for upgrading an old project.

The existing `.claude/hook-decisions.jsonl` contains `timing.phases` and the
observed REQ, generation, round and assignment when available. Its decision ID,
Runtime and session fields associate the observations with the same invocation.
Each phase reports measured `calls` and `duration_ns`; missing phases remain
absent. `doctor`/metrics output includes per-event and per-phase sample count,
p50 and p95 milliseconds for the most recent 256 observed samples. Phase totals
within one invocation include repeated calls. Nested phases overlap: never sum
them as user waiting time, and do not interpret missing timing or cost as zero.
A process killed before emitting its decision has no completed timing record.

Read-only snapshots still use the process lock and compatibility sentinel.
Shared-reader locks require a proven writer-version fence before they can be
used safely with older writers. Journal validation still checks complete current
history bytes; no mtime cache or trusted-prefix checkpoint was introduced.

## S8 contract approval

`runtime investigation contract approve` accepts `--operation-id`. Preserve that
ID, the exact draft path/hash, Case, approver, decision/delegation references and
any explicit `--occurred-at` on retry. `--expected-revision` is a CAS check, not a
new operation identity. Query the durable receipt with `runtime operation --id`.

The approved Contract, approved Case revision, authority consumption and Runtime
journal/state now commit in one recoverable artifact bundle. The writer rechecks
the draft, original Case, sealed baseline, causal evidence and human/delegated
approval under CAS. A different actor, hash, Case or approval reference with the
same operation ID conflicts. A concurrent identical retry returns the original
receipt and consumes a human decision or delegation allowance only once.

The receipt's artifact paths and committed revision describe the original
approval even after the active Case advances. A durable retry no longer needs
the temporary draft file. Without an operation ID, existing already-approved
Case reconciliation remains available, but stable IDs are preferred for lost
responses. Pending/unknown outcomes retain staging and published artifacts;
never remove them to make a retry succeed.

Investigation approval publication uses pending format `2.2.0`, with exact
Contract/Case output directories. Formats 1.0, 2.0 and 2.1 remain readable and
their original schemas are unchanged. Run the matching complete binary/assets
set and isolate old writers; this is not a historical project migration or a
universal fence against separately installed older recovery tools.

## Zero-change confirmation and legacy authority restoration

A repair already completed in an earlier Session may be confirmed without a
new implementation diff. Open with `runtime repair session open --intent confirm
--confirmation-sources <handoff-refs.json> --session-id <id> --created-by <agent>`.
The sources file is a JSON array of `{ "path": "<prior-handoff-path>", "sha256":
"<exact-hash>" }` references. Every source must be a previously committed
RepairHandoff indexed in the same Runtime, with a passing result and independent
targeted verification. The current approved RepairContract still authorizes the
scope and assertions; a prior PASS does not approve current behavior or a changed
contract. Historical artifacts and approval bytes are never edited.

The new Session records `intent=confirm`, `confirmation_sources`, and server-derived
`verified_subjects`. Each subject has a path and exact hash. A confirmed deletion
also has `status=deleted` and requires the path to remain absent. Changes elsewhere
in the Session implementation baseline also reject confirmation. If implementation
or new test-source changes are needed, return to S8/S9 planning and use an
`implement` Session; the confirmation mode is specifically a zero-source-change
flow. Execution outputs may use the existing repair-control/evidence directories.

Keep the existing Assignment/PlanReport/execution checkpoints. Confirmation
PlanReports put current passing, evidence-backed checks in the legacy `red_checks`
field; do not fabricate a pre-fix failure. Confirmation Results have
`changed_artifacts=[]`, current passing `checks`, and exactly the inherited
`verified_subjects` inside that Assignment's scope. `before_fix_checks` can remain
empty; prior failures are traceable through the pinned source handoff. Every
Assignment must report its current verification; neither the old PASS nor a
checkpoint substitutes for its Result.

Compute the Changeset from the Session (the existing default CLI path). Its
`artifacts=[]` is the actual diff; `verified_subjects` separately carries the
confirmation surface. Create ChangeImpact with `session_ref` bound to the current
Session, `changed_artifacts=[]`, and its exact `verified_subjects`; decision scopes
must cover those subjects. Do not put yesterday's repair into today's change list.
The independent targeted checks and stop-condition assessments remain mandatory;
handoff opens a fresh complete S7 and freezes the confirmation subjects there.
This does not make confirmation an E2E exemption.

Confirmation Session/Result/Changeset and ChangeImpact use artifact schema
`1.1.0`. New schemas are separate; original strict `1.0.0` schemas remain
byte-identical. Readers dispatch by artifact type and version. Runtime state and
journal protocols are unchanged. Matching binaries and assets must be deployed
together and incompatible installed executables isolated before using the new
artifacts; this does not introduce general Runtime migration or revoke external
copies of old writers.

`LOOP_ALLOW_EMPTY_FINGERPRINT=1` no longer relaxes authority, empty-diff or exact-set
checks. An old Session missing only its Runtime fingerprint can use:

```bash
loop-harness runtime repair authority restore --root <root> --actor <agent>
```

The command checks the pinned immutable Session, current approved Contract and
Runtime/REQ/generation binding, and requires actual changes to equal its already
committed Results. It checks authority freshness and confirmation bytes again
inside the Runtime CAS lock, restores only the Session's original digest, and
journals the restoration. It never captures a replacement baseline or approves
an unrecorded mutation. A matching fingerprint makes a retry a no-op; a different
fingerprint, stale Contract, unrecorded changes or invalid provenance is rejected.
Preserve those inputs and route through S8/S9 planning instead of enabling an
environment bypass.

## Related-module scope operations

New S7 execution requires ReviewPlan 1.1 with the framework-generated `module_scope`. Use `s7 draft`, refine dispositions/Claims, then run `s7 lint`; author-only lint cannot establish Runtime readiness. `s7 status` exposes candidate counts, unresolved inputs, decisions and the input digest. Preserve operation IDs for retries. Drift or unknown ownership needs corrected/approved input and a fresh scope; deleting candidates or downgrading the schema cannot recover it. Top-level scope revisions invalidate their obligations, and final consumption verifies actual Result artifacts. See [related-module verification](related-module-verification.md).
