package runtime_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func packetFixture(t *testing.T) (string, string, string, workpacket.Packet) {
	t.Helper()
	root := t.TempDir()
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	writeExampleRuntime(t, statePath)
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	state["schema_version"] = "1.2.0"
	state["lifecycle"] = map[string]any{"state": "document_verification", "phase": nil, "phase_revision": 7}
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": true}
	manifest := []byte(fmt.Sprintf(`{"runtime_id":%q,"baseline_generation":1,"workgroup_id":"team-dv","workgroup_kind":"document_verifier","assignments":[{"assignment_id":"assignment-spec","agent_id":"reviewer-spec","responsibility_id":"DV-SPEC-CONSISTENCY","role_family":"document-verifier"}]}`, state["runtime_id"]))
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), manifest, 0600); err != nil {
		t.Fatal(err)
	}
	input := []byte("# REQ\n> Status: locked\n")
	if err := os.WriteFile(filepath.Join(root, "req.md"), input, 0600); err != nil {
		t.Fatal(err)
	}
	ref := workpacket.Reference{ID: "REQ-039", Path: "req.md", Version: "v1.0.0", SHA256: workpacket.Digest(input)}
	state["bound_req"] = map[string]any{"id": ref.ID, "path": ref.Path, "version": ref.Version, "sha256": ref.SHA256, "status": "locked", "approved_by": "user", "approved_at": "2026-10-13T00:00:00Z", "metadata": map[string]any{"ui_impact": "none"}}
	state["documents"] = []any{map[string]any{"id": ref.ID, "path": ref.Path, "kind": "req", "version": ref.Version, "sha256": ref.SHA256, "status": "registered", "generation": 1}}
	entities := state["entities"].(map[string]any)
	entities["agents"] = []any{map[string]any{"id": "reviewer-spec", "role": "document-verifier", "state": "working", "task_ids": []any{}, "team_id": "team-dv", "definition_ref": "agents/document-verifier.md", "prompt_ref": "manifest.json#assignment-spec", "readback_ref": nil, "activation_ref": nil, "activation_revision": nil, "updated_at": "2026-10-13T00:00:00Z"}}
	entities["teams"] = []any{map[string]any{"id": "team-dv", "platform_team_id": "platform-dv", "kind": "document_verifier", "status": "active", "manifest_ref": "manifest.json", "responsibility_ids": []any{"DV-SPEC-CONSISTENCY"}, "agent_ids": []any{"reviewer-spec"}, "review_round": nil, "packet_stage_entry": 7, "packet_generation": 1}}
	data, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, data, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := workpacket.BuildS5(workpacket.Binding{RuntimeID: state["runtime_id"].(string), Generation: 1, StageEntry: 7, Owner: "reviewer-spec", AssignmentID: "assignment-spec", AssignmentSHA: workpacket.Digest(manifest), Responsibility: "DV-SPEC-CONSISTENCY", Attempt: 1}, []workpacket.Reference{ref})
	if err != nil {
		t.Fatal(err)
	}
	return root, statePath, journalPath, p
}

type simultaneousPacketReads struct {
	fileview.Reader
	reads atomic.Int32
	ready chan struct{}
}

func (r *simultaneousPacketReads) ReadFile(path string) ([]byte, error) {
	n := r.reads.Add(1)
	if n == 2 {
		close(r.ready)
	}
	if n <= 2 {
		<-r.ready
	}
	return r.Reader.ReadFile(path)
}

func TestConcurrentDocumentReceptionIsUnique(t *testing.T) {
	for _, different := range []bool{false, true} {
		t.Run(map[bool]string{false: "identical", true: "content conflict"}[different], func(t *testing.T) {
			root, statePath, journalPath, p := packetFixture(t)
			if _, err := runtime.RecordWorkPacket(root, statePath, journalPath, p, fileview.Disk{Root: root}, testCandidateValidator()); err != nil {
				t.Fatal(err)
			}
			files := &simultaneousPacketReads{Reader: fileview.Disk{Root: root}, ready: make(chan struct{})}
			requests := make([]runtime.EvidenceRequest, 2)
			for i := range 2 {
				answer := passAnswer()
				path := "review.json"
				if different && i == 1 {
					answer = fixAnswer(p)
					path = "other-review.json"
				}
				requests[i] = submissionFixture(t, root, p, path, answer)
				requests[i].Submission.Files = files
			}
			errs := make([]error, 2)
			var group sync.WaitGroup
			for i := range 2 {
				group.Add(1)
				go func(i int) {
					defer group.Done()
					_, errs[i] = runtime.RecordEvidence(root, statePath, journalPath, requests[i])
				}(i)
			}
			group.Wait()
			accepted, conflicts := 0, 0
			for _, err := range errs {
				if err == nil {
					accepted++
				} else if errors.Is(err, runtime.ErrSubmissionConflict) {
					conflicts++
				} else {
					t.Fatalf("unexpected concurrent result: %v", err)
				}
			}
			if (!different && accepted != 2) || (different && (accepted != 1 || conflicts != 1)) {
				t.Fatalf("accepted=%d conflicts=%d", accepted, conflicts)
			}
			snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if len(snapshot.State["evidence"].([]any)) != 1 || len(snapshot.State["entities"].(map[string]any)["submissions"].([]any)) != 1 {
				t.Fatal("duplicate domain commit")
			}
		})
	}
}

func TestWorkPacketIssuanceIsDurableAndIdempotent(t *testing.T) {
	root, statePath, journalPath, p := packetFixture(t)
	first, err := runtime.RecordWorkPacket(root, statePath, journalPath, p, fileview.Disk{Root: root}, testCandidateValidator())
	if err != nil {
		t.Fatal(err)
	}
	second, err := runtime.RecordWorkPacket(root, statePath, journalPath, p, fileview.Disk{Root: root}, testCandidateValidator())
	if err != nil {
		t.Fatal(err)
	}
	if second.Revision != first.Revision {
		t.Fatal("replay appended a new issuance")
	}
	if len(second.State["entities"].(map[string]any)["work_packets"].([]any)) != 1 {
		t.Fatal("duplicate issuance")
	}
}

func TestWorkPacketRejectsUnregisteredOwnerAndInputDrift(t *testing.T) {
	for _, mutation := range []string{"owner", "stage", "input", "manifest"} {
		t.Run(mutation, func(t *testing.T) {
			root, statePath, journalPath, p := packetFixture(t)
			switch mutation {
			case "owner":
				p.Binding.Owner = "other"
			case "stage":
				p.Binding.StageEntry++
			case "input":
				if err := os.WriteFile(filepath.Join(root, "req.md"), []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			case "manifest":
				if err := os.WriteFile(filepath.Join(root, "manifest.json"), []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mutation == "owner" || mutation == "stage" {
				var err error
				p, err = workpacket.BuildS5(p.Binding, p.Inputs)
				if err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := runtime.RecordWorkPacket(root, statePath, journalPath, p, fileview.Disk{Root: root}, testCandidateValidator()); err == nil {
				t.Fatal("invalid packet accepted")
			}
			after, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("rejected issuance changed runtime")
			}
		})
	}
}

func TestWorkPacketCannotIssueAnotherAttemptWithoutRevision(t *testing.T) {
	root, statePath, journalPath, p := packetFixture(t)
	if _, err := runtime.RecordWorkPacket(root, statePath, journalPath, p, fileview.Disk{Root: root}, testCandidateValidator()); err != nil {
		t.Fatal(err)
	}
	p.Binding.Attempt++
	p.Binding.PreviousSubmissionKey = workpacket.Digest([]byte("submission:" + p.ID))
	p.Binding.RevisionReason = "recheck a concrete observation"
	p, err := workpacket.BuildS5(p.Binding, p.Inputs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.RecordWorkPacket(root, statePath, journalPath, p, fileview.Disk{Root: root}, testCandidateValidator()); err == nil {
		t.Fatal("new attempt issued without a trusted revision event")
	}
}

func TestDocumentAnswerReceptionUsesStableIdentity(t *testing.T) {
	root, statePath, journalPath, p := packetFixture(t)
	if _, err := runtime.RecordWorkPacket(root, statePath, journalPath, p, fileview.Disk{Root: root}, testCandidateValidator()); err != nil {
		t.Fatal(err)
	}
	request := submissionFixture(t, root, p, "review.json", passAnswer())
	first, err := runtime.RecordEvidence(root, statePath, journalPath, request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runtime.RecordEvidence(root, statePath, journalPath, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != second.Revision {
		t.Fatal("repeated answer appended evidence")
	}
	rows := second.State["entities"].(map[string]any)["submissions"].([]any)
	if len(rows) != 1 || len(second.State["evidence"].([]any)) != 1 {
		t.Fatal("domain and reception were not one commit")
	}
	row := rows[0].(map[string]any)
	if row["control_status"] != "pending" || row["followup_event_id"] == "" {
		t.Fatal("recovery event was not persisted with domain reception")
	}
	canonicalHash, err := workpacket.AnswerDigest(passAnswer())
	if err != nil || row["content_sha256"] != canonicalHash {
		t.Fatalf("stored digest is not the validated answer: %v", row)
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, request.Submission.AnswerJSON, "", "  "); err != nil {
		t.Fatal(err)
	}
	request.Submission.AnswerJSON = pretty.Bytes()
	replay, err := runtime.RecordEvidence(root, statePath, journalPath, request)
	if err != nil || replay.Revision != first.Revision {
		t.Fatalf("format-only answer change was not idempotent: %v", err)
	}
	draft := p.Draft
	draft.Answer = fixAnswer(p)
	request.Submission.AnswerJSON, err = json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.RecordEvidence(root, statePath, journalPath, request); !errors.Is(err, runtime.ErrSubmissionConflict) {
		t.Fatalf("same identity accepted different content: %v", err)
	}
}

func passAnswer() workpacket.Answer {
	conclusion := "pass"
	return workpacket.Answer{Conclusion: &conclusion, Findings: []workpacket.Finding{}}
}

func fixAnswer(p workpacket.Packet) workpacket.Answer {
	conclusion := "fix_required"
	return workpacket.Answer{Conclusion: &conclusion, Findings: []workpacket.Finding{{Path: p.Inputs[0].Path + "#checks", Expected: "executable checks", Observed: "missing check command"}}}
}

func submissionFixture(t *testing.T, root string, p workpacket.Packet, path string, answer workpacket.Answer) runtime.EvidenceRequest {
	t.Helper()
	draft := p.Draft
	draft.Answer = answer
	answerData, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	envelope := workpacket.DocumentReview(p, "ev-answer", answer, time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC))
	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, path), data, 0600); err != nil {
		t.Fatal(err)
	}
	// This low-level fixture isolates domain atomicity and answer binding.
	// Production adapters inject the exact source and any explicit approval check.
	return runtime.EvidenceRequest{ExpectedRevision: -1, ID: "ev-answer", Kind: "document_review", Path: path, ProducedBy: []string{p.Binding.Owner}, ResponsibilityID: p.Binding.Responsibility, Validator: testCandidateValidator(), Submission: &runtime.SubmissionRequest{PacketID: p.ID, Owner: p.Binding.Owner, AnswerJSON: answerData, Files: fileview.Disk{Root: root}, Authorize: func(string, map[string]any) error { return nil }}}
}

func TestDocumentSubmissionUsesIssuedQuestionnaireWithoutGenericPlan(t *testing.T) {
	root, statePath, journalPath, p := packetFixture(t)
	data, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	agents := state["entities"].(map[string]any)["agents"].([]any)
	agent := agents[0].(map[string]any)
	agent["state"] = "reading"
	agent["dispatch_mode"] = "plan_checkpoint"
	agent["plan_reported_ref"] = nil
	data, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.RecordWorkPacket(root, statePath, journalPath, p, fileview.Disk{Root: root}, testCandidateValidator()); err != nil {
		t.Fatal(err)
	}
	request := submissionFixture(t, root, p, "review.json", passAnswer())
	result, err := runtime.RecordEvidence(root, statePath, journalPath, request)
	if err != nil {
		t.Fatalf("current issued S5 answer required a generic PLAN checkpoint: %v", err)
	}
	if record, found, err := runtime.FindSubmission(result.State, p.ID); err != nil || !found || record.Owner != p.Binding.Owner {
		t.Fatalf("original S5 consumer did not record the answer: %+v found=%v err=%v", record, found, err)
	}
}

func TestDocumentSubmissionRejectsInvalidAnswerAndEnvelopeWithoutConsumingAttempt(t *testing.T) {
	for _, change := range []string{"missing answer", "draft", "unknown answer", "duplicate answer", "missing findings", "incomplete finding", "modified question", "wrong caller hash", "wrong conclusion", "wrong findings", "missing subjects", "extra subjects", "duplicate subjects", "wrong subject hash", "wrong subject version", "wrong subject path", "wrong schema", "wrong evidence id", "wrong runtime", "wrong generation", "wrong producer", "wrong responsibility", "wrong event", "review round", "registration review round", "unknown envelope", "duplicate envelope"} {
		t.Run(change, func(t *testing.T) {
			root, statePath, journalPath, p := packetFixture(t)
			before, err := runtime.RecordWorkPacket(root, statePath, journalPath, p, fileview.Disk{Root: root}, testCandidateValidator())
			if err != nil {
				t.Fatal(err)
			}
			valid := submissionFixture(t, root, p, "review.json", passAnswer())
			request := valid
			submission := *valid.Submission
			request.Submission = &submission
			draft := p.Draft
			draft.Answer = passAnswer()
			data, err := os.ReadFile(filepath.Join(root, "review.json"))
			if err != nil {
				t.Fatal(err)
			}
			var envelope map[string]any
			if err := json.Unmarshal(data, &envelope); err != nil {
				t.Fatal(err)
			}
			subjects := envelope["subject_refs"].([]any)
			switch change {
			case "missing answer":
				submission.AnswerJSON = nil
			case "draft":
				draft.Answer.Conclusion = nil
			case "unknown answer":
				submission.AnswerJSON = bytes.Replace(submission.AnswerJSON, []byte(`"answer":{`), []byte(`"answer":{"producer":"other",`), 1)
			case "duplicate answer":
				submission.AnswerJSON = bytes.Replace(submission.AnswerJSON, []byte(`"conclusion":"pass"`), []byte(`"conclusion":"fix_required","conclusion":"pass"`), 1)
			case "missing findings":
				submission.AnswerJSON = bytes.Replace(submission.AnswerJSON, []byte(`,"findings":[]`), nil, 1)
			case "incomplete finding":
				draft.Answer = fixAnswer(p)
				draft.Answer.Findings[0].Observed = ""
			case "modified question":
				draft.Title = "other responsibility"
			case "wrong caller hash":
				submission.ContentHash = workpacket.Digest([]byte("caller supplied"))
			case "wrong conclusion":
				envelope["conclusion"] = "fix_required"
			case "wrong findings":
				envelope["findings"] = fixAnswer(p).Findings
			case "missing subjects":
				envelope["subject_refs"] = []any{}
			case "extra subjects":
				envelope["subject_refs"] = append(subjects, map[string]any{"path": "other.md", "version": "v1", "sha256": p.Inputs[0].SHA256})
			case "duplicate subjects":
				envelope["subject_refs"] = append(subjects, subjects[0])
			case "wrong subject hash":
				subjects[0].(map[string]any)["sha256"] = workpacket.Digest([]byte("wrong"))
			case "wrong subject version":
				subjects[0].(map[string]any)["version"] = "v2"
			case "wrong subject path":
				subjects[0].(map[string]any)["path"] = "other.md"
			case "wrong schema":
				envelope["schema_version"] = "9.0.0"
			case "wrong evidence id":
				envelope["evidence_id"] = "different-evidence"
			case "wrong runtime":
				envelope["runtime_id"] = "loop-other"
			case "wrong generation":
				envelope["baseline_generation"] = 2
			case "wrong producer":
				envelope["producer_agent_id"] = "other"
			case "wrong responsibility":
				envelope["producer_responsibility"] = "DV-TASK-EXECUTABILITY"
			case "wrong event":
				envelope["requested_event"] = "document_fix_required"
			case "review round":
				envelope["review_round"] = 0
			case "registration review round":
				round := 1
				request.ReviewRound = &round
			case "unknown envelope":
				envelope["unregistered"] = true
			case "duplicate envelope": // preserve duplicate keys in the serialized envelope below
			}
			if change == "draft" || change == "modified question" || change == "incomplete finding" {
				submission.AnswerJSON, err = json.Marshal(draft)
				if err != nil {
					t.Fatal(err)
				}
			}
			data, err = json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			if change == "duplicate envelope" {
				data = bytes.Replace(data, []byte(`"conclusion":"pass"`), []byte(`"conclusion":"fix_required","conclusion":"pass"`), 1)
			}
			if err := os.WriteFile(filepath.Join(root, "review.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := runtime.RecordEvidence(root, statePath, journalPath, request); err == nil {
				t.Fatal("invalid submission consumed an immutable attempt")
			}
			after, err := runtime.NewStore(statePath, journalPath).Snapshot()
			if err != nil || after.Revision != before.Revision || len(after.State["evidence"].([]any)) != 0 {
				t.Fatalf("rejection changed authoritative state: %v", err)
			}
			valid = submissionFixture(t, root, p, "review.json", passAnswer())
			if _, err := runtime.RecordEvidence(root, statePath, journalPath, valid); err != nil {
				t.Fatalf("rejected input occupied the attempt: %v", err)
			}
		})
	}
}

func TestDocumentSubmissionKeepsProfessionalFailureAndHumanRoute(t *testing.T) {
	for _, conclusion := range []string{"fix_required", "req_change_required"} {
		t.Run(conclusion, func(t *testing.T) {
			root, statePath, journalPath, p := packetFixture(t)
			if _, err := runtime.RecordWorkPacket(root, statePath, journalPath, p, fileview.Disk{Root: root}, testCandidateValidator()); err != nil {
				t.Fatal(err)
			}
			answer := fixAnswer(p)
			answer.Conclusion = &conclusion
			request := submissionFixture(t, root, p, "review.json", answer)
			result, err := runtime.RecordEvidence(root, statePath, journalPath, request)
			if err != nil {
				t.Fatal(err)
			}
			if result.State["lifecycle"].(map[string]any)["state"] != "document_verification" {
				t.Fatal("domain reception inferred a lifecycle or human decision")
			}
			data, err := os.ReadFile(filepath.Join(root, "review.json"))
			if err != nil {
				t.Fatal(err)
			}
			var envelope workpacket.DocumentReviewEnvelope
			if err := json.Unmarshal(data, &envelope); err != nil {
				t.Fatal(err)
			}
			if conclusion == "req_change_required" && envelope.RequestedEvent != "" {
				t.Fatal("professional requirement-change judgment forged an automatic event")
			}
			if conclusion == "fix_required" && envelope.RequestedEvent != "document_fix_required" {
				t.Fatal("document fix lost the original routing event")
			}
		})
	}
}

type driftOnCommitReads struct {
	fileview.Disk
	reads int
}

func (r *driftOnCommitReads) ReadFile(path string) ([]byte, error) {
	r.reads++
	if r.reads == 2 {
		if err := os.WriteFile(filepath.Join(r.Root, path), []byte("changed after preflight"), 0600); err != nil {
			return nil, err
		}
	}
	return r.Disk.ReadFile(path)
}

func TestDocumentSubmissionRechecksFrozenInputInsideWriter(t *testing.T) {
	root, statePath, journalPath, p := packetFixture(t)
	before, err := runtime.RecordWorkPacket(root, statePath, journalPath, p, fileview.Disk{Root: root}, testCandidateValidator())
	if err != nil {
		t.Fatal(err)
	}
	request := submissionFixture(t, root, p, "review.json", passAnswer())
	reader := &driftOnCommitReads{Disk: fileview.Disk{Root: root}}
	request.Submission.Files = reader
	if _, err := runtime.RecordEvidence(root, statePath, journalPath, request); err == nil {
		t.Fatal("accepted a source that drifted after preflight")
	}
	if reader.reads < 2 {
		t.Fatalf("missing locked validation after preflight, got %d reads", reader.reads)
	}
	after, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil || after.Revision != before.Revision || len(after.State["evidence"].([]any)) != 0 {
		t.Fatalf("failed locked validation recorded reception: %v", err)
	}
}

func TestDocumentSubmissionReplayCannotReplaceDomainArtifact(t *testing.T) {
	for _, change := range []string{"path", "timestamp", "judgment"} {
		t.Run(change, func(t *testing.T) {
			root, statePath, journalPath, p := packetFixture(t)
			if _, err := runtime.RecordWorkPacket(root, statePath, journalPath, p, fileview.Disk{Root: root}, testCandidateValidator()); err != nil {
				t.Fatal(err)
			}
			request := submissionFixture(t, root, p, "review.json", passAnswer())
			first, err := runtime.RecordEvidence(root, statePath, journalPath, request)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(root, "review.json"))
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "path":
				request.Path = "other.json"
			case "timestamp":
				data = bytes.ReplaceAll(data, []byte("2026-10-13"), []byte("2026-10-14"))
			case "judgment":
				data = bytes.ReplaceAll(data, []byte(`"conclusion":"pass"`), []byte(`"conclusion":"fix_required"`))
			}
			if err := os.WriteFile(filepath.Join(root, request.Path), data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := runtime.RecordEvidence(root, statePath, journalPath, request); err == nil {
				t.Fatal("replay replaced its accepted domain artifact")
			}
			after, err := runtime.NewStore(statePath, journalPath).Snapshot()
			if err != nil || after.Revision != first.Revision {
				t.Fatalf("invalid replay changed state: %v", err)
			}
		})
	}
}
