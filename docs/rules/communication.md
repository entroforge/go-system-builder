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

## Required Communication

- Identify the runtime ID/revision or state that justifies the next action.
- Link authoritative REQ, specification, assignment, activation, BUG, and evidence documents.
- State blockers without silently reducing scope or acceptance.
- Record baseline decisions before acting on them.
- Use schema-valid Agent message envelopes for phase changes.
- Treat completion reports as evidence requests, not authoritative state changes.

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
- asking a subagent to bypass runtime or Hook enforcement
