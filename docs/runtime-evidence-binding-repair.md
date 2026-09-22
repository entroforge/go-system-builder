# Repair an S5 evidence ID misbinding

`runtime evidence add` must use the same ID, kind, Runtime, generation, producer
and responsibility as the JSON envelope. In initial S5, omit `--review-round`
and the envelope's `review_round`: `-r3` in a re-signing ID is not S7 round 3.
Bound document reviews are validated before registration. Unbound bootstrap
artifacts retain legacy compatibility. This is binding validation, not proof
that the underlying review is complete or true.

For a previously registered current, valid `document_review` whose ID differs
from the fingerprint-intact JSON envelope, use the narrow maintenance command:

```sh
loop-harness runtime repair-evidence-binding --root . --id <bad-id> > /tmp/evidence-binding-plan.json
# Inspect the plan, preserve a state/journal/artifact backup, test in a copy.
loop-harness runtime repair-evidence-binding --root . --id <bad-id> --apply-plan /tmp/evidence-binding-plan.json
```

The first call only inspects. Application pins the full state hash and revision,
Runtime ID, target envelope, locked REQ and registered document fingerprints;
rechecks under Writer CAS; and journals `EVIDENCE-BINDING-REPAIR`. It changes
only the target's invalidation fields and drops the stale milestone cache.
Original evidence rows, files, other evidence, documents and lifecycle remain.
Writer-owned revision, timestamp and journal metadata advance normally.

Only unpaused S5 before S7 is eligible. Correctly bound evidence, stale plans,
drifted/missing artifacts, other kinds/generations/stages, and replay are rejected.
This command does not grant PASS, re-sign another verifier's result, repair
arbitrary integrity errors, or directly advance TR-003. Run read-only `ready`
to inspect the remaining gate; normal Claude Hooks own subsequent advancement.
Do not delete Runtime rows, rewrite journal history, or deliberately change a
registered artifact to exploit fingerprint mismatch filtering.
