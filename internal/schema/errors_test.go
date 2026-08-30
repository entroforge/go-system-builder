package schema_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/entroforge/go-system-builder/internal/schema"
)

// completionReport returns a valid completion_report envelope; drop removes
// one hard-required base field to force a oneOf failure. (Extension fields
// are only warn-tier — see WarnMissingExtensionFields — so dropping one of
// them no longer produces a hard validation error.)
func completionReport(drop string) map[string]any {
	message := map[string]any{
		"schema_version":            "1.0.0",
		"message_type":              "completion_report",
		"message_id":                "msg-1",
		"correlation_id":            "corr-1",
		"runtime_id":                "loop-REQ-001",
		"expected_runtime_revision": 1,
		"agent_id":                  "agent-qa-1",
		"agent_definition_ref":      "agents/qa.md",
		"task_id":                   "TASK-001",
		"bug_id":                    nil,
		"team_id":                   nil,
		"occurred_at":               "2026-08-22T00:00:00Z",
		"activation_id":             "act-1",
		"status":                    "completed",
		"summary":                   "all checks pass",
		"changed_paths":             []any{},
		"reviewed_paths":            []any{},
		"checks":                    []any{},
		"evidence_refs":             []any{},
		"finding_refs":              []any{},
		"remaining_risks":           []any{},
		"scope_deviations":          []any{},
		"requested_event":           "completion_reported",
	}
	delete(message, drop)
	return message
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestOneOfPruningReportsClosestBranchOnly(t *testing.T) {
	validator := schema.NewEmbeddedValidator()
	data := mustJSON(t, completionReport("correlation_id"))

	err := validator.ValidateBytes("agent-message.schema.json", data)
	if err == nil {
		t.Fatal("expected a validation error")
	}
	pruned := err.Error()

	// The real missing field survives pruning.
	if !strings.Contains(pruned, "correlation_id") {
		t.Errorf("pruned error must still name the missing field, got:\n%s", pruned)
	}
	// The identified branch is named; irrelevant branches are not expanded.
	if !strings.Contains(pruned, `"completionReport"`) {
		t.Errorf("pruned error should name the closest branch, got:\n%s", pruned)
	}
	for _, other := range []string{"readback_request", "plan_report", "shutdown_approval"} {
		if strings.Contains(pruned, "must be \""+other+"\"") {
			t.Errorf("pruned error should not expand ruled-out branch %q, got:\n%s", other, pruned)
		}
	}
	if !strings.Contains(pruned, schema.SchemaVerboseEnv) {
		t.Errorf("pruned error should point at the verbose escape hatch, got:\n%s", pruned)
	}

	// The full error is one env var away and is much longer.
	t.Setenv(schema.SchemaVerboseEnv, "1")
	fullErr := validator.ValidateBytes("agent-message.schema.json", data)
	if fullErr == nil {
		t.Fatal("expected a validation error in verbose mode")
	}
	full := fullErr.Error()
	if !strings.Contains(full, "readback_request") {
		t.Errorf("verbose mode must keep the full per-branch enumeration, got:\n%s", full)
	}
	if len(pruned) >= len(full)/2 {
		t.Errorf("pruned error (%d bytes) should be significantly shorter than the full error (%d bytes)", len(pruned), len(full))
	}

	// The unwrap chain still exposes the library error type.
	var ve *jsonschema.ValidationError
	if !errors.As(err, &ve) {
		t.Error("pruned error must unwrap to *jsonschema.ValidationError")
	}
}

func TestOneOfPruningFallsBackToBranchSummary(t *testing.T) {
	validator := schema.NewEmbeddedValidator()
	message := completionReport("")
	message["message_type"] = "explosion_report" // no branch's discriminator accepts this
	data := mustJSON(t, message)

	t.Setenv(schema.SchemaVerboseEnv, "1")
	fullErr := validator.ValidateBytes("agent-message.schema.json", data)
	if fullErr == nil {
		t.Fatal("expected a validation error in verbose mode")
	}

	t.Setenv(schema.SchemaVerboseEnv, "")
	err := validator.ValidateBytes("agent-message.schema.json", data)
	if err == nil {
		t.Fatal("expected a validation error")
	}
	pruned := err.Error()

	if !strings.Contains(pruned, "no target branch is identifiable") {
		t.Errorf("expected the undecidable summary, got:\n%s", pruned)
	}
	// The truncated summary lists branch names without their full trees.
	for _, branch := range []string{"readbackRequest", "completionReport", "lifecycleEvent"} {
		if !strings.Contains(pruned, branch) {
			t.Errorf("summary should list branch %s, got:\n%s", branch, pruned)
		}
	}
	if !strings.Contains(pruned, schema.SchemaVerboseEnv) {
		t.Errorf("summary should point at the verbose escape hatch, got:\n%s", pruned)
	}
	if len(pruned) >= len(fullErr.Error())/2 {
		t.Errorf("summary (%d bytes) should be significantly shorter than the full error (%d bytes)", len(pruned), len(fullErr.Error()))
	}
}

func TestNonOneOfFailuresPassThroughUnchanged(t *testing.T) {
	validator := schema.NewEmbeddedValidator()
	// team-manifest with a bad manifest_id pattern: a plain leaf failure,
	// not a root oneOf — the error text must not be rewritten.
	manifest := map[string]any{"manifest_id": "bogus"}
	prunedErr := validator.ValidateBytes("team-manifest.schema.json", mustJSON(t, manifest))
	if prunedErr == nil {
		t.Fatal("expected a validation error")
	}
	t.Setenv(schema.SchemaVerboseEnv, "1")
	verboseErr := validator.ValidateBytes("team-manifest.schema.json", mustJSON(t, manifest))
	if prunedErr.Error() != verboseErr.Error() {
		t.Errorf("non-oneOf errors must pass through unchanged:\npruned:  %s\nverbose: %s", prunedErr, verboseErr)
	}
	if strings.Contains(prunedErr.Error(), "closest to branch") {
		t.Errorf("non-oneOf error must not mention branch pruning, got:\n%s", prunedErr)
	}
}

// TestPruningLocatesNestedDiscriminatorOneOf proves the walker can find a
// oneOf failure even when it sits below the top-level *kind.Schema wrapper
// the library always inserts. The agent-message envelope is the canonical
// case: the real-world schema nests the discriminator split one level deep
// inside the root marker, and the older "root-level only" implementation
// missed it for the realistic missing-field case. Here the instance omits
// `correlation_id` (a base field required by every branch) while keeping
// message_type=completion_report, so the only failing branch is
// completionReport and the pruned output must highlight it.
func TestPruningLocatesNestedDiscriminatorOneOf(t *testing.T) {
	validator := schema.NewEmbeddedValidator()
	data := mustJSON(t, completionReport("correlation_id"))

	t.Setenv(schema.SchemaVerboseEnv, "1")
	verboseErr := validator.ValidateBytes("agent-message.schema.json", data)
	if verboseErr == nil {
		t.Fatal("expected a validation error in verbose mode")
	}
	verbose := verboseErr.Error()

	t.Setenv(schema.SchemaVerboseEnv, "")
	prunedErr := validator.ValidateBytes("agent-message.schema.json", data)
	if prunedErr == nil {
		t.Fatal("expected a validation error")
	}
	pruned := prunedErr.Error()

	if !strings.Contains(pruned, "completionReport") {
		t.Errorf("pruned output must name the matching branch, got:\n%s", pruned)
	}
	if !strings.Contains(pruned, "correlation_id") {
		t.Errorf("pruned output must still call out the missing field, got:\n%s", pruned)
	}
	if !strings.Contains(pruned, schema.SchemaVerboseEnv) {
		t.Errorf("pruned output must always expose the escape hatch, got:\n%s", pruned)
	}
	// The pruned output must be at least 3x smaller than the verbose
	// rendering; the verbose form expands every branch's full tree.
	if len(pruned)*3 >= len(verbose) {
		t.Errorf("pruned output (%d bytes) should be much shorter than verbose (%d bytes)",
			len(pruned), len(verbose))
	}
	// And the verbose form really does carry the per-branch fan-out.
	if !strings.Contains(verbose, "readback_request") {
		t.Errorf("verbose form must carry the per-branch enumeration, got:\n%s", verbose)
	}

	// Unwrap chain still resolves to the library error.
	var ve *jsonschema.ValidationError
	if !errors.As(prunedErr, &ve) {
		t.Error("pruned error must unwrap to *jsonschema.ValidationError")
	}
}

// TestPruningLocatesNestedDiscriminatorAnyOf proves the walker also finds
// anyOf failures nested below the root marker. The review-result schema's
// verdict=finding anyOf (findings vs. blocked_claims) sits inside an
// allOf/if/then, not at the document root, and the older implementation
// skipped it entirely. Submitting a verdict=finding ReviewResult with no
// findings and no blocked_claims triggers the anyOf; the pruned output
// must name both candidate branches and point at the escape hatch.
func TestPruningLocatesNestedDiscriminatorAnyOf(t *testing.T) {
	validator := schema.NewEmbeddedValidator()
	// Build a minimal review-result that fails the verdict=finding anyOf:
	// the required envelope fields are present, but neither findings nor
	// blocked_claims has minItems: 1.
	reviewResult := map[string]any{
		"schema_version":      "1.0.0",
		"result_id":           "review-result-test-001",
		"assignment_id":       "assignment-test-001",
		"assignment_revision": 1,
		"review_plan_id":      "review-plan-test-001",
		"review_round":        1,
		"baseline_generation": 1,
		"producer_agent_id":   "agent-qa-1",
		"subject_digest":      "0000000000000000000000000000000000000000000000000000000000000000",
		// claim_results is required at the top level; keep it non-empty so
		// the second anyOf passes. The first anyOf (verdict=finding) is
		// the one that fails zero-match: both findings and blocked_claims
		// are present-but-empty, so neither minItems: 1 branch succeeds.
		"claim_results": []any{
			map[string]any{
				"claim_id":      "claim-test-1",
				"conclusion":    "fail",
				"observed":      "observed something",
				"evidence_refs": []any{},
			},
		},
		"findings":       []any{},
		"blocked_claims": []any{},
		"verdict":        "finding",
	}
	data := mustJSON(t, reviewResult)

	t.Setenv(schema.SchemaVerboseEnv, "")
	prunedErr := validator.ValidateBytes("review-result.schema.json", data)
	if prunedErr == nil {
		t.Fatal("expected a validation error")
	}
	pruned := prunedErr.Error()

	// Pruning must (a) name the anyOf keyword, (b) surface both candidate
	// branches, (c) point at the escape hatch — these are the criteria the
	// user-facing message needs to satisfy.
	if !strings.Contains(pruned, "anyOf") {
		t.Errorf("pruned output must name the anyOf failure, got:\n%s", pruned)
	}
	if !strings.Contains(pruned, "/findings") {
		t.Errorf("pruned output must surface the findings branch failure, got:\n%s", pruned)
	}
	if !strings.Contains(pruned, "/blocked_claims") {
		t.Errorf("pruned output must surface the blocked_claims branch failure, got:\n%s", pruned)
	}
	if !strings.Contains(pruned, schema.SchemaVerboseEnv) {
		t.Errorf("pruned output must always expose the escape hatch, got:\n%s", pruned)
	}

	// Sanity: the verbose form would have buried the failure under two
	// layers of "allOf → anyOf" wrapping; the pruned form leads with the
	// anyOf.
	if strings.HasPrefix(pruned, "validate data: jsonschema validation failed") {
		t.Errorf("pruned output should not pass through the verbose wrapper, got:\n%s", pruned)
	}

	var ve *jsonschema.ValidationError
	if !errors.As(prunedErr, &ve) {
		t.Error("pruned error must unwrap to *jsonschema.ValidationError")
	}
}

// TestAnyOfPartialMatchIsNotPruned proves the walker does NOT touch anyOf
// failures where at least one branch DID match — pruning those would mask
// the very real per-branch errors a partial match surfaces. We trigger a
// partial-match anyOf by giving the verdict=finding ReviewResult a
// findings[] array with minItems=1 satisfied but a finding item that fails
// its inner schema: the anyOf branch passes (findings satisfied) but the
// findings array's items produce their own per-branch failures. The error
// text must pass through unchanged.
func TestAnyOfPartialMatchIsNotPruned(t *testing.T) {
	validator := schema.NewEmbeddedValidator()
	reviewResult := map[string]any{
		"schema_version":      "1.0.0",
		"result_id":           "review-result-test-002",
		"assignment_id":       "assignment-test-002",
		"assignment_revision": 1,
		"review_plan_id":      "review-plan-test-002",
		"review_round":        1,
		"baseline_generation": 1,
		"producer_agent_id":   "agent-qa-1",
		"subject_digest":      "0000000000000000000000000000000000000000000000000000000000000000",
		"claim_results": []any{
			map[string]any{
				"claim_id":      "claim-test-1",
				"conclusion":    "fail",
				"observed":      "observed something",
				"evidence_refs": []any{},
			},
		},
		// finding item missing required fields → finding branch fails too,
		// but its anyOf still passes (minItems: 1 is satisfied by the
		// empty-shaped entry). The library renders the per-item error;
		// pruning must not rewrite that.
		"findings": []any{
			map[string]any{"finding_id": "finding-bogus"},
		},
		"verdict": "finding",
	}
	data := mustJSON(t, reviewResult)

	t.Setenv(schema.SchemaVerboseEnv, "1")
	verboseErr := validator.ValidateBytes("review-result.schema.json", data)
	if verboseErr == nil {
		t.Fatal("expected a validation error in verbose mode")
	}
	verbose := verboseErr.Error()

	t.Setenv(schema.SchemaVerboseEnv, "")
	prunedErr := validator.ValidateBytes("review-result.schema.json", data)
	if prunedErr == nil {
		t.Fatal("expected a validation error")
	}
	pruned := prunedErr.Error()

	// Partial-match anyOf must NOT be rewritten: text must match verbose.
	if pruned != verbose {
		t.Errorf("partial-match anyOf must pass through unchanged:\npruned:  %s\nverbose: %s", pruned, verbose)
	}
	if strings.Contains(pruned, "closest to branch") {
		t.Errorf("partial-match anyOf must not be rewritten as discriminator pruning, got:\n%s", pruned)
	}
}
