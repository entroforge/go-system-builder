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
   `s10:<type>_manifest:<id>:evidence_ref_missing`.

When that conflict appears in the Hook packet, fix the named ids in the
manifest, revalidate, then register a new fingerprinted envelope — never edit
the old one in place.
