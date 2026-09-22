# Shared models and progressive contract reading

## Authority and order

REQ owns goals; S2 owns domain meaning, rules and states. Shared models own data definitions; SYNC owns operations, timing, errors and recovery. FE/BE own implementation responsibilities; TASK owns the current execution scope. Refine upstream facts; never override them in a downstream contract.

Converge the shared model and SYNC together for the affected business slice, then finalize FE/BE in parallel. Keep one current project model, not a model copy per REQ. Reuse existing native schemas. Domain, wire and storage/view projections are responsibilities, not three mandatory files.

## Executable first-version contract

A new CONTRACTS index declares `> Shared model policy: json-schema-v1` and an H2 `Shared model baseline` table with these exact columns:

| Operation | Slot | Schema | Consumers | Valid example | Structural negative |
|:---|:---|:---|:---|:---|:---|
| cancelOrder | request | Markdown link to the authoritative JSON Schema, optionally with fragment | Markdown links to consuming FE/BE/SYNC contracts | Markdown link to a valid JSON file | Markdown link to a structurally invalid JSON instance, or N/A |

Use an existing operation name and data slot (request, response-200, etc.). Each operation/slot has one authoritative source across all indexes for the same REQ; repeating the same source is allowed. Schema resource identifiers must not identify different authoring sources in that batch. Every listed consumer includes the exact same schema reference in its H2 `Shared model inputs` section. Paths are relative to the Markdown file, not the repository root. References do not duplicate the definition itself.

This adapter validates JSON Schema using local files; it does not claim to validate arbitrary OpenAPI or Proto. Pin remote dependencies locally; no network resource is loaded during a gate. Use native schema recursion, which is legal. A valid example is required; a structural negative is optional but must actually fail schema validation. Malformed JSON is not a structural test instance. Business negatives (authorization/state/concurrency) are valid data and belong in CASE/CT, not the structural-negative column.

If shared data is not applicable, declare `> Shared model policy: none` and a concrete `> Shared model reason: ...`. Old indexes without a policy produce a legacy warning: they are not verified by this mechanism. New planning and independent S5 review require an explicit applicable policy. Other protocol formats require a supported adapter or an explicitly reviewed legacy limitation, never a false `none` declaration.

`contracts check` is an authoring check on disk. Formal S3 checks read the declared committed Git view. S3 registers locally loaded schema dependencies and sample files as design documents. S5 review subjects include them; freezing rejects drift or a silently reduced input set. Model hashes are not mutable evidence hashes.

## Reading and execution

A new TASK declares `> Reading policy: linked-v1`. Its Document Manifest retains Order, Kind, ID, Path and Clauses, and adds Purpose and Mode. Path is a Markdown file link; Mode is `required`, `conditional` or `optional`. Required rows must resolve. Conditional rows state the condition in Purpose and must be checked/read when that condition applies; they are not automatically proven by the checker.

Start with TASK goal, scope, dependencies and closing assertions. Read the linked local contract clause, SYNC operation, shared definition, then only the relevant state/scenario explanation. Use stable explicit anchors where possible. Do not recursively read every background link. Links may point back; only execution prerequisites form a DAG. The existing TASK dependency checker owns that DAG.

For contract authors, enter through CONTRACTS; reviewers work back to REQ/S2; integration verifiers start from SYNC/CASE. Downstream documents declare their upstream references. Query existing CONTRACTS/TASK indexes for consumers instead of updating locked contracts with future task/evidence rows.

## Actual consumers and change

A model_ref alone is not implementation. Use the project's generator or boundary validator in FE, BE and Mock; fix tool versions and configuration. Check generated outputs only when generation exists. Independent review checks whether the model itself matches the requirement, and real integration checks timing, state, persistence and recovery.

Split a foundation TASK only for a real shared implementation dependency. Independent generation into separate output paths may run concurrently. Merge a required shared implementation back into the bound development branch before creating dependent worktrees. Shared design identity compares the referenced file closure, not whole checkout commit equality.

Model/protocol changes follow [change control](change-control.md); re-review affected subjects rather than refreshing their hashes as evidence. Worker integration consumes the Runtime frozen-document set and rejects modifications, deletions and renames of those files before merging. Assignment write scope and required checks do not override that set. Preserve [scenario truth](scenario-model.md) and existing CASE/AC coverage; do not create a second test denominator.

For a runnable, explicitly synthetic consumer example, see [the shared-model pilot](../examples/shared-model/README.md). It demonstrates this adapter; it does not certify a target product.
