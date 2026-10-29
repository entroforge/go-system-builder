# S8/S9 领域请求示例与显式命令参考

当前原生工作先读[工作区指南](../../workspace-integration.md)。Main 使用原领域命令登记 Hypothesis、派发、路由及维护 Contract；S8 Investigator 和 S9 Builder/Targeted 通过准确问卷或 brief 工作，保存实际产物、返回准确引用，由 SubagentStop 接收。不额外交通用 PLAN_REPORT、SendMessage 激活或手工 execution begin。

本目录 JSON 是原 `--file` 领域接口的请求形状，供 Main 显式操作、诊断及已冻结兼容来源使用，不是让新子代理照抄整串命令的普通步骤。所有示例都是编写参考，不是已登记证据；路径、身份、ID、摘要和断言必须来自当前真实责任。

## 请求和持久化产物的区别

每个请求的字段跟随原 CLI 接口；Runtime 从它推导并持久化领域记录。后者才是 `repair-*.schema.json`、`review-evidence.schema.json` 等产物 Schema 描述的对象。因此请求的 `session_ref`、`occurred_at` 与持久化产物的 `session_id`、`reported_at` 等字段可能不同。不要拿产物 Schema 验证请求或另造一份记录；由原命令消费请求，校验并保存派生产物。

示例占位 ID、路径和摘要不具备真实资格。调用者省略可选显式 revision/hash 断言时，Runtime/Case Writer 使用原当前指针；这不允许猜一个来源或刷新旧记录。

## 显式请求与消费者

以下直接接口沿原权限和原来源使用。新原生 Builder/Investigator 的专业答卷由 Stop 调用同一消费者，不重复执行 submit。

| 请求文件 | 原消费者命令 |
| --- | --- |
| [repair-contract-draft.json](repair-contract-draft.json) | `loop-harness runtime investigation contract approve --case-id <case> --file repair-contract-draft.json --approved-by <actor> --approval-hash <sha256> --approval-evidence-id <evidence-id>` |
| [repair-plan-report.json](repair-plan-report.json) | `loop-harness runtime repair plan-report submit --file repair-plan-report.json --actor <agent>` |
| [repair-result.json](repair-result.json) | `loop-harness runtime repair result submit --file repair-result.json --actor <agent>` |
| [change-impact.json](change-impact.json) | `loop-harness runtime repair impact create --file change-impact.json`，再 `... impact commit --file <created-impact.json>` |
| [targeted-reverification.json](targeted-reverification.json) | `loop-harness runtime repair targeted create --file targeted-reverification.json`，再 `... targeted commit --file <created-reverification.json>` |
| [repair-handoff.json](repair-handoff.json) | `loop-harness runtime repair handoff create --file repair-handoff.json`，再 `... handoff commit --file <created-handoff.json>` |

Contract 的批准仍需要原真实人类决定及其证据，示例的 `main-session` 字符串不构成批准。

## Main 的调查与原生交接

Main 先登记可证伪假设和判别方法；假设登记使用原 flags，不要求新建 JSON：

```text
loop-harness runtime investigation hypothesis register \
  --case-id investigation-case-... --id hypothesis-1 \
  --assignment-id assignment-s8-hypothesis-1 \
  --statement "..." --invariant "..." \
  --discriminator "..." --support "..." --refute "..." \
  --source-finding finding-...
```

Main 再用 Hypothesis 已保存的同一 `assignment_id` 准备责任。下面的命令生成原 manifest/task，随后原准备／问卷入口提供当前准确 view/answer；Main 还须把这些引用交给 Claude Code 原生 Agent 真正派发：

```text
loop-harness runtime investigation dispatch \
  --case-id investigation-case-... \
  --hypothesis-id hypothesis-1 \
  --agent-id agent-investigator-1
```

原生 Investigator 在本人答卷写方法、实际观察、证据、判别与结论，返回准确路径，Stop 接收。只有明确诊断或已冻结非原生来源才使用以下直接结果命令；不能对已返回的原生答卷重复提交：

```text
loop-harness runtime investigation hypothesis result \
  --case-id investigation-case-... --hypothesis-id hypothesis-1 \
  --assignment-id assignment-s8-hypothesis-1 \
  --method "read-only boundary trace" --observed "..." \
  --result supported --explains finding-... \
  --source-boundary src/server/decoder.go:87 \
  --evidence path:... --counterfactual "..."
```

S8 的 `--source-finding`、`--source-boundary`、`--evidence`、`--explains`、`--does-not-explain` 可重复或用逗号分隔。CLI 保留完整列表，原 Case 校验器检查必需值、重复和来源 Finding 子集；列表被解析不代表结论成立。

## Main 路由、批准与修复派发

因果闭合后由 Main 消费原 Case 路由；`s9_repair` 的材料可参考 [causal-model.json](causal-model.json)、[blast-radius.json](blast-radius.json)、[detection-gap.json](detection-gap.json)。下面是 Main 原领域操作，不是 Investigator 的额外交付：

```text
loop-harness runtime investigation route \
  --case-id investigation-case-... --route s9_repair \
  --reason "..." --primary-root-cause "..." \
  --causal-model-file causal-model.json \
  --blast-radius-file blast-radius.json \
  --detection-gap-file detection-gap.json
```

只有原人类决定成立且证据匹配时，Main 才提交原 Contract 批准：

```text
loop-harness runtime investigation contract approve \
  --case-id investigation-case-... \
  --file repair-contract-draft.json \
  --approved-by <actual-approved-actor> \
  --approval-hash <draft-sha256> \
  --approval-evidence-id <human-decision-id>
```

原 Case 输出的 `next` 是当前可执行接续，不能用示例取代。Main 修复派发从不可变 RepairAssignment 内部生成 manifest/task，不接受外部 `--manifest`：

```text
loop-harness runtime repair dispatch \
  --assignment-id repair-assignment-... \
  --agent-id agent-... \
  --role-family backend-builder \
  --agent-definition agents/backend-builder.md
```

命令 JSON 返回原生成路径与 next_action，原准备／问卷入口提供准确 native brief。Main 携带该 brief 通过 Claude Code Agent 真正派发，Builder 在实际 worktree 先保存 PlanReport，再修复、自检、提交实际变化并保存 Result，返回后由 Stop 接收。原红绿验证、Contract 约束、明确审批和独立性继续保留。直接 submit、`runtime repair execution begin` 只服务明确诊断或已冻结的旧执行，不是新 Builder 的附加步骤。

## Main 接收与 fresh S7

候选接收后，Main 用原集成和必需检查接收实际结果；随后沿原 Changeset、ChangeImpact、独立 TargetedReverification 与 Handoff 工作。ChangeImpact 每项决定的 `scope` 要覆盖实际变更产物；独立 verifier 使用当前准确 Targeted 答卷返回，由 Stop 接收，不能让 Builder 代签。显式 `targeted create/commit` 请求仅供它自己的原来源使用。

Handoff 以原已接收事实成立，commit 触发 TR-012 并保存 S7 seed；Main 查看 `loop-harness s7 status`、完善专业计划后再派发下一轮。PlanReport accepted、候选 accepted 和 Main 集成完成均不能代替 fresh S7 或发布授权。

## 显式请求中的 Artifact 摘要

原 commit 命令可能重写先前创建的产物（例如 `impact commit` 更新 status）；create 返回的 SHA 在 commit 后可能不再适用，Changeset 文件名也随其内容摘要变化。Main 显式编写低层 `repair-handoff.json` 请求时，要使用当前原路径和真实摘要，由原 validator 核对当前字节，不能抄某次 create 的旧输出。原生专业答卷的机器字段仍由原准备/接收器填写，不把此显式请求规则扩展成子代理手抄 SHA 的要求。

## 完整业务示例：撤回后保留期间合法修订

下面是假设产品已确认的业务规则，用于说明怎样写完整 oracle，不向实际产品新增业务要求。实际项目以自己的 locked 需求、场景与 Contract 为准。示例 ID、路径和观察均不是真实 Runtime 记录或测试 PASS。

业务约定为：股东可以编辑地址、提交申请、撤回并重新打开草稿；申请待审期间，联系人电话可以经另一条授权流程独立生效。撤回只取消本次申请，不能回滚独立生效的资料；撤回后的草稿保留待编辑地址，同时显示当前合法联系人信息。证书消费者读取当前已生效资料。

### 从 witness 到完整旅程

| 场景事实与分区 | fixture 中的具体含义 |
| --- | --- |
| `FACT-SUBMISSION / pending-address-change` | 已生效版本 v10 的地址为 A；草稿 D 将地址改为 B，提交产生申请 S，尚未批准 |
| `FACT-CONCURRENT-REVISION / approved-contact-update` | S 待审期间，另一条真实授权流程把电话 P1 改为 P2，形成已生效版本 v11 |
| `FACT-ACTOR / submission-owner` | 当前账号是 S 的所有人，有撤回及编辑权限；没有额外的批准权限 |

`fixture-contract.json` 应说明合成主体、创建上述状态的合法方式和按本次 ID 清理的办法；不能修改真实业务记录或把关键消费者全部 mock 掉。`flows.md` 中的 `PATH-WITHDRAW-REOPEN` 连接以下操作：

1. 在 v10 上打开 D，编辑地址为 B 并提交为 S；观察待审状态，已生效地址仍为 A。
2. 通过允许的独立流程将电话更新为 P2，确认其真实持久化版本为 v11。该变化必须发生在提交后、撤回前。
3. 原账号撤回 S，观察其状态为 withdrawn、产生本次撤回审计，且没有待审残留。
4. 重新打开可编辑草稿 D：地址仍为 B、电话为 P2；原申请 S 的历史仍为 withdrawn，不被重新标成待审。
5. 刷新或重新读取实际服务，验证 v11 的已生效地址 A、电话 P2 没被旧 v10 快照覆盖。
6. 走证书消费者的实际入口，确认读取 v11 对应资料，而非待审地址 B 或旧电话 P1；核对没有本次撤回引起的重复证书或通知。

不要把第 3 步按钮成功当作整个旅程通过。UI、API 与可用持久化/审计证据分别证明可见结果、真实服务状态和禁止副作用；缺哪项就如实说明观察缺口。

### CASE 的完整形式

实际维护模块的 `scenario-model.json` 规则分支并生成 `cases.json`，不手工另造每轮场景清单。以下展示生成后 `cases.json` 的完整字段形状；witness 使用上表分区，资料来源、fixture、story 和 flow 在实际项目的同一模块包中定义。

```json
{
  "module": "shareholder",
  "cases": [
    {
      "id": "CASE-WITHDRAW-REOPEN",
      "rule_id": "BR-WITHDRAW-ISOLATION",
      "branch_id": "withdraw-with-concurrent-approved-revision",
      "title": "撤回申请并重开草稿时保留独立生效的联系人修订",
      "polarity": "positive",
      "required": true,
      "witness": {
        "FACT-SUBMISSION": "pending-address-change",
        "FACT-CONCURRENT-REVISION": "approved-contact-update",
        "FACT-ACTOR": "submission-owner"
      },
      "oracle": {
        "visible": ["申请 S 显示已撤回", "草稿 D 可编辑且显示地址 B、电话 P2"],
        "terminal_state": "S 保持 withdrawn；D 可编辑；已生效主体保持 v11",
        "persisted_effects": ["只撤回 S 并保存对应审计", "D 保留本次地址编辑 B", "已生效 v11 仍为地址 A、电话 P2", "证书消费者仍读取 v11 的已生效资料"],
        "forbidden_side_effects": ["用 v10 快照覆盖独立生效的 P2", "将未批准地址 B 写入已生效主体或证书", "重开 D 时恢复 S 为待审", "因重试或撤回重复生成证书或通知"]
      },
      "fixture_id": "FIXTURE-WITHDRAW-CONCURRENT-REVISION",
      "story_refs": ["STORY-WITHDRAW-AND-EDIT"],
      "flow_refs": ["PATH-WITHDRAW-REOPEN"],
      "browser_required": true
    }
  ]
}
```

真实观察发现“电话回到 P1”时，S7 Finding 应记录旅程、提交/合法修订/撤回的实际顺序、持久化和消费者观察以及证据引用；不能直接宣布数据库回滚就是根因。S8 通过原 Hypothesis 区分旧客户端缓存、撤回服务覆盖整条记录、消费者读错版本等解释，保存支持与反证，因果成立后才形成以下修复约束。

### RepairContract 怎样承接问题

以下是结构完整的草案形状，不是假装获得批准。它只在原 Case 证据证明“撤回服务把提交时整行快照写回当前主体”后适用；`causal_model_ref` 和 Finding 必须替换成实际原记录。

```json
{
  "schema_version": "1.0.0",
  "repair_contract_id": "repair-contract-withdraw-isolation",
  "case_id": "investigation-case-withdraw-isolation",
  "revision": 1,
  "status": "draft",
  "source_finding_ids": ["finding-withdraw-reverts-contact"],
  "root_cause_statement": "撤回服务用提交时整行快照覆盖已生效主体，抹掉期间独立批准的电话修订",
  "violated_invariant": "撤回只改变本次申请及其草稿状态，保留其他合法修订和已生效资料的版本关系",
  "causal_model_ref": "case://investigation-case-withdraw-isolation/causal-model",
  "architecture_intent": "沿现有申请与已生效主体边界进行有版本条件的局部更新，消费者继续读取已生效来源",
  "repair_units": [
    {
      "id": "repair-unit-withdraw-isolation",
      "description": "修正撤回的更新边界，并验证草稿重开和证书消费者没有旧快照回写",
      "scope": ["src/shareholder/withdraw.go", "src/certificate/source.go", "tests/withdraw_reopen_test.go"],
      "assertion_ids": ["symptom-1", "root-1", "gap-1"],
      "depends_on": [],
      "resource_locks": ["withdraw-example-fixture"]
    }
  ],
  "prospective_scope": ["src/shareholder/withdraw.go", "src/certificate/source.go", "tests/withdraw_reopen_test.go"],
  "forbidden_scope": ["其他申请或已生效主体的无条件快照恢复", "未经批准的需求或契约变更", "真实生产数据修补"],
  "compatibility_migration": "保留现有申请身份和撤回历史；不回退已生效主体版本，不做本范围外的数据迁移",
  "symptom_assertions": ["编辑 B、提交 S、独立生效 P2、撤回、重开 D 后，D 显示 B/P2，已生效主体及证书仍为 A/P2"],
  "root_invariant_assertions": ["撤回只以原版本条件更新 S/D，不以提交时整行快照覆盖当前已生效主体；并发合法修订保持有效"],
  "detection_gap_assertions": ["回归实际交错提交与独立批准；重新引入整行快照回写时，持久化及证书消费断言必须失败，而非只检查撤回接口 200"],
  "regression_surfaces": ["docs/design/prototypes/shareholder/cases.json#CASE-WITHDRAW-REOPEN", "docs/design/prototypes/certificate/cases.json#CASE-CERT-CURRENT"],
  "stop_escalation_conditions": ["locked 规则并未明确重开草稿与并发资料的合并语义", "真实原因不支持当前局部更新假设", "修复需要扩大批准范围或迁移生产资料", "关键消费者被 mock 或无法观察实际持久化版本"],
  "approved_by": null,
  "approved_at": null,
  "approval_hash": null
}
```

症状断言证明用户旅程恢复；根因断言约束修复方式和业务边界；漏检断言要求旧错误重现时测试能失败。三者共同约束修复，不能只补一个“撤回成功”的测试。重复撤回、过期版本拒绝和其他独立申请是否受影响，按实际规则与风险补到原场景/Claim，不因为这个示例就增加统一必填套件。

Main 在原 ReviewPlan 处置股东与已明确受影响证书 CASE：可并行的责任保持并行，冲突 fixture 按原资源锁排序；无关模块不强制加入。独立 Targeted 检查这份原 Contract，之后 fresh S7 再检查完整当轮覆盖。示例的 Schema 结构正确，也不能证明实际模块七文件闭合、根因成立或真实浏览器运行通过。
