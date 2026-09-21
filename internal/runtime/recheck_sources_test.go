package runtime_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/transition"
)

// TestRecheckMutableReviewRefreshesWithoutChangingFrozenSubject is an
// evidence-consumer acceptance case for the explicit mutable-evidence path.
// It updates two current-round review envelopes' archival metadata in one
// worktree update, lets the Writer refresh their index hashes, and evaluates
// GATE-DOCUMENT-PASS. The frozen model remains byte-identical and its
// registered document hash is preserved while the valid disk evidence update
// is consumed.
func TestRecheckMutableReviewRefreshesWithoutChangingFrozenSubject(t *testing.T) {
	root := t.TempDir()
	runRecheckGit(t, root, "init", "-qb", "development")
	runRecheckGit(t, root, "config", "user.name", "recheck-audit")
	runRecheckGit(t, root, "config", "user.email", "recheck-audit@example.invalid")
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	writer := testWriter(statePath, journalPath)
	modelPath := filepath.Join(root, "docs", "design", "ARCH.md")
	if err := os.MkdirAll(filepath.Dir(modelPath), 0o755); err != nil {
		t.Fatal(err)
	}
	model := []byte("# reviewed architecture\n")
	if err := os.WriteFile(modelPath, model, 0o644); err != nil {
		t.Fatal(err)
	}
	subjectSHA := recheckSHA(model)
	runtimeID := "loop-recheck"
	reviewFile := func(id, responsibility string) []byte {
		return []byte(fmt.Sprintf(`{"schema_version":"1.0.0","evidence_id":%q,"kind":"document_review","runtime_id":%q,"baseline_generation":1,"review_round":1,"producer_agent_id":%q,"producer_responsibility":%q,"subject_refs":[{"path":"docs/design/ARCH.md","version":"v1","sha256":%q}],"conclusion":"pass","requested_event":"","created_at":"2026-09-20T00:00:00Z","invalidated_by":""}`+"\n", id, runtimeID, id+"-agent", responsibility, subjectSHA))
	}
	paths := []string{".claude/evidence/review-spec.json", ".claude/evidence/review-task.json"}
	responsibilities := []string{"DV-SPEC-CONSISTENCY", "DV-TASK-EXECUTABILITY"}
	staleReviewHash := "1111111111111111111111111111111111111111111111111111111111111111"
	entries := make([]any, 0, len(paths))
	for i, path := range paths {
		data := reviewFile("ev-recheck-"+fmt.Sprint(i), responsibilities[i])
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, map[string]any{
			"id": "ev-recheck-" + fmt.Sprint(i), "kind": "document_review", "path": path,
			// Keep the registered index stale for the before-refresh control. A
			// hash mismatch is a clean not_ready result; it does not turn the
			// expected pass conclusion into an evaluator conflict.
			"sha256": staleReviewHash, "status": "valid", "baseline_generation": 1, "review_round": 1,
			"produced_by": []any{"ev-recheck-" + fmt.Sprint(i) + "-agent"}, "invalidated_by": nil,
			"invalidation_rule": nil, "invalidation_reason": nil, "responsibility_id": responsibilities[i], "scope_refs": []any{},
		})
	}
	stateData, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatal(err)
	}
	state["runtime_id"] = runtimeID
	state["revision"] = 0
	state["lifecycle"] = map[string]any{"state": "document_verification", "phase": nil, "phase_revision": 0}
	state["baseline"] = map[string]any{"generation": 1, "captured_at": "2026-09-20T00:00:00Z"}
	state["review"] = map[string]any{"round": 1, "clean_round": nil}
	state["journal"] = map[string]any{"path": ".claude/loop-events.jsonl", "last_sequence": 0, "last_event_id": nil}
	state["documents"] = []any{map[string]any{
		"id": "ARCH-RECHECK", "kind": "design", "path": "docs/design/ARCH.md", "version": "v1", "sha256": subjectSHA,
		"status": "locked", "generation": 1,
	}}
	state["evidence"] = entries
	stateBytes, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, append(stateBytes, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	runRecheckGit(t, root, "add", "docs", "loop-state.json")
	runRecheckGit(t, root, "commit", "-qm", "committed review baseline")

	// The unchanged frozen model plus pass review conclusions must be rejected
	// before any refresh because both registered hashes are stale. This is the
	// positive control: the later satisfied result is attributable to the
	// mutable disk-evidence refresh, rather than to an already-open gate.
	initialView, err := fileview.New(root, "refs/heads/development", []fileview.Rule{
		{Path: ".", Source: "git_tree"},
		{Path: ".claude/evidence", Source: "disk"},
	})
	if err != nil {
		t.Fatal(err)
	}
	initialSnapshot, err := writer.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := transition.LoadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := qualitygate.NewRegistry(catalog)
	if err != nil {
		t.Fatal(err)
	}
	evaluator := qualitygate.NewEvaluator(registry)
	before, err := evaluator.Evaluate(nil, qualitygate.Input{
		Root: root, Snapshot: initialSnapshot, TransitionID: "TR-003", GateID: "GATE-DOCUMENT-PASS", Files: initialView,
	})
	if err != nil {
		t.Fatal(err)
	}
	if before.Status != qualitygate.StatusNotReady {
		t.Fatalf("baseline reviews with stale hashes unexpectedly opened GATE-DOCUMENT-PASS: %+v", before)
	}

	// A later worktree update edits only archival metadata in the already-
	// registered review envelopes. The model subject and review conclusions
	// stay byte-identical.
	for i, path := range paths {
		data := reviewFile("ev-recheck-"+fmt.Sprint(i), responsibilities[i])
		var envelope map[string]any
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatalf("decode edited review %s: %v", path, err)
		}
		envelope["created_at"] = "2026-09-20T01:00:00Z"
		encoded, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), append(encoded, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	view, err := fileview.New(root, "refs/heads/development", []fileview.Rule{
		{Path: ".", Source: "git_tree"},
		{Path: ".claude/evidence", Source: "disk"},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := writer.RefreshEvidenceFingerprints(root, map[string]bool{"document_review": true}, view.ReadFile, func(path string) bool {
		source, sourceErr := view.Source(path)
		return sourceErr == nil && source == "disk"
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("refresh result for declared disk review files: %+v", result)
	snapshot, err := writer.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	evaluation, err := evaluator.Evaluate(nil, qualitygate.Input{
		Root: root, Snapshot: snapshot, TransitionID: "TR-003", GateID: "GATE-DOCUMENT-PASS", Files: view,
	})
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.Status != qualitygate.StatusSatisfied {
		t.Fatalf("mutable review evidence should satisfy GATE-DOCUMENT-PASS after refresh: %+v", evaluation)
	}
	if len(evaluation.EvidenceRefs) != len(paths) {
		t.Fatalf("Gate did not consume both refreshed review envelopes: %+v", evaluation)
	}
	documents, _ := snapshot.State["documents"].([]any)
	for _, raw := range documents {
		doc, _ := raw.(map[string]any)
		if doc != nil && doc["path"] == "docs/design/ARCH.md" && doc["sha256"] != subjectSHA {
			t.Fatalf("mutable evidence refresh changed frozen model registration: got %v want %s", doc["sha256"], subjectSHA)
		}
	}
}

func recheckSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

func runRecheckGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Recheck",
		"GIT_AUTHOR_EMAIL=recheck@example.invalid",
		"GIT_COMMITTER_NAME=Recheck",
		"GIT_COMMITTER_EMAIL=recheck@example.invalid",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
