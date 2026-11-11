package investigation_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	req039fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestRegisterHypothesisCreatesImmutableCaseRevisionAndHistory(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-2", "finding-1"})
	pointer := investigationPointer(t, fixture)
	oldPath := filepath.Join(fixture.root, filepath.FromSlash(pointer["path"].(string)))
	oldBytes, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}

	snapshot, err := investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, investigation.HypothesisRequest{
		ExpectedRevision: -1,
		CaseID:           "investigation-case-review-plan-r1",
		HypothesisID:     "hypothesis-payload-drift",
		Statement:        "FE and BE payload schemas drift at the serialization boundary",
		Invariant:        "one authoritative payload contract owns field shape",
		Discriminator:    "compare generated client payload with the server DTO",
		ExpectedOutcomes: map[string]any{
			"support": "the field differs at the boundary",
			"refute":  "the field is identical across the boundary",
		},
		SourceFindingIDs: []string{"finding-1", "finding-2"},
		EvidenceRefs:     []string{"evidence://schema-drift"},
		AssignmentID:     "assignment-hypothesis-1",
	})
	if err != nil {
		t.Fatalf("RegisterHypothesis() error = %v", err)
	}
	if snapshot.Revision != 2 {
		t.Fatalf("runtime revision = %d, want 2", snapshot.Revision)
	}
	newPointer := investigationPointerFromState(t, snapshot.State)
	if newPointer["revision"] != float64(2) && newPointer["revision"] != 2 {
		t.Fatalf("case pointer revision = %v, want 2", newPointer["revision"])
	}
	if newPointer["path"] == pointer["path"] {
		t.Fatal("case revision must record its actual new path")
	}
	if _, exists := snapshot.State["review"].(map[string]any)["investigation_baseline_drift"]; exists {
		t.Fatal("Case revision registered an obsolete baseline drift state")
	}
	if string(oldBytes) != string(mustRead(t, oldPath)) {
		t.Fatal("previous Case revision was mutated")
	}

	newPath := filepath.Join(fixture.root, filepath.FromSlash(newPointer["path"].(string)))
	caseBytes := mustRead(t, newPath)
	if err := schema.NewEmbeddedValidator().ValidateBytes("review-investigation-case.schema.json", caseBytes); err != nil {
		t.Fatalf("revised Case schema: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(caseBytes, &document); err != nil {
		t.Fatal(err)
	}
	if len(document["hypotheses"].([]any)) != 1 {
		t.Fatalf("hypotheses = %#v, want one registered hypothesis", document["hypotheses"])
	}
	hypothesis := document["hypotheses"].([]any)[0].(map[string]any)
	if hypothesis["hypothesis_id"] != "hypothesis-payload-drift" || hypothesis["discriminator"] == "" {
		t.Fatalf("unexpected hypothesis = %#v", hypothesis)
	}
	history := document["revision_history"].([]any)
	if len(history) != 1 || history[0].(map[string]any)["revision"] != float64(1) {
		t.Fatalf("revision_history = %#v, want predecessor revision 1", history)
	}
}

func TestRegisterHypothesisRequiresBoundAssignment(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})
	_, err := investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, investigation.HypothesisRequest{
		ExpectedRevision:     -1,
		ExpectedCaseRevision: 1,
		CaseID:               "investigation-case-review-plan-r1",
		HypothesisID:         "hypothesis-unbound",
		Statement:            "the boundary contract is inconsistent",
		Invariant:            "the boundary has one owner",
		Discriminator:        "inspect both sides of the boundary",
		ExpectedOutcomes:     map[string]any{"support": "drift", "refute": "no drift"},
		SourceFindingIDs:     []string{"finding-1"},
		EvidenceRefs:         []string{"evidence://boundary"},
	})
	if err == nil || !strings.Contains(err.Error(), "assignment_id") || !strings.Contains(err.Error(), "assignment- prefix") {
		t.Fatalf("unbound hypothesis error = %v, want Assignment binding guidance", err)
	}
}

func TestRegisterHypothesisRequiresEvidenceRefs(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})
	_, err := investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, investigation.HypothesisRequest{
		ExpectedRevision:     -1,
		ExpectedCaseRevision: 1,
		CaseID:               "investigation-case-review-plan-r1",
		HypothesisID:         "hypothesis-evidence-free",
		Statement:            "the boundary contract is inconsistent",
		Invariant:            "the boundary has one owner",
		Discriminator:        "inspect both sides of the boundary",
		ExpectedOutcomes:     map[string]any{"support": "drift", "refute": "no drift"},
		SourceFindingIDs:     []string{"finding-1"},
		AssignmentID:         "assignment-evidence-free",
	})
	// L3-S8:119 keeps `evidence_refs` a required field ("提出该问题依赖的实际证据").
	// The rejection now surfaces through the Case schema (`minItems`) instead of a
	// bespoke Go message, so the assertion follows the field, not the wording.
	if err == nil || !strings.Contains(err.Error(), "evidence_refs") {
		t.Fatalf("evidence-free hypothesis error = %v, want evidence_refs guidance", err)
	}
	if !strings.Contains(err.Error(), "minItems") && !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("evidence-free hypothesis error = %v, want a minimum-evidence requirement", err)
	}
}

// G18 §3.1/§3.3 reverse assertion: the Runtime write count is not a domain
// gate on the Case revision. The explicit caller binding stays a mechanism —
// a caller that names a revision it no longer holds is still refused — and the
// G18 §2 domain-version clause still refuses a revision that was built on a
// changed Case.
func TestCaseWorkflowRejectsStaleRevisionAndHashDrift(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})
	request := investigation.HypothesisRequest{
		ExpectedRevision:     0,
		ExpectedCaseRevision: 1,
		CaseID:               "investigation-case-review-plan-r1",
		HypothesisID:         "hypothesis-stale",
		Statement:            "the boundary contract is inconsistent",
		Invariant:            "the boundary has one owner",
		Discriminator:        "inspect both sides of the boundary",
		ExpectedOutcomes:     map[string]any{"support": "drift", "refute": "no drift"},
		SourceFindingIDs:     []string{"finding-1"},
		EvidenceRefs:         []string{"evidence://boundary"},
		AssignmentID:         "assignment-hypothesis-stale",
	}
	_, err := investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, request)
	if !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale error = %v, want stale guidance", err)
	}
	if !errors.Is(err, runtime.ErrStaleRevision) {
		t.Fatalf("stale error = %v, want ErrStaleRevision", err)
	}

	// L3-S8:43 and L3-S9:15 remove hash/SHA computation, registration and
	// verification from the Hook and its consumers. Identity, version and
	// idempotency stay internal persistence duties (L3-S8:231), so the stale
	// revision above is still refused; but a subject digest must no longer be a
	// qualification gate for the first question.
	request.ExpectedRevision = 1

	if _, err := investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, request); err != nil {
		t.Fatalf("current revision rejected without a hash gate: %v", err)
	}
}

// G18 §3.1/§4.5 reverse assertion: the Runtime write count is not a gate on
// the professional completion of a Case revision. A caller that omits the
// expectation completes even after the Runtime moved for an unrelated reason,
// while the G18 §2 domain-version clause still refuses a revision that names
// a Case revision the Runtime no longer points at.
func TestCaseRevisionCompletesAfterAnUnrelatedRuntimeMoveAndKeepsTheCaseRevisionAsTheDomainVersion(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})
	moveIntakeRuntimeForAnUnrelatedReason(t, fixture)

	request := investigation.HypothesisRequest{
		ExpectedRevision:     -1,
		ExpectedCaseRevision: 1,
		CaseID:               "investigation-case-review-plan-r1",
		HypothesisID:         "hypothesis-after-runtime-move",
		Statement:            "the boundary contract is inconsistent",
		Invariant:            "the boundary has one owner",
		Discriminator:        "inspect both sides of the boundary",
		ExpectedOutcomes:     map[string]any{"support": "drift", "refute": "no drift"},
		SourceFindingIDs:     []string{"finding-1"},
		EvidenceRefs:         []string{"evidence://boundary"},
		AssignmentID:         "assignment-hypothesis-runtime-move",
	}
	snapshot, err := investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, request)
	if err != nil {
		t.Fatalf("RegisterHypothesis() error = %v", err)
	}
	pointer := investigationPointerFromState(t, snapshot.State)
	if pointer["revision"] != float64(2) && pointer["revision"] != 2 {
		t.Fatalf("case pointer revision = %v, want the Case's own next revision 2", pointer["revision"])
	}

	// G18 §2 domain-version clause: the Case's own revision still refuses a
	// revision that names the Case revision the Runtime no longer points at.
	_, err = investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, investigation.HypothesisRequest{
		ExpectedRevision:     -1,
		ExpectedCaseRevision: 1,
		CaseID:               "investigation-case-review-plan-r1",
		HypothesisID:         "hypothesis-on-a-changed-case",
		Statement:            "the boundary contract is inconsistent",
		Invariant:            "the boundary has one owner",
		Discriminator:        "inspect both sides of the boundary",
		ExpectedOutcomes:     map[string]any{"support": "drift", "refute": "no drift"},
		SourceFindingIDs:     []string{"finding-1"},
		EvidenceRefs:         []string{"evidence://boundary"},
		AssignmentID:         "assignment-hypothesis-changed-case",
	})
	if err == nil || !strings.Contains(err.Error(), "stale Case revision") {
		t.Fatalf("revision of a changed Case error = %v, want the stale Case revision refusal", err)
	}
}

func TestSubmitHypothesisResultRequiresRegisteredHypothesisAndFindingSubset(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1", "finding-2"})
	register := investigation.HypothesisRequest{
		ExpectedRevision:     1,
		ExpectedCaseRevision: 1,
		CaseID:               "investigation-case-review-plan-r1",
		HypothesisID:         "hypothesis-boundary",
		Statement:            "the payload contract drifts at the boundary",
		Invariant:            "one contract owns the payload shape",
		Discriminator:        "compare request and DTO fields",
		ExpectedOutcomes:     map[string]any{"support": "field drift", "refute": "no drift"},
		SourceFindingIDs:     []string{"finding-1", "finding-2"},
		EvidenceRefs:         []string{"evidence://boundary"},
		AssignmentID:         "assignment-boundary",
	}
	if _, err := investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, register); err != nil {
		t.Fatalf("RegisterHypothesis() error = %v", err)
	}
	bad := investigation.HypothesisResultRequest{
		ExpectedRevision:     2,
		ExpectedCaseRevision: 2,
		CaseID:               register.CaseID,
		HypothesisID:         "hypothesis-missing",
		AssignmentID:         "assignment-1",
		Method:               "read-only schema comparison",
		EvidenceRefs:         []string{"evidence://schema-diff"},
		SourceBoundaryRefs:   []string{"finding-1:boundary"},
		Observed:             "the field is absent from the DTO",
		Counterfactual:       "if the contract is aligned, both sides carry the field",
		Result:               "supported",
		ExplainsFindingIDs:   []string{"finding-1", "finding-2", "finding-outside-case"},
	}
	_, err := investigation.SubmitHypothesisResult(fixture.root, fixture.statePath, fixture.journalPath, bad)
	if err == nil || !strings.Contains(err.Error(), "registered") || !strings.Contains(err.Error(), "source Finding") {
		t.Fatalf("invalid result error = %v, want binding guidance", err)
	}
}

func TestSubmitHypothesisResultRejectsUnboundFollowUpHypothesis(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})
	register := investigation.HypothesisRequest{
		ExpectedRevision:     1,
		ExpectedCaseRevision: 1,
		CaseID:               "investigation-case-review-plan-r1",
		HypothesisID:         "hypothesis-primary",
		Statement:            "the boundary drops the required value",
		Invariant:            "the value survives the boundary",
		Discriminator:        "trace the value across the boundary",
		ExpectedOutcomes:     map[string]any{"support": "the value is dropped", "refute": "the value survives"},
		SourceFindingIDs:     []string{"finding-1"},
		EvidenceRefs:         []string{"evidence://boundary"},
		AssignmentID:         "assignment-primary",
	}
	if _, err := investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, register); err != nil {
		t.Fatalf("RegisterHypothesis() error = %v", err)
	}
	// L3-S8:162: an Investigator's `new_hypotheses` entry is only a suggestion.
	// The system preserves it as authored and Main decides whether to register a
	// new responsibility, so the suggestion must not be rejected for lacking an
	// `assignment-` prefix — and it must not become a registered Hypothesis.
	_, err := investigation.SubmitHypothesisResult(fixture.root, fixture.statePath, fixture.journalPath, investigation.HypothesisResultRequest{
		ExpectedRevision:     -1,
		ExpectedCaseRevision: 2,
		CaseID:               register.CaseID,
		HypothesisID:         register.HypothesisID,
		AssignmentID:         register.AssignmentID,
		Method:               "read-only boundary trace",
		EvidenceRefs:         []string{"evidence://boundary-trace"},
		SourceBoundaryRefs:   []string{"service.go:87"},
		Observed:             "the value is dropped at the decoder",
		Counterfactual:       "an aligned decoder preserves the value",
		Result:               "supported",
		ExplainsFindingIDs:   []string{"finding-1"},
		NewHypotheses: []map[string]any{{
			"hypothesis_id":      "hypothesis-follow-up",
			"statement":          "the decoder has a second field mapping",
			"invariant":          "one decoder mapping owns the field",
			"discriminator":      "compare generated and runtime mappings",
			"expected_outcomes":  map[string]any{"support": "mappings differ", "refute": "mappings match"},
			"source_finding_ids": []any{"finding-1"},
		}},
	})
	if err != nil {
		t.Fatalf("authored follow-up suggestion was rejected: %v", err)
	}
	document := readCaseDocument(t, fixture.root, investigationPointer(t, fixture)["path"].(string))
	for _, raw := range document["hypotheses"].([]any) {
		if raw.(map[string]any)["hypothesis_id"] == "hypothesis-follow-up" {
			t.Fatal("suggested follow-up was registered as a new Hypothesis responsibility")
		}
	}
}

func TestUpdateCaseRouteIsDeterministicAndPreservesExactFindings(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1", "finding-2"})

	register := investigation.HypothesisRequest{
		ExpectedRevision:     1,
		ExpectedCaseRevision: 1,
		CaseID:               "investigation-case-review-plan-r1",
		HypothesisID:         "hypothesis-root",
		Statement:            "one broken payload authority explains both findings",
		Invariant:            "the payload contract has one owner",
		Discriminator:        "compare generated schema and DTO",
		ExpectedOutcomes:     map[string]any{"support": "mismatch", "refute": "match"},
		SourceFindingIDs:     []string{"finding-1", "finding-2"},
		EvidenceRefs:         []string{"evidence://root"},
		AssignmentID:         "assignment-root",
	}
	if _, err := investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, register); err != nil {
		t.Fatalf("RegisterHypothesis() error = %v", err)
	}

	result := investigation.HypothesisResultRequest{
		ExpectedRevision:     2,
		ExpectedCaseRevision: 2,
		CaseID:               register.CaseID,
		HypothesisID:         register.HypothesisID,
		AssignmentID:         "assignment-root",
		Method:               "read-only schema comparison",
		EvidenceRefs:         []string{"evidence://schema-diff"},
		SourceBoundaryRefs:   []string{"finding-1:boundary", "finding-2:boundary"},
		Observed:             "the same schema drift is present in both paths",
		Counterfactual:       "aligned schema removes both boundary mismatches",
		Result:               "supported",
		ExplainsFindingIDs:   []string{"finding-1", "finding-2"},
	}
	if _, err := investigation.SubmitHypothesisResult(fixture.root, fixture.statePath, fixture.journalPath, result); err != nil {
		t.Fatalf("SubmitHypothesisResult() error = %v", err)
	}

	// S8-4: the leading hypothesis must be discriminated — register a
	// competing hypothesis and refute it before the s9_repair route.
	competing := register
	competing.ExpectedRevision = 3
	competing.ExpectedCaseRevision = 3

	competing.HypothesisID = "hypothesis-competitor"
	competing.AssignmentID = "assignment-competitor"
	competing.EvidenceRefs = []string{"evidence://competitor"}
	competing.Statement = "a caching layer drops the update between the two boundaries"
	competing.ExpectedOutcomes = map[string]any{"support": "cache is stale while the schema matches", "refute": "cache is coherent"}
	if _, err := investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, competing); err != nil {
		t.Fatalf("RegisterHypothesis(competitor) error = %v", err)
	}

	if _, err := investigation.SubmitHypothesisResult(fixture.root, fixture.statePath, fixture.journalPath, investigation.HypothesisResultRequest{
		ExpectedRevision:     -1,
		ExpectedCaseRevision: 4,
		CaseID:               register.CaseID,
		HypothesisID:         competing.HypothesisID,
		AssignmentID:         competing.AssignmentID,
		Method:               "read-only cache inspection",
		EvidenceRefs:         []string{"evidence://cache-inspection"},
		SourceBoundaryRefs:   []string{"finding-1:boundary"},
		Observed:             "the cache is coherent while the schema drift persists",
		Counterfactual:       "a stale cache would mask the schema drift",
		Result:               "refuted",
	}); err != nil {
		t.Fatalf("SubmitHypothesisResult(refuted) error = %v", err)
	}

	snapshot, err := investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision:     -1,
		ExpectedCaseRevision: 5,
		CaseID:               register.CaseID,
		Route:                "s9_repair",
		RouteReason:          "supported causal model identifies an implementation boundary repair",
		PrimaryRootCause:     "two incompatible payload authorities drift",
		CausalModel:          map[string]any{"trigger": "new field", "propagation": "decoder drops field"},
		BlastRadius:          map[string]any{"paths": []any{"internal/api/request.go", "internal/api/response.go"}},
		DetectionGap:         map[string]any{"gap_type": "contract", "evidence_refs": []any{"evidence://boundary"}},
	})
	if err != nil {
		t.Fatalf("UpdateCaseRoute() error = %v", err)
	}
	if snapshot.Revision != 6 {
		t.Fatalf("runtime revision = %d, want 6", snapshot.Revision)
	}
	finalPointer := investigationPointerFromState(t, snapshot.State)
	caseDocument := readCaseDocument(t, fixture.root, finalPointer["path"].(string))
	if caseDocument["route"] != "s9_repair" || caseDocument["status"] != "investigating" {
		t.Fatalf("route/status = %v/%v, want s9_repair/investigating", caseDocument["route"], caseDocument["status"])
	}
	if got := caseDocument["source_finding_ids"].([]any); len(got) != 2 || got[0] != "finding-1" || got[1] != "finding-2" {
		t.Fatalf("source Finding set changed: %#v", got)
	}
	if _, err := investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision:     -1,
		ExpectedCaseRevision: 6,
		CaseID:               register.CaseID,
		Route:                "s9_repair",
		RouteReason:          "duplicate conflicting route attempt",
	}); err == nil || !strings.Contains(err.Error(), "deterministic") {
		t.Fatalf("inconsistent route update error = %v, want deterministic route guidance", err)
	}
}

// discriminatedCaseFixture drives a Case through register -> supported ->
// refuted and returns the latest Runtime pointer coordinates. The caller can
// then corrupt individual causal-material fields to prove each gate rejects.
func discriminatedCaseFixture(t *testing.T, fixture *intakeFixture) (map[string]any, string, string) {
	t.Helper()
	register := investigation.HypothesisRequest{
		ExpectedRevision:     1,
		ExpectedCaseRevision: 1,
		CaseID:               "investigation-case-review-plan-r1",
		HypothesisID:         "hypothesis-boundary",
		AssignmentID:         "assignment-boundary",
		Statement:            "the boundary drops the required value",
		Invariant:            "the value survives the boundary",
		Discriminator:        "trace the value through the boundary",
		ExpectedOutcomes:     map[string]any{"support": "the value is dropped", "refute": "the value survives"},
		SourceFindingIDs:     []string{"finding-1"},
		EvidenceRefs:         []string{"evidence://boundary"},
	}
	if _, err := investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, register); err != nil {
		t.Fatalf("RegisterHypothesis() error = %v", err)
	}
	competing := register
	competing.ExpectedRevision = 2
	competing.ExpectedCaseRevision = 2

	competing.HypothesisID = "hypothesis-cache"
	competing.AssignmentID = "assignment-cache"
	competing.EvidenceRefs = []string{"evidence://cache"}
	competing.Statement = "the request cache serves a stale payload without the value"
	competing.ExpectedOutcomes = map[string]any{"support": "cache holds a stale payload", "refute": "cache is coherent"}
	if _, err := investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, competing); err != nil {
		t.Fatalf("RegisterHypothesis(cache) error = %v", err)
	}
	supported := investigation.HypothesisResultRequest{
		ExpectedRevision:     3,
		ExpectedCaseRevision: 3,
		CaseID:               register.CaseID,
		HypothesisID:         register.HypothesisID,
		AssignmentID:         register.AssignmentID,
		Method:               "read-only boundary trace",
		EvidenceRefs:         []string{"evidence://boundary-trace"},
		SourceBoundaryRefs:   []string{"service.go:87"},
		Observed:             "the value is dropped at the decoder",
		Counterfactual:       "an aligned decoder preserves the value",
		Result:               "supported",
		ExplainsFindingIDs:   []string{"finding-1"},
	}
	if _, err := investigation.SubmitHypothesisResult(fixture.root, fixture.statePath, fixture.journalPath, supported); err != nil {
		t.Fatalf("SubmitHypothesisResult(supported) error = %v", err)
	}
	refuted := investigation.HypothesisResultRequest{
		ExpectedRevision:     4,
		ExpectedCaseRevision: 4,
		CaseID:               register.CaseID,
		HypothesisID:         competing.HypothesisID,
		AssignmentID:         competing.AssignmentID,
		Method:               "read-only cache inspection",
		EvidenceRefs:         []string{"evidence://cache-inspection"},
		SourceBoundaryRefs:   []string{"service.go:87"},
		Observed:             "the cache is coherent while the decoder drops the value",
		Counterfactual:       "a stale cache would also drop the value",
		Result:               "refuted",
	}
	if _, err := investigation.SubmitHypothesisResult(fixture.root, fixture.statePath, fixture.journalPath, refuted); err != nil {
		t.Fatalf("SubmitHypothesisResult(refuted) error = %v", err)
	}
	return investigationPointer(t, fixture), register.CaseID, "assignment-cache"
}

// seedCausalClosure writes a complete causal closure into the current Case
// revision, then applies the given mutation so a test can remove exactly one
// required element.
func seedCausalClosure(t *testing.T, fixture *intakeFixture, pointer map[string]any, mutate func(map[string]any) error) map[string]any {
	t.Helper()
	snapshot, err := investigation.UpdateCase(fixture.root, fixture.statePath, fixture.journalPath, investigation.CaseRevisionRequest{
		ExpectedRevision:     -1,
		ExpectedCaseRevision: integerPointerRevision(pointer),
		CaseID:               "investigation-case-review-plan-r1",
		Operation:            "seed_causal_closure",
		Mutate: func(document map[string]any) error {
			document["causal_model"] = map[string]any{"trigger": "payload crosses boundary", "propagation": "decoder drops value"}
			document["primary_root_cause"] = "the decoder drops the required value"
			document["blast_radius"] = map[string]any{"paths": []any{"internal/service/decoder.go"}}
			document["detection_gap"] = map[string]any{"gap_type": "test", "evidence_refs": []any{"evidence://boundary"}}
			if mutate != nil {
				return mutate(document)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("seed causal closure: %v", err)
	}
	return investigationPointerFromState(t, snapshot.State)
}

// currentRuntimeRevision reads the live Runtime revision through the store so
// tests can compose mutations without hard-coding CAS coordinates.
func currentRuntimeRevision(t *testing.T, fixture *intakeFixture) int {
	t.Helper()
	snapshot, err := runtime.NewStore(fixture.statePath, fixture.journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return snapshot.Revision
}

func integerPointerRevision(pointer map[string]any) int {
	switch value := pointer["revision"].(type) {
	case float64:
		return int(value)
	case int:
		return value
	default:
		return 0
	}
}

func routeClosedCase(t *testing.T, fixture *intakeFixture, pointer map[string]any, blastRadius, detectionGap map[string]any) error {
	t.Helper()
	_, err := investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision:     -1,
		ExpectedCaseRevision: integerPointerRevision(pointer),
		CaseID:               "investigation-case-review-plan-r1",
		Route:                "s9_repair",
		RouteReason:          "supported causal model identifies an implementation boundary repair",
		PrimaryRootCause:     "the decoder drops the required value",
		CausalModel:          map[string]any{"trigger": "payload crosses boundary", "propagation": "decoder drops value"},
		BlastRadius:          blastRadius,
		DetectionGap:         detectionGap,
	})
	return err
}

// TestUpdateCaseRouteRejectsHollowBlastRadius proves the S8-3 shape rule:
// blast_radius is a free professional object (L3-S8:297). The blueprint states
// these are recommended expression positions and add no mechanical mandatory
// gate (L3-S8:299), and §9.3 has Main close professionally while the machine
// does not certify causality (L3-S8:303). A hollow impact surface therefore
// must no longer be refused by the machine; the authored content is kept
// verbatim for the reviewer.
func TestUpdateCaseRouteRejectsHollowBlastRadius(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})
	pointer, _, _ := discriminatedCaseFixture(t, fixture)
	pointer = seedCausalClosure(t, fixture, pointer, nil)
	// Remove the blast_radius paths so the artifact carries a hollow object.
	pointer = func() map[string]any {
		snapshot, err := investigation.UpdateCase(fixture.root, fixture.statePath, fixture.journalPath, investigation.CaseRevisionRequest{
			ExpectedRevision:     -1,
			ExpectedCaseRevision: 6,
			CaseID:               "investigation-case-review-plan-r1",
			Operation:            "hollow_blast_radius",
			Mutate: func(document map[string]any) error {
				document["blast_radius"] = map[string]any{}
				return nil
			},
		})
		if err != nil {
			t.Fatalf("hollow blast radius: %v", err)
		}
		return investigationPointerFromState(t, snapshot.State)
	}()
	err := routeClosedCase(t, fixture, pointer, nil, nil)
	if err != nil {
		t.Fatalf("machine refused a free professional blast_radius object: %v", err)
	}
	document := readCaseDocument(t, fixture.root, investigationPointer(t, fixture)["path"].(string))
	blastRadius, _ := document["blast_radius"].(map[string]any)
	if len(blastRadius) != 0 {
		t.Fatalf("machine rewrote the authored blast_radius object: %v", blastRadius)
	}
}

// TestUpdateCaseRouteRejectsDetectionGapWithoutTypeAndEvidence proves the
// S8-3 detection_gap shape: detection_gap is a free professional object
// (L3-S8:299). The blueprint states these are recommended expression positions
// and add no mechanical mandatory gate (L3-S8:299), and §9.3 has Main close
// professionally while the machine does not certify causality (L3-S8:303).
// A missing gap_type or empty evidence_refs must no longer be refused by the
// machine; the authored content is kept verbatim for the reviewer.
func TestUpdateCaseRouteRejectsDetectionGapWithoutTypeAndEvidence(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})
	pointer, _, _ := discriminatedCaseFixture(t, fixture)
	pointer = seedCausalClosure(t, fixture, pointer, func(document map[string]any) error {
		document["detection_gap"] = map[string]any{"note": "no contract test"}
		return nil
	})
	err := routeClosedCase(t, fixture, pointer, nil, nil)
	if err != nil {
		t.Fatalf("machine refused a free professional detection_gap object: %v", err)
	}
	document := readCaseDocument(t, fixture.root, investigationPointer(t, fixture)["path"].(string))
	detectionGap, _ := document["detection_gap"].(map[string]any)
	if _, ok := detectionGap["gap_type"]; ok {
		t.Fatalf("machine invented a gap_type for the author: %v", detectionGap)
	}

	fixture2 := readyCaseFixture(t, []string{"finding-1"})
	pointer2, _, _ := discriminatedCaseFixture(t, fixture2)
	pointer2 = seedCausalClosure(t, fixture2, pointer2, func(document map[string]any) error {
		document["detection_gap"] = map[string]any{"gap_type": "test", "evidence_refs": []any{}}
		return nil
	})
	err = routeClosedCase(t, fixture2, pointer2, nil, nil)
	if err != nil {
		t.Fatalf("machine refused an empty detection_gap evidence set: %v", err)
	}
}

// TestUpdateCaseRouteRejectsSingleSupportedWithoutRefutation proves the S8-4
// shape rule: L3-S8:160 states that `competing_hypothesis_assessment` and
// `no_competing_hypothesis` express Main's judgment and reasoning rather than
// two machine proof routes, and that no refuted result is required before an
// exit may be selected. L3-S8:307 repeats that "at least one refutation" is not
// a machine gate. One supported hypothesis with no refutation therefore must
// not be refused by the machine, and the machine must not invent a competing
// declaration or a refuted result on the author's behalf.
func TestUpdateCaseRouteRejectsSingleSupportedWithoutRefutation(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})
	pointer := investigationPointer(t, fixture)
	register := investigation.HypothesisRequest{
		ExpectedRevision:     1,
		ExpectedCaseRevision: 1,
		CaseID:               "investigation-case-review-plan-r1",
		HypothesisID:         "hypothesis-only",
		AssignmentID:         "assignment-only",
		Statement:            "the boundary drops the required value",
		Invariant:            "the value survives the boundary",
		Discriminator:        "trace the value through the boundary",
		ExpectedOutcomes:     map[string]any{"support": "the value is dropped", "refute": "the value survives"},
		SourceFindingIDs:     []string{"finding-1"},
		EvidenceRefs:         []string{"evidence://boundary"},
	}
	if _, err := investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, register); err != nil {
		t.Fatalf("RegisterHypothesis() error = %v", err)
	}
	pointer = investigationPointer(t, fixture)
	if _, err := investigation.SubmitHypothesisResult(fixture.root, fixture.statePath, fixture.journalPath, investigation.HypothesisResultRequest{
		ExpectedRevision:     -1,
		ExpectedCaseRevision: 2,
		CaseID:               register.CaseID,
		HypothesisID:         register.HypothesisID,
		AssignmentID:         register.AssignmentID,
		Method:               "read-only boundary trace",
		EvidenceRefs:         []string{"evidence://boundary-trace"},
		SourceBoundaryRefs:   []string{"service.go:87"},
		Observed:             "the value is dropped at the decoder",
		Counterfactual:       "an aligned decoder preserves the value",
		Result:               "supported",
		ExplainsFindingIDs:   []string{"finding-1"},
	}); err != nil {
		t.Fatalf("SubmitHypothesisResult() error = %v", err)
	}
	pointer = seedCausalClosure(t, fixture, investigationPointer(t, fixture), nil)
	err := routeClosedCase(t, fixture, pointer, nil, nil)
	if err != nil {
		t.Fatalf("machine refused a route without a refuted result: %v", err)
	}
	document := readCaseDocument(t, fixture.root, investigationPointer(t, fixture)["path"].(string))
	if document["no_competing_hypothesis"] != nil || document["competing_hypothesis_assessment"] != nil {
		t.Fatalf("machine invented a competing-hypothesis declaration: %v", document)
	}
	for _, raw := range document["hypothesis_results"].([]any) {
		if raw.(map[string]any)["result"] == "refuted" {
			t.Fatal("machine invented a refuted result")
		}
	}
}

// TestUpdateCaseRouteAcceptsExplicitNoCompetingHypothesis proves the S8-4
// positive alternative: an explicit declaration may substitute for the
// refuted result.
func TestUpdateCaseRouteAcceptsExplicitNoCompetingHypothesis(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})
	pointer := investigationPointer(t, fixture)
	register := investigation.HypothesisRequest{
		ExpectedRevision:     1,
		ExpectedCaseRevision: 1,
		CaseID:               "investigation-case-review-plan-r1",
		HypothesisID:         "hypothesis-only",
		AssignmentID:         "assignment-only",
		Statement:            "the boundary drops the required value",
		Invariant:            "the value survives the boundary",
		Discriminator:        "trace the value through the boundary",
		ExpectedOutcomes:     map[string]any{"support": "the value is dropped", "refute": "the value survives"},
		SourceFindingIDs:     []string{"finding-1"},
		EvidenceRefs:         []string{"evidence://boundary"},
	}
	if _, err := investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, register); err != nil {
		t.Fatalf("RegisterHypothesis() error = %v", err)
	}
	pointer = investigationPointer(t, fixture)
	if _, err := investigation.SubmitHypothesisResult(fixture.root, fixture.statePath, fixture.journalPath, investigation.HypothesisResultRequest{
		ExpectedRevision:     -1,
		ExpectedCaseRevision: 2,
		CaseID:               register.CaseID,
		HypothesisID:         register.HypothesisID,
		AssignmentID:         register.AssignmentID,
		Method:               "read-only boundary trace",
		EvidenceRefs:         []string{"evidence://boundary-trace"},
		SourceBoundaryRefs:   []string{"service.go:87"},
		Observed:             "the value is dropped at the decoder",
		Counterfactual:       "an aligned decoder preserves the value",
		Result:               "supported",
		ExplainsFindingIDs:   []string{"finding-1"},
	}); err != nil {
		t.Fatalf("SubmitHypothesisResult() error = %v", err)
	}
	pointer = investigationPointer(t, fixture)
	pointer = seedCausalClosure(t, fixture, pointer, func(document map[string]any) error {
		document["no_competing_hypothesis"] = "the sealed occurrence admits only one credible mechanism; every alternative requires an artifact that does not exist in this runtime"
		return nil
	})
	if err := routeClosedCase(t, fixture, pointer, nil, nil); err != nil {
		t.Fatalf("explicit no_competing_hypothesis route error = %v, want accepted route", err)
	}
}

func TestUpdateCaseRouteCanReopenInvestigateMoreAfterNewEvidence(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})
	setInvestigationReentryLimit(t, fixture, 1)

	register := investigation.HypothesisRequest{
		ExpectedRevision:     1,
		ExpectedCaseRevision: 1,
		CaseID:               "investigation-case-review-plan-r1",
		HypothesisID:         "hypothesis-boundary",
		AssignmentID:         "assignment-boundary",
		Statement:            "the boundary drops the required value",
		Invariant:            "the value survives the boundary",
		Discriminator:        "trace the value through the boundary",
		ExpectedOutcomes:     map[string]any{"support": "the value is dropped", "refute": "the value survives"},
		SourceFindingIDs:     []string{"finding-1"},
		EvidenceRefs:         []string{"evidence://boundary"},
	}
	if _, err := investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, register); err != nil {
		t.Fatalf("RegisterHypothesis() error = %v", err)
	}

	snapshot, err := investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision:     -1,
		ExpectedCaseRevision: 2,
		CaseID:               register.CaseID,
		Route:                "investigate_more",
		RouteReason:          "the source Finding is not yet explained",
	})
	if err != nil {
		t.Fatalf("initial investigate_more route error = %v", err)
	}

	result := investigation.HypothesisResultRequest{
		ExpectedRevision:     3,
		ExpectedCaseRevision: 3,
		CaseID:               register.CaseID,
		HypothesisID:         register.HypothesisID,
		AssignmentID:         register.AssignmentID,
		Method:               "read-only boundary trace",
		EvidenceRefs:         []string{"evidence://boundary-trace"},
		SourceBoundaryRefs:   []string{"service.go:87"},
		Observed:             "the value is dropped at the decoder",
		Counterfactual:       "an aligned decoder preserves the value",
		Result:               "supported",
		ExplainsFindingIDs:   []string{"finding-1"},
	}
	snapshot, err = investigation.SubmitHypothesisResult(fixture.root, fixture.statePath, fixture.journalPath, result)
	if err != nil {
		t.Fatalf("SubmitHypothesisResult() after investigate_more error = %v", err)
	}

	// S8-4: discriminate the leading hypothesis before the causal closure —
	// refute the competing cache mechanism through its own assignment.
	competing := register
	competing.ExpectedRevision = 4
	competing.ExpectedCaseRevision = 4

	competing.HypothesisID = "hypothesis-cache"
	competing.AssignmentID = "assignment-cache"
	competing.EvidenceRefs = []string{"evidence://cache"}
	competing.Statement = "the request cache serves a stale payload without the value"
	competing.ExpectedOutcomes = map[string]any{"support": "cache holds a stale payload", "refute": "cache is coherent"}
	if _, err := investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, competing); err != nil {
		t.Fatalf("RegisterHypothesis(cache) error = %v", err)
	}

	competingResult := investigation.HypothesisResultRequest{
		ExpectedRevision:     5,
		ExpectedCaseRevision: 5,
		CaseID:               register.CaseID,
		HypothesisID:         competing.HypothesisID,
		AssignmentID:         competing.AssignmentID,
		Method:               "read-only cache inspection",
		EvidenceRefs:         []string{"evidence://cache-inspection"},
		SourceBoundaryRefs:   []string{"service.go:87"},
		Observed:             "the cache is coherent while the decoder drops the value",
		Counterfactual:       "a stale cache would also drop the value",
		Result:               "refuted",
	}
	if _, err := investigation.SubmitHypothesisResult(fixture.root, fixture.statePath, fixture.journalPath, competingResult); err != nil {
		t.Fatalf("SubmitHypothesisResult(refuted cache) error = %v", err)
	}

	snapshot, err = investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision:     -1,
		ExpectedCaseRevision: 6,
		CaseID:               register.CaseID,
		Route:                "s9_repair",
		RouteReason:          "new supported evidence closes the causal chain",
		PrimaryRootCause:     "the decoder drops the required value",
		CausalModel:          map[string]any{"trigger": "payload crosses boundary", "propagation": "decoder drops value"},
		BlastRadius:          map[string]any{"paths": []any{"internal/service/decoder.go"}},
		DetectionGap:         map[string]any{"gap_type": "test", "evidence_refs": []any{"evidence://boundary"}},
	})
	if err != nil {
		t.Fatalf("re-route to s9_repair error = %v", err)
	}
	finalPointer := investigationPointerFromState(t, snapshot.State)
	caseDocument := readCaseDocument(t, fixture.root, finalPointer["path"].(string))
	if caseDocument["route"] != "s9_repair" {
		t.Fatalf("route = %v, want s9_repair", caseDocument["route"])
	}
	history, ok := caseDocument["route_history"].([]any)
	if !ok || len(history) != 2 {
		t.Fatalf("route_history = %#v, want initial and re-route entries", caseDocument["route_history"])
	}
	if history[0].(map[string]any)["to"] != "investigate_more" || history[1].(map[string]any)["from"] != "investigate_more" || history[1].(map[string]any)["to"] != "s9_repair" {
		t.Fatalf("route_history = %#v, want investigate_more -> s9_repair", history)
	}
}

// Existing configuration and unchanged evidence do not gate Main's judgment.
func TestInvestigateMoreContinuesAndRoutesWithSameMaterial(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})
	_, caseID, _ := discriminatedCaseFixture(t, fixture)
	setInvestigationReentryLimit(t, fixture, 1)
	for attempt := 0; attempt < 7; attempt++ {
		pointer := investigationPointer(t, fixture)
		if _, err := investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
			ExpectedRevision: -1, ExpectedCaseRevision: integerPointerRevision(pointer), CaseID: caseID, Route: "investigate_more", RouteReason: "the same material remains unresolved",
		}); err != nil {
			t.Fatalf("continue %d: %v", attempt, err)
		}
	}
	pointer := investigationPointer(t, fixture)
	snapshot, err := investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision: -1, ExpectedCaseRevision: integerPointerRevision(pointer), CaseID: caseID, Route: "s2_spec_rework", RouteReason: "the same observations establish a specification contradiction",
	})
	if err != nil {
		t.Fatal(err)
	}
	document := readCaseDocument(t, fixture.root, investigationPointerFromState(t, snapshot.State)["path"].(string))
	if document["route"] != "s2_spec_rework" {
		t.Fatalf("route = %v", document["route"])
	}
	for _, row := range document["route_history"].([]any) {
		if _, exists := row.(map[string]any)["evidence_fingerprint"]; exists {
			t.Fatal("new route records a progress fingerprint")
		}
	}
}

func TestUpdateCaseRouteReopensApprovedCaseWithCausalReassessmentEvidence(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})

	contractRef := ".claude/review/investigation/contracts/repair-contract-r2.json"
	approvedSnapshot, err := investigation.UpdateCase(fixture.root, fixture.statePath, fixture.journalPath, investigation.CaseRevisionRequest{
		ExpectedRevision:     -1,
		ExpectedCaseRevision: 1,
		CaseID:               "investigation-case-review-plan-r1",
		Operation:            "test_contract_approved_case",
		Mutate: func(document map[string]any) error {
			document["status"] = "contract_approved"
			document["route"] = "s9_repair"
			document["repair_contract_ref"] = contractRef
			return nil
		},
	})
	if err != nil {
		t.Fatalf("seed approved Case: %v", err)
	}
	approvedState := approvedSnapshot.State["review"].(map[string]any)
	approvedState["repair"] = map[string]any{
		"session_id": "repair-session-old", "case_id": "investigation-case-review-plan-r1", "contract_id": "repair-contract-old",
		"contract_ref": contractRef,
		"path":         ".claude/review/repair/sessions/repair-session-old.json", "revision": 1,
		"status": "blocked", "failure_route": "fail_same_cause", "targeted_reverification_refs": []any{".claude/review/repair/reverification/reverify-failure.json"},
		"targeted_reverification_artifacts": []any{}, "updated_at": "2026-08-26T00:00:00Z",
		"next_action": "re-open the Case with causal reassessment",
	}
	req039fixtures.WriteState(t, fixture.root, approvedSnapshot.State)

	evidenceRel := ".claude/review/repair/reverification/reverify-failure.json"
	evidence := []byte("{\"result\":\"fail\",\"failure_class\":\"fail_same_cause\"}\n")
	evidencePath := filepath.Join(fixture.root, filepath.FromSlash(evidenceRel))
	if err := os.MkdirAll(filepath.Dir(evidencePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(evidencePath, evidence, 0o644); err != nil {
		t.Fatal(err)
	}

	snapshot, err := investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision:     -1,
		ExpectedCaseRevision: 2,
		CaseID:               "investigation-case-review-plan-r1",
		Route:                "investigate_more",
		RouteReason:          "targeted reverification shows the approved causal model needs reassessment",
		CausalReassessmentEvidenceRefs: []investigation.EvidenceReference{{
			Path: evidenceRel,
		}},
	})
	if err != nil {
		t.Fatalf("reopen approved Case: %v", err)
	}
	finalPointer := investigationPointerFromState(t, snapshot.State)
	document := readCaseDocument(t, fixture.root, finalPointer["path"].(string))
	if document["status"] != "investigating" || document["route"] != "investigate_more" {
		t.Fatalf("reopened Case status/route = %v/%v, want investigating/investigate_more", document["status"], document["route"])
	}
	if document["repair_contract_ref"] != nil {
		t.Fatalf("reopened Case must clear the superseded Contract pointer: %#v", document)
	}
	currentReview := snapshot.State["review"].(map[string]any)
	if repair, ok := currentReview["repair"].(map[string]any); ok && repair != nil {
		t.Fatalf("reopening the Case must retire the superseded S9 pointer so a new Contract can open a new session: %#v", repair)
	}
	refs, ok := document["causal_reassessment_refs"].([]any)
	if !ok || len(refs) != 1 || refs[0].(map[string]any)["path"] != evidenceRel {
		t.Fatalf("reopened Case must retain exact causal reassessment evidence refs: %#v", document["causal_reassessment_refs"])
	}
	history := document["route_history"].([]any)
	last := history[len(history)-1].(map[string]any)
	if last["from"] != "s9_repair" || last["to"] != "investigate_more" {
		t.Fatalf("route history = %#v, want s9_repair -> investigate_more", history)
	}
}

func TestDuplicateRoutePersistsCanonicalCaseReference(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})
	pointer := investigationPointer(t, fixture)
	currentPath := filepath.Join(fixture.root, filepath.FromSlash(pointer["path"].(string)))
	canonicalBytes := mustRead(t, currentPath)
	var canonical map[string]any
	if err := json.Unmarshal(canonicalBytes, &canonical); err != nil {
		t.Fatal(err)
	}
	canonical["case_id"] = "investigation-case-canonical"
	canonicalBytes, err := json.MarshalIndent(canonical, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	canonicalBytes = append(canonicalBytes, '\n')
	canonicalRel := ".claude/review/investigation/cases/investigation-case-canonical-r1.json"
	canonicalPath := filepath.Join(fixture.root, filepath.FromSlash(canonicalRel))
	if err := os.WriteFile(canonicalPath, canonicalBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	snapshot, err := investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision:     -1,
		ExpectedCaseRevision: 1,
		CaseID:               "investigation-case-review-plan-r1",
		Route:                "duplicate",
		RouteReason:          "the same causal incident is already tracked canonically",
		CanonicalCaseID:      "investigation-case-canonical",
	})
	if err != nil {
		t.Fatalf("duplicate route error = %v", err)
	}
	finalPointer := investigationPointerFromState(t, snapshot.State)
	caseDocument := readCaseDocument(t, fixture.root, finalPointer["path"].(string))
	if caseDocument["route"] != "duplicate" || caseDocument["canonical_case_id"] != "investigation-case-canonical" {
		t.Fatalf("duplicate routing fields = %#v", caseDocument)
	}
	if caseDocument["canonical_case_ref"] != canonicalRel {
		t.Fatalf("canonical reference = %v, want %s", caseDocument["canonical_case_ref"], canonicalRel)
	}
}

func readyCaseFixture(t *testing.T, findingIDs []string) *intakeFixture {
	t.Helper()
	fixture := newIntakeFixture(t, findingIDs)
	setContractLifecycle(t, fixture)
	if _, err := investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, investigation.IngestRequest{
		ExpectedRevision:  -1,
		GroupingRationale: "the sealed batch is the provisional grouping boundary",
	}); err != nil {
		t.Fatalf("Ingest() error = %v", err)
	}
	return fixture
}

func investigationPointer(t *testing.T, fixture *intakeFixture) map[string]any {
	t.Helper()
	snapshot, err := runtime.NewStore(fixture.statePath, fixture.journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return investigationPointerFromState(t, snapshot.State)
}

func investigationPointerFromState(t *testing.T, state map[string]any) map[string]any {
	t.Helper()
	return state["review"].(map[string]any)["investigation"].(map[string]any)
}

func readCaseDocument(t *testing.T, root, relative string) map[string]any {
	t.Helper()
	data := mustRead(t, filepath.Join(root, filepath.FromSlash(relative)))
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func setInvestigationReentryLimit(t *testing.T, fixture *intakeFixture, limit int) {
	t.Helper()
	var state map[string]any
	if err := json.Unmarshal(mustRead(t, fixture.statePath), &state); err != nil {
		t.Fatal(err)
	}
	state["configuration"].(map[string]any)["repair"].(map[string]any)["max_investigation_reentries"] = limit
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.statePath, append(data, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
}
