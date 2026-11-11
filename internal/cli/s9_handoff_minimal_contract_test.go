package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestHandoffRequestUsesAlreadyAvailableFactsWithoutChangeset(t *testing.T) {
	root := t.TempDir()
	pointer := map[string]any{
		"session_id": "repair-session-test", "path": "session.json", "sha256": "session-sha",
		"plan_ref": "plan.json", "plan_sha256": "plan-sha", "contract_ref": "contract.json", "contract_sha256": "contract-sha",
		"impact_ref": "impact.json", "impact_sha256": "impact-sha",
		"targeted_reverification_artifacts": []any{map[string]any{"path": "targeted.json", "sha256": "targeted-sha"}},
	}
	state := map[string]any{"review": map[string]any{"repair": pointer}}
	request, err := repairHandoffRequestFromRuntime(root, state)
	if err != nil {
		t.Fatalf("original facts are sufficient for handoff: %v", err)
	}
	if request.Result.Path != "" || request.Changeset.Path != "" || len(request.TargetedReverifications) != 1 {
		t.Fatalf("handoff invented another fact: %+v", request)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("request created artifacts or read dependent directories: %v, %v", entries, err)
	}
	pointer["result_ref"], pointer["result_sha256"] = "old-final.json", "old-final-sha"
	pointer["changeset_ref"], pointer["changeset_sha256"] = "old-changeset.json", "old-changeset-sha"
	request, err = repairHandoffRequestFromRuntime(root, state)
	if err != nil || request.Result.Path != "old-final.json" || request.Changeset.Path != "old-changeset.json" {
		t.Fatalf("existing history should remain usable: %+v, %v", request, err)
	}
}

func TestTargetedMainReturnPreservesUnknownHistoricalAssociation(t *testing.T) {
	root := t.TempDir()
	// The current Targeted reference line keeps the internally optional
	// performing responsibility and conclusion; the retired path/sha-only
	// reference line is no longer parsed into a record. The surviving
	// reference belongs to another performing responsibility, so the result
	// this responsibility needs cannot be determined from history.
	state := map[string]any{"review": map[string]any{"repair": map[string]any{
		"targeted_reverification_artifacts": []any{map[string]any{
			"path": "missing-old-targeted.json", "performing_assignment_id": "repair-assignment-verification-two", "result": "pass",
		}},
	}}}
	subject := nativeReturnSubject{kind: "Targeted", id: "repair-assignment-verification-one", ref: "original-answer.json", targeted: &repairTargetedSelection{}}
	message, err := nativeReturnSummaryContext(context.Background(), root, runtime.Snapshot{State: state}, subject)
	if err != nil {
		t.Fatalf("unknown history should remain unknown: %v", err)
	}
	for _, want := range []string{
		"No original Targeted reception is recorded",
		"Main owns the missing original input/source",
		"no supported saved input is established for replay",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("unknown historical association was not handed to Main as unknown (missing %q): %s", want, message)
		}
	}
	for _, unwanted := range []string{
		"Original Targeted records:",
		"result=pass",
		"missing-old-targeted.json result=",
	} {
		if strings.Contains(message, unwanted) {
			t.Fatalf("historical reference was projected as this responsibility's conclusion (%q): %s", unwanted, message)
		}
	}
}

func TestTargetedMainReturnUsesAcceptedProjectionWithoutOldReport(t *testing.T) {
	root := t.TempDir()
	state := map[string]any{"review": map[string]any{"repair": map[string]any{
		"targeted_reverification_artifacts": []any{map[string]any{"path": "missing-accepted.json", "sha256": "old", "performing_assignment_id": "repair-assignment-verification-one", "result": "pass"}},
	}}}
	subject := nativeReturnSubject{kind: "Targeted", id: "repair-assignment-verification-one", ref: "original-answer.json", targeted: &repairTargetedSelection{}}
	message, err := nativeReturnSummaryContext(context.Background(), root, runtime.Snapshot{State: state}, subject)
	if err != nil || !strings.Contains(message, "missing-accepted.json result=pass") || !strings.Contains(message, "do not by themselves establish coverage of later changes") {
		t.Fatalf("accepted original fact should not depend on historical source: %s, %v", message, err)
	}
}

func TestTargetedLookupKeepsOldImpactAndUnavailableHistoryDistinct(t *testing.T) {
	root := t.TempDir()
	assignment := "repair-assignment-verification-one"
	initial := "impact-repair-session-1"
	pointer := map[string]any{"session_id": "repair-session-1", "targeted_reverification_artifacts": []any{
		map[string]any{"path": "original.json", "sha256": "known", "performing_assignment_id": assignment, "result": "pass"},
	}}
	selection := repairTargetedSelection{performingAssignmentID: assignment, impact: repair.ChangeImpact{ImpactID: initial}}
	// An accepted projection is enough while the original Impact has not changed.
	first := lookupTargetedForSelection(root, pointer, selection, "pass")
	if !first.found || first.unknown {
		t.Fatalf("direct accepted fact should not require the old report: %+v", first)
	}
	selection.impact.ImpactID = initial + "-r7"
	missing := lookupTargetedForSelection(root, pointer, selection, "pass")
	if missing.found || !missing.unknown {
		t.Fatalf("missing old Impact association must remain unknown: %+v", missing)
	}
	old := repair.TargetedReverification{PerformingAssignmentID: assignment, Result: "pass", ImpactID: initial, AssertionResults: []repair.AssertionResult{}}
	data, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "original.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	previous := lookupTargetedForSelection(root, pointer, selection, "pass")
	if previous.found || previous.unknown || previous.previous.Path != "original.json" || previous.previousReport == nil {
		t.Fatalf("old PASS must not claim later Impact: %+v", previous)
	}
	unchanged := repair.TargetedReverificationRequest{Result: "pass"}
	if !sameTargetedProfessionalAnswer(unchanged, previous.previousReport) {
		t.Fatal("nil and empty original observations must not create a new delivery")
	}
	unchanged.Result = "fail"
	if sameTargetedProfessionalAnswer(unchanged, previous.previousReport) {
		t.Fatal("a real new conclusion must remain eligible for natural reception")
	}
}
