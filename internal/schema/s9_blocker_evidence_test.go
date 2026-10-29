package schema_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

type s9EvidenceShapeCase struct {
	name   string
	mutate func(map[string]any)
}

// These checks cover only the v1.2 Runtime evidence row shape. They do not
// prove that an original S9 lifecycle Writer authorized or accepted a row.
func TestS9PipelineOwnedEvidenceRowsAcceptAllHistoricalStatuses(t *testing.T) {
	validator := schema.NewEmbeddedValidator()
	for _, kind := range []string{"repair_result_blocker", "repair_result_resolution"} {
		for _, status := range []string{"valid", "invalid", "superseded"} {
			t.Run(kind+"/"+status, func(t *testing.T) {
				state := s9EvidenceSchemaState(t, "1.2.0", s9EvidenceRow(kind, status))
				if err := s9ValidateRuntimeState(validator, state); err != nil {
					t.Fatalf("valid %s evidence row with historical status %q rejected: %v", kind, status, err)
				}
			})
		}
	}
}

func TestS9PipelineOwnedEvidenceRowsRejectOutOfContractShape(t *testing.T) {
	validator := schema.NewEmbeddedValidator()
	for _, kind := range []string{"repair_result_blocker", "repair_result_resolution"} {
		cases := []s9EvidenceShapeCase{
			{name: "scope refs too few", mutate: func(row map[string]any) { row["scope_refs"] = []any{"packet-" + strings.Repeat("b", 64)} }},
			{name: "scope refs too many", mutate: func(row map[string]any) {
				row["scope_refs"] = append(row["scope_refs"].([]any), "unexpected-extra")
			}},
			{name: "duplicate scope refs", mutate: func(row map[string]any) {
				refs := row["scope_refs"].([]any)
				refs[1] = refs[0]
			}},
			{name: "bad id address", mutate: func(row map[string]any) { row["id"] = "evidence/arbitrary.json" }},
			{name: "bad physical path address", mutate: func(row map[string]any) { row["path"] = "../../outside.json" }},
			{name: "zero generation", mutate: func(row map[string]any) { row["baseline_generation"] = 0 }},
			{name: "missing round", mutate: func(row map[string]any) { row["review_round"] = nil }},
			{name: "zero round", mutate: func(row map[string]any) { row["review_round"] = 0 }},
		}
		if kind == "repair_result_blocker" {
			cases = append(cases,
				s9EvidenceShapeCase{name: "multiple producers", mutate: func(row map[string]any) { row["produced_by"] = []any{"agent-owner", "main"} }},
				s9EvidenceShapeCase{name: "wrong responsibility", mutate: func(row map[string]any) { row["responsibility_id"] = "OTHER" }},
			)
		} else {
			cases = append(cases,
				s9EvidenceShapeCase{name: "wrong resolution directory", mutate: func(row map[string]any) {
					row["path"] = strings.Replace(row["path"].(string), "/blocker-messages/", "/blocker-resolution/", 1)
				}},
				s9EvidenceShapeCase{name: "non-main producer", mutate: func(row map[string]any) { row["produced_by"] = []any{"agent-owner"} }},
				s9EvidenceShapeCase{name: "additional producer", mutate: func(row map[string]any) { row["produced_by"] = []any{"main", "agent-owner"} }},
				s9EvidenceShapeCase{name: "non-null responsibility", mutate: func(row map[string]any) { row["responsibility_id"] = "BUILD-WORK-PACKAGE" }},
			)
		}
		for _, test := range cases {
			t.Run(kind+"/"+test.name, func(t *testing.T) {
				row := s9EvidenceRow(kind, "valid")
				test.mutate(row)
				state := s9EvidenceSchemaState(t, "1.2.0", row)
				if err := s9ValidateRuntimeState(validator, state); err == nil {
					t.Fatalf("invalid %s evidence shape %q was accepted", kind, test.name)
				}
			})
		}
	}
}

func TestS9PipelineOwnedEvidenceKindsAreV12OnlyAndOrdinaryEvidenceStaysCompatible(t *testing.T) {
	validator := schema.NewEmbeddedValidator()
	for _, kind := range []string{"repair_result_blocker", "repair_result_resolution"} {
		state := s9EvidenceSchemaState(t, "1.1.0", s9EvidenceRow(kind, "valid"))
		if err := s9ValidateRuntimeState(validator, state); err == nil {
			t.Fatalf("v1.1 Runtime schema accepted v1.2-only evidence kind %q", kind)
		}
	}

	ordinary := map[string]any{
		"id": "evidence-design-1", "kind": "planning_design", "path": "evidence/design.json",
		"sha256": strings.Repeat("a", 64), "status": "valid", "baseline_generation": 1, "review_round": nil,
		"produced_by": []any{"agent-architect"}, "invalidated_by": nil,
		"invalidation_rule": nil, "invalidation_reason": nil,
		"responsibility_id": "Architect", "scope_refs": []any{},
	}
	if err := s9ValidateRuntimeState(validator, s9EvidenceSchemaState(t, "1.2.0", ordinary)); err != nil {
		t.Fatalf("ordinary planning_design evidence compatibility changed: %v", err)
	}
}

func s9EvidenceRow(kind, status string) map[string]any {
	packetID := "packet-" + strings.Repeat("b", 64)
	artifactDigest := strings.Repeat("c", 64)
	base := ".claude/evidence/loop-REQ-001/g2/S9/"
	rowPath := base + "blockers/" + packetID + "/" + artifactDigest + ".json"
	responsibility := any("BUILD-WORK-PACKAGE")
	producer := []any{"agent-builder-1"}
	scopeRefs := []any{packetID, "repair-assignment-1"}
	if kind == "repair_result_resolution" {
		rowPath = base + "blocker-messages/" + packetID + "/" + artifactDigest + ".json"
		responsibility = nil
		producer = []any{"main"}
		scopeRefs = []any{packetID, "repair-assignment-1", base + "blockers/" + packetID + "/" + strings.Repeat("d", 64) + ".json"}
	}
	invalidatedBy, invalidationRule, invalidationReason := any(nil), any(nil), any(nil)
	if status == "invalid" || status == "superseded" {
		invalidatedBy, invalidationRule, invalidationReason = "evt-test-invalidation", "test-rule", "test historical invalidation"
	}
	return map[string]any{
		"id": rowPath, "kind": kind, "path": rowPath, "sha256": artifactDigest,
		"status": status, "baseline_generation": 2, "review_round": 3,
		"produced_by": producer, "invalidated_by": invalidatedBy,
		"invalidation_rule": invalidationRule, "invalidation_reason": invalidationReason,
		"responsibility_id": responsibility, "scope_refs": scopeRefs,
	}
}

func s9EvidenceSchemaState(t *testing.T, version string, row map[string]any) []byte {
	t.Helper()
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["schema_version"] = version
	state["evidence"] = []any{row}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func s9ValidateRuntimeState(validator *schema.Validator, state []byte) error {
	return validator.ValidateBytes("loop-state.schema.json", state)
}
