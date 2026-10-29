# Claude Code compatibility and acceptance

Reference checked 2026-10-25: local Claude Code 2.1.299. The preserved S9
platform run is a compatibility failure, not full acceptance. Record the actual
CLI version alongside the release manifest and each platform test result.

Official references: https://code.claude.com/docs/en/agent-teams,
https://code.claude.com/docs/en/hooks, https://code.claude.com/docs/en/sub-agents,
https://code.claude.com/docs/en/permissions and
https://code.claude.com/docs/en/scheduled-tasks.

- Since 2.1.178 teams are session-managed. TeamCreate/TeamDelete were removed;
  Agent.team_name is ignored. Do not require either for dispatch. Team support
  is experimental and must be enabled; prefer a single role-bearing subagent
  when peer coordination is unnecessary. Runtime identity remains assignment
  plus registered owner, not a deprecated platform team name.
- Native `Agent`/`Task` creates a subagent. Continue an ended subagent with
  `SendMessage(to=<actual original agent ID>, message=<original task direction>)`.
  The [official resume contract](https://code.claude.com/docs/en/sub-agents#resume-subagents)
  resumes that actor in the background without a new Agent invocation. Do not
  rely on `Agent.resume`: in the preserved 2.1.299 run the model requested it,
  the actual Hook input omitted it and a new actor/worktree was created.
  No framework alias can turn that replacement into the original actor.
- Immediate SendMessage success establishes sending only. Follow the actual
  completion notification and inspect the original consumer receipt/current
  delivery. S7/S9 observe ready original answers at PreToolUse, with successful
  PostToolUse and Stop/SubagentStop as fallback through the same consumer; S6
  and S8 retain their original SubagentStop reception. Main still owns
  integration/checks. SDK `task_notification` frames are actual platform
  evidence, not a new registered Hook. Main's existing
  read-only progress supplies current responsibility and reception facts; real
  platform acceptance must demonstrate the notification and accurate handoff
  reach Main, without assuming a second Agent PostToolUse after SendMessage.
- Platform continuation tests compare model request, actual Hook input/response,
  original actor/worktree Start and Stop, and completed SDK notification for the
  same run/tool call. S9 engineering handoff acceptance also reads the recorder's
  actual Hook `stdout`, parses `hookSpecificOutput.additionalContext` on a Main
  PreToolUse after that completion, and binds the exact owner, assignment, original
  input, accepted partial facts/concrete failure and consumer command to the next
  actual Main tool request and its Hook pair. A user/systemMessage summary, SDK
  notification alone, stale event or unexecuted instruction cannot pass. This S9
  check is separate from the shared S6/S9 execution evidence check.
  A model parameter or successful send alone cannot pass.
  Preserve negative cases for discarded resume, replacement actor, wrong target,
  missing Stop and stopped/mismatched notifications. The current official docs
  also note a recent-work recall issue before 2.1.304 when continuing immediately
  after completion; same ID alone does not prove the 2.1.299 conversation retained
  its latest work. Verify existing commit and Result remain intact.
- For a diagnosed S9 reception failure, Main uses `runtime stage-answer consume`
  only with the exact original native flat, saved typed or Targeted input supplied
  by current feedback. It reuses the original consumer without another Worker
  return. Accepted Plan/candidate/Targeted facts retain their original successor;
  an accepted Targeted with unfinished Handoff uses `runtime repair handoff complete`.
  Normal SubagentStop adds no manual procedure. Missing exact inputs stay unknown;
  this is not a general S5/S6/S7/S8 native recovery promise.
- Role definitions live in .claude/agents with valid YAML frontmatter. Never
  substitute a role invisibly because it failed to load. Platform permission
  mode and plan approval do not grant framework business authority.
- Stop/SubagentStop honor stop_hook_active and event-specific continuation
  semantics. A reminder budget prevents endless reminders, not missing work
  from blocking completion. Matching Hooks may run concurrently: Writer CAS
  and immutable artifact identity remain required.
- Async Hooks cannot veto completed actions. Long verification may execute in
  the background, but verified/complete gates require its actual tree-bound
  result. Hook timeout or process start is not PASS. SessionStart context is
  guidance, not a platform write-denial mechanism.
- /loop is a session wake-up mechanism. On recovery, check process/artifact
  state; background Bash is not guaranteed to survive session resumption.
- Do not enable bypassPermissions to fix a workflow. Existing permission and
  human release boundaries remain intact.

`tools/claude-hook-smoke.sh` tests command/JSON boundaries and detects the CLI;
it does not prove actual agent continuation. Before deployment, a disposable
small project must exercise role loading, plan checkpoint, independent review,
Stop/Idle continuation, session recovery and S9 -> fresh S7 -> S11. Preserve
actual transcripts, exits and tree identities. Never seed PASS records to call
this a real platform test. A missing credential/platform capability is reported
as NOT EXECUTED, not success.
