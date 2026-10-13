# Related-module verification in S7

ReviewPlan 1.1 prevents a REQ's prototype links from silently becoming the
entire regression scope. Changing a provider brings its declared consumers
into assessment, including transitive consumers. This preserves the existing
parallel Assignment scheduler, capacity policy, static/behavior waves and
resource locks. It does not combine QA and E2E roles or defer S7 across REQs.

## Stable project declaration

During S2, the planning Agent creates `docs/architecture/module-relations.json`
from the project's architecture, contracts and module inventory. Start with
[the worked template](architecture/module-relations-template.json), replace the
example modules/resources, and retain the complete existing project inventory.
The installer ships only the template; it does not install a fake empty graph.

The S2 design-registration action records the JSON graph as a design document,
with its hash, generation and registering actor. Its declared CASE/flow JSON,
Markdown and HTML inputs are registered as design subjects too, so lowering
CASE required flags without specification approval is detected. S5's existing independent
exact-document-manifest review must include it. S3 contracts and S5 review must
cross-check providers, consumers, ownership and browser applicability. A Git
commit alone is not a specification approval. Keep this declaration stable
between requirements; Agents update it when interfaces or ownership change.
No per-REQ user setting or execution tier is introduced.

- IDs are unique lowercase module names. Use a trailing `/` for directory
  ownership and an exact path for a file. `orders/` does not match `orders-old/`.
- Resources describe shared APIs, types, models, events, components,
  authorization and configuration. Propagation follows provider → consumers;
  it does not depend on the language's import direction.
- Multiple ownership is explicit. Every resource participant must be declared.
- `browser_applicability` is `required`, `not_applicable` or `unknown`.
  The latter two need `applicability_refs` to real specification files.
  `unknown` cannot register an executable round.
- `ui_paths` can identify browser behavior paths explicitly, including UI code
  outside conventional frontend directories. These paths require browser
  applicability and cannot use an integration/no-impact disposition when changed.
- Pure backend modules may omit `cases_path`. A declared but missing CASE
  file is an error. The first version expects flat CASE/flow module directories; nested directories
  produce explicit diagnostics rather than being silently skipped. Required browser modules need required CASEs and explicit
  `PATH-…` entries in each CASE's `flow_refs`.
- Module declarations, referenced contracts and applicability sources must
  match their current-generation registered locked hashes. Existing project
  declarations must never be overwritten with template examples.

## One fixed scope and explicit dispositions

`loop-harness s7 draft --out plan.json` reads the bound development Git view,
REQ baseline (`workspace.bound_commit`), immutable S6 completion envelopes,
S9 change/confirmation evidence, module/CASE inventory and declared sources.
Both old and new paths are considered. Old/new resource edges are unioned, and
S9's pinned seed retains prior-plan provenance and graphs. A removed edge
therefore cannot erase a known consumer. Cycles terminate by visited modules.
The scope denominator and input digest are framework-generated.

The Plan's `module_scope` records provenance, exact candidates, diagnostics and
one disposition per candidate. Claims use `scenario_refs` with `cases_path`,
`case_id` and `path_ids`; reusable E2E assets also need `module_id` and
`cases_path`. Bare `CASE-001` cannot substitute for another module's CASE.

| Disposition | Required work |
| --- | --- |
| `browser_regression` | Required E2E behavior Claims/Assignments cover every existing required browser CASE/PATH for the module. |
| `integration_verification` | Fingerprinted source references; a required independent QA static scope-assessment Claim; required QA behavior verification Claims depending on the assessment. |
| `no_behavior_impact` | Fingerprinted exclusion sources and a required independent QA static scope-assessment Claim. A Planner rationale alone does not pass. |
| `unknown` | Diagnostic authoring state only; registration and dispatch refuse it. |

Scope assessment Claims declare `focus_key: module-scope:<module-id>` and
`source_refs` containing `module:<module-id>`. Verification Claims also carry
that module source. Reuse an appropriate QA Assignment for several assessment
Claims; do not create a new Agent for every exclusion by default. Assessors
cannot self-certify their own locked inputs or cumulative Builder changes.

Direct UI/CASE changes cannot be waived as integration-only or no-impact.
For related modules, a separately checked exclusion is allowed. This is an
assessment candidate set, not an unconditional command to run every module's
whole test library. Inside each selected browser module, the existing full
required CASE/PATH denominator is preserved.

For example, a REQ names only customers, but orders consumes customer status.
Changing the status provider produces customers and orders candidates. The
Planner can arrange their browser regressions in separate parallel behavior
Assignments, or substantiate and independently verify that orders' behavior is
unchanged. Deleting orders from the candidate list is rejected mechanically.

## Missing inputs, drift and completion

An incomplete graph or old baseline expands the known inventory as
`conservative_fallback`. This is a list to assess, not an automatic all-project
browser run. A changed current-generation hash-locked architecture/TASK/dispatch document
without narrower ownership likewise brings all known modules into assessment.
This does not exempt arbitrary Markdown or unapproved files. Unknown ownership,
unregistered Git product changes, unreadable
sources, missing CASE/PATHs and inventory conflicts remain specific
`unresolved_inputs`; an unresolved draft cannot register or dispatch.
Source/import search can suggest missing declarations; it never proves the
absence of business dependencies. The framework guarantees coverage relative
to verified declarations, not omniscient discovery of undeclared business facts.

Run `s7 lint --file plan.json` before registering. Author-only lint cannot
verify Runtime completeness or input freshness. Registration/revision prepare
scope outside the Runtime lock and recheck authority, fixed view and evidence
inside CAS. Input/candidate tampering is rejected. A top-level scope revision
invalidates its validation/assessment Claims even when Claim wording is unchanged.
Retired Results remain invalid immutable history; only freshly consumed replacement
Results can satisfy the revised obligations.
Input drift is checked again at dispatch, Result submission, final consumption,
CleanRound evaluation and production exit gates (including TR-009 and S10).

The final gate verifies hash-pinned consumed Result artifacts, current Claim
responsibilities and passes. Runtime round, coverage-state and workspace pointers
must match the immutable Plan; removing or replacing a workspace pointer cannot
waive its sealed-artifact checks. `status=clean` alone is insufficient. Evidence or
files added inside the authorized verification workspace do not invalidate
scope solely because Git HEAD changed. When the workspace digest changes after
consumption, an earlier Result must pin at least one still-matching typed workspace
path/hash anchor; missing anchors cannot prove that its execution inputs are stable. Movement between preparation and CAS
requires preparing again, including when the concurrent commit was unrelated.

## Compatibility and delivery

New executable rounds require Plan 1.1; Results/Findings retain their existing
1.0 protocol. Historical Plan 1.0 is schema-checked and readable, but cannot
register, dispatch, submit or pass a new completion gate using this binary.
Removing `module_scope` or changing the version to 1.0 is not a waiver.

Install matching binaries, Skills, schemas embedded in the binary, templates
and manuals together. Fresh installation is supported. A controlled existing
installation update is permitted only when inactive and unbound, with backups
and the existing declaration/custom configuration preserved. The installer
refuses overlays. Finish/close legacy active rounds with their matching release;
this release does not provide online active-round migration or Runtime reset.

Some older binaries can load new JSON while ignoring unknown fields: version
numbers cannot revoke an external old writer. Isolate older local executable
paths and retain a matching rollback set. See [the upgrade guide](framework-upgrade.md).
Validation of synthetic plans/results proves protocol and gate behavior;
it does not prove real browser behavior or the completeness of a business model.
