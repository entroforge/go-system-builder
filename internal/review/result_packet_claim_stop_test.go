package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Start with a real issued questionnaire. The author supplies observations
// and paths, never the complete internal Result envelope or evidence hashes.
func issuedClaimStopFixture(t *testing.T) (string, string, string, workpacket.ReviewResultPacket, IssuedSubmitRequest) {
	t.Helper()
	root, statePath, journalPath, _, _ := packetModeFixture(t)
	var state map[string]any
	if err := json.Unmarshal(mustReadTestFile(t, statePath), &state); err != nil {
		t.Fatal(err)
	}
	for _, raw := range state["entities"].(map[string]any)["agents"].([]any) {
		agent := raw.(map[string]any)
		if agent["id"] == "agent-qa-1" {
			agent["state"], agent["dispatch_mode"] = "reading", "plan_checkpoint"
			agent["plan_reported_ref"], agent["activation_ref"], agent["readback_ref"] = nil, nil, nil
		}
	}
	data, _ := json.Marshal(state)
	if err := os.WriteFile(statePath, data, 0600); err != nil {
		t.Fatal(err)
	}
	packets, err := IssueResultPackets(root, statePath, journalPath, -1, "agent-qa-1")
	if err != nil || len(packets) != 1 {
		t.Fatalf("issue QA packet: %v (%d packets)", err, len(packets))
	}
	p := packets[0]
	return root, statePath, journalPath, p, IssuedSubmitRequest{ExpectedRevision: -1, PacketID: p.ID, AnswerPath: p.AnswerPath, OwnerAgentID: p.Binding.OwnerAgentID}
}

// Each negative case gets its own copy of a genuinely issued Git and Runtime
// fixture, so a rejected answer cannot leave artifacts for another case.
func cloneIssuedClaimStopFixture(t *testing.T, root, statePath, journalPath string, packet workpacket.ReviewResultPacket, request IssuedSubmitRequest) (string, string, string, workpacket.ReviewResultPacket, IssuedSubmitRequest) {
	t.Helper()
	clone := t.TempDir()
	if err := os.CopyFS(clone, os.DirFS(root)); err != nil {
		t.Fatalf("clone issued fixture: %v", err)
	}
	stateRel, err := filepath.Rel(root, statePath)
	if err != nil {
		t.Fatal(err)
	}
	journalRel, err := filepath.Rel(root, journalPath)
	if err != nil {
		t.Fatal(err)
	}
	previous := fixtureEvidenceRoot
	fixtureEvidenceRoot = clone
	t.Cleanup(func() { fixtureEvidenceRoot = previous })
	return clone, filepath.Join(clone, stateRel), filepath.Join(clone, journalRel), packet, request
}

func claimStopAnswer(t *testing.T, severity string) map[string]any {
	t.Helper()
	finding := codeInspectionFinding("finding-safety-stop", "claim-qa-1")
	finding.Severity = severity
	finding.Encounter.LastGoodCheckpoint = ""
	finding.Encounter.CaptureGaps = []string{"stopped the remaining checks before exercising the data-destructive operation"}
	if severity != "P0" {
		finding.Encounter.LastGoodCheckpoint = "caller still preserves the requested state"
	}
	data, _ := json.Marshal(finding)
	var professional map[string]any
	_ = json.Unmarshal(data, &professional)
	delete(professional, "schema_version")
	delete(professional, "lens")
	delete(professional, "finding_id")
	ref := strings.SplitN(finding.EvidenceRefs[0], "#sha256=", 2)[0]
	professional["evidence_refs"] = []any{ref}
	return map[string]any{
		"claim_results":  []any{map[string]any{"claim_id": "claim-qa-1", "conclusion": "fail", "method": "trace the actual mutation path", "observed": "the failed operation persists a destructive write", "evidence_refs": []any{ref}}},
		"blocked_claims": []any{}, "checks": []any{}, "findings": []any{professional},
		"deviations": []any{}, "site_lost": []any{}, "verification_artifact_digest": nil, "verdict": "finding",
	}
}

func saveClaimStopAnswer(t *testing.T, root string, p workpacket.ReviewResultPacket, answer map[string]any) []byte {
	t.Helper()
	p.Draft.Answer, _ = json.Marshal(answer)
	data, _ := json.Marshal(p.Draft)
	if err := os.WriteFile(filepath.Join(root, p.AnswerPath), data, 0600); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestIssuedP0PartialClaimStopPreservesUnobservedAndReplay(t *testing.T) {
	root, statePath, journalPath, packet, request := issuedClaimStopFixture(t)
	authored := saveClaimStopAnswer(t, root, packet, claimStopAnswer(t, "P0"))
	accepted, err := SubmitIssuedResult(root, statePath, journalPath, request)
	if err != nil {
		t.Fatalf("P0 must not require unsafe remaining Claims: %v", err)
	}
	if ptr := PlanPointerFromState(accepted.State); ptr.Status != "observation_sealed" {
		t.Fatalf("P0 did not seal: %+v", ptr)
	}
	batch := readBatchFile(t, root, accepted)
	if batch["drain_policy"] != "immediate_stop" {
		t.Fatalf("wrong drain: %v", batch)
	}
	var unobserved []string
	for _, raw := range batch["unobserved_claim_ids"].([]any) {
		unobserved = append(unobserved, raw.(string))
	}
	if !slices.Equal(unobserved, []string{"claim-dv-1", "claim-qa-2"}) {
		t.Fatalf("own and other unfinished Claims must remain explicit: %v", unobserved)
	}
	for _, id := range unobserved {
		d := Dispositions(accepted.State)[id]
		if d.ResultID != "" || d.Disposition == "pass" || d.Disposition == "blocked" {
			t.Fatalf("unobserved Claim fabricated a result: %+v", d)
		}
	}
	if err := loopruntime.ValidateCheckpointIndices(root, accepted.State, mustReadTestFile(t, journalPath)); err != nil {
		t.Fatalf("partial P0 broke durable checkpoint: %v", err)
	}
	beforeState, beforeJournal := mustReadTestFile(t, statePath), mustReadTestFile(t, journalPath)
	if _, err := SubmitIssuedResult(root, statePath, journalPath, request); err != nil {
		t.Fatalf("partial P0 replay: %v", err)
	}
	if !reflect.DeepEqual(beforeState, mustReadTestFile(t, statePath)) || !reflect.DeepEqual(beforeJournal, mustReadTestFile(t, journalPath)) || !reflect.DeepEqual(authored, mustReadTestFile(t, filepath.Join(root, packet.AnswerPath))) {
		t.Fatal("replay changed authority or authored bytes")
	}
}

func TestIssuedPartialClaimStopRejectsInvalidExceptions(t *testing.T) {
	templateRoot, templateState, templateJournal, templatePacket, templateRequest := issuedClaimStopFixture(t)
	for _, test := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"ordinary_missing", func(a map[string]any) { a["findings"].([]any)[0].(map[string]any)["severity"] = "P1" }},
		{"no_finding", func(a map[string]any) { a["findings"] = []any{} }},
		{"no_failed_claim", func(a map[string]any) { a["claim_results"].([]any)[0].(map[string]any)["conclusion"] = "pass" }},
		{"null_is_not_unobserved", func(a map[string]any) {
			a["claim_results"] = append(a["claim_results"].([]any), map[string]any{"claim_id": "claim-qa-2", "conclusion": nil, "observed": "", "method": "", "evidence_refs": []any{}})
		}},
		{"duplicate_claim", func(a map[string]any) {
			a["claim_results"] = append(a["claim_results"].([]any), a["claim_results"].([]any)[0])
		}},
		{"foreign_claim", func(a map[string]any) { a["claim_results"].([]any)[0].(map[string]any)["claim_id"] = "claim-dv-1" }},
		{"foreign_finding", func(a map[string]any) { a["findings"].([]any)[0].(map[string]any)["claim_id"] = "claim-dv-1" }},
		{"unsafe_gap_missing", func(a map[string]any) {
			a["findings"].([]any)[0].(map[string]any)["encounter"].(map[string]any)["capture_gaps"] = []any{}
		}},
		{"false_blocking", func(a map[string]any) { a["findings"].([]any)[0].(map[string]any)["blocking"] = false }},
		{"missing_evidence", func(a map[string]any) {
			a["findings"].([]any)[0].(map[string]any)["evidence_refs"] = []any{"path:missing.txt"}
		}},
		{"pause_cannot_bypass_coverage", func(a map[string]any) { a["verdict"] = "release_blocked" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, statePath, journalPath, packet, request := cloneIssuedClaimStopFixture(t, templateRoot, templateState, templateJournal, templatePacket, templateRequest)
			answer := claimStopAnswer(t, "P0")
			test.edit(answer)
			saveClaimStopAnswer(t, root, packet, answer)
			beforeState, beforeJournal := mustReadTestFile(t, statePath), mustReadTestFile(t, journalPath)
			if _, err := SubmitIssuedResult(root, statePath, journalPath, request); err == nil {
				t.Fatal("invalid safety stop was accepted")
			}
			if !reflect.DeepEqual(beforeState, mustReadTestFile(t, statePath)) || !reflect.DeepEqual(beforeJournal, mustReadTestFile(t, journalPath)) {
				t.Fatal("rejected safety stop changed authority")
			}
		})
	}
}

func TestIssuedBlockedClaimConsumesTemplateEvidence(t *testing.T) {
	for _, form := range []string{"path", "same_finding", "runtime"} {
		t.Run(form, func(t *testing.T) {
			root, statePath, journalPath, packet, request := issuedClaimStopFixture(t)
			answer := claimStopAnswer(t, "P1")
			finding := answer["findings"].([]any)[0].(map[string]any)
			finding["finding_id"] = "finding-product-blocker"
			ref := finding["evidence_refs"].([]any)[0].(string)
			if form == "same_finding" {
				ref = "finding-product-blocker"
			}
			if form == "runtime" {
				var state map[string]any
				_ = json.Unmarshal(mustReadTestFile(t, statePath), &state)
				rel := strings.TrimPrefix(ref, "path:")
				row := revisionSourceState()["evidence"].([]any)[0].(map[string]any)
				row["id"], row["path"], row["sha256"] = "evidence-blocked-start", rel, sha256Of(mustReadTestFile(t, filepath.Join(root, rel)))
				state["evidence"] = append(evidenceEntries(state), row)
				data, _ := json.Marshal(state)
				if err := os.WriteFile(statePath, data, 0600); err != nil {
					t.Fatal(err)
				}
				ref = "runtime:evidence-blocked-start"
			}
			answer["blocked_claims"] = []any{map[string]any{
				"claim_id": "claim-qa-2", "blocking_finding_ids": []any{"finding-product-blocker"},
				"failed_precondition": map[string]any{"kind": "start", "detail": "the confirmed product defect prevents the required startup"},
				"evidence_refs":       []any{ref}, "after_repair_required": true,
			}}
			authored := saveClaimStopAnswer(t, root, packet, answer)
			accepted, err := SubmitIssuedResult(root, statePath, journalPath, request)
			if err != nil {
				t.Fatalf("normal blocked %s evidence: %v", form, err)
			}
			if d := Dispositions(accepted.State)["claim-qa-2"]; d.Disposition != "blocked" || !slices.Equal(d.FindingIDs, []string{"finding-product-blocker"}) {
				t.Fatalf("blocked projection lost its Finding: %+v", d)
			}
			beforeState, beforeJournal := mustReadTestFile(t, statePath), mustReadTestFile(t, journalPath)
			if _, err := SubmitIssuedResult(root, statePath, journalPath, request); err != nil {
				t.Fatalf("blocked replay: %v", err)
			}
			if !reflect.DeepEqual(beforeState, mustReadTestFile(t, statePath)) || !reflect.DeepEqual(beforeJournal, mustReadTestFile(t, journalPath)) || !reflect.DeepEqual(authored, mustReadTestFile(t, filepath.Join(root, packet.AnswerPath))) {
				t.Fatal("blocked replay changed authority or author bytes")
			}
		})
	}
}

func TestIssuedBlockedClaimRejectsFalseBindingsAndEvidence(t *testing.T) {
	templateRoot, templateState, templateJournal, templatePacket, templateRequest := issuedClaimStopFixture(t)
	for _, test := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"unconfirmed_finding", func(b map[string]any) { b["blocking_finding_ids"] = []any{"finding-not-confirmed"} }},
		{"unknown_evidence", func(b map[string]any) { b["evidence_refs"] = []any{"evidence-not-registered"} }},
		{"missing_file", func(b map[string]any) { b["evidence_refs"] = []any{"path:does-not-exist.txt"} }},
		{"escaped_file", func(b map[string]any) { b["evidence_refs"] = []any{"path:../outside.txt"} }},
		{"wrong_digest", func(b map[string]any) {
			b["evidence_refs"] = []any{b["evidence_refs"].([]any)[0].(string) + "#sha256=" + strings.Repeat("0", 64)}
		}},
		{"no_precondition", func(b map[string]any) { b["failed_precondition"] = map[string]any{"kind": "start", "detail": ""} }},
		{"no_repair_debt", func(b map[string]any) { b["after_repair_required"] = false }},
		{"already_answered_claim", func(b map[string]any) { b["claim_id"] = "claim-qa-1" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, statePath, journalPath, packet, request := cloneIssuedClaimStopFixture(t, templateRoot, templateState, templateJournal, templatePacket, templateRequest)
			answer := claimStopAnswer(t, "P1")
			finding := answer["findings"].([]any)[0].(map[string]any)
			finding["finding_id"] = "finding-product-blocker"
			blocked := map[string]any{
				"claim_id": "claim-qa-2", "blocking_finding_ids": []any{"finding-product-blocker"},
				"failed_precondition": map[string]any{"kind": "start", "detail": "the confirmed defect prevents required startup"},
				"evidence_refs":       finding["evidence_refs"], "after_repair_required": true,
			}
			test.edit(blocked)
			answer["blocked_claims"] = []any{blocked}
			saveClaimStopAnswer(t, root, packet, answer)
			beforeState, beforeJournal := mustReadTestFile(t, statePath), mustReadTestFile(t, journalPath)
			if _, err := SubmitIssuedResult(root, statePath, journalPath, request); err == nil {
				t.Fatal("false blocked declaration was accepted")
			}
			if !reflect.DeepEqual(beforeState, mustReadTestFile(t, statePath)) || !reflect.DeepEqual(beforeJournal, mustReadTestFile(t, journalPath)) {
				t.Fatal("rejected blocked declaration changed authority")
			}
		})
	}
}

func TestBlockedRuntimeEvidenceRetainsCurrentRoundAndStatus(t *testing.T) {
	root := t.TempDir()
	ref := fixtureEvidenceRef(t, root, "blocked-current-round.txt")
	rel, digest, err := parsePathEvidenceRef(ref)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		round  int
		status string
	}{
		{"prior_round", 1, "valid"}, {"invalid", 2, "invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := map[string]any{"review": map[string]any{"round": 2}, "evidence": []any{map[string]any{
				"id": "blocked-proof", "review_round": test.round, "status": test.status, "path": rel, "sha256": digest,
			}}}
			plan := &Plan{Claims: []Claim{{ClaimID: "claim-two", Applicability: "required"}}}
			result := &Result{ReviewRound: 2, Findings: []Finding{{FindingID: "finding-current", EvidenceRefs: []string{ref}}}, BlockedClaims: []BlockedClaim{{
				ClaimID: "claim-two", BlockingFindingIDs: []string{"finding-current"},
				FailedPrecondition: FailedPrecondition{Kind: "start", Detail: "actual startup failure"}, EvidenceRefs: []string{"runtime:blocked-proof"}, AfterRepairRequired: true,
			}}}
			if err := validateBlockedClaims(root, state, plan, &PlanAssignment{}, result); err == nil || !strings.Contains(err.Error(), "current-round") {
				t.Fatalf("stale Runtime evidence was accepted: %v", err)
			}
		})
	}
}

func TestIssuedHistoricalS7TemplateRestoresAcceptsAndReplays(t *testing.T) {
	root, statePath, journalPath, current, _ := issuedClaimStopFixture(t)
	priorTemplate, err := schema.ReadAsset("stage-form-s7-template-before-safety-stop.json")
	if err != nil {
		t.Fatal(err)
	}
	legacyInputs := current.Inputs
	legacyInputs.Methods, legacyInputs.MethodSource = nil, nil
	old, err := workpacket.RebuildReviewResult(workpacket.ReviewResultPacket{Binding: current.Binding, Inputs: legacyInputs, TemplateSHA: workpacket.Digest(priorTemplate)})
	if err != nil || old.ID == current.ID {
		t.Fatalf("historical template fixture: %v", err)
	}
	// The helper issued today's packet only to obtain its responsibility. Its
	// synthetic answer and view did not exist in this simulated old history.
	// Remove exactly those synthetic files before replacing the registry row,
	// so the real Git drift scan still rejects unrelated orphaned artifacts.
	for _, rel := range []string{current.ViewPath, current.AnswerPath} {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Fatal(err)
		}
	}
	// Seed a historical issuance, including its original event, as if the
	// binary had been upgraded after that issuance. Production never rewrites
	// either identity: the recovery call below must preserve both byte sets.
	var state map[string]any
	_ = json.Unmarshal(mustReadTestFile(t, statePath), &state)
	rows := state["entities"].(map[string]any)["work_packets"].([]any)
	for i, raw := range rows {
		if raw.(map[string]any)["packet_id"] == current.ID {
			data, _ := json.Marshal(old)
			var row map[string]any
			_ = json.Unmarshal(data, &row)
			rows[i] = row
		}
	}
	data, _ := json.Marshal(state)
	data = []byte(strings.ReplaceAll(string(data), current.ID, old.ID))
	if err := os.WriteFile(statePath, data, 0600); err != nil {
		t.Fatal(err)
	}
	journal := []byte(strings.ReplaceAll(string(mustReadTestFile(t, journalPath)), current.ID, old.ID))
	if err := os.WriteFile(journalPath, journal, 0600); err != nil {
		t.Fatal(err)
	}
	// Old packets must recover even when the new method projection cannot
	// resolve a source; do not retrofit requirements onto their frozen identity.
	if err := os.Remove(filepath.Join(root, ".claude/workgroups/assignment-qa-1.json")); err != nil {
		t.Fatal(err)
	}
	beforeState := mustReadTestFile(t, statePath)
	restored, err := IssueResultPackets(root, statePath, journalPath, -1, old.Binding.OwnerAgentID)
	if err != nil || len(restored) != 1 || !reflect.DeepEqual(restored[0], old) {
		t.Fatalf("upgrade reissued rather than restoring frozen responsibility: %+v %v", restored, err)
	}
	if !reflect.DeepEqual(beforeState, mustReadTestFile(t, statePath)) || !reflect.DeepEqual(journal, mustReadTestFile(t, journalPath)) {
		t.Fatal("template upgrade rewrote original issuance history")
	}
	answer := claimStopAnswer(t, "P1")
	answer["claim_results"] = append(answer["claim_results"].([]any), map[string]any{
		"claim_id": "claim-qa-2", "conclusion": "pass", "method": "inspect independent branch", "observed": "the separate branch preserves its invariant", "evidence_refs": answer["claim_results"].([]any)[0].(map[string]any)["evidence_refs"],
	})
	authored := saveClaimStopAnswer(t, root, old, answer)
	request := IssuedSubmitRequest{ExpectedRevision: -1, PacketID: old.ID, AnswerPath: old.AnswerPath, OwnerAgentID: old.Binding.OwnerAgentID}
	var changed map[string]any
	_ = json.Unmarshal(authored, &changed)
	changed["instruction"] = current.Draft.Instruction
	wrong, _ := json.Marshal(changed)
	if err := os.WriteFile(filepath.Join(root, old.AnswerPath), wrong, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := SubmitIssuedResult(root, statePath, journalPath, request); err == nil {
		t.Fatal("upgrading author questions silently rebound the old issuance")
	}
	if err := os.WriteFile(filepath.Join(root, old.AnswerPath), authored, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := SubmitIssuedResult(root, statePath, journalPath, request); err != nil {
		t.Fatalf("legal historical authoring was rejected after upgrade: %v", err)
	}
	beforeState, journal = mustReadTestFile(t, statePath), mustReadTestFile(t, journalPath)
	if _, err := SubmitIssuedResult(root, statePath, journalPath, request); err != nil {
		t.Fatalf("historical replay: %v", err)
	}
	if !reflect.DeepEqual(beforeState, mustReadTestFile(t, statePath)) || !reflect.DeepEqual(journal, mustReadTestFile(t, journalPath)) || !reflect.DeepEqual(authored, mustReadTestFile(t, filepath.Join(root, old.AnswerPath))) {
		t.Fatal("historical replay refreshed its frozen sources or author bytes")
	}
}
