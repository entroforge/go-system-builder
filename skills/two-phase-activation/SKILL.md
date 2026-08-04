---
name: two-phase-activation
description: Use when spawning, reassigning, or reactivating any specialized Builder, Document Verifier, Delivery Verifier, QA, or E2E Tester Agent
category: methodology
version: 1.5.0
---
# Two-Phase Activation

## Authority
Only the main session approves understanding and runtime activation. Message schemas are embedded in the Harness; Hook policy lives in `docs/hook-policy.json`; the activation method is inlined below.

## Entry Conditions
- An Agent Definition exists under `.claude/agents/<role>.md`.
- A team manifest with a single-responsibility assignment exists and is validated.
- The document chain (TASK/BUG → contracts → REQ → design → rules) is readable and fingerprinted.

## Required Inputs
| Input | Path / field | Why |
|:---|:---|:---|
| Agent Definition | `.claude/agents/<role>.md` | identity, tools, max permissions |
| Assignment | team manifest `assignments[]` entry | responsibility, allowed paths, message template |
| Document chain | BUG (if repair) → TASK → contracts → REQ → design/UI → rules | readback reading order |
| Current fingerprints | runtime `documents[]` | detect drift before activation |
| Activation scope | computed from 6-way intersection | effective permissions |

## Message envelope (SYNC-003 §7)

Every message in the activation chain carries the same envelope shape:

```text
runtime_id, runtime_revision
workgroup_id, assignment_id, role, responsibility_id
spec_chain[]: {kind, path, version, sha256}
allowed_tools[], allowed_read_paths[], allowed_write_paths[], allowed_command_classes[]
expected_outputs[], done_when[]
previous_message_id, previous_message_sha256
output_contract (the canonical schema for this message type)
```

The chain is fixed:

```text
phase_one_request
  -> readback_response
  -> main_session_approval
  -> phase_two_activation
  -> progress | finding | completion_report
```

`previous_message_sha256` is required on every message after the
`phase_one_request`. Any message that breaks the chain (missing prior hash,
stale revision, drifted spec hash) is rejected without side effect.

A `finding` message is **not** a repair assignment. It enters the canonical
BUG lifecycle owned by `bug-resolution` (root-cause investigation -> main-
session BUG approval -> repair activation). Skipping that lifecycle by
treating a finding as a direct repair request is forbidden.

## Skill Delivery

The Agent Definition's `skills:` frontmatter declares default preloads for an
independent Claude Code subagent. Agent Team teammates do not receive those
preloads. The manifest and readback/activation envelope therefore remain the
authoritative assignment-specific Skill list in both modes. Before phase-two
work, the activated Agent must load every applicable named Skill through the
`Skill` tool and report how it applies; missing Skill access is a stop
condition, not a reason to improvise from a summary.

## CLI invocation discipline

A `loop-harness` subcommand returning a non-zero exit code does **not** mean
the subcommand is unsupported. The empty-args usage string (e.g. `runtime
requires <reconcile|fingerprint>`) is the only built-in "list of
subcommands" surface, and it can be stale or partial after a binary rebuild.

Before declaring a subcommand missing and routing around it (e.g. switching
to a `general-purpose` agent to skip the activation envelope), run
`loop-harness <verb> <subcommand> --help`. If `--help` prints the expected
flags, the subcommand is supported — treat the non-zero exit as a genuine
CLI error (bad flags, stale revision, missing agent record, bad message
path, etc.) and read the stderr message for the real cause. Only treat the
subcommand as absent when `--help` itself returns "unknown command" or
"requires <...>" without listing the requested subcommand.

## Procedure
1. Generate a `phase_one_request` envelope per assignment: the full envelope above (with `previous_message_*` empty for the first message), plus fingerprinted document paths in reading order, forbidden paths, required outputs, stop conditions.
2. Launch the Agent in phase one (read-only). The Agent reads the chain in order and returns a `readback_response`: `ready`, `conflict`, or `missing`. The response must include `previous_message_sha256` of the `phase_one_request` it answers.
3. Treat the open read-back as the current Driver action. The main session must not self-execute the delegated responsibility while this assignment is in phase one; if the work should return to the main session, revoke or reassign it first.
4. Compare the Agent's readback against the current document versions and fingerprints. Verify the Agent understood the responsibility, the Closing Contract, and the forbidden paths.
5. If `ready` and fingerprints match: request `understanding_approved` via `loop-harness runtime agent-event`, committing a runtime revision. The `main_session_approval` message carries the prior `readback_response` hash and the runtime revision of the approval.
6. Construct the `phase_two_activation` envelope: agent ID, approved readback hash (chained), allowed tools, allowed write paths, allowed command classes, activation expiry, the runtime revision it binds to, and the output contract for `completion_report`.
7. Request `activated` via `loop-harness runtime agent-event`. The Hook policy now permits writes within the activation scope.
8. Monitor the activated Agent's writes. The Hook evaluates scope on every PreToolUse; an out-of-scope action surfaces in the Recovery Packet's `missing[]` and the Quality Gate returns `not_ready` rather than denying the tool. The main session narrows, extends, or revokes the activation envelope, waits for the next Hook to re-evaluate, and confirms the scope is valid before continuing.
9. On Agent completion, collect the `completion_report` (chained to the `phase_two_activation` hash) and route to the consuming Skill (e.g. Verifier results to `clean-round-evaluation`).
10. If `conflict` or `missing`: request `understanding_rejected`, return the Agent to `reading`, and surface the conflict to the Orchestrator.

## Outputs
- Readback request and response envelopes (evidence of phase one).
- `understanding_approved` and `activated` runtime events (evidence of phase two).
- Activation envelope with the 6-way intersection scope.

## Exit Conditions
- The same Agent is `activated` with current fingerprints and a scope no wider than the assignment requires, **or**
- The Agent is `understanding_rejected` with a recorded conflict.

## Stop Conditions
Stop immediately and surface to the human if any of:
- The Agent's readback understanding is incomplete or contradicts the document chain.
- Fingerprints drifted between readback and activation (a document changed mid-activation).
- The requested scope exceeds the Agent Definition or team manifest (permission expansion).
- Activation approval is missing or stale.

## Non-Goals
- Do not self-activate or auto-approve understanding — the main session must explicitly approve.
- Do not treat a pending read-back as permission for the main session to do the delegated work.
- Do not embed specification bodies into the prompt — the Agent reads the real documents.
- Do not treat a short prompt or chat summary as activation authority.

## Inlined Methodology

Loop Engineering uses one stable Agent Definition plus phase-one read-back prompt plus main-session approval plus phase-two activation prompt plus runtime-aware PreToolUse enforcement. The same Agent remains alive across both conversations so its document reading and submitted understanding stay in one context. Phase one is not enforced by trusting the prompt. The Agent Definition declares a static maximum tool set; before phase-two activation the Agent should not write target outputs — the Quality Gate and Transition Guard will return `not_ready` with a `missing[]` list when evidence is insufficient, but ordinary tool calls are not blocked by the Hook in this state. The Main session keeps the Agent in phase one (no write side effects) by reading the Recovery Packet and refusing to commit `understanding_approved` / `activated` until the read-back and fingerprints are valid. Dynamic scope is the intersection of: Agent Definition maximum AND team-manifest responsibility AND task allowed paths AND activation allowed paths AND current runtime lifecycle AND Hook policy. Anything outside the intersection is out of scope and must be corrected before the assignment can be considered valid. Agent Definition must not contain: copied REQ, contract, TASK or BUG content; a complete Methodology Skill; project runtime facts; a universal checklist for every technical domain; permission to write `.claude/loop-state.json`; permission to lock requirements, contracts or tasks; permission to squash merge or formally release. Read order: `TASK -> CONTRACTS -> REQ -> UI DESIGN PACKAGE/DESIGN -> RULES` for normal assignments; `BUG -> TASK -> CONTRACTS -> REQ -> UI DESIGN PACKAGE/DESIGN -> RULES` for repair assignments. Structured read-back response: `ready`, `conflict`, `missing_input`, or `out_of_scope`. Phase-two activation references approved read-back message ID and hash, approval evidence ID, current runtime revision, activation ID and expiration condition, allowed tools, allowed read and write paths, allowed command classes, expected output paths, checkpoints and stop conditions, and task/BUG and document fingerprints. Activation scope cannot exceed the prospective scope declared in phase one or the Agent Definition maximum. Phase-one allowed operations: file listing, search and read; read-only source inspection; explicitly classified non-mutating commands; messages and structured read-back submission. Phase-one write, branch, package-install, formatter/generator, task/team/runtime/evidence mutation, or release-shaped attempts are rejected with recovery instructions unless they hit a human-only hard block. `git push <remote> master|main` and `gh pr merge` / `gh release create` are not blocked by Hook Policy; they are constrained by the release-ready Gateway and human approval, so the absence of a Hook denial is not authorization to run them. Completion report: status (`completed`, `blocked`, `failed`), concise result, changed paths or reviewed paths for read-only roles, commands/checks run and results, created evidence and report references, discovered findings and BUG draft references, remaining risks and assumptions, scope deviations (must be empty for `completed`), requested lifecycle event.
