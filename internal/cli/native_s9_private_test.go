package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestNativeS9PrivateOriginalConsumer(t *testing.T) {
	for _, scenario := range []string{"changed", "nochange", "blocked", "same-packet-scratch", "other-packet-sibling", "staged-plan", "ignored-result", "changed-after-receipt"} {
		t.Run(scenario, func(t *testing.T) {
			root, worker, p, _, answer := nativeS9WorkerFixture(t)
			plan, result, err := nativeS9AuthorPaths(root, p)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(plan, ".loop-submissions/native-s9/") {
				t.Fatal(plan)
			}
			switch scenario {
			case "nochange":
				repairLeadGit(t, worker, "reset", "--hard", "HEAD~1")
			case "blocked":
				answer.Verdict = "blocked"
				for i := range answer.UnitResults {
					answer.UnitResults[i].Status = "blocked"
				}
				if err := persistRepairLeadQuestionnaire(filepath.Join(worker, result), answer); err != nil {
					t.Fatal(err)
				}
			case "same-packet-scratch":
				writeRepairLeadFile(t, worker, filepath.ToSlash(filepath.Join(filepath.Dir(plan), "unknown.txt")), "unreceived\n")
			case "other-packet-sibling":
				other := "packet-" + strings.Repeat("c", 64)
				if other == p.ID {
					other = "packet-" + strings.Repeat("d", 64)
				}
				writeRepairLeadFile(t, worker, filepath.ToSlash(filepath.Join(filepath.Dir(filepath.Dir(plan)), other, "unknown.txt")), "another responsibility\n")
			case "staged-plan":
				repairLeadGit(t, worker, "add", plan)
			case "ignored-result":
				repairLeadGit(t, worker, "config", "core.excludesFile", filepath.Join(root, "private-excludes"))
				if err := os.WriteFile(filepath.Join(root, "private-excludes"), []byte(result+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var diagnostic bytes.Buffer
			err = receiveNativeS9BuilderFrom(context.Background(), root, repairDispatchSnapshot(t, root).State, p, worker, false, &diagnostic, nil)
			negative := scenario == "other-packet-sibling" || scenario == "staged-plan" || scenario == "ignored-result"
			if negative {
				if err == nil {
					t.Fatalf("unsafe source accepted: %s", diagnostic.String())
				}
				return
			}
			if err != nil {
				t.Fatalf("original consumer: %v %s", err, diagnostic.String())
			}
			if scenario == "same-packet-scratch" {
				data, err := os.ReadFile(filepath.Join(worker, filepath.Dir(plan), "unknown.txt"))
				if err != nil || string(data) != "unreceived\n" {
					t.Fatalf("Candidate reception changed or removed the author's private scratch: %q %v", data, err)
				}
			}
			snap := repairDispatchSnapshot(t, root)
			registry, err := workspace.Decode(snap.State)
			if err != nil || registry == nil {
				t.Fatalf("registry %v", err)
			}
			e := registry.Executions[p.Binding.ManifestAssignmentID]
			if repair.NativeS9SourceReference(e).Path != "" {
				t.Fatal("D created C source")
			}
			if scenario == "blocked" {
				if e.DeliveryRef != "" {
					t.Fatal("blocker became candidate")
				}
				return
			}
			candidate, err := repair.ReadWorkspaceCandidate(root, repair.ArtifactRef{Path: e.DeliveryRef, SHA256: e.DeliverySHA256})
			if err != nil {
				t.Fatal(err)
			}
			if candidate.Provenance == nil || len(candidate.Provenance.PrivateSubmissions) != 2 {
				t.Fatalf("private byte identities missing: %+v", candidate.Provenance)
			}
			packets, err := runtime.CurrentRepairResultPackets(root, snap.State, e.AgentID)
			if err != nil || len(packets) != 1 {
				t.Fatalf("packets %v", err)
			}
			projection, err := repair.NativeS9PrivateProjection(t.Context(), root, packets[0], e, candidate, false)
			if err != nil || len(projection) != 2 {
				t.Fatalf("projection %v %v", projection, err)
			}
			if scenario == "changed-after-receipt" {
				raw, err := os.ReadFile(filepath.Join(worker, plan))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(worker, plan), append(raw, ' '), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := repair.NativeS9PrivateProjection(t.Context(), root, packets[0], e, candidate, false); err == nil {
					t.Fatal("changed raw bytes adopted")
				}
			}
		})
	}
}

func TestNativeS9PrivatePathsHonorFrozenPreparation(t *testing.T) {
	root := t.TempDir()
	p := workpacket.RepairPlanReportPacket{ID: "packet-" + strings.Repeat("a", 64), ViewPath: ".claude/work-packets/original.json", AnswerPath: ".claude/submissions/original/answer.json"}
	_, pin, legacyResult := nativeS9Paths(p)
	writeRepairLeadFile(t, root, pin, `{"base_commit":"original","source_retention_version":1}`)
	plan, result, err := nativeS9AuthorPaths(root, p)
	if err != nil || plan != p.AnswerPath || result != legacyResult {
		t.Fatalf("legacy contract changed: %s %s %v", plan, result, err)
	}
	// A stray modern-looking file never upgrades an already issued preparation.
	newPlan, newResult, err := repair.NativeS9PrivatePaths(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	writeRepairLeadFile(t, root, newPlan, `{"plan":"unrelated"}`)
	plan, result, err = nativeS9AuthorPaths(root, p)
	if err != nil || plan != p.AnswerPath || result != legacyResult {
		t.Fatalf("file existence upgraded frozen source: %s %s %v", plan, result, err)
	}
	writeRepairLeadFile(t, root, pin, `{"base_commit":"original","private_submission_version":1}`)
	plan, result, err = nativeS9AuthorPaths(root, p)
	if err != nil || plan != newPlan || result != newResult {
		t.Fatalf("D contract unavailable: %s %s %v", plan, result, err)
	}
}
