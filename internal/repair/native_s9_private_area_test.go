package repair

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// This isolates the original Candidate product view. Issuance, the Writer and
// Main integration are tested separately through their real public paths.
func TestCandidateNativeS9PrivateAreaProjectsScratchAndRetainsSources(t *testing.T) {
	for _, mode := range []string{"29_private_outputs", "other_packet", "product_change", "product_scope", "indexed_output", "base_output", "changed_plan", "changed_evidence", "changed_capture", "scratch_symlink"} {
		t.Run(mode, func(t *testing.T) {
			root, worker := t.TempDir(), t.TempDir()
			boundaryGit(t, worker, "init")
			boundaryGit(t, worker, "config", "user.name", "fixture")
			boundaryGit(t, worker, "config", "user.email", "fixture@example.com")
			boundaryWrite(t, worker, "internal/api/product.go", "package api\n")
			boundaryGit(t, worker, "add", "internal/api/product.go")
			boundaryGit(t, worker, "commit", "-qm", "original product")
			base, err := workspace.Git(t.Context(), worker, "rev-parse", "HEAD")
			if err != nil {
				t.Fatal(err)
			}
			planID := "packet-" + strings.Repeat("a", 64)
			resultID := "packet-" + strings.Repeat("b", 64)
			planPath, resultPath, err := NativeS9PrivatePaths(planID)
			if err != nil {
				t.Fatal(err)
			}
			area := filepath.ToSlash(filepath.Dir(planPath))
			binding := workpacket.RepairPlanReportBinding{RuntimeID: "loop-native-s9", BaselineGeneration: 1, ReviewRound: 1, SessionID: "repair-session-fixture", RepairAssignmentID: "repair-assignment-1", ManifestAssignmentID: "assignment-1", OwnerAgentID: "builder-1"}
			session := RepairSession{SessionID: binding.SessionID, RuntimeID: binding.RuntimeID, BaselineGeneration: binding.BaselineGeneration}
			sessionBytes, _ := json.Marshal(session)
			report, _ := json.Marshal(PlanReport{Receipt: &runtimepkg.RepairPlanReportReceipt{PacketID: planID, OwnerAgentID: binding.OwnerAgentID}})
			packet := workpacket.RepairResultPacket{ID: resultID, Binding: workpacket.RepairResultBinding{Sources: binding}, Inputs: workpacket.RepairResultInputs{PlanReport: report, Sources: workpacket.RepairPlanReportInputs{Session: sessionBytes}}}
			pin := fmt.Sprintf(".claude/work-packets/%s/g%d/S9-plan/r%d/%s/%s/native/source.json", binding.RuntimeID, binding.BaselineGeneration, binding.ReviewRound, binding.RepairAssignmentID, planID)
			pinBytes := []byte(`{"private_submission_version":1}`)
			boundaryWrite(t, root, pin, string(pinBytes))
			e := workspace.Execution{Purpose: workspace.NativeDeliveryPurpose, RuntimeID: binding.RuntimeID, BaselineGeneration: binding.BaselineGeneration, AssignmentID: binding.ManifestAssignmentID, AgentID: binding.OwnerAgentID, Path: worker, BaseCommit: base, WritePaths: []string{"internal/api"}, Inputs: map[string]string{pin: workpacket.Digest(pinBytes)}}
			boundaryWrite(t, worker, planPath, `{"plan":"actual"}`)
			boundaryWrite(t, worker, resultPath, `{"verdict":"completed"}`)
			provenance := runtimepkg.RepairResultCandidateProvenance{PacketID: resultID, PrivateSubmissions: []runtimepkg.RepairResultPrivateSubmission{
				{Kind: "plan", Path: planPath, SHA256: workpacket.Digest([]byte(`{"plan":"actual"}`))},
				{Kind: "result", Path: resultPath, SHA256: workpacket.Digest([]byte(`{"verdict":"completed"}`))},
			}}
			refs := []string{}
			paths := []string{}
			for i := 0; i < 29; i++ {
				name := fmt.Sprintf("check-%02d.txt", i)
				if i == 0 {
					name = "go-list.json"
				}
				p := area + "/logs/" + name
				paths = append(paths, p)
				boundaryWrite(t, worker, p, fmt.Sprintf("actual observation %d\n", i))
				if i < 17 {
					refs = append(refs, "path:"+p)
				}
			}
			answer, _ := json.Marshal(map[string]any{"evidence_refs": refs})
			provenance.LocalEvidence, err = captureS9LocalEvidence(root, worker, resultID, answer)
			if err != nil || len(provenance.LocalEvidence) != 17 {
				t.Fatalf("captured sources: %d %v", len(provenance.LocalEvidence), err)
			}
			proposal := RepairResultRequest{issued: &issuedRepairResultCapability{packet: packet, provenance: provenance}}
			wantDirty := ""
			switch mode {
			case "other_packet":
				other := ".loop-submissions/native-s9/packet-" + strings.Repeat("c", 64) + "/logs/other.log"
				boundaryWrite(t, worker, other, "other responsibility")
				otherAnswer, _ := json.Marshal(map[string]any{"evidence_refs": []string{"path:" + other}})
				otherCapture, err := captureS9LocalEvidence(root, worker, resultID, otherAnswer)
				if err != nil {
					t.Fatal(err)
				}
				proposal.issued.provenance.LocalEvidence = append(proposal.issued.provenance.LocalEvidence, otherCapture...)
				wantDirty = other
			case "product_change":
				boundaryWrite(t, worker, "internal/api/product.go", "package api\nconst changed = true\n")
				wantDirty = "internal/api/product.go"
			case "product_scope":
				e.WritePaths = append(e.WritePaths, paths[20])
				wantDirty = paths[20]
			case "indexed_output":
				boundaryGit(t, worker, "add", "--", paths[20])
				wantDirty = paths[20]
			case "base_output":
				p := area + "/logs/prior-product.txt"
				boundaryWrite(t, worker, p, "original product")
				boundaryGit(t, worker, "add", "--", p)
				boundaryGit(t, worker, "commit", "-qm", "original base product")
				e.BaseCommit, err = workspace.Git(t.Context(), worker, "rev-parse", "HEAD")
				if err != nil {
					t.Fatal(err)
				}
				boundaryGit(t, worker, "rm", "--", p)
				boundaryGit(t, worker, "commit", "-qm", "remove base product")
				boundaryWrite(t, worker, p, "untracked replacement")
				wantDirty = p
			case "changed_plan":
				boundaryWrite(t, worker, planPath, `{"plan":"changed"}`)
			case "changed_evidence":
				boundaryWrite(t, worker, paths[0], "changed source")
			case "changed_capture":
				boundaryWrite(t, root, provenance.LocalEvidence[0].Path, "changed capture")
			case "scratch_symlink":
				target := filepath.Join(t.TempDir(), "external")
				if err := os.WriteFile(target, []byte("external unchanged"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(worker, area, "logs", "private-link")); err != nil {
					t.Fatal(err)
				}
			}
			status, err := candidateProductStatus(t.Context(), root, e, session, proposal)
			switch mode {
			case "29_private_outputs", "scratch_symlink":
				if err != nil || status != "" {
					t.Fatalf("private outputs became product changes: %q %v", status, err)
				}
			case "changed_plan", "changed_evidence", "changed_capture":
				if err == nil {
					t.Fatalf("changed original/captured bytes bypassed by area: %q", status)
				}
			default:
				if err != nil || !strings.Contains(status, wantDirty) {
					t.Fatalf("product or other responsibility hidden: want=%s status=%q err=%v", wantDirty, status, err)
				}
			}
			if mode == "29_private_outputs" {
				projected, err := WorkspaceCandidateLogProjectionForPacket(t.Context(), root, packet, e, WorkspaceCandidate{Provenance: &provenance})
				if err != nil || len(projected) != 17 {
					t.Fatalf("original capture handoff: %d %v", len(projected), err)
				}
				for _, item := range provenance.LocalEvidence {
					if projected[item.SourcePath] != item.SHA256 {
						t.Fatalf("captured source lost original digest: %s", item.SourcePath)
					}
				}
			}
		})
	}
}
