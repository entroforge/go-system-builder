package repair_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
)

func TestHandoffPreservesOnlyAvailableOriginalReferences(t *testing.T) {
	ref := func(path string) repair.ArtifactRef { return repair.ArtifactRef{Path: path} }
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "without-final-or-changeset", true: "existing-legacy-references"}[legacy], func(t *testing.T) {
			root := t.TempDir()
			request := repair.HandoffRequest{
				HandoffID: "repair-handoff-test", Session: ref("session.json"), Plan: ref("plan.json"),
				Contract: repair.ContractRef{Path: "contract.json"}, ChangeImpact: ref("impact.json"),
				TargetedReverifications: []repair.ArtifactRef{ref("targeted.json")}, HandedOffBy: "Main", NextAction: "start S7",
			}
			if legacy {
				request.Result, request.Changeset = ref("result.json"), ref("changeset.json")
			}
			_, stored, err := repair.CreateRepairHandoff(root, request)
			if err != nil {
				t.Fatalf("create handoff: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(stored.Path)))
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]any
			if err := json.Unmarshal(data, &wire); err != nil {
				t.Fatal(err)
			}
			_, resultPresent := wire["result_ref"]
			_, changesetPresent := wire["changeset_ref"]
			if resultPresent != legacy || changesetPresent != legacy {
				t.Fatalf("handoff changed available facts: %s", data)
			}
			parsed, err := repair.ValidateRepairHandoff(root, stored)
			if err != nil {
				t.Fatalf("existing handoff remains readable: %v", err)
			}
			if legacy && (parsed.ResultRef.Path != "result.json" || parsed.ChangesetRef.Path != "changeset.json") {
				t.Fatalf("legacy references changed: %+v", parsed)
			}
		})
	}
}

func TestTargetedOriginalCanRetainUnknownImpactAssociation(t *testing.T) {
	root := t.TempDir()
	value, ref, err := repair.CreateTargetedReverification(root, repair.TargetedReverificationRequest{
		ReverificationID: "reverify-unknown-impact", RuntimeID: "loop-1", CaseID: "case-1", BaselineGeneration: 1,
		OriginalAssignmentID: "repair-assignment-one", PerformingAssignmentID: "repair-assignment-verification-one",
		ImpactID: "", Result: "fail",
	})
	if err != nil {
		t.Fatalf("unknown original association must not prevent reception: %v", err)
	}
	if value.ImpactID != "" || value.Result != "fail" || value.FailureClass != "" {
		t.Fatalf("unknown association or cause was invented: %+v", value)
	}
	saved, err := repair.ValidateTargetedReverification(root, ref)
	if err != nil || saved.ImpactID != "" || saved.FailureClass != "" {
		t.Fatalf("original unknown facts not preserved: %+v, %v", saved, err)
	}
}
