# Vibe Coding Documentation System

Reusable documentation scaffolding for AI/Agent-assisted software delivery.

## Entry Points

1. `AGENTS-template.md`: project-local `AGENTS.md` source — Layer 1 entry (Main-session Driver). Step 0 checks Project Design Foundation before the first `UI impact=changed` REQ.
2. `loop-template.md`: project-local `.claude/loop.md` source — Layer 2 Wake-up Prompt.
3. `settings.json`: Hook registration — Layer 3 guardrail enforcement.
4. `docs/agent-protocol.md`: authoritative Main Spine (S0-S11 stage contracts).
5. `docs/README.md`: setup and usage order.
6. `docs/project-map-template.md`: template for project facts, stage, PM todo, and gates.
7. `docs/requirements/REQ-template.md`: requirement template.
8. `docs/rules/design-foundation.md` and `skills/design-foundation/SKILL.md`: F0–F6 before locking UI-changing REQs.
9. `prelude.md`: main-session onboarding.

## Copy Into A New Project

- Copy `AGENTS-template.md` to project root as `AGENTS.md`.
- Copy `docs/project-map-template.md` to `docs/project-map.md`.
- Follow `docs/README.md`.

## Repository Boundary

This repository stores reusable templates, rules, and reference material only.

Project-instance files such as `AGENTS.md`, `docs/project-map.md`, `docs/requirements/REQ-*.md`, requirement indexes, and reports are local to each target project and must not be committed back to this template repository.

## Core Gates

- No bound REQ, no requirement work (the machine-enforced gate; the PM todo block was removed from the REQ template).
- No stage check, no next stage.
- No locked requirement, no design lock, contract lock, task split, Builder dispatch, or feature branch.
- No UI Design Package Gate, no FE/BE/SYNC contract lock for UI-impacting requirements.
- No `loop-harness req bind` on a human-locked REQ, no Engineering Loop and no contract lock, formal task split, Agent Team, Builder dispatch, or implementation branch.
- Engineering Loop binding and Claude `/loop` are independent lifetimes. `/loop` only delivers the Layer 2 Wake-up Prompt; `req bind` supplies engineering authorization, so no separate `/goal` is required.
- No locked contract, no Builder execution.
- No approved task read-back, no sub agent activation.
- No Document Verifier approval, no Builder activation.
- No Delivery Verifier, QA, and E2E Browser evidence, no loop completion.
- No clean full-depth Delivery + QA + E2E review round, no release audit.
- Delivery Verifier / QA / E2E Browser findings enter S8 root-cause investigation before becoming accepted canonical BUGs for Builder repair.
- No human release approval, no squash merge to `master/main`.
