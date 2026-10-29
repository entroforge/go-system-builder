package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/acceptance"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/transition"
)

// Registration and invalidation use the real Writer; only the initial project
// and professional observations are fixtures, never claimed product validation.
func TestS10OriginalRegistrationKeepsStatusGateAndTransitionConsistent(t *testing.T) {
	for _, scenario := range []string{"missing", "pass", "unfinished", "unreadable-latest", "invalidated-then-new", "review-required"} {
		t.Run(scenario, func(t *testing.T) {
			root, question := s10QuestionnaireHookRoot(t)
			statePath, journalPath := filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")
			var latestID, latestPath string
			register := func(token, outcome string, unfinished bool) {
				t.Helper()
				fillS10QuestionnaireAnswer(t, question, "acceptance", outcome)
				data, err := os.ReadFile(question)
				if err != nil {
					t.Fatal(err)
				}
				var report map[string]any
				if err := json.Unmarshal(data, &report); err != nil {
					t.Fatal(err)
				}
				if unfinished {
					report["coverage_inventory"].([]any)[0].(map[string]any)["disposition"] = "unknown"
				}
				data, err = json.Marshal(report)
				if err != nil {
					t.Fatal(err)
				}
				manifestPath := "s10/selection/" + token + "-manifest.json"
				writeS10QuestionnaireFile(t, root, manifestPath, data)
				latestID = "ev-selection-" + token
				latestPath = "s10/selection/" + token + ".json"
				envelope := map[string]any{"schema_version": "1.0.0", "evidence_id": latestID, "kind": "acceptance", "runtime_id": report["runtime_id"], "baseline_generation": report["baseline_generation"], "review_round": report["review_round"], "producer_agent_id": "Main", "producer_responsibility": "Acceptance", "subject_refs": []string{}, "conclusion": outcome, "audit_manifest_path": manifestPath, "audit_manifest_sha256": sha256Hex(data)}
				body, err := json.Marshal(envelope)
				if err != nil {
					t.Fatal(err)
				}
				writeS10QuestionnaireFile(t, root, latestPath, body)
				if _, err := runtime.RecordEvidence(root, statePath, journalPath, runtime.EvidenceRequest{ExpectedRevision: -1, ID: latestID, Kind: "acceptance", Path: latestPath, ProducedBy: []string{"Main"}, ResponsibilityID: "Acceptance", Validator: semantic.RuntimeCandidateValidator{}}); err != nil {
					t.Fatalf("original registration %s: %v", token, err)
				}
			}
			expectReady := scenario == "pass" || scenario == "invalidated-then-new"
			switch scenario {
			case "pass":
				register("current", "pass", false)
			case "unfinished":
				register("older", "pass", false)
				register("current", "pass", true)
			case "review-required":
				register("older", "pass", false)
				register("current", "review_required", false)
			case "unreadable-latest":
				register("older", "pass", false)
				register("current", "pass", false)
				if err := os.Remove(filepath.Join(root, latestPath)); err != nil {
					t.Fatal(err)
				}
			case "invalidated-then-new":
				register("older", "pass", false)
				oldID, oldPath := latestID, latestPath
				_, err := runtime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).UpdateCurrent(runtime.Mutation{EventID: "evt-selection-invalidate", TransitionID: "EVIDENCE-INVALIDATED", Event: "evidence_invalidated", Actor: "Main", IdempotencyKey: "selection:invalidate", Apply: func(state map[string]any) error {
					for _, raw := range state["evidence"].([]any) {
						row := raw.(map[string]any)
						if row["id"] == oldID {
							row["status"] = "invalid"
							row["invalidated_by"] = "evt-selection-invalidate"
							row["invalidation_rule"] = "source_changed"
							row["invalidation_reason"] = "fixture source superseded"
						}
					}
					return nil
				}})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(root, oldPath)); err != nil {
					t.Fatal(err)
				}
				register("current", "pass", false)
			}
			snap, err := runtime.NewStore(statePath, journalPath).Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(statePath)
			var out, diag bytes.Buffer
			if code := cli.Run([]string{"s10", "status", "--root", root}, strings.NewReader(""), &out, &diag); code != 0 {
				t.Fatalf("status: %s", &diag)
			}
			var rawStatus map[string]any
			if err := json.Unmarshal(out.Bytes(), &rawStatus); err != nil {
				t.Fatal(err)
			}
			artifact := rawStatus["acceptance"].(map[string]any)
			if (artifact["state"] == "ready") != expectReady {
				t.Fatalf("status chose the wrong report: %s", &out)
			}
			catalog, err := transition.LoadCatalog(root)
			if err != nil {
				t.Fatal(err)
			}
			registry, err := qualitygate.NewRegistry(catalog)
			if err != nil {
				t.Fatal(err)
			}
			gate, err := qualitygate.NewEvaluator(registry).Evaluate(context.Background(), qualitygate.Input{Root: root, Snapshot: snap, GateID: "GATE-ACCEPTANCE-COMPLETE", TransitionID: "TR-015"})
			if err != nil {
				t.Fatal(err)
			}
			if (gate.Status == qualitygate.StatusSatisfied) != expectReady {
				t.Fatalf("gate disagrees with status: %+v", gate)
			}
			if err := acceptance.ValidateCurrentS10Evidence(root, snap.State, "acceptance"); (err == nil) != expectReady {
				t.Fatalf("original completion guard disagrees: %v", err)
			}
			var cleanID string
			for _, raw := range snap.State["evidence"].([]any) {
				row := raw.(map[string]any)
				if row["kind"] == "clean_round" {
					cleanID = row["id"].(string)
				}
			}
			if latestID == "" {
				latestID = "missing-report"
			}
			_, err = transition.Apply(root, statePath, journalPath, transition.Request{TransitionID: "TR-015", ExpectedRevision: snap.Revision, Actor: "orchestrator", Evidence: map[string]string{"acceptance_record": latestID, "clean_round_record": cleanID}})
			if (err == nil) != expectReady {
				t.Fatalf("actual TR-015 disagrees: %v", err)
			}
			if !expectReady {
				after, _ := os.ReadFile(statePath)
				if !bytes.Equal(before, after) {
					t.Fatal("rejected selection mutated Runtime")
				}
			}
		})
	}
}
