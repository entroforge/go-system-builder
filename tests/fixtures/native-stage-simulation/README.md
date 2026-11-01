# Native full-stage simulation inputs

These files are unaccepted synthetic Main documentation inputs. `ledger-calibrated/` serves `ledger`, `offline-calibrated/` serves `offline-cli`; the legacy `documents/` directory is an unused ledger example and is not loaded by the runner. They contain no accepted Runtime, receipts or Worker answers. The `offline-cli` scenario uses separate business inputs and does not load these NFR-only candidates. Native Agents author their own simulated professional answers through the current installed forms and original consumers.

Build the current formal release **once**, preserving its source snapshot and build log. Reuse that exact archive in two new, independent artifacts directories. Supply the installed Claude executable as an absolute path; the driver records its version without printing provider configuration.

```sh
python3 tools/claude-full-stage-simulation.py prepare --artifacts /tmp/sim-ledger \
  --scenario ledger --release-archive /absolute/path/release.tar.gz \
  --claude-bin /absolute/path/to/claude --env-file ~/.claude/minimax
python3 tools/claude-full-stage-simulation.py prepare --artifacts /tmp/sim-offline-cli \
  --scenario offline-cli --release-archive /absolute/path/release.tar.gz \
  --claude-bin /absolute/path/to/claude --env-file ~/.claude/minimax
python3 tools/claude-full-stage-simulation.py run --artifacts /tmp/sim-ledger --driver
python3 tools/claude-full-stage-simulation.py verify --artifacts /tmp/sim-ledger
```

The second lane runs and verifies using its own artifacts directory. `--dry-run` on `prepare` reports the scenario and candidate inputs without building, installing or starting a model. `--resume <actual Main session id> --prompt-file <file>` continues a recorded session and must not replace original facts; interventions and window timeouts remain part of the evidence.

Both scenarios include explicitly synthetic first-round Findings, human choices and professional conclusions. Necessary placeholder files, fixture checks, normal tools and local Git commits are allowed when the original workflow needs them. No real business product is developed, and no external publish, deploy or push is authorized. Baseline `make check` validates only the preset marker. The S6 Builder must actually create and commit its new note: `simulation/deliveries/ledger-note.txt` or `simulation/deliveries/offline-note.txt`, initially absent. Each note states SIMULATION purpose and its own input path. Main integrates the actual delivery branch, then runs both `make check` and the TASK’s `make check DELIVERY=<note path>`, which checks the nonempty note, simulation label and input reference. S0–S5 never require that future output. These checks prove only those actual file facts; neither proves CLI behavior, a real repair or professional acceptance. Main gives `simulation/observations/first-round.txt` to the applicable original Reviewer on the first S7 dispatch. That Reviewer labels the Finding as synthetic in their own original answer; Main never fills it for them. The original consumer receives it and routes it to S8. `simulation/DECISIONS.md` supplies test choices for S8 and S11 through their existing decision consumers; choices are not preaccepted approvals. A consumer unable to accept them is a real blocker, never permission to edit Runtime. S7 Findings enter S8 directly; the runner does not require an ObservationBatch or force S9 onto an old no-change route.

The model is launched by Bash after `source ~/.claude/minimax` (or the explicit `--env-file`) with `claude --permission-mode bypassPermissions`. Provider configuration contents are not logged. No model deadline applies by default; `--timeout-seconds` is optional and records an operator intervention. Real failures and prior outputs remain when continuing:

```sh
python3 tools/claude-full-stage-simulation.py summarize --artifacts /tmp/sim-ledger
python3 tools/claude-full-stage-simulation.py run --artifacts /tmp/sim-ledger \
  --resume ACTUAL_SESSION_ID --prompt-file /absolute/path/continuation.txt
python3 -m unittest discover -s tools -p 'test_claude_full_stage_simulation.py'
```

The Python regression tests mock model calls and never install, build, run Go tests or launch Claude. The real simulation is opt-in and separate from Go/CI tests. Main and Workers need no extra work logs: the independent sidecar captures original feedback and attempts.

The observer forwards the exact original Hook command and its stdout, stderr and exit status. Its wrapper and path are visible to the model, so this is not a blind experiment. Raw Hooks, model streams, available native actor transcripts, Runtime/journal and fixture observations are retained. Complete stdout/stderr chunks also carry observer arrival timestamps in `model-stream-timing-*.jsonl`; these timestamps do not measure model inference time. Verification reports stage cursors, native events, model process and fixture facts as observations; it deliberately returns an unknown/audit-required result rather than claiming complete receipt or source causality from counts. Independent review must associate the original Finding, S8/S9 sources, new complete S7, S10/S11 transitions and each actual actor/return. Nonempty ordinary Hook output can be legitimate feedback. Commands, path categories or missing explicit Write calls alone do not prove whether real product work occurred.

The live transcript sidecar starts independently with each model run. The Hook
wrapper records actual `SessionStart` input **before** invoking the original
Hook, so a hanging Hook still leaves the announced `session_id` and exact
`transcript_path`. The sidecar only reads that announced Main transcript and
`<session_id>/subagents/agent-*.jsonl` / `agent-*.meta.json` adjacent to it inside
Claude's projects directory. It does not select the newest session or scan other
projects. Missing, mismatched or inaccessible paths remain explicit `unknown`.

While running, inspect `live-transcripts-N/index.jsonl` for arrival timestamps,
session, actor, source file, byte offsets and segment filenames. The `.bin`
segments preserve original bytes, including incomplete JSON and invalid UTF-8;
subagent output and metadata do not require an Agent Stop. The observer polls at
0.2 seconds with bounded reads. Inode changes, shrinking files or changed tail
anchors start new segments without overwriting earlier captures. Polling cannot
recover bytes created and removed between observations or prove that an
unobservable truncate/rewrite did not occur; timestamps describe observation,
not model generation.

Shutdown attempts a one-second final drain and waits at most two seconds for the
transcript observer. An incomplete drain is recorded as `unknown`; the daemon
observer never holds model shutdown indefinitely. An interrupt retains all
already written segments. Neither observer writes into original transcripts,
Runtime or provider configuration. No extra Worker logs or tool restrictions are
introduced. Keep the prepared release/runner snapshot unchanged for continuation;
use a fresh independent artifacts directory when changing the runner or release.
