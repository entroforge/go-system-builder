package cli_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/catalog"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

// This is a mechanism acceptance test: reviewers and native Stop payloads are
// simulated, while S2/S3 reception, S4 Post, S5 Writer, gates, transitions and
// S6 preparation use their original production entrypoints. No Claude runs.
func TestS4S5NaturalChainWithEntireEvidenceRootOccupied(t *testing.T) {
	for _, scenario := range []string{"file/pass", "symlink/pass", "file/fix_required", "symlink/fix_required"} {
		t.Run(scenario, func(t *testing.T) {
			kind, verdict, _ := strings.Cut(scenario, "/")
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
				for _, rel := range []string{"agents/backend-builder.md", "agents/document-verifier.md", "skills/document-verification", "skills/specification-planning", "skills/api-contracts", "skills/testing-strategy", "skills/agent-dispatch"} {
					copyS6RecordContextTree(t, filepath.Join(fixtures.RepoRoot(t), rel), filepath.Join(root, ".claude", rel))
				}
				for _, skill := range catalog.Skills {
					copyS6RecordContextTree(t, filepath.Join(fixtures.RepoRoot(t), "skills", skill.Name), filepath.Join(root, ".claude/skills", skill.Name))
				}
			})
			stageJSON(t, root, contracts.AnswerPath, completedContractDraft(contracts))
			stageCLI(t, "runtime", "stage-answer", "consume", "--root", root, "--agent", "Main", "--path", contracts.AnswerPath)
			settleMainPlanningFollowups(t, root)
			// Reuse the complete TASK and dispatch index text from the established
			// fixture, with the same professional closure and only one active TASK.
			writeContractSource(t, root, "docs/dev/tasks/TASK-039-01.md", "# Task\n> Status: complete\n> Version: v1.0.0\n> Source REQ refs: REQ-039\n> Primary contract: BE-039\n\n## 3. Delivered Clauses\n\n| Contract | Delivered clauses |\n|:--|:--|\n| BE-039 | §1 |\n\n## 4. Scope\n\n| Type | Paths / Commands |\n|:--|:--|\n| prospective write paths | `internal/fixture/service.go` |\n\n## 7. Closing Contract\n\n```text\nassert go test ./internal/fixture == pass\n```\n")
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
			s4S5NaturalReviewChain(t, root, verdict)
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

func s4S5NaturalReviewChain(t *testing.T, root, verdict string) {
	t.Helper()
	packets, err := runtime.S5Packets(root, stageSnapshot(t, root).State, "")
	if err != nil || len(packets) != 2 {
		t.Fatalf("natural S4 Post did not issue the independent review pair: %d %v", len(packets), err)
	}
	if planningTransitionEventCount(t, root, "TR-002") != 1 {
		t.Fatal("natural S4 Post did not commit exactly one TR-002")
	}
	for i, p := range packets {
		conclusion := "pass"
		if i == len(packets)-1 {
			conclusion = verdict
		}
		draft := completedDraft(p, conclusion)
		if conclusion == "fix_required" {
			draft.Answer.Findings = []workpacket.Finding{{Path: "docs/dev/tasks/TASK-039-01.md#scope", Expected: "all deliverables named", Observed: "a concrete missing output"}}
		}
		stageJSON(t, root, p.AnswerPath, draft)
		answer := mustStageFile(t, filepath.Join(root, p.AnswerPath))
		// Two distinct opaque platform identities return the two originally
		// issued responsibilities; neither supplies fabricated evidence.
		actor := "simulated-natural-reviewer-" + p.Binding.Owner
		code, out, diagnostic := nativeReviewEvent(t, root, "SubagentStop", actor, filepath.Join(root, p.ViewPath))
		if code != 0 {
			t.Fatalf("original S5 Stop failed: %d %s %s", code, out, diagnostic)
		}
		snapshot := stageSnapshot(t, root)
		record, accepted, err := runtime.FindSubmission(snapshot.State, p.ID)
		if err != nil || !accepted || record.EvidenceID == "" || record.ControlStatus == "pending" {
			t.Fatalf("Stop exit alone is not reception/control completion: %+v accepted=%t err=%v diagnostic=%s", record, accepted, err, diagnostic)
		}
		var reportPath string
		for _, raw := range snapshot.State["evidence"].([]any) {
			row := raw.(map[string]any)
			if row["id"] == record.EvidenceID {
				if row["kind"] != "document_review" {
					t.Fatalf("S5 submission points to the wrong evidence kind: %+v", row)
				}
				reportPath, _ = row["path"].(string)
			}
		}
		if !strings.HasPrefix(reportPath, ".claude/evidence-alt-") {
			t.Fatalf("S5 report did not use the registered alternate root: %q", reportPath)
		}
		report, err := runtime.ReadRecordedEvidenceReport(root, reportPath)
		if err != nil {
			t.Fatalf("registered S5 report is unreadable: %s %v", reportPath, err)
		}
		if err := workpacket.ValidateDocumentReview(p, record.EvidenceID, draft.Answer, report); err != nil {
			t.Fatalf("registered S5 report lost original owner, subjects, versions or judgment: %v", err)
		}
		var envelope workpacket.DocumentReviewEnvelope
		if err := json.Unmarshal(report, &envelope); err != nil {
			t.Fatal(err)
		}
		if conclusion == "fix_required" && envelope.RequestedEvent != "document_fix_required" {
			t.Fatalf("fix_required lost the original requested event: %+v", envelope)
		}
		if !bytes.Equal(answer, mustStageFile(t, filepath.Join(root, p.AnswerPath))) {
			t.Fatal("reception changed the reviewer's original answer")
		}
		if i == 0 {
			if snapshot.State["lifecycle"].(map[string]any)["state"] != "document_verification" || planningTransitionEventCount(t, root, "TR-003") != 0 || planningTransitionEventCount(t, root, "TR-004") != 0 {
				t.Fatal("one independent PASS incorrectly completed the S5 gate")
			}
		}
		// Replaying the exact original Stop reads the registered winner; it
		// cannot publish another report, receive again or advance twice.
		if code, _, diagnostic := nativeReviewEvent(t, root, "SubagentStop", actor, filepath.Join(root, p.ViewPath)); code != 0 {
			t.Fatalf("received Stop replay failed: %d %s", code, diagnostic)
		}
		if stageSnapshot(t, root).Revision != snapshot.Revision || !bytes.Equal(report, mustStageFile(t, filepath.Join(root, reportPath))) {
			t.Fatal("received Stop replay changed Runtime or registered report bytes")
		}
	}
	state := stageSnapshot(t, root).State
	lifecycle := state["lifecycle"].(map[string]any)
	transitionID, otherTransition, expectedState := "TR-003", "TR-004", "building"
	if verdict == "fix_required" {
		transitionID, otherTransition, expectedState = "TR-004", "TR-003", "planning"
	}
	if lifecycle["state"] != expectedState || planningTransitionEventCount(t, root, transitionID) != 1 || planningTransitionEventCount(t, root, otherTransition) != 0 {
		t.Fatalf("original gate did not take exactly %s: lifecycle=%v", transitionID, lifecycle)
	}
	if verdict == "fix_required" {
		if lifecycle["phase"] != "design" {
			t.Fatalf("TR-004 did not return to original S2 design responsibility: %v", lifecycle)
		}
		design, issued, err := runtime.CurrentIssuedDesignPacket(state)
		if err != nil || !issued {
			t.Fatalf("TR-004 did not prepare the current S2 packet: %v", err)
		}
		for _, path := range []string{design.ViewPath, design.AnswerPath} {
			if !json.Valid(mustStageFile(t, filepath.Join(root, path))) {
				t.Fatalf("TR-004 current S2 material is unreadable: %s", path)
			}
		}
		return
	}
	// Prepare through the original S6 Main command. This proves material
	// readiness only; no Builder is launched and no product is accepted.
	stageCLI(t, "s6", "prepare", "--root", root, "--task", "TASK-039-01", "--role", "backend-builder")
	record, found, err := assignmentrecord.ForOwner(stageSnapshot(t, root).State, "native-TASK-039-01")
	if err != nil || !found {
		t.Fatalf("original S6 preparation did not register the reviewed TASK responsibility: found=%t err=%v", found, err)
	}
	brief, _, _ := assignmentrecord.NativePaths(record)
	if len(mustStageFile(t, filepath.Join(root, brief))) == 0 {
		t.Fatal("original S6 preparation produced no actual native brief")
	}
}
