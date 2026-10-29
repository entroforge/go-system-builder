package review

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func packetModeVerificationState() map[string]any {
	state := baseVerificationState()
	state["schema_version"] = "1.2.0"
	configuration, _ := state["configuration"].(map[string]any)
	configuration["optimization"] = map[string]any{"packet": true}
	return state
}

func packetModeFixture(t *testing.T) (string, string, string, loopruntime.Snapshot, *Plan) {
	t.Helper()
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, packetModeVerificationState())
	fixtureEvidenceRoot = root
	t.Cleanup(func() { fixtureEvidenceRoot = "" })
	snapshot := registerFixturePlan(t, root, statePath, journalPath)
	snapshot = markDispatched(t, root, statePath, journalPath, snapshot, "assignment-dv-1", "agent-dv-1")
	snapshot = markDispatched(t, root, statePath, journalPath, snapshot, "assignment-qa-1", "agent-qa-1")
	plan, _, err := LoadPlan(root, snapshot.State)
	if err != nil {
		t.Fatal(err)
	}
	seedResultMethodSources(t, root, snapshot.State, plan)
	writeMethodState(t, statePath, snapshot.State)
	reviewGitBaseline(t, root)
	return root, statePath, journalPath, snapshot, plan
}

func TestTypedResultReceiptsFollowPacketRuntimeVersions(t *testing.T) {
	for _, test := range []struct {
		version string
		want    bool
	}{
		{version: "1.1.0", want: false},
		{version: "1.2.0", want: true},
		{version: "1.3.0", want: true},
	} {
		state := map[string]any{
			"schema_version": test.version,
			"configuration": map[string]any{
				"optimization": map[string]any{"packet": true},
			},
		}
		if got := typedResultReceiptsEnabled(state); got != test.want {
			t.Errorf("typedResultReceiptsEnabled(%s) = %v, want %v", test.version, got, test.want)
		}
	}
}

func TestTypedResultReceiptPassReplayIgnoresCaptureAndStaleRevision(t *testing.T) {
	root, statePath, journalPath, snapshot, plan := packetModeFixture(t)
	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-1", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	accepted, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err != nil {
		t.Fatalf("first typed submit: %v", err)
	}
	row := accepted.State["review"].(map[string]any)["assignments"].(map[string]any)["assignment-qa-1"].(map[string]any)
	if row["status"] != "consumed" || row["result_receipt"] == nil {
		t.Fatalf("typed Assignment receipt missing: %#v", row)
	}
	var receipt ResultReceipt
	encoded, _ := json.Marshal(row["result_receipt"])
	if err := json.Unmarshal(encoded, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.SubmissionKey != "review-result-qa-1" || receipt.ResultRef != row["result_ref"] || receipt.AcceptedAfterRevision != accepted.Revision || receipt.AcceptedSequence != intField(accepted.State["journal"].(map[string]any)["last_sequence"]) {
		t.Fatalf("receipt does not bind accepted tuple/cursor: %+v", receipt)
	}
	resultArtifact, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(receipt.ResultRef)))
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(resultArtifact, &envelope); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"submitted_answer", "answer_sha256", "effective_answer", "effective_answer_sha256"} {
		if len(envelope[field]) == 0 {
			t.Fatalf("typed envelope missing %s", field)
		}
	}
	dvPath := writeResultFile(t, root, plan, "assignment-dv-1", "review-result-dv-final", "agent-dv-1", "pass",
		map[string]string{"claim-dv-1": "pass"}, nil)
	accepted, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: accepted.Revision, AssignmentID: "assignment-dv-1", ResultPath: dvPath,
	})
	if err != nil {
		t.Fatalf("final typed pass: %v", err)
	}
	if PlanPointerFromState(accepted.State).Status != "clean" || accepted.State["review"].(map[string]any)["clean_round"] != 1 {
		t.Fatalf("fixture did not create its original CleanRound: %+v", PlanPointerFromState(accepted.State))
	}
	cleanEvidenceCount := 0
	for _, raw := range accepted.State["evidence"].([]any) {
		if raw.(map[string]any)["kind"] == "clean_round" {
			cleanEvidenceCount++
		}
	}
	if cleanEvidenceCount != 1 {
		t.Fatalf("CleanRound evidence count = %d, want exactly 1", cleanEvidenceCount)
	}
	beforeState, _ := os.ReadFile(statePath)
	beforeJournal, _ := os.ReadFile(journalPath)
	_, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
		CaptureDir: filepath.Join(root, "capture-dir-that-no-longer-exists"),
	})
	if err != nil {
		t.Fatalf("exact replay with stale caller and missing capture buffer: %v", err)
	}
	afterState, _ := os.ReadFile(statePath)
	afterJournal, _ := os.ReadFile(journalPath)
	if string(beforeState) != string(afterState) || string(beforeJournal) != string(afterJournal) {
		t.Fatal("exact replay wrote Runtime state or journal")
	}

	// The same stable key cannot be rebound to another professional answer.
	var changed map[string]any
	input, _ := os.ReadFile(qaPath)
	if err := json.Unmarshal(input, &changed); err != nil {
		t.Fatal(err)
	}
	claims := changed["claim_results"].([]any)
	claims[0].(map[string]any)["observed"] = "changed after acceptance"
	changedBytes, _ := json.Marshal(changed)
	if err := os.WriteFile(qaPath, changedBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: -1, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err == nil || !strings.Contains(err.Error(), "conflicts with the originally accepted") {
		t.Fatalf("same key with changed answer must conflict, got %v", err)
	}
	changed["result_id"] = "review-result-qa-replacement"
	changedBytes, _ = json.Marshal(changed)
	if err := os.WriteFile(qaPath, changedBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: -1, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err == nil || !strings.Contains(err.Error(), "new result_id cannot replace") {
		t.Fatalf("new key must not overwrite consumed Assignment, got %v", err)
	}
}

func TestTypedResultReceiptRejectsTamperedDurableClosure(t *testing.T) {
	for _, tamper := range []string{"receipt", "envelope", "journal"} {
		t.Run(tamper, func(t *testing.T) {
			root, statePath, journalPath, snapshot, plan := packetModeFixture(t)
			resultPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-tamper", "agent-qa-1", "pass",
				map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
			accepted, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
				ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: resultPath,
			})
			if err != nil {
				t.Fatalf("accept result: %v", err)
			}
			row := accepted.State["review"].(map[string]any)["assignments"].(map[string]any)["assignment-qa-1"].(map[string]any)
			receiptJSON, _ := json.Marshal(row["result_receipt"])
			var receipt ResultReceipt
			if err := json.Unmarshal(receiptJSON, &receipt); err != nil {
				t.Fatal(err)
			}
			switch tamper {
			case "receipt":
				stateData, _ := os.ReadFile(statePath)
				var state map[string]any
				if err := json.Unmarshal(stateData, &state); err != nil {
					t.Fatal(err)
				}
				assignmentRow := state["review"].(map[string]any)["assignments"].(map[string]any)["assignment-qa-1"].(map[string]any)
				assignmentRow["result_receipt"].(map[string]any)["answer_sha256"] = strings.Repeat("0", 64)
				writeJSONFile(t, statePath, state)
			case "envelope":
				artifactPath := filepath.Join(root, filepath.FromSlash(receipt.ResultRef))
				data, _ := os.ReadFile(artifactPath)
				var envelope map[string]any
				if err := json.Unmarshal(data, &envelope); err != nil {
					t.Fatal(err)
				}
				envelope["verdict"] = "tampered"
				writeJSONFile(t, artifactPath, envelope)
			case "journal":
				data, _ := os.ReadFile(journalPath)
				var lines []string
				for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
					var event map[string]any
					if err := json.Unmarshal([]byte(line), &event); err != nil {
						t.Fatal(err)
					}
					if event["event_id"] == receipt.EventID {
						event["idempotency_key"] = "runtime:tampered"
					}
					encoded, _ := json.Marshal(event)
					lines = append(lines, string(encoded))
				}
				if err := os.WriteFile(journalPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			_, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
				ExpectedRevision: -1, AssignmentID: "assignment-qa-1", ResultPath: resultPath,
				CaptureDir: filepath.Join(root, "missing-captures"),
			})
			if err == nil {
				t.Fatalf("replay accepted %s tampering", tamper)
			}
		})
	}
}

func TestTypedResultReceiptRejectsIncomingMachineTupleEditsWithoutWrites(t *testing.T) {
	root, statePath, journalPath, snapshot, plan := packetModeFixture(t)
	resultPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-tuple", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	accepted, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: resultPath,
	})
	if err != nil {
		t.Fatalf("accept result: %v", err)
	}
	if accepted.Revision <= snapshot.Revision {
		t.Fatalf("accepted typed Result did not advance Runtime revision: %d -> %d", snapshot.Revision, accepted.Revision)
	}
	authored, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	beforeJournal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name          string
		field         string
		value         any
		requestAssign string
	}{
		{name: "assignment", field: "assignment_id", value: "assignment-other", requestAssign: "assignment-qa-1"},
		{name: "assignment revision", field: "assignment_revision", value: 22, requestAssign: "assignment-qa-1"},
		{name: "plan id", field: "review_plan_id", value: "review-plan-other", requestAssign: "assignment-qa-1"},
		{name: "round", field: "review_round", value: 22, requestAssign: "assignment-qa-1"},
		{name: "baseline generation", field: "baseline_generation", value: 22, requestAssign: "assignment-qa-1"},
		{name: "owner", field: "producer_agent_id", value: "agent-other", requestAssign: "assignment-qa-1"},
		{name: "subject digest", field: "subject_digest", value: strings.Repeat("a", 64), requestAssign: "assignment-qa-1"},
		{name: "verification artifact digest", field: "verification_artifact_digest", value: strings.Repeat("b", 64), requestAssign: "assignment-qa-1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var answer map[string]any
			if err := json.Unmarshal(authored, &answer); err != nil {
				t.Fatal(err)
			}
			answer[tc.field] = tc.value
			changed, err := json.Marshal(answer)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(resultPath, changed, 0o644); err != nil {
				t.Fatal(err)
			}
			_, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
				ExpectedRevision: -1, AssignmentID: tc.requestAssign, ResultPath: resultPath,
				CaptureDir: filepath.Join(root, "capture-buffer-must-not-be-read"),
			})
			if err == nil {
				t.Fatalf("replay accepted edited machine tuple field %s", tc.field)
			}
			afterState, readErr := os.ReadFile(statePath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			afterJournal, readErr := os.ReadFile(journalPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !reflect.DeepEqual(beforeState, afterState) || !reflect.DeepEqual(beforeJournal, afterJournal) {
				t.Fatalf("rejected machine tuple edit %s wrote Runtime state or journal", tc.field)
			}
			if err := os.WriteFile(resultPath, authored, 0o644); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTypedResultReceiptRejectsEvidenceInvalidationMarkers(t *testing.T) {
	root, statePath, journalPath, snapshot, plan := packetModeFixture(t)
	resultPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-invalidated", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	accepted, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: resultPath,
	})
	if err != nil {
		t.Fatalf("accept result: %v", err)
	}
	row := accepted.State["review"].(map[string]any)["assignments"].(map[string]any)["assignment-qa-1"].(map[string]any)
	rawReceipt, _ := json.Marshal(row["result_receipt"])
	var receipt ResultReceipt
	if err := json.Unmarshal(rawReceipt, &receipt); err != nil {
		t.Fatal(err)
	}
	var incoming Result
	answer, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(answer, &incoming); err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"invalidated_by", "invalidation_rule", "invalidation_reason"} {
		t.Run(marker, func(t *testing.T) {
			stateBytes, _ := json.Marshal(accepted.State)
			var state map[string]any
			if err := json.Unmarshal(stateBytes, &state); err != nil {
				t.Fatal(err)
			}
			evidence, err := uniqueEvidenceByID(state, receipt.SubmissionKey)
			if err != nil {
				t.Fatal(err)
			}
			evidence[marker] = "forged-invalidation"
			if err := verifyResultReceiptClosure(root, state, state["review"].(map[string]any)["assignments"].(map[string]any)["assignment-qa-1"].(map[string]any), receipt, &incoming); err == nil {
				t.Fatalf("accepted valid evidence row with non-null %s", marker)
			}
		})
	}
}

func TestTypedResultReceiptChecksEffectiveAnswerMachineTuple(t *testing.T) {
	root, statePath, journalPath, snapshot, plan := packetModeFixture(t)
	resultPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-effective-tuple", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	accepted, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: resultPath,
	})
	if err != nil {
		t.Fatalf("accept result: %v", err)
	}
	stateBytes, _ := json.Marshal(accepted.State)
	var state map[string]any
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	row := state["review"].(map[string]any)["assignments"].(map[string]any)["assignment-qa-1"].(map[string]any)
	var receipt ResultReceipt
	receiptBytes, _ := json.Marshal(row["result_receipt"])
	if err := json.Unmarshal(receiptBytes, &receipt); err != nil {
		t.Fatal(err)
	}
	artifactPath := filepath.Join(root, filepath.FromSlash(receipt.ResultRef))
	artifact, err := workpacket.ReadFile(root, receipt.ResultRef, resultReceiptReadLimit)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(artifact, &envelope); err != nil {
		t.Fatal(err)
	}
	var effective map[string]any
	if err := json.Unmarshal(envelope["effective_answer"], &effective); err != nil {
		t.Fatal(err)
	}
	effective["baseline_generation"] = 77
	envelope["effective_answer"], err = json.Marshal(effective)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err = json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifactPath, artifact, 0o644); err != nil {
		t.Fatal(err)
	}
	receipt.ResultSHA256 = sha256Of(artifact)
	evidence, err := uniqueEvidenceByID(state, receipt.SubmissionKey)
	if err != nil {
		t.Fatal(err)
	}
	evidence["sha256"] = receipt.ResultSHA256
	var incoming Result
	authored, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(authored, &incoming); err != nil {
		t.Fatal(err)
	}
	if err := verifyResultReceiptClosure(root, state, row, receipt, &incoming); err == nil || !strings.Contains(err.Error(), "tuple") {
		t.Fatalf("replay accepted effective answer machine tuple tampering, err=%v", err)
	}
}

func TestReviewResultCleanupRetainsArtifactForPendingOrReferencedRuntime(t *testing.T) {
	for _, mode := range []struct {
		name  string
		state func() map[string]any
	}{
		{name: "legacy-1.1", state: baseVerificationState},
		{name: "packet-1.2", state: packetModeVerificationState},
	} {
		t.Run(mode.name, func(t *testing.T) {
			root := t.TempDir()
			statePath, journalPath := writeState(t, root, mode.state())
			stateBytes, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			var state map[string]any
			if err := json.Unmarshal(stateBytes, &state); err != nil {
				t.Fatal(err)
			}
			const relative = ".claude/evidence/runtime-test/g0/reviews/agent-test/result.json"
			artifactBytes := []byte("durable result bytes\n")
			artifactPath := filepath.Join(root, filepath.FromSlash(relative))
			if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(artifactPath, artifactBytes, 0o644); err != nil {
				t.Fatal(err)
			}
			store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
			marker := statePath + ".commit-pending.json"
			if err := os.WriteFile(marker, []byte(`{"pending":true}`), 0o600); err != nil {
				t.Fatal(err)
			}
			removed, err := removeStagedArtifactIfUnreferenced(store, state, intField(state["revision"]), relative, sha256Of(artifactBytes))
			if err != nil || removed {
				t.Fatalf("cleanup with a durable pending marker = (%t,%v), want (false,nil)", removed, err)
			}
			if _, err := os.Stat(artifactPath); err != nil {
				t.Fatalf("pending commit artifact was removed: %v", err)
			}
			if err := os.Remove(marker); err != nil {
				t.Fatal(err)
			}
			state["evidence"] = []any{map[string]any{"path": relative}}
			removed, err = removeStagedArtifactIfUnreferenced(store, state, intField(state["revision"]), relative, sha256Of(artifactBytes))
			if err != nil || removed {
				t.Fatalf("cleanup of a state-referenced artifact = (%t,%v), want (false,nil)", removed, err)
			}
			if _, err := os.Stat(artifactPath); err != nil {
				t.Fatalf("state-referenced artifact was removed: %v", err)
			}
		})
	}
}

func TestTypedResultReceiptRecoversSealedCommitAndReplaysOnce(t *testing.T) {
	for _, withFinding := range []bool{false, true} {
		name := "clean_round"
		if withFinding {
			name = "finding_seal"
		}
		t.Run(name, func(t *testing.T) {
			root, statePath, journalPath, snapshot, plan := packetModeFixture(t)
			qaID := "review-result-qa-recovery"
			qaVerdict := "pass"
			qaConclusions := map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}
			var findings []Finding
			var capturePath string
			if withFinding {
				finding := codeInspectionFinding("finding-qa-recovery", "claim-qa-1")
				findings = []Finding{finding}
				qaVerdict = "finding"
				qaConclusions["claim-qa-1"] = "fail"
				capturePath = CaptureFile(root, "loop-REQ-TEST", 1, "assignment-qa-1")
				if err := os.MkdirAll(filepath.Dir(capturePath), 0o755); err != nil {
					t.Fatal(err)
				}
				captureRef := fixtureEvidenceRef(t, root, "typed-recovery-capture.txt")
				if err := os.WriteFile(capturePath, []byte(`{"sequence":1,"claim_id":"claim-qa-1","action":"open screen","observed":"save failed","evidence_refs":["`+captureRef+`"]}`+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			qaPath := writeResultFile(t, root, plan, "assignment-qa-1", qaID, "agent-qa-1", qaVerdict, qaConclusions, findings)
			qaAccepted, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
				ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath, CaptureDir: capturePath,
			})
			if err != nil {
				t.Fatalf("submit QA result: %v", err)
			}
			beforeState, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			beforeJournal, err := os.ReadFile(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			dvPath := writeResultFile(t, root, plan, "assignment-dv-1", "review-result-dv-recovery", "agent-dv-1", "pass",
				map[string]string{"claim-dv-1": "pass"}, nil)
			final, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
				ExpectedRevision: qaAccepted.Revision, AssignmentID: "assignment-dv-1", ResultPath: dvPath,
			})
			if err != nil {
				t.Fatalf("submit final DV result: %v", err)
			}
			afterState, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			afterJournal, err := os.ReadFile(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			stateDocument, err := runtimeStateDocumentForReview(final.State)
			if err != nil {
				t.Fatal(err)
			}
			if string(stateDocument) != string(afterState) {
				t.Fatal("accepted Snapshot state serializes differently from the durable Runtime state")
			}
			stateJSON, err := json.Marshal(final.State)
			if err != nil {
				t.Fatal(err)
			}
			var normalizedState map[string]any
			if err := json.Unmarshal(stateJSON, &normalizedState); err != nil {
				t.Fatal(err)
			}
			normalizedDocument, err := runtimeStateDocumentForReview(normalizedState)
			if err != nil {
				t.Fatal(err)
			}
			if string(normalizedDocument) != string(stateDocument) {
				t.Fatal("Runtime state hash changes after JSON decode/encode normalization")
			}

			journalLines := strings.Split(strings.TrimSpace(string(afterJournal)), "\n")
			if len(journalLines) == 0 || journalLines[len(journalLines)-1] == "" {
				t.Fatal("final Runtime journal event is missing")
			}
			var event map[string]any
			if err := json.Unmarshal([]byte(journalLines[len(journalLines)-1]), &event); err != nil {
				t.Fatal(err)
			}
			eventBytes, err := json.Marshal(event)
			if err != nil {
				t.Fatal(err)
			}
			eventLine := append(eventBytes, '\n')
			marker := map[string]any{
				"schema_version":         "1.0.0",
				"previous_state_sha256":  sha256Of(beforeState),
				"previous_revision":      qaAccepted.Revision,
				"state_sha256":           sha256Of(stateDocument),
				"journal_event_sha256":   sha256Of(eventLine),
				"request_id":             event["request_id"],
				"idempotency_key":        event["idempotency_key"],
				"retain_last_transition": false,
				"state":                  final.State,
				"journal_event":          event,
			}
			if err := os.WriteFile(statePath, beforeState, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(journalPath, beforeJournal, 0o644); err != nil {
				t.Fatal(err)
			}
			writeJSONFile(t, statePath+".commit-pending.json", marker)

			replayed, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
				ExpectedRevision: qaAccepted.Revision, AssignmentID: "assignment-dv-1", ResultPath: dvPath,
				CaptureDir: filepath.Join(root, "capture-buffer-not-needed-after-commit"),
			})
			if err != nil {
				t.Fatalf("recover pending accepted commit and replay: %v", err)
			}
			if replayed.Revision != final.Revision {
				t.Fatalf("replayed revision = %d, want recovered commit revision %d", replayed.Revision, final.Revision)
			}
			recoveredState, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			recoveredJournal, err := os.ReadFile(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(recoveredState) != string(afterState) || string(recoveredJournal) != string(afterJournal) {
				t.Fatal("pending recovery/replay changed the accepted state or journal a second time")
			}
			if _, err := os.Stat(statePath + ".commit-pending.json"); !os.IsNotExist(err) {
				t.Fatalf("recovered pending marker remains: %v", err)
			}
			if _, err := uniqueEvidenceByID(replayed.State, "review-result-dv-recovery"); err != nil {
				t.Fatalf("recovered Result evidence missing or duplicated: %v", err)
			}
			if countJournalEvent(t, recoveredJournal, stringField(final.State["last_transition"].(map[string]any)["event_id"])) != 1 {
				t.Fatal("recovered Result journal event was duplicated")
			}
			row := replayed.State["review"].(map[string]any)["assignments"].(map[string]any)["assignment-dv-1"].(map[string]any)
			if row["result_receipt"] == nil {
				t.Fatal("recovered consumed Assignment has no typed receipt")
			}
			cleanRounds := 0
			for _, raw := range replayed.State["evidence"].([]any) {
				if raw.(map[string]any)["kind"] == "clean_round" {
					cleanRounds++
				}
			}
			if withFinding {
				findings := replayed.State["entities"].(map[string]any)["findings"].([]any)
				if len(findings) != 1 || replayed.State["review"].(map[string]any)["observation_batch"] == nil || cleanRounds != 0 {
					t.Fatalf("finding recovery duplicated or altered closing projections: findings=%d batch=%v cleanRounds=%d", len(findings), replayed.State["review"].(map[string]any)["observation_batch"], cleanRounds)
				}
			} else if cleanRounds != 1 || intField(replayed.State["review"].(map[string]any)["clean_round"]) != 1 {
				t.Fatalf("clean-round recovery duplicated or lost its projection: evidence=%d projection=%v", cleanRounds, replayed.State["review"].(map[string]any)["clean_round"])
			}
		})
	}
}

func TestTypedResultReceiptConcurrentSameKeyHasOneCommitAndReplay(t *testing.T) {
	root, statePath, journalPath, snapshot, plan := packetModeFixture(t)
	resultPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-concurrent", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	start := make(chan struct{})
	type submitOutcome struct {
		snapshot loopruntime.Snapshot
		err      error
	}
	outcomes := make([]submitOutcome, 2)
	var workers sync.WaitGroup
	for index := range outcomes {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			outcomes[index].snapshot, outcomes[index].err = SubmitResult(root, statePath, journalPath, SubmitRequest{
				ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: resultPath,
			})
		}(index)
	}
	close(start)
	workers.Wait()
	accepted := 0
	for _, outcome := range outcomes {
		if outcome.err == nil {
			accepted++
		}
	}
	if accepted == 0 {
		t.Fatalf("both concurrent same-key submits failed: %v; %v", outcomes[0].err, outcomes[1].err)
	}
	for index, outcome := range outcomes {
		if outcome.err == nil {
			continue
		}
		replayed, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
			ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: resultPath,
			CaptureDir: filepath.Join(root, "capture-buffer-must-not-be-read"),
		})
		if err != nil {
			t.Fatalf("retry concurrent submit %d after initial error %v: %v", index, outcome.err, err)
		}
		outcomes[index].snapshot = replayed
		outcomes[index].err = nil
	}
	var final loopruntime.Snapshot
	for _, outcome := range outcomes {
		if outcome.snapshot.Revision > final.Revision {
			final = outcome.snapshot
		}
	}
	row := final.State["review"].(map[string]any)["assignments"].(map[string]any)["assignment-qa-1"].(map[string]any)
	var receipt ResultReceipt
	receiptBytes, _ := json.Marshal(row["result_receipt"])
	if err := json.Unmarshal(receiptBytes, &receipt); err != nil {
		t.Fatal(err)
	}
	if _, err := uniqueEvidenceByID(final.State, "review-result-qa-concurrent"); err != nil {
		t.Fatalf("concurrent accepted Result evidence is missing or duplicated: %v", err)
	}
	journal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if countJournalEvent(t, journal, receipt.EventID) != 1 {
		t.Fatal("concurrent same-key submissions committed more than one Runtime event")
	}
}

func runtimeStateDocumentForReview(state map[string]any) ([]byte, error) {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func countJournalEvent(t *testing.T, data []byte, eventID string) int {
	t.Helper()
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event["event_id"] == eventID {
			count++
		}
	}
	return count
}

func TestLegacyResultSubmissionDoesNotAddTypedReceipt(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	fixtureEvidenceRoot = root
	t.Cleanup(func() { fixtureEvidenceRoot = "" })
	snapshot := registerFixturePlan(t, root, statePath, journalPath)
	snapshot = markDispatched(t, root, statePath, journalPath, snapshot, "assignment-qa-1", "agent-qa-1")
	plan, _, err := LoadPlan(root, snapshot.State)
	if err != nil {
		t.Fatal(err)
	}
	resultPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-legacy", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	accepted, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: resultPath,
	})
	if err != nil {
		t.Fatalf("legacy submit: %v", err)
	}
	row := accepted.State["review"].(map[string]any)["assignments"].(map[string]any)["assignment-qa-1"].(map[string]any)
	if _, exists := row["result_receipt"]; exists {
		t.Fatalf("1.1 Assignment shape gained typed receipt: %#v", row)
	}
	evidence := accepted.State["evidence"].([]any)
	for _, raw := range evidence {
		entry := raw.(map[string]any)
		if entry["id"] != "review-result-legacy" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(entry["path"].(string))))
		if err != nil {
			t.Fatal(err)
		}
		var envelope map[string]any
		if err := json.Unmarshal(data, &envelope); err != nil {
			t.Fatal(err)
		}
		if _, exists := envelope["submitted_answer"]; exists {
			t.Fatal("1.1 Result envelope gained typed replay fields")
		}
		return
	}
	t.Fatal("legacy result evidence missing")
}

func TestTypedResultReceiptRevisionClearsChangedAssignmentReceipt(t *testing.T) {
	root, statePath, journalPath, snapshot, _ := packetModeFixture(t)
	dvPath := writeResultFile(t, root, mustLoadPlan(t, root, snapshot.State), "assignment-dv-1", "review-result-dv-revise", "agent-dv-1", "pass",
		map[string]string{"claim-dv-1": "pass"}, nil)
	accepted, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-dv-1", ResultPath: dvPath,
	})
	if err != nil {
		t.Fatalf("submit original DV result: %v", err)
	}
	revisionPath := receiptRevisionPlan(t, root, accepted.State, "review-result-dv-revise", true)
	revised, err := RevisePlan(root, statePath, journalPath, ReviseRequest{
		ExpectedRevision: accepted.Revision, PlanPath: revisionPath,
		SourceRef: "review-result-dv-revise", AffectedSurface: "internal/example",
	})
	if err != nil {
		t.Fatalf("revise plan: %v", err)
	}
	row := revised.State["review"].(map[string]any)["assignments"].(map[string]any)["assignment-dv-1"].(map[string]any)
	if row["status"] != "planned" || row["agent_id"] != nil || row["result_ref"] != nil || row["result_receipt"] != nil {
		t.Fatalf("assignment metadata change retained its consumed Result projection: %#v", row)
	}
	if got := Dispositions(revised.State)["claim-dv-1"].Disposition; got != "planned" {
		t.Fatalf("metadata-changed Assignment claim disposition = %s, want planned", got)
	}
	for _, raw := range revised.State["evidence"].([]any) {
		entry := raw.(map[string]any)
		if entry["id"] == "review-result-dv-revise" && entry["status"] != "invalid" {
			t.Fatalf("metadata-changed Assignment result evidence status = %v, want invalid", entry["status"])
		}
	}
}

func TestTypedResultReceiptUntouchedAssignmentReplaysAcrossPlanRevision(t *testing.T) {
	root, statePath, journalPath, snapshot, plan := packetModeFixture(t)
	dvPath := writeResultFile(t, root, plan, "assignment-dv-1", "review-result-dv-preserved", "agent-dv-1", "pass",
		map[string]string{"claim-dv-1": "pass"}, nil)
	accepted, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-dv-1", ResultPath: dvPath,
	})
	if err != nil {
		t.Fatalf("submit original DV result: %v", err)
	}
	revisionPath := receiptRevisionPlan(t, root, accepted.State, "review-result-dv-preserved", false)
	revised, err := RevisePlan(root, statePath, journalPath, ReviseRequest{
		ExpectedRevision: accepted.Revision, PlanPath: revisionPath,
		SourceRef: "review-result-dv-preserved", AffectedSurface: "internal/example",
	})
	if err != nil {
		t.Fatalf("revise unrelated QA claim: %v", err)
	}
	row := revised.State["review"].(map[string]any)["assignments"].(map[string]any)["assignment-dv-1"].(map[string]any)
	if row["status"] != "consumed" || row["result_receipt"] == nil || row["result_ref"] == nil {
		t.Fatalf("untouched Assignment lost its original receipt: %#v", row)
	}
	if _, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-dv-1", ResultPath: dvPath,
		CaptureDir: filepath.Join(root, "missing-capture-buffer"),
	}); err != nil {
		t.Fatalf("untouched receipt must replay after a Plan revision: %v", err)
	}
}

func TestTypedResultReceiptMovedClaimToNewAssignmentRequiresFreshAnswer(t *testing.T) {
	MovedClaimRevisionFixtureForTest(t)
}

// Test-only original Writer chain for the external CleanRound consumer test.
// Reuse its source/receipt/ownership assertions without a production fixture API.
func MovedClaimRevisionFixtureForTest(t *testing.T) (string, string, string, loopruntime.Snapshot) {
	t.Helper()
	root, statePath, journalPath, snapshot, initialPlan := packetModeFixture(t)
	oldPath := writeResultFile(t, root, initialPlan, "assignment-dv-1", "review-result-moved-old", "agent-dv-1", "pass",
		map[string]string{"claim-dv-1": "pass"}, nil)
	accepted, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-dv-1", ResultPath: oldPath,
	})
	if err != nil {
		t.Fatalf("submit original DV result: %v", err)
	}

	// The QA edit makes this a source-bound controlled revision. The DV Claim
	// itself is byte-for-byte unchanged; only its owner moves to a new
	// AssignmentID, which must still invalidate the old accepted answer.
	revisionPath := receiptRevisionPlan(t, root, accepted.State, "review-result-moved-old", false)
	planBytes, err := os.ReadFile(revisionPath)
	if err != nil {
		t.Fatal(err)
	}
	var planBody map[string]any
	if err := json.Unmarshal(planBytes, &planBody); err != nil {
		t.Fatal(err)
	}
	for _, raw := range planBody["assignments"].([]any) {
		assignment := raw.(map[string]any)
		if assignment["assignment_id"] == "assignment-dv-1" {
			assignment["assignment_id"] = "assignment-dv-2"
		}
	}
	writeJSONFile(t, revisionPath, planBody)

	revised, err := RevisePlan(root, statePath, journalPath, ReviseRequest{
		ExpectedRevision: accepted.Revision, PlanPath: revisionPath,
		SourceRef: "review-result-moved-old", AffectedSurface: "internal/example",
	})
	if err != nil {
		t.Fatalf("revise with a moved Claim: %v", err)
	}
	if got := Dispositions(revised.State)["claim-dv-1"].Disposition; got != "planned" {
		t.Fatalf("moved Claim retained old disposition %q; want planned", got)
	}
	assignments := revised.State["review"].(map[string]any)["assignments"].(map[string]any)
	if _, exists := assignments["assignment-dv-1"]; exists {
		t.Fatalf("removed Assignment remains in current projection: %#v", assignments["assignment-dv-1"])
	}
	newDV, ok := assignments["assignment-dv-2"].(map[string]any)
	if !ok || newDV["status"] != "planned" || newDV["result_ref"] != nil || newDV["result_receipt"] != nil {
		t.Fatalf("new Assignment inherited the old result: %#v", assignments["assignment-dv-2"])
	}
	for _, raw := range revised.State["evidence"].([]any) {
		entry := raw.(map[string]any)
		if entry["id"] == "review-result-moved-old" && entry["status"] != "invalid" {
			t.Fatalf("old Result evidence status = %v; want invalid", entry["status"])
		}
	}

	// Completing all other Claims cannot close the round using the old DV
	// PASS. A fresh result from the new Assignment is required first.
	revisedPlan := mustLoadPlan(t, root, revised.State)
	revised = markDispatchedForRevision(t, root, statePath, journalPath, revised, "assignment-qa-1", "agent-qa-1")
	revised = markDispatchedForRevision(t, root, statePath, journalPath, revised, "assignment-dv-2", "agent-dv-2")
	qaPath := writeResultFile(t, root, revisedPlan, "assignment-qa-1", "review-result-moved-qa", "agent-qa-1", "pass",
		map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
	setResultAssignmentRevision(t, qaPath, 2)
	qaAccepted, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: revised.Revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
	})
	if err != nil {
		t.Fatalf("submit fresh QA result: %v", err)
	}
	if PlanPointerFromState(qaAccepted.State).Status == "clean" || Dispositions(qaAccepted.State)["claim-dv-1"].Disposition != "planned" {
		t.Fatal("round closed after QA using the superseded DV answer")
	}
	dvPath := writeResultFile(t, root, revisedPlan, "assignment-dv-2", "review-result-moved-fresh", "agent-dv-2", "pass",
		map[string]string{"claim-dv-1": "pass"}, nil)
	setResultAssignmentRevision(t, dvPath, 2)
	final, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: qaAccepted.Revision, AssignmentID: "assignment-dv-2", ResultPath: dvPath,
	})
	if err != nil {
		t.Fatalf("submit fresh moved-Claim result: %v", err)
	}
	if PlanPointerFromState(final.State).Status != "clean" {
		t.Fatalf("round did not close after the fresh moved-Claim result: %+v", PlanPointerFromState(final.State))
	}
	return root, statePath, journalPath, final
}

func TestReviewResultInputRejectsOversizeWithoutWrites(t *testing.T) {
	root, statePath, journalPath, snapshot, _ := packetModeFixture(t)
	beforeState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	beforeJournal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the input outside Main to preserve the supported worker-worktree
	// path form while exercising the byte bound.
	outside := t.TempDir()
	largePath := filepath.Join(outside, "oversized-result.json")
	file, err := os.Create(largePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(reviewResultInputLimit + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: largePath,
	})
	if err == nil || !strings.Contains(err.Error(), "exceeds the 20971520-byte limit") {
		t.Fatalf("oversized external input error = %v", err)
	}
	afterState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	afterJournal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeState, afterState) || !reflect.DeepEqual(beforeJournal, afterJournal) {
		t.Fatal("oversized ReviewResult input changed Runtime state or journal")
	}
}

func setResultAssignmentRevision(t *testing.T, path string, revision int) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	result["assignment_revision"] = revision
	writeJSONFile(t, path, result)
}

func markDispatchedForRevision(t *testing.T, root, statePath, journalPath string, snapshot loopruntime.Snapshot, assignmentID, agentID string) loopruntime.Snapshot {
	t.Helper()
	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	next, err := store.Update(snapshot.Revision, loopruntime.Mutation{
		EventID:        "evt-test-revision-dispatch-" + assignmentID,
		TransitionID:   "TEST",
		Event:          "test_revision_dispatch",
		Actor:          "test",
		IdempotencyKey: "test:revision-dispatch:" + assignmentID,
		Apply: func(state map[string]any) error {
			entities := state["entities"].(map[string]any)
			agents := entities["agents"].([]any)
			registered := false
			for _, raw := range agents {
				if agent, ok := raw.(map[string]any); ok && agent["id"] == agentID {
					registered = true
					break
				}
			}
			if !registered {
				agents = append(agents, map[string]any{
					"id": agentID, "role": "delivery-verifier", "state": "working",
					"task_ids": []any{}, "team_id": "team-review-1",
					"definition_ref": "agents/qa.md", "prompt_ref": ".claude/workgroups/REQ-TEST/m.json#" + agentID,
					"readback_ref": nil, "activation_ref": nil, "activation_revision": nil,
					"updated_at": "2026-08-18T00:00:00Z",
				})
				entities["agents"] = agents
			}
			reviewMap := state["review"].(map[string]any)
			row, ok := reviewMap["assignments"].(map[string]any)[assignmentID].(map[string]any)
			if !ok {
				return fmt.Errorf("assignment %s missing from revision projection", assignmentID)
			}
			row["status"] = "dispatched"
			row["agent_id"] = agentID
			return nil
		},
	})
	if err != nil {
		t.Fatalf("markDispatchedForRevision: %v", err)
	}
	return next
}

func mustLoadPlan(t *testing.T, root string, state map[string]any) *Plan {
	t.Helper()
	plan, _, err := LoadPlan(root, state)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func receiptRevisionPlan(t *testing.T, root string, state map[string]any, sourceRef string, changeDVMetadata bool) string {
	t.Helper()
	ptr := PlanPointerFromState(state)
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ptr.Path)))
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	for _, raw := range body["claims"].([]any) {
		claim := raw.(map[string]any)
		if claim["claim_id"] == "claim-qa-1" {
			claim["assertion"] = "updated QA assertion for revision receipt test"
			claim["source_refs"] = []string{sourceRef}
		}
	}
	if changeDVMetadata {
		for _, raw := range body["assignments"].([]any) {
			assignment := raw.(map[string]any)
			if assignment["assignment_id"] == "assignment-dv-1" {
				assignment["non_overlap_boundary"] = "updated delivery traceability boundary"
			}
		}
	}
	path := filepath.Join(root, "docs", "reports", "plan-revision.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, path, body)
	return path
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTypedResultReceiptFindingReplayDoesNotDuplicateFindingOrSeal(t *testing.T) {
	root, statePath, journalPath, snapshot, plan := packetModeFixture(t)
	finding := codeInspectionFinding("finding-qa-receipt", "claim-qa-1")
	qaPath := writeResultFile(t, root, plan, "assignment-qa-1", "review-result-qa-finding", "agent-qa-1", "finding",
		map[string]string{"claim-qa-1": "fail", "claim-qa-2": "pass"},
		[]Finding{finding})
	capturePath := CaptureFile(root, "loop-REQ-TEST", 1, "assignment-qa-1")
	if err := os.MkdirAll(filepath.Dir(capturePath), 0o755); err != nil {
		t.Fatal(err)
	}
	captureRef := fixtureEvidenceRef(t, root, "typed-capture-evidence.txt")
	if err := os.WriteFile(capturePath, []byte(`{"sequence":1,"claim_id":"claim-qa-1","action":"open screen","observed":"save failed","evidence_refs":["`+captureRef+`"]}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	accepted, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath, CaptureDir: capturePath,
	})
	if err != nil {
		t.Fatalf("submit finding result: %v", err)
	}
	if got := len(accepted.State["entities"].(map[string]any)["findings"].([]any)); got != 1 {
		t.Fatalf("finding count = %d, want 1", got)
	}
	resultEvidence, err := uniqueEvidenceByID(accepted.State, "review-result-qa-finding")
	if err != nil {
		t.Fatal(err)
	}
	artifactBytes, err := workpacket.ReadFile(root, stringField(resultEvidence["path"]), resultReceiptReadLimit)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(artifactBytes, &envelope); err != nil {
		t.Fatal(err)
	}
	var submitted, effective Result
	if err := json.Unmarshal(envelope["submitted_answer"], &submitted); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(envelope["effective_answer"], &effective); err != nil {
		t.Fatal(err)
	}
	if len(submitted.Findings[0].Encounter.Timeline) != 0 || len(effective.Findings[0].Encounter.Timeline) != 1 || stringFieldFromRaw(envelope["answer_sha256"]) == stringFieldFromRaw(envelope["effective_answer_sha256"]) {
		t.Fatal("typed envelope must preserve the authored empty timeline and separately digest its capture-enriched effective answer")
	}
	if accepted.State["review"].(map[string]any)["observation_batch"] != nil {
		t.Fatal("ordinary finding should drain while another Assignment is pending")
	}
	dvPath := writeResultFile(t, root, plan, "assignment-dv-1", "review-result-dv-1", "agent-dv-1", "pass",
		map[string]string{"claim-dv-1": "pass"}, nil)
	sealed, err := SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: accepted.Revision, AssignmentID: "assignment-dv-1", ResultPath: dvPath,
	})
	if err != nil {
		t.Fatalf("submit final pass: %v", err)
	}
	batchBefore := sealed.State["review"].(map[string]any)["observation_batch"]
	findingsBefore := len(sealed.State["entities"].(map[string]any)["findings"].([]any))
	_, err = SubmitResult(root, statePath, journalPath, SubmitRequest{
		ExpectedRevision: snapshot.Revision, AssignmentID: "assignment-qa-1", ResultPath: qaPath,
		CaptureDir: filepath.Join(root, "missing-captures"),
	})
	if err != nil {
		t.Fatalf("replay finding result after round seal: %v", err)
	}
	current, err := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(current.State["entities"].(map[string]any)["findings"].([]any)) != findingsBefore || !reflect.DeepEqual(current.State["review"].(map[string]any)["observation_batch"], batchBefore) {
		t.Fatal("finding replay duplicated a Finding or changed the sealed ObservationBatch")
	}
}

func TestProfessionalAnswerDigestCanonicalizesNestedObjectKeys(t *testing.T) {
	left := []byte(`{"verdict":"pass","claim_results":[{"claim_id":"c1","conclusion":"pass","observed":"ok","evidence_refs":["path:x#sha256=abc"]}]}`)
	right := []byte(`{"claim_results":[{"evidence_refs":["path:x#sha256=abc"],"observed":"ok","conclusion":"pass","claim_id":"c1"}],"verdict":"pass"}`)
	leftDigest, err := professionalAnswerDigest(left)
	if err != nil {
		t.Fatal(err)
	}
	rightDigest, err := professionalAnswerDigest(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftDigest != rightDigest {
		t.Fatalf("JSON key order changed canonical answer digest: %s != %s", leftDigest, rightDigest)
	}
}
