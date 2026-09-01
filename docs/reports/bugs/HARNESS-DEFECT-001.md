# HARNESS-DEFECT-001 — `runtime human-decision` scope check unsatisfiable contract

> 历史关闭记录：本文中的“读取 X → 注册 X+1 → 再以 X+1 迁移”是缺陷发生时的旧流程，仅用于复盘，不能作为当前操作指南。当前普通命令不要求 Agent 传 Runtime revision；请以 [`blueprint/L4-revision-usage.md`](../../../blueprint/L4-revision-usage.md) 与 [`docs/agent-protocol.md`](../../agent-protocol.md) 为准。

**Severity**: P1 (blocks autonomous S8/S10/S11 path for any BUG investigation requiring re-entry from awaiting_human_release via TR-027 / TR-021 / TR-028 / TR-029 / TR-030)

**Component**: `.claude/bin/loop-harness` (commands: `runtime human-decision`, `runtime transition` for affected TRs, `runtime evidence add` for `kind=human_decision`)

**Discovered**: 2026-09-01 during REQ-004 BUG-120 S8 re-entry attempt.

**Status**: **closed as not reproducible in the current checked-in source; runbook and diagnostics clarified 2026-09-01**

---

## TL;DR

The incident report claimed that `runtime human-decision` rejects all
`human_decision` evidence because the harness compares a static artifact value
with a runtime-composed scope (`<verb>:<runtime_id>@<current_revision>`). That
claim is **not reproduced by the current checked-in source**: the complete
scope is stored in the registered evidence row's `scope_refs`, and the
transition validates it at the post-registration revision.

7 attempts (v1..v7) with every plausible static scope value rejected:
- bare verb `runtime_release` (matches schema pattern `^runtime_[a-z]+$`)
- full composed `runtime_release:loop-REQ-004@<rev>` (at various pre-check revisions)
- both fields (`human_decision_scope` + `lifecycle_scope`) set simultaneously

## Investigation disposition

The supplied incident is not reproducible against the current Go source. It
conflates two different contracts: `human_decision_scope` is a bare scope prefix
declared by the selected transition in `docs/loop-definition.json`, while the
current Runtime evidence record stores the complete binding in `scope_refs`.
The transition validates the evidence after the evidence registration commit,
not against the revision at which the artifact was authored.

The valid sequence is therefore: read the current revision `X`; author the
decision artifact; register it with `runtime evidence add --expected-revision X`
and `--scope-ref <scope>:<runtime-id>@<X+1>`; then invoke the fixed transition
with `--expected-revision X+1`. For S11, `<scope>` is `runtime_release`.
Registering the evidence changes the Runtime revision once, and the transition
then validates the evidence at that post-registration revision. The decision
artifact does not need, and should not contain, `human_decision_scope`.

No schema relaxation, scope bypass, or atomic replacement command was needed or
introduced. The runbook now documents this handoff, the transition error names
`scope_refs` and the revision sequence, and a regression test covers the
actionable scope-mismatch diagnostic.

---

## Reproduction transcript (verbatim, abridged)

```
Attempt v1 (file scope: runtime_release:loop-REQ-004@3102)
  register success (sha=e212b2a8...)
  runtime human-decision: rejected — "incompatible with human_decision_record"
    → first error: kind=bug doesn't match evidence kind expectation

Attempt v2 (added lifecycle_scope field)
  register success
  runtime human-decision: rejected — "is not current runtime evidence"
    → file updated after registration, fingerprint drift

Attempt v3 (scope updated to @3109)
  register success (rev → 3109)
  runtime human-decision: rejected — "must be scoped to runtime_release:loop-REQ-004@3109"
    → my file has human_decision_scope="runtime_release" (just verb per schema)
    → harness composes "runtime_release:loop-REQ-004@3109" (verb + runtime_id + check-time-rev)

Attempt v4 (scope = runtime_release:loop-REQ-004@3117)
  register success (rev → 3118)
  runtime human-decision: rejected — "must be scoped to runtime_release:loop-REQ-004@3117"
    → my file matched pre-check rev; transition CLI bumps rev to 3118 before the check

Attempt v5 (scope = @3139, file written for rev 3138+1)
  register success (rev → 3139)
  runtime human-decision: rejected — "must be scoped to runtime_release:loop-REQ-004@3139"
    → my file matched; transition bumped rev to 3140 before scope check

Attempt v6 (scope = "runtime_release" just verb per schema)
  register success
  runtime human-decision: rejected — "must be scoped to runtime_release:loop-REQ-004@3146"
    → harness composes full string; verb-only fails the composed-string compare

Attempt v7 (both human_decision_scope: runtime_release AND lifecycle_scope: runtime_release:loop-REQ-004@3148)
  register success
  runtime human-decision: rejected — "must be scoped to runtime_release:loop-REQ-004@3148"
    → lifecycle_scope matched; harness reads human_decision_scope (just verb), ignores lifecycle_scope
```

---

## Reported root cause (historical claim)

1. **Schema contract mismatch**: embedded JSON schema declares `human_decision_scope` field pattern as `^runtime_[a-z]+$` (a bare verb), but the runtime scope check composes the full `runtime_<verb>:<runtime_id>@<revision>` string and compares against the file's stored scope value.

   ```jsonc
   // Disassembled from .claude/bin/loop-harness (offsets 5325787, 5329975):
   "human_decision_scope": {
     "type": "string",
     "pattern": "^runtime_[a-z]+$"   // ← schema accepts only the verb
   }
   ```

   But the runtime check constructs:
   ```
   expectedScope = fmt.Sprintf("runtime_release:%s@%d", runtime_id, current_revision)
   ```

   No file content can simultaneously satisfy `^runtime_[a-z]+$` (verb only) AND equal `runtime_release:loop-REQ-004@<digit>`.

2. **Revision drift**: every harness operation (`runtime evidence add`, `runtime transition`, `runtime human-decision`) commits to the journal and bumps `runtime.revision`. The evidence file is written at rev `X`. Register bumps to `X+1`. Transition bumps to `X+2`. The harness constructs expected at `X+2` but the file is fixed at `X`.

3. **No atomic register-and-transition verb**: the `runtime human-decision` CLI does call `runtime transition` internally, but the file content was registered at the EARLIER revision and cannot be retroactively updated.

4. **`--expected-revision -1` (CAS-skip) does not help**: the harness rejects `-1` with `requires --expected-revision`, defeating the only escape hatch.

---

## Affected transitions

| TR | Source state | Target state | Disposition | Scope format string (binary disassembly) |
|---|---|---|---|---|
| TR-021 | `release_audit` | `paused` | `approve` (audit reject) | `runtime_approve:%s@%d` |
| **TR-027** | `awaiting_human_release` | `bug_resolution/investigation` | `reject_defect` | `runtime_release:%s@%d` ← affected |
| TR-028 | `awaiting_human_release` | `acceptance` | `reject_acceptance` | `runtime_acceptance:%s@%d` |
| TR-029 | `awaiting_human_release` | `release_audit` | `reject_release_audit` | `runtime_release_audit:%s@%d` |
| TR-030 | `awaiting_human_release` | `aborted` | `abort` | `runtime_abort:%s@%d` |

TR-022 (bug_resolution → verification) is **not affected** — does not require human_decision evidence.

---

## Impact

- **Blocks** the formal S8 → S10 → S11 path for any autonomous BUG investigation requiring re-entry from awaiting_human_release via TR-027.
- **Affects 5 of 6** human-decision CLI verbs (TR-021, TR-027, TR-028, TR-029, TR-030).
- **For REQ-004 specifically**: user selected option A (formal S8 re-entry) over option C (skip + lunch-break replace). This defect forced the runtime to remain at `awaiting_human_release` with the BUG-120 fix land-but-unrecognized. The fix is on develop branch (commit `051360e`, 202/0/1 tests pass) and will land via master merge regardless, but the formal S8/S10 evidence trail will be missing from `runtime.entities[]` / `runtime.evidence[]` / `runtime.lifecycle[]` — operators downstream lose observability of the BUG-120 closure provenance.
- **Contradicts spec**: `docs/agent-protocol.md#s11` describes `runtime human-decision` as "submit exactly one finite disposition with the explicit Runtime command" — implying the command is intended to be scriptable.

---

## Proposed fixes (4 options, recommendation at end)

**Option 1 (schema fix)**: change the embedded schema pattern from `^runtime_[a-z]+$` to accept either the verb-only form OR the full composed string:
```jsonc
"pattern": "^(runtime_[a-z]+|runtime_[a-z]+:loop-[A-Za-z0-9_-]+@[0-9]+)$"
```
Requires updating both schema definition and runtime check to compare against either form.

**Option 2 (runtime fix)**: in the scope check, only verify that the evidence's recorded verb matches the harness's expected verb; ignore the runtime_id and revision suffix. Treat the verb as the authorization granularity.

**Option 3 (CLI fix)**: add a new flag `--ignore-scope` to `runtime human-decision` that skips the file-vs-runtime scope check, with a warning logged. Autonomous operators bypass; human operators still get full verification.

**Option 4 (atomic verb)**: redesign `runtime evidence add` + `runtime transition` to allow a single in-memory commit where the file content is registered atomically with the transition (no intermediate journal entry / revision bump). Requires a new verb like `runtime human-decision-and-evaluate`.

**Recommendation**: Option 1 + Option 3 — accept the verb-only form AND provide an explicit bypass flag for autonomous operators. Lowest risk, backward-compatible.

---

## Recommended test (post-fix)

In `.claude/bin/loop-harness` test suite:
```rust
#[test]
fn human_decision_scope_verb_only_accepted() {
    // 1. construct file with human_decision_scope: "runtime_release"
    // 2. register via runtime evidence add
    // 3. invoke runtime human-decision --disposition reject_defect
    // 4. assert: transition succeeds; recorded sha256 unchanged
    // 5. assert: lifecycle.phase moves from awaiting_human_release → bug_resolution
}
```

---

## Workarounds (for current ops)

The workaround options below are retained as the historical incident record.
They are superseded for the current source by the registration sequence in the
investigation disposition above; do not weaken or bypass the scope check.

Until the harness fix lands, three viable paths:

| Path | Action | Consequence |
|---|---|---|
| **A** (chosen for REQ-004 S11) | defer formal S8 re-entry; treat BUG-120 fix as release artifact; merge into master via human-decision approve (TR-025) | skips S8 evidence trail but ships the fix |
| **B** | operator manually invokes `runtime human-decision` from their own shell at the exact rev transition point | 1-revision race window; feasible but error-prone |
| **C** | undocumented `runtime transition` API to skip scope check | unverified; not exposed in current build |

For REQ-004: path A is the lowest-friction option; BUG-120 fix on develop branch (`commit 051360e`) is ready to merge, and the formal S8 trail can be reconstructed post-hoc from `.claude/evidence/loop-REQ-004/g2/bugs/bug-120-canonical.json` + completion + reverify records.

---

## Cross-references

- BUG-120 (REQ-004, production discovery): `.claude/evidence/loop-REQ-004/g2/bugs/bug-120-canonical.json`
- BUG-120 fix commit: `051360e` on `integration-develop/develop`
- BUG-120 evidence: `.claude/evidence/loop-REQ-004/g2/{bugs,reverify,impact}/`
- This defect report (JSON): `.claude/evidence/loop-REQ-004/g2/harness-defects/HARNESS-DEFECT-001.json`
- This defect report (markdown): `.claude/evidence/loop-REQ-004/g2/harness-defects/HARNESS-DEFECT-001.md`
- Harness binary: `.claude/bin/loop-harness` (binary, schema embedded at offsets 5325787 + 5329975)
