# Bounded repair autonomy

This is opt-in authority for ordinary S8 → S9 repairs, not a replacement for
quality gates. Existing REQs have no delegation by default. It does not waive
S2 ADR sign-off, locked REQ changes, explicit destructive-operation permission,
review budgets, complete S7, or release approval. Do not promise fully unattended
execution merely because a repair grant exists.

## Authority choices

Prefer the reusable project policy below when the owner has approved installation
policy. The following per-REQ grant is the alternative for projects without that
policy; do not demand both forms for the same repair.

## Alternative: one human grant per intended bounded scope

At REQ startup, after binding, the human may authorize the completed grant in
`docs/examples/autonomy/repair-delegation.json`. Replace every placeholder and
agree the finite scope, reviewer, expiry and contract budget before recording
it. The grant's approved_by must match the bound REQ's recorded approver;
reviewer must be a different explicit Driver identity. An account name alone
is not permission. Do not manufacture grants from an earlier generic task.

Register the agreed bytes through the existing Writer:

```sh
loop-harness runtime evidence add --root . --id <decision-id> \
  --kind human_decision --path <grant.json> --produced-by <human> \
  --scope-ref s8_repair_delegation:<runtime-id>
```

The scope has no Runtime revision suffix. Binding includes runtime_id, the
locked REQ SHA and baseline generation. A changed requirement/generation needs
fresh authorization. Paths are literal repository-relative files/directories;
no globs, traversal, symlink components or repository-root wildcard. The whole
prospective_scope must fit: a broad directory overlapping a forbidden child
must be narrowed. Locked specification/governance paths are never authorized
by this repair mechanism. Use conservative module-local grants, not the whole
repository. If scope is insufficient, prepare one complete revised decision
package rather than asking for file-by-file permission.

## Driver technical review per contract

Finish the Case causal closure and contract scope preflight first. Delegated activation also checks explicit per-unit assertion ownership and dependency validity before spending a grant use, so missing assertion_ids or a dependency cycle cannot first surface at S9 plan compilation. Use
`docs/examples/autonomy/repair-technical-review.json`, citing real registered,
current-generation, SHA-verified supporting evidence. The four booleans are
review conclusions, not automatic semantic proofs; explain the entry path,
transaction boundaries, compatibility, complete file scope and reversibility.
Unknown or changed business semantics cannot use this route.

```sh
loop-harness runtime evidence add --root . --id <review-id> \
  --kind repair_contract_review --path <review.json> --produced-by <driver> \
  --scope-ref s8_contract_review:<case-id>
loop-harness runtime investigation contract approve --root . \
  --case-id <case-id> --file <contract-draft.json> --approved-by <driver> \
  --approval-hash <draft-sha256> --approval-evidence-id <review-id> \
  --delegation-evidence-id <grant-id>
```

The approved artifact records both the human grant provenance and the Driver
review identity. The Writer revalidates the grant and increments its use count
atomically with approval under configuration.repair.delegation_uses. Each new
contract approval spends one use. The grant is intentionally reusable within
its budget and expiry; it is not consumed as a one-shot approve_contract receipt.
Invalid/revoked evidence cannot authorize a new contract. Existing approved
contracts remain immutable historical authority; invalidating a grant alone
is not an emergency cancellation of an already active repair session.

Without --delegation-evidence-id the existing human approval path is unchanged.
Neither a grant nor a technical review is a release decision. Do not import a
technical review through recovery as trusted authority.

## Retries and escalation

Retrying the same approved Case/draft hash/actor/evidence IDs returns the pinned
approval without changing Runtime or spending another use. Changed content or
identity must use causal reassessment, not overwrite. A supplied stale CAS
assertion still fails; normal callers omit expected-revision. Old approvals
without the new provenance field are consumed from their current pointer,
not retroactively re-signed to enable this retry feature.

Expired/exhausted/out-of-scope/missing authority requires a human decision or
an ordinary explicit contract approval. Do all independent technical work
first. Do not auto-renew expiry or budget. Ordinary test failures still follow
the repair cycle and a fresh complete S7 after repair.

## Compatibility and rollout

Ordinary Builder manifests retain pre-and-post merge checks. Select integration_check_mode=post_merge only with an explicit reviewed plan recording its reason, cost tradeoff and failure recovery; it is not a global default.
Omission retains legacy pre-and-post checks; never edit an active manifest to
change it. The new mode separates static inspection from mandatory merged-tree
delivery checks. No pass evidence cache or reduced test inventory is introduced.

Install at a quiescent boundary before the next REQ; preserve project Skills.
New authority metadata/evidence kinds are not guaranteed readable by old binaries.
Rollback requires a compatible runtime snapshot/journal and control assets, not
just replacing the executable. Do not overwrite an active REQ workspace; complete or explicitly migrate its recorded execution first.

## Reusable project policy pinned at new REQ binding

To avoid authoring a human grant for each routine repair, the project owner may
approve one versioned policy at installation. It contains `version: "1"`,
`approved_by`, independent `reviewer`, literal `allowed_paths` and
`forbidden_paths`, a positive per-REQ `max_contracts`, and optional `expires_at`.
No enabled policy is shipped by default. This is explicit installation policy,
not inferred permission from a username. The policy cannot authorize locked
requirements, framework governance, irreversible work or release.

After the human locks a new REQ, the Driver mechanically includes the already
approved policy path and digest with binding:

```
loop-harness req bind --req <locked-req> --approved-by <human> \
  --repair-policy <approved-project-policy> --repair-policy-sha256 <exact-sha>
```

The Writer pins policy path/SHA, approver, REQ SHA, generation and Runtime ID in
configuration.repair.bound_policy. Existing runtimes are not migrated. Changing
policy bytes or amending the REQ invalidates new approvals under that pin; a
budget cannot auto-renew. An omitted expiry means this bound REQ's lifecycle,
not unlimited authority across requirements. Explicit expiry remains enforced.

The Driver still produces a real `repair_contract_review`, with its
`delegation_evidence_id` set to `binding-policy:<policy-sha>`, and invokes the
existing approval command with that same `--delegation-evidence-id`. The policy
is a binding authority reference, not a fabricated human_decision evidence row.
All independent technical review, protected paths, scope, budget, CAS and
idempotent approval rules above remain enforced. A repeated request to approve
an ordinary in-scope technical repair is unnecessary; changed business semantics
or unavailable authority still requires the proper Gateway.
