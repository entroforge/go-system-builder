# S10 existing Manifest fields

These are fictional **unfinished drafts** illustrating existing fields, not
submission files or evidence of a passing review. Use the exact current Manifest
path supplied for your real task. Do not copy these example paths, IDs or machine
metadata into it.

The system supplies `schema_version`, kind, runtime/generation/round, row identity,
owner, known `expected` and `source_refs`. `expected` is the system-prefilled
original source commitment, often the full material text; preserve that baseline.
Write your actual judgment in `disposition` and observations in `oracle`. It derives metrics and the readable
Markdown projection; authors do not maintain a second report or envelope.

- Row values for `coverage_inventory[].disposition`,
  `counterevidence[].outcome`, and `audit_areas[].conclusion` are
  `pass` / `not_applicable` / `unknown` / `fail`. These are distinct from the
  whole Manifest's `outcome`; do not invent synonyms such as `refuted`.
- `disposition`: your actual conclusion; unobserved work stays `unknown`.
- `oracle`: when useful, actual observations, judgment basis or unfinished cause.
  Do not invent a new standard or a counterexample for every row.
- `evidence_refs`: existing real output/file locations when available. Empty is
  allowed; do not create logs or register IDs just to fill it.
- `na_reason`: one explanation for genuinely inapplicable work. A missing category
  does not require an invented N/A row.
- `outcome`: leave empty for a draft. A final acceptance decision uses `pass` or
  `review_required`; audit uses `approved`, `approved_with_risk` or `blocked`.
  An explicit passing outcome cannot erase reported unfinished work or blockers.

Main collects actual reviewers' results with attribution. Reuse applicable S7
observations; add checks only for specific missing work, contradictions or actual
risks. Necessary independent review still requires real work and notification,
not an owner label. Related counterevidence may reuse the same observation.

The audit example references acceptance rather than repeating its full table.
Eight audit topics are reading prompts, not eight mandatory tasks. If reporting
a topic, name it in `audit_areas[].id` and use `audit_areas[].conclusion` for
`pass` / `not_applicable` / `unknown` / `fail`; retain real `owner` and
`evidence_refs` when available. Related observations or N/A explanations can
reuse the existing coverage row's `oracle` / `na_reason`. Actual risks
need impact, responsibility and handling, not prescribed ticket/recovery spelling.

Save the same supplied current file using edits, scripts or MCP. Draft saving does
not select a negative route; final recording gives actual remaining work or the
original step's next path. Do not manually issue, assemble an evidence envelope,
register evidence, render a second report or wait for another Pre to advance.
S10 completion hands off to S11 for the real human decision; it does not approve release.

## Optional object shapes (fictional examples only)

No actual counterevidence or risks to report? Keep `counterevidence: []` and
`risks: []`. The same observation can be written once in the existing coverage
row's `oracle`; do not duplicate it just to fill `counterevidence`.
If you actually use these arrays, their entries are objects, not strings.
Only professional content is needed below. The prepared coverage rows already
carry identities and source associations; do not invent `id` or copy `inventory_id` to
make an optional entry acceptable. Those fields are not reception requirements.

```json
{
  "counterevidence": [{
    "question": "Does the existing restart observation contradict the conclusion?",
    "evidence_refs": [],
    "outcome": "unknown"
  }],
  "risks": [{
    "severity": "P2",
    "impact": "Rollback instructions do not yet cover the changed data; the release owner must resolve this before release.",
    "owner": "actual release owner",
    "tracking_ref": "",
    "recovery_point": ""
  }]
}
```

Use real content only. The example responsibility is not an assignment.
`tracking_ref` and `recovery_point` may remain empty; do not invent tickets or
recovery evidence. This excerpt adds no required rows or professional duties.
