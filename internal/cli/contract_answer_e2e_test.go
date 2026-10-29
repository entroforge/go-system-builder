package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func newContractAnswerFixture(t *testing.T, configure ...func(string)) (string, workpacket.ContractPacket) {
	t.Helper()
	root, design := newDesignFixture(t, "none")
	return contractAnswerFromDesignFixture(t, root, design, configure...)
}

func contractAnswerFromDesignFixture(t *testing.T, root string, design workpacket.DesignPacket, configure ...func(string)) (string, workpacket.ContractPacket) {
	t.Helper()
	stageJSON(t, root, design.AnswerPath, designDraft(design))
	stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--agent", "Main", "--path", design.AnswerPath)
	settleMainPlanningFollowups(t, root)
	writeContractSource(t, root, "docs/design/README.md", "# Design sources\n")
	writeContractSource(t, root, "docs/dev/contracts/CONTRACTS-039.md", "# Contract batch\n> 需求：REQ-039\n> 状态：locked\n> 版本：v1.0.0\n> Shared model policy: none\n> Shared model reason: This fixture delivers no shared wire model.\n\n## 合同清单\n\n| 合同ID | 类型 | 负责人 | 状态 | 版本 | 联调点 |\n|:---|:---|:---|:---|:---|:---|\n| BE-039 | 后端 | Main | locked | v1.0.0 | N/A |\n\n## 需求覆盖矩阵\n\n| REQ source_ref | FE 合同条款 | BE 合同条款 | SYNC 条款 |\n|:---|:---|:---|:---|\n")
	writeContractSource(t, root, "docs/dev/contracts/BE-039.md", "# Backend contract\n> 状态：locked\n> 版本：v1.0.0\n\n## Contract clauses\n\n| 本合同条款 | 条款 |\n|:---|:---|\n| §1 | Fixture service behavior |\n")
	for _, setup := range configure {
		setup(root)
	}
	// Only authored artifacts are committed in this temporary fixture.
	// The original source declarations determine which bytes consumers read.
	fixtures.CommitCurrentFixture(t, root)
	var p workpacket.ContractPacket
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "issue", "--root", root)), &p); err != nil {
		t.Fatal(err)
	}
	return root, p
}

func settleMainPlanningFollowups(t *testing.T, root string) {
	t.Helper()
	snap := stageSnapshot(t, root)
	rows, _ := snap.State["entities"].(map[string]any)["submissions"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["owner"] == "Main" && row["control_status"] == "pending" {
			s, err := submission.New(root)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Recover(context.Background(), "Main", 2); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
}

func writeContractSource(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func completedContractDraft(p workpacket.ContractPacket) workpacket.ContractDraft {
	pass, index := "pass", "docs/dev/contracts/CONTRACTS-039.md"
	a := p.Draft
	a.Answer = workpacket.ContractAnswer{Conclusion: &pass, IndexPath: &index, ContractPaths: []string{"docs/dev/contracts/BE-039.md"}, Applicability: map[string]workpacket.ContractApplicability{"FE": {Disposition: "not_applicable", Rationale: "The fixture requirement has no client."}, "BE": {Disposition: "required", Rationale: "This batch specifies the service."}, "SYNC": {Disposition: "not_applicable", Rationale: "There is no separate client/provider boundary in this fixture."}}}
	return a
}

func TestContractAnswerMainHookOriginalRouteAndHistoricalReceipt(t *testing.T) {
	root, p := newContractAnswerFixture(t, func(root string) {
		path := "docs/dev/contracts/CONTRACTS-039.md"
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		writeContractSource(t, root, path, string(data)+"| REQ-039/NFR-001 | — | BE-039 §1 | — |\n")
		for _, rel := range []string{"docs/dev/tasks/TASK-template.md", "docs/rules/dispatch-plan.md"} {
			data, err := os.ReadFile(filepath.Join(fixtures.RepoRoot(t), rel))
			if err != nil {
				t.Fatal(err)
			}
			writeContractSource(t, root, rel, string(data))
		}
	})
	assertDesignCheckpoint(t, root)
	stageJSON(t, root, p.AnswerPath, completedContractDraft(p))
	body, _ := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(root, p.AnswerPath)}, "cwd": root})
	var out, errout bytes.Buffer
	if code := cli.Run([]string{"hook", "--root", root, "--event", "PostToolUse"}, bytes.NewReader(body), &out, &errout); code != 0 {
		t.Fatalf("S3 Hook %d %s %s", code, out.String(), errout.String())
	}
	if !strings.Contains(out.String(), "S3 SUBMISSION RECEIPT") {
		t.Fatal(out.String())
	}
	snap := stageSnapshot(t, root)
	record, found, err := runtime.FindSubmission(snap.State, p.ID)
	if err != nil || !found || record.ControlStatus != "advanced" {
		service, _ := submission.New(root)
		if service != nil {
			catalog, _ := transition.LoadCatalog(root)
			registry, _ := qualitygate.NewRegistry(catalog)
			gate, gateErr := qualitygate.NewEvaluator(registry).Evaluate(context.Background(), qualitygate.Input{Root: root, Snapshot: snap, TransitionID: "PTR-PLAN-02", GateID: "GATE-PLANNING-CONTRACTS-COMPLETE", Files: service.Files})
			t.Logf("original S3 gate: %+v %v", gate, gateErr)
		}
		t.Fatalf("original S3 reception: %+v %v %s", record, err, out.String())
	}
	life := snap.State["lifecycle"].(map[string]any)
	if life["state"] != "planning" || life["phase"] != "tasks" {
		t.Fatalf("original S3 route: %+v", life)
	}
	prepared := false
	for _, raw := range snap.State["entities"].(map[string]any)["work_packets"].([]any) {
		row := raw.(map[string]any)
		if row["template_id"] == "s4-task-planning-v1" {
			prepared = true
			for _, key := range []string{"view_path", "answer_path"} {
				path := row[key].(string)
				if _, err := os.Stat(filepath.Join(root, path)); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(out.String(), path) {
					t.Fatalf("next S4 path absent from receipt: %s", out.String())
				}
			}
		}
	}
	if !prepared {
		t.Fatalf("successful S3 outlet did not prepare S4: %s", out.String())
	}

	registered := map[string]bool{}
	for _, raw := range snap.State["documents"].([]any) {
		row := raw.(map[string]any)
		if row["kind"] == "contract" {
			registered[row["path"].(string)] = true
		}
	}
	if !registered["docs/dev/contracts/CONTRACTS-039.md"] || !registered["docs/dev/contracts/BE-039.md"] {
		t.Fatal("original PTR did not register complete batch", registered)
	}
	assertDesignCheckpoint(t, root)
	stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--agent", "Main", "--path", p.AnswerPath)
	if stageSnapshot(t, root).State["revision"] != snap.State["revision"] {
		t.Fatal("replay changed original Runtime")
	}
	// Historical receipt must reconstruct the original professional facts;
	// current authoring bytes can no longer dictate its applicability/hash.
	writeContractSource(t, root, p.AnswerPath, "corrupt mutable answer\n")
	var receipt submission.Receipt
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "receipt", "--root", root, "--agent", "Main", "--packet", p.ID)), &receipt); err != nil {
		t.Fatal(err)
	}
	if !receipt.Accepted || !receipt.WorkComplete {
		t.Fatalf("original historical receipt: %+v", receipt)
	}
}

func TestContractAnswerPendingAndOriginalPairRecovery(t *testing.T) {
	root, p := newContractAnswerFixture(t)
	stageJSON(t, root, p.AnswerPath, completedContractDraft(p))
	s, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = &cancelAfterDurableReception{Context: ctx, statePath: s.StatePath, packetID: p.ID, cancel: cancel}
	r, err := s.SaveContracts(ctx, p.AnswerPath)
	if err != nil || !r.Accepted || !r.WorkComplete || len(r.WaitingOn) == 0 {
		t.Fatalf("pending S3: %+v %v", r, err)
	}
	before := stageSnapshot(t, root)
	old, found, err := runtime.FindSubmission(before.State, p.ID)
	if err != nil || !found || old.ControlStatus != "pending" {
		t.Fatalf("pending original reception: %+v %v", old, err)
	}
	path, pair := latestStageCheckpoint(t, root)
	for _, mutation := range []string{"replaced_without_decision", "wrong_owner", "wrong_professional_digest"} {
		t.Run(mutation, func(t *testing.T) {
			state := stageSnapshot(t, root).State
			rows := state["entities"].(map[string]any)["submissions"].([]any)
			for _, raw := range rows {
				row := raw.(map[string]any)
				if row["packet_id"] != p.ID {
					continue
				}
				switch mutation {
				case "replaced_without_decision":
					row["control_status"] = "replaced"
				case "wrong_owner":
					row["owner"] = "other"
				case "wrong_professional_digest":
					row["content_sha256"] = strings.Repeat("0", 64)
				}
			}
			if err := runtime.ValidateCheckpointIndices(root, state, pair.JournalBytes); err == nil {
				t.Fatal("forged S3 checkpoint accepted")
			}
		})
	}
	damageStagePair(t, root)
	plan := checkpointPlan(t, root, path, pair.State)
	stageCLI(t, "runtime", "recover", "apply", "--root", root, "--plan", plan, "--approved-by", "operator")
	if !reflect.DeepEqual(stageSnapshot(t, root).State, before.State) {
		t.Fatal("S2/S3 original pair facts changed during restore")
	}
	stageCLI(t, "runtime", "stage-answer", "recover", "--root", root, "--agent", "Main")
	after := stageSnapshot(t, root)
	next, _, err := runtime.FindSubmission(after.State, p.ID)
	if err != nil || next.ControlStatus != "advanced" || old.Key != next.Key || old.EvidenceID != next.EvidenceID || old.ContentHash != next.ContentHash || len(after.State["evidence"].([]any)) != len(before.State["evidence"].([]any)) {
		t.Fatalf("recovery changed professional reception: %+v %v", next, err)
	}
	assertDesignCheckpoint(t, root)
	stageCLI(t, "runtime", "stage-answer", "recover", "--root", root, "--agent", "Main")
	if stageSnapshot(t, root).State["revision"] != after.State["revision"] {
		t.Fatal("recovered followup replay changed Runtime")
	}
}

func TestContractAnswerConditionalADRPassesOriginalGate(t *testing.T) {
	root, p := newContractAnswerFixture(t, func(root string) {
		writeContractSource(t, root, "docs/design/decisions/ADR-039.md", "# Decision\n> 状态：accepted\n> 版本：v1.0.0\n> 关联需求：REQ-039\n\nAn explicitly accepted decision for this fixture.\n")
	})
	if len(p.Inputs.Sources) != 1 || p.Inputs.Sources[0].Path != "docs/design/decisions/ADR-039.md" {
		t.Fatalf("accepted ADR omitted: %+v", p.Inputs)
	}
	stageJSON(t, root, p.AnswerPath, completedContractDraft(p))
	stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--agent", "Main", "--path", p.AnswerPath)
	snap := stageSnapshot(t, root)
	record, found, err := runtime.FindSubmission(snap.State, p.ID)
	if err != nil || !found || record.ControlStatus != "advanced" {
		t.Fatalf("S3 conditional original gate: %+v %v", record, err)
	}
	assertDesignCheckpoint(t, root)
}

func TestContractAnswerRejectsDraftAndSourceDriftBeforeReception(t *testing.T) {
	for _, mode := range []string{"draft", "unknown_contract", "source_drift", "semantic_token"} {
		t.Run(mode, func(t *testing.T) {
			root, p := newContractAnswerFixture(t, func(root string) {
				if mode == "semantic_token" {
					path := "docs/dev/contracts/BE-039.md"
					data, err := os.ReadFile(filepath.Join(root, path))
					if err != nil {
						t.Fatal(err)
					}
					writeContractSource(t, root, path, string(data)+"\nStory S-999 is required.\n")
				}
			})
			stageJSON(t, root, p.AnswerPath, completedContractDraft(p))
			switch mode {
			case "draft":
				writeContractSource(t, root, "docs/dev/contracts/BE-039.md", "# Draft\n> Status: draft\n> Version: v1.0.0\n")
			case "unknown_contract":
				writeContractSource(t, root, "docs/dev/contracts/FE-039.md", "# Extra draft\n> Status: draft\n> Version: v1.0.0\n")
			case "source_drift":
				writeContractSource(t, root, "docs/design/prototypes/new-module/scenario-model.json", "{}\n")
			}
			before := stageSnapshot(t, root)
			s, err := submission.New(root)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "semantic_token" {
				artifacts, err := runtime.ContractArtifacts(root, before.State, p, completedContractDraft(p).Answer, s.Files)
				if err != nil {
					t.Fatalf("semantic-only fixture failed before Writer: %v", err)
				}
				// A raw recording cannot skip the real consumer's professional check.
				envelope, err := workpacket.ContractEvidence(p, "ev-missing-professional-check", artifacts, completedContractDraft(p).Answer, time.Now().UTC())
				if err != nil {
					t.Fatal(err)
				}
				path := ".claude/evidence/missing-professional-check.json"
				stageJSON(t, root, path, envelope)
				answerJSON, _ := json.Marshal(completedContractDraft(p))
				_, rawErr := runtime.RecordEvidence(root, s.StatePath, s.JournalPath, runtime.EvidenceRequest{ExpectedRevision: -1, ID: "ev-missing-professional-check", Kind: "planning_contract", Path: path, ProducedBy: []string{"Main"}, ResponsibilityID: "Orchestrator", Validator: s.Validator, Submission: &runtime.SubmissionRequest{PacketID: p.ID, Owner: "Main", AnswerJSON: answerJSON, Files: s.Files}})
				if rawErr == nil || !strings.Contains(rawErr.Error(), "实际合同专业检查") || !strings.Contains(rawErr.Error(), p.AnswerPath) || stageSnapshot(t, root).Revision != before.Revision {
					t.Fatalf("raw reception skipped the professional check or lost the current path: %v", rawErr)
				}
			}
			r, saveErr := s.SaveContracts(context.Background(), p.AnswerPath)
			if mode == "semantic_token" && (saveErr == nil || !strings.Contains(saveErr.Error(), "S-999")) {
				t.Fatalf("expected Writer semantic token error: %+v %v", r, saveErr)
			}
			if saveErr == nil || r.Accepted {
				t.Fatalf("invalid S3 accepted: %+v %v", r, saveErr)
			}
			if r.WorkComplete || !strings.Contains(r.NextAction, filepath.Join(root, p.AnswerPath)) || r.Error != saveErr.Error() {
				t.Fatalf("unreceived S3 must retain the actual error and current answer: %+v %v", r, saveErr)
			}
			after := stageSnapshot(t, root)
			if before.Revision != after.Revision || len(before.State["evidence"].([]any)) != len(after.State["evidence"].([]any)) {
				t.Fatal("rejected S3 changed original reception")
			}
		})
	}
}

func TestContractAnswerCommittedDesignCorrectionAfterS2UsesOriginalOutlet(t *testing.T) {
	root, design := newDesignFixture(t, "none")
	adr := "docs/design/decisions/ADR-005.md"
	writeContractSource(t, root, adr, "> 状态: locked\n> 关联需求: `REQ-039`\n\nThe operator has accepted this direction; the ADR status spelling will be corrected.\n")
	fixtures.CommitCurrentFixture(t, root)
	stageJSON(t, root, design.AnswerPath, designDraft(design))
	if out := observedQuestionnaireHook(t, root, "Edit", map[string]any{"file_path": filepath.Join(root, design.AnswerPath)}, nil); !strings.Contains(out, "S2 SUBMISSION RECEIPT") {
		t.Fatal(out)
	}
	before := stageSnapshot(t, root)
	s2, accepted, err := runtime.FindSubmission(before.State, design.ID)
	if err != nil || !accepted || s2.ControlStatus != "advanced" {
		t.Fatal("S2 actual reception was not recorded", s2, err)
	}
	if before.State["lifecycle"].(map[string]any)["phase"] != "contracts" {
		t.Fatal("S2 original transition did not finish")
	}
	for _, raw := range before.State["entities"].(map[string]any)["work_packets"].([]any) {
		if raw.(map[string]any)["template_id"] == "s3-contract-v1" {
			t.Fatal("fixture already issued S3 before the professional correction")
		}
	}
	journal, err := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	oldDocuments := before.State["documents"]
	arch := "docs/architecture/ARCHITECTURE-039.md"
	data, err := os.ReadFile(filepath.Join(root, arch))
	if err != nil {
		t.Fatal(err)
	}
	planningBoundaryEvent(t, root, "PreToolUse", "Bash", "", false)
	current := append(data, []byte("\nThe accepted ADR direction is recorded with status accepted.\n")...)
	writeContractSource(t, root, arch, string(current))
	writeContractSource(t, root, adr, "> 状态: accepted\n> 关联需求: `REQ-039`\n\nThe same operator direction is accepted.\n")
	writeContractSource(t, root, "docs/dev/contracts/CONTRACTS-039.md", "# Contract batch\n> 需求：REQ-039\n> 状态：locked\n> 版本：v1.0.0\n> Shared model policy: none\n> Shared model reason: This fixture delivers no shared wire model.\n\n## 合同清单\n\n| 合同ID | 类型 | 负责人 | 状态 | 版本 | 联调点 |\n|:---|:---|:---|:---|:---|:---|\n| BE-039 | 后端 | Main | locked | v1.0.0 | N/A |\n\n## 需求覆盖矩阵\n\n| REQ source_ref | FE 合同条款 | BE 合同条款 | SYNC 条款 |\n|:---|:---|:---|:---|\n| REQ-039/NFR-001 | — | BE-039 §1 | — |\n")
	writeContractSource(t, root, "docs/dev/contracts/BE-039.md", "# Backend contract\n> 状态：locked\n> 版本：v1.0.0\n\n## Contract clauses\n\n| 本合同条款 | 条款 |\n|:---|:---|\n| §1 | Fixture service behavior |\n")
	fixtures.CommitCurrentFixture(t, root)
	// A normal successful tool observes the real commit and finishes the original
	// issuer. No issue/recover command or historical document rewrite is used.
	planningBoundaryEvent(t, root, "PostToolUse", "Bash", "", false)
	prepared := stageSnapshot(t, root)
	var packet workpacket.ContractPacket
	for _, raw := range prepared.State["entities"].(map[string]any)["work_packets"].([]any) {
		row := raw.(map[string]any)
		if row["template_id"] == "s3-contract-v1" {
			packet, _, err = runtime.FindContractPacket(prepared.State, row["packet_id"].(string))
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if packet.ID == "" {
		t.Fatal("committed professional correction did not prepare S3")
	}
	if len(packet.Inputs.Designs) != 1 || packet.Inputs.Designs[0].SHA256 != workpacket.Digest(current) {
		t.Fatal("S3 did not use current committed design", packet.Inputs.Designs)
	}
	if !reflect.DeepEqual(oldDocuments, prepared.State["documents"]) {
		t.Fatal("original S2 documents were rewritten")
	}
	afterJournal, err := os.ReadFile(filepath.Join(root, ".claude/loop-events.jsonl"))
	if err != nil || !bytes.HasPrefix(afterJournal, journal) {
		t.Fatal("original S2 journal history changed", err)
	}
	stageJSON(t, root, packet.AnswerPath, completedContractDraft(packet))
	out := observedQuestionnaireHook(t, root, "Edit", map[string]any{"file_path": filepath.Join(root, packet.AnswerPath)}, nil)
	received := stageSnapshot(t, root)
	record, found, err := runtime.FindSubmission(received.State, packet.ID)
	if err != nil || !found || record.ControlStatus != "advanced" {
		service, _ := submission.New(root)
		catalog, _ := transition.LoadCatalog(root)
		registry, _ := qualitygate.NewRegistry(catalog)
		gate, gateErr := qualitygate.NewEvaluator(registry).Evaluate(context.Background(), qualitygate.Input{Root: root, Snapshot: received, TransitionID: "PTR-PLAN-02", GateID: "GATE-PLANNING-CONTRACTS-COMPLETE", Files: service.Files})
		t.Logf("original S3 gate diagnostic: %+v %v", gate, gateErr)
		for _, raw := range received.State["evidence"].([]any) {
			row := raw.(map[string]any)
			if row["id"] != record.EvidenceID {
				continue
			}
			evidencePath := row["path"].(string)
			evidence, readErr := service.Files.ReadFile(evidencePath)
			t.Logf("actual original evidence read: path=%s err=%v digest=%s index=%v", evidencePath, readErr, workpacket.Digest(evidence), row["sha256"])
			var envelope struct {
				Subjects []workpacket.Subject `json:"subject_refs"`
			}
			json.Unmarshal(evidence, &envelope)
			for _, subject := range envelope.Subjects {
				if subject.Path == arch {
					t.Logf("actual S3 architecture subject: %+v; original S2 documents: %+v; projected conditional sources: %+v", subject, oldDocuments, packet.Inputs.Sources)
				}
			}
		}

		t.Fatalf("original S3 consumer did not receive/advance: %+v %v %s", record, err, out)
	}
	previous, _, err := runtime.FindSubmission(received.State, design.ID)
	if err != nil || !reflect.DeepEqual(s2, previous) {
		t.Fatal("historical S2 reception changed", previous, err)
	}
}
