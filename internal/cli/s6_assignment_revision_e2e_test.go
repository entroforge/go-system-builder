package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/cli"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

type s6RevisionCLIOutput struct {
	Revision int                                  `json:"Revision"`
	State    map[string]any                       `json:"State"`
	Receipt  assignment.RevisionAcceptanceReceipt `json:"assignment_revision_receipt"`
}

func TestS6AssignmentRevisionCLIReceiptPendingRecoveryAndHistoricalReplay(t *testing.T) {
	fixture := readyS6CanonicalResultCLI(t)
	statePath := filepath.Join(fixture.root, ".claude/loop-state.json")
	journalPath := filepath.Join(fixture.root, ".claude/loop-events.jsonl")
	beforeBadState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	beforeBadJournal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	var badOut, badErr bytes.Buffer
	badCode := cli.Run([]string{
		"runtime", "register-workgroup", "--root", fixture.root,
		"--manifest", ".claude/workgroups/manifest-s6-native-context.json",
		"--task-id", fixture.record.Task.ID, "--task", fixture.record.Task.Ref,
		"--revise-assignment", fixture.record.AssignmentID,
		"--revision-intent-key", "revision-paired-flags-negative",
		"--expected-record-ref", `{"runtime_id":"x"}`,
	}, nil, &badOut, &badErr)
	if badCode != 2 || !strings.Contains(badErr.String(), "requires --expected-record-ref and --expected-record-sha256 together") {
		t.Fatalf("unpaired Record assertion code=%d stderr=%s", badCode, badErr.String())
	}
	assertS6RevisionPairUnchanged(t, statePath, journalPath, beforeBadState, beforeBadJournal)
	staleCode, _, staleErr := runS6RevisionCLI(fixture.root, ".claude/workgroups/manifest-s6-native-context.json",
		fixture.record.Task.ID, fixture.record.Task.Ref, fixture.record.AssignmentID,
		"revision-stale-record-negative", fixture.record.Reference(), strings.Repeat("0", 64))
	if staleCode == 0 || !strings.Contains(staleErr, "stale Assignment revision") {
		t.Fatalf("stale Record SHA was not rejected by the original Writer: code=%d stderr=%s", staleCode, staleErr)
	}
	assertS6RevisionPairUnchanged(t, statePath, journalPath, beforeBadState, beforeBadJournal)

	// Accept one actual S6 Result and Main ACK before revising. Revision must
	// clear their current pointers while preserving the original artifacts.
	if err := os.WriteFile(filepath.Join(fixture.root, filepath.FromSlash(fixture.sourceRef)), []byte("durable Result source for revision test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	completion := s6CanonicalCompletionMessage(fixture, "msg-s6-revision-result", "I completed the reviewed TASK and recorded its evidence.")
	completion["occurred_at"] = "2026-10-14T14:10:00Z"
	completionPath := filepath.Join(fixture.root, ".claude/evidence/s6-revision/result.json")
	stageJSON(t, fixture.root, ".claude/evidence/s6-revision/result.json", completion)
	code, completionOutput, completionErr := s6RunCanonicalTaskComplete(fixture, completionPath)
	if code != 0 {
		t.Fatalf("accept canonical Result: code=%d stderr=%s stdout=%s", code, completionErr, completionOutput)
	}
	acceptedResult := s6DecodeCanonicalTaskCompleteOutput(t, completionOutput).Acceptance
	if acceptedResult.ResultRef == "" || !acceptedResult.Current {
		t.Fatalf("canonical Result was not current before revision: %+v", acceptedResult)
	}
	ackMessage := map[string]any{
		"schema_version": "1.0.0", "message_type": "completion_ack",
		"message_id": "msg-s6-revision-main-ack", "correlation_id": "corr-s6-revision-main-ack",
		"runtime_id": fixture.record.RuntimeID, "agent_id": fixture.record.OwnerAgentID,
		"agent_definition_ref": fixture.record.AgentDefinition.Ref, "task_id": fixture.record.Task.ID,
		"bug_id": nil, "team_id": fixture.record.TeamID, "occurred_at": "2026-10-14T14:11:00Z",
	}
	stageJSON(t, fixture.root, ".claude/evidence/s6-revision/ack.json", ackMessage)
	ackPath := filepath.Join(fixture.root, ".claude/evidence/s6-revision/ack.json")
	acknowledged, err := assignment.AdvanceAgent(fixture.root, statePath, journalPath, assignment.AgentEventRequest{
		ExpectedRevision: -1, AgentID: fixture.record.OwnerAgentID,
		Event: "completion_acknowledged", MessagePath: ackPath,
	})
	if err != nil {
		t.Fatalf("record original Main ACK: %v", err)
	}
	oldAgent := s6AgentRow(t, acknowledged.State, fixture.record.OwnerAgentID)
	if oldAgent["state"] != "done" || oldAgent["completion_acknowledged_ref"] == nil {
		t.Fatalf("fixture did not persist original ACK: %+v", oldAgent)
	}
	oldACKRef := oldAgent["completion_acknowledged_ref"].(string)
	oldACKBytes, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(oldACKRef)))
	if err != nil {
		t.Fatalf("read original ACK artifact %s: %v", oldACKRef, err)
	}
	oldResultBytes, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(acceptedResult.ResultRef)))
	if err != nil || workpacket.Digest(oldResultBytes) != acceptedResult.ResultSHA256 {
		t.Fatalf("read original accepted Result artifact: %v", err)
	}

	preRevisionState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	preRevisionJournal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	r1SHA, err := fixture.record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	r2ManifestPath := ".claude/workgroups/manifest-s6-native-context.json"
	s6WriteRevisionManifest(t, fixture.root, r2ManifestPath, "r2", "agent-s6-native-context-r2")
	r2ManifestBytes, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(r2ManifestPath)))
	if err != nil {
		t.Fatal(err)
	}
	issuedR2, outR2, errout := runS6RevisionCLI(fixture.root, r2ManifestPath, fixture.record.Task.ID, fixture.record.Task.Ref,
		fixture.record.AssignmentID, "s6-revision-intent-r2", fixture.record.Reference(), r1SHA)
	if issuedR2 != 0 {
		t.Fatalf("issue r2 through register-workgroup: code=%d stderr=%s stdout=%s", issuedR2, errout, outR2)
	}
	r2Output := decodeS6RevisionOutput(t, outR2)
	r2Ref := assignmentrecord.RecordReference{RuntimeID: fixture.record.RuntimeID, BaselineGeneration: fixture.record.BaselineGeneration, AssignmentID: fixture.record.AssignmentID, Revision: 2}
	r2IssuedRecord, found, err := assignmentrecord.FindRecordRevision(mustS6Records(t, r2Output.State), r2Ref)
	if err != nil || !found {
		t.Fatalf("load issued r2 Record: found=%v err=%v", found, err)
	}
	r2IssuedSHA, err := r2IssuedRecord.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if r2Output.Receipt.AssignmentRecord != r2Ref || r2Output.Receipt.Revision != 2 ||
		r2Output.Receipt.AssignmentRecordSHA256 != r2IssuedSHA || r2Output.Receipt.RuntimeEventID == "" ||
		r2Output.Receipt.RuntimeCommitRevision != r2IssuedRecord.RegistrationRevision ||
		!r2Output.Receipt.Current || r2Output.Receipt.Replayed {
		t.Fatalf("r2 CLI did not return its original first-issue receipt: %+v", r2Output.Receipt)
	}
	if got := s6AgentRow(t, r2Output.State, fixture.record.OwnerAgentID); got["state"] != "done" ||
		got["completion_acknowledged_ref"] != nil || got["completion_reported_ref"] != nil || got["completion_acknowledged_reports"] != nil {
		t.Fatalf("revision failed to invalidate terminal owner's current Result/ACK pointers: %+v", got)
	}
	if got := s6TaskRow(t, r2Output.State, fixture.record.Task.ID); got["completion_report_ref"] != nil {
		t.Fatalf("revision kept prior TASK current completion pointer: %+v", got)
	}
	if !s6ResultEvidenceSuperseded(r2Output.State, acceptedResult.ResultRef, r2Output.Receipt.RuntimeEventID) {
		t.Fatal("revision did not mark the prior Result evidence superseded by its original event")
	}
	if after, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(acceptedResult.ResultRef))); err != nil || !bytes.Equal(after, oldResultBytes) {
		t.Fatalf("revision removed/changed historical Result artifact: %v", err)
	}
	if after, err := os.ReadFile(filepath.Join(fixture.root, filepath.FromSlash(oldACKRef))); err != nil || !bytes.Equal(after, oldACKBytes) {
		t.Fatalf("revision removed/changed historical ACK artifact: %v", err)
	}
	assertS6RevisionReceiptRejectsInvalidJournal(t, r2Output.State, r2Output.Receipt.RuntimeEventID, journalPath)

	// Mechanistic pending-boundary fixture: retain the valid Writer candidate
	// and original event, restore the exact pre-write pair, then let the same
	// original Writer recover that recorded candidate. This is not a simulated
	// process crash or a new recovery path.
	targetStateBytes, targetJournal, err := readS6RevisionPair(t, statePath, journalPath)
	if err != nil {
		t.Fatal(err)
	}
	marker := s6SyntheticCommitPending(t, preRevisionState, targetStateBytes, targetJournal)
	if err := os.WriteFile(statePath, preRevisionState, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, preRevisionJournal, 0o600); err != nil {
		t.Fatal(err)
	}
	markerPath := statePath + ".commit-pending.json"
	if err := os.WriteFile(markerPath, marker, 0o600); err != nil {
		t.Fatal(err)
	}
	recoveredCode, recoveredOut, recoveredErr := runS6RevisionCLI(fixture.root, r2ManifestPath, fixture.record.Task.ID, fixture.record.Task.Ref,
		fixture.record.AssignmentID, "s6-revision-intent-r2", fixture.record.Reference(), r1SHA)
	if recoveredCode != 0 {
		t.Fatalf("same-intent call did not recover the recorded pending Writer candidate: code=%d stderr=%s stdout=%s", recoveredCode, recoveredErr, recoveredOut)
	}
	recovered := decodeS6RevisionOutput(t, recoveredOut)
	if recovered.Receipt.AssignmentRecord != r2Ref || !recovered.Receipt.Current || !recovered.Receipt.Replayed {
		t.Fatalf("pending recovery did not return original r2 issuance receipt: %+v", recovered.Receipt)
	}
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("original Store did not retire the recovered pending marker: %v", err)
	}
	stateAfterRecovery, journalAfterRecovery, err := readS6RevisionPair(t, statePath, journalPath)
	if err != nil {
		t.Fatal(err)
	}
	assertS6RevisionPairMatchesSnapshot(t, stateAfterRecovery, journalAfterRecovery, targetStateBytes, targetJournal)
	stateBeforeExactReplay, journalBeforeExactReplay := append([]byte(nil), stateAfterRecovery...), append([]byte(nil), journalAfterRecovery...)
	replayCode, replayOut, replayErr := runS6RevisionCLI(fixture.root, r2ManifestPath, fixture.record.Task.ID, fixture.record.Task.Ref,
		fixture.record.AssignmentID, "s6-revision-intent-r2", fixture.record.Reference(), r1SHA)
	if replayCode != 0 {
		t.Fatalf("exact r2 replay: code=%d stderr=%s stdout=%s", replayCode, replayErr, replayOut)
	}
	replayedR2 := decodeS6RevisionOutput(t, replayOut)
	if !replayedR2.Receipt.Replayed || !replayedR2.Receipt.Current || replayedR2.Receipt.Revision != 2 {
		t.Fatalf("exact current r2 replay did not identify the original issuance: %+v", replayedR2.Receipt)
	}
	assertS6RevisionPairUnchanged(t, statePath, journalPath, stateBeforeExactReplay, journalBeforeExactReplay)

	r2Record, found, err := assignmentrecord.ForOwner(replayedR2.State, "agent-s6-native-context-r2")
	if err != nil || !found {
		t.Fatalf("load current r2 Record: found=%v err=%v", found, err)
	}
	r2SHA, err := r2Record.Digest()
	if err != nil {
		t.Fatal(err)
	}
	r3ManifestPath := ".claude/workgroups/manifest-s6-native-context-r3.json"
	s6WriteRevisionManifest(t, fixture.root, r3ManifestPath, "r3", "agent-s6-native-context-r3")
	r3Code, r3Out, r3Err := runS6RevisionCLI(fixture.root, r3ManifestPath, fixture.record.Task.ID, fixture.record.Task.Ref,
		fixture.record.AssignmentID, "s6-revision-intent-r3", r2Record.Reference(), r2SHA)
	if r3Code != 0 {
		t.Fatalf("issue r3: code=%d stderr=%s stdout=%s", r3Code, r3Err, r3Out)
	}
	r3Output := decodeS6RevisionOutput(t, r3Out)
	r3Ref := r2Ref
	r3Ref.Revision = 3
	if r3Output.Receipt.AssignmentRecord != r3Ref || r3Output.Receipt.Revision != 3 || !r3Output.Receipt.Current || r3Output.Receipt.Replayed {
		t.Fatalf("r3 CLI receipt mismatch: %+v", r3Output.Receipt)
	}
	r3Record, found, err := assignmentrecord.ForOwner(r3Output.State, "agent-s6-native-context-r3")
	if err != nil || !found || r3Record.Revision != 3 {
		t.Fatalf("current state did not advance to r3: found=%v record=%+v err=%v", found, r3Record, err)
	}

	// Historical r2 input is still exact at its original manifest ref. Its
	// replay returns the r2 Writer coordinates while the state remains r3.
	stateBeforeHistoricalReplay, journalBeforeHistoricalReplay, err := readS6RevisionPair(t, statePath, journalPath)
	if err != nil {
		t.Fatal(err)
	}
	historicalCode, historicalOut, historicalErr := runS6RevisionCLI(fixture.root, r2ManifestPath, fixture.record.Task.ID, fixture.record.Task.Ref,
		fixture.record.AssignmentID, "s6-revision-intent-r2", fixture.record.Reference(), r1SHA)
	if historicalCode != 0 {
		t.Fatalf("historical r2 replay after r3: code=%d stderr=%s stdout=%s", historicalCode, historicalErr, historicalOut)
	}
	historical := decodeS6RevisionOutput(t, historicalOut)
	if historical.Receipt.AssignmentRecord != r2Ref || historical.Receipt.Revision != 2 || !historical.Receipt.Replayed || historical.Receipt.Current {
		t.Fatalf("historical replay reported current r3 coordinates instead of the original r2 receipt: %+v", historical.Receipt)
	}
	if historical.Revision != r3Output.Revision {
		t.Fatalf("historical replay regressed Runtime revision: got %d want %d", historical.Revision, r3Output.Revision)
	}
	currentAfterHistorical, found, err := assignmentrecord.ForOwner(historical.State, "agent-s6-native-context-r3")
	if err != nil || !found || currentAfterHistorical.Reference() != r3Record.Reference() {
		t.Fatalf("historical r2 replay rolled current Assignment back: found=%v current=%+v err=%v", found, currentAfterHistorical, err)
	}
	assertS6RevisionPairUnchanged(t, statePath, journalPath, stateBeforeHistoricalReplay, journalBeforeHistoricalReplay)
	if !bytes.Equal(r2ManifestBytes, mustReadS6(t, fixture.root, r2ManifestPath)) {
		t.Fatal("historical r2 manifest source changed during replay")
	}
}

func TestS6AssignmentRevisionSamePredecessorConcurrentIntentsOnlyOneWinner(t *testing.T) {
	root, taskID, taskPath := newS6RecordContextFixture(t)
	stageCLI(t, "runtime", "register-workgroup", "--root", root,
		"--manifest", ".claude/workgroups/manifest-s6-native-context.json", "--task-id", taskID, "--task", taskPath)
	initial := stageSnapshot(t, root)
	previous, found, err := assignmentrecord.ForOwner(initial.State, "agent-s6-native-context")
	if err != nil || !found {
		t.Fatalf("load predecessor Record: found=%v err=%v", found, err)
	}
	previousSHA, err := previous.Digest()
	if err != nil {
		t.Fatal(err)
	}
	manifests := []string{
		".claude/workgroups/manifest-s6-concurrent-a.json",
		".claude/workgroups/manifest-s6-concurrent-b.json",
	}
	owners := []string{"agent-s6-concurrent-a", "agent-s6-concurrent-b"}
	for i := range manifests {
		s6WriteRevisionManifest(t, root, manifests[i], owners[i], owners[i])
	}
	type outcome struct {
		code   int
		stdout string
		stderr string
	}
	results := make([]outcome, len(manifests))
	start := make(chan struct{})
	var group sync.WaitGroup
	for i := range manifests {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			<-start
			results[i].code, results[i].stdout, results[i].stderr = runS6RevisionCLI(root, manifests[i], taskID, taskPath,
				previous.AssignmentID, "s6-concurrent-intent-"+owners[i], previous.Reference(), previousSHA)
		}(i)
	}
	close(start)
	group.Wait()
	winners, losers := 0, 0
	for _, result := range results {
		if result.code == 0 {
			winners++
			accepted := decodeS6RevisionOutput(t, result.stdout)
			if accepted.Receipt.Revision != 2 || !accepted.Receipt.Current || accepted.Receipt.Replayed {
				t.Fatalf("concurrent winner returned wrong r2 receipt: %+v", accepted.Receipt)
			}
		} else {
			losers++
			if !strings.Contains(result.stderr, "stale Assignment revision") && !strings.Contains(result.stderr, "current Record changed") && !strings.Contains(result.stderr, "integration workspace busy") {
				t.Fatalf("concurrent loser did not fail busy/stale before issuing another Record: %s", result.stderr)
			}
		}
	}
	if winners != 1 || losers != 1 {
		t.Fatalf("same-predecessor concurrent revisions winners=%d losers=%d results=%+v", winners, losers, results)
	}
	final := stageSnapshot(t, root)
	records, err := assignmentrecord.RecordsFromState(final.State)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || len(final.State["entities"].(map[string]any)["assignment_revision_receipts"].([]any)) != 1 {
		t.Fatalf("concurrent loser left an extra issued Record/receipt: records=%d state=%v", len(records), final.State["entities"].(map[string]any)["assignment_revision_receipts"])
	}
	current, found, err := assignmentrecord.ForOwner(final.State, currentOwnerFromRecords(t, records, previous.AssignmentID))
	if err != nil || !found || current.Revision != 2 {
		t.Fatalf("concurrent winner did not leave exactly current r2: found=%v current=%+v err=%v", found, current, err)
	}
}

func currentOwnerFromRecords(t *testing.T, records []assignmentrecord.Record, assignmentID string) string {
	t.Helper()
	for _, record := range records {
		if record.AssignmentID == assignmentID && record.Revision == 2 {
			return record.OwnerAgentID
		}
	}
	t.Fatalf("no r2 Record for %s", assignmentID)
	return ""
}

func s6WriteRevisionManifest(t *testing.T, root, path, suffix, owner string) {
	t.Helper()
	basePath := filepath.Join(root, ".claude/workgroups/manifest-s6-native-context.json")
	data, err := os.ReadFile(basePath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["manifest_id"] = "team-manifest-s6-" + suffix
	manifest["version"] = "v1.0.1"
	manifest["platform_team_id"] = "platform-s6-" + suffix
	manifest["workgroup_id"] = "workgroup-s6-" + suffix
	assignments, _ := manifest["assignments"].([]any)
	if len(assignments) != 1 {
		t.Fatalf("fixture manifest assignments=%d want exactly one", len(assignments))
	}
	row, _ := assignments[0].(map[string]any)
	row["agent_id"] = owner
	stageJSON(t, root, path, manifest)
}

func runS6RevisionCLI(root, manifestPath, taskID, taskPath, assignmentID, intent string, previous assignmentrecord.RecordReference, previousSHA string) (int, string, string) {
	refBytes, _ := json.Marshal(previous)
	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "register-workgroup", "--root", root, "--manifest", manifestPath,
		"--task-id", taskID, "--task", taskPath, "--revise-assignment", assignmentID,
		"--revision-intent-key", intent, "--expected-record-ref", string(refBytes), "--expected-record-sha256", previousSHA,
	}, nil, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func decodeS6RevisionOutput(t *testing.T, output string) s6RevisionCLIOutput {
	t.Helper()
	var decoded s6RevisionCLIOutput
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("decode revision CLI output: %v\n%s", err, output)
	}
	if decoded.State == nil || decoded.Receipt.AssignmentRecord.Revision == 0 {
		t.Fatalf("revision CLI omitted current snapshot or durable receipt: %s", output)
	}
	return decoded
}

func assertS6RevisionReceiptRejectsInvalidJournal(t *testing.T, state map[string]any, eventID, journalPath string) {
	t.Helper()
	if err := loopruntime.ValidateAssignmentRevisionReceipts(state, nil); err == nil {
		t.Fatal("revision Record was accepted without its original journal event")
	}
	data, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	found := false
	for i, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event["event_id"] != eventID {
			continue
		}
		found = true
		event["transition_id"] = "WRONG-REVISION-EVENT"
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		lines[i] = string(encoded)
	}
	if !found {
		t.Fatalf("original revision event %s not found in journal", eventID)
	}
	if err := loopruntime.ValidateAssignmentRevisionReceipts(state, []byte(strings.Join(lines, "\n")+"\n")); err == nil {
		t.Fatal("revision Record was accepted with a journal event from the wrong transition")
	}
}

func s6SyntheticCommitPending(t *testing.T, previousStateBytes, targetStateBytes, targetJournalBytes []byte) []byte {
	t.Helper()
	var previousState map[string]any
	if err := json.Unmarshal(previousStateBytes, &previousState); err != nil {
		t.Fatal(err)
	}
	var targetState map[string]any
	if err := json.Unmarshal(targetStateBytes, &targetState); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(targetJournalBytes)), "\n")
	if len(lines) == 0 || lines[len(lines)-1] == "" {
		t.Fatal("target journal has no committed event")
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &event); err != nil {
		t.Fatal(err)
	}
	eventBytes, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	previousHash := s6StateHash(t, previousState)
	targetHash := s6StateHash(t, targetState)
	eventHash := s6SHA256(append(eventBytes, '\n'))
	marker := map[string]any{
		"schema_version": "1.0.0", "previous_state_sha256": previousHash,
		"previous_revision": previousState["revision"], "state_sha256": targetHash,
		"journal_event_sha256": eventHash, "request_id": event["request_id"],
		"idempotency_key": event["idempotency_key"], "retain_last_transition": false,
		"state": targetState, "journal_event": event,
	}
	encoded, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(encoded, '\n')
}

func s6StateHash(t *testing.T, state map[string]any) string {
	t.Helper()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return s6SHA256(append(data, '\n'))
}

func s6SHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func readS6RevisionPair(t *testing.T, statePath, journalPath string) ([]byte, []byte, error) {
	t.Helper()
	state, err := os.ReadFile(statePath)
	if err != nil {
		return nil, nil, err
	}
	journal, err := os.ReadFile(journalPath)
	if err != nil {
		return nil, nil, err
	}
	return state, journal, nil
}

func assertS6RevisionPairUnchanged(t *testing.T, statePath, journalPath string, expectedState, expectedJournal []byte) {
	t.Helper()
	state, journal, err := readS6RevisionPair(t, statePath, journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(state, expectedState) || !bytes.Equal(journal, expectedJournal) {
		t.Fatal("rejected or exact-replay revision request changed the Runtime pair")
	}
}

func assertS6RevisionPairMatchesSnapshot(t *testing.T, stateBytes, journalBytes []byte, targetState, targetJournal []byte) {
	t.Helper()
	var gotState, wantState map[string]any
	if err := json.Unmarshal(stateBytes, &gotState); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(targetState, &wantState); err != nil {
		t.Fatal(err)
	}
	var gotEvents, wantEvents []map[string]any
	for _, input := range [][]byte{journalBytes, targetJournal} {
		var events []map[string]any
		for _, line := range strings.Split(strings.TrimSpace(string(input)), "\n") {
			var event map[string]any
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				t.Fatal(err)
			}
			events = append(events, event)
		}
		if len(gotEvents) == 0 {
			gotEvents = events
		} else {
			wantEvents = events
		}
	}
	gotBytes, _ := json.Marshal(gotState)
	wantBytes, _ := json.Marshal(wantState)
	gotEventBytes, _ := json.Marshal(gotEvents)
	wantEventBytes, _ := json.Marshal(wantEvents)
	if !bytes.Equal(gotBytes, wantBytes) || !bytes.Equal(gotEventBytes, wantEventBytes) {
		t.Fatal("Store recovery did not reconstruct the exact recorded candidate and event")
	}
}

func mustReadS6(t *testing.T, root, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func s6AgentRow(t *testing.T, state map[string]any, agentID string) map[string]any {
	t.Helper()
	for _, raw := range state["entities"].(map[string]any)["agents"].([]any) {
		row, _ := raw.(map[string]any)
		if row["id"] == agentID {
			return row
		}
	}
	t.Fatalf("Agent %s missing", agentID)
	return nil
}

func s6TaskRow(t *testing.T, state map[string]any, taskID string) map[string]any {
	t.Helper()
	for _, raw := range state["entities"].(map[string]any)["tasks"].([]any) {
		row, _ := raw.(map[string]any)
		if row["id"] == taskID {
			return row
		}
	}
	t.Fatalf("TASK %s missing", taskID)
	return nil
}

func s6ResultEvidenceSuperseded(state map[string]any, path, eventID string) bool {
	for _, raw := range state["evidence"].([]any) {
		row, _ := raw.(map[string]any)
		if row["kind"] == "completion_report" && row["path"] == path {
			return row["status"] == "superseded" && row["invalidated_by"] == eventID && row["invalidation_rule"] == "assignment_revision_issued"
		}
	}
	return false
}

func mustS6Records(t *testing.T, state map[string]any) []assignmentrecord.Record {
	t.Helper()
	records, err := assignmentrecord.RecordsFromState(state)
	if err != nil {
		t.Fatal(err)
	}
	return records
}
