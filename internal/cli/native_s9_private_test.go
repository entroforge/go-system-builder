package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestNativeS9PrivateOriginalConsumer(t *testing.T) {
	c := s9PreDispatchCatalogForTest(t)
	for _, scenario := range []string{"changed", "nochange", "blocked", "same-packet-scratch", "other-packet-sibling", "staged-plan", "ignored-result", "changed-after-receipt"} {
		t.Run(scenario, func(t *testing.T) {
			root, repairPlan, division := s9PreDispatchFixtureFromCatalog(t, c)
			root, worker, p, _, answer := nativeS9WorkerFixtureFromPlanning(t, root, repairPlan, division)
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
			if scenario == "blocked" {
				if e.DeliveryRef != "" {
					t.Fatal("blocker became candidate")
				}
				return
			}
			candidate, err := repair.ReadWorkspaceCandidate(root, repair.ArtifactRef{Path: e.DeliveryRef})
			if err != nil {
				t.Fatal(err)
			}
			if candidate.Provenance == nil {
				t.Fatal("accepted original result has no recorded responsibility")
			}
		})
	}
}
