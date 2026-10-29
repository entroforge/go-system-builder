---
name: requirement-funnel
description: Use when a requirement is being drafted or amended in S0 and the expected outcome needs to become a human-locked REQ baseline
category: methodology
version: 0.2.1
---
# Requirement Funnel

## Authority
Stage contract: `docs/control/agent-protocol.md` #s0 (primary_skill). Structure and field definitions: `docs/requirements/REQ-template.md` (§A→§B→§C funnel). This skill carries process only — how to think, converge, and hand up.

## Entry Conditions
- S0 starts (new REQ), or an amendment reopens a locked REQ (new generation) — both run the same funnel.

## Required Inputs
| Input | Path / field | Why |
|:--|:--|:--|
| Expected outcome | 人的自然语言意图与已明确决定；原始材料可供追溯，REQ 共识使用消歧后的整理稿 | the funnel's raw material |
| Project facts | `docs/project-map.md` (§1 facts + §3 `design investment` row) | grounding for §B constraints and F0 basis |
| Design Foundation | `docs/design/DESIGN.md` §0 Next-agent card + current `SUR-*` diff (when `core`/`extended`) or module `derivation/REQ-{id}.md` with `Foundation: local` (when `local`) | inherit published language; do not invent brand in the REQ |
| Module truth packages | `docs/design/prototypes/<module>/` (when touched) | UI impact and regression scope |
| Design investment | `project-map.md` §3 `design investment` (`local / core / extended / N/A`) | F0 outcome that decides whether a Foundation is required at all |

## Procedure
0. **真实人意与锁定**：专业判断由Main完成，只有真实意图不清或必要价值选择才提请人决定。复用已经明确的决定，不把§A→§B→§C方法变成逐层强制重复确认。把实际决定如实记入原REQ；无决定留空等待，不代批。
0.5 **F0 — investment + Foundation check**: if the expected outcome may change screens, visual
    behavior, copy, motion, or user-visible states:
    (a) first decide investment tier `local / core / extended / N/A` from `project-map.md` §3 and
    `docs/rules/design-foundation.md` §4 (reuse / handoff / Surface count / shared token-component / risk);
    `local` → §C will record `Foundation reference: local` + `Surface: local` + `Design posture: local`
    and a module `derivation/REQ-{id}.md` (`Foundation: local`), no published Foundation required;
    (b) `core`/`extended` with `DESIGN.md` `missing`/`draft`/`in-review`/`provisional`/`superseded`/stale/uncovered surface,
    missing §0, or `published` with confirmation still PENDING
    → stop the funnel, load `skills/design-foundation`, finish F0–F6 (thin vs full per that Skill),
    obtain human publish confirmation (dates, not PENDING), then resume §A; only `published` with dates recorded is a covering lock — `draft`/`in-review`/`provisional`/`superseded` must not be treated as (c);
    (c) `published` (dates recorded) and covering the Surface → continue; later §C only records
    versioned references (`DESIGN.md@version`, `SUR-*@version`). Do not substitute a few style
    sentences in §B for a project-level Foundation and do not promote a local primitive/hex to
    project scope. `provisional` is never (c).
    Runtime does not yet hard-block this; the default agent path must still stop.
1. **工作方式**：人提供真实意图，Main 承担调查、设计、推荐与整合。能够从事实和既有决定解决的问题自己处理；方向、范围、优先级或风险容忍度等真实未决选择，形成完整提案交人决定，不能为追求自主而代批。
2. **§A 理念**：消除口语歧义，核实隐含前提，形成 A1 的结构化理解；仅在真实意图仍不清楚时请人澄清，复用已经明确的确认。继续形成 A2～A4、相关角色与术语，使后续取舍有共同依据。需要人的决定时上交完整 §A 提案，不为完成本层重复索取确认。
3. **§B 方向与约束**：调查确实可行且会改变取舍的方向，收敛到有依据的推荐，说明真实被否决方案的代价。方向已由人明确且事实支持时直接承接，不制造陪衬方案。检验每条“必须”的来源及破坏代价，无依据的偏好不能伪装成硬约束；记录假设与风险。
4. **§C 具体需求**：将方向分解为需求点，并追溯到 §A；无法说明目标来源的功能应检查是否扩张范围。展开成功、拒绝与异常流程、可判定验收、非功能、迁移清单与三值界面影响。真实业务选择交人，具体架构和实现问题留给相应下游。
5. **层内收敛**：展开可能解释或路线 → 依上层结论与事实剪枝 → 自审 → 处理必要决定并记录依据。依赖真实未决选择前先取得决定，已明确的同一决定不逐层重复批准。
6. **提案纪律**：上交时说明推荐、理由、真实选择的代价及需要人裁定的事项。先完成自己能够承担的调查与专业推导，不能把资料搜索、模板填写或一串裸问题推给人。“比较两三个方向”“集中少量拍板点”是组织建议，不是数量门；不得为凑数造选项，也不能为满足配额删掉独立价值问题或反对意见。问题较多时按真实依赖组织，让每次决定消除具体不确定性。
7. **决定点表达**：说明“推荐什么、依据是什么、其他真实选择的代价、仍需人决定什么”。不存在真实替代方案时不强造一个；尚无充分推荐依据时如实说明调查结果和分歧，不用模板措辞伪装确定性。
8. **Self-review** (light before each hand-up; full before proposing lock) — attack your own reasoning in three roles: implementer ("which acceptance criterion can't I build?"), user ("which sentence would I misread?"), maintainer ("where do §A4 exclusions and §B constraints collide?"). Findings: fix, or downgrade to a decision-point annotation. Review the reasoning chain (A→B→C traceable), not the format. The full-review conclusion is one paragraph recorded before §E.
9. **Escalate with a proposal** (never escalate a bare question): contradictions between approved layers, unresolvable intent, or value tradeoffs without a judgment basis — "I lean toward A because …; if your intent is B, then X/Y/Z must be redone."

## Outputs
- REQ §A／§B／§C 中完整且连贯的理念、方向与需求推导；实际必要决定如实记录，不以层数或数量制造审批。
- §D entries for open clarifications (each marked blocking / non-blocking).
- Before lock: the self-review conclusion paragraph (§E) accompanying the lock proposal.

## Exit Conditions
- Human confirms §A-§C match their intent and gives the lock gesture: an explicit "锁定" (or equivalent) in the conversation.
- **落锁手势（谁写 locked）**：你依据该拍板把 REQ 顶部 `状态：` 置为 `locked`，并在 §E 锁定记录写入操作人与时间——决策权在人（拍板构成授权），文件编辑由你执行（你是唯一的文件写手）。若人已同时明确锁定并启动，S1直接承接该真实决定，不再二次确认。只有锁定未授权启动，原binding申请的授权槽留空等待。
- S1系统给准确当前requirement-binding.md，原四问题正常保存自动原TR001/Writer交S2；人名仅记录，不认证或索工具权限确认。req bind仅兼容恢复，不作正常履职步骤。

## Stop Conditions
- Waiting for a human 拍板 is not a stop condition — state what needs confirming and hold; only the three below are stops.
- Intent cannot be understood even after restatement attempts → stop, escalate with candidate interpretations.
- Approved layers contradict each other → stop, escalate for arbitration.
- A value tradeoff has no judgment basis available to the agent → stop, hand the decision point up.

## Non-Goals
- No architecture design (§B ends at direction and constraints; S2 owns architecture).
- No rewrite of Project Design Foundation (S0 records the version reference; Kernel/Grammar live in `docs/design/DESIGN.md`).
- No value decisions on the human's behalf (priority, scope, direction sign-off).
- REQ amendment after lock follows the single [Approved requirement amendment entry](../../loop-harness.md#approved-requirement-amendment) in the generated manual; reuse the actual approved correction and returned current questionnaire. Hook reports a genuinely missing decision without intercepting tools.
- No restating template field definitions — the template is self-describing.
