// s7_manifest_draft_test.go proves the `s7 manifest-draft` CLI path: the
// subcommand parses its flags, reads the control plane read-only, and emits
// a schema-valid reviewer manifest draft pre-filled from the registered
// ReviewPlan.
package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/schema"
)

// writeManifestDraftFixture builds a minimal runtime with a registered
// ReviewPlan carrying one QA assignment.
func writeManifestDraftFixture(t *testing.T, root string) {
	t.Helper()
	data, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["journal"] = map[string]any{
		"path":          ".claude/loop-events.jsonl",
		"last_sequence": 0,
		"last_event_id": nil,
	}
	planBytes, err := json.MarshalIndent(map[string]any{
		"schema_version":      "1.0.0",
		"review_plan_id":      "review-plan-cli-md-1",
		"review_round":        1,
		"baseline_generation": 1,
		"frozen_subjects": []any{
			map[string]any{"path": "internal/example/service.go", "sha256": strings.Repeat("1", 64), "kind": "product_code"},
		},
		"claims": []any{
			map[string]any{
				"claim_id": "claim-qa-1", "lens": "qa",
				"target": "internal/example", "assertion": "errors propagate", "oracle": "no dropped error",
				"method": "code review", "applicability": "required", "source_refs": []string{"REQ-002"},
				"focus_key": "logic-state-error",
			},
		},
		"assignments": []any{
			map[string]any{
				"assignment_id": "assignment-qa-static", "lens": "qa",
				"claim_ids":            []string{"claim-qa-1"},
				"focus_keys":           []string{"logic-state-error"},
				"non_overlap_boundary": "owns static quality",
				"execution_wave":       "static",
			},
		},
		"e2e_coverage_state":       "not_applicable",
		"dispatch_capacity_policy": "coverage_complete",
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	planBytes = append(planBytes, '\n')
	sum := sha256.Sum256(planBytes)
	planRel := filepath.Join(".claude", "review", "plans", "review-plan-cli-md-1.json")
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(planRel)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, planRel), planBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	state["review"] = map[string]any{
		"round":       1,
		"clean_round": nil,
		"plan": map[string]any{
			"plan_id":      "review-plan-cli-md-1",
			"path":         filepath.ToSlash(planRel),
			"sha256":       fmt.Sprintf("%x", sum[:]),
			"revision":     1,
			"review_round": 1,
			"status":       "running",
		},
		"claims": map[string]any{},
		"assignments": map[string]any{
			"assignment-qa-static": map[string]any{
				"lens": "qa", "status": "planned", "claim_ids": []any{"claim-qa-1"},
			},
		},
	}
	stateData, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), append(stateData, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestS7ManifestDraftEndToEnd(t *testing.T) {
	root := t.TempDir()
	writeManifestDraftFixture(t, root)

	out := filepath.Join(root, "manifest-draft.json")
	var stdout, stderr bytes.Buffer
	code := runS7Command([]string{"manifest-draft", "--root", root, "--assignment", "assignment-qa-static", "--out", out}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("s7 manifest-draft exit = %d, stdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "draft team manifest written") {
		t.Errorf("expected the write confirmation, got:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "note:") {
		t.Errorf("expected planner notes, got:\n%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "agent_id") || !strings.Contains(stdout.String(), "registration rejects") {
		t.Errorf("manifest-draft guidance must explain the identity replacement and hard registration gate, got:\n%s", stdout.String())
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	// The emitted draft passes the register-workgroup schema gate.
	if err := schema.NewValidator(root).ValidateBytes("team-manifest.schema.json", data); err != nil {
		t.Fatalf("draft fails team-manifest schema: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest["workgroup_kind"] != "qa" {
		t.Errorf("workgroup_kind = %v, want qa", manifest["workgroup_kind"])
	}
	if manifest["runtime_id"] != "loop-REQ-002-example" {
		t.Errorf("runtime_id = %v, want the fixture runtime id", manifest["runtime_id"])
	}
	rows, _ := manifest["assignments"].([]any)
	if len(rows) != 1 {
		t.Fatalf("assignments = %d rows, want 1", len(rows))
	}
	row, _ := rows[0].(map[string]any)
	if row["assignment_id"] != "assignment-qa-static" {
		t.Errorf("assignment_id = %v", row["assignment_id"])
	}
	claims, _ := row["claim_ids"].([]any)
	if len(claims) != 1 || claims[0] != "claim-qa-1" {
		t.Errorf("claim_ids = %v, want the exact plan set", claims)
	}

	// The control plane is untouched: re-running produces the same state.
	before, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var again bytes.Buffer
	if code := runS7Command([]string{"manifest-draft", "--root", root, "--assignment", "assignment-qa-static"}, &again, &stderr); code != 0 {
		t.Fatalf("second run exit = %d", code)
	}
	after, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("s7 manifest-draft must be read-only against the control plane")
	}
}

func TestS7ManifestDraftRequiresAssignment(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runS7Command([]string{"manifest-draft"}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "--assignment") {
		t.Errorf("stderr should name the missing flag, got:\n%s", stderr.String())
	}
}
