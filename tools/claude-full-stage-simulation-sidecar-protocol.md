# Simulation sidecar observation protocol

The operator runs `claude-full-stage-simulation.py` in a new disposable project
installed from the current release archive. This protocol describes the observer;
it adds no work-log or reporting task for Main or Workers.

The scenario supplies explicitly labeled synthetic requirements, observations,
professional conclusions and human choices. Native authors use their own original
answers and current installed Hooks. Necessary placeholders, local Git commits and
normal tools are allowed. Do not develop a real business product, fabricate
Runtime/journal/receipts or accepted facts, bypass consumers, push, deploy or
perform a real release. A simulated terminal state is not product acceptance.

The sidecar forwards each frozen original Hook command with its original stdin,
stdout, stderr and exit code. It independently records full payloads and outputs,
before/after Runtime samples and wall-clock timing. The wrapper adds observation
cost; before/after revision changes can overlap and do not identify a writer.
Model stdout/stderr are retained unabridged. `model-stream-timing-*.jsonl` records
arrival samples of their complete byte chunks; these are observer arrival times,
not token generation or inference times. Available platform actor transcripts are
copied without asking authors to produce extra logs. Preview text in a summary
never replaces the raw records.

There is no default deadline or 15-minute ceiling. An explicit operator deadline,
interrupt, nonzero exit, missing transcript or real Hook failure remains in the
artifacts. Resume the actual Main session using `--resume` and a continuation
prompt when needed; preserve prior attempts. Do not edit Runtime to restart it or
delete failed runs. No automatic retry count changes a professional obligation.

`verify` reports observations and requires independent review of the original
responsibility/acceptance relationships. Fixture changes, normal commands and
optional evidence hashes are diagnostic observations, not new acceptance gates.
A green marker check establishes only the marker behavior it actually checked.

Live transcript capture uses the actual `SessionStart` payload, recorded before
running the original Hook. Exact Main and session-local subagent transcript/meta
files are sampled without waiting for Stop. `live-transcripts-N/index.jsonl`
associates timestamped original-byte segments with session, actor, source and
offset. Rotation/truncation observed through inode, size or tail-anchor changes
starts another segment. Missing paths and incomplete bounded final drains stay
`unknown`; no session is inferred from recency, no unrelated project or provider
environment is read, and no Worker logging task is introduced. Polling cannot
recover bytes deleted before observation. Final drain has a one-second budget;
the daemon thread has a bounded shutdown join, preserving earlier captures when
interrupted without blocking the main process indefinitely.

## Two calibrated synthetic document topics

`ledger` and `offline-cli` now use separate prewritten, unaccepted candidate
business documents. The first represents a delivery-source ledger; the second
represents an offline archive inventory. Each uses one synthetic NFR and one
TASK, and its own marker sample. Both are document-only simulations: neither
implements an actual archive/CLI nor asks any author to create a product or test
program. The candidates are optional starting material in the given project,
not already accepted state, receipts, or author answers. Main and each Worker
still use the original issued responsibilities, forms and consumers; real
findings are not suppressed and cannot be overwritten with a simulated PASS.

`make check` alone checks the designated preset marker. In S6 the Builder creates and commits the previously absent scenario note (`simulation/deliveries/ledger-note.txt` or `simulation/deliveries/offline-note.txt`), with SIMULATION purpose and its input path. The original TASK Closing Contract supplies two executable checks: `make check` and `make check DELIVERY=<scenario note>`. Original S6 prepare projects both into Main required_checks and CheckProfiles with post_merge mode; Main integrates the actual delivery branch and runs both. The latter checks the nonempty output, simulation label and reference. Baseline S0–S5 checks never require the future output. It neither
proves archive behavior nor certifies the candidate documents, a real repair,
or a real product. The topics are distinct, but share a simple document-based
shape; these two runs alone do not test the range of production UI, API, or
other business behavior. Formal release and real native platform evidence are
still needed to claim stage transitions.

Before model invocation, fixture maintainers may validate the proposed inputs
in an independently installed disposable project, using existing documentation,
contract and TASK checks where those are available without binding an accepted
project. This is a calibration step for the test scenario, not a new Hook,
product gate, author requirement or guarantee that a Reviewer will return PASS.
Record any check warnings and unavailable checks; do not fabricate Runtime,
receipts or baseline to satisfy a preflight. Preserve original S5 feedback.

The first S7 Reviewer receives the explicitly synthetic observation on original dispatch and authors their own original Finding answer, which the original consumer routes to S8. Main does not substitute an answer. S8/S11 simulation-human choices must be applied through their existing original decision consumers; an unsupported choice remains a real blocker. Candidate documents carry no previous run, S2/S5 review, receipt, approval, source digest or check-result history.
