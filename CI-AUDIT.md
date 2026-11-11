# CI Test Necessity Audit — `go-system-builder`

**Scope:** full sweep of `.github/workflows/*`, the `Makefile` verification graph, the Go test corpus
(995 tracked `_test.go` files / 3613 test functions / 77 packages), and the release packaging path.
**Method:** 5 parallel sub-agents (workflow topology · test-corpus value · runtime profiling · build/packaging
overhead · test-selection redesign) + direct inspection of live GitHub Actions run history via the
authenticated `gh` CLI + a CI-equivalent timing matrix run in a clean `git clone`.

**Labels:** `[M]` measured · `[S]` static inference from code · `[U]` unverified assumption.

---

## 0. Executive summary

The brief was "which tests can we delete to make CI faster". The data says the framing is wrong in three
specific ways — and the real problem is worse than a speed problem.

### 0.1 The CI is OFF, and the one live workflow is the wrong one

| Fact | Evidence |
|---|---|
| `ci.yml` has **not run in 37 days** (last run 2026-09-21) | `gh run list --workflow=ci.yml` — 13 runs ever, newest 2026-09-21 |
| `main` has **not moved in 37 days** (0 commits since 2026-09-21) | `git log --since=2026-09-22 main` → 0 |
| The repo has had **exactly 1 PR in its entire history** (2026-08-30) | `gh pr list --state all` → 1 |
| ⇒ `ci.yml`'s `pull_request:` trigger is **dead code** | it never fired outside that one PR |
| All recent work went to feature branches | `git for-each-ref` → `codex/backup-global-optimization-20261013` |
| `runtime-portability.yml` is the **only** workflow still running | 17 runs / 56 days, all `push` events |
| …and it fails **35%** of the time (6 of 17) | `gh run list --workflow=runtime-portability.yml` |
| `main` is **not branch-protected** (404 "Branch not protected") | `gh api .../branches/main/protection` |

**The "fast PR lane" designed in commit `2fb3700` (2026-10-22) has never executed in CI.** The only live
signal on developer pushes is a workflow that runs two redundant test commands on three OSes and is red a
third of the time.

### 0.2 The fast lane is RED on a clean checkout, and it is over its own timeout

Measured in a **clean `git clone` at HEAD `9bf73ca`** (85 MB, 77 packages, no untracked artifacts) — the
exact state a GitHub runner checks out `[M]`:

| Target | Cold GOCACHE | Warm GOCACHE | Exit |
|---|---|---|---|
| `go run ./cmd/loop-harness init --root .` | 127.5 s | — | 0 |
| `fmt-check` | 3.8 s | 3.2 s | 0 |
| `vet` | 294.7 s | 8.3 s | 0 |
| `test-ci-core` | 720.9 s | 338.0 s | **2 — FAIL** |
| `test-ci-smoke` | 435.2 s | 310.8 s | 0 |
| `test-ci-race` | 675.9 s | 324.3 s | 2 / **0** |
| `doctor` | 5.3 s | 2.0 s | 0 |
| `validate` | 2.5 s | 2.4 s | 0 |
| `build` | 8.5 s | 1.2 s | 0 |
| **PR lane total** | **2274 s = 37.9 min** | **990 s = 16.5 min** | |

`ci.yml:28` sets `timeout-minutes: 15` (900 s). **Cold = 2.53× over. Warm = 1.10× over.**
Both regimes also **fail** — see §0.3.

### 0.3 Two distinct failure modes, only one of which is a real bug

| Package | Inside `test-ci-core` | Run alone (`-timeout=20m`) | Verdict |
|---|---|---|---|
| `internal/assignment` | `panic: test timed out after 5m0s` (300.1 s) | **ok — 143.3 s** | **Not a bug.** A `-p 2` contention artifact: the package needs 143 s of CPU but exceeds the 5-minute *wall-clock* per-package budget when it shares the runner. |
| `internal/review` | `panic: test timed out after 5m0s` (300.1 s) | **FAIL — 164.7 s** | **A real bug on the committed tree.** `TestIssuedResultPacketConcurrentIssuanceUsesOneOriginalEvent` (`internal/review/result_packet_test.go:463`): *"frozen baseline drift: undeclared product file(s) `.claude/work-packets/loop-REQ-TEST/g1/S7/r1/assignment-qa-1/.packet-<hash>.tmp` outside frozen_subjects"* — an atomic-write temp file escapes the frozen baseline. |
| `internal/semantic` | fails only in the **dirty working tree** | **ok** in the clean clone | **Local artifact.** `internal/semantic/scenario_integration_test.go:15` walks the repo root and reads `.local/framework-maintenance/…` as a file → *"is a directory"*. The untracked `.local/` tree is **8.4 GB**; the test helper `copyRepositoryForScenarioTest` (`scenario_integration_test.go:39-56`) copies the whole repo root except `.git`. |

**So the CI-blocking bug list is short and specific:** one real assertion failure (`internal/review`), one
timeout budget that is too tight for contention (`internal/assignment`), and one non-hermetic test that
breaks any developer who has an 8.4 GB `.local/` directory.

### 0.4 The test suite is not padded — cutting tests is not the lever

| Bucket | Count | Confidence |
|---|---|---|
| Genuinely necessary | **~2850 (79%)** | medium-high |
| Plausibly redundant / mergeable | **~660 (18%)** | medium |
| Effectively dead weight | **~100 (3%)** | medium-high |

Real redundancy harvest: **~100–200 functions (3–6%)**, worth maybe 2–4 minutes. **`internal/cli` is 94%
scenario/E2E** — only 44 of 1065 test functions are transitively free of filesystem and process work. There
is no "obviously fast subset" to carve out; a fast lane has to be **built**.

### 0.5 Where the time actually goes

From the real Release run (2026-09-21, run `35564562083`) `[M]`:

| Step | Duration | Share |
|---|---|---|
| **Verify (`make ci-verify-full`)** | **446 s** | **95%** |
| Initialize local runtime | 9 s | 2% |
| Build release package (5 cross-compiles + tarball + sha256) | 6 s | 1% |
| Publish assets | 3 s | 1% |
| Setup Go | 4 s | 1% |
| Checkout | 1 s | <1% |

**Cross-compiling all 5 release platforms, building the tarball and hashing it costs 6 seconds.**
Everything expensive is test execution. Build caching, `go run` overhead, checkout size and packaging
structure are noise — worth minutes at most. **Test selection is worth tens of minutes.**

---

## 1. The CI surface

### 1.1 What exists

| Workflow | Triggers | Jobs | Measured |
|---|---|---|---|
| `ci.yml` | `pull_request`→{main, integration/loop-next}, `push`→{main}, `workflow_dispatch(full)` | 1 (`verify`) | 7m36s `[M]` (run 35564553715 — **pre-split**, i.e. the *full* lane) |
| `release.yml` | `push` tag `v*.*.*`, `release: published`, `workflow_dispatch(tag)` | 1 (`release`) | 7m51s `[M]` (run 35564562083) |
| `runtime-portability.yml` | `on: [push, pull_request]` — **no branch filter** | 3 (ubuntu/windows/macos) | ubuntu 51–71 s, windows 2m08s–3m18s, macos 63s–94 s `[M]` |

### 1.2 The make verification graph

```
ci-verify      = fmt-check vet test-ci test-ci-race doctor validate build          # PR lane
ci-verify-full = fmt-check vet test test-race doctor doctor-staged validate build   # release lane

test-ci       = test-ci-core + test-ci-smoke
test-ci-core  = go test -short -p 2 -timeout=5m  <72 of 77 pkgs>       # sed filter, Makefile:62
test-ci-smoke = 3 targeted `go test -short -run <regex>` on internal/cli, internal/integration, internal/investigation
test-ci-race  = go test -race -short -p 2 -timeout=5m  <10 pkgs> + integration -run '^TestCheckpointStore.*'
```

### 1.3 The macOS leg is a hard failure, and GitHub told us why

Two consecutive runs on 2026-10-28 failed `[M]`:

| Run | ubuntu | windows | macOS |
|---|---|---|---|
| `40684668033` (12:03) | 71 s ✓ | 3m18s ✓ | 94 s ✓ |
| `40686352987` (12:15) | 51 s ✓ | 2m26s ✓ | **901 s, cancelled, 0 steps** |
| `40710425077` (14:53) | 51 s ✓ | 2m28s ✓ | **901 s, cancelled, 0 steps** |

GitHub's own check-run annotations give the cause `[M]`:
> *"The job was not acquired by Runner of type hosted even after multiple attempts"* +
> *"Due to capacity constraints, jobs targeting macOS arm64 runners may experience longer queue times."*

`runner_id: 0`, `runner_name: ""`, `steps: []`, no logs. It was **not** a test failure, **not** a job timeout
(the workflow sets none → default 360 min), and **not** commit-dependent — the same SHA `13d4622b` succeeded
12 minutes earlier on another branch. History: 13 healthy macOS runs (33–94 s), then 2 consecutive
queue-cancellations. Pure capacity.

---

## 2. Redundancy — the proof

### 2.1 `runtime-portability.yml` runs zero tests that `ci.yml` doesn't already run

| Portability command | Already covered by | Proof |
|---|---|---|
| `go test ./internal/runtime ./internal/recovery` | `ci-verify` → `test-ci-core` | the `sed` filter at `Makefile:62` removes only `internal/(cli\|integration\|investigation)` and `tests/system/…`; `go list` confirms both packages survive the filter `[M]` |
| `go test ./internal/cli -run 'TestBuildInfoDoesNotNeedProject\|TestInitDerivesHookMetadataAndCreatesJournals'` | `ci-verify` → `test-ci-smoke` | both names appear in the `Makefile:70` alternation `[M]` |

**The workflow's only unique value is the non-Linux OS matrix** — real (see §2.2), but currently buried under a
redundant ubuntu leg, an unfiltered trigger, and a flaky macOS runner.

### 2.2 …but the Windows/macOS legs have caught real defects `[M]`

Run `39711564836`: the **windows** leg genuinely failed (job `119603859961`, 02:28:32→02:30:06), and the
following commits are direct fixes: *"normalize shared-model consumer directories on Windows"*, *"keep S9
rejection assertions portable across Go versions"*. Commit `df31016` added the per-platform `syncDir`
(POSIX fsync vs Windows directory-handle semantics), documented at `docs/runtime-portability.md:67-77`.
**Do not cut the Windows or macOS legs.**

### 2.3 `runtime-portability.yml` cannot fire its `pull_request` trigger `[M]`

```
$ git ls-tree -r --name-only origin/main -- .github/
.github/workflows/ci.yml
.github/workflows/release.yml
```

The portability workflow **does not exist on `main`** — it lives only on feature branches. GitHub resolves
`pull_request` workflow files from the PR's **base** branch, so its `pull_request` trigger can never fire.
Its `push` trigger *does* fire (push events read the file from the pushed ref) — so it runs on every
feature-branch push and never on a PR. **Exactly inverted.**

### 2.4 Release verifies the same commit twice `[M]`

`git rev-parse v0.1.6^{commit}` = `60c34400…` = the `head_sha` of successful CI run `35564553715`.
The tag commit already passed CI; the Release job then re-ran `ci-verify-full` on the identical tree.

---

## 3. Necessity verdict — keep / change / cut

### 3.1 Workflow & job level

| Target | Verdict | Action | Saving |
|---|---|---|---|
| `ci.yml` → `verify` | **KEEP** | — | — |
| `ci.yml` PR path (`make ci-verify`) | **KEEP** | keep as the fast lane | — |
| `ci.yml` `timeout-minutes: 15` | **CHANGE** | → 30 (warm lane alone needs 16.5 min; cold needs 37.9) | prevents false red |
| `ci.yml` `pull_request:` trigger | **CHANGE** | 1 PR in the repo's life → make the **push** trigger fire on feature branches, or adopt PRs | restores the gate |
| `runtime-portability.yml` **ubuntu** leg | **CUT** | 100% duplicated by `ci-verify` (§2.1) | 1 job, 51–71 s |
| `runtime-portability.yml` windows/macOS legs | **KEEP** | caught real portability defects (§2.2) | — |
| `runtime-portability.yml` trigger | **CHANGE** | → `pull_request`{main, integration/loop-next} + `push`{main}; **merge the file into `main`** | 17 → ~2 runs/wk |
| `runtime-portability.yml` macOS | **CHANGE** | `continue-on-error: true` or pin `macos-15` — 2/2 queue-cancellations | removes red flake |
| `runtime-portability.yml` setup-go `cache` | **NO CHANGE** | `cache` **defaults to `true`** in setup-go v5 (`action.yml`) `[S]` — the explicit `cache: true` elsewhere is a redundant no-op | — |
| `release.yml` → `make ci-verify-full` | **CHANGE** | run only the delta (`test test-race doctor-staged`); CI already ran fmt/vet/doctor/validate on the same SHA | ~40 s |
| `release.yml` `release: published` trigger | **CHANGE** | token-created releases don't re-trigger, but a **human-published** release fires a second full run in the same non-cancelling concurrency group | ~8 min on the human-publish path |
| `release.yml` `timeout-minutes` | **ADD** | currently none → defaults to 360 min | — |

### 3.2 Makefile target level

| Target | Verdict | Action |
|---|---|---|
| `fmt-check` | **KEEP** | 3.2–3.8 s `[M]` |
| `vet` | **KEEP** | 8.3 s warm / 294.7 s cold `[M]` — type-checks all 995 test files |
| `test-ci-core` / `-smoke` / `-race` | **KEEP** | the PR lane's substance |
| `CI_TEST_TIMEOUT ?= 5m` | **RAISE** → 15 m | `internal/assignment` needs 143 s of CPU but blows the 300 s wall-clock budget under `-p 2` `[M]`. The repo's own doc calls this a *"failure bound, not an expected duration"* (`docs/guides/engineering-loop.md`) |
| `doctor` | **KEEP** | load-bearing; without `init` it fails (`internal/semantic/validator.go:184`) `[S]` |
| `validate` | **CUT from CI** | `Makefile:95` calls **only** `semantic.ValidateRepository` (`internal/cli/run.go:2872`), which `doctor` also calls (`run.go:2969`) → strict subset, same root. 2.4 s |
| `doctor-staged` | **TRIM** | `Makefile:130` (staged release-graph), `:134` (installed release-graph) and `:136` (installed `validate`) are re-run **inside** `install` (`internal/cli/install_command.go:67,157,160`) → drop those 3 lines; **keep** `install` and the installed-tree `doctor` (`install` never runs the agent-definition / evidence-catalog / policy-ref checks) |
| `build-all` inside `release` | **CUT from release path** | `build-release.sh:74-78` cross-compiles independently; `dist/bin/*` is referenced **nowhere** outside the Makefile `[S]` |
| `go run` → prebuilt binary | **CHANGE** | 3 `go run` per lane ≈ 2–3 s each; `build` already exists and `doctor-staged: build` proves the pattern |
| `platform-smoke*`, `health`, `manual`, `release-list`, `clean-release`, `release-graph-validate` | **KEEP (local only)** | **zero CI cost** — no workflow invokes them. Do not delete. |
| `release-graph` (in `.PHONY`, no rule) | **FIX** | `make release-graph` fails — add a rule or drop it from `.PHONY` |
| `fmt-check` coverage | **EXTEND** | `gofmt -l cmd internal tests` omits `tools/` — where the new tooling would live |

### 3.3 Test-corpus level — the honest verdict

**`internal/cli` (333 files / 1065 funcs) — KEEP, but restructure.** 94% scenario/E2E (129 files spawn
processes, 185 touch the filesystem); only 44 funcs are transitively FS/process-free. It is the only coverage
of S0–S11 stage transitions. Cutting it wholesale is the tempting-but-dangerous cut.

**`tests/system/req039` (39/115) — KEEP.** The **only L4 system-fidelity layer**; 21 of 24 CT-039 IDs are
duplicated with `internal/cli`. But they are **not** the same layer: `internal/cli` drives
`controller.RunControlCycle` in-process, `req039` drives the real hook path via
`tests/fixtures/req039/fixtures.go:29`. **Delete the `internal/cli` "thin pins" (which the code itself calls
thin — `internal/cli/ct_11_24_test.go:104-106`), never the system layer.**

**`internal/integration` (41/115) and `internal/investigation` (37/104) — KEEP.** `internal/integration` is
the PR lane's **only git coverage**; `internal/investigation` owns human-approval authority
(`TechnicalReviewCannotMasqueradeAsHumanApproval`).

**The PR lane currently drops `tests/system/req039` + `tests/system/recovery` entirely** (`Makefile:62`
excludes `/tests/system(/|$)`) — 127 functions including the only L4 layer. **That is the biggest risk hole
in the current design, not the biggest cost.**

**`internal/review` — fix, don't cut.** It holds a real failing test (`result_packet_test.go:463`) and is
one of the two packages that blow the 5-minute budget.

**~45 LOW-VALUE functions across 12 tiny packages** (`internal/e2ecoverage`, `internal/doclinks`,
`internal/docscope`, `internal/reviewmeta`, `internal/missingtokens`, `internal/packetbody`,
`internal/bugprojection`, `internal/diagnostic*`, …) — safe to drop, but worth ~0 seconds. Do this for
clarity, not speed.

**~30 mechanical duplicate merges — do these.** Zero coverage loss:
- `internal/cli/planning_guidance_test.go:390` vs `:137` — identical 26-line body modulo the stage string
- `internal/releasegraph/validator_test.go` — 3 exact-duplicate pairs (ratio 1.000)
- `internal/assignment/bug_lifecycle_test.go` — 3 exact-duplicate pairs
- `internal/scenario/bridge_test.go` — 3 exact-duplicate pairs
- `internal/cli/ct_11_24_test.go:256` — **a permanently-skipped stub** (the body is only `t.Skip`) → delete
- `internal/verification/clean_round_matrix_test.go:17` vs `clean_round_test.go:199` — ratio 1.000
- 7 env-gated (`GSB_*`) probes that never run in CI — exclude from the PR lane, don't delete

**188 zero-churn critical files — never cut.** `internal/filelock/lock_test.go` is 31 lines guarding lock
contention; `internal/hook/responsibility_test.go` +942 lines, churn 1; `internal/qualitygate/
canonical_builder_result_test.go` +866, churn 1. *Churn measures maintenance cost, never necessity.*

**Highest-churn files — table-drive, don't cut.** `internal/cli/controller_test.go` (11 mods, +1937),
`internal/cli/run_test.go` (11 mods, +1870), `internal/cli/repair_lead_questionnaire_test.go` (9 mods, +1472
= 174 lines/func), `internal/controller/cycle_test.go` (7 mods, +856). Table-driving these collapses ~4600
lines to ~2000 and cuts churn ~70%.

---

## 4. The test-selection mechanism — the real fix

### 4.1 Current mechanism and its failure modes

`Makefile:70` is a **single 2380-byte line** holding an alternation of 45 test names (43 exact + 2
wildcards), applied to a package with 1065 test functions.

| Failure mode | Status |
|---|---|
| **Zero-match `-run` exits 0** — `go test -run '^TestDoesNotExist$'` prints *"no tests to run"* and **passes** | **Confirmed empirically** `[M]` |
| A renamed test silently stops running | Live risk — no guard exists |
| `NativeS5.*` and `NativeReviewResume.*` are **unanchored**, so new `TestNativeS5…` tests are auto-pulled into the PR lane — **contradicting the Makefile's own comment** at `Makefile:68`** (*"Use anchored names so a similarly named slow scenario is not added by accident"*) | **Confirmed** `[M]` — 12 + 2 extra tests today |
| `Makefile:70` changed in **7 of the 15** commits that touched the Makefile — a merge-conflict magnet | `[M]` |
| Currently **0 dead entries** (all 45/7/6 names resolve) | **Confirmed** `[M]` — the regex is accurate today, just unguarded |

> **Correction:** the regex is **not** truncated. `Makefile:70` is 2380 bytes and ends
> `…HookBudgetTimeoutKeepsListenerNonBlocking)$$'`, a complete, valid RE2 alternation. The apparent
> truncation was a **display artifact** of the file reader.

### 4.2 Option comparison

| Option | Effort | Silent coverage-loss risk | Verdict |
|---|---|---|---|
| **A** `testing.Short()` + `t.Skip` | ~340 in-body edits across 335 files; **inverts** the burden (today you opt *out*; `Short` requires opting *in*, so a forgotten guard puts a slow test in the PR lane) | Low for renames, high for new slow tests | Viable 2nd choice. Only **1** tracked file uses `testing.Short()` today (`internal/hook/adapter_test.go:431`) — no convention exists. |
| **B** build tag `//go:build !fast` | ~340 **one-line** insertions, scriptable | Low — `!fast` files are included by default, so `go test ./...` semantics are unchanged | **RECOMMENDED** |
| **B′** `//go:build short` | — | — | **DOES NOT EXIST.** `cmd/go` forwards `-short` as `-test.short` (`cmd/go/internal/test/test.go:1857`); it never sets a `short` build constraint. A `short` tag would be dead code. |
| **C** package split | `git mv` 179 files + package-clause rewrite; **1–2 engineer-days** | Low | **Rejected for value.** The 1008 never-run tests reference **348 distinct unexported production symbols** (vs only 30 for the 57 selected) → splitting the slow half means exporting ~348 symbols. Also the `sed` filter at `Makefile:62` uses a `$` end-anchor, so `internal/cli/scenarios` would **leak into** `test-ci-core`. |
| **D** curated manifest file + guard tool | ~1 file + ~150-line `go/ast` tool (precedent: `tools/drift-check/`, `tools/optimization-eval/`) | Low once guarded | **RECOMMENDED (paired with B)** |
| **E** test-list sharding | Medium | Low | **Poor fit.** Go cannot shard within a package without `-run`; each shard recompiles a 45 MB test binary. The PR lane's dominant cost is the 72 core packages, not the 102 filtered tests. |
| **F** auto-derive the regex from a convention | Medium | Low | **Blocked** — no derivable rule: the 57 selected span 23 distinct name prefixes across 31 files; only 3 files match `*scenario*_test.go` while 80 match `*live*`/`*e2e*`. A `//ci:fast` marker is option A under a new name. |

### 4.3 Recommended mechanism

**Build tag `!fast` (B) + curated manifest & guard (D).** Together they give:
- compile-time exclusion of the 129 process-spawning / 185 FS-touching `internal/cli` files from the PR lane —
  **zero** coverage removed from the release lane, **zero** risk of a slow test sneaking into the PR lane
- a loud guard that fails the build when a manifest entry stops resolving (closing the silent-zero-match hole)
- the ability to **expand** the fast lane safely, one manifest line at a time — which is where the actual
  saving comes from, because the PR lane currently runs only **5.35%** of `internal/cli`

### 4.4 Selection coverage today

| Package | Test funcs | Selected by fast lane | Never run in PR lane | Rate |
|---|---|---|---|---|
| `internal/cli` | 1065 | 57 | 1008 | **5.35%** |
| `internal/integration` | 115 | 28 | 87 | 24.3% |
| `internal/investigation` | 104 | 17 | 87 | 16.3% |
| core lane (72 pkgs) | 2197 | 2197 | 0 | 100% |
| `tests/system/*` | 126 | 0 | 126 | **0%** |
| **repo total** | **3607** | **2299** | **1308** | **63.7%** |

Repo-wide the PR lane looks healthy at 63.7%, but **inside the three filtered packages only 102 of 1284
tests (7.9%) ever run**, and those three packages hold 35.6% of the corpus.

---

## 5. Prioritized plan

### Tier 0 — make CI green and keep it green (nothing else matters until this is done)

| # | Change | Why | Effort |
|---|---|---|---|
| 0.1 | **Fix `internal/review/result_packet_test.go:463`** — the `.packet-<hash>.tmp` file escaping the frozen baseline | the only *real* failing test on the committed tree; blocks `ci-verify` outright | ~1 h |
| 0.2 | **Raise `CI_TEST_TIMEOUT` 5 m → 15 m** | `internal/assignment` needs 143 s of CPU but blows the 300 s wall-clock budget under `-p 2`; it is a false timeout, not a real one | 1 min |
| 0.3 | **Raise `ci.yml` `timeout-minutes` 15 → 30** | warm lane = 16.5 min, cold = 37.9 min; today it times out either way | 1 min |
| 0.4 | **Add a `push:` trigger on feature branches to `ci.yml`** (or adopt PRs) | the gate has not fired in 37 days; `main` hasn't moved | 5 min |
| 0.5 | **Merge `runtime-portability.yml` into `main`** | its `pull_request` trigger can never fire from a branch-only file (§2.3) | 5 min |
| 0.6 | **Fix the macOS leg** — `continue-on-error: true` or pin `macos-15` | 2/2 queue-cancellations = 35% workflow failure rate | 5 min |
| 0.7 | **Make `gofmt`/`vet` cover `tools/`** | the new tooling lands there | 1 min |
| 0.8 | **Make `internal/semantic`'s repo copy hermetic** — skip `.local/` (and any dot-dir) in `copyRepositoryForScenarioTest` | the 8.4 GB untracked `.local/` tree makes this test fail for every developer who has it | ~30 min |

### Tier 1 — structural savings (high confidence, low risk)

| # | Change | Measured saving |
|---|---|---|
| 1.1 | Cut the portability **ubuntu** leg | 1 job, 51–71 s `[M]` |
| 1.2 | Gate portability to PR + main, drop the blanket `push` | 17 → ~2 runs/wk |
| 1.3 | Remove `validate` from the CI lane (subsumed by `doctor`) | ~2.4 s + one full-tree validation |
| 1.4 | Trim `doctor-staged`: drop the 3 lines `install` already performs | 3 fewer tree validations |
| 1.5 | Unhook `build-all` from `release` (its output is unused) | ~6 s `[M]` |
| 1.6 | Release gate → delta only (`test test-race doctor-staged`) | ~40 s `[M]` |
| 1.7 | Drop the duplicate `release: published` trigger, or set `cancel-in-progress: true` | ~8 min on the human-publish path |
| 1.8 | `go run` → prebuilt binary (3 calls per lane) | ~5–10 s |

### Tier 2 — the fast lane itself (the real win)

| # | Change | Expected |
|---|---|---|
| 2.1 | Tag the 314 dirty `internal/cli`/integration/investigation files `//go:build !fast` | removes all process/FS cost from the PR lane; **zero** release-lane coverage loss; ~2 h scripted |
| 2.2 | Replace the 2380-byte regex with `ci/fast-lane.txt` + `tools/testsel` | closes the silent-zero-match hole; ~4 h |
| 2.3 | Add the guard: fail if a manifest entry stops resolving, or if a package's selected count drops below a committed baseline | ~1 h; ~5–10 s CI cost |
| 2.4 | Expand the fast lane using the now-safe loop — seed from the measured per-test timing ranking | this is where the minutes come from |
| 2.5 | Bring `tests/system/req039` CT-039-01…-24 into the PR lane at L3 fidelity | **closes the biggest risk hole** (127 functions, incl. the only L4 layer, currently never run) |
| 2.6 | Re-measure `-p` sensitivity (2 → 4/8) **after** 0.2; the 5-minute budget was the binding constraint, not `-p` itself | potentially large, but must be re-measured with the timeout fixed |

### Tier 3 — test-corpus cleanup (small time win, real maintenance win)

| # | Change | Note |
|---|---|---|
| 3.1 | Merge the ~30 exact/near-duplicate pairs (9 groups listed in §3.3) | zero coverage loss, ~4 h |
| 3.2 | Delete the permanently-skipped stub `internal/cli/ct_11_24_test.go:256` | <1 h |
| 3.3 | Table-drive the 4 highest-churn files (~4600 → ~2000 lines) | ~1 day; churn −70% |
| 3.4 | Delete the `internal/cli` CT-039 "thin pins" (keep the `req039` system layer) | the code itself documents them as thin |
| 3.5 | Drop the ~45 LOW-VALUE functions in 12 tiny packages | ~0 s saved — do it for clarity, not speed |

---

## 6. Risks and tempting-but-dangerous cuts

1. **Do NOT cut `internal/cli` wholesale from the PR lane.** It looks like a fat target (1067 funcs, 94%
   scenario) but it is the only coverage of S0–S11 stage transitions. Use the build-tag split instead.
2. **Do NOT cut `tests/system/req039` on the grounds that `internal/cli` covers CT-039-01…24.** Different
   layers (in-process controller vs real hook path). Cut the thin pins, never the system layer.
3. **Do NOT cut the 188 zero-churn critical files.** Churn measures maintenance cost, not necessity.
4. **Do NOT remove `init` from the workflows.** Measured: without `.claude/loop-state.json`, both `doctor` and
   `validate` fail (`internal/semantic/validator.go:184`).
5. **Do NOT remove the source-tree `doctor`** on the theory the installed-tree one is a superset. Different
   trees — the source root holds `blueprint/`, `agents/`, `docs/requirements/REQ-*`, the root `loop-harness.md`;
   the installed tree holds only the release subset. `install` never runs the agent-definition /
   evidence-catalog / policy-ref checks.
6. **Do NOT trust the 7m36s CI number as the fast lane's cost.** That run predates the split — it ran the
   *full* lane. The fast lane has never been measured in CI.
7. **Branch protection:** `main` is currently unprotected, so deleting the `runtime (ubuntu-latest)` job
   breaks nothing today. But if a rule is added later, required checks key on **job names** — a required
   `runtime (ubuntu-latest)` would hang at *"Expected — Waiting for status to be reported"* forever. Re-baseline
   required checks in the same PR that removes the leg.
8. **Do NOT treat the cold numbers as CI cost, and do NOT treat the warm numbers as CI cost.** The truth
   depends entirely on whether the setup-go primary key hits. Today it hits (`go.sum` unchanged in 105
   commits); the next Go patch release or runner-image bump breaks it — and there are **no restore-keys**.
9. **The `release-checksum` artifact is currently NOT the validated artifact.** `doctor-staged` validates a
   throwaway tmp tarball that is then deleted; `release-checksum` ships a *different* tarball. The repo's own
   audit note flags this (`.local/framework-maintenance/v0.3.0-release-review/CONVERGE-CI.md:33`). Fixing it
   (build once → validate that artifact → checksum that artifact) is a correctness win that also removes a
   cross-compile set.
10. **Public repo ⇒ GitHub-hosted runners are free.** Standard runner minutes for public repositories are
    free, so the macOS 10× multiplier is a **wall-clock and queue-pressure** problem, not a billing one.
    Confirm against the account's plan before quoting a dollar figure.

---

## 7. Appendix — corrections and contested claims

| Claim | Verdict |
|---|---|
| "The `Makefile:70` regex is truncated" | **REFUTED** — display artifact; the line is 2380 bytes, valid RE2, 57 tests match, 0 dead entries |
| "`runtime-portability.yml` has no Go cache" | **REFUTED** — setup-go v5's `cache` input **defaults to `true`** (`action.yml`); the explicit `cache: true` elsewhere is a redundant no-op |
| "The 40 MB `integration.test` slows checkout" | **REFUTED** — untracked and gitignored (`.gitignore:14:*.test`); local working-tree noise only, safe to `rm` |
| "`.local/framework-maintenance/` copies pollute the build" | **REFUTED for the build** — Go's `./...` ignores dot-directories; `go list ./...` = 77 packages, none under `.local/`. **CONFIRMED for the tests** — `copyRepositoryForScenarioTest` copies the repo root *except* `.git`, so the 8.4 GB `.local/` tree breaks `internal/semantic` and inflates local timings |
| "The release pipeline cross-compiles 15 times" | **STRUCTURE CONFIRMED, TIME OVERSTATED.** 15 cross-compiles + 2 tarballs is correct (5 `build-all` + 5 `doctor-staged` + 5 `release-checksum`), but the *measured* CI cost of the redundant sets is **6 s**, not 8–12 min — the Go build cache is shared, so sets 2 and 3 only re-link (measured: `build-all` = 27.6 s cold, 6 s inside the release job). The saving is ~6 s; the correctness fix (validate the shipped artifact) is the real value. |
| "3 files use `testing.Short()`" | **CORRECTED → 1 tracked file** (`internal/hook/adapter_test.go:431`); the other two hits were untracked `.local/` copies |
| "`go test -short` could drive a build tag" | **REFUTED** — `cmd/go` forwards `-short` as `-test.short`; no `short` build constraint is ever set |
| "Local test timings represent CI cost" | **REFUTED** — the working tree's 8.4 GB `.local/` tree distorts them. All timings in §0.2 were re-measured in a clean `git clone` at HEAD. |
