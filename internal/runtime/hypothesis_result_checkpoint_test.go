package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// acceptedHypothesisCheckpointFixture is a structural Runtime checkpoint
// fixture, not a live investigation Writer/model-evaluation test. It builds
// the same closed answer, Case receipt, acceptance event, and immutable
// successor history that the Runtime recovery reader must validate.
func acceptedHypothesisCheckpointFixture(t *testing.T) hypothesisPacketRuntimeFixture {
	t.Helper()
	f := newHypothesisPacketRuntimeFixture(t)
	answer := workpacket.HypothesisResultAnswer{
		Method:                        "Compared the same read with and without the cache.",
		EvidenceRefs:                  []string{"EVID-S8-HYPOTHESIS-001", "EVID-S8-CACHE-BYPASS"},
		SourceBoundaryRefs:            []string{"finding-001.encounter.first_bad_checkpoint"},
		Observed:                      "The cache-bypass read returned the persisted value.",
		CounterfactualOrDiscriminator: "A stale response without the cache would refute this hypothesis.",
		Result:                        hypothesisCheckpointString("supported"),
		// The Case row below intentionally stores these set-valued IDs in the
		// opposite order. The original consumer treats them as sets.
		ExplainsFindingIDs: []string{"finding-002", "finding-001"},
		DoesNotExplain:     []string{"finding-004", "finding-003"},
		NewHypotheses: []workpacket.HypothesisNewQuestion{{
			Statement:     "A second read path may retain stale values after invalidation.",
			Invariant:     "Invalidation must be visible to every read path.",
			Discriminator: "Compare both read paths after a single invalidation.",
			ExpectedOutcomes: workpacket.HypothesisExpectedOutcomes{
				Support: "Only one path observes the invalidation.",
				Refute:  "Both paths observe the invalidation.",
			},
			SourceFindingIDs: []string{"finding-001", "finding-002"},
			EvidenceRefs:     []string{"EVID-S8-HYPOTHESIS-001"},
		}},
	}
	answerBytes, err := json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	authoring := f.packet.Draft
	authoring.Answer = answerBytes
	authoringBytes, err := json.Marshal(authoring)
	if err != nil {
		t.Fatal(err)
	}
	canonicalAnswer, issues := workpacket.ValidateHypothesisResultAnswer(f.packet, authoringBytes)
	if len(issues) != 0 {
		t.Fatalf("mechanical complete S8 answer fixture is invalid: %#v", issues)
	}
	packetSHA, err := workpacket.HypothesisResultPacketDigest(f.packet)
	if err != nil {
		t.Fatal(err)
	}
	const successorRef = ".claude/review/investigation/cases/investigation-case-001-r2.json"
	receipt := HypothesisResultReceipt{
		Contract: "s8-hypothesis-result-receipt-v1", PacketID: f.packet.ID, PacketSHA256: packetSHA,
		AnswerSHA256: workpacket.Digest(canonicalAnswer), SubmittedAnswer: canonicalAnswer,
		OwnerAgentID: f.packet.Binding.OwnerAgentID, RuntimeID: f.packet.Binding.RuntimeID,
		BaselineGeneration: f.packet.Binding.BaselineGeneration, ReviewRound: f.packet.Binding.ReviewRound,
		AssignmentID: f.packet.Binding.AssignmentID, AssignmentSHA256: f.packet.Binding.AssignmentSHA256,
		TeamID: f.packet.Binding.TeamID, TaskID: f.packet.Binding.TaskID,
		TaskRef: f.packet.Binding.TaskRef, TaskSHA256: f.packet.Binding.TaskSHA256,
		ManifestRef: f.packet.Binding.ManifestRef, ManifestSHA256: f.packet.Binding.ManifestSHA256,
		ExecutionRoot: f.root, IssuanceEventID: f.packet.IssuanceEventID,
		AcceptedIdempotencyKey:       "runtime:investigation-case:" + f.packet.Binding.CaseID + ":3",
		AcceptedEventID:              "evt-investigation-case-" + f.packet.Binding.CaseID + "-r4",
		AcceptedAfterRuntimeRevision: 4, AcceptedSequence: 3,
		SuccessorCaseRef: successorRef, SuccessorCaseRevision: 2,
	}
	receiptBytes, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	var receiptMap map[string]any
	if err := json.Unmarshal(receiptBytes, &receiptMap); err != nil {
		t.Fatal(err)
	}
	newRows := expectedHypothesisCheckpointNewRows(t, f.packet, answer.NewHypotheses)
	newRowsAny := make([]any, 0, len(newRows))
	for _, row := range newRows {
		newRowsAny = append(newRowsAny, row)
	}
	resultRow := map[string]any{
		"hypothesis_id": f.packet.Binding.HypothesisID, "assignment_id": f.packet.Binding.AssignmentID,
		"method": answer.Method, "evidence_refs": answer.EvidenceRefs, "source_boundary_refs": answer.SourceBoundaryRefs,
		"observed": answer.Observed, "counterfactual_or_discriminator": answer.CounterfactualOrDiscriminator,
		"result": *answer.Result, "explains_finding_ids": []string{"finding-001", "finding-002"},
		"does_not_explain": []string{"finding-003", "finding-004"}, "receipt": receiptMap,
		"new_hypotheses": newRowsAny,
	}
	casePath := filepath.Join(f.root, filepath.FromSlash(f.packet.Binding.CaseRef))
	originalBytes, err := os.ReadFile(casePath)
	if err != nil {
		t.Fatal(err)
	}
	var originalCase map[string]any
	if err := json.Unmarshal(originalBytes, &originalCase); err != nil {
		t.Fatal(err)
	}
	successorCase := s8CheckpointCloneMap(t, originalCase)
	successorCase["revision"] = 2
	for _, hypothesis := range objectRows(successorCase["hypotheses"]) {
		if hypothesis["hypothesis_id"] == f.packet.Binding.HypothesisID {
			hypothesis["status"] = *answer.Result
		}
	}
	successorCase["hypotheses"] = append(objectRows(successorCase["hypotheses"]), newRows...)
	successorCase["hypothesis_results"] = []any{resultRow}
	successorBytes := s8CheckpointWriteJSON(t, f.root, successorRef, successorCase)

	// The active Case is now r3. Its durable revision_history preserves the
	// exact r2 result artifact that owns the receipt, exercising historical
	// source resolution instead of only the current-pointer fast path.
	const currentRef = ".claude/review/investigation/cases/investigation-case-001-r3.json"
	currentCase := s8CheckpointCloneMap(t, successorCase)
	currentCase["revision"] = 3
	currentCase["revision_history"] = []any{map[string]any{
		"revision": 2, "path": successorRef, "sha256": workpacket.Digest(successorBytes),
		"operation": "hypothesis_result_submitted", "occurred_at": "2026-10-14T00:02:00Z",
	}}
	currentBytes := s8CheckpointWriteJSON(t, f.root, currentRef, currentCase)

	cursor := map[string]any{"state": "bug_resolution", "phase": "investigation"}
	acceptedEvent := buildJournalEvent(Mutation{
		EventID: receipt.AcceptedEventID, TransitionID: "INVESTIGATION-CASE-REVISION-UPDATED", Event: "transition_committed",
		Actor: "orchestrator", IdempotencyKey: receipt.AcceptedIdempotencyKey, RuntimeID: f.packet.Binding.RuntimeID,
		BaselineGeneration: f.packet.Binding.BaselineGeneration, From: cursor, To: cursor,
		EvidenceIDs: []string{f.packet.Binding.CaseID},
	}, f.packet.Binding.RuntimeID, 3, 4, 3, time.Date(2026, 10, 14, 0, 2, 0, 0, time.UTC))
	f.journal = s8CheckpointJournalBytes(t, f.registerEvent, f.issueEvent, acceptedEvent)
	f.state["revision"] = 4
	f.state["journal"] = map[string]any{"last_sequence": 3, "last_event_id": receipt.AcceptedEventID}
	review := f.state["review"].(map[string]any)
	review["investigation"] = map[string]any{
		"case_id": f.packet.Binding.CaseID, "path": currentRef,
		"sha256": workpacket.Digest(currentBytes), "revision": 3,
	}
	return f
}

func hypothesisCheckpointString(value string) *string { return &value }

func expectedHypothesisCheckpointNewRows(t *testing.T, packet workpacket.HypothesisResultPacket, questions []workpacket.HypothesisNewQuestion) []map[string]any {
	t.Helper()
	rows := make([]map[string]any, 0, len(questions))
	for i, question := range questions {
		suffix := workpacket.Digest([]byte(packet.ID + "\x00" + strconv.Itoa(i)))
		rows = append(rows, map[string]any{
			"hypothesis_id": "hypothesis-s8-" + suffix,
			"assignment_id": "assignment-s8-" + suffix,
			"statement":     strings.TrimSpace(question.Statement),
			"invariant":     strings.TrimSpace(question.Invariant),
			"discriminator": strings.TrimSpace(question.Discriminator),
			"expected_outcomes": map[string]any{
				"support": strings.TrimSpace(question.ExpectedOutcomes.Support),
				"refute":  strings.TrimSpace(question.ExpectedOutcomes.Refute),
			},
			"source_finding_ids": append([]string(nil), question.SourceFindingIDs...),
			"evidence_refs":      append([]string(nil), question.EvidenceRefs...),
			"status":             "open",
		})
	}
	return rows
}

func readHypothesisCheckpointCase(t *testing.T, f hypothesisPacketRuntimeFixture) (map[string]any, string) {
	t.Helper()
	pointer := f.state["review"].(map[string]any)["investigation"].(map[string]any)
	ref := pointer["path"].(string)
	data, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(ref)))
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	return document, ref
}

func writeHypothesisCheckpointCurrentCase(t *testing.T, f *hypothesisPacketRuntimeFixture, document map[string]any, ref string) {
	t.Helper()
	data := s8CheckpointWriteJSON(t, f.root, ref, document)
	pointer := f.state["review"].(map[string]any)["investigation"].(map[string]any)
	pointer["sha256"] = workpacket.Digest(data)
}

func TestHypothesisResultCheckpointAcceptsCompleteAnswerAndSuccessorHistory(t *testing.T) {
	f := acceptedHypothesisCheckpointFixture(t)
	if _, err := validateHypothesisResultCheckpointIndices(f.root, f.state, f.journal); err != nil {
		t.Fatalf("valid typed S8 receipt/history checkpoint rejected: %v", err)
	}
}

func TestCurrentHypothesisResultPacketRemainsSelectableAfterConsumedRouteAndSiblingRevision(t *testing.T) {
	f := acceptedHypothesisCheckpointFixture(t)
	document, ref := readHypothesisCheckpointCase(t, f)
	document["route"] = "s9_repair"
	document["route_reason"] = "The accepted hypothesis now supports the original Case route."
	document["status"] = "routed"
	document["hypotheses"] = append(objectRows(document["hypotheses"]), map[string]any{
		"hypothesis_id": "hypothesis-sibling-002", "assignment_id": "assignment-sibling-002",
		"statement":          "A later sibling investigation tracks another discriminator.",
		"invariant":          "The sibling must remain independently answerable.",
		"discriminator":      "Compare the sibling path after invalidation.",
		"expected_outcomes":  map[string]any{"support": "The sibling path is stale.", "refute": "The sibling path is current."},
		"source_finding_ids": []string{"finding-001"}, "evidence_refs": []string{"EVID-S8-HYPOTHESIS-001"},
		"status": "open",
	})
	writeHypothesisCheckpointCurrentCase(t, &f, document, ref)
	selected, err := CurrentHypothesisResultPackets(f.root, f.state, f.packet.Binding.OwnerAgentID)
	if err != nil || len(selected) != 1 || selected[0].ID != f.packet.ID {
		t.Fatalf("accepted packet was lost after a compatible sibling/route revision: packets=%d err=%v", len(selected), err)
	}
}

func TestAcceptedHypothesisResultRemainsQueryableAfterPointerOnlyRouteConsumption(t *testing.T) {
	f := acceptedHypothesisCheckpointFixture(t)
	pointer := f.state["review"].(map[string]any)["investigation"].(map[string]any)
	pointer["route"] = "investigate_more"
	pointer["route_reason"] = "The Case needs one more investigation pass."
	pointer["route_consumed_at"] = "2026-10-14T12:00:00Z"
	pointer["route_consumer"] = "S8-ROUTE-CONSUME"

	selected, err := CurrentHypothesisResultPackets(f.root, f.state, f.packet.Binding.OwnerAgentID)
	if err != nil || len(selected) != 1 || selected[0].ID != f.packet.ID {
		t.Fatalf("accepted S8 receipt was lost from read-only query after pointer-only route consumption: packets=%d err=%v", len(selected), err)
	}
}

func TestHypothesisResultCheckpointRejectsReceiptTupleAnswerEventAndHistoryDrift(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *hypothesisPacketRuntimeFixture, map[string]any, string, []byte)
	}{
		{
			name: "receipt machine tuple",
			mutate: func(t *testing.T, f *hypothesisPacketRuntimeFixture, document map[string]any, ref string, journal []byte) {
				row := objectRows(document["hypothesis_results"])[0]
				receipt := row["receipt"].(map[string]any)
				receipt["assignment_sha256"] = strings.Repeat("e", 64)
				writeHypothesisCheckpointCurrentCase(t, f, document, ref)
				if _, err := validateHypothesisResultCheckpointIndices(f.root, f.state, journal); err == nil || !strings.Contains(err.Error(), "machine tuple") {
					t.Fatalf("tampered receipt tuple was accepted: %v", err)
				}
			},
		},
		{
			name: "complete answer digest",
			mutate: func(t *testing.T, f *hypothesisPacketRuntimeFixture, document map[string]any, ref string, journal []byte) {
				row := objectRows(document["hypothesis_results"])[0]
				receipt := row["receipt"].(map[string]any)
				answer := receipt["submitted_answer"].(map[string]any)
				answer["observed"] = "A different answer was injected after commit."
				writeHypothesisCheckpointCurrentCase(t, f, document, ref)
				if _, err := validateHypothesisResultCheckpointIndices(f.root, f.state, journal); err == nil || !strings.Contains(err.Error(), "complete professional answer") {
					t.Fatalf("tampered complete answer was accepted: %v", err)
				}
			},
		},
		{
			name: "acceptance event actor",
			mutate: func(t *testing.T, f *hypothesisPacketRuntimeFixture, document map[string]any, ref string, journal []byte) {
				corrupt := s8CheckpointChangeJournalEvent(t, journal, 2, func(event map[string]any) {
					event["actor"] = map[string]any{"type": "agent", "id": f.packet.Binding.OwnerAgentID}
				})
				if _, err := validateHypothesisResultCheckpointIndices(f.root, f.state, corrupt); err == nil || !strings.Contains(err.Error(), "acceptance coordinates") {
					t.Fatalf("acceptance event with worker-authored actor was accepted: %v", err)
				}
			},
		},
		{
			name: "acceptance event idempotency key",
			mutate: func(t *testing.T, f *hypothesisPacketRuntimeFixture, document map[string]any, ref string, journal []byte) {
				corrupt := s8CheckpointChangeJournalEvent(t, journal, 2, func(event map[string]any) { event["idempotency_key"] = "other-result" })
				if _, err := validateHypothesisResultCheckpointIndices(f.root, f.state, corrupt); err == nil || !strings.Contains(err.Error(), "acceptance coordinates") {
					t.Fatalf("acceptance event with wrong idempotency key was accepted: %v", err)
				}
			},
		},
		{
			name: "receipt and event use a noncanonical acceptance key",
			mutate: func(t *testing.T, f *hypothesisPacketRuntimeFixture, document map[string]any, ref string, journal []byte) {
				row := objectRows(document["hypothesis_results"])[0]
				receipt := row["receipt"].(map[string]any)
				receipt["accepted_idempotency_key"] = "runtime:investigation-case:forged:3"
				writeHypothesisCheckpointCurrentCase(t, f, document, ref)
				corrupt := s8CheckpointChangeJournalEvent(t, journal, 2, func(event map[string]any) {
					event["idempotency_key"] = "runtime:investigation-case:forged:3"
				})
				if _, err := validateHypothesisResultCheckpointIndices(f.root, f.state, corrupt); err == nil || !strings.Contains(err.Error(), "acceptance identity") {
					t.Fatalf("matching forged event/receipt key bypassed the original constructor: %v", err)
				}
			},
		},
		{
			name: "receipt and journal use a noncanonical acceptance event ID",
			mutate: func(t *testing.T, f *hypothesisPacketRuntimeFixture, document map[string]any, ref string, journal []byte) {
				row := objectRows(document["hypothesis_results"])[0]
				receipt := row["receipt"].(map[string]any)
				forgedID := "evt-investigation-case-forged-r4"
				receipt["accepted_event_id"] = forgedID
				writeHypothesisCheckpointCurrentCase(t, f, document, ref)
				corrupt := s8CheckpointChangeJournalEvent(t, journal, 2, func(event map[string]any) {
					event["event_id"] = forgedID
				})
				f.state["journal"].(map[string]any)["last_event_id"] = forgedID
				if _, err := validateHypothesisResultCheckpointIndices(f.root, f.state, corrupt); err == nil || !strings.Contains(err.Error(), "acceptance identity") {
					t.Fatalf("matching forged event/receipt ID bypassed the original constructor: %v", err)
				}
			},
		},
		{
			name: "server-assigned new hypothesis identity",
			mutate: func(t *testing.T, f *hypothesisPacketRuntimeFixture, document map[string]any, ref string, journal []byte) {
				row := objectRows(document["hypothesis_results"])[0]
				newQuestion := objectRows(row["new_hypotheses"])[0]
				newQuestion["hypothesis_id"] = "hypothesis-s8-forged"
				writeHypothesisCheckpointCurrentCase(t, f, document, ref)
				if _, err := validateHypothesisResultCheckpointIndices(f.root, f.state, journal); err == nil || !strings.Contains(err.Error(), "server-assigned new hypotheses") {
					t.Fatalf("tampered server-assigned hypothesis ID was accepted: %v", err)
				}
			},
		},
		{
			name: "server-assigned new hypothesis Assignment identity",
			mutate: func(t *testing.T, f *hypothesisPacketRuntimeFixture, document map[string]any, ref string, journal []byte) {
				row := objectRows(document["hypothesis_results"])[0]
				newQuestion := objectRows(row["new_hypotheses"])[0]
				newQuestion["assignment_id"] = "assignment-s8-forged"
				writeHypothesisCheckpointCurrentCase(t, f, document, ref)
				if _, err := validateHypothesisResultCheckpointIndices(f.root, f.state, journal); err == nil || !strings.Contains(err.Error(), "server-assigned new hypotheses") {
					t.Fatalf("tampered server-assigned hypothesis Assignment ID was accepted: %v", err)
				}
			},
		},
		{
			name: "new hypothesis professional answer",
			mutate: func(t *testing.T, f *hypothesisPacketRuntimeFixture, document map[string]any, ref string, journal []byte) {
				row := objectRows(document["hypothesis_results"])[0]
				receipt := row["receipt"].(map[string]any)
				answer := receipt["submitted_answer"].(map[string]any)
				newQuestion := objectRows(answer["new_hypotheses"])[0]
				newQuestion["statement"] = "A different follow-up question was substituted."
				writeHypothesisCheckpointCurrentCase(t, f, document, ref)
				if _, err := validateHypothesisResultCheckpointIndices(f.root, f.state, journal); err == nil || !strings.Contains(err.Error(), "complete professional answer") {
					t.Fatalf("tampered new hypothesis answer was accepted: %v", err)
				}
			},
		},
		{
			name: "new hypothesis missing from original successor",
			mutate: func(t *testing.T, f *hypothesisPacketRuntimeFixture, document map[string]any, ref string, journal []byte) {
				history := objectRows(document["revision_history"])[0]
				successorRef := history["path"].(string)
				successorData, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(successorRef)))
				if err != nil {
					t.Fatal(err)
				}
				var successor map[string]any
				if err := json.Unmarshal(successorData, &successor); err != nil {
					t.Fatal(err)
				}
				newID := objectRows(objectRows(successor["hypothesis_results"])[0]["new_hypotheses"])[0]["hypothesis_id"]
				kept := make([]map[string]any, 0)
				for _, question := range objectRows(successor["hypotheses"]) {
					if question["hypothesis_id"] != newID {
						kept = append(kept, question)
					}
				}
				successor["hypotheses"] = kept
				successorBytes := s8CheckpointWriteJSON(t, f.root, successorRef, successor)
				history["sha256"] = workpacket.Digest(successorBytes)
				writeHypothesisCheckpointCurrentCase(t, f, document, ref)
				if _, err := validateHypothesisResultCheckpointIndices(f.root, f.state, journal); err == nil || !strings.Contains(err.Error(), "missing or duplicated") {
					t.Fatalf("new question absent from the original successor was accepted: %v", err)
				}
			},
		},
		{
			name: "successor embedded revision",
			mutate: func(t *testing.T, f *hypothesisPacketRuntimeFixture, document map[string]any, ref string, journal []byte) {
				history := objectRows(document["revision_history"])[0]
				successorRef := history["path"].(string)
				successorData, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(successorRef)))
				if err != nil {
					t.Fatal(err)
				}
				var successor map[string]any
				if err := json.Unmarshal(successorData, &successor); err != nil {
					t.Fatal(err)
				}
				successor["revision"] = 9
				successorBytes := s8CheckpointWriteJSON(t, f.root, successorRef, successor)
				history["sha256"] = workpacket.Digest(successorBytes)
				writeHypothesisCheckpointCurrentCase(t, f, document, ref)
				if _, err := validateHypothesisResultCheckpointIndices(f.root, f.state, journal); err == nil || !strings.Contains(err.Error(), "successor Case revision differs") {
					t.Fatalf("successor with a mismatched embedded revision was accepted: %v", err)
				}
			},
		},
		{
			name: "accepted result must close original hypothesis",
			mutate: func(t *testing.T, f *hypothesisPacketRuntimeFixture, document map[string]any, ref string, journal []byte) {
				history := objectRows(document["revision_history"])[0]
				successorRef := history["path"].(string)
				successorData, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(successorRef)))
				if err != nil {
					t.Fatal(err)
				}
				var successor map[string]any
				if err := json.Unmarshal(successorData, &successor); err != nil {
					t.Fatal(err)
				}
				for _, question := range objectRows(successor["hypotheses"]) {
					if question["hypothesis_id"] == f.packet.Binding.HypothesisID {
						question["status"] = "open"
					}
				}
				successorBytes := s8CheckpointWriteJSON(t, f.root, successorRef, successor)
				history["sha256"] = workpacket.Digest(successorBytes)
				writeHypothesisCheckpointCurrentCase(t, f, document, ref)
				if _, err := validateHypothesisResultCheckpointIndices(f.root, f.state, journal); err == nil {
					t.Fatal("accepted result with an open original hypothesis was accepted")
				}
			},
		},
		{
			name: "original hypothesis conclusion",
			mutate: func(t *testing.T, f *hypothesisPacketRuntimeFixture, document map[string]any, ref string, journal []byte) {
				history := objectRows(document["revision_history"])[0]
				successorRef := history["path"].(string)
				successorData, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(successorRef)))
				if err != nil {
					t.Fatal(err)
				}
				var successor map[string]any
				if err := json.Unmarshal(successorData, &successor); err != nil {
					t.Fatal(err)
				}
				for _, question := range objectRows(successor["hypotheses"]) {
					if question["hypothesis_id"] == f.packet.Binding.HypothesisID {
						question["status"] = "open"
					}
				}
				successorBytes := s8CheckpointWriteJSON(t, f.root, successorRef, successor)
				history["sha256"] = workpacket.Digest(successorBytes)
				writeHypothesisCheckpointCurrentCase(t, f, document, ref)
				if _, err := validateHypothesisResultCheckpointIndices(f.root, f.state, journal); err == nil {
					t.Fatal("original successor retaining an open hypothesis was accepted")
				}
			},
		},
		{
			name: "successor history digest",
			mutate: func(t *testing.T, f *hypothesisPacketRuntimeFixture, document map[string]any, ref string, journal []byte) {
				history := objectRows(document["revision_history"])[0]
				history["sha256"] = strings.Repeat("f", 64)
				writeHypothesisCheckpointCurrentCase(t, f, document, ref)
				if _, err := validateHypothesisResultCheckpointIndices(f.root, f.state, journal); err == nil {
					t.Fatal("tampered original successor history digest was accepted")
				}
			},
		},
		{
			name: "pinned indexed evidence source",
			mutate: func(t *testing.T, f *hypothesisPacketRuntimeFixture, document map[string]any, ref string, journal []byte) {
				evidence := f.packet.Inputs.EvidenceRefs[0]
				if err := os.WriteFile(filepath.Join(f.root, filepath.FromSlash(evidence.Path)), []byte(`{"changed":true}`), 0o600); err != nil {
					t.Fatal(err)
				}
				if _, err := validateHypothesisResultCheckpointIndices(f.root, f.state, journal); err == nil {
					t.Fatal("changed indexed evidence bytes were accepted as part of the original S8 packet")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := acceptedHypothesisCheckpointFixture(t)
			document, ref := readHypothesisCheckpointCase(t, f)
			tt.mutate(t, &f, document, ref, f.journal)
		})
	}
}

func TestHypothesisResultCheckpointRejectsCurrentCaseReceiptWithoutTypedIssue(t *testing.T) {
	f := acceptedHypothesisCheckpointFixture(t)
	state := s8CheckpointCloneMap(t, f.state)
	state["entities"].(map[string]any)["work_packets"] = []any{}
	if _, err := validateHypothesisResultCheckpointIndices(f.root, state, f.journal); err == nil {
		t.Fatal("current typed receipt without its original issued packet was accepted")
	}

	// Keep only a schema-valid issue event but remove its corresponding state
	// row. The checkpoint reader must reject the event instead of reconstructing
	// its missing packet from the accepted Case receipt.
	if _, err := validateHypothesisResultCheckpointIndices(f.root, state, f.journal); err == nil {
		t.Fatal("orphan S8 receipt/issue pair was accepted")
	}
}
