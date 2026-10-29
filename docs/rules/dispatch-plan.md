# Overall dispatch plan

S4 delivers one `docs/dev/tasks/index-REQ-<id>.md` plan for the current REQ, using
[index template](../dev/tasks/index-template.md). Every non-cancelled TASK appears
exactly once as an unchecked Markdown file link under `## W1`, `## W2`, etc.
The header declares `REQ`, `Dispatch policy: waves-v1`, a positive `Revision`,
and `Status: complete` before review. This is the plan-document status required
by its original gate; it does not mean any implementation responsibility is
`done`. Responsibility progress follows the shared five-state semantics in
[`state-machine.md`](state-machine.md#21-loop-responsibility-status). TASKs explicitly declare Source REQ refs.
Use numeric TASK IDs such as `TASK-009` or `TASK-009-01`, with matching file
names. Cancelled TASK files remain in the full S4 answer batch but do not appear
as active plan members; they contribute no clause coverage. Resolve any resulting
coverage gap without removing those files from the delivered batch. Source REQ
refs binds support and cancelled TASKs to the current REQ as well.
New REQs and TASKs declare waves-v1; missing plans are not a legacy exemption.

Keep real artifact dependencies in TASK Dependencies, not in a second graph.
Share read-only schemas freely: independent consumers may develop in parallel.
List concrete repository-relative prospective write paths, preferably as a
comma-separated list of individual code spans: `internal/a.go`, `tests/a_test.go`.
Existing plain comma-separated paths remain supported. A complete code list can
end with one paired Chinese or English parenthetical explanation, for example
`simulation/products/`（仿真产品输出区）. This explanation is excluded from scope.
Mixed bare/code paths, per-item notes, unconsumed tail text, unclosed delimiters,
placeholders, globs and paths escaping the repository are rejected with the
original TASK and Scope line before freezing. Keep other prose after the table.
Only the prospective write paths row supplies the reviewed write boundary;
read, output and forbidden rows cannot enlarge it. S4 checking/reception, S5
analysis and Assignment preparation consume the same parsed paths.
Declare mutable external resources in TASK Resources (`resource:<stable-name>`).
Narrow overly broad scopes.
Same-wave write/resource overlap must be resolved before dispatch; use independent
ownership or a justified Resource order table (Before TASK, After TASK, Resource,
Reason). Invalid non-header resource rows are errors, not absent constraints.
Resource order and artifact dependencies must form one acyclic order.
The effective write scope includes both assignment write_paths and output_paths;
both must stay within the reviewed TASK scope. For native S6, registration and delivery reception are engineering facts, not execution permission. Legacy activation and
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
claim a dispatch batch. A predecessor releases its consumers only after the
original Result has been consumed and its required integration checkpoint
verified, without waiting for unrelated tasks in the prior wave. Queued work is retained.

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
Never rewrite locked TASKs, issued Assignments or accepted facts to repair a
parser-derived scope. With unchanged original TASK bytes, an applicable existing
Assignment execution revision can produce an accurate successor subject to its
original new-owner, contribution-disposition and approval constraints; modes
without approval rebinding support remain unsupported. A real TASK or
professional scope change still requires original planning and review.
Before acceptance, correct the original TASK/index and save its original answer.
For accepted planning while S4 is still current, use
`runtime stage-answer rework --target tasks --reason "<concrete TASK/index correction>" --root . --agent Main`
when the frozen REQ/S2/S3 remain valid. Keep correct upstream work. Once in S5,
follow its independent review and original document-rework route. Ordinary
planning requires no fresh human approval; actual REQ changes or decisions
outside authorization retain their human boundary.

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
