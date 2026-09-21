# Overall dispatch plan

S4 delivers one `docs/dev/tasks/index-REQ-<id>.md` plan for the current REQ, using
[index template](../dev/tasks/index-template.md). Every non-cancelled TASK appears
exactly once as an unchecked Markdown file link under `## W1`, `## W2`, etc.
The header declares `REQ`, `Dispatch policy: waves-v1`, a positive `Revision`,
and `Status: complete` before review. TASKs explicitly declare Source REQ refs.
New REQs and TASKs declare waves-v1; missing plans are not a legacy exemption.

Keep real artifact dependencies in TASK Dependencies, not in a second graph.
Share read-only schemas freely: independent consumers may develop in parallel.
List concrete repository-relative prospective write paths (comma separated; no globs) and any mutable external
resources in TASK Resources (`resource:<stable-name>`). Narrow overly broad scopes.
Same-wave write/resource overlap must be resolved before dispatch; use independent
ownership or a justified Resource order table (Before TASK, After TASK, Resource,
Reason). Invalid non-header resource rows are errors, not absent constraints.
Resource order and artifact dependencies must form one acyclic order.
The effective write scope includes both assignment write_paths and output_paths;
both must stay within the reviewed TASK scope. Registration, activation and
integration consume this same effective scope.

S5's existing two reviewers explicitly review membership, parallel boundaries,
missing dependencies, shared resources and unnecessary serial work. The plan is
an exact review subject, frozen alongside TASKs. Commit formal inputs on the
bound development branch; disk-only edits cannot replace the reviewed Git tree.

S6 reads plan → live checklist → next TASK → its ordered Document Manifest.
Use `loop-harness s6 status --capacity <total available concurrent slots>`;
`--json` gives the same projection. Outside the building state, status reports
the lifecycle state without readiness or a selected Builder batch. Capacity is the actual total, not remaining
slots. Without a capacity declaration the command shows readiness but does not
claim a dispatch batch. A completed predecessor releases its consumers without
waiting for unrelated tasks in the prior wave. Queued work is retained.

A reported result is not integration. The current result, checks and matching
assignment checkpoint must be verified after merging back into the root's bound
development branch. The TASK and assignment consume the same canonical Result, including
resubmissions. The checkpoint binds the result path and content SHA256;
changing content requires fresh integration checks even if created_at is unchanged.
A checkpoint without this binding cannot release planned consumers.
Never update frozen plan checkboxes or TASK lifecycle fields;
execution progress is runtime-owned. Clean temporary worktrees promptly; cleanup
remains advisory. Plan/member/dependency/scope changes return to planning/review;
capacity changes and runtime progress do not change the plan.

Legacy executions without a registered plan remain recoverable and are labeled
legacy. Do not invent retrospective S5 signatures. New planning must use this
contract. The harness projects and validates; platform spawn success is a separate
fact. No idle-capacity or worktree-cleanup Stop gate is introduced.

The parent receives a short live summary at SessionStart and after Agent returns
or Harness workgroup/result/integration actions. It names eligible tasks, results
awaiting integration and dependency waits (up to four per category). Async agent
launches are not completion events. This uses the existing additionalContext
channel; Stop/Idle messages do not claim parent delivery. Projection failures are
advisory and never introduce a new tool or Stop gate.
