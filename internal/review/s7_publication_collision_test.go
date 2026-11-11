package review

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

func TestReviewPlanWriterAllocatesAroundUnknownOccupant(t *testing.T) {
	for _, same := range []bool{false, true} {
		t.Run(map[bool]string{false: "different", true: "identical"}[same], func(t *testing.T) {
			root := t.TempDir()
			statePath, journalPath := writeState(t, root, baseVerificationState())
			answerPath := writePlanFile(t, root)
			answer, err := os.ReadFile(answerPath)
			if err != nil {
				t.Fatal(err)
			}
			var plan Plan
			if err := json.Unmarshal(answer, &plan); err != nil {
				t.Fatal(err)
			}
			candidate := boundedReviewArtifactPath([]string{".claude", "review", "plans"}, plan.ReviewPlanID)
			occupied := filepath.Join(root, filepath.FromSlash(candidate))
			if err := os.MkdirAll(filepath.Dir(occupied), 0755); err != nil {
				t.Fatal(err)
			}
			unknown := []byte("unknown private file")
			if same {
				unknown = append(canonicalJSON(answer), '\n')
			}
			if err := os.WriteFile(occupied, unknown, 0600); err != nil {
				t.Fatal(err)
			}
			snapshot, err := RegisterPlan(root, statePath, journalPath, PlanRequest{ExpectedRevision: 1, PlanPath: answerPath})
			if err != nil {
				t.Fatal(err)
			}
			ptr := PlanPointerFromState(snapshot.State)
			if ptr == nil || ptr.Path == candidate {
				t.Fatalf("claimed unknown path: %+v", ptr)
			}
			if got, err := os.ReadFile(occupied); err != nil || !bytes.Equal(got, unknown) {
				t.Fatalf("unknown file changed: %q %v", got, err)
			}
			if got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ptr.Path))); err != nil || !bytes.Equal(got, append(canonicalJSON(answer), '\n')) {
				t.Fatalf("registered Plan lost original answer: %v", err)
			}
			if got, err := os.ReadFile(answerPath); err != nil || !bytes.Equal(got, answer) {
				t.Fatalf("author answer changed: %v", err)
			}
		})
	}
}

func TestReviewerWriterAllocatesUnoccupiedViewAndAnswer(t *testing.T) {
	for _, target := range []string{"view", "answer"} {
		t.Run(target, func(t *testing.T) {
			root, statePath, journalPath, before, _ := packetModeFixture(t)
			plan, ptr, err := loadResultPacketPlan(root, before.State, true)
			if err != nil {
				t.Fatal(err)
			}
			a := findPlanAssignment(plan, "assignment-qa-1")
			packet, err := buildCurrentResultPacket(root, before.State, plan, ptr, a, "agent-qa-1")
			if err != nil {
				t.Fatal(err)
			}
			path := packet.ViewPath
			if target == "answer" {
				path = packet.AnswerPath
			}
			occupied := filepath.Join(root, filepath.FromSlash(path))
			if err := os.MkdirAll(filepath.Dir(occupied), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(occupied, []byte("unknown private file"), 0600); err != nil {
				t.Fatal(err)
			}
			packets, err := IssueResultPackets(root, statePath, journalPath, before.Revision, "agent-qa-1")
			if err != nil || len(packets) != 1 {
				t.Fatalf("issue: %v %v", packets, err)
			}
			issued := packets[0]
			if issued.ViewPath == packet.ViewPath || issued.AnswerPath == packet.AnswerPath {
				t.Fatalf("did not allocate a distinct delivery: %+v", issued)
			}
			if data, err := os.ReadFile(occupied); err != nil || string(data) != "unknown private file" {
				t.Fatalf("unknown occupant changed: %q %v", data, err)
			}
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(issued.AnswerPath))); err != nil {
				t.Fatal(err)
			}
			state, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			again, err := IssueResultPackets(root, statePath, journalPath, state.Revision, "agent-qa-1")
			if err != nil || len(again) != 1 || again[0].AnswerPath != issued.AnswerPath || again[0].ViewPath != issued.ViewPath {
				t.Fatalf("registered delivery did not replay: %+v %v", again, err)
			}
		})
	}
}

func TestS7IssuedPacketReplayReturnsRegisteredPaths(t *testing.T) {
	root, statePath, journalPath, before, _ := packetModeFixture(t)
	first, err := IssueResultPackets(root, statePath, journalPath, before.Revision, "agent-qa-1")
	if err != nil || len(first) != 1 {
		t.Fatalf("first: %v %v", first, err)
	}
	current, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	again, err := IssueResultPackets(root, statePath, journalPath, current.Revision, "agent-qa-1")
	if err != nil || len(again) != 1 {
		t.Fatalf("replay: %v %v", again, err)
	}
	firstJSON, _ := json.Marshal(first[0])
	againJSON, _ := json.Marshal(again[0])
	if !bytes.Equal(firstJSON, againJSON) {
		t.Fatalf("replay differs:\nfirst %s\nagain %s", firstJSON, againJSON)
	}
	after, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
	if err != nil || after.Revision != current.Revision {
		t.Fatalf("replay wrote registry: %v %+v", err, after)
	}
}

func TestNaturalS7ReceivesOriginalPlanAndReviewerAroundUnknownFiles(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, packetModeVerificationState())
	answerPath := writePlanFile(t, root)
	var plan Plan
	if err := json.Unmarshal(mustReadTestFile(t, answerPath), &plan); err != nil {
		t.Fatal(err)
	}
	claim := plan.Claims[1]
	claim.Lens, claim.Applicability, claim.Method, claim.SourceRefs = "", "", "", nil
	claim.Assertion = "the intended operation remains observable"
	assignment := plan.Assignments[1]
	assignment.ClaimIDs = []string{claim.ClaimID}
	plan.Claims, plan.Assignments, plan.FrozenSubjects, plan.E2ECoverageState = []Claim{claim}, []PlanAssignment{assignment}, nil, ""
	data, _ := json.Marshal(plan)
	if err := os.WriteFile(answerPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	planCandidate := boundedReviewArtifactPath([]string{".claude", "review", "plans"}, plan.ReviewPlanID)
	planOccupant := filepath.Join(root, filepath.FromSlash(planCandidate))
	if err := os.MkdirAll(filepath.Dir(planOccupant), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planOccupant, []byte("unknown plan file"), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, outcome, err := ReceivePlanAnswer(root, statePath, journalPath, PlanRequest{ExpectedRevision: -1, PlanPath: answerPath})
	if err != nil || outcome != PlanReceptionAccepted {
		t.Fatalf("natural Plan reception: %s %v", outcome, err)
	}
	if ptr := PlanPointerFromState(snapshot.State); ptr == nil || ptr.Path == planCandidate {
		t.Fatalf("Plan claimed unknown file: %+v", ptr)
	}
	snapshot = markDispatched(t, root, statePath, journalPath, snapshot, assignment.AssignmentID, "agent-qa-1")
	seedResultMethodSources(t, root, snapshot.State, &plan)
	for _, raw := range snapshot.State["entities"].(map[string]any)["agents"].([]any) {
		agent := raw.(map[string]any)
		if agent["id"] == "agent-qa-1" {
			agent["state"], agent["dispatch_mode"] = "reading", "plan_checkpoint"
		}
	}
	writeMethodState(t, statePath, snapshot.State)
	currentPlan, ptr, err := loadResultPacketPlan(root, snapshot.State, true)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := buildCurrentResultPacket(root, snapshot.State, currentPlan, ptr, findPlanAssignment(currentPlan, assignment.AssignmentID), "agent-qa-1")
	if err != nil {
		t.Fatal(err)
	}
	answerOccupant := filepath.Join(root, filepath.FromSlash(packet.AnswerPath))
	if err := os.MkdirAll(filepath.Dir(answerOccupant), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(answerOccupant, []byte("unknown answer file"), 0600); err != nil {
		t.Fatal(err)
	}
	packets, err := IssueResultPackets(root, statePath, journalPath, -1, "agent-qa-1")
	if err != nil || len(packets) != 1 || packets[0].AnswerPath == packet.AnswerPath {
		t.Fatalf("issue around unknown: %+v %v", packets, err)
	}
	packet = packets[0]
	packet.Draft.Answer = json.RawMessage(`{"verdict":"pass"}`)
	data, _ = json.Marshal(packet.Draft)
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(packet.AnswerPath)), data, 0600); err != nil {
		t.Fatal(err)
	}
	accepted, err := SubmitIssuedResult(root, statePath, journalPath, IssuedSubmitRequest{ExpectedRevision: -1, PacketID: packet.ID, AnswerPath: packet.AnswerPath, OwnerAgentID: packet.Binding.OwnerAgentID})
	if err != nil || !RequiredResponsibilitiesComplete(accepted.State) {
		t.Fatalf("natural Reviewer reception: %v", err)
	}
	for file, expected := range map[string]string{planOccupant: "unknown plan file", answerOccupant: "unknown answer file"} {
		if got, err := os.ReadFile(file); err != nil || string(got) != expected {
			t.Fatalf("unknown file changed: %q %v", got, err)
		}
	}
}

func TestReviewPlanWriterSkipsOccupiedCandidateParent(t *testing.T) {
	root := t.TempDir()
	statePath, journalPath := writeState(t, root, baseVerificationState())
	answerPath := writePlanFile(t, root)
	parent := filepath.Join(root, ".claude", "review", "plans")
	if err := os.MkdirAll(filepath.Dir(parent), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(parent, []byte("unknown parent file"), 0600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := RegisterPlan(root, statePath, journalPath, PlanRequest{ExpectedRevision: 1, PlanPath: answerPath})
	if err != nil {
		t.Fatal(err)
	}
	ptr := PlanPointerFromState(snapshot.State)
	if ptr == nil || !strings.Contains(ptr.Path, "plans-candidate-") {
		t.Fatalf("Plan did not use an alternate directory: %+v", ptr)
	}
	if got, err := os.ReadFile(parent); err != nil || string(got) != "unknown parent file" {
		t.Fatalf("unknown parent changed: %q %v", got, err)
	}
}

func TestReviewerWriterSkipsOccupiedCandidateParent(t *testing.T) {
	for _, target := range []string{"view", "answer"} {
		t.Run(target, func(t *testing.T) {
			root, statePath, journalPath, before, _ := packetModeFixture(t)
			plan, ptr, err := loadResultPacketPlan(root, before.State, true)
			if err != nil {
				t.Fatal(err)
			}
			packet, err := buildCurrentResultPacket(root, before.State, plan, ptr, findPlanAssignment(plan, "assignment-qa-1"), "agent-qa-1")
			if err != nil {
				t.Fatal(err)
			}
			candidate := packet.ViewPath
			if target == "answer" {
				candidate = packet.AnswerPath
			}
			parent := filepath.Dir(filepath.Join(root, filepath.FromSlash(candidate)))
			if err := os.MkdirAll(filepath.Dir(parent), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(parent, []byte("unknown parent file"), 0600); err != nil {
				t.Fatal(err)
			}
			packets, err := IssueResultPackets(root, statePath, journalPath, before.Revision, "agent-qa-1")
			if err != nil || len(packets) != 1 || packets[0].AnswerPath == packet.AnswerPath {
				t.Fatalf("issue alternate delivery: %+v %v", packets, err)
			}
			if got, err := os.ReadFile(parent); err != nil || string(got) != "unknown parent file" {
				t.Fatalf("unknown parent changed: %q %v", got, err)
			}
		})
	}
}
