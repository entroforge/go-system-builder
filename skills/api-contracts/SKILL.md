---
name: api-contracts
description: Use when APIs, events, shared schemas, integration payloads, or compatibility promises change
category: best-practice
version: 1.1.1
---
# API Contracts
## Authority
The locked contract owns interface semantics. The applicable activation and prompt method is summarized below; stage routing lives in `docs/control/agent-protocol.md`, and current assignment authority comes from the team manifest and Agent message envelopes.
## Applicability
Apply to `api` risk and shared component boundaries.
## Required Inputs
Read FE/BE/SYNC contracts, schemas, consumers, version policy, and error model.
## Quality Criteria
Check field meaning, validation, compatibility, defaults, error codes, idempotency, pagination, ordering, retries, and side effects.
## Outputs
Contract-aligned implementation or compatibility evidence.
## N/A Criteria
N/A when no machine-consumed boundary changes.
## Stop Conditions
Stop on consumer disagreement, unspecified compatibility, or ambiguous side effects.
## Non-Goals
Do not silently extend locked contracts.

## Inlined Methodology

Loop Engineering uses one stable Agent Definition plus phase-one read-back prompt plus main-session approval plus phase-two activation prompt plus runtime-aware PreToolUse enforcement. The same Agent remains alive across both conversations so its document reading and submitted understanding stay in one context. Phase one is not enforced by trusting the prompt. The Agent Definition declares a static maximum tool set; before phase-two activation the Agent should not write target outputs — the Quality Gate and Transition Guard will return `not_ready` with a `missing[]` list when evidence is insufficient, but `not_ready` itself does not block authorized file listing, search, read, or other non-mutating inspection. Phase-one write, branch, package-install, formatter/generator, task/team/runtime/evidence mutation, or release-shaped attempts are independently rejected with recovery instructions unless they hit a human-only hard block. Read order: `TASK -> CONTRACTS -> REQ -> UI DESIGN PACKAGE/DESIGN -> RULES` for normal assignments; `BUG -> TASK -> CONTRACTS -> REQ -> UI DESIGN PACKAGE/DESIGN -> RULES` for repair assignments. References are ordered and fingerprinted. Phase-one expected operations are file listing, search and read; read-only source inspection; explicitly classified non-mutating commands; messages and structured read-back submission. `git push <remote> master|main`, `gh pr merge`, and `gh release create` that match `docs/control/protected-commands.json` are denied by PreToolUse Hook Policy; the release-ready Gateway and human approval still record the human decision, and the Harness never performs merge, publication, deployment, or formal release side effects. Structured read-back response: `ready`, `conflict`, `missing_input`, or `out_of_scope`. A `ready` response must include objective and user-visible value, single assigned responsibility, REQ/contract/TASK/optional BUG clauses covered, planned files or review surfaces, allowed and forbidden actions, dependencies and integration boundaries, expected outputs and evidence, Closing Contract or review conclusion criteria, selected Skills and how they apply, assumptions, risks and unresolved questions, and document fingerprints actually read. Best-practice selection maps the `api` risk tag to the `api-contracts` Skill. Prompt minimality: prompt content must include only instance identity and correlation metadata, document and Skill references, role responsibility, allowed/prospective/forbidden scope, output and evidence destinations, current phase and stop conditions; it must not include copied specification sections, generic engineering checklists already in Skills, role doctrine already in the Agent Definition, Loop procedure already in Methodology Skills, guessed missing context, or hidden instructions that contradict the envelope.

## Shared source and reading order

Apply [shared-model contract rules in the factory](../../docs/rules/shared-model-contracts.md) (after installation: [project rule](../../docs/rules/shared-model-contracts.md)).
Converge the shared model and SYNC behavior before finalizing FE/BE contracts.
Use native definitions and exact references; do not copy field tables. Check
actual consumer/provider/Mock behavior, including structurally valid business
rejections. Read only the operation and model slices named by the current TASK.
