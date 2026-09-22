package qualitygate_test

import (
	"context"
	"encoding/json"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"testing"
)

func TestS10CorrectionSelectsOnlyVerifiedLatestEnvelope(t *testing.T) {
	for _, tampered := range []bool{false, true} {
		input := s10ReviewRequiredInput(t, "GATE-RELEASE-AUDIT-REVIEW-REQUIRED", "TR-031", nil)
		input.Snapshot.State["lifecycle"].(map[string]any)["state"] = "release_audit"
		files := input.Files.(memoryFiles)
		manifest := validS10Manifest(t, "acceptance")
		files["manifest.json"] = manifest
		rows := input.Snapshot.State["evidence"].([]any)
		old := rows[0].(map[string]any)
		var envelope map[string]any
		if err := json.Unmarshal(files[old["path"].(string)], &envelope); err != nil {
			t.Fatal(err)
		}
		envelope["evidence_id"] = "zz-corrected"
		envelope["audit_manifest_path"] = "manifest.json"
		envelope["audit_manifest_sha256"] = sha256Hex(manifest)
		data, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		files["corrected.json"] = data
		corrected := map[string]any{}
		for k, v := range old {
			corrected[k] = v
		}
		corrected["id"] = "zz-corrected"
		corrected["path"] = "corrected.json"
		corrected["sha256"] = sha256Hex(data)
		input.Snapshot.State["evidence"] = append(rows, corrected)
		if tampered {
			files["manifest.json"] = []byte("tampered")
		}
		got, err := newTestEvaluator(t).Evaluate(context.Background(), input)
		if err != nil {
			t.Fatal(err)
		}
		if tampered {
			if got.Status == qualitygate.StatusSatisfied {
				t.Fatal("tampered latest manifest passed")
			}
		} else {
			if got.Status != qualitygate.StatusSatisfied {
				t.Fatalf("status=%s missing=%v conflicts=%v", got.Status, got.Missing, got.Conflicts)
			}
			if contains(got.EvidenceRefs, "ev-acc") || !contains(got.EvidenceRefs, "zz-corrected") {
				t.Fatalf("commit refs differ from checked manifest: %v", got.EvidenceRefs)
			}
		}
	}
}

func TestUnauthorizedHistoricalProducerDoesNotReplaceQualifiedEvidence(t *testing.T) {
	for _, corrected := range []bool{false, true} {
		input := reviewGateInput(t, 3)
		files := input.Files.(memoryFiles)
		rows := input.Snapshot.State["evidence"].([]any)
		original := rows[0].(map[string]any)
		var envelope map[string]any
		if err := json.Unmarshal(files[original["path"].(string)], &envelope); err != nil {
			t.Fatal(err)
		}
		envelope["evidence_id"] = "foreign"
		envelope["producer_responsibility"] = "Builder"
		data, _ := json.Marshal(envelope)
		files["foreign.json"] = data
		foreign := map[string]any{}
		for k, v := range original {
			foreign[k] = v
		}
		foreign["id"] = "foreign"
		foreign["path"] = "foreign.json"
		foreign["sha256"] = sha256Hex(data)
		foreign["responsibility_id"] = "Builder"
		if corrected {
			input.Snapshot.State["evidence"] = append(rows, foreign)
		} else {
			rows[0] = foreign
		}
		got, err := newTestEvaluator(t).Evaluate(context.Background(), input)
		if err != nil {
			t.Fatal(err)
		}
		if contains(got.EvidenceRefs, "foreign") {
			t.Fatal("unauthorized evidence qualified")
		}
		if corrected && contains(got.Conflicts, "evidence:foreign:producer") {
			t.Fatalf("historical producer wedges corrected slot: %v", got.Conflicts)
		}
		if !corrected && !contains(got.Conflicts, "evidence:foreign:producer") {
			t.Fatalf("missing producer conflict: %v", got)
		}
	}
}
