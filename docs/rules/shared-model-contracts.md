# Shared models and progressive contract reading

## Authority and order

REQ owns goals; S2 owns domain meaning, rules and states. Shared models own data definitions; SYNC owns operations, timing, errors and recovery. FE/BE own implementation responsibilities; TASK owns the current execution scope. Refine upstream facts; never override them in a downstream contract.

Decide FE, BE and SYNC applicability independently from actual responsibilities.
For a real shared operation, converge its applicable shared model and SYNC
behavior together, then finalize the participating FE/BE responsibilities.
FE-only local behavior or BE-only internal work may need no SYNC; a real SYNC
boundary may have no separate FE/BE contract. With no shared data use the
`none` policy below without creating Schema/examples. This order does not
require creating Builder agents or delivering all three contract types. Keep one current project model, not a model copy per REQ. Reuse existing native schemas. S3 may create or refine new definitions within
S2 domain meaning; missing structural detail is S3 work. Changing frozen S2
content or source membership requires formal design rework; changing REQ
semantics requires requirement amendment. Domain, wire and storage/view projections are responsibilities, not three mandatory files.

## Executable first-version contract

A new CONTRACTS index declares `> Shared model policy: json-schema-v1` and an H2 `Shared model baseline` table with these exact columns:

| Operation | Slot | Schema | Consumers | Valid example | Structural negative |
|:---|:---|:---|:---|:---|:---|
| cancelOrder | request | Markdown link to the authoritative JSON Schema, optionally with fragment | Markdown links to consuming FE/BE/SYNC contracts | Markdown link to a valid JSON file | Markdown link to a structurally invalid JSON instance, or N/A |

Use an existing operation name and data slot (request, response-200, etc.). Each operation/slot has one authoritative source across all indexes for the same REQ; repeating the same source is allowed. Schema resource identifiers must not identify different authoring sources in that batch. Every listed consumer includes the exact same schema reference in its H2 `Shared model inputs` section. Paths are relative to the Markdown file, not the repository root. References do not duplicate the definition itself.

This adapter validates JSON Schema using local files; it does not claim to validate arbitrary OpenAPI or Proto. Pin remote dependencies locally; no network resource is loaded during a gate. Use native schema recursion, which is legal. A valid example is required; a structural negative is optional but must actually fail schema validation. Malformed JSON is not a structural test instance. Business negatives (authorization/state/concurrency) are valid data and belong in CASE/CT, not the structural-negative column.

If shared data is not applicable, declare `> Shared model policy: none` and a concrete `> Shared model reason: ...`. Old indexes without a policy produce a legacy warning: they are not verified by this mechanism. New planning and independent S5 review require an explicit applicable policy. Other protocol formats require a supported adapter or an explicitly reviewed legacy limitation, never a false `none` declaration.

`contracts check` is an authoring check on disk, available for autonomous
self-review and diagnosis without an extra submission obligation. Formal S3 checks read the declared committed Git view. S3 registers locally loaded schema dependencies and sample files as design documents. S5 review subjects include them; freezing rejects drift or a silently reduced input set. Model hashes are not mutable evidence hashes.

## Reading and execution

A new TASK declares `> Reading policy: linked-v1`. Its Document Manifest retains Order, Kind, ID, Path and Clauses, and adds Purpose and Mode. Path is a Markdown file link; Mode is `required`, `conditional` or `optional`. Required rows must resolve. Conditional rows state the condition in Purpose and must be checked/read when that condition applies; they are not automatically proven by the checker.

Start with TASK goal, scope, dependencies and closing assertions. Read the linked local contract clause, applicable SYNC operation and shared definition, then only the relevant state/scenario explanation. Omit inapplicable SYNC/model entries with their sourced N/A basis; preserve actual clauses and CASE obligations. Use stable explicit anchors where possible. Do not recursively read every background link. Links may point back; only execution prerequisites form a DAG. The existing TASK dependency checker owns that DAG.

For contract authors, enter through CONTRACTS; reviewers work back to REQ/S2; integration verifiers start from SYNC/CASE. Downstream documents declare their upstream references. Query existing CONTRACTS/TASK indexes for consumers instead of updating locked contracts with future task/evidence rows.

## Actual consumers and change

A model_ref alone is not implementation. Use the project's generator or boundary validator in the actual consumers,
providers and applicable Mock; fix tool versions and configuration. This does
not require creating a front end or Mock when neither exists. Check generated outputs only when generation exists. Independent review checks whether the model itself matches the requirement, and real integration checks timing, state, persistence and recovery.

Split a foundation TASK only for a real shared implementation dependency. Independent generation into separate output paths may run concurrently. Merge a required shared implementation back into the bound development branch before creating dependent worktrees. Shared design identity compares the referenced file closure, not whole checkout commit equality.

Substantive changes to accepted models/protocols or frozen upstream sources
follow [change control](change-control.md); re-review affected subjects rather
than refreshing their hashes as evidence. Current unreceived S3 outputs remain
editable even when the author has marked their documents `locked`; complete
self-review and required Git delivery before saving the original answer. Worker integration consumes the Runtime frozen-document set and rejects modifications, deletions and renames of those files before merging. Assignment write scope and required checks do not override that set. Preserve [scenario truth](scenario-model.md) and existing CASE/AC coverage; do not create a second test denominator.

For a runnable, explicitly synthetic consumer example, see [the shared-model pilot](../examples/shared-model/README.md). It demonstrates this adapter; it does not certify a target product.

For current S3/S4 professional corrections, use the explicit
[planning rework boundary](../control/agent-protocol.md#s3): `--target design`
for frozen S2 corrections, or `--target contracts` for accepted contract
corrections with unchanged S2. Technical `issue`/`recover` continuation does
not revise accepted judgments. Unreceived outputs remain ordinarily editable.
