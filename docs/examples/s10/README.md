# S10 machine manifest examples

These files are copyable starting shapes for the S10 machine ledger. Replace
the runtime, round, source, owner, and evidence references with facts from the
current Runtime and reports; do not treat the example evidence IDs as real.

Validate before registering the human-readable evidence envelope:

```text
loop-harness s10 manifest validate --root <root> \
  --file <manifest.json> --type <acceptance|release_audit>
```

The acceptance example demonstrates the requirement, contract, changed-path,
and counterevidence rows. The release-audit example additionally demonstrates
the complete eight-area audit set. A hard category must remain explicit; use
an evidence-backed `not_applicable` row when it is genuinely out of scope.

Two failure modes to avoid (both observed in the 2026-08-28 walkthrough):

1. **Stale binding** — `runtime_id` / `baseline_generation` / `review_round`
   must be copied from the current `.claude/loop-state.json`. A manifest bound
   to another runtime passes standalone validation but is rejected at
   registration/gate time with a `binding_mismatch` conflict.
2. **Non-verbatim evidence ids** — every `evidence_refs` entry must match an
   existing, valid, current-generation `loop-state.json` evidence[].id
   character-for-character (`repair-handoff-r13-1.json` ≠
   `repair-handoff-r13-1`). A wrong id surfaces as
   a diagnostic naming the rejected `evidence_ref`.

When that conflict appears in the Hook packet, fix the named ids in the
manifest, revalidate, then register a new fingerprinted envelope — never edit
the old one in place.

## Candidate identity and preflight

Use `s10 manifest scaffold --kind acceptance` or `--kind release_audit` to
create an unfinished envelope. Supply an explicit `--outcome` only after the
observations support it: acceptance permits `pass` / `review_required`;
release audit permits `approved` / `approved_with_risk` / `blocked`.
The deprecated `--type accepted|blocked` only maps to release-audit outcomes.
It is not an acceptance template.

After filling the envelope, run:

```text
loop-harness s10 envelope lint --root <root> --file <envelope.json>
```

This validates a proposed registration without writing or locking Runtime.
`artifact_valid` does not imply `transition_ready`. Registration rechecks the
same candidate inside the revision CAS. Production registration, status, gates,
and recovery guards require the bound REQ, pinned S7 plan and declared file
source contract. Deep authority and reference reads use the same file view.
Manifest validation without Runtime is labeled `validation_scope=author_only`;
it makes no claim about registration or release readiness.

Within the historical v1 append model, the newest authorized registration in
the same generation and round is selected before content validation. An
unreadable, drifted or invalidated current candidate blocks consumption; the
reader never falls back to an older PASS. A recovery transition naming an older
ID fails with a selection conflict. An unauthorized responsibility cannot
replace the slot's admitted candidate. Explicit supersedes lineage requires the
future versioned protocol; these reads remain labeled legacy append selection.

`subject_refs=[]` is valid. Non-empty subjects bind registered documents, not
the audit manifest; the latter has its own path/hash fields. A pipeline-created
`repair_handoff` can be cited, but cannot be created with generic evidence add
or recovery import. An execution anchor such as `test://...` is not an S10
Runtime evidence ID.
