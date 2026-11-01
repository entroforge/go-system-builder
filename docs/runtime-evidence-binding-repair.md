# Repair an S5 evidence ID misbinding

`runtime evidence add` must use the same ID, kind, Runtime, generation, producer
and responsibility as the JSON envelope. S5 records the original responsibility
without a review-round counter; a suffix in an evidence ID is not such a counter.
Bound document reviews are validated before registration. Unbound bootstrap
artifacts retain legacy compatibility. This is binding validation, not proof
that the underlying review is complete or true.

For a previously registered current, valid `document_review` whose ID differs
from its original JSON envelope, use the narrow maintenance command only when
the original responsibility and evidence are available:

```sh
loop-harness runtime repair-evidence-binding --root . --id <bad-id> > /tmp/evidence-binding-plan.json
# Inspect the plan, preserve a state/journal/artifact backup, test in a copy.
loop-harness runtime repair-evidence-binding --root . --id <bad-id> --apply-plan /tmp/evidence-binding-plan.json
```

The first call only inspects. Application uses the existing Runtime revision,
target envelope and original responsibility and journals `EVIDENCE-BINDING-REPAIR`.
The operation uses the existing Runtime revision and original responsibility;
it changes only the target's invalidation fields and drops the stale milestone cache.
Original evidence rows, files, other evidence, documents and lifecycle remain.
Writer-owned revision, timestamp and journal metadata advance normally.

Only the actual unpaused S5 stage is eligible. Correctly bound evidence, stale plans,
drifted/missing artifacts, other kinds/generations/stages, and replay are rejected.
This command does not grant PASS, re-sign another verifier's result, repair
arbitrary integrity errors, or directly advance TR-003. Run read-only `ready`
to inspect the remaining gate; normal Claude Hooks own subsequent advancement.
Do not delete Runtime rows, rewrite journal history, or deliberately change a
registered artifact to change the identity of an existing result.
