# HARNESS-BUG-003 — Intermediate guidance and non-English text leaked into the Agent Protocol

> Severity: P2 (authoritative documentation integrity and operator guidance drift)
> Discovered: 2026-09-01
> Component: `docs/agent-protocol.md`
> Status: **resolved in the working tree; CI guard added; large-section review remains manual**

## 1. Symptom

The authoritative Agent Protocol contained 111 lines with Chinese characters. The
largest contiguous addition was a roughly 109-line S7-S9 control-plane audit and
gap-fix section. It mixed implementation audit notes, roadmap decisions, and
operational detail into the Main Spine, which is read as turn-by-turn routing
guidance.

The protocol is expected to be an English-language constitution and route table.
Detailed execution rules belong in the relevant schema, Assignment, CLI, error,
gate, L3 stage blueprint, or L4 mechanism document.

## 2. Evidence

- `git show HEAD^:docs/agent-protocol.md` is 556 lines and contains no Chinese
  characters.
- The current file contained 111 Chinese-bearing lines, and `git blame` attributes
  the contaminated block and the other mixed-language fragments to release commit
  `98006dbe54edb9b78d897912fce9aa2cb2894b10` (`release: v0.1.3 (#3)`).
- `blueprint/L3-S6-build.md`, `blueprint/L3-S7-verification-round.md`,
  `blueprint/L3-S8-finding-investigation.md`, and
  `blueprint/L4-agent-dispatch-governance.md` all define `agent-protocol.md` as a
  compact entry/index document and place detailed execution guidance on the
  mandatory paths.
- The L3-README says cross-stage mechanisms are L4-owned and that each L3 only
  declares how it consumes them. L3-S6 §10 and its P3 plan say to reduce the
  protocol to invariants and a route table; L3-S7 §8 says to keep only the S7
  entry, three lenses, four exits, and links; L3-S8 §13.5 says to keep only an
  entry/exit index; L3-S9 §10 says the long protocol cannot carry execution
  correctness.
- The same S7-S9 guidance-chain commit that added the map describes protocol
  slimming as a required outcome. This is an internal cross-document scope
  contradiction, not evidence that the map was a mandatory protocol section.
- The `s7s9-control-plane-map` anchor was referenced by `loop-harness.md`,
  `internal/transition/manual.go`, and several blueprints. Those consumers had to
  be redirected before removing the section to avoid broken references.

## 3. Root cause

An intermediate audit/guidance summary was promoted into the canonical Main
Spine without applying the already-recorded layer boundary. The map is useful as
a human navigation summary, but its authority chain, revision glossary,
instrumentation matrix, implementation audit, and gap backlog duplicate L4/L3
content. The repository also has no CI lint that rejects Han characters or checks
that the protocol remains within its constitution/route-table role.

This was a documentation-governance failure, not a runtime state-machine defect.

## 4. Processing performed

The working copy removes the long map and its anchor from the runbook. Its
operational references now point directly to the owning L3/L4 documents and the
existing S7/S8/S9 runbook sections. The mixed-language fragments elsewhere in
the protocol were translated to English. Stage routing semantics, transition
identifiers, command names, and compatibility notes were not removed.

Verification target:

```text
rg -n -P '[\x{4e00}-\x{9fff}]' docs/agent-protocol.md
```

The command now returns no matches.

## 5. Preventive guard and remaining review

The working tree now has a lightweight CI check that:

1. rejects Han characters in `docs/agent-protocol.md`;
2. rejects the removed S7-S9 map anchor from returning.

Large audit/implementation sections still require human review before release;
there is no reliable size-only lint for deciding whether a section is valid
runbook content.

The protocol should retain only invariants, the simple state graph, tool entry
points, human boundaries/escalation rules, and authority-location links. New
execution detail should be added to its owning document and linked from the
protocol.

## 6. Related investigation

`docs/reports/bugs/HARNESS-DEFECT-001.md` was investigated in the same pass. The
current checked-in source does not reproduce its claimed unsatisfiable
`human_decision_scope` contract; its report now records the source-level finding
and the corrected registration sequence.
