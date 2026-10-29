# Simulation Sidecar Feedback Protocol (v1)

You are running a framework self-verification simulation. Your job is to move
fast through S0 → S11, NOT to build real product. Rules:

1. **Placeholder only.** S6 Builder output, S9 repairs, S2 rework: write the
   smallest placeholder that satisfies the gate (a paragraph, a fixture
   file), commit it, move on. Never implement real business logic.
2. **Speed first.** Do not investigate deeply, do not grep the framework
   source unless a gate blocks you for more than 3 attempts.
3. **Sidecar after every stage transition.** After each TR commit or stage
   exit, append one entry to `.claude/FLOW-LOG.md` in the project root:
   `## <UTC time> <from> → <to> (rev N)` + 3 lines: what passed, what was
   placeholder, any surprise. Keep each entry under 10 lines.
4. **Blocker sidecar immediately.** The moment any command fails twice with
   the same message: stop pushing that command. Write
   `.claude/BLOCKED-<STAGE>.md` with: (a) the VERBATIM gate/command output,
   (b) what you tried (commands + results, max 5), (c) what you think the
   harness should have done instead. Then continue with the next available
   step; do NOT loop on the same command.
5. **Questions welcome, loops forbidden.** If a questionnaire or gate is
   unclear, record the confusion in the flow log in one paragraph and take
   your best guess. Never retry the identical call more than 3 times.
6. **No hand-edits.** Never hand-edit loop-state.json, journal, packets,
   records or evidence. If the only way forward looks like a hand-edit,
   that IS the blocker — write the blocker sidecar and move on.

The operator reads only your sidecars, not your thinking. Make the sidecars
count.
