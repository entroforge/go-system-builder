package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestS4LargeReportExceedsReplayAndDynamicReadLimits(t *testing.T) {
	root, packet := s4HandoffFixture(t)
	// Each actual document remains below TaskPlanningArtifacts' existing 4 MiB
	// budget. SemVer build metadata permits a nonempty ASCII alphanumeric
	// identifier of this length; no large prose is added to the author's answer.
	const metadataBytes = 2200 << 10
	versions := map[string]string{
		"docs/dev/tasks/TASK-039-02.md": "v1.0.0+" + strings.Repeat("a", metadataBytes),
		"docs/dev/tasks/TASK-039-03.md": "v1.0.0+" + strings.Repeat("b", metadataBytes),
	}
	for path, version := range versions {
		content := "# Task\n> Status: cancelled\n> Version: " + version + "\n> Source REQ refs: REQ-039\n"
		if len(content) >= 4<<20 {
			t.Fatal("fixture exceeds the original per-TASK budget")
		}
		writeContractSource(t, root, path, content)
	}
	fixtures.CommitCurrentFixture(t, root)
	draft := completedTaskPlanningDraft(packet)
	stageJSON(t, root, packet.AnswerPath, draft)
	if answer := mustStageFile(t, filepath.Join(root, packet.AnswerPath)); len(answer) >= 256<<10 {
		t.Fatalf("large fixture leaked into the author's answer: %d bytes", len(answer))
	}
	// Exercise the original reception and controller without treating a
	// race-instrumented, in-process hook deadline as a report-size limit. The
	// separate natural-hook tests cover the real PostToolUse entry and handoff.
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.SaveTaskPlanning(context.Background(), packet.AnswerPath)
	if err != nil || !receipt.Accepted {
		t.Fatalf("large report reception failed: accepted=%v err=%v", receipt.Accepted, err)
	}
	// Exercise S5 preparation through its original recovery/issue entry;
	// this capacity test does not assert that a race-instrumented Hook can
	// perform the entire large transition within one event budget.
	stageCLI(t, "runtime", "stage-answer", "issue", "--root", root)
	report := s4AssertNaturalAcceptedReportAndSuccessors(t, root, packet, "", false, false)
	limit := workpacket.TaskPlanningEvidenceReadLimit(packet)
	if len(report) <= 4<<20 || int64(len(report)) <= limit {
		t.Fatalf("natural report is too small: bytes=%d formerReplay=%d dynamic=%d", len(report), 4<<20, limit)
	}
	var envelope struct {
		Subjects []workpacket.Subject          `json:"subject_refs"`
		Answer   workpacket.TaskPlanningAnswer `json:"task_planning_answer"`
	}
	if err := json.Unmarshal(report, &envelope); err != nil {
		t.Fatal(err)
	}
	actual := map[string]string{}
	for _, subject := range envelope.Subjects {
		actual[subject.Path] = subject.Version
	}
	for path, version := range versions {
		if actual[path] != version {
			t.Fatalf("actual TASK version was truncated: %s want=%d got=%d", path, len(version), len(actual[path]))
		}
	}
	if len(envelope.Answer.TaskPaths) != 1 || envelope.Answer.TaskPaths[0] != draft.Answer.TaskPaths[0] {
		t.Fatal("cancelled source inventory was inserted into the author's answer")
	}
	before := stageSnapshot(t, root).Revision
	receipt, err = service.SaveTaskPlanning(context.Background(), packet.AnswerPath)
	if err != nil || !receipt.Accepted {
		t.Fatalf("full natural report replay failed: accepted=%v err=%v", receipt.Accepted, err)
	}
	if stageSnapshot(t, root).Revision != before {
		t.Fatal("replay repeated reception or transition")
	}
	t.Logf("natural report=%d bytes; old replay=%d; dynamic=%d; each TASK version=%d; author lists one complete TASK", len(report), 4<<20, limit, len(versions["docs/dev/tasks/TASK-039-02.md"]))
}

func TestS4NaturalSaveWithEntireEvidenceRootOccupied(t *testing.T) {
	for _, kind := range []string{"file", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			unknown := []byte("unknown evidence root owner; retain these bytes\n")
			var occupied, target string
			// Occupy the root before the original S2 issue/reception, so S2 and S3
			// naturally publish elsewhere. No accepted historical file is removed.
			root, design := newDesignFixture(t, "none", func(root string) {
				occupied = filepath.Join(root, ".claude/evidence")
				if kind == "file" {
					if err := os.WriteFile(occupied, unknown, 0600); err != nil {
						t.Fatal(err)
					}
				} else {
					target = t.TempDir()
					if err := os.WriteFile(filepath.Join(target, "unknown.txt"), unknown, 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(target, occupied); err != nil {
						if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.ENOTSUP) {
							t.Skipf("symlink creation is unavailable on this platform: %v", err)
						}
						t.Fatalf("create evidence root symlink: %v", err)
					}
				}
			})
			root, contracts := contractAnswerFromDesignFixture(t, root, design, func(root string) {
				// The same real authored method/context sources as s4HandoffFixture.
				index := "docs/dev/contracts/CONTRACTS-039.md"
				data := mustStageFile(t, filepath.Join(root, index))
				writeContractSource(t, root, index, string(data)+"| REQ-039/NFR-001 | — | BE-039 §1 | — |\n")
				for _, rel := range []string{"docs/dev/tasks/TASK-template.md", "docs/control/agent-protocol.md", "docs/rules"} {
					copyS6RecordContextTree(t, filepath.Join(fixtures.RepoRoot(t), rel), filepath.Join(root, rel))
				}
				for _, rel := range []string{"agents/document-verifier.md", "skills/document-verification", "skills/specification-planning", "skills/api-contracts", "skills/testing-strategy", "skills/agent-dispatch"} {
					copyS6RecordContextTree(t, filepath.Join(fixtures.RepoRoot(t), rel), filepath.Join(root, ".claude", rel))
				}
			})
			stageJSON(t, root, contracts.AnswerPath, completedContractDraft(contracts))
			stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--agent", "Main", "--path", contracts.AnswerPath)
			settleMainPlanningFollowups(t, root)
			// Reuse the complete TASK and dispatch index text from the established
			// fixture, with the same professional closure and only one active TASK.
			writeContractSource(t, root, "docs/dev/tasks/TASK-039-01.md", "# Task\n> Status: complete\n> Version: v1.0.0\n> Source REQ refs: REQ-039\n> Primary contract: BE-039\n\n## 3. Delivered Clauses\n\n| Contract | Delivered clauses |\n|:--|:--|\n| BE-039 | §1 |\n\n## 4. Scope\n\n| Type | Paths / Commands |\n|:--|:--|\n| prospective write paths | `internal/fixture/service.go` |\n\n## 7. Closing Contract\n\n```text\nassert BE-039 §1 == satisfied\n```\n")
			writeContractSource(t, root, "docs/dev/tasks/index-REQ-039.md", "# REQ-039 dispatch\n> REQ: REQ-039\n> Dispatch policy: waves-v1\n> Revision: 1\n> Status: complete\n\n## W1 First wave\n\n- [ ] [TASK-039-01](TASK-039-01.md)\n")
			fixtures.CommitCurrentFixture(t, root)
			var packet workpacket.TaskPlanningPacket
			if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "issue", "--root", root)), &packet); err != nil {
				t.Fatal(err)
			}
			// Preserve every upstream report byte across S4 reception and its outlet.
			historical := map[string][]byte{}
			for _, raw := range stageSnapshot(t, root).State["evidence"].([]any) {
				row := raw.(map[string]any)
				if row["kind"] == "planning_design" || row["kind"] == "planning_contract" {
					path := row["path"].(string)
					if !strings.HasPrefix(path, ".claude/evidence-alt-") {
						t.Fatalf("upstream did not naturally use alternate root: %s", path)
					}
					historical[path] = mustStageFile(t, filepath.Join(root, path))
				}
			}
			if len(historical) != 2 {
				t.Fatalf("want original S2 and S3 reports, got %d", len(historical))
			}
			stageJSON(t, root, packet.AnswerPath, completedTaskPlanningDraft(packet))
			output := s4NaturalAcceptancePost(t, root, packet)
			s4AssertNaturalAcceptedReportAndSuccessors(t, root, packet, output, true, true)
			for path, original := range historical {
				if !bytes.Equal(original, mustStageFile(t, filepath.Join(root, path))) {
					t.Fatalf("historical report changed: %s", path)
				}
			}
			if kind == "file" {
				info, err := os.Lstat(occupied)
				if err != nil || !info.Mode().IsRegular() || !bytes.Equal(unknown, mustStageFile(t, occupied)) {
					t.Fatalf("unknown root file changed: %v", err)
				}
			} else {
				link, err := os.Readlink(occupied)
				if err != nil || link != target {
					t.Fatalf("unknown root symlink changed: %q %v", link, err)
				}
				entries, err := os.ReadDir(target)
				if err != nil || len(entries) != 1 || entries[0].Name() != "unknown.txt" || !bytes.Equal(unknown, mustStageFile(t, filepath.Join(target, "unknown.txt"))) {
					t.Fatalf("unknown symlink target changed: %v", err)
				}
			}
		})
	}
}

func s4NaturalAcceptancePost(t *testing.T, root string, packet workpacket.TaskPlanningPacket) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"hook_event_name": "PostToolUse", "session_id": "s4-natural-acceptance", "cwd": root, "tool_name": "Write", "tool_input": map[string]any{"file_path": filepath.Join(root, packet.AnswerPath)}, "tool_response": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	var out, diag bytes.Buffer
	if code := cli.Run([]string{"hook", "--event", "PostToolUse", "--root", root}, bytes.NewReader(body), &out, &diag); code != 0 {
		t.Fatalf("natural S4 save failed %d: %s %s", code, &out, &diag)
	}
	if strings.Contains(out.String(), "delivery record unavailable") || strings.Contains(out.String(), "questionnaire cannot be read") {
		t.Fatalf("accepted delivery guidance could not read the report: %s", &out)
	}
	return out.String()
}

func s4AssertNaturalAcceptedReportAndSuccessors(t *testing.T, root string, packet workpacket.TaskPlanningPacket, output string, requireAlternate, requireGuidance bool) []byte {
	t.Helper()
	snap := stageSnapshot(t, root)
	record, found, err := runtime.FindSubmission(snap.State, packet.ID)
	if err != nil || !found || record.ControlStatus != "advanced" || record.TransitionEventID == "" {
		t.Fatalf("original S4/TR-002 did not advance: %+v %v; hook output: %s", record, err, output)
	}
	var path string
	for _, raw := range snap.State["evidence"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == record.EvidenceID {
			path, _ = row["path"].(string)
		}
	}
	if requireAlternate && !strings.HasPrefix(path, ".claude/evidence-alt-") {
		t.Fatalf("occupied root did not select evidence-alt: %q", path)
	}
	report, err := runtime.ReadTaskPlanningReport(root, path)
	if err != nil || !json.Valid(report) {
		t.Fatalf("registered S4 report unreadable: %s %v", path, err)
	}
	guidance := submission.AcceptedPlanningDeliveryGuidance(root, snap.State, record, "S4")
	if !strings.Contains(guidance, "Recorded accepted delivery:") || !strings.Contains(guidance, "TASK-039-01.md") || !strings.Contains(guidance, filepath.Join(root, path)) {
		t.Fatalf("accepted delivery guidance could not read the natural report: %s", guidance)
	}
	registeredTask := registeredDocument(snap.State, "task")
	registeredPlan := registeredDocument(snap.State, "dispatch_plan")
	if registeredTask == nil || registeredPlan == nil || registeredTask["path"] != "docs/dev/tasks/TASK-039-01.md" || registeredTask["version"] != "v1.0.0" || registeredPlan["path"] != "docs/dev/tasks/index-REQ-039.md" {
		t.Fatal("TR-002 did not register the original complete TASK and dispatch index")
	}
	service, err := submission.New(root)
	if err != nil {
		t.Fatal(err)
	}
	formal, err := runtime.S5FormalInputs(root, snap.State, service.Files)
	if err != nil {
		t.Fatalf("S5 formal inputs could not consume original report: %v", err)
	}
	active := false
	for _, ref := range formal.References {
		if ref.Path == "docs/dev/tasks/TASK-039-01.md" {
			active = true
		}
		if ref.Path == "docs/dev/tasks/TASK-039-02.md" || ref.Path == "docs/dev/tasks/TASK-039-03.md" {
			t.Fatal("cancelled TASK entered the S5 active denominator")
		}
	}
	if !active || snap.State["lifecycle"].(map[string]any)["state"] != "document_verification" {
		t.Fatal("original active TASK or S5 lifecycle lost")
	}
	packets, err := runtime.S5Packets(root, snap.State, "")
	if err != nil || len(packets) != 2 {
		t.Fatalf("S5 independent original packets: count=%d err=%v", len(packets), err)
	}
	responsibilities, owners := map[string]bool{}, map[string]bool{}
	for _, p := range packets {
		if responsibilities[p.Binding.Responsibility] || owners[p.Binding.Owner] {
			t.Fatal("S5 original packets share responsibility or owner")
		}
		responsibilities[p.Binding.Responsibility], owners[p.Binding.Owner] = true, true
		if _, found, err := runtime.FindWorkPacket(snap.State, p.ID); err != nil || !found {
			t.Fatalf("S5 original packet not issued: %s %v", p.ID, err)
		}
		for _, rel := range []string{p.ViewPath, p.AnswerPath} {
			if !json.Valid(mustStageFile(t, filepath.Join(root, rel))) || (requireGuidance && !strings.Contains(output, filepath.Join(root, rel))) {
				t.Fatalf("S5 issued material not readable/dispatched: %s", rel)
			}
		}
	}
	for _, responsibility := range []string{"DV-SPEC-CONSISTENCY", "DV-TASK-EXECUTABILITY"} {
		if !responsibilities[responsibility] {
			t.Fatalf("S5 responsibility missing: %s", responsibility)
		}
	}
	return report
}
