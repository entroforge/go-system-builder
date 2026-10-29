package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/controlproof"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

type hypothesisPacketRuntimeFixture struct {
	root          string
	packet        workpacket.HypothesisResultPacket
	state         map[string]any
	journal       []byte
	registerEvent map[string]any
	issueEvent    map[string]any
}

func newHypothesisPacketRuntimeFixture(t *testing.T) hypothesisPacketRuntimeFixture {
	t.Helper()
	root := t.TempDir()
	const generation = 2
	const (
		runtimeID  = "loop-REQ-001"
		teamID     = "workgroup-s8-1"
		ownerID    = "investigator-01"
		caseID     = "investigation-case-001"
		batchID    = "observation-batch-001"
		findingID  = "finding-001"
		findingID2 = "finding-002"
		findingID3 = "finding-003"
		findingID4 = "finding-004"
		hypID      = "hypothesis-cache-1"
		assignID   = "assignment-investigator-1"
		taskID     = "TASK-S8-001"
		manifestID = "team-manifest-REQ-001-s8-r1"
	)
	const (
		caseRef     = ".claude/review/investigation/cases/investigation-case-001-r1.json"
		batchRef    = ".claude/evidence/loop-REQ-001/g2/review/observation-batch-001.json"
		findingRef  = ".claude/evidence/loop-REQ-001/g2/findings/finding-001.json"
		findingRef2 = ".claude/evidence/loop-REQ-001/g2/findings/finding-002.json"
		findingRef3 = ".claude/evidence/loop-REQ-001/g2/findings/finding-003.json"
		findingRef4 = ".claude/evidence/loop-REQ-001/g2/findings/finding-004.json"
		taskRef     = ".claude/tasks/TASK-S8-001.json"
		manifestRef = ".claude/workgroups/workgroup-s8-1.json"
	)
	write := func(ref string, value any) []byte {
		t.Helper()
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, '\n')
		path := filepath.Join(root, filepath.FromSlash(ref))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return data
	}

	indexedQuestionRef := "EVID-S8-HYPOTHESIS-001"
	indexedFindingRef := "EVID-S8-FINDING-001"
	indexedFindingRef2 := "EVID-S8-FINDING-002"
	indexedFindingRef3 := "EVID-S8-FINDING-003"
	indexedFindingRef4 := "EVID-S8-FINDING-004"
	questionEvidencePath := ".claude/evidence/loop-REQ-001/g2/S8/hypothesis.json"
	findingEvidencePath := ".claude/evidence/loop-REQ-001/g2/S8/finding.json"
	findingEvidencePath2 := ".claude/evidence/loop-REQ-001/g2/S8/finding-002.json"
	findingEvidencePath3 := ".claude/evidence/loop-REQ-001/g2/S8/finding-003.json"
	findingEvidencePath4 := ".claude/evidence/loop-REQ-001/g2/S8/finding-004.json"
	questionEvidence := write(questionEvidencePath, map[string]any{"kind": "experiment", "source": "registered-runtime-evidence"})
	findingEvidence := write(findingEvidencePath, map[string]any{"kind": "observation", "source": "registered-runtime-evidence"})
	findingEvidence2 := write(findingEvidencePath2, map[string]any{"kind": "observation", "source": "registered-runtime-evidence-002"})
	findingEvidence3 := write(findingEvidencePath3, map[string]any{"kind": "observation", "source": "registered-runtime-evidence-003"})
	findingEvidence4 := write(findingEvidencePath4, map[string]any{"kind": "observation", "source": "registered-runtime-evidence-004"})

	hypothesis := workpacket.HypothesisResultQuestion{
		HypothesisID: hypID, AssignmentID: assignID,
		Statement:     "A stale cache entry causes the observed response.",
		Invariant:     "A fresh read must reflect the persisted value.",
		Discriminator: "Compare the same request with the cache bypassed.",
		ExpectedOutcomes: workpacket.HypothesisExpectedOutcomes{
			Support: "The stale response disappears when bypassing the cache.",
			Refute:  "The same stale response remains without the cache.",
		},
		SourceFindingIDs: []string{findingID}, EvidenceRefs: []string{indexedQuestionRef}, Status: "open",
	}
	caseRoute := workpacket.HypothesisResultCaseRoute{Status: "investigating"}
	routeSHA, err := workpacket.HypothesisCaseRouteDigest(caseRoute)
	if err != nil {
		t.Fatal(err)
	}

	finding := map[string]any{
		"schema_version": "1.0.0", "finding_id": findingID, "claim_id": "claim-cache-read", "lens": "qa", "severity": "P1",
		"expected": "A fresh read reflects the persisted value.", "authority_refs": []string{"REQ-001"},
		"observed": "The response contains the previously persisted value.", "observation_mode": "code_inspection",
		"encounter": map[string]any{
			"journey_summary": "Inspected the read path and found its first divergence.",
			"wall_action":     "Read the newly persisted record.", "first_bad_checkpoint": "The cache returned the older value.",
			"inspection_entry": "internal/cache/read.go", "symbol_trail": "Read -> Lookup -> stale entry",
		},
		"reproducibility": "always", "evidence_refs": []string{indexedFindingRef},
	}
	findingBytes := write(findingRef, finding)
	finding2 := s8CheckpointCloneMap(t, finding)
	finding2["finding_id"] = findingID2
	finding2["observed"] = "The second related boundary contains the same stale response."
	finding2["evidence_refs"] = []string{indexedFindingRef2}
	findingBytes2 := write(findingRef2, finding2)
	finding3 := s8CheckpointCloneMap(t, finding)
	finding3["finding_id"] = findingID3
	finding3["observed"] = "The third related boundary reproduces the stale response."
	finding3["evidence_refs"] = []string{indexedFindingRef3}
	findingBytes3 := write(findingRef3, finding3)
	finding4 := s8CheckpointCloneMap(t, finding)
	finding4["finding_id"] = findingID4
	finding4["observed"] = "The fourth related boundary reproduces the stale response."
	finding4["evidence_refs"] = []string{indexedFindingRef4}
	findingBytes4 := write(findingRef4, finding4)

	batch := map[string]any{
		"schema_version": "1.0.0", "observation_batch_id": batchID, "conclusion": "sealed", "evidence_id": batchID,
		"kind": "observation_batch", "runtime_id": runtimeID, "producer_agent_id": "round-consumer",
		"producer_responsibility": "Orchestrator", "review_plan_id": "review-plan-001", "review_round": 1,
		"baseline_generation": generation, "subject_digest": strings.Repeat("c", 64), "finding_ids": []string{findingID, findingID2, findingID3, findingID4},
		"drain_policy": "complete_required_claims",
		"claim_coverage_summary": map[string]any{
			"total_required": 4, "pass": 0, "finding": 4, "not_applicable": 0, "blocked": 0,
			"blocked_claims": []any{}, "plan_revision": 1,
		},
		"unobserved_claim_ids": []string{}, "original_finder_routes": []any{},
		"investigation_readiness": []any{map[string]any{"finding_id": findingID, "status": "ready"}},
		"sealed_at":               "2026-10-14T00:00:00Z", "sealed_by": "round-consumer", "revision": 1,
	}
	batchBytes := write(batchRef, batch)

	caseArtifact := map[string]any{
		"schema_version": "1.0.0", "case_id": caseID, "revision": 1, "status": "investigating",
		"source_finding_ids": []string{findingID, findingID2, findingID3, findingID4}, "observation_batch_id": batchID, "observation_batch_ref": batchRef,
		"observation_batch_sha256": workpacket.Digest(batchBytes), "baseline_generation": generation, "baseline_digest": strings.Repeat("c", 64),
		"grouping_rationale":      "The single sealed Finding is the provisional boundary.",
		"unexplained_finding_ids": []string{findingID, findingID2, findingID3, findingID4}, "failure_boundary_refs": []string{"finding-001.encounter.first_bad_checkpoint"},
		"cross_layer_trace": nil, "evidence_gaps": []string{}, "hypotheses": []any{hypothesis}, "hypothesis_results": []any{},
		"causal_model": nil, "primary_root_cause": nil, "blast_radius": nil, "detection_gap": nil,
		"route": nil, "route_reason": nil, "repair_contract_ref": nil,
	}
	caseBytes := write(caseRef, caseArtifact)

	task := map[string]any{"task_id": taskID, "case_id": caseID, "hypothesis_id": hypID, "assignment_id": assignID}
	taskBytes := write(taskRef, task)
	assignment := map[string]any{
		"assignment_id": assignID, "responsibility_id": "S8-CAUSAL-INVESTIGATION", "role_family": "investigator",
		"scope": []string{"case:" + caseID, "hypothesis:" + hypID}, "agent_id": ownerID,
		"agent_definition_ref": ".claude/agents/investigator.md", "skill_refs": []string{"s8-investigation"},
		"read_paths": []string{caseRef, batchRef, findingRef, findingRef2, findingRef3, findingRef4}, "write_paths": []string{}, "output_paths": []string{},
		"depends_on": []string{}, "reuse_decision": "create", "grouping_rationale": "Investigate the exact registered Hypothesis.", "status": "planned",
	}
	manifest := map[string]any{
		"schema_version": "1.0.0", "manifest_id": manifestID, "version": "v1.0.0", "runtime_id": runtimeID,
		"req_id": "REQ-001", "baseline_generation": generation, "review_round": 1, "platform_team_id": "platform-s8-1",
		"workgroup_id": teamID, "workgroup_kind": "investigator", "status": "active",
		"documents": []any{
			map[string]any{"id": "investigation-case", "path": caseRef, "version": "v1.0.0", "sha256": workpacket.Digest(caseBytes)},
			map[string]any{"id": taskID, "path": taskRef, "version": "v1.0.0", "sha256": workpacket.Digest(taskBytes)},
		},
		"risk_tags": []any{}, "responsibility_dispositions": []any{map[string]any{
			"responsibility_id": "S8-CAUSAL-INVESTIGATION", "disposition": "assigned", "trigger": "S8 hypothesis was registered",
			"assignment_ids": []string{assignID}, "na_rationale": nil, "evidence_ref": nil,
		}},
		"assignments": []any{assignment}, "separation_edges": []any{}, "planned_agent_count": 1, "max_parallel_agents": 1,
		"quantity_rationale": "One investigator owns the registered Hypothesis.",
		"validation":         map[string]any{"result": "pass", "missing_responsibilities": []any{}, "unresolved_conflicts": []any{}, "warnings": []any{}, "validated_at": "2026-10-14T00:00:00Z"},
	}
	manifestBytes := write(manifestRef, manifest)
	if err := schema.NewEmbeddedValidator().ValidateBytes("team-manifest.schema.json", manifestBytes); err != nil {
		t.Fatalf("investigator manifest fixture is invalid: %v", err)
	}
	var decodedManifest map[string]any
	if err := json.Unmarshal(manifestBytes, &decodedManifest); err != nil {
		t.Fatal(err)
	}
	assignmentMap := objectRows(decodedManifest["assignments"])[0]
	assignmentCanonical, err := json.Marshal(assignmentMap)
	if err != nil {
		t.Fatal(err)
	}

	baseline, round := generation, 1
	inputs := workpacket.HypothesisResultInputs{
		Hypothesis: hypothesis, CaseRoute: caseRoute,
		Findings: []workpacket.HypothesisResultFinding{
			{FindingID: findingID, Ref: findingRef, SHA256: workpacket.Digest(findingBytes), Document: json.RawMessage(findingBytes)},
			{FindingID: findingID2, Ref: findingRef2, SHA256: workpacket.Digest(findingBytes2), Document: json.RawMessage(findingBytes2)},
			{FindingID: findingID3, Ref: findingRef3, SHA256: workpacket.Digest(findingBytes3), Document: json.RawMessage(findingBytes3)},
			{FindingID: findingID4, Ref: findingRef4, SHA256: workpacket.Digest(findingBytes4), Document: json.RawMessage(findingBytes4)},
		},
		EvidenceRefs: []workpacket.HypothesisEvidenceRef{
			{Ref: indexedQuestionRef, Origin: "hypothesis", Attestation: "runtime_index", Kind: "investigation_probe", Path: questionEvidencePath, SHA256: workpacket.Digest(questionEvidence), Status: "valid", BaselineGeneration: &baseline, ReviewRound: &round},
			{Ref: indexedFindingRef, Origin: "finding", SourceFindingID: findingID, Attestation: "runtime_index", Kind: "finding_observation", Path: findingEvidencePath, SHA256: workpacket.Digest(findingEvidence), Status: "valid", BaselineGeneration: &baseline, ReviewRound: &round},
			{Ref: indexedFindingRef2, Origin: "finding", SourceFindingID: findingID2, Attestation: "runtime_index", Kind: "finding_observation", Path: findingEvidencePath2, SHA256: workpacket.Digest(findingEvidence2), Status: "valid", BaselineGeneration: &baseline, ReviewRound: &round},
			{Ref: indexedFindingRef3, Origin: "finding", SourceFindingID: findingID3, Attestation: "runtime_index", Kind: "finding_observation", Path: findingEvidencePath3, SHA256: workpacket.Digest(findingEvidence3), Status: "valid", BaselineGeneration: &baseline, ReviewRound: &round},
			{Ref: indexedFindingRef4, Origin: "finding", SourceFindingID: findingID4, Attestation: "runtime_index", Kind: "finding_observation", Path: findingEvidencePath4, SHA256: workpacket.Digest(findingEvidence4), Status: "valid", BaselineGeneration: &baseline, ReviewRound: &round},
		},
	}
	questionSHA, err := workpacket.HypothesisQuestionDigest(hypothesis)
	if err != nil {
		t.Fatal(err)
	}
	binding := workpacket.HypothesisResultBinding{
		RuntimeID: runtimeID, BaselineGeneration: generation, ReviewRound: 1,
		CaseID: caseID, CaseRef: caseRef, CaseSHA256: workpacket.Digest(caseBytes), CaseRevision: 1, CaseRouteSHA256: routeSHA,
		ObservationBatchID: batchID, ObservationBatchRef: batchRef, ObservationBatchSHA256: workpacket.Digest(batchBytes),
		BaselineDigest: strings.Repeat("c", 64), SourceFindingIDs: []string{findingID, findingID2, findingID3, findingID4}, HypothesisID: hypID,
		HypothesisQuestionSHA256: questionSHA, HypothesisSourceFindingIDs: []string{findingID},
		AssignmentID: assignID, AssignmentSHA256: workpacket.Digest(assignmentCanonical), TeamID: teamID,
		TaskID: taskID, TaskRef: taskRef, TaskSHA256: workpacket.Digest(taskBytes),
		ManifestRef: manifestRef, ManifestSHA256: workpacket.Digest(manifestBytes), OwnerAgentID: ownerID,
	}
	packet, err := workpacket.BuildHypothesisResult(binding, inputs)
	if err != nil {
		t.Fatalf("BuildHypothesisResult fixture: %v", err)
	}

	cursor := map[string]any{"state": "bug_resolution", "phase": "investigation"}
	registerEvent := buildJournalEvent(Mutation{
		EventID: "evt-register-" + teamID + "-r2", TransitionID: "ENTITY-REGISTER", Event: "transition_committed",
		Actor: "orchestrator", IdempotencyKey: "runtime:register:" + manifestID + ":1", RuntimeID: runtimeID,
		BaselineGeneration: generation, From: cursor, To: cursor, EvidenceIDs: []string{manifestRef, taskRef},
	}, runtimeID, 1, 2, 1, time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC))
	issueEvent := buildJournalEvent(Mutation{
		EventID: packet.IssuanceEventID, TransitionID: "PACKET-ISSUE", Event: "transition_committed",
		Actor: "orchestrator", IdempotencyKey: "packet:" + packet.ID, RuntimeID: runtimeID,
		BaselineGeneration: generation, From: cursor, To: cursor,
	}, runtimeID, 2, 3, 2, time.Date(2026, 10, 14, 0, 1, 0, 0, time.UTC))
	journal := s8CheckpointJournalBytes(t, registerEvent, issueEvent)
	packetBytes, err := json.Marshal(packet)
	if err != nil {
		t.Fatal(err)
	}
	var packetRow map[string]any
	if err := json.Unmarshal(packetBytes, &packetRow); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{
		"schema_version": "1.2.0", "runtime_id": runtimeID, "revision": 3,
		"baseline":      map[string]any{"generation": generation},
		"lifecycle":     map[string]any{"state": "bug_resolution", "phase": "investigation"},
		"configuration": map[string]any{"optimization": map[string]any{"packet": true}},
		"journal":       map[string]any{"last_sequence": 2, "last_event_id": packet.IssuanceEventID},
		"entities": map[string]any{
			"work_packets": []any{packetRow}, "submissions": []any{},
			"agents": []any{map[string]any{
				"id": ownerID, "role": "investigator", "team_id": teamID, "prompt_ref": manifestRef + "#" + assignID,
				"task_ids": []any{taskID},
			}},
			"teams": []any{map[string]any{
				"id": teamID, "kind": "investigator", "manifest_ref": manifestRef,
				"manifest_sha256": workpacket.Digest(manifestBytes),
			}},
		},
		"review": map[string]any{
			"round": 1,
			"investigation": map[string]any{
				"case_id": caseID, "path": caseRef, "sha256": workpacket.Digest(caseBytes), "revision": 1,
				"status": "investigating", "route": nil, "route_reason": nil,
				"route_consumed_at": nil, "route_consumer": nil,
			},
		},
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), journal, 0o600); err != nil {
		t.Fatal(err)
	}
	return hypothesisPacketRuntimeFixture{root: root, packet: packet, state: state, journal: journal, registerEvent: registerEvent, issueEvent: issueEvent}
}

func s8CheckpointJournalBytes(t *testing.T, events ...map[string]any) []byte {
	t.Helper()
	var data []byte
	for _, event := range events {
		line, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, line...)
		data = append(data, '\n')
	}
	return data
}

func s8CheckpointCloneMap(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var clone map[string]any
	if err := json.Unmarshal(data, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func s8CheckpointChangeJournalEvent(t *testing.T, journal []byte, index int, mutate func(map[string]any)) []byte {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(string(journal), "\n"), "\n")
	if index < 0 || index >= len(lines) {
		t.Fatalf("journal event index %d out of range", index)
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(lines[index]), &event); err != nil {
		t.Fatal(err)
	}
	mutate(event)
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	lines[index] = string(encoded)
	return []byte(strings.Join(lines, "\n") + "\n")
}

func s8CheckpointWriteJSON(t *testing.T, root, ref string, value any) []byte {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	path := filepath.Join(root, filepath.FromSlash(ref))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestHypothesisResultPacketLookupIssueAndFullCheckpointFiltering(t *testing.T) {
	f := newHypothesisPacketRuntimeFixture(t)
	before, _ := json.Marshal(f.state)
	got, found, err := FindHypothesisResultPacket(f.state, f.packet.ID)
	if err != nil || !found || !reflect.DeepEqual(got, f.packet) {
		t.Fatalf("FindHypothesisResultPacket() found=%t err=%v", found, err)
	}
	current, err := CurrentHypothesisResultPackets(f.root, f.state, f.packet.Binding.OwnerAgentID)
	if err != nil || len(current) != 1 || current[0].ID != f.packet.ID {
		t.Fatalf("CurrentHypothesisResultPackets() = %d packets, err=%v", len(current), err)
	}
	if err := VerifyHypothesisResultPacketIssue(f.root, f.state, f.packet); err != nil {
		t.Fatalf("valid original S8 issue/source closure rejected: %v", err)
	}
	filtered, err := validateHypothesisResultCheckpointIndices(f.root, f.state, f.journal)
	if err != nil {
		t.Fatalf("S8 checkpoint packet rejected: %v", err)
	}
	filteredRows := filtered["entities"].(map[string]any)["work_packets"].([]any)
	if len(filteredRows) != 0 {
		t.Fatalf("S8 packet subtype remained for legacy recovery: rows=%v", filteredRows)
	}
	after, _ := json.Marshal(f.state)
	if string(before) != string(after) {
		t.Fatal("typed packet lookup/checkpoint filtering mutated durable state")
	}
	if err := ValidateCheckpointIndices(f.root, f.state, f.journal); err != nil {
		t.Fatalf("full Runtime ValidateCheckpointIndices rejected original packet source/journal pair: %v", err)
	}
	finalBytes, _ := json.Marshal(f.state)
	if string(before) != string(finalBytes) {
		t.Fatal("full Runtime checkpoint validation mutated durable state")
	}
}

func TestHypothesisResultPacketRequiresOriginalRegisteredManifestSHA(t *testing.T) {
	for _, test := range []struct {
		name  string
		value any
	}{
		{name: "missing", value: nil},
		{name: "changed", value: strings.Repeat("e", 64)},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newHypothesisPacketRuntimeFixture(t)
			team := objectRows(f.state["entities"].(map[string]any)["teams"])[0]
			if test.value == nil {
				delete(team, "manifest_sha256")
			} else {
				team["manifest_sha256"] = test.value
			}
			if err := VerifyHypothesisResultPacketIssue(f.root, f.state, f.packet); err == nil || !strings.Contains(err.Error(), "registered Team authority") {
				t.Fatalf("packet accepted without the original Team manifest SHA: %v", err)
			}
			selected, err := CurrentHypothesisResultPackets(f.root, f.state, f.packet.Binding.OwnerAgentID)
			if err != nil || len(selected) != 0 {
				t.Fatalf("packet remained selectable after registered manifest SHA drift: %d packets, err=%v", len(selected), err)
			}
		})
	}
}

func TestCurrentHypothesisResultPacketRejectsPointerOnlyRouteConsumption(t *testing.T) {
	f := newHypothesisPacketRuntimeFixture(t)
	pointer := f.state["review"].(map[string]any)["investigation"].(map[string]any)
	pointer["route"] = "investigate_more"
	pointer["route_reason"] = "The Case needs one more investigation pass."
	pointer["route_consumed_at"] = "2026-10-14T12:00:00Z"
	pointer["route_consumer"] = "S8-ROUTE-CONSUME"

	selected, err := CurrentHypothesisResultPackets(f.root, f.state, f.packet.Binding.OwnerAgentID)
	if err != nil || len(selected) != 0 {
		t.Fatalf("fresh S8 packet survived pointer-only route consumption: %d packets, err=%v", len(selected), err)
	}
}

func TestHypothesisResultPacketLookupRejectsDuplicateIDsAndMissingPacketContract(t *testing.T) {
	f := newHypothesisPacketRuntimeFixture(t)
	rows := f.state["entities"].(map[string]any)["work_packets"].([]any)
	duplicate := s8CheckpointCloneMap(t, rows[0].(map[string]any))
	f.state["entities"].(map[string]any)["work_packets"] = append(rows, duplicate)
	if _, err := HypothesisResultPackets(f.state); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate packet identities accepted: %v", err)
	}

	f = newHypothesisPacketRuntimeFixture(t)
	f.state["configuration"].(map[string]any)["optimization"].(map[string]any)["packet"] = false
	if _, err := HypothesisResultPackets(f.state); err == nil || !strings.Contains(err.Error(), "explicit v1.2 packet contract") {
		t.Fatalf("S8 packet without explicit packet flag accepted: %v", err)
	}
}

func TestHypothesisResultPacketRejectsSourceByteAndTupleDrift(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *hypothesisPacketRuntimeFixture)
	}{
		{
			name: "case question source bytes",
			mutate: func(t *testing.T, f *hypothesisPacketRuntimeFixture) {
				var doc map[string]any
				path := filepath.Join(f.root, filepath.FromSlash(f.packet.Binding.CaseRef))
				data, _ := os.ReadFile(path)
				if err := json.Unmarshal(data, &doc); err != nil {
					t.Fatal(err)
				}
				doc["grouping_rationale"] = "A changed source byte invalidates the pinned Case."
				s8CheckpointWriteJSON(t, f.root, f.packet.Binding.CaseRef, doc)
			},
		},
		{
			name: "case question tuple with self-consistent sha",
			mutate: func(t *testing.T, f *hypothesisPacketRuntimeFixture) {
				var doc map[string]any
				data, _ := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(f.packet.Binding.CaseRef)))
				if err := json.Unmarshal(data, &doc); err != nil {
					t.Fatal(err)
				}
				hyp := objectRows(doc["hypotheses"])[0]
				hyp["statement"] = "A source-author changed the registered question."
				caseBytes := s8CheckpointWriteJSON(t, f.root, f.packet.Binding.CaseRef, doc)
				binding := f.packet.Binding
				binding.CaseSHA256 = workpacket.Digest(caseBytes)
				forged, err := workpacket.BuildHypothesisResult(binding, f.packet.Inputs)
				if err != nil {
					t.Fatal(err)
				}
				f.packet = forged
			},
		},
		{
			name: "case route tuple with self-consistent sha",
			mutate: func(t *testing.T, f *hypothesisPacketRuntimeFixture) {
				var doc map[string]any
				data, _ := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(f.packet.Binding.CaseRef)))
				if err := json.Unmarshal(data, &doc); err != nil {
					t.Fatal(err)
				}
				doc["route"] = "investigate_more"
				doc["route_reason"] = "A new route was written after packet issuance."
				caseBytes := s8CheckpointWriteJSON(t, f.root, f.packet.Binding.CaseRef, doc)
				binding := f.packet.Binding
				binding.CaseSHA256 = workpacket.Digest(caseBytes)
				forged, err := workpacket.BuildHypothesisResult(binding, f.packet.Inputs)
				if err != nil {
					t.Fatal(err)
				}
				f.packet = forged
			},
		},
		{
			name: "observation batch bytes",
			mutate: func(t *testing.T, f *hypothesisPacketRuntimeFixture) {
				var doc map[string]any
				data, _ := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(f.packet.Binding.ObservationBatchRef)))
				if err := json.Unmarshal(data, &doc); err != nil {
					t.Fatal(err)
				}
				doc["severity_summary"] = "changed after original sealing"
				s8CheckpointWriteJSON(t, f.root, f.packet.Binding.ObservationBatchRef, doc)
			},
		},
		{
			name: "finding bytes",
			mutate: func(t *testing.T, f *hypothesisPacketRuntimeFixture) {
				var doc map[string]any
				data, _ := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(f.packet.Inputs.Findings[0].Ref)))
				if err := json.Unmarshal(data, &doc); err != nil {
					t.Fatal(err)
				}
				doc["observed"] = "changed after immutable Finding capture"
				s8CheckpointWriteJSON(t, f.root, f.packet.Inputs.Findings[0].Ref, doc)
			},
		},
		{
			name: "task tuple with self-consistent sha",
			mutate: func(t *testing.T, f *hypothesisPacketRuntimeFixture) {
				var doc map[string]any
				data, _ := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(f.packet.Binding.TaskRef)))
				if err := json.Unmarshal(data, &doc); err != nil {
					t.Fatal(err)
				}
				doc["hypothesis_id"] = "hypothesis-other"
				taskBytes := s8CheckpointWriteJSON(t, f.root, f.packet.Binding.TaskRef, doc)
				binding := f.packet.Binding
				binding.TaskSHA256 = workpacket.Digest(taskBytes)
				forged, err := workpacket.BuildHypothesisResult(binding, f.packet.Inputs)
				if err != nil {
					t.Fatal(err)
				}
				f.packet = forged
			},
		},
		{
			name: "manifest Assignment tuple with self-consistent shas",
			mutate: func(t *testing.T, f *hypothesisPacketRuntimeFixture) {
				var doc map[string]any
				data, _ := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(f.packet.Binding.ManifestRef)))
				if err := json.Unmarshal(data, &doc); err != nil {
					t.Fatal(err)
				}
				assignment := objectRows(doc["assignments"])[0]
				assignment["scope"] = []any{"case:" + f.packet.Binding.CaseID, "hypothesis:hypothesis-other"}
				manifestBytes := s8CheckpointWriteJSON(t, f.root, f.packet.Binding.ManifestRef, doc)
				assignmentBytes, _ := json.Marshal(assignment)
				binding := f.packet.Binding
				binding.ManifestSHA256 = workpacket.Digest(manifestBytes)
				binding.AssignmentSHA256 = workpacket.Digest(assignmentBytes)
				forged, err := workpacket.BuildHypothesisResult(binding, f.packet.Inputs)
				if err != nil {
					t.Fatal(err)
				}
				f.packet = forged
			},
		},
		{
			name: "manifest missing the exact pinned Case source",
			mutate: func(t *testing.T, f *hypothesisPacketRuntimeFixture) {
				var doc map[string]any
				data, _ := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(f.packet.Binding.ManifestRef)))
				if err := json.Unmarshal(data, &doc); err != nil {
					t.Fatal(err)
				}
				doc["documents"] = []any{objectRows(doc["documents"])[1]}
				manifestBytes := s8CheckpointWriteJSON(t, f.root, f.packet.Binding.ManifestRef, doc)
				binding := f.packet.Binding
				binding.ManifestSHA256 = workpacket.Digest(manifestBytes)
				forged, err := workpacket.BuildHypothesisResult(binding, f.packet.Inputs)
				if err != nil {
					t.Fatal(err)
				}
				f.packet = forged
			},
		},
		{
			name: "manifest Case document wrong pin",
			mutate: func(t *testing.T, f *hypothesisPacketRuntimeFixture) {
				var doc map[string]any
				data, _ := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(f.packet.Binding.ManifestRef)))
				if err := json.Unmarshal(data, &doc); err != nil {
					t.Fatal(err)
				}
				objectRows(doc["documents"])[0]["sha256"] = strings.Repeat("e", 64)
				manifestBytes := s8CheckpointWriteJSON(t, f.root, f.packet.Binding.ManifestRef, doc)
				binding := f.packet.Binding
				binding.ManifestSHA256 = workpacket.Digest(manifestBytes)
				forged, err := workpacket.BuildHypothesisResult(binding, f.packet.Inputs)
				if err != nil {
					t.Fatal(err)
				}
				f.packet = forged
			},
		},
		{
			name: "indexed evidence physical bytes",
			mutate: func(t *testing.T, f *hypothesisPacketRuntimeFixture) {
				ref := f.packet.Inputs.EvidenceRefs[0]
				path := filepath.Join(f.root, filepath.FromSlash(ref.Path))
				if err := os.WriteFile(path, []byte(`{"kind":"experiment","source":"changed"}`), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newHypothesisPacketRuntimeFixture(t)
			journal, err := controlproof.InspectJournalData(f.journal)
			if err != nil {
				t.Fatal(err)
			}
			tt.mutate(t, &f)
			if err := verifyHypothesisResultPacketSources(f.root, f.state, f.packet, journal, 3); err == nil {
				t.Fatal("source drift was accepted")
			}
		})
	}
}

func TestHypothesisResultPacketVerifiesOriginalRegisterAndIssueEventTuples(t *testing.T) {
	for _, which := range []string{"register", "issue"} {
		t.Run(which, func(t *testing.T) {
			f := newHypothesisPacketRuntimeFixture(t)
			mutations := []struct {
				name string
				edit func(map[string]any)
			}{
				{"actor", func(e map[string]any) { e["actor"] = map[string]any{"type": "agent", "id": "intruder"} }},
				{"idempotency key", func(e map[string]any) { e["idempotency_key"] = "other-key" }},
				{"baseline generation", func(e map[string]any) { e["baseline_generation"] = float64(3) }},
				{"cursor", func(e map[string]any) {
					e["from"] = map[string]any{"state": "bug_resolution", "phase": "repair"}
					e["to"] = map[string]any{"state": "bug_resolution", "phase": "repair"}
				}},
			}
			if which == "issue" {
				mutations = append(mutations, struct {
					name string
					edit func(map[string]any)
				}{"runtime", func(e map[string]any) { e["runtime_id"] = "loop-other" }})
			} else {
				mutations = append(mutations, struct {
					name string
					edit func(map[string]any)
				}{"runtime", func(e map[string]any) { e["runtime_id"] = "loop-other" }})
			}
			for _, mutation := range mutations {
				t.Run(mutation.name, func(t *testing.T) {
					index := 1
					if which == "register" {
						index = 0
					}
					corrupt := s8CheckpointChangeJournalEvent(t, f.journal, index, mutation.edit)
					inspection, err := controlproof.InspectJournalData(corrupt)
					if err == nil {
						err = verifyHypothesisResultPacketIssue(f.root, f.state, f.packet, inspection)
					}
					if err == nil {
						t.Fatal("tampered original registration/issuance tuple was accepted")
					}
				})
			}
		})
	}
}

func TestHypothesisResultPacketRejectsMissingAndOrphanOriginalIssue(t *testing.T) {
	f := newHypothesisPacketRuntimeFixture(t)
	registerOnlyState := s8CheckpointCloneMap(t, f.state)
	registerOnlyState["revision"] = 2
	registerOnlyState["journal"] = map[string]any{"last_sequence": 1, "last_event_id": f.registerEvent["event_id"]}
	registerOnly := s8CheckpointJournalBytes(t, f.registerEvent)
	inspection, err := controlproof.InspectJournalData(registerOnly)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyHypothesisResultPacketIssue(f.root, registerOnlyState, f.packet, inspection); err == nil || !strings.Contains(err.Error(), "no original issuance event") {
		t.Fatalf("packet without its exact original issue event was accepted: %v", err)
	}

	issueWithoutRegistrationState := s8CheckpointCloneMap(t, f.state)
	issueWithoutRegistrationState["journal"] = map[string]any{"last_sequence": 1, "last_event_id": f.issueEvent["event_id"]}
	issueOnly := s8CheckpointCloneMap(t, f.issueEvent)
	issueOnly["sequence"] = float64(1)
	inspection, err = controlproof.InspectJournalData(s8CheckpointJournalBytes(t, issueOnly))
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyHypothesisResultPacketIssue(f.root, issueWithoutRegistrationState, f.packet, inspection); err == nil || !strings.Contains(err.Error(), "no unique original registration") {
		t.Fatalf("packet without its original manifest/TASK registration event was accepted: %v", err)
	}
	orphan := buildJournalEvent(Mutation{
		EventID: "evt-hypothesis-result-packet-packet-orphan", TransitionID: "PACKET-ISSUE", Event: "transition_committed",
		Actor: "orchestrator", IdempotencyKey: "packet:packet-orphan", RuntimeID: f.packet.Binding.RuntimeID,
		BaselineGeneration: f.packet.Binding.BaselineGeneration, From: map[string]any{"state": "bug_resolution", "phase": "investigation"}, To: map[string]any{"state": "bug_resolution", "phase": "investigation"},
	}, f.packet.Binding.RuntimeID, 3, 4, 3, time.Date(2026, 10, 14, 0, 2, 0, 0, time.UTC))
	orphanJournal := s8CheckpointJournalBytes(t, f.registerEvent, f.issueEvent, orphan)
	state := s8CheckpointCloneMap(t, f.state)
	state["revision"] = 4
	state["journal"] = map[string]any{"last_sequence": 3, "last_event_id": orphan["event_id"]}
	if _, err := validateHypothesisResultCheckpointIndices(f.root, state, orphanJournal); err == nil || !strings.Contains(err.Error(), "no durable packet row") {
		t.Fatalf("orphan issue event accepted: %v", err)
	}

	removed := s8CheckpointCloneMap(t, f.state)
	removed["entities"].(map[string]any)["work_packets"] = []any{}
	if _, err := validateHypothesisResultCheckpointIndices(f.root, removed, f.journal); err == nil || !strings.Contains(err.Error(), "no durable packet row") {
		t.Fatalf("packet row removed after issue was accepted: %v", err)
	}
}

func TestHypothesisResultPacketSubtypeFilterPreservesOtherRows(t *testing.T) {
	f := newHypothesisPacketRuntimeFixture(t)
	other := map[string]any{"packet_id": "packet-s7-placeholder", "template_id": "s7-review-result-v1", "opaque": true}
	rows := f.state["entities"].(map[string]any)["work_packets"].([]any)
	f.state["entities"].(map[string]any)["work_packets"] = append(rows, other)
	packets, err := HypothesisResultPackets(f.state)
	if err != nil || len(packets) != 1 || packets[0].ID != f.packet.ID {
		t.Fatalf("S8 lookup did not isolate the S8 subtype: packets=%v err=%v", packets, err)
	}
	filtered, err := validateHypothesisResultCheckpointIndices(f.root, f.state, f.journal)
	if err != nil {
		t.Fatal(err)
	}
	filteredRows := filtered["entities"].(map[string]any)["work_packets"].([]any)
	if len(filteredRows) != 1 || filteredRows[0].(map[string]any)["packet_id"] != other["packet_id"] {
		t.Fatalf("S8 checkpoint removed another subtype: %#v", filteredRows)
	}
	if got := len(f.state["entities"].(map[string]any)["work_packets"].([]any)); got != 2 {
		t.Fatalf("subtype filter mutated source state: %d rows remain", got)
	}
}
