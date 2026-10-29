package qualitygate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestVerifiedContractPacketSourcesProjectsAcceptedFrozenSources(t *testing.T) {
	fixture := newContractPacketSubjectFixture(t, true)
	facts, err := verifiedContractPacketSources(fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	expected := len(fixture.packet.Inputs.Sources) + len(fixture.packet.Inputs.Designs)
	if len(facts) != expected {
		t.Fatalf("projected %d source facts for %d current design/source refs", len(facts), expected)
	}
	want := make(map[string]workpacket.Subject, expected)
	for _, design := range fixture.packet.Inputs.Designs {
		want[design.Path] = workpacket.Subject{Path: design.Path, Version: design.Version, SHA256: design.SHA256}
	}
	for _, source := range fixture.packet.Inputs.Sources {
		want[source.Path] = source
	}
	var originalEnvelope struct {
		SubjectRefs []workpacket.Subject `json:"subject_refs"`
	}
	if err := json.Unmarshal(fixture.envelopeBytes, &originalEnvelope); err != nil {
		t.Fatal(err)
	}
	envelopeSubjects := make(map[string]workpacket.Subject, len(originalEnvelope.SubjectRefs))
	for _, subject := range originalEnvelope.SubjectRefs {
		envelopeSubjects[subject.Path] = subject
	}
	for _, fact := range facts {
		source, ok := want[fact.Path]
		if !ok {
			t.Fatalf("projected non-frozen source %s", fact.Path)
		}
		if fact.Kind != "source" || fact.Status != "frozen" || fact.Generation != fixture.packet.Binding.Generation || fact.SHA256 != source.SHA256 {
			t.Errorf("source fact identity for %s = %#v", fact.Path, fact)
		}
		if fact.Version != envelopeSubjects[fact.Path].Version {
			t.Errorf("source version for %s = %q, want exact envelope version %q", fact.Path, fact.Version, envelopeSubjects[fact.Path].Version)
		}
		if strings.HasPrefix(fact.Path, "docs/dev/contracts/") {
			t.Errorf("S3 output was projected as an immutable source: %s", fact.Path)
		}
		delete(want, fact.Path)
	}
	if len(want) != 0 {
		t.Fatalf("omitted frozen sources: %#v", want)
	}
	if !containsContractSource(fixture.packet.Inputs.Sources, "docs/design/decisions/ADR-001.md") || !containsContractSource(fixture.packet.Inputs.Sources, "docs/design/decisions/ADR-002.md") {
		t.Fatal("S3 projection fixture failed to include transitive local ADR references")
	}

	// The source resolver deliberately ignores authoring outputs. Adding a
	// newly written contract must leave the source projection stable.
	writeContractPacketSubjectFile(t, fixture.root, "docs/dev/contracts/BE-002.md", "# More S3 output\n")
	writeContractPacketSubjectFile(t, fixture.root, "docs/dev/contracts/CONTRACTS-002.md", "# More index output\n")
	again, err := verifiedContractPacketSources(fixture.input)
	if err != nil {
		t.Fatalf("adding contract outputs drifted S3 inputs: %v", err)
	}
	if !reflect.DeepEqual(again, facts) {
		t.Fatalf("adding contract outputs changed projected sources: got %#v want %#v", again, facts)
	}
}

func TestVerifiedContractPacketFactsUsesRecordedDeliveryWithoutRecheckingDisk(t *testing.T) {
	fixture := newContractPacketSubjectFixture(t, false, true)
	state := fixture.input.Snapshot.State
	staleContract := []byte("previous registered bytes")
	documents := state["documents"].([]any)
	state["documents"] = append(documents, map[string]any{
		"id": "SYNC-001", "kind": "contract", "path": "docs/dev/contracts/SYNC-001.md",
		"version": "v0.9.0", "sha256": workpacket.Digest(staleContract), "status": "locked", "generation": 1,
	})
	facts, accepted, err := verifiedContractPacketFacts(fixture.input)
	if err != nil {
		t.Fatal(err)
	}
	if !accepted {
		t.Fatal("accepted S3 planning_contract was treated as an unfilled questionnaire")
	}
	byPath := make(map[string]documentFact, len(facts))
	for _, fact := range facts {
		byPath[fact.Path] = fact
	}
	for _, path := range []string{"docs/dev/contracts/CONTRACTS-001.md", "docs/dev/contracts/SYNC-001.md"} {
		fact, ok := byPath[path]
		if !ok {
			t.Fatalf("original S3 delivery omitted fact for %s: %#v", path, facts)
		}
		if fact.Status != "locked" || fact.SHA256 == "" || fact.Version != "v1.0.0" {
			t.Errorf("S3 delivered fact for %s = %#v", path, fact)
		}
	}
	if fact := byPath["docs/dev/contracts/SYNC-001.md"]; fact.Kind != "contract" {
		t.Errorf("S3 contract artifact kind = %q, want contract", fact.Kind)
	}
	evaluator := newContractPacketGateEvaluator(t)
	evaluationInput := fixture.input
	evaluationInput.GateID = "GATE-PLANNING-CONTRACTS-COMPLETE"
	evaluationInput.TransitionID = "PTR-PLAN-02"
	result, err := evaluator.Evaluate(context.Background(), evaluationInput)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusSatisfied {
		t.Fatalf("accepted S3 delivery must satisfy through its original complete artifact set despite stale documents[] row: status=%q missing=%v conflicts=%v", result.Status, result.Missing, result.Conflicts)
	}

	writeContractPacketSubjectFile(t, fixture.root, "docs/dev/contracts/SYNC-001.md", "# Synchronization contract\n> 状态：locked\n> 版本：v1.0.0\n\n## Changed after acceptance\n")
	// Disk changes after acceptance do not replace the recorded delivery facts.
	again, accepted, err := verifiedContractPacketFacts(fixture.input)
	if err != nil || !accepted || !reflect.DeepEqual(again, facts) {
		t.Fatalf("reproved accepted delivery from changed disk bytes: %v %v", accepted, err)
	}
	result, err = evaluator.Evaluate(context.Background(), evaluationInput)
	if err != nil || result.Status != StatusSatisfied {
		t.Fatalf("recorded delivery stopped qualifying: %+v %v", result, err)
	}
	for _, path := range []string{"docs/architecture/data-model/order.schema.json", "docs/architecture/data-model/order-example.json"} {
		fact, ok := byPath[path]
		if !ok || fact.ID != "shared-model:"+path || fact.Kind != "source" {
			t.Fatalf("accepted shared-model definition/example omitted: %s %+v", path, fact)
		}
	}
}

func TestVerifiedContractPacketFactsTreatsUnacceptedIssuanceAsMissing(t *testing.T) {
	fixture := newContractPacketSubjectFixture(t, false)
	entities := fixture.input.Snapshot.State["entities"].(map[string]any)
	entities["submissions"] = []any{}
	facts, accepted, err := verifiedContractPacketFacts(fixture.input)
	if err != nil || accepted || len(facts) != 0 {
		t.Fatalf("unaccepted S3 questionnaire must remain ordinary missing evidence: facts=%#v accepted=%v err=%v", facts, accepted, err)
	}
}

func TestPacketModeS3DoesNotUseUnacceptedLockedDiskOutputs(t *testing.T) {
	fixture := newContractPacketSubjectFixture(t, false)
	entities := fixture.input.Snapshot.State["entities"].(map[string]any)
	entities["submissions"] = []any{}

	evaluator := newContractPacketGateEvaluator(t)
	input := fixture.input
	input.GateID = "GATE-PLANNING-CONTRACTS-COMPLETE"
	input.TransitionID = "PTR-PLAN-02"
	result, err := evaluator.Evaluate(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusNotReady || result.ErrorCode != "" || len(result.Conflicts) != 0 {
		t.Fatalf("packet-mode S3 must wait for accepted delivery despite locked files: status=%q error=%q missing=%v conflicts=%v", result.Status, result.ErrorCode, result.Missing, result.Conflicts)
	}
	if len(result.Missing) != 1 || result.Missing[0] != "evidence:planning_contract_record" {
		t.Fatalf("missing = %#v, want the unaccepted S3 delivery", result.Missing)
	}
}

func TestVerifiedContractPacketSourcesRejectsChangedReceiptBytes(t *testing.T) {
	fixture := newContractPacketSubjectFixture(t, true)
	unknownPath := "docs/architecture/data-model/unregistered.json"
	unknownBytes := []byte(`{"source":"not in original S3 closure"}`)
	writeContractPacketSubjectFile(t, fixture.root, unknownPath, string(unknownBytes))

	var envelope map[string]any
	if err := json.Unmarshal(fixture.envelopeBytes, &envelope); err != nil {
		t.Fatal(err)
	}
	refs := envelope["subject_refs"].([]any)
	refs = append(refs, map[string]any{
		"path": unknownPath, "version": "v999", "sha256": workpacket.Digest(unknownBytes),
	})
	envelope["subject_refs"] = refs
	fixture.envelopeBytes, _ = json.Marshal(envelope)
	if err := os.WriteFile(filepath.Join(fixture.root, filepath.FromSlash(fixture.evidencePath)), fixture.envelopeBytes, 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := verifiedContractPacketSources(fixture.input); err == nil || !strings.Contains(err.Error(), "envelope") {
		t.Fatalf("accepted changed receipt bytes without its registered hash: %v", err)
	}
}

func TestVerifiedContractPacketSourcesDoesNotRecheckRecordedDirectoryMembership(t *testing.T) {
	t.Run("previously absent prototypes root", func(t *testing.T) {
		fixture := newContractPacketSubjectFixture(t, false)
		if _, ok := fixture.packet.Inputs.Directories["docs/design"]; !ok {
			t.Fatal("source packet did not record bounded parent that proves prototypes absence")
		}
		writeContractPacketSubjectFile(t, fixture.root, "docs/design/prototypes/orders/scenario-model.json", `{"module":"orders"}`)
		if _, err := verifiedContractPacketSources(fixture.input); err != nil {
			t.Fatal("rechecked directory membership after reception:", err)
		}
	})

	t.Run("new module under existing prototypes root", func(t *testing.T) {
		fixture := newContractPacketSubjectFixture(t, true)
		writeContractPacketSubjectFile(t, fixture.root, "docs/design/prototypes/refunds/scenario-model.json", `{"module":"refunds"}`)
		if _, err := verifiedContractPacketSources(fixture.input); err != nil {
			t.Fatalf("rechecked added source module: %v", err)
		}
	})

	t.Run("deleted original module", func(t *testing.T) {
		fixture := newContractPacketSubjectFixture(t, true)
		if err := os.RemoveAll(filepath.Join(fixture.root, "docs/design/prototypes/orders")); err != nil {
			t.Fatal(err)
		}
		if _, err := verifiedContractPacketSources(fixture.input); err != nil {
			t.Fatal("rechecked a recorded module package:", err)
		}
	})
}

func TestVerifiedContractPacketSourcesNeedsAcceptedAnswerHashAndEnvelopeReader(t *testing.T) {
	t.Run("answer hash", func(t *testing.T) {
		fixture := newContractPacketSubjectFixture(t, true)
		fixture.submissionRow["content_sha256"] = workpacket.Digest([]byte("different answer"))
		if _, err := verifiedContractPacketSources(fixture.input); err == nil || !strings.Contains(err.Error(), "professional answer") {
			t.Fatalf("accepted a submission with a different professional answer hash: %v", err)
		}
	})

	t.Run("read-only original envelope view", func(t *testing.T) {
		fixture := newContractPacketSubjectFixture(t, true)
		fixture.input.Files = readOnlyContractSubjectView{root: fixture.root}
		if _, err := verifiedContractPacketSources(fixture.input); err != nil {
			t.Fatalf("required directory scans for an accepted envelope: %v", err)
		}
	})
}

type contractPacketSubjectFixture struct {
	root          string
	input         Input
	packet        workpacket.ContractPacket
	envelopeBytes []byte
	evidencePath  string
	evidenceRow   map[string]any
	submissionRow map[string]any
}

func newContractPacketSubjectFixture(t *testing.T, withModule bool, acceptedModels ...bool) *contractPacketSubjectFixture {
	t.Helper()
	root := t.TempDir()
	reqPath := "docs/requirements/REQ-001.md"
	designPath := "docs/architecture/ARCHITECTURE-001.md"
	req := "# REQ-001\n> Status: locked\n> Version: v1.0.0\n"
	if withModule {
		req += "\n| ID | AC | Target |\n|:--|:--|:--|\n| AC-001 | Complete order | FR-001 |\n"
	} else {
		req += "\n| ID | AC | Target |\n|:--|:--|:--|\n| NFR-001 | Explicit scope | \u00a74 N/A |\n| AC-001 | No scenario needed | NFR-001 |\n"
	}
	design := "# Architecture\n> Status: locked\n> Version: v1.0.0\n\n[Decision](../design/decisions/ADR-001.md)\n"
	writeContractPacketSubjectFile(t, root, reqPath, req)
	writeContractPacketSubjectFile(t, root, designPath, design)
	writeContractPacketSubjectFile(t, root, "docs/design/decisions/ADR-001.md", "# ADR-001\n[Follow-up](ADR-002.md)\n")
	writeContractPacketSubjectFile(t, root, "docs/design/decisions/ADR-002.md", "# ADR-002\n")
	if withModule {
		writeContractPacketSubjectFile(t, root, "docs/design/prototypes/orders/scenario-model.json", `{"module":"orders"}`)
	}

	state := map[string]any{
		"schema_version": "1.2.0",
		"runtime_id":     "runtime-qualitygate-contract-sources",
		"configuration":  map[string]any{"optimization": map[string]any{"packet": true}},
		"baseline":       map[string]any{"generation": 1},
		"lifecycle":      map[string]any{"state": "planning", "phase": "contracts", "phase_revision": 3},
		"bound_req": map[string]any{
			"id": "REQ-001", "path": reqPath, "version": "v1.0.0", "sha256": workpacket.Digest([]byte(req)), "status": "locked",
			"workspace": map[string]any{"project_root": root},
		},
		"documents": []any{
			map[string]any{"id": "REQ-001", "kind": "req", "path": reqPath, "version": "v1.0.0", "sha256": workpacket.Digest([]byte(req)), "status": "locked", "generation": 1},
			map[string]any{"id": "ARCHITECTURE-001", "kind": "design", "path": designPath, "version": "v1.0.0", "sha256": workpacket.Digest([]byte(design)), "status": "locked", "generation": 1},
		},
	}
	inputs, err := runtimepkg.ResolveContractInputs(root, state, fileview.Disk{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	binding, _, err := runtimepkg.CurrentContractBinding(state)
	if err != nil {
		t.Fatal(err)
	}
	packet, err := workpacket.BuildContracts(binding, inputs)
	if err != nil {
		t.Fatal(err)
	}

	indexPath := "docs/dev/contracts/CONTRACTS-001.md"
	contractPath := "docs/dev/contracts/SYNC-001.md"
	indexBytes := []byte("# Contract index\n> REQ: REQ-001\n> Status: locked\n> Version: v1.0.0\n> Shared model policy: none\n> Shared model reason: This fixture changes no shared model.\n\n" +
		"## \u5408\u540c\u6e05\u5355\n\n| \u5408\u540cID | \u7c7b\u578b | \u8d1f\u8d23\u4eba | \u72b6\u6001 | \u7248\u672c | \u8054\u8c03\u70b9 |\n|:--|:--|:--|:--|:--|:--|\n\n" +
		"## \u8054\u8c03\u70b9\u77e9\u9635\n\n| \u8054\u8c03\u70b9 | \u524d\u7aef\u5408\u540c | \u540e\u7aef\u5408\u540c | \u5951\u7ea6\u6587\u6863 | \u72b6\u6001 |\n|:--|:--|:--|:--|:--|\n| SYNC-001 | N/A | N/A | SYNC-001.md | locked |\n\n" +
		"## \u9700\u6c42\u8986\u76d6\u77e9\u9635\n\n| REQ source_ref | Rule \u2192 CASE \u2192 Story \u2192 PATH \u2192 Spec | FE \u5408\u540c\u6761\u6b3e | BE \u5408\u540c\u6761\u6b3e | SYNC \u6761\u6b3e |\n|:--|:--|:--|:--|:--|\n")
	contractBytes := []byte("# Synchronization contract\n> \u72b6\u6001\uff1alocked\n> \u7248\u672c\uff1av1.0.0\n\n## Contract clauses\n")
	writeContractPacketSubjectFile(t, root, indexPath, string(indexBytes))
	writeContractPacketSubjectFile(t, root, contractPath, string(contractBytes))
	answer := workpacket.ContractAnswer{
		Conclusion:    stringPointer("pass"),
		IndexPath:     stringPointer(indexPath),
		ContractPaths: []string{contractPath},
		Applicability: map[string]workpacket.ContractApplicability{
			"FE":   {Disposition: "not_applicable", Rationale: "No front-end contract is required."},
			"BE":   {Disposition: "not_applicable", Rationale: "No back-end contract is required."},
			"SYNC": {Disposition: "required", Rationale: "The synchronization contract is delivered."},
		},
	}
	artifacts := []workpacket.Reference{
		{Path: indexPath, Version: "v1.0.0", SHA256: workpacket.Digest(indexBytes)},
		{Path: contractPath, Version: "v1.0.0", SHA256: workpacket.Digest(contractBytes)},
	}
	if len(acceptedModels) > 0 && acceptedModels[0] {
		for path, data := range map[string]string{"docs/architecture/data-model/order.schema.json": `{"type":"object"}`, "docs/architecture/data-model/order-example.json": `{"id":"accepted-example"}`} {
			writeContractPacketSubjectFile(t, root, path, data)
			artifacts = append(artifacts, workpacket.Reference{Path: path, SHA256: workpacket.Digest([]byte(data))})
		}
	}

	evidenceID := "evidence-s3-contract-sources"
	evidenceValue, err := workpacket.ContractEvidence(packet, evidenceID, artifacts, answer, time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	envelopeBytes, err := json.Marshal(evidenceValue)
	if err != nil {
		t.Fatal(err)
	}
	evidencePath := ".claude/evidence/planning-contract.json"
	writeContractPacketSubjectFile(t, root, evidencePath, string(envelopeBytes))
	answerDigest, err := workpacket.ContractAnswerDigest(answer)
	if err != nil {
		t.Fatal(err)
	}
	key := workpacket.Digest([]byte("submission:" + packet.ID))
	submission := runtimepkg.SubmissionRecord{
		Key: key, PacketID: packet.ID, Owner: "Main", ContentHash: answerDigest, EvidenceID: evidenceID,
		FollowupID: workpacket.Digest([]byte(key + ":after-domain")), ControlStatus: "pending",
		OriginState: "planning", OriginPhase: "contracts", OriginEntry: packet.Binding.StageEntry,
	}
	packetRow := jsonObject(t, packet)
	submissionRow := jsonObject(t, submission)
	evidenceRow := map[string]any{
		"id": evidenceID, "kind": "planning_contract", "path": evidencePath, "sha256": workpacket.Digest(envelopeBytes),
		"status": "valid", "baseline_generation": 1, "responsibility_id": "Orchestrator",
		"produced_by": []any{"Main"}, "invalidated_by": nil,
	}
	state["entities"] = map[string]any{
		"work_packets": []any{packetRow}, "submissions": []any{submissionRow},
	}
	state["evidence"] = []any{evidenceRow}
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0700); err != nil {
		t.Fatal(err)
	}
	stateBytes, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), stateBytes, 0600); err != nil {
		t.Fatal(err)
	}

	return &contractPacketSubjectFixture{
		root:   root,
		input:  Input{Root: root, Snapshot: runtimepkg.Snapshot{State: state}, Files: fileview.Disk{Root: root}},
		packet: packet, envelopeBytes: envelopeBytes, evidencePath: evidencePath,
		evidenceRow: evidenceRow, submissionRow: submissionRow,
	}
}

func writeContractPacketSubjectFile(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func jsonObject(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	return object
}

func stringPointer(value string) *string { return &value }

func containsContractSource(sources []workpacket.Subject, path string) bool {
	for _, source := range sources {
		if source.Path == path {
			return true
		}
	}
	return false
}

func verifiedContractPacketSources(input Input) ([]documentFact, error) {
	verified, accepted, err := readAcceptedContractSubmission(input)
	if err != nil {
		return nil, err
	}
	if !accepted {
		return nil, fmt.Errorf("current S3 packet has no accepted planning_contract submission")
	}
	return contractPacketSourceFacts(verified.packet, verified.subjects)
}

func newContractPacketGateEvaluator(t *testing.T) *Engine {
	t.Helper()
	catalog, err := transition.LoadCatalog("../..")
	if err != nil {
		t.Fatal(err)
	}
	registry, err := NewRegistry(catalog)
	if err != nil {
		t.Fatal(err)
	}
	return NewEvaluator(registry)
}

type readOnlyContractSubjectView struct{ root string }

func (v readOnlyContractSubjectView) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(filepath.Join(v.root, filepath.FromSlash(path)))
}

func TestVerifiedContractPacketFactsReportsMissingReceiptAndCurrentREQ(t *testing.T) {
	for _, mode := range []string{"missing receipt", "registered hash mismatch", "different current REQ"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newContractPacketSubjectFixture(t, false)
			switch mode {
			case "missing receipt":
				if err := os.Remove(filepath.Join(fixture.root, fixture.evidencePath)); err != nil {
					t.Fatal(err)
				}
			case "registered hash mismatch":
				fixture.evidenceRow["sha256"] = workpacket.Digest([]byte("other receipt"))
			case "different current REQ":
				fixture.input.Snapshot.State["bound_req"].(map[string]any)["version"] = "v2.0.0"
			}
			if _, accepted, err := verifiedContractPacketFacts(fixture.input); !accepted || err == nil {
				t.Fatalf("lost actual receipt/current responsibility mismatch: %v %v", accepted, err)
			}
		})
	}
}

func TestVerifiedContractPacketFactsReportsActualGitRefMovement(t *testing.T) {
	fixture := newContractPacketSubjectFixture(t, false)
	git := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("git", append([]string{"-C", fixture.root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("fixture git: %v %s", err, out)
		}
	}
	git("init", "-q", "-b", "accepted")
	git("add", "docs")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "Accepted professional inputs")
	view, err := fileview.New(fixture.root, "refs/heads/accepted", []fileview.Rule{{Path: ".", Source: "git_tree"}, {Path: ".claude", Source: "disk"}})
	if err != nil {
		t.Fatal(err)
	}
	fixture.input.Files = view
	if _, accepted, err := verifiedContractPacketFacts(fixture.input); !accepted || err != nil {
		t.Fatal("accepted record unavailable before ref movement", accepted, err)
	}
	writeContractPacketSubjectFile(t, fixture.root, "docs/changed.md", "# Actual later committed work\n")
	git("add", "docs/changed.md")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "Later committed professional work")
	if _, accepted, err := verifiedContractPacketFacts(fixture.input); !accepted || err == nil || !strings.Contains(err.Error(), "file source moved: refs/heads/accepted") {
		t.Fatalf("actual ref movement lost its original feedback: %v %v", accepted, err)
	}
}
