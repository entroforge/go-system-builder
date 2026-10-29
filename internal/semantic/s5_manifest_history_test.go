package semantic

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestS5ManifestHistoryRequiresOriginalPacketPinAndSkipsOnlyOldDocumentHashes(t *testing.T) {
	t.Run("historical S5 during planning keeps manifest bytes but tolerates current source drift", func(t *testing.T) {
		root, state, manifestPath, documentPath := s5ManifestHistoryFixture(t, "planning", 7, 1, 4, true)
		if err := os.WriteFile(filepath.Join(root, documentPath), []byte("# Current TASK\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		statuses, err := state.s5ManifestStatuses(root)
		if err != nil {
			t.Fatalf("read original S5 binding: %v", err)
		}
		status := statuses[manifestPath]
		if !status.Bound || !status.Historical {
			t.Fatalf("status = %+v, want a packet-bound historical S5 manifest", status)
		}
		if err := validateReviewManifestReferencesWithHistory(root, manifestPath, "loop-s5", status.Historical); err != nil {
			t.Fatalf("historical S5 manifest should retain schema/runtime/path checks without re-hashing its old source: %v", err)
		}
		if got := crossManifestDependencyRefs([]string{manifestPath, "docs/teams/current/manifest.json"}, statuses); len(got) != 1 || got[0] != "docs/teams/current/manifest.json" {
			t.Fatalf("cross-manifest refs = %#v, want only current refs", got)
		}

		manifestFile := filepath.Join(root, manifestPath)
		if err := os.WriteFile(manifestFile, append(mustReadS5TestFile(t, manifestFile), '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := state.s5ManifestStatuses(root); err == nil || !strings.Contains(err.Error(), "original packet binding") {
			t.Fatalf("edited historical manifest should fail its original packet pin, got %v", err)
		}
	})

	t.Run("current S5 and unbound legacy manifests keep strict document hashes", func(t *testing.T) {
		root, current, manifestPath, documentPath := s5ManifestHistoryFixture(t, "document_verification", 8, 1, 8, true)
		if err := os.WriteFile(filepath.Join(root, documentPath), []byte("# Current TASK\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		statuses, err := current.s5ManifestStatuses(root)
		if err != nil {
			t.Fatal(err)
		}
		status := statuses[manifestPath]
		if !status.Bound || status.Historical {
			t.Fatalf("current entry status = %+v, want bound/current", status)
		}
		if err := validateReviewManifestReferencesWithHistory(root, manifestPath, "loop-s5", status.Historical); err == nil || !strings.Contains(err.Error(), manifestPath) {
			t.Fatalf("current S5 source drift should fail with manifest path, got %v", err)
		}

		legacyRoot, legacy, legacyPath, legacyDocument := s5ManifestHistoryFixture(t, "document_verification", 8, 1, 8, false)
		if err := os.WriteFile(filepath.Join(legacyRoot, legacyDocument), []byte("# Current TASK\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		legacyStatuses, err := legacy.s5ManifestStatuses(legacyRoot)
		if err != nil {
			t.Fatal(err)
		}
		legacyStatus := legacyStatuses[legacyPath]
		if legacyStatus.Bound || legacyStatus.Historical {
			t.Fatalf("unbound legacy status = %+v, want strict", legacyStatus)
		}
		if err := validateReviewManifestReferencesWithHistory(legacyRoot, legacyPath, "loop-s5", legacyStatus.Historical); err == nil {
			t.Fatal("unbound manifest was allowed to ignore current document drift")
		}
	})

	t.Run("old generation S5 is historical but current builder manifest is not", func(t *testing.T) {
		root, oldGeneration, s5Path, documentPath := s5ManifestHistoryFixture(t, "building", 2, 2, 8, true)
		if err := os.WriteFile(filepath.Join(root, documentPath), []byte("# Current TASK\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		statuses, err := oldGeneration.s5ManifestStatuses(root)
		if err != nil {
			t.Fatal(err)
		}
		if !statuses[s5Path].Historical {
			t.Fatalf("old-generation S5 status = %+v, want historical", statuses[s5Path])
		}
		builderPath := "docs/teams/current-builder/manifest.json"
		if got := crossManifestDependencyRefs([]string{s5Path, builderPath}, statuses); len(got) != 1 || got[0] != builderPath {
			t.Fatalf("dependency refs = %#v, want current builder only", got)
		}
		if !strings.HasPrefix(s5Path, ".claude/workgroups") {
			t.Fatalf("fixture did not use S5 workgroup path: %s", s5Path)
		}
	})

	t.Run("latest S5 entry stays strict through building execution", func(t *testing.T) {
		root, building, manifestPath, documentPath := s5ManifestHistoryFixture(t, "building", 9, 1, 8, true)
		if err := os.WriteFile(filepath.Join(root, documentPath), []byte("# Current TASK\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		statuses, err := building.s5ManifestStatuses(root)
		if err != nil {
			t.Fatal(err)
		}
		if statuses[manifestPath].Historical {
			t.Fatalf("latest S5 execution baseline was classified as history: %+v", statuses[manifestPath])
		}
		if err := validateReviewManifestReferencesWithHistory(root, manifestPath, "loop-s5", statuses[manifestPath].Historical); err == nil {
			t.Fatal("building execution allowed drift from the latest accepted S5 input")
		}
	})

	t.Run("future S5 entry is not treated as historical", func(t *testing.T) {
		root, future, manifestPath, _ := s5ManifestHistoryFixture(t, "document_verification", 7, 1, 8, true)
		if _, err := future.s5ManifestStatuses(root); err == nil || !strings.Contains(err.Error(), "future Runtime entry") {
			t.Fatalf("future S5 binding was not rejected, manifest=%s err=%v", manifestPath, err)
		}
	})
}

func s5ManifestHistoryFixture(t *testing.T, lifecycle string, currentEntry, currentGeneration, packetEntry int, withPacket bool) (string, runtimeReachability, string, string) {
	t.Helper()
	root := t.TempDir()
	manifestPath := ".claude/workgroups/review-e4/manifest.json"
	documentPath := "docs/dev/tasks/TASK-001.md"
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, documentPath)), 0o755); err != nil {
		t.Fatal(err)
	}
	documentBytes := []byte("# Original TASK\n")
	if err := os.WriteFile(filepath.Join(root, documentPath), documentBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	manifest := map[string]any{
		"runtime_id": "loop-s5", "baseline_generation": 1,
		"workgroup_id": "workgroup-review-e4", "workgroup_kind": "document_verifier",
		"assignments": []any{map[string]any{
			"assignment_id": "assignment-review-e4", "agent_id": "reviewer-e4",
			"responsibility_id": "DV-SPEC-CONSISTENCY",
		}},
		"documents": []any{map[string]any{
			"id": "TASK-001", "path": documentPath, "sha256": fmt.Sprintf("%x", sha256.Sum256(documentBytes)),
		}},
	}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, manifestPath)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, manifestPath), manifestData, 0o644); err != nil {
		t.Fatal(err)
	}
	packets := []any{}
	if withPacket {
		packets = append(packets, map[string]any{
			"template_id": "s5-document-review-v1",
			"binding": map[string]any{
				"runtime_id": "loop-s5", "generation": 1, "stage_entry": packetEntry,
				"owner": "reviewer-e4", "assignment_id": "assignment-review-e4",
				"assignment_sha256": fmt.Sprintf("%x", sha256.Sum256(manifestData)),
				"responsibility":    "DV-SPEC-CONSISTENCY",
			},
		})
	}
	runtimeData, err := json.Marshal(map[string]any{
		"runtime_id": "loop-s5",
		"baseline":   map[string]any{"generation": currentGeneration},
		"lifecycle":  map[string]any{"state": lifecycle, "phase_revision": currentEntry},
		"entities": map[string]any{
			"teams": []any{map[string]any{
				"id": "workgroup-review-e4", "kind": "document_verifier", "status": "active",
				"packet_generation": 1, "packet_stage_entry": packetEntry, "manifest_ref": manifestPath,
			}},
			"agents": []any{map[string]any{
				"id": "reviewer-e4", "role": "document-verifier", "team_id": "workgroup-review-e4",
				"prompt_ref": manifestPath + "#assignment-review-e4",
			}},
			"work_packets": packets,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var state runtimeReachability
	if err := json.Unmarshal(runtimeData, &state); err != nil {
		t.Fatal(err)
	}
	return root, state, manifestPath, documentPath
}

func mustReadS5TestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
