package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/cli"
)

// These are synthetic human choices in disposable repositories. Start at the
// normal S10 save/prepare boundary; the author never supplies machine metadata.
func TestS11NormalSaveSuppliesMachineMetadataForEveryDisposition(t *testing.T) {
	for _, tc := range []struct {
		disposition, transition, state, phase string
	}{
		{"approve", "TR-025", "release_authorized", ""},
		{"defer", "TR-026", "paused", ""},
		{"reject_defect", "TR-027", "bug_resolution", "investigation"},
		{"reject_acceptance", "TR-028", "acceptance", ""},
		{"reject_release_audit", "TR-029", "release_audit", ""},
		{"abort", "TR-030", "aborted", ""},
	} {
		t.Run(tc.disposition, func(t *testing.T) {
			root, acceptancePath := s10QuestionnaireHookRoot(t)
			releasePath := prepareS10QuestionnaireReleaseAudit(t, root, acceptancePath)
			fillS10QuestionnaireAnswer(t, releasePath, "release_audit", "approved")
			callS10QuestionnaireHook(t, root, s11SaveEvent(root, releasePath))
			paths, err := filepath.Glob(filepath.Join(root, ".claude", "decisions", "hd-s11-*.json"))
			if err != nil || len(paths) != 1 {
				t.Fatalf("original TR-017 did not prepare one current application: %v %v", paths, err)
			}
			path := paths[0]
			prepared := readS10QuestionnaireState(t, path)
			if prepared["disposition"] != "" || prepared["approved_by"] != "" {
				t.Fatal("prepare invented a human choice or attribution")
			}
			before := stageSnapshot(t, root)
			statePath, journalPath := filepath.Join(root, ".claude", "loop-state.json"), filepath.Join(root, ".claude", "loop-events.jsonl")
			if tc.disposition == "approve" {
				stateBytes, _ := os.ReadFile(statePath)
				journalBytes, _ := os.ReadFile(journalPath)
				s11WriteChoice(t, path, "", "A synthetic draft has no actual human decision.")
				feedback := callS10QuestionnaireHook(t, root, s11SaveEvent(root, path))
				afterState, _ := os.ReadFile(statePath)
				afterJournal, _ := os.ReadFile(journalPath)
				if !bytes.Equal(stateBytes, afterState) || !bytes.Equal(journalBytes, afterJournal) || !strings.Contains(feedback, "Draft only") {
					t.Fatalf("empty professional choice wrote a decision or lacked draft feedback: %s", feedback)
				}
			}
			reason := "Synthetic human choice: " + tc.disposition
			if tc.disposition == "approve" {
				reason = "" // A reason is optional; absence must not require author repair.
			}
			s11WriteChoice(t, path, tc.disposition, reason)
			feedback := callS10QuestionnaireHook(t, root, s11SaveEvent(root, path))
			after := stageSnapshot(t, root)
			cursor := after.State["lifecycle"].(map[string]any)
			if cursor["state"] != tc.state || (tc.phase != "" && cursor["phase"] != tc.phase) || after.Revision != before.Revision+1 || !strings.Contains(feedback, tc.transition) {
				t.Fatalf("normal save did not consume its one fixed exit: cursor=%v revision=%d/%d feedback=%s", cursor, after.Revision, before.Revision, feedback)
			}
			id := strings.TrimSuffix(filepath.Base(path), ".json")
			body, _ := os.ReadFile(path)
			digest := sha256.Sum256(body)
			answer := readS10QuestionnaireState(t, path)
			if answer["decision_id"] != id || answer["runtime_id"] != before.State["runtime_id"] || answer["approved_by"] != "Main" || answer["disposition"] != tc.disposition || answer["reason"] != reason || answer["scope"] != "runtime_release:"+before.State["runtime_id"].(string) {
				t.Fatalf("Writer did not supply the original decision metadata: %v", answer)
			}
			var recorded int
			for _, raw := range after.State["evidence"].([]any) {
				row := raw.(map[string]any)
				if row["kind"] != "human_decision" {
					continue
				}
				recorded++
				if row["id"] != id || row["consumed_by"] != tc.transition || row["sha256"] != hex.EncodeToString(digest[:]) || row["baseline_generation"] != before.State["baseline"].(map[string]any)["generation"] || row["review_round"] != before.State["review"].(map[string]any)["round"] {
					t.Fatalf("machine evidence was not bound to the actual saved choice: %v", row)
				}
				if produced, ok := row["produced_by"].([]any); !ok || len(produced) != 1 || produced[0] != "Main" {
					t.Fatalf("machine caller attribution = %v", row["produced_by"])
				}
				if _, err := time.Parse(time.RFC3339Nano, row["consumed_at"].(string)); err != nil {
					t.Fatalf("machine consumption time = %v: %v", row["consumed_at"], err)
				}
			}
			if recorded != 1 {
				t.Fatalf("one professional save recorded %d human decisions", recorded)
			}
			if tc.disposition == "reject_acceptance" && !strings.Contains(feedback, acceptancePath) || tc.disposition == "reject_release_audit" && !strings.Contains(feedback, releasePath) {
				t.Fatalf("original return omitted the exact current S10 material: %s", feedback)
			}
			if tc.disposition == "defer" {
				s11VerifyResumeCreatesNewEmptyApplication(t, root, path)
			}
		})
	}
}

func s11VerifyResumeCreatesNewEmptyApplication(t *testing.T, root, consumedPath string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	// This explicit synthetic resume is a separate human lifecycle decision.
	if code := cli.Run([]string{"runtime", "resume", "--root", root, "--approved-by", "synthetic-resume-human"}, nil, &stdout, &stderr); code != 0 {
		t.Fatalf("original resume failed: code=%d stdout=%s stderr=%s", code, &stdout, &stderr)
	}
	paths, err := filepath.Glob(filepath.Join(root, ".claude", "decisions", "hd-s11-*.json"))
	if err != nil || len(paths) != 2 {
		t.Fatalf("resume reused the consumed S11 responsibility: %v %v", paths, err)
	}
	var currentPath string
	for _, path := range paths {
		if path != consumedPath {
			currentPath = path
		}
	}
	current := readS10QuestionnaireState(t, currentPath)
	if current["disposition"] != "" || current["reason"] != "" || !strings.Contains(stderr.String(), currentPath) {
		t.Fatalf("resume omitted the new empty application: %v feedback=%s", current, &stderr)
	}
	statePath, journalPath := filepath.Join(root, ".claude", "loop-state.json"), filepath.Join(root, ".claude", "loop-events.jsonl")
	beforeState, _ := os.ReadFile(statePath)
	beforeJournal, _ := os.ReadFile(journalPath)
	callS10QuestionnaireHook(t, root, s11SaveEvent(root, consumedPath))
	afterState, _ := os.ReadFile(statePath)
	afterJournal, _ := os.ReadFile(journalPath)
	if !bytes.Equal(beforeState, afterState) || !bytes.Equal(beforeJournal, afterJournal) {
		t.Fatal("saving the old consumed application advanced the restored gateway")
	}
	feedback := callS10QuestionnaireHook(t, root, map[string]any{"hook_event_name": "PreToolUse", "cwd": root, "tool_name": "Read"})
	if !strings.Contains(feedback, currentPath) {
		t.Fatalf("restored gateway omitted accurate current material: %s", feedback)
	}
	s11WriteChoice(t, currentPath, "approve", "Synthetic new human approval after resume.")
	feedback = callS10QuestionnaireHook(t, root, s11SaveEvent(root, currentPath))
	if stageSnapshot(t, root).State["lifecycle"].(map[string]any)["state"] != "release_authorized" || !strings.Contains(feedback, "TR-025") {
		t.Fatalf("new professional-only application did not complete its original exit: %s", feedback)
	}
}

func s11SaveEvent(root, path string) map[string]any {
	return map[string]any{"hook_event_name": "PostToolUse", "cwd": root, "tool_name": "Edit", "tool_input": map[string]any{"file_path": path}}
}

func s11WriteChoice(t *testing.T, path, disposition, reason string) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"disposition": disposition, "reason": reason})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}
