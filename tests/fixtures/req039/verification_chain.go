package req039fixtures

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type verificationDimensionSpec struct {
	dimension      string
	workgroupKind  string
	reviewRecord   string
	wireReview     string
	responsibility string
	agent          string
	e2eNA          bool
}

var verificationDimensions = map[string]verificationDimensionSpec{
	"delivery": {
		dimension: "delivery", workgroupKind: "delivery_verifier",
		reviewRecord: "delivery_review_record", wireReview: "delivery_review",
		responsibility: "Delivery Verifier", agent: "delivery-1",
	},
	"qa": {
		dimension: "qa", workgroupKind: "qa",
		reviewRecord: "qa_review_record", wireReview: "qa_review",
		responsibility: "QA", agent: "qa-1",
	},
	"e2e_browser": {
		dimension: "e2e_browser", workgroupKind: "e2e_browser",
		reviewRecord: "e2e_review_record", wireReview: "e2e_review",
		responsibility: "E2E Browser", agent: "e2e-1", e2eNA: true,
	},
}

// WriteVerificationDimensionPass appends angle_declaration, team_manifest, and
// dimension PASS review evidence required by PTR-VERIFY-01/02/03 guards.
func WriteVerificationDimensionPass(t *testing.T, root string, state map[string]any, dimension string) {
	t.Helper()
	spec, ok := verificationDimensions[dimension]
	if !ok {
		t.Fatalf("unknown verification dimension %q", dimension)
	}
	EnsureStateRoot(state, root)
	round := reviewRoundFromState(state)
	if spec.e2eNA {
		if meta, ok := state["bound_req"].(map[string]any); ok {
			meta["metadata"] = map[string]any{"ui_impact": "none"}
		}
	}
	writeAngleAndManifest(t, root, state, spec, round)
	evID := "ev-" + spec.dimension + "-pass"
	envelope := EvidenceEnvelope(state, evID, spec.wireReview, spec.agent, spec.responsibility, "pass", map[string]any{
		"review_round": round,
	})
	AppendEvidence(state, WriteEvidenceEnvelope(t, root, state, evID, spec.wireReview, spec.agent, spec.responsibility, envelope, []any{}))
}

// WriteCleanRoundEvaluationPass seeds all three verification dimensions plus a
// same-round clean_round pass record for PTR-VERIFY-04 / TR-009.
// EnsureVerificationWorkgroups registers review teams required by
// all_required_dimensions_passed (OrganicSpine has empty entities.teams after TR-006).
func WriteCleanRoundEvaluationPass(t *testing.T, root string, state map[string]any) {
	t.Helper()
	EnsureVerificationWorkgroups(state)
	for _, dim := range []string{"delivery", "qa", "e2e_browser"} {
		WriteVerificationDimensionPass(t, root, state, dim)
	}
	round := reviewRoundFromState(state)
	envelope := EvidenceEnvelope(state, "ev-clean-round-pass", "clean_round", "orchestrator-1", "Orchestrator", "pass", map[string]any{
		"review_round": round,
	})
	AppendEvidence(state, WriteEvidenceEnvelope(t, root, state, "ev-clean-round-pass", "clean_round", "orchestrator-1", "Orchestrator", envelope, []any{}))
	state["review"] = map[string]any{"round": round, "clean_round": round}
}

func reviewRoundFromState(state map[string]any) int {
	review, _ := state["review"].(map[string]any)
	if review == nil {
		return 1
	}
	switch v := review["round"].(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 1
	}
}

func writeAngleAndManifest(t *testing.T, root string, state map[string]any, spec verificationDimensionSpec, round int) {
	t.Helper()
	runtimeID := RuntimeIDFromState(state)
	reqID := "REQ-039"
	if br, ok := state["bound_req"].(map[string]any); ok {
		if id, _ := br["id"].(string); id != "" {
			reqID = id
		}
	}
	declared := buildDeclaredAngles(spec)
	dispatchedAt := "2026-07-30T09:00:00Z"
	committedAt := "2026-07-30T08:59:59Z"
	angleBody := map[string]any{
		"schema_version": "1.0.0", "evidence_id": "ev-angle-" + spec.dimension,
		"runtime_id": runtimeID, "req_id": reqID, "review_round": round,
		"dimension": spec.dimension, "declared_at": committedAt, "committed_at": committedAt,
		"declared_angles": declared,
		"dispositions":    buildDispositions(spec),
	}
	angleRel := writeEvidenceFile(t, root, "ev-angle-"+spec.dimension+".json", mustJSON(t, angleBody))
	angleEntry := evidenceIndexEntry("ev-angle-"+spec.dimension, "angle_declaration", angleRel, Sha256Hex(mustJSON(t, angleBody)), round, spec.agent, spec.responsibility, []any{})
	angleEntry["dimension"] = spec.dimension
	AppendEvidence(state, angleEntry)

	manifestBody := map[string]any{
		"schema_version": "1.0.0", "manifest_id": "team-manifest-" + spec.dimension,
		"version": "v1.0.0", "runtime_id": runtimeID, "req_id": reqID,
		"baseline_generation": 1, "review_round": round,
		"platform_team_id": "platform-" + spec.dimension,
		"workgroup_id":     "workgroup-" + spec.dimension,
		"workgroup_kind":   spec.workgroupKind, "dimension": spec.dimension,
		"status": "active", "dispatched_at": dispatchedAt,
		"inherited_angles": buildInheritedAngles(spec),
	}
	if !spec.e2eNA {
		manifestBody["min_angles"] = 3
	}
	manifestRel := writeEvidenceFile(t, root, "ev-manifest-"+spec.dimension+".json", mustJSON(t, manifestBody))
	manifestEntry := evidenceIndexEntry("ev-manifest-"+spec.dimension, "team_manifest", manifestRel, Sha256Hex(mustJSON(t, manifestBody)), round, spec.agent, spec.responsibility, []any{})
	manifestEntry["dimension"] = spec.dimension
	AppendEvidence(state, manifestEntry)
}

func buildDeclaredAngles(spec verificationDimensionSpec) []map[string]any {
	if spec.e2eNA {
		return []map[string]any{{
			"id": "DECL-E2E-001", "statement": "ui impact unchanged",
			"target": "blockquote.ui_impact",
		}}
	}
	targets := []string{
		"internal/cli/projection.go",
		"internal/runtime/store.go",
		"docs/loop-definition.json",
	}
	out := make([]map[string]any, len(targets))
	for i, target := range targets {
		out[i] = map[string]any{
			"id": fmtID("DECL", spec.dimension, i+1), "statement": "investigate " + target, "target": target,
		}
	}
	return out
}

func buildInheritedAngles(spec verificationDimensionSpec) []map[string]any {
	if spec.e2eNA {
		return []map[string]any{}
	}
	return []map[string]any{{
		"id": "ANG-RUNTIME-001", "module": "internal/runtime",
		"statement": "runtime angle", "target": "internal/runtime/store.go",
		"last_applied_in": "REQ-001",
	}}
}

func buildDispositions(spec verificationDimensionSpec) []map[string]any {
	if spec.e2eNA {
		return []map[string]any{}
	}
	return []map[string]any{{
		"angle_id": "ANG-RUNTIME-001", "kind": "confirm", "note": "still relevant",
	}}
}

func fmtID(prefix, dimension string, n int) string {
	return fmt.Sprintf("%s-%s-%03d", prefix, dimension, n)
}

func mustJSON(t *testing.T, body map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// EnsureVerificationWorkgroups registers the three review workgroup kinds
// required by verification.EvaluateCleanRound (PTR-VERIFY-04 / TR-009).
func EnsureVerificationWorkgroups(state map[string]any) {
	entities, _ := state["entities"].(map[string]any)
	if entities == nil {
		entities = map[string]any{}
		state["entities"] = entities
	}
	teams, _ := entities["teams"].([]any)
	present := map[string]bool{}
	for _, raw := range teams {
		team, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if kind, _ := team["kind"].(string); kind != "" {
			present[kind] = true
		}
	}
	round := reviewRoundFromState(state)
	for _, spec := range verificationDimensions {
		if present[spec.workgroupKind] {
			continue
		}
		teams = append(teams, map[string]any{
			"id":                 "team-" + spec.dimension,
			"platform_team_id":   "platform-" + spec.dimension,
			"kind":               spec.workgroupKind,
			"status":             "complete",
			"manifest_ref":       filepath.Join("evidence", "ev-manifest-"+spec.dimension+".json"),
			"responsibility_ids": []any{spec.responsibility},
			"agent_ids":          []any{spec.agent},
			"review_round":       round,
		})
	}
	entities["teams"] = teams
}

// WriteBlockingFindingEvidence appends a blocking finding for TR-008.
func WriteBlockingFindingEvidence(t *testing.T, root string, state map[string]any, agent, responsibility string) {
	t.Helper()
	envelope := EvidenceEnvelope(state, "ev-blocking-finding", "bug", agent, responsibility, "blocking", map[string]any{
		"requested_event": "blocking_findings_reported",
		"review_round":    reviewRoundFromState(state),
	})
	AppendEvidence(state, WriteEvidenceEnvelope(t, root, state, "ev-blocking-finding", "bug", agent, responsibility, envelope, []any{}))
}

// EnsureREQDoc writes the bound REQ file when angle guards need ui_impact metadata.
func EnsureREQDoc(t *testing.T, root string, state map[string]any, uiImpact string) {
	t.Helper()
	reqPath := "docs/requirements/REQ-039-loop-control-plane.md"
	if br, ok := state["bound_req"].(map[string]any); ok {
		if p, _ := br["path"].(string); p != "" {
			reqPath = p
		}
	}
	content := []byte("# REQ-039\n> UI impact: " + uiImpact + "\n")
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, reqPath)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, reqPath), content, 0o644); err != nil {
		t.Fatal(err)
	}
}
