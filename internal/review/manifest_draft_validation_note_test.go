// The draft disclosure identifies preparation metadata and preserves the original
// registration validator as the authority; Main need not rewrite machine tuples.
package review

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

// writeManifestDraftFixtureForNoteTest mirrors the fixture in
// internal/cli/s7_manifest_draft_test.go but is local so the
// assertion lives in the review package which owns the production
// code that emits the notes.
func writeManifestDraftFixtureForNoteTest(t *testing.T, root string) {
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
		"review_plan_id":      "review-plan-dn-1",
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
	planRel := filepath.Join(".claude", "review", "plans", "review-plan-dn-1.json")
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
			"plan_id":            "review-plan-dn-1",
			"path":               filepath.ToSlash(planRel),
			"sha256":             fmt.Sprintf("%x", sum[:]),
			"revision":           1,
			"review_round":       1,
			"status":             "running",
			"e2e_coverage_state": "not_applicable",
			"submitted_at":       "2026-08-22T00:00:00Z",
		},
		"claims": map[string]any{},
		"assignments": map[string]any{
			"assignment-qa-static": map[string]any{
				"lens": "qa", "status": "planned", "claim_ids": []any{"claim-qa-1"},
			},
		},
	}
	out, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDraftManifestValidationDisclosureNote rejects misleading PASS and manual metadata guidance.
func TestDraftManifestValidationDisclosureNote(t *testing.T) {
	root := t.TempDir()
	writeManifestDraftFixtureForNoteTest(t, root)

	data, err := os.ReadFile(filepath.Join(root, ".claude", "loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	draft, notes, err := DraftManifest(root, state, "assignment-qa-static")
	if err != nil {
		t.Fatalf("DraftManifest: %v", err)
	}

	var buffer bytes.Buffer
	for _, note := range notes {
		buffer.WriteString(note)
		buffer.WriteString("\n")
	}
	rendered := buffer.String()

	for _, want := range []string{
		"validation uses the existing pass|warn|block schema",
		"warn and its timestamp describe this generated draft",
		"not professional review or registration acceptance",
		"original register-workgroup validates the completed manifest",
		"no manual validation tuple or timestamp is required",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("validation disclosure note missing %q:\n%s", want, rendered)
		}
	}
	if draft.Validation.Result != "warn" || len(draft.Validation.Warnings) == 0 {
		t.Errorf("incomplete draft must disclose preparation warnings, got %+v", draft.Validation)
	}
	for _, unwanted := range []string{"hand-fill", "pass|warnings|fail", "pre-filled as pass"} {
		if strings.Contains(rendered, unwanted) {
			t.Errorf("obsolete validation guidance %q:\n%s", unwanted, rendered)
		}
	}
}
