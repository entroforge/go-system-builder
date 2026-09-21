# Dispatch plan example

Read [the plan](project/docs/dev/tasks/index-REQ-042.md), then each linked TASK.
Validate with `loop-harness tasks check --root docs/examples/dispatch-plan/project --req REQ-042`.

This fixture demonstrates ordering, not implemented product code. Initially
01/02/03 can run together. Once 01/02 are integrated, 04 can start while 03
is still running. 05 waits for 01/03; 06 waits for 04/05. The integration checks,
results and runtime assignment records must come from actual execution.
