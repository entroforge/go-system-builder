# Controlled E2E adapter

This package is an implementation component under development. It has **no
public execution command and no Runtime registration privilege yet**. Runtime,
Review and Repair v2 are not enabled by adding this package. Source discovery
remains a candidate hint; it cannot create runner evidence.

`RunFormal` is the execution adapter; `Capture.Run` delegates to that same
entry while preserving raw output durably. Both require a resolved
`fileview.View` and the exact SHA256 of `docs/control/e2e-runner.json`. The
profile, config and every executable input come from the formal Git tree,
including paths with a more-specific source rule. Working-tree fallbacks and
disk overrides are rejected. The caller's context bounds subsequent Git reads,
copying and execution. A moved branch invalidates the observation.

The profile schema is `e2e-runner-profile.schema.json`. It pins Linux OS,
architecture and kernel release, existing Node, the complete module dependency
tree, provisioned browser binaries, a sparse system root and the platform's
`/usr/bin/bwrap`. The adapter copies and hashes all five tool inputs before use.
`InspectTool` and `InspectPlatform` gather facts without executing, installing
or approving anything. Identical normalized tool trees have identical digests;
directory inode sizes and rewritten internal absolute symlinks do not change
their identity. There is no shipped provisioner or approved default profile.

Bubblewrap creates a private network namespace and mounts the copied product
and tools read-only. Only temporary storage is writable. Host `/usr`, `/lib`
and `/etc` are not mounted into the child. The bwrap executable and every parent
directory must be root-owned and not writable by other users. The host kernel
and the loader/libraries that start bwrap remain trusted platform dependencies;
this is not a reproducible VM or protection against the same account modifying
the host control plane. Unsupported isolation fails without an unconfined retry.

Collection uses the pinned Playwright CLI's `--list` under the same isolation as
execution: imports and config are executable. Execution requires an explicit
nonempty collected test ID set. The actual command fixes one worker, zero
retries, one repetition and `--forbid-only`; selectors, collected IDs and final
executed IDs must match. Profile/input/source/subject/Runtime/generation/round
must still match the collection. The receipt preserves raw events, exit status,
output, all reported attempts and observed API steps. Preparation failures after
structural binding also produce an UNKNOWN observation with `invoked=false`.
`invoked=true` means the isolation adapter was called, not that a browser
successfully started. Only `browser_page_observed` records page creation.

The supported browser observation model is per-test page creation. The pinned
reporter observes Playwright `pw:api` steps and recognizes the exercised exact
`Create page` event. User `test.step` titles and printed JSON do not count.
Shared pages created outside a test's attempt cannot currently satisfy browser
coverage; they need an adapter with explicit ownership. A pure unit test can
have `outcome=pass` for its function, but `ValidateExecutionCoverage` refuses it
as browser coverage. Skip, interruption, expected failures and retries never
become passing coverage. A failed product assertion remains a failed attempt.

`gsb.case` annotations carry an array of explicit module-qualified CASE/oracle,
persona and data-profile references, one annotation per tuple. Requirements
are the finite explicit browser/project/persona/profile set in the ReviewPlan;
`ReadOracles` reads the original cases.json documents, and never invents a new
per-REQ registry or a Cartesian product. A test may answer several explicit
oracles, but its execution ID remains one observation. Missing/duplicate sets,
foreign modules, overlapping executions and incompatible input snapshots fail.

`DecodeReceipt` validates the strict versioned receipt and recomputes its
summary from raw events. It does **not** authenticate caller JSON or construct
an `Observation`. Only `RunFormal` produces the private observation type. A
future Runtime producer must validate permissions, index only that type,
publish via its artifact transaction and recheck authority under CAS. Consumers
must verify the immutable index and exact Plan/assignment/round before using
coverage helpers. Reviewed JavaScript and the reporter share a process;
neither this adapter nor annotation text proves assertion adequacy. Independent
oracle review and discriminating counterexamples remain necessary.

`PrepareCapture` freezes the formal request and creates an exclusive private
directory under `.claude/operations/e2e-runs/`. Its versioned `intent.json`,
empty events/output files and parent directories are fsynced before return.
Collection mode rejects execution selectors; execution mode requires a valid
matching collection and a nonempty unique selected subset. `Capture.Run` is
single-use, writes/fsyncs observed output as it arrives and saves the returned
observation. A failed capture write cancels execution and retains bytes already
observed in memory. `Close` never deletes recovery material. Source views,
receipt values and selected slices are copied before intent publication.

Before invoking `Capture.Run`, the future producer must commit its exact
`IntentBytes()` in the existing Runtime journal/artifact transaction and hold a
per-operation OS lease. These producer steps are **not implemented** here.
`InspectInterruptedCapture` compares the exact registered intent bytes and
returns bounded raw recovery material with outcome UNKNOWN. It deliberately
cannot construct an `Observation` or promote a saved/caller-written PASS into
execution evidence. Missing or changed capture files fail inspection and must
remain a recovery problem; they do not authorize rerunning the same operation.

The Linux integration test kills an actual adapter process after the first
browser assertion failed and the next test began. It checks that the failure
is still present and observed browser/isolator descendants stop running. This
does not test Runtime commit boundaries: durable start/completion registration,
operation leases, generic add/import exclusions, Plan/Result/round consumers,
writer fencing, migration and mixed readiness are subsequent work.

Unit tests run normally without browser downloads. The explicit real-browser
test requires `GSB_E2E_INTEGRATION=1` plus already provisioned tool paths through
`GSB_E2E_NODE`, `GSB_E2E_MODULES`, `GSB_E2E_BROWSERS`, `GSB_E2E_SYSTEM` and
`GSB_E2E_ISOLATOR`. Set `GSB_E2E_BROWSER_NAME` and
`GSB_E2E_BROWSER_EXECUTABLE` for the provisioned browser. Missing capabilities
fail the enabled test. `GSB_E2E_RECEIPT_DIR` optionally retains fixture receipts.
