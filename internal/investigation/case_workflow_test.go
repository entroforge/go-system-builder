package investigation_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
		ExpectedRevision:     1,
		ExpectedCaseRevision: 1,
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               "investigation-case-observation-batch-r1",
		HypothesisID:         "hypothesis-payload-drift",
		Statement:            "FE and BE payload schemas drift at the serialization boundary",
		Invariant:            "one authoritative payload contract owns field shape",
		Discriminator:        "compare generated client payload with the server DTO",
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
	if newPointer["sha256"] == pointer["sha256"] {
		t.Fatal("case revision must receive a new content hash")
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
	pointer := investigationPointer(t, fixture)
	_, err := investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, investigation.HypothesisRequest{
		ExpectedRevision:     1,
		ExpectedCaseRevision: 1,
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               "investigation-case-observation-batch-r1",
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
	pointer := investigationPointer(t, fixture)
	_, err := investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, investigation.HypothesisRequest{
		ExpectedRevision:     1,
		ExpectedCaseRevision: 1,
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               "investigation-case-observation-batch-r1",
		HypothesisID:         "hypothesis-evidence-free",
		Statement:            "the boundary contract is inconsistent",
		Invariant:            "the boundary has one owner",
		Discriminator:        "inspect both sides of the boundary",
		ExpectedOutcomes:     map[string]any{"support": "drift", "refute": "no drift"},
		SourceFindingIDs:     []string{"finding-1"},
		AssignmentID:         "assignment-evidence-free",
	})
	if err == nil || !strings.Contains(err.Error(), "evidence_refs") || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("evidence-free hypothesis error = %v, want evidence_refs guidance", err)
	}
}

func TestCaseWorkflowRejectsStaleRevisionAndHashDrift(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})
	pointer := investigationPointer(t, fixture)
	request := investigation.HypothesisRequest{
		ExpectedRevision:     0,
		ExpectedCaseRevision: 1,
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               "investigation-case-observation-batch-r1",
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

	request.ExpectedRevision = 1
	request.ExpectedCaseSHA256 = strings.Repeat("b", 64)
	_, err = investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, request)
	if err == nil || !strings.Contains(err.Error(), "hash") || !strings.Contains(err.Error(), "runtime investigation") {
		t.Fatalf("hash drift error = %v, want hash and recovery guidance", err)
	}
}

func TestSubmitHypothesisResultRequiresRegisteredHypothesisAndFindingSubset(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1", "finding-2"})
	pointer := investigationPointer(t, fixture)
	register := investigation.HypothesisRequest{
		ExpectedRevision:     1,
		ExpectedCaseRevision: 1,
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               "investigation-case-observation-batch-r1",
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
	pointer = investigationPointer(t, fixture)
	bad := investigation.HypothesisResultRequest{
		ExpectedRevision:     2,
		ExpectedCaseRevision: 2,
		ExpectedCaseSHA256:   pointer["sha256"].(string),
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
	pointer := investigationPointer(t, fixture)
	register := investigation.HypothesisRequest{
		ExpectedRevision:     1,
		ExpectedCaseRevision: 1,
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               "investigation-case-observation-batch-r1",
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
	pointer = investigationPointer(t, fixture)
	_, err := investigation.SubmitHypothesisResult(fixture.root, fixture.statePath, fixture.journalPath, investigation.HypothesisResultRequest{
		ExpectedRevision:     2,
		ExpectedCaseRevision: 2,
		ExpectedCaseSHA256:   pointer["sha256"].(string),
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
	if err == nil || !strings.Contains(err.Error(), "assignment_id") || !strings.Contains(err.Error(), "assignment- prefix") {
		t.Fatalf("unbound follow-up hypothesis error = %v, want Assignment binding guidance", err)
	}
}

func TestUpdateCaseRouteIsDeterministicAndPreservesExactFindings(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1", "finding-2"})
	pointer := investigationPointer(t, fixture)
	register := investigation.HypothesisRequest{
		ExpectedRevision:     1,
		ExpectedCaseRevision: 1,
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               "investigation-case-observation-batch-r1",
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
	pointer = investigationPointer(t, fixture)
	result := investigation.HypothesisResultRequest{
		ExpectedRevision:     2,
		ExpectedCaseRevision: 2,
		ExpectedCaseSHA256:   pointer["sha256"].(string),
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
	pointer = investigationPointer(t, fixture)
	// S8-4: the leading hypothesis must be discriminated — register a
	// competing hypothesis and refute it before the s9_repair route.
	competing := register
	competing.ExpectedRevision = 3
	competing.ExpectedCaseRevision = 3
	competing.ExpectedCaseSHA256 = pointer["sha256"].(string)
	competing.HypothesisID = "hypothesis-competitor"
	competing.AssignmentID = "assignment-competitor"
	competing.EvidenceRefs = []string{"evidence://competitor"}
	competing.Statement = "a caching layer drops the update between the two boundaries"
	competing.ExpectedOutcomes = map[string]any{"support": "cache is stale while the schema matches", "refute": "cache is coherent"}
	if _, err := investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, competing); err != nil {
		t.Fatalf("RegisterHypothesis(competitor) error = %v", err)
	}
	pointer = investigationPointer(t, fixture)
	if _, err := investigation.SubmitHypothesisResult(fixture.root, fixture.statePath, fixture.journalPath, investigation.HypothesisResultRequest{
		ExpectedRevision:     4,
		ExpectedCaseRevision: 4,
		ExpectedCaseSHA256:   pointer["sha256"].(string),
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
	pointer = investigationPointer(t, fixture)
	snapshot, err := investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision:     5,
		ExpectedCaseRevision: 5,
		ExpectedCaseSHA256:   pointer["sha256"].(string),
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
		ExpectedRevision:     6,
		ExpectedCaseRevision: 6,
		ExpectedCaseSHA256:   finalPointer["sha256"].(string),
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
	pointer := investigationPointer(t, fixture)
	register := investigation.HypothesisRequest{
		ExpectedRevision:     1,
		ExpectedCaseRevision: 1,
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               "investigation-case-observation-batch-r1",
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
	competing.ExpectedCaseSHA256 = investigationPointer(t, fixture)["sha256"].(string)
	competing.HypothesisID = "hypothesis-cache"
	competing.AssignmentID = "assignment-cache"
	competing.EvidenceRefs = []string{"evidence://cache"}
	competing.Statement = "the request cache serves a stale payload without the value"
	competing.ExpectedOutcomes = map[string]any{"support": "cache holds a stale payload", "refute": "cache is coherent"}
	if _, err := investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, competing); err != nil {
		t.Fatalf("RegisterHypothesis(cache) error = %v", err)
	}
	pointer = investigationPointer(t, fixture)
	supported := investigation.HypothesisResultRequest{
		ExpectedRevision:     3,
		ExpectedCaseRevision: 3,
		ExpectedCaseSHA256:   pointer["sha256"].(string),
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
		ExpectedCaseSHA256:   investigationPointer(t, fixture)["sha256"].(string),
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
	runtimeRevision := currentRuntimeRevision(t, fixture)
	snapshot, err := investigation.UpdateCase(fixture.root, fixture.statePath, fixture.journalPath, investigation.CaseRevisionRequest{
		ExpectedRevision:     runtimeRevision,
		ExpectedCaseRevision: integerPointerRevision(pointer),
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               "investigation-case-observation-batch-r1",
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
		ExpectedRevision:     currentRuntimeRevision(t, fixture),
		ExpectedCaseRevision: integerPointerRevision(pointer),
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               "investigation-case-observation-batch-r1",
		Route:                "s9_repair",
		RouteReason:          "supported causal model identifies an implementation boundary repair",
		PrimaryRootCause:     "the decoder drops the required value",
		CausalModel:          map[string]any{"trigger": "payload crosses boundary", "propagation": "decoder drops value"},
		BlastRadius:          blastRadius,
		DetectionGap:         detectionGap,
	})
	return err
}

// TestUpdateCaseRouteRejectsHollowBlastRadius proves the S8-3 negative case:
// a blast_radius without a non-empty path set must never open an s9_repair
// route, no matter how polished the rest of the dossier looks.
func TestUpdateCaseRouteRejectsHollowBlastRadius(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})
	pointer, _, _ := discriminatedCaseFixture(t, fixture)
	pointer = seedCausalClosure(t, fixture, pointer, nil)
	// Remove the blast_radius paths so the artifact carries a hollow object.
	pointer = func() map[string]any {
		snapshot, err := investigation.UpdateCase(fixture.root, fixture.statePath, fixture.journalPath, investigation.CaseRevisionRequest{
			ExpectedRevision:     6,
			ExpectedCaseRevision: 6,
			ExpectedCaseSHA256:   pointer["sha256"].(string),
			CaseID:               "investigation-case-observation-batch-r1",
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
	if err == nil || !strings.Contains(err.Error(), "blast_radius") || !strings.Contains(err.Error(), "paths") {
		t.Fatalf("hollow blast_radius error = %v, want non-empty path set guidance", err)
	}
}

// TestUpdateCaseRouteRejectsDetectionGapWithoutTypeAndEvidence proves the
// S8-3 detection_gap shape: a gap must name gap_type and bind evidence_refs.
func TestUpdateCaseRouteRejectsDetectionGapWithoutTypeAndEvidence(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})
	pointer, _, _ := discriminatedCaseFixture(t, fixture)
	pointer = seedCausalClosure(t, fixture, pointer, func(document map[string]any) error {
		document["detection_gap"] = map[string]any{"note": "no contract test"}
		return nil
	})
	err := routeClosedCase(t, fixture, pointer, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "gap_type") {
		t.Fatalf("detection_gap without gap_type error = %v, want gap_type guidance", err)
	}

	fixture2 := readyCaseFixture(t, []string{"finding-1"})
	pointer2, _, _ := discriminatedCaseFixture(t, fixture2)
	pointer2 = seedCausalClosure(t, fixture2, pointer2, func(document map[string]any) error {
		document["detection_gap"] = map[string]any{"gap_type": "test", "evidence_refs": []any{}}
		return nil
	})
	err = routeClosedCase(t, fixture2, pointer2, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "evidence_refs") {
		t.Fatalf("detection_gap without evidence_refs error = %v, want evidence_refs guidance", err)
	}
}

// TestUpdateCaseRouteRejectsSingleSupportedWithoutRefutation proves the S8-4
// negative case: one supported hypothesis with zero refuted results and no
// no_competing_hypothesis declaration must be rejected as an untested
// conclusion.
func TestUpdateCaseRouteRejectsSingleSupportedWithoutRefutation(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})
	pointer := investigationPointer(t, fixture)
	register := investigation.HypothesisRequest{
		ExpectedRevision:     1,
		ExpectedCaseRevision: 1,
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               "investigation-case-observation-batch-r1",
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
		ExpectedRevision:     2,
		ExpectedCaseRevision: 2,
		ExpectedCaseSHA256:   pointer["sha256"].(string),
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
	if err == nil || !strings.Contains(err.Error(), "refuted") || !strings.Contains(err.Error(), "no_competing_hypothesis") {
		t.Fatalf("single-supported route error = %v, want competing-hypothesis guidance", err)
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
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               "investigation-case-observation-batch-r1",
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
		ExpectedRevision:     2,
		ExpectedCaseRevision: 2,
		ExpectedCaseSHA256:   pointer["sha256"].(string),
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
	pointer := investigationPointer(t, fixture)
	register := investigation.HypothesisRequest{
		ExpectedRevision:     1,
		ExpectedCaseRevision: 1,
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               "investigation-case-observation-batch-r1",
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
	pointer = investigationPointer(t, fixture)
	snapshot, err := investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision:     2,
		ExpectedCaseRevision: 2,
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               register.CaseID,
		Route:                "investigate_more",
		RouteReason:          "the source Finding is not yet explained",
	})
	if err != nil {
		t.Fatalf("initial investigate_more route error = %v", err)
	}
	pointer = investigationPointerFromState(t, snapshot.State)
	result := investigation.HypothesisResultRequest{
		ExpectedRevision:     3,
		ExpectedCaseRevision: 3,
		ExpectedCaseSHA256:   pointer["sha256"].(string),
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
	pointer = investigationPointerFromState(t, snapshot.State)
	// S8-4: discriminate the leading hypothesis before the causal closure —
	// refute the competing cache mechanism through its own assignment.
	competing := register
	competing.ExpectedRevision = 4
	competing.ExpectedCaseRevision = 4
	competing.ExpectedCaseSHA256 = investigationPointer(t, fixture)["sha256"].(string)
	competing.HypothesisID = "hypothesis-cache"
	competing.AssignmentID = "assignment-cache"
	competing.EvidenceRefs = []string{"evidence://cache"}
	competing.Statement = "the request cache serves a stale payload without the value"
	competing.ExpectedOutcomes = map[string]any{"support": "cache holds a stale payload", "refute": "cache is coherent"}
	if _, err := investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, competing); err != nil {
		t.Fatalf("RegisterHypothesis(cache) error = %v", err)
	}
	pointer = investigationPointerFromState(t, snapshot.State)
	competingResult := investigation.HypothesisResultRequest{
		ExpectedRevision:     5,
		ExpectedCaseRevision: 5,
		ExpectedCaseSHA256:   investigationPointer(t, fixture)["sha256"].(string),
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
	pointer = investigationPointerFromState(t, snapshot.State)
	pointer = investigationPointer(t, fixture)
	snapshot, err = investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision:     6,
		ExpectedCaseRevision: 6,
		ExpectedCaseSHA256:   pointer["sha256"].(string),
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

// TestInvestigateMoreReRouteRequiresNewEvidenceFingerprint proves the S8-7
// freshness contract: after an investigate_more checkpoint, a re-route is
// unlocked by new evidence_ref content (a changed evidence fingerprint), not
// by a bare count increase or a recycled reference.
func TestInvestigateMoreReRouteRequiresNewEvidenceFingerprint(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})
	pointer, caseID, _ := discriminatedCaseFixture(t, fixture)

	// Route to investigate_more first; the checkpoint records the current
	// evidence fingerprint. The Case is discriminated but not causally closed,
	// so investigate_more is a legitimate disposition.
	snapshot, err := investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision:     currentRuntimeRevision(t, fixture),
		ExpectedCaseRevision: integerPointerRevision(pointer),
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               caseID,
		Route:                "investigate_more",
		RouteReason:          "the mechanism needs one more discriminator before repair",
	})
	if err != nil {
		t.Fatalf("initial investigate_more route error = %v", err)
	}

	// Recycle the same evidence: a new hypothesis whose evidence_refs duplicate
	// an existing reference does not change the fingerprint and must be
	// rejected as a mechanical unlock.
	pointer = investigationPointerFromState(t, snapshot.State)
	recycled := investigation.HypothesisRequest{
		ExpectedRevision:     snapshot.Revision,
		ExpectedCaseRevision: integerPointerRevision(pointer),
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               caseID,
		HypothesisID:         "hypothesis-recycled",
		AssignmentID:         "assignment-recycled",
		Statement:            "the boundary drops the required value for a second reason",
		Invariant:            "the value survives the boundary",
		Discriminator:        "trace the value through the boundary again",
		ExpectedOutcomes:     map[string]any{"support": "the value is dropped again", "refute": "the value survives"},
		SourceFindingIDs:     []string{"finding-1"},
		EvidenceRefs:         []string{"evidence://boundary"},
	}
	snapshot, err = investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, recycled)
	if err != nil {
		t.Fatalf("RegisterHypothesis(recycled evidence) error = %v", err)
	}
	pointer = investigationPointerFromState(t, snapshot.State)
	_, err = investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision:     currentRuntimeRevision(t, fixture),
		ExpectedCaseRevision: integerPointerRevision(pointer),
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               caseID,
		Route:                "investigate_more",
		RouteReason:          "attempting a second checkpoint without new evidence content",
	})
	if err == nil || !strings.Contains(err.Error(), "new hypothesis or result evidence") {
		t.Fatalf("recycled-evidence re-route error = %v, want freshness guidance", err)
	}

	// A genuinely new evidence_ref changes the fingerprint and unlocks the
	// re-entry point.
	pointer = investigationPointer(t, fixture)
	fresh := recycled
	fresh.ExpectedRevision = snapshot.Revision
	fresh.ExpectedCaseRevision = integerPointerRevision(pointer)
	fresh.ExpectedCaseSHA256 = pointer["sha256"].(string)
	fresh.HypothesisID = "hypothesis-fresh"
	fresh.AssignmentID = "assignment-fresh"
	fresh.EvidenceRefs = []string{"evidence://fresh-probe"}
	fresh.Statement = "a second decoder mapping drops the value"
	if _, err := investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, fresh); err != nil {
		t.Fatalf("RegisterHypothesis(fresh evidence) error = %v", err)
	}
	pointer = investigationPointer(t, fixture)
	snapshot, err = investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision:     currentRuntimeRevision(t, fixture),
		ExpectedCaseRevision: integerPointerRevision(pointer),
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               caseID,
		Route:                "investigate_more",
		RouteReason:          "a new discriminator probe was registered with fresh evidence",
	})
	if err != nil {
		t.Fatalf("fresh-evidence re-route error = %v, want accepted route", err)
	}
	finalPointer := investigationPointerFromState(t, snapshot.State)
	document := readCaseDocument(t, fixture.root, finalPointer["path"].(string))
	history, ok := document["route_history"].([]any)
	if !ok || len(history) != 2 {
		t.Fatalf("route_history = %#v, want two investigate_more checkpoints", document["route_history"])
	}
	if first, second := history[0].(map[string]any), history[1].(map[string]any); first["evidence_fingerprint"] == second["evidence_fingerprint"] {
		t.Fatalf("second checkpoint must carry a new evidence fingerprint: %v vs %v", first["evidence_fingerprint"], second["evidence_fingerprint"])
	}
}

func TestUpdateCaseRouteReopensApprovedCaseWithCausalReassessmentEvidence(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})
	pointer := investigationPointer(t, fixture)
	contractRef := ".claude/review/investigation/contracts/repair-contract-r2.json"
	approvedSnapshot, err := investigation.UpdateCase(fixture.root, fixture.statePath, fixture.journalPath, investigation.CaseRevisionRequest{
		ExpectedRevision:     1,
		ExpectedCaseRevision: 1,
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               "investigation-case-observation-batch-r1",
		Operation:            "test_contract_approved_case",
		Mutate: func(document map[string]any) error {
			document["status"] = "contract_approved"
			document["route"] = "s9_repair"
			document["repair_contract_ref"] = contractRef
			document["repair_contract_sha256"] = strings.Repeat("a", 64)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("seed approved Case: %v", err)
	}
	approvedState := approvedSnapshot.State["review"].(map[string]any)
	approvedState["repair"] = map[string]any{
		"session_id": "repair-session-old", "case_id": "investigation-case-observation-batch-r1", "contract_id": "repair-contract-old",
		"contract_ref": contractRef, "contract_sha256": strings.Repeat("c", 64),
		"path": ".claude/review/repair/sessions/repair-session-old.json", "sha256": strings.Repeat("d", 64), "revision": 1,
		"status": "blocked", "failure_route": "fail_same_cause", "targeted_reverification_refs": []any{".claude/review/repair/reverification/reverify-failure.json"},
		"targeted_reverification_artifacts": []any{}, "updated_at": "2026-08-26T00:00:00Z",
		"next_action": "re-open the Case with causal reassessment",
	}
	req039fixtures.WriteState(t, fixture.root, approvedSnapshot.State)
	pointer = investigationPointerFromState(t, approvedSnapshot.State)
	evidenceRel := ".claude/review/repair/reverification/reverify-failure.json"
	evidence := []byte("{\"result\":\"fail\",\"failure_class\":\"fail_same_cause\"}\n")
	evidencePath := filepath.Join(fixture.root, filepath.FromSlash(evidenceRel))
	if err := os.MkdirAll(filepath.Dir(evidencePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(evidencePath, evidence, 0o644); err != nil {
		t.Fatal(err)
	}
	evidenceSHA := sha256.Sum256(evidence)

	snapshot, err := investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision:     2,
		ExpectedCaseRevision: 2,
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               "investigation-case-observation-batch-r1",
		Route:                "investigate_more",
		RouteReason:          "targeted reverification shows the approved causal model needs reassessment",
		CausalReassessmentEvidenceRefs: []investigation.EvidenceReference{{
			Path: evidenceRel, SHA256: hex.EncodeToString(evidenceSHA[:]),
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
	if document["repair_contract_ref"] != nil || document["repair_contract_sha256"] != nil {
		t.Fatalf("reopened Case must clear the superseded Contract pointer: %#v", document)
	}
	currentReview := snapshot.State["review"].(map[string]any)
	if repair, ok := currentReview["repair"].(map[string]any); ok && repair != nil {
		t.Fatalf("reopening the Case must retire the superseded S9 pointer so a new Contract can open a new session: %#v", repair)
	}
	refs, ok := document["causal_reassessment_refs"].([]any)
	if !ok || len(refs) != 1 || refs[0].(map[string]any)["path"] != evidenceRel || refs[0].(map[string]any)["sha256"] != hex.EncodeToString(evidenceSHA[:]) {
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
	canonicalSHA := sha256.Sum256(canonicalBytes)

	snapshot, err := investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision:     1,
		ExpectedCaseRevision: 1,
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               "investigation-case-observation-batch-r1",
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
	if caseDocument["canonical_case_ref"] != canonicalRel || caseDocument["canonical_case_sha256"] != hex.EncodeToString(canonicalSHA[:]) {
		t.Fatalf("canonical reference = %v/%v, want %s/%s", caseDocument["canonical_case_ref"], caseDocument["canonical_case_sha256"], canonicalRel, hex.EncodeToString(canonicalSHA[:]))
	}
}

func readyCaseFixture(t *testing.T, findingIDs []string) *intakeFixture {
	t.Helper()
	fixture := newIntakeFixture(t, findingIDs)
	setContractLifecycle(t, fixture)
	if _, err := investigation.Ingest(fixture.root, fixture.statePath, fixture.journalPath, investigation.IngestRequest{
		ExpectedRevision:  0,
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

// TestInvestigateMoreLivelockCapRejects covers RC-15 (S9-T2/L1): after the
// investigate_more attempt chain reaches the livelock cap (default 5) the
// route verb fails closed with an actionable converge instruction instead of
// allowing another silent re-entry. Each attempt is legitimately fresh: the
// test registers a new hypothesis with new evidence before re-routing, which
// is exactly the mechanical unlock the fingerprint gate requires — proving
// the cap bites on content-valid re-entry chains, not just stale ones.
func TestInvestigateMoreLivelockCapRejects(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})
	_, caseID, _ := discriminatedCaseFixture(t, fixture)
	pointer := investigationPointer(t, fixture)

	// First route: the discriminated but not causally closed Case may enter
	// investigate_more legitimately.
	if _, err := investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision:     currentRuntimeRevision(t, fixture),
		ExpectedCaseRevision: integerPointerRevision(pointer),
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               caseID,
		Route:                "investigate_more",
		RouteReason:          "the mechanism needs one more discriminator before repair",
	}); err != nil {
		t.Fatalf("initial investigate_more route error = %v", err)
	}

	// Four more full unlock cycles: new hypothesis (new evidence) → re-route.
	// After the 5th investigate_more entry the cap is reached; the 6th route
	// is rejected even with fresh evidence available.
	for attempt := 0; attempt < 4; attempt++ {
		pointer = investigationPointer(t, fixture)
		register := investigation.HypothesisRequest{
			ExpectedRevision:     currentRuntimeRevision(t, fixture),
			ExpectedCaseRevision: integerPointerRevision(pointer),
			ExpectedCaseSHA256:   pointer["sha256"].(string),
			CaseID:               caseID,
			HypothesisID:         fmt.Sprintf("hypothesis-cap-%d", attempt),
			AssignmentID:         fmt.Sprintf("assignment-cap-%d", attempt),
			Statement:            fmt.Sprintf("mechanism variant %d drops the value", attempt),
			Invariant:            "the value survives the boundary",
			Discriminator:        "trace the value through the boundary",
			ExpectedOutcomes:     map[string]any{"support": "the value is dropped", "refute": "the value survives"},
			SourceFindingIDs:     []string{"finding-1"},
			EvidenceRefs:         []string{fmt.Sprintf("evidence://cap-variant-%d", attempt)},
		}
		if _, err := investigation.RegisterHypothesis(fixture.root, fixture.statePath, fixture.journalPath, register); err != nil {
			t.Fatalf("RegisterHypothesis(cap-%d) error = %v", attempt, err)
		}
		pointer = investigationPointer(t, fixture)
		if _, err := investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
			ExpectedRevision:     currentRuntimeRevision(t, fixture),
			ExpectedCaseRevision: integerPointerRevision(pointer),
			ExpectedCaseSHA256:   pointer["sha256"].(string),
			CaseID:               caseID,
			Route:                "investigate_more",
			RouteReason:          "the variant needs another discriminator round",
		}); err != nil {
			t.Fatalf("investigate_more unlock route %d error = %v", attempt, err)
		}
	}

	// The 6th route hits the cap even though a fresh hypothesis could be
	// registered: the Case must converge on a concrete disposition now.
	pointer = investigationPointer(t, fixture)
	_, err := investigation.UpdateCaseRoute(fixture.root, fixture.statePath, fixture.journalPath, investigation.RouteRequest{
		ExpectedRevision:     currentRuntimeRevision(t, fixture),
		ExpectedCaseRevision: integerPointerRevision(pointer),
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               caseID,
		Route:                "investigate_more",
		RouteReason:          "one more unbounded re-entry",
	})
	if err == nil {
		t.Fatal("expected investigate_more route beyond the livelock cap to fail")
	}
	if !strings.Contains(err.Error(), "investigate_more attempts exhausted") {
		t.Fatalf("expected livelock cap error, got %v", err)
	}
}

// TestUpdateCaseRevisionWarnsOnBaselineDigestDrift covers RC-18 S8-H3: every
// Case revision recomputes the frozen ReviewPlan subject digest. When the
// pinned baseline_digest no longer matches, the warning lands in the journal
// message and in review.investigation_baseline_drift for the status board —
// and the pinned digest itself is never rewritten to bless the drift.
func TestUpdateCaseRevisionWarnsOnBaselineDigestDrift(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})
	pointer := investigationPointer(t, fixture)

	planRel := ".claude/review/plans/review-plan-drift.json"
	planBytes := []byte(`{"review_plan_id":"review-plan-drift","frozen_subjects":[{"path":"internal/service/decoder.go","sha256":"` + strings.Repeat("b", 64) + `"}]}` + "\n")
	planPath := filepath.Join(fixture.root, filepath.FromSlash(planRel))
	if err := os.MkdirAll(filepath.Dir(planPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, planBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	planSum := sha256.Sum256(planBytes)
	setStateReviewPlan(t, fixture, "review-plan-drift", planRel, hex.EncodeToString(planSum[:]))

	snapshot, err := investigation.UpdateCase(fixture.root, fixture.statePath, fixture.journalPath, investigation.CaseRevisionRequest{
		ExpectedRevision:     currentRuntimeRevision(t, fixture),
		ExpectedCaseRevision: integerPointerRevision(pointer),
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               "investigation-case-observation-batch-r1",
		Operation:            "drift_probe",
		Mutate:               func(document map[string]any) error { return nil },
	})
	if err != nil {
		t.Fatalf("UpdateCase() error = %v", err)
	}

	// Status-board outlet: the drift warning is readable at review.investigation_baseline_drift.
	warning, _ := snapshot.State["review"].(map[string]any)["investigation_baseline_drift"].(string)
	if !strings.Contains(warning, "baseline_digest drift") || !strings.Contains(warning, strings.Repeat("a", 64)) {
		t.Fatalf("investigation_baseline_drift = %q, want the pinned-digest drift warning", warning)
	}
	// Journal outlet: the revision event message carries the warning.
	event := readLastJournalEvent(t, fixture.journalPath)
	if !strings.Contains(event["message"].(string), "WARNING baseline_digest drift") {
		t.Fatalf("journal message = %#v, want the drift warning", event["message"])
	}
	// The pinned digest in the Case artifact is never rewritten.
	caseRel := snapshot.State["review"].(map[string]any)["investigation"].(map[string]any)["path"].(string)
	var document map[string]any
	if err := json.Unmarshal(mustRead(t, filepath.Join(fixture.root, filepath.FromSlash(caseRel))), &document); err != nil {
		t.Fatal(err)
	}
	if got := document["baseline_digest"]; got != strings.Repeat("a", 64) {
		t.Fatalf("baseline_digest = %v, want the pinned digest unchanged (drift is warned, never blessed)", got)
	}
}

// TestUpdateCaseRevisionClearsBaselineDriftWhenBaselineMatches proves the
// drift projection is cleared (set back to null) once the frozen subjects
// digest to the pinned value again.
func TestUpdateCaseRevisionClearsBaselineDriftWhenBaselineMatches(t *testing.T) {
	fixture := readyCaseFixture(t, []string{"finding-1"})
	pointer := investigationPointer(t, fixture)

	planRel := ".claude/review/plans/review-plan-aligned.json"
	planBytes := []byte(`{"review_plan_id":"review-plan-aligned","frozen_subjects":[]}` + "\n")
	planPath := filepath.Join(fixture.root, filepath.FromSlash(planRel))
	if err := os.MkdirAll(filepath.Dir(planPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, planBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	// The subject digest of an empty frozen_subjects list is sha256("");
	// seed the Case with that pinned digest so the recomputation matches.
	emptyDigest := sha256.Sum256([]byte(""))
	planSum := sha256.Sum256(planBytes)
	setStateReviewPlan(t, fixture, "review-plan-aligned", planRel, hex.EncodeToString(planSum[:]))

	_, err := investigation.UpdateCase(fixture.root, fixture.statePath, fixture.journalPath, investigation.CaseRevisionRequest{
		ExpectedRevision:     currentRuntimeRevision(t, fixture),
		ExpectedCaseRevision: integerPointerRevision(pointer),
		ExpectedCaseSHA256:   pointer["sha256"].(string),
		CaseID:               "investigation-case-observation-batch-r1",
		Operation:            "align_baseline",
		Mutate: func(document map[string]any) error {
			document["baseline_digest"] = hex.EncodeToString(emptyDigest[:])
			return nil
		},
	})
	if err != nil {
		t.Fatalf("UpdateCase() error = %v", err)
	}
	snapshot, err := runtime.NewStore(fixture.statePath, fixture.journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if drift := snapshot.State["review"].(map[string]any)["investigation_baseline_drift"]; drift != nil {
		t.Fatalf("investigation_baseline_drift = %v, want nil when the baseline matches", drift)
	}
}

// setStateReviewPlan pins a ReviewPlan pointer into review.plan so SubjectDigest
// can be recomputed during Case revisions.
func setStateReviewPlan(t *testing.T, fixture *intakeFixture, planID, planRel, planSHA string) {
	t.Helper()
	data, err := os.ReadFile(fixture.statePath)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	review := document["review"].(map[string]any)
	review["plan"] = map[string]any{
		"plan_id": planID, "path": planRel, "sha256": planSHA, "revision": 1,
		"review_round": 1, "status": "running",
		"e2e_coverage_state": "not_applicable", "submitted_at": "2026-08-25T00:00:00Z",
	}
	updated, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.statePath, append(updated, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
