package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestS9RepairLeadExistingImpactReminderSkipsPreparation(t *testing.T) {
	root := t.TempDir() // No Git repository or validated Session exists.
	state := repairLeadReminderState()
	path, err := repairLeadQuestionnairePath(root, state, "change-impact.json")
	if err != nil {
		t.Fatal(err)
	}
	writeRepairLeadFile(t, root, strings.TrimPrefix(path, root+string(filepath.Separator)), `{"change_types":["test"],"decisions":[]}`)
	before, _ := os.ReadFile(path)
	context := stageRepairLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse"}, runtime.Snapshot{State: state})
	if !strings.Contains(context, path) || strings.Contains(context, "could not be prepared") {
		t.Fatalf("existing answer unnecessarily prepared Session/Git: %s", context)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("reminder rewrote the existing professional draft")
	}
	// A new indexed source changes the deterministic answer path; the old
	// authoring bytes survive even when original preparation cannot proceed.
	repairPointerCLI(state)["plan_sha256"] = strings.Repeat("b", 64)
	currentPath, err := repairLeadQuestionnairePath(root, state, "change-impact.json")
	if err != nil {
		t.Fatal(err)
	}
	context = stageRepairLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse"}, runtime.Snapshot{State: state})
	if currentPath == path || !strings.Contains(context, currentPath) || !strings.Contains(context, "not prepared") || !strings.Contains(context, "Main/system") {
		t.Fatalf("missing current answer did not identify its actual path and preparation owner: %s", context)
	}
	if _, err := os.Stat(currentPath); !os.IsNotExist(err) {
		t.Fatalf("ordinary reminder prepared a replacement answer: %v", err)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("source change overwrote the old draft")
	}
}

func TestS9RepairLeadTargetedReminderUsesIndexedProjection(t *testing.T) {
	root := t.TempDir()
	state := repairLeadReminderState()
	pointer := repairPointerCLI(state)
	pointer["status"] = "targeted_reverification"
	assignment := "repair-assignment-reminder"
	verifier := repair.VerificationAssignmentID(assignment)
	pointer["assignment_owners"] = map[string]any{assignment: "builder", verifier: "verifier"}
	pointer["required_reverification_ids"] = []any{"reverify-reminder"}
	// These indexed JSON projections deliberately lack domain-complete source
	// artifacts: a reminder can display facts, but cannot accept this fixture.
	plan := `{"assignments":[{"assignment_id":"repair-assignment-reminder"}]}`
	impact := `{"impact_id":"impact-reminder","required_reverification_ids":["reverify-reminder"]}`
	writeRepairLeadFile(t, root, "plan.json", plan)
	writeRepairLeadFile(t, root, "impact.json", impact)
	pointer["plan_sha256"] = repairLeadDigest([]byte(plan))
	pointer["impact_ref"], pointer["impact_sha256"] = "impact.json", repairLeadDigest([]byte(impact))
	path, err := repairTargetedQuestionnairePath(root, state, verifier)
	if err != nil {
		t.Fatal(err)
	}
	writeRepairLeadFile(t, root, strings.TrimPrefix(path, root+string(filepath.Separator)), `{"continuity_reason":"unfinished"}`)
	before, _ := os.ReadFile(path)
	paths := repairLeadQuestionnairePaths(root, state)
	context := stageRepairLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse", AgentID: "verifier"}, runtime.Snapshot{State: state})
	if len(paths) != 1 || paths[0] != path || !strings.Contains(context, path) || strings.Contains(context, "questionnaire preparation failed") || strings.Contains(context, "source is unavailable") {
		t.Fatalf("reminder ran full domain source validation: paths=%v context=%s", paths, context)
	}
	// Reminders do not re-certify all historical Plan bytes. This remains
	// a read-only display, not acceptance or a source-integrity guarantee.
	writeRepairLeadFile(t, root, "plan.json", plan+"\n")
	context = stageRepairLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse", AgentID: "verifier"}, runtime.Snapshot{State: state})
	if !strings.Contains(context, path) || !strings.Contains(context, "independent verifier verifier") {
		t.Fatalf("reminder lost the registered current responsibility: %s", context)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("changed indexed source rewrote the original answer")
	}
}

func repairLeadReminderState() map[string]any {
	return map[string]any{"runtime_id": "loop-reminder", "baseline": map[string]any{"generation": 1},
		"lifecycle": map[string]any{"state": "bug_resolution", "phase": "fixing"},
		"review": map[string]any{"round": 1, "repair": map[string]any{
			"status": "impact_reconciliation", "case_id": "investigation-case-reminder", "contract_id": "repair-contract-reminder",
			"contract_ref": "contract.json", "contract_sha256": strings.Repeat("a", 64),
			"session_id": "repair-session-reminder", "path": "session.json", "sha256": strings.Repeat("a", 64),
			"plan_ref": "plan.json", "plan_sha256": strings.Repeat("a", 64),
		}}}
}

func TestS9RepairLeadAnswerShapeUsesActualSources(t *testing.T) {
	root := t.TempDir()
	contract := "contracts/approved.json"
	result := "results/accepted.json"
	second := "results/accepted-second.json"
	evidence := ".claude/evidence/observed.json"
	writeRepairLeadFile(t, root, contract, `{"symptom_assertions":["actual symptom text"],"root_invariant_assertions":["actual root text"],"detection_gap_assertions":[{"description":"actual gap text"}],"stop_escalation_conditions":["consumer scope exceeds the approved Contract"]}`)
	writeRepairLeadFile(t, root, result, `{"checks":[{"evidence_refs":["test://actual-original-check"]}]}`)
	writeRepairLeadFile(t, root, second, `{"checks":[{"evidence_refs":["test://second-original-check"]}]}`)
	state := map[string]any{
		"bound_req": map[string]any{"path": "docs/REQ-039.md"},
		"evidence":  []any{map[string]any{"id": "original-evidence-id", "path": evidence, "status": "valid"}},
		"review": map[string]any{"repair": map[string]any{"contract_ref": contract, "result_ref": result, "result_refs": []any{
			map[string]any{"path": result}, map[string]any{"path": second},
		}}},
	}
	contractBytes, _ := os.ReadFile(filepath.Join(root, contract))
	repairPointerCLI(state)["contract_sha256"] = repairLeadDigest(contractBytes)
	for _, raw := range repairPointerCLI(state)["result_refs"].([]any) {
		ref := raw.(map[string]any)
		data, _ := os.ReadFile(filepath.Join(root, ref["path"].(string)))
		ref["sha256"] = repairLeadDigest(data)
	}
	for _, targeted := range []bool{false, true} {
		help := repairLeadAnswerHelp(root, state, targeted)
		for _, required := range []string{filepath.Join(root, contract), filepath.Join(root, result), filepath.Join(root, second), filepath.Join(root, "docs/REQ-039.md"), "symptom-1", "actual symptom text", "root-1", "actual root text", "gap-1", "actual gap text", "test://actual-original-check", "test://second-original-check", "not executed evidence"} {
			if !strings.Contains(help, required) {
				t.Fatalf("targeted=%v omitted %q: %s", targeted, required, help)
			}
		}
		if strings.Contains(help, "original-evidence-id") || strings.Contains(help, filepath.Join(root, evidence)) {
			t.Fatalf("unrelated evidence index was dumped: %s", help)
		}
		if !targeted && !strings.Contains(help, "loop-state.json#/evidence") {
			t.Fatalf("impact omitted original index navigation: %s", help)
		}
		var fields []string
		if targeted {
			fields = []string{"continuity_reason and scope_compliance are optional legacy metadata", "assertion_results", "assertion_id", "evidence_refs", "result=pass|fail|blocked|scope_changed", "fail_same_cause|fail_new_cause|blocked|scope_changed|stale"}
			fields = append(fields, "consumer scope exceeds the approved Contract", "environmental inability is blocked", "No condition IDs")
		} else {
			fields = []string{"change_types", "source_id", "target_id", "relation", "rule_id", "decision", "responsibility_id", "scope", "rationale", "recovery_evidence", "IM-[A-Z0-9-]+", "invalidate|supersede|retain|reverify", "invalidated_evidence_ids", "superseded_evidence_ids", "retained_evidence_ids"}
		}
		for _, field := range fields {
			if !strings.Contains(help, field) {
				t.Fatalf("targeted=%v omitted original field/choice %q: %s", targeted, field, help)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".claude")); !os.IsNotExist(err) {
		t.Fatalf("help created Runtime/questionnaire state: %v", err)
	}
}

// Reuse the original domain chain fixture: neither a machine-only edit nor a
// conflicting professional edit may rewrite its Runtime/journal or artifact.
func assertRepairLeadAnswerReplay(t *testing.T, root, path string, input policy.Input, registered string, change func(map[string]any)) {
	t.Helper()
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	protected := map[string][]byte{}
	for _, ref := range []string{".claude/loop-state.json", ".claude/loop-events.jsonl", registered} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ref)))
		if err != nil {
			t.Fatal(err)
		}
		protected[ref] = data
	}
	defer func() {
		if err := os.WriteFile(path, original, 0o644); err != nil {
			t.Fatal(err)
		}
	}()
	var answer map[string]any
	if err := json.Unmarshal(original, &answer); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"runtime_id", "req_id", "case_id", "impact_id", "reverification_id", "original_assignment_id", "performing_assignment_id", "analyzed_by"} {
		if _, exists := answer[field]; exists {
			answer[field] = "machine-field-is-not-professional-content"
		}
	}
	for _, field := range []string{"analyzed_at", "performed_at", "occurred_at"} {
		if _, exists := answer[field]; exists {
			answer[field] = "2026-10-17T00:00:00Z"
		}
	}
	for _, field := range []string{"changed_artifacts", "source_case_ids", "required_reverification_ids"} {
		if _, exists := answer[field]; exists {
			answer[field] = []any{}
		}
	}
	for _, conflict := range []bool{false, true} {
		if conflict {
			change(answer)
		}
		writeRepairLeadJSON(t, path, answer)
		var out, stderr bytes.Buffer
		handled, code := runSavedRepairLeadQuestionnaire(root, input, &out, &stderr)
		want := "Replay did not write Runtime"
		if conflict {
			want = "replay conflict"
		}
		if !handled || code != 0 || !strings.Contains(out.String(), want) || !strings.Contains(out.String(), path) {
			t.Fatalf("conflict=%v handled=%v code=%d stdout=%s stderr=%s", conflict, handled, code, out.String(), stderr.String())
		}
		for ref, before := range protected {
			after, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ref)))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("conflict=%v replay rewrote %s: %v", conflict, ref, err)
			}
		}
	}
}

func TestS9EvidenceProjectionPreservesBuilderRootAndCapturedHandoff(t *testing.T) {
	mainRoot, workerRoot := t.TempDir(), t.TempDir()
	raw := `{"receipt":{"provenance":{"execution_root":"WORKER","local_evidence":[{"authored_ref":"logs/check.txt","source_path":"logs/check.txt","path":".claude/evidence/s9/captured","sha256":"digest"}]}},"unit_results":[{"evidence_refs":["logs/check.txt","path:legacy.log#sha256=abc","old-id","test://actual-run"]}],"checks":[{"evidence_refs":["logs/check.txt"]}]}`
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	doc["receipt"].(map[string]any)["provenance"].(map[string]any)["execution_root"] = workerRoot
	var b strings.Builder
	repairLeadEvidenceHelp(mainRoot, &b, doc)
	help := b.String()
	for _, want := range []string{filepath.Join(mainRoot, ".claude/evidence/s9/captured"), "Builder root " + workerRoot, "original ref \"logs/check.txt\"", "Recorded Main check evidence candidate: " + filepath.Join(mainRoot, "logs/check.txt"), "historical/unarchived reference", "test://actual-run"} {
		if !strings.Contains(help, want) {
			t.Fatalf("projection omitted %q: %s", want, help)
		}
	}
	if strings.Contains(help, filepath.Join(mainRoot, "path:legacy.log")) || strings.Contains(help, filepath.Join(mainRoot, "old-id")) {
		t.Fatalf("unarchived Builder evidence mislabeled Main: %s", help)
	}
	// Captures are recorded navigation. Ordinary reminders do not re-read/hash
	// source bytes, require the Worker to survive, or manufacture a new receipt.
	if _, err := os.Stat(filepath.Join(mainRoot, ".claude")); !os.IsNotExist(err) {
		t.Fatal("projection wrote capture")
	}
	delete(doc, "receipt")
	b.Reset()
	repairLeadEvidenceHelp(mainRoot, &b, doc)
	if !strings.Contains(b.String(), "historical source root unspecified") {
		t.Fatalf("legacy source guessed Main: %s", b.String())
	}
	if strings.Contains(b.String(), "Recorded Main check evidence candidate:") || strings.Contains(b.String(), filepath.Join(mainRoot, "logs/check.txt")) {
		t.Fatalf("legacy check evidence guessed Main: %s", b.String())
	}
}
