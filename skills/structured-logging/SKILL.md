---
name: structured-logging
description: Use when application logging, log schemas, correlation, redaction, audit events, or log retention change
category: best-practice
version: 0.3.0
---
# Structured Logging
## Authority
Quality guidance only. Security and data-handling policy remain in `docs/rules/security.md`; stage routing remains in `docs/agent-protocol.md`.
## Applicability
Apply to application logs, structured fields, correlation IDs, audit records, redaction, log levels, or operational diagnostics.
## Required Inputs
Read operational requirements, data classification, error model, trace/correlation design, and retention policy.
## Quality Criteria
Log actionable context, preserve correlation, avoid secrets and sensitive data, keep event schemas stable, and make severity meaningful.
## Outputs
One observable implementation or scoped logging review conclusion.
## N/A Criteria
N/A when no log-producing path, log schema, or operational diagnostic changes.
## Stop Conditions
Stop on sensitive-data leakage, missing incident correlation, or logging that changes control flow.
## Non-Goals
Do not treat logs as a replacement for user-facing error contracts or metrics.

## Operating Procedure
1. Define the event name, severity, correlation id, safe identifiers, outcome, and retention/classification before adding a log call.
2. Derive a logger from context at request/job entry and group subsystem fields; use typed attributes rather than interpolating unstructured payloads.
3. Apply redaction at the logging boundary and add tests for secret/token/PII-bearing error paths.
4. Verify output schema, sampling/level behavior, and incident correlation without relying on logs for control flow.

## Evidence Checklist
- Event-schema field list, classification/redaction policy, and correlation propagation path.
- Example redacted output for success and failure.
- Tests or inspection evidence for no secret/PII leakage.

## Common Failure Modes
- Request bodies, JWTs, credentials, or raw errors are logged unchanged.
- The same event uses different field names across services and cannot be queried reliably.
- A log call becomes the only observation of a failure while metrics/traces/returned error are absent.

## Primary Sources
- [Go `log/slog`](https://pkg.go.dev/log/slog)
- [Project security rule](../../docs/rules/security.md)
