package repair_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/repair"
)

func stopAssessments(t *testing.T, root string, ref repair.ContractRef) []repair.StopConditionAssessment {
	t.Helper()
	contract, err := repair.ValidateApprovedContractRef(root, ref)
	if err != nil {
		t.Fatal(err)
	}
	items := make([]repair.StopConditionAssessment, len(contract.StopEscalationConditions))
	for index := range items {
		items[index] = repair.StopConditionAssessment{ContractSHA256: ref.SHA256, ConditionIndex: index, Outcome: "not_triggered", Rationale: "Independent scope and invariant inspection found no deviation", EvidenceRefs: []string{"test://stop-condition-review"}}
	}
	return items
}

// Exercise the public consumer while the fixture is at the real S9 targeted
// checkpoint. All rejected candidates must leave state and journal unchanged.
func assertStopConditionRejections(t *testing.T, root, statePath, journalPath string, valid repair.ArtifactRef) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, valid.Path))
	if err != nil {
		t.Fatal(err)
	}
	state, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		edit  func(*repair.TargetedReverification)
		actor string
		want  string
	}{
		{"missing", func(v *repair.TargetedReverification) { v.StopConditionAssessments = nil }, "qa", "coverage"},
		{"wrong-contract", func(v *repair.TargetedReverification) {
			v.StopConditionAssessments[0].ContractSHA256 = strings.Repeat("f", 64)
		}, "qa", "wrong contract"},
		{"out-of-range", func(v *repair.TargetedReverification) { v.StopConditionAssessments[0].ConditionIndex = 99 }, "qa", "out-of-range"},
		{"duplicate", func(v *repair.TargetedReverification) {
			v.StopConditionAssessments = append(v.StopConditionAssessments, v.StopConditionAssessments[0])
		}, "qa", "coverage"},
		{"unknown", func(v *repair.TargetedReverification) { v.StopConditionAssessments[0].Outcome = "unknown" }, "qa", "cannot pass"},
		{"triggered-residual", func(v *repair.TargetedReverification) {
			v.StopConditionAssessments[0].Outcome = "triggered"
			v.StopConditionAssessments[0].Rationale = "Design changed; residual risk disclosed"
		}, "qa", "cannot pass"},
		{"no-evidence", func(v *repair.TargetedReverification) { v.StopConditionAssessments[0].EvidenceRefs = nil }, "qa", "schema"},
		{"phantom-evidence", func(v *repair.TargetedReverification) {
			v.StopConditionAssessments[0].EvidenceRefs = []string{"evidence/phantom.json"}
		}, "qa", "evidence"},
		{"actor-disguise", func(v *repair.TargetedReverification) {}, "builder-1", "not the dispatched verifier"},
	} {
		t.Run("stop-condition-"+tc.name, func(t *testing.T) {
			var candidate repair.TargetedReverification
			if err := json.Unmarshal(data, &candidate); err != nil {
				t.Fatal(err)
			}
			tc.edit(&candidate)
			path := filepath.Join(root, ".claude/review/repair/reverification/stop-negative-"+tc.name+".json")
			payload, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, payload, 0600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			code := cli.Run([]string{"runtime", "repair", "targeted", "commit", "--root", root, "--state", statePath, "--journal", journalPath, "--file", path, "--actor", tc.actor, "--expected-revision", "6"}, bytes.NewReader(nil), &stdout, &stderr)
			if code == 0 || !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("exit=%d expected %q: %s", code, tc.want, stderr.String())
			}
			currentState, stateErr := os.ReadFile(statePath)
			currentJournal, journalErr := os.ReadFile(journalPath)
			if stateErr != nil || journalErr != nil || !bytes.Equal(state, currentState) || !bytes.Equal(journal, currentJournal) {
				t.Fatal("rejected stop condition candidate changed Runtime or journal")
			}
		})
	}
}

// A pre-upgrade ready_for_full_review pointer must not turn an old PASS into
// a new completion without an actual independent stop-condition review.
func assertLegacyStopReviewCannotHandoff(t *testing.T, root, statePath, journalPath string, handoff repair.RepairHandoff) {
	t.Helper()
	stateBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.WriteFile(statePath, stateBytes, 0600); err != nil {
			t.Fatal(err)
		}
	}()
	journal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, handoff.TargetedReverificationRefs[0].Path))
	if err != nil {
		t.Fatal(err)
	}
	var target repair.TargetedReverification
	if err := json.Unmarshal(data, &target); err != nil {
		t.Fatal(err)
	}
	target.StopConditionAssessments = nil
	data, err = json.Marshal(target)
	if err != nil {
		t.Fatal(err)
	}
	legacyRef := repair.ArtifactRef{Path: ".claude/review/repair/reverification/legacy-stop-pass.json", SHA256: fileHash(data)}
	if err := os.WriteFile(filepath.Join(root, legacyRef.Path), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := repair.ValidateTargetedReverification(root, legacyRef); err != nil {
		t.Fatalf("historical targeted PASS is no longer readable: %v", err)
	}
	handoff.TargetedReverificationRefs = []repair.ArtifactRef{legacyRef}
	var state map[string]any
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	pointer := state["review"].(map[string]any)["repair"].(map[string]any)
	pointer["targeted_reverification_artifacts"] = []repair.ArtifactRef{legacyRef}
	pointer["targeted_reverification_refs"] = []string{legacyRef.Path}
	candidateState, _ := json.Marshal(state)
	if err := os.WriteFile(statePath, candidateState, 0600); err != nil {
		t.Fatal(err)
	}
	data, _ = json.Marshal(handoff)
	handoffRef := repair.ArtifactRef{Path: ".claude/review/repair/handoffs/legacy-stop-handoff.json", SHA256: fileHash(data)}
	if err := os.WriteFile(filepath.Join(root, handoffRef.Path), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := repair.CommitRepairHandoff(root, statePath, journalPath, repair.CommitHandoffRequest{RuntimeRequest: repair.RuntimeRequest{ExpectedRevision: 7, Actor: "main"}, Handoff: handoffRef}); err == nil || !strings.Contains(err.Error(), "stop condition coverage") {
		t.Fatalf("old PASS bypassed the handoff completion gate: %v", err)
	}
	currentState, stateErr := os.ReadFile(statePath)
	currentJournal, journalErr := os.ReadFile(journalPath)
	if stateErr != nil || journalErr != nil || !bytes.Equal(candidateState, currentState) || !bytes.Equal(journal, currentJournal) {
		t.Fatal("rejected legacy handoff changed Runtime or journal")
	}
}
