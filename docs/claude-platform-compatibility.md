# Claude Code compatibility and acceptance

Reference checked 2026-09-19: Claude Code 2.1.276. This is the target for
acceptance, not a claim that every future version is supported. Record the
actual CLI version alongside the release manifest and platform test results.

Official references: https://code.claude.com/docs/en/agent-teams,
https://code.claude.com/docs/en/hooks, https://code.claude.com/docs/en/sub-agents,
https://code.claude.com/docs/en/permissions and
https://code.claude.com/docs/en/scheduled-tasks.

- Since 2.1.178 teams are session-managed. TeamCreate/TeamDelete were removed;
  Agent.team_name is ignored. Do not require either for dispatch. Team support
  is experimental and must be enabled; prefer a single role-bearing subagent
  when peer coordination is unnecessary. Runtime identity remains assignment
  plus registered owner, not a deprecated platform team name.
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
