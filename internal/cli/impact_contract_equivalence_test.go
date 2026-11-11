package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

func TestImpactOriginalProfessionalEquivalence(t *testing.T) {
	var answer repair.ChangeImpactRequest
	original := `{"change_types":["implementation"],"changed_artifacts":["src/file.go"],"decisions":[{"source_id":"case-1","target_id":"src/file.go","relation":"changed","rule_id":"","decision":"reverify","scope":[],"rationale":"changed behavior","recovery_evidence":[]}],"integration_notes":"check pending"}`
	if err := repair.DecodeOriginalChangeImpact([]byte(original), &answer); err != nil {
		t.Fatal(err)
	}
	var accepted repair.ChangeImpact
	stored := `{"change_types":["implementation"],"changed_artifacts":[{"id":"changed-src-file.go","path":"src/file.go","sha256":""}],"decisions":[{"source_id":"case-1","target_id":"src/file.go","relation":"changed","rule_id":"","decision":"reverify","responsibility_id":null,"scope":[],"rationale":"changed behavior","recovery_evidence":[]}],"escalation_level":"assignment","integration_notes":"check pending"}`
	if err := json.Unmarshal([]byte(stored), &accepted); err != nil {
		t.Fatal(err)
	}
	if !sameImpactProfessionalAnswer(answer, accepted) {
		t.Fatal("equivalent original and stored impact should not be received twice")
	}
	answer.ChangedArtifacts[0].Path = "src/other.go"
	if sameImpactProfessionalAnswer(answer, accepted) {
		t.Fatal("changing only a path must be recognized as a new judgment")
	}
	answer.ChangedArtifacts[0].Path = "src/file.go"
	answer.IntegrationNotes = "checks passed"
	if sameImpactProfessionalAnswer(answer, accepted) {
		t.Fatal("changing integration notes must be recognized as a new judgment")
	}
}

func TestImpactOriginalPublicHookRecoversConflictingStagedName(t *testing.T) {
	root, _, _, _, _ := nativeS9WorkerFixture(t)
	before := repairDispatchSnapshot(t, root)
	impactPath, err := prepareRepairImpactQuestionnaire(root, before.State)
	if err != nil {
		t.Fatal(err)
	}
	// A process interrupted before committing may leave an incomplete file at
	// the revision-derived name. It is not an accepted Impact or author error.
	sessionID := stringValue(repairPointerCLI(before.State)["session_id"])
	occupied := filepath.Join(root, ".claude/review/repair/change-impact", "impact-"+dispatchSlug(sessionID)+".json")
	if err := os.MkdirAll(filepath.Dir(occupied), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(occupied, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	main := policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: root, ToolName: "Bash", ToolInput: map[string]any{"command": "true"}}
	output, diagnostic := s9ReferencePublicHook(t, root, main)
	after := repairPointerCLI(repairDispatchSnapshot(t, root).State)
	accepted := stringValue(after["impact_ref"])
	if accepted == "" || filepath.Clean(resolveRootPath(root, accepted)) == filepath.Clean(occupied) {
		t.Fatalf("staged collision stopped original reception: %v output=%s diagnostic=%s answer=%s", after, output, diagnostic, impactPath)
	}
	if data, err := os.ReadFile(occupied); err != nil || string(data) != `{}` {
		t.Fatalf("unaccepted staged file was modified: %q %v", data, err)
	}
}

func TestImpactOriginalPublicHookDoesNotReceiveEquivalentAnswerTwice(t *testing.T) {
	root, _, _, _, _ := nativeS9WorkerFixture(t)
	before := repairDispatchSnapshot(t, root)
	impactPath, err := prepareRepairImpactQuestionnaire(root, before.State)
	if err != nil {
		t.Fatal(err)
	}
	writeRepairLeadJSON(t, impactPath, map[string]any{
		"change_types":      []string{"implementation"},
		"changed_artifacts": []string{"src/file.go"},
		"decisions":         []any{map[string]any{"source_id": "case", "target_id": "file", "relation": "affects", "rule_id": "", "decision": "reverify", "scope": []string{}, "rationale": "actual change", "recovery_evidence": []string{}}},
	})
	main := policy.Input{Event: "PreToolUse", AgentID: "Main", CWD: root, ToolName: "Bash", ToolInput: map[string]any{"command": "true"}}
	output, diagnostic := s9ReferencePublicHook(t, root, main)
	first := stringValue(repairPointerCLI(repairDispatchSnapshot(t, root).State)["impact_ref"])
	if first == "" {
		t.Fatalf("original impact not accepted: output=%s diagnostic=%s", output, diagnostic)
	}
	s9ReferencePublicHook(t, root, main)
	second := stringValue(repairPointerCLI(repairDispatchSnapshot(t, root).State)["impact_ref"])
	if second != first {
		t.Fatalf("equivalent author judgment created another impact: first=%s second=%s", first, second)
	}
}

func TestImpactRetryNeverRebindsOriginalAnswerToAnotherSession(t *testing.T) {
	root, _, _, _, _ := nativeS9WorkerFixture(t)
	original := repairDispatchSnapshot(t, root)
	answerPath, err := prepareRepairImpactQuestionnaire(root, original.State)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := os.ReadFile(answerPath)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, ".claude/loop-state.json")
	journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
	store := runtime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).WithContext(t.Context())
	_, err = store.Update(original.Revision, runtime.Mutation{
		EventID: "evt-impact-test-new-session", TransitionID: "IMPACT-TEST", Event: "impact_test_new_session",
		Actor: "Main", RuntimeID: stringValue(original.State["runtime_id"]), IdempotencyKey: "impact-test:new-session",
		RetainLastTransition: true, OccurredAt: time.Now().UTC(),
		Apply: func(state map[string]any) error {
			pointer := repairPointerCLI(state)
			pointer["session_id"] = "repair-session-another"
			pointer["case_id"] = "investigation-case-another"
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	handled, code := saveRepairImpactQuestionnaireBytes(root, original, answerPath, answer, &output, t.Context())
	if !handled || code != 0 || !strings.Contains(output.String(), "original S9 Impact responsibility changed") {
		t.Fatalf("original responsibility not preserved: handled=%v code=%d feedback=%s", handled, code, output.String())
	}
	current := repairPointerCLI(repairDispatchSnapshot(t, root).State)
	if current["session_id"] != "repair-session-another" || stringValue(current["impact_ref"]) != "" {
		t.Fatalf("old answer was attributed to a different session: %#v", current)
	}
	if after, err := os.ReadFile(answerPath); err != nil || !bytes.Equal(after, answer) {
		t.Fatalf("original answer changed while reporting a system race: %v", err)
	}
}

func TestImpactConcurrentAcceptanceContinuesRegisteredTargeted(t *testing.T) {
	root, targetPath, _ := s9ReferenceMixedTargetedFixture(t)
	current := repairDispatchSnapshot(t, root)
	impactRef := stringValue(repairPointerCLI(current.State)["impact_ref"])
	registered, err := os.ReadFile(resolveRootPath(root, impactRef))
	if err != nil {
		t.Fatal(err)
	}
	var originalImpact repair.ChangeImpact
	if err := json.Unmarshal(registered, &originalImpact); err != nil {
		t.Fatal(err)
	}
	impactPath, err := prepareRepairImpactQuestionnaire(root, current.State)
	if err != nil {
		t.Fatal(err)
	}
	writeRepairLeadJSON(t, impactPath, originalImpact)
	answer, err := os.ReadFile(impactPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(targetPath); err != nil {
		t.Fatal(err)
	}
	// The in-flight observer started before the other event's acceptance.
	// Its saved snapshot predates impact_ref; the persisted current state keeps
	// the real accepted Impact and the original registered verifier.
	stateBytes, err := json.Marshal(current.State)
	if err != nil {
		t.Fatal(err)
	}
	var oldState map[string]any
	if err := json.Unmarshal(stateBytes, &oldState); err != nil {
		t.Fatal(err)
	}
	delete(repairPointerCLI(oldState), "impact_ref")
	delete(repairPointerCLI(oldState), "impact_sha256")
	old := runtime.Snapshot{Revision: current.Revision, State: oldState}
	statePath := filepath.Join(root, ".claude/loop-state.json")
	journalPath := filepath.Join(root, ".claude/loop-events.jsonl")
	store := runtime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).WithContext(t.Context())
	_, err = store.Update(current.Revision, runtime.Mutation{
		EventID: "evt-impact-concurrent-acceptance", TransitionID: "IMPACT-TEST", Event: "impact_concurrent_acceptance",
		Actor: "Main", RuntimeID: stringValue(current.State["runtime_id"]), IdempotencyKey: "impact-test:concurrent-acceptance",
		RetainLastTransition: true, OccurredAt: time.Now().UTC(), Apply: func(map[string]any) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	handled, code := saveRepairImpactQuestionnaireBytes(root, old, impactPath, answer, &output, t.Context())
	if !handled || code != 0 || !strings.Contains(output.String(), "already received") {
		t.Fatalf("concurrent acceptance not retained: handled=%v code=%d output=%s", handled, code, output.String())
	}
	if _, err := os.Stat(targetPath); err != nil {
		t.Fatalf("original registered Targeted successor was not prepared in this event: %v; output=%s", err, output.String())
	}
	after := repairPointerCLI(repairDispatchSnapshot(t, root).State)
	if stringValue(after["impact_ref"]) != impactRef {
		t.Fatalf("concurrent acceptance was replaced: %#v", after)
	}
}
