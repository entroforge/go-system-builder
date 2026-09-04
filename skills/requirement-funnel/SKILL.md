---
name: requirement-funnel
description: Use when a requirement is being drafted or amended in S0 and the expected outcome needs to become a human-locked REQ baseline
category: methodology
version: 0.2.1
---
# Requirement Funnel

## Authority
Stage contract: `docs/agent-protocol.md` #s0 (primary_skill). Structure and field definitions: `docs/requirements/REQ-template.md` (§A→§B→§C funnel). This skill carries process only — how to think, converge, and hand up.

## Entry Conditions
- S0 starts (new REQ), or an amendment reopens a locked REQ (new generation) — both run the same funnel.

## Required Inputs
| Input | Path / field | Why |
|:--|:--|:--|
| Expected outcome | the human's natural-language statement (conversation, never recorded verbatim) | the funnel's raw material |
| Project facts | `docs/project-map.md` (§1 facts + §3 `design investment` row) | grounding for §B constraints and F0 basis |
| Design Foundation | `docs/design/DESIGN.md` §0 Next-agent card + current `SUR-*` diff (when `core`/`extended`) or module `derivation/REQ-{id}.md` with `Foundation: local` (when `local`) | inherit published language; do not invent brand in the REQ |
| Module truth packages | `docs/design/prototypes/<module>/` (when touched) | UI impact and regression scope |
| Design investment | `project-map.md` §3 `design investment` (`local / core / extended / N/A`) | F0 outcome that decides whether a Foundation is required at all |

## Procedure
0. **拍板手势（全流程通则）**：一层"被批准"的唯一形式 = 人类在对话中对你的提案给出明确肯定（"可以 / 按建议办 / 选方案 A"）。收到拍板后，当场把一行记录写进 §E 逐层拍板记录表（层 / 日期 / 拍板人 / 方式），然后才进入下一层——对话不是权威，未落盘的拍板等于没发生。等待拍板是正常姿态：给出提案后明确说"需要你确认 §X"，然后停下，不要自己继续。
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
1. **Mode**: the human states the expected outcome; you own the design end-to-end; the human only approves (拍板). You are not an interviewer — keep questions to yourself, hand proposals up.
2. **§A 理念**: filter the expected outcome — strip ambiguous colloquial wording, surface implicit premises — into a structured restatement (A1) and have the human confirm it says what they meant. Recording raw quotes records ambiguity. Then draft A2-A4, stakeholders, glossary. Hand up: complete §A proposal, ≤3 decision points.
3. **§B 方向与约束**: design 2-3 viable directions, prune to one recommendation with a one-line rejection reason per discarded direction; challenge every "must" constraint ("what does breaking it cost?" — no answer means preference, demote it); log assumptions and risks. Hand up: complete §B proposal, ≤3 decision points.
4. **§C 具体需求**: decompose into requirement points, each tracing back to a §A item (untraceable = scope creep, challenge it on the spot); flows with rejection paths; testable acceptance criteria; NFRs; migration inventory; UI impact table (three-valued). Hand up: complete §C proposal, ≤3 decision points.
5. **Per-layer loop**: diverge exhaustively → prune with the layer above → self-review → hand up. Never design the next layer before the current one is approved.
6. **Proposal discipline** (the only acceptable hand-up shape): a complete proposal — recommendation, rationale, rejected alternative with its cost. Open questions are forbidden: decide what you can, record the rationale in the matching field; only non-derivable value judgments (direction / scope boundary / priority / risk tolerance) become decision points, ≤3 per layer. More than 3 means the design has not converged — go back and prune, do not keep asking. Dumping questions on the human feeds L1's named failure mode of addictive dependence on "having a human re-check each time".
7. **Decision-point format**: "Recommend X (rationale); alternative Y (cost); the one thing you must decide: <one sentence>."
8. **Self-review** (light before each hand-up; full before proposing lock) — attack your own reasoning in three roles: implementer ("which acceptance criterion can't I build?"), user ("which sentence would I misread?"), maintainer ("where do §A4 exclusions and §B constraints collide?"). Findings: fix, or downgrade to a decision-point annotation. Review the reasoning chain (A→B→C traceable), not the format. The full-review conclusion is one paragraph recorded before §E.
9. **Escalate with a proposal** (never escalate a bare question): contradictions between approved layers, unresolvable intent, or value tradeoffs without a judgment basis — "I lean toward A because …; if your intent is B, then X/Y/Z must be redone."

## Outputs
- Per layer: a complete proposal with ≤3 decision points, recorded into the REQ §A/§B/§C sections.
- §D entries for open clarifications (each marked blocking / non-blocking).
- Before lock: the self-review conclusion paragraph (§E) accompanying the lock proposal.

## Exit Conditions
- Human confirms §A-§C match their intent and gives the lock gesture: an explicit "锁定" (or equivalent) in the conversation.
- **落锁手势（谁写 locked）**：你依据该拍板把 REQ 顶部 `状态：` 置为 `locked`，并在 §E 锁定记录写入操作人与时间——决策权在人（拍板构成授权），文件编辑由你执行（你是唯一的文件写手）。`req bind --approved-by <同一人>` 是 S1 的二次确认，不重复也不替代此手势。
- bind itself is human-gated (see the lifecycle-verb whitelist in AGENTS.md), outside this skill.

## Stop Conditions
- Waiting for a human 拍板 is not a stop condition — state what needs confirming and hold; only the three below are stops.
- Intent cannot be understood even after restatement attempts → stop, escalate with candidate interpretations.
- Approved layers contradict each other → stop, escalate for arbitration.
- A value tradeoff has no judgment basis available to the agent → stop, hand the decision point up.

## Non-Goals
- No architecture design (§B ends at direction and constraints; S2 owns architecture).
- No rewrite of Project Design Foundation (S0 records the version reference; Kernel/Grammar live in `docs/design/DESIGN.md`).
- No value decisions on the human's behalf (priority, scope, direction sign-off).
- No bind execution or REQ mutation after lock (human-only; hook-enforced).
- No restating template field definitions — the template is self-describing.
