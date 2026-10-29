# Native full-stage simulation inputs

These files are synthetic Main documentation inputs for the `ledger` scenario. They contain no accepted Runtime, receipts or Worker answers. The `offline-cli` scenario uses separate business inputs and does not load these NFR-only candidates. Native Agents author their own simulated professional answers through the current installed forms and original consumers.

Build the current formal release **once**, preserving its source snapshot and build log. Reuse that exact archive in two new, independent artifacts directories. Supply the installed Claude executable as an absolute path; the driver records its version and digest without printing provider configuration.

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

Both scenarios include one explicitly synthetic first-round Finding, a simulated human decision and simulated professional conclusions. The offline CLI is not implemented or tested as a real product. The marker fixture and `make check` are not proof of that CLI's behavior. In S6, only if the original integration requires a source commit, the native Builder may create one explicitly labeled empty SIMULATION fixture commit; it is not a product implementation. S9 uses the original no-change route. All stage decisions, answers and gate transitions remain with the installed original consumers.

The observer forwards the exact original Hook command and its stdout, stderr and exit status. Its wrapper and path are visible to the model, so this is not a blind experiment. Raw Hooks, model streams, available native actor transcripts, Runtime/journal and fixture hashes are retained. Verification reports stage cursors, native events, model process and fixture facts as observations; it deliberately returns an unknown/audit-required result rather than claiming complete receipt or source causality from counts. Independent review must associate the original Finding, S8/S9 sources, fresh S7 clean round, S10/S11 transitions and each actual actor/return. Nonempty ordinary Hook output can be legitimate feedback. Commands, path categories or missing explicit Write calls alone do not prove whether real product work occurred.
