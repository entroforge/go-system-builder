# Capture provenance contract

The top-level `capture exec` command remains a diagnostic command. It may run in a dirty
checkout, a non-Git directory, or a checkout that produces temporary review
artifacts. The capture buffer records the execution root, the active
ReviewPlan and Assignment when one is available, the start and end hashes of
the plan's frozen subjects, and the start/end Git commit when Git can report
one. A capture without a current plan is marked diagnostic-only.

`runtime review-result submit` validates a `command_output:` reference against
its persisted file, digest, byte count, and truncation metadata. A withheld or
truncated stream, an environment-presence marker, and an artifact change
marker are observation metadata; they cannot certify a passing Claim. A PASS
that cites an automatic command-output artifact must also provide the capture
buffer and matching provenance. The captured frozen subjects must match the
registered ReviewPlan at both ends of the command window, and a commit change
during the command rejects that PASS. Dirty control-plane, documentation,
report, and temporary review-artifact surfaces remain allowed; undeclared
product files in the execution checkout are recorded as product drift and
reject that PASS.

Older hand-authored `capture step` JSON remains readable. Hash-bound manual
`path:` evidence under ordinary repository surfaces continues to support
diagnostic and review flows. A path rewritten to point into the automatic
capture `exec` surface is treated as automatic evidence and cannot bypass the
capture provenance check by omitting `--captures`.
