# Documentation-Driven Communication Rule

---
rule_id: R-COMM-01
category: Process
status: locked
owner: Project Manager / Architect
scope: all agents
---

## Rule

Chat communicates status, questions, decisions awaiting recording, and links.
Versioned documents and structured evidence are the source of truth.

When reporting progress on a Loop responsibility attempt, use `pending`,
`active`, `blocked`, `done`, or `cancelled` as defined by the shared
[state rule](state-machine.md#21-loop-responsibility-status). Report the stage
cursor/gate, professional outcome, current receipt and platform session as
separate facts. Exact existing payload values remain unchanged; do not ask an
Agent to hand-author a generic status.

## Required Communication

- Identify the runtime ID and the current state/`next` action that justifies
  the next step. A committed Runtime revision may be cited as optional audit
  metadata, but it is never a prerequisite for normal agent communication.
- Link authoritative REQ, specification, assignment, activation, BUG, and evidence documents.
- State blockers without silently reducing scope or acceptance.
- Record baseline decisions before acting on them.
- Use schema-valid Agent message envelopes for phase changes.
- Treat completion reports as evidence requests, not authoritative state changes; the original receiver projects responsibility status.

## Subagent Boundary

Subagents receive work only through a fingerprinted assignment and phase-one
request. They do not negotiate requirements with the user, infer omitted scope
from chat, self-activate, or broaden their assignment.

Read-back, activation, completion, team planning, BUG handling, and clean-round
procedures are owned by:

- `.claude/skills/agent-dispatch/SKILL.md`
- `.claude/skills/team-planning/SKILL.md`
- `.claude/skills/bug-resolution/SKILL.md`
- `.claude/skills/clean-round-evaluation/SKILL.md`

## Escalation

| Situation | Record |
|:---|:---|
| requirement ambiguity/change | REQ question or change request; pause when locked |
| specification conflict | structured phase-one conflict |
| blocking delivery/quality finding | canonical `BUG-*` |
| changed impact or stale PASS | impact-analysis evidence |
| release audit blocker | release architecture audit finding |
| final release action | explicit human release approval |

## Forbidden

- chat-only baseline decisions
- prompt summaries replacing document reading
- assuming idle means complete
- silently treating missing evidence as N/A
- calling release architecture audit “human approval”
- asking a subagent to claim a runtime transition or stage approval without its original evidence
