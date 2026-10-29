# S6 native Builder delivery

Main selects an eligible reviewed TASK and role:

```sh
loop-harness s6 prepare --root <Main> --task <TASK> --role backend-builder
```

Carry the returned exact task brief into a native Claude Code Agent call. Use native worktree isolation for writes, with worktree.baseRef=head on the committed bound development branch. No Team ID, platform Agent ID or separate workspace launcher is needed. Start provides engineering guidance. The Builder reads actual sources, saves its professional plan, implements, runs TASK self-checks, commits and saves its professional Result. Return the exact brief/result reference. Stop receives the actual artifacts through the original domain consumer; Main explicitly integrates and runs required Main checks afterwards.

Read TASK, linked specifications and product sources at the brief's worktree-relative paths in your actual worktree, starting from its fixed source commit. The exact brief and installed method references may point to Main's control root; that does not make Main's changing product files your assigned source.

The plan and Result drafts contain professional fields only and live under the actual Worker worktree’s private `.claude/assignments/native/` directory. Commit TASK products; keep these drafts out of Git. Save does not mean activation or acceptance; there is no manual begin/task-complete step. For a real blocker before or after forming the Plan, save the brief's minimal blocked Result at the same Result path: explain progress and unfinished work in `summary`, give the real reasons in `risks_blockers`, and set `closing_assessment.disposition` to `blocked`. Preserve any existing Plan and actual evidence; missing checks need no invented evidence or placeholder plan. Return the exact brief and absolute Result references. Main owns recovery; this handoff does not create accepted completion or integration.

For normal delivery, fill the current professional structures supplied in the brief.
Plan author fields are `objective`, `planned_paths`, `assertion_checks` and
`risks_blockers`; each assertion has `assertion`, `oracle` and zero-based
`done_when_indexes`. Registered dependencies are supplied by the receiver, not
filled in either private JSON file. Each Result `assertion_evidence` item names its zero-based `plan_assertion_index` in your actual Plan and supplies nonempty `evidence_refs`. The receiver carries the original assertion/oracle into the canonical Result; it does not change the criterion or infer success. Judge each Done when condition with `done_when_index`, `judgment` and `rationale`; the receiver derives omitted `plan_assertion_indexes` from the Plan's existing mapping. Historical full assertion/oracle rows and explicit closing mappings remain accepted compatibility forms and are checked strictly against the original Plan; they are not current required author fields. Do not mix the compact index and full text in the same evidence row.

Evidence references must be exact existing worktree-relative file paths within the TASK write scope or listed Read paths, such as `src/pkg/input.go`; omit line numbers, descriptions and command strings. Put command results and observations in `summary` or closing rationale. Stop reports specific professional field errors through the original S6 Schema and retains the original evidence scope checks. Completed delivery still needs evidence and a satisfied judgment for every original assertion and Done when condition; a blocked handoff preserves the gaps instead of satisfying them.

The JSON workgroup-manifest example in this directory is an old explicit s6-v1.2 compatibility fixture. Existing frozen workspace assignments retain their original prepare/launch/resume and recovery commands. Do not copy that manifest or relaunch a running old assignment for the native path. Actual identity, checks and evidence are never inferred from the example's placeholders.
