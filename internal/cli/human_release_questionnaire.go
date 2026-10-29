package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/acceptance"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/transition"
)

func stageHumanReleaseQuestionnaireContext(root string, request policy.Input, snapshot runtime.Snapshot) string {
	if !mainQuestionnaireViewer(request) {
		return ""
	}
	if stringValue(nestedStateValue(snapshot.State, "lifecycle", "state")) != "awaiting_human_release" {
		return ""
	}
	var packages []string
	for _, kind := range []string{acceptance.ManifestAcceptance, acceptance.ManifestReleaseAudit} {
		found := false
		rows := stateEvidence(snapshot.State)
		for i := len(rows) - 1; i >= 0; i-- {
			entry, _ := rows[i].(map[string]any)
			if stringValue(entry["kind"]) != kind || stringValue(entry["status"]) != "valid" ||
				integerValue(entry["baseline_generation"]) != integerValue(nestedStateValue(snapshot.State, "baseline", "generation")) ||
				integerValue(entry["review_round"]) != integerValue(nestedStateValue(snapshot.State, "review", "round")) {
				continue
			}
			file, err := safeS10Path(root, stringValue(entry["path"]))
			if err != nil {
				return fmt.Sprintf("S11 %s recorded material path is unavailable: %v", kind, err)
			}
			data, err := os.ReadFile(file)
			if err != nil {
				return fmt.Sprintf("S11 %s material %s cannot be read: %v", kind, file, err)
			}
			var envelope struct {
				ManifestPath string `json:"audit_manifest_path"`
			}
			if err := json.Unmarshal(data, &envelope); err != nil {
				return fmt.Sprintf("S11 recorded %s material %s cannot be decoded: %v", kind, file, err)
			}
			manifestPath, err := absoluteQuestionnairePath(root, envelope.ManifestPath)
			if err != nil {
				return fmt.Sprintf("S11 recorded %s questionnaire path is missing: %v", kind, err)
			}
			found = true
			reportPath := strings.TrimSuffix(manifestPath, ".json") + ".md"
			if _, err := os.Stat(reportPath); err == nil {
				packages = append(packages, fmt.Sprintf("%s readable report: %s; original questionnaire: %s", kind, reportPath, manifestPath))
			} else {
				packages = append(packages, fmt.Sprintf("%s readable report is unavailable at %s (%v); original questionnaire: %s", kind, reportPath, err, manifestPath))
			}
			var report acceptance.Manifest
			if data, err := os.ReadFile(manifestPath); err == nil && json.Unmarshal(data, &report) == nil {
				for _, risk := range report.Risks {
					packages = append(packages, fmt.Sprintf("Reported risk: %s; impact/handling: %s; owner: %s; existing follow-up: %s", risk.ID, risk.Impact, risk.Owner, risk.TrackingRef))
				}
			}
			break
		}
		if !found {
			packages = append(packages, "S11 recorded "+kind+" material is missing; Main must resolve this material gap.")
		}
	}
	path, decisionID, err := humanReleaseQuestionnairePath(root, snapshot.State)
	if err != nil {
		return fmt.Sprintf("S11 current human decision path is unavailable: %v. Main must resolve the recorded material gap; do not reconstruct metadata.", err)
	}
	context := humanDecisionApplicationContext(strings.Join(packages, "\n"), path, decisionID)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		context += " The stage-entry application is missing at that exact path; this is a system preparation gap, not a reason to wait for another Pre."
	} else if err != nil {
		context += fmt.Sprintf(" Application inspection failed: %v.", err)
	}
	return context
}

func humanReleaseQuestionnairePath(root string, state map[string]any) (string, string, error) {
	runtimeID := strings.TrimSpace(stringValue(state["runtime_id"]))
	if runtimeID == "" {
		return "", "", fmt.Errorf("current Runtime has no runtime_id")
	}
	generation := integerValue(nestedStateValue(state, "baseline", "generation"))
	round := integerValue(nestedStateValue(state, "review", "round"))
	if generation < 1 || round < 1 {
		return "", "", fmt.Errorf("current Runtime has no positive baseline generation and review round")
	}
	segment := auditPathSegment.ReplaceAllString(runtimeID, "-")
	if segment == "" || segment == "." || segment == ".." {
		return "", "", fmt.Errorf("current runtime_id cannot form the human decision application path")
	}
	var auditID string
	rows := stateEvidence(state)
	if stringValue(nestedStateValue(state, "lifecycle", "state")) != "awaiting_human_release" {
		for i := len(rows) - 1; i >= 0; i-- {
			entry, _ := rows[i].(map[string]any)
			if stringValue(entry["kind"]) == "human_decision" && runtime.HumanDecisionEvidenceConsumed(entry) && integerValue(entry["baseline_generation"]) == generation && integerValue(entry["review_round"]) == round {
				path, err := absoluteQuestionnairePath(root, stringValue(entry["path"]))
				return path, stringValue(entry["id"]), err
			}
		}
	}
	for i := len(rows) - 1; i >= 0; i-- {
		entry, _ := rows[i].(map[string]any)
		if stringValue(entry["kind"]) == acceptance.ManifestReleaseAudit && stringValue(entry["status"]) == "valid" && integerValue(entry["baseline_generation"]) == generation && integerValue(entry["review_round"]) == round {
			auditID = stringValue(entry["id"])
			break
		}
	}
	if auditID == "" {
		return "", "", fmt.Errorf("current release audit record is missing")
	}
	decisionID := fmt.Sprintf("hd-s11-%s-g%d-r%d-p%d-%s", segment, generation, round, integerValue(nestedStateValue(state, "lifecycle", "phase_revision")), auditPathSegment.ReplaceAllString(auditID, "-"))
	path, err := absoluteQuestionnairePath(root, filepath.ToSlash(filepath.Join(".claude", "decisions", decisionID+".json")))
	return path, decisionID, err
}

func prepareHumanDecisionApplication(path string, state map[string]any, decisionID string) error {
	data, err := json.MarshalIndent(map[string]any{
		"decision": "", "disposition": "", "decision_id": decisionID,
		"reason": "", "approved_by": "", "runtime_id": stringValue(state["runtime_id"]),
		"target_cursor": map[string]any{"state": "awaiting_human_release", "phase": nil},
		"scope":         "runtime_release:" + stringValue(state["runtime_id"]),
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode human decision application: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create human decision application directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if os.IsExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create human decision application: %w", err)
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		return fmt.Errorf("write human decision application: %w", err)
	}
	return file.Close()
}

func humanDecisionApplicationContext(packageText, path, decisionID string) string {
	return fmt.Sprintf("S11 human review materials:\n%s\nHuman decision application: %s (system decision_id %s). The human chooses the actual disposition and may give a reason: approve → release_authorized (authorization recorded, no deployment); defer → paused; reject_defect → S8 investigation; reject_acceptance → acceptance; reject_release_audit → release_audit; abort → aborted. After the human explicitly chooses, Main faithfully fills only disposition and optional reason in this current application and saves it using edits, scripts or MCP. The normal save records and consumes the decision through the original Writer and follows its fixed exit automatically; no command or evidence registration is needed. Empty disposition remains a draft. Main must not invent the human choice; saving is not identity authentication. Leave decision, decision_id, approved_by, runtime_id, scope and target_cursor as supplied, including blanks: these are system metadata populated by the original Writer when applicable; blank metadata is not missing human input. Authorization does not publish or deploy.", packageText, path, decisionID)
}

// A normal save carries the human's explicit choice, not an identity proof.
func runSavedHumanReleaseQuestionnaire(root string, request policy.Input, stdout, stderr io.Writer) (bool, int) {
	if request.Event != "PostToolUse" || request.Error != "" {
		return false, 0
	}
	if failed, _ := request.ToolResponse["is_error"].(bool); failed {
		return false, 0
	}
	actual, saved := savedQuestionnairePath(request)
	if !saved {
		if path, ok := request.ToolResponse["file_path"].(string); ok {
			located := request
			located.ToolInput = map[string]any{"file_path": path}
			actual, saved = savedQuestionnairePath(located)
		}
	}
	if !saved {
		return false, 0
	}
	statePath, journalPath := filepath.Join(root, ".claude", "loop-state.json"), filepath.Join(root, ".claude", "loop-events.jsonl")
	snapshot, err := runtime.NewStore(statePath, journalPath).Snapshot()
	if err != nil || lifecycleState(snapshot.State) != "awaiting_human_release" {
		return false, 0
	}
	path, id, err := humanReleaseQuestionnairePath(root, snapshot.State)
	if err != nil || !samePath(actual, path) {
		return false, 0
	}
	feedback := func(message string) (bool, int) {
		return true, auditQuestionnaireFeedback(stdout, "S11 decision application: "+path+". "+message)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return feedback("Saved file cannot be read: " + err.Error())
	}
	var answer struct {
		Disposition string `json:"disposition"`
		Reason      string `json:"reason"`
	}
	if err = json.Unmarshal(data, &answer); err != nil {
		return feedback("Saved decision fields cannot be decoded: " + err.Error() + ". Fill disposition and optional reason using strings.")
	}
	answer.Disposition = strings.TrimSpace(answer.Disposition)
	if answer.Disposition == "" {
		return feedback("Draft only: the actual human disposition is not supplied. No Runtime or journal decision was recorded. Choose only after the human's explicit intent: approve, defer, reject_defect, reject_acceptance, reject_release_audit, abort.")
	}
	transitionID, err := runtime.HumanReleaseTransitionID(answer.Disposition)
	if err != nil {
		return feedback("Disposition must be approve, defer, reject_defect, reject_acceptance, reject_release_audit, or abort; the actual choice is still required.")
	}
	actor := strings.TrimSpace(request.AgentID)
	if actor == "" {
		actor = "Main"
	}
	next, err := transition.Apply(root, statePath, journalPath, transition.Request{
		TransitionID: transitionID, ExpectedRevision: snapshot.Revision, ExpectedRuntimeID: stringValue(snapshot.State["runtime_id"]), Actor: actor,
		Params: map[string]any{"decision_id": id, "decision_path": path, "disposition": answer.Disposition, "reason": answer.Reason},
	})
	if err != nil {
		if errors.Is(err, runtime.ErrStaleRevision) {
			return feedback("The Runtime changed during decision reception. Main must inspect the original decision receipt and the current S11 application before continuing the original transition: " + err.Error() + ". Preserve the human's answer; a changed release scope requires renewed human judgment, not a guessed revision or an automatic retry.")
		}
		return feedback("No successful transition was confirmed: " + err.Error() + ". The original Writer reports the actual recording result; do not guess another path or repeat already accepted work.")
	}
	message := fmt.Sprintf("Recorded and consumed %s through %s; current cursor %s. Actual reason: %s.", answer.Disposition, transitionID, lifecycleState(next.State), answer.Reason)
	if transitionID == "TR-028" || transitionID == "TR-029" {
		paths, err := prepareAuditReworkQuestionnaires(root, next.State, transitionID, path)
		if err != nil {
			return feedback(message + " Decision is recorded, but S10 rework preparation failed: " + err.Error())
		}
		message += " Current S10 report(s), retaining observations with final outcome cleared once: " + strings.Join(paths, ", ") + "."
	} else if transitionID == "TR-027" {
		message += " " + stageInvestigationLeadQuestionnaireContext(root, policy.Input{Event: "PreToolUse"}, next)
	}
	return feedback(message)
}

func evidenceIncludesScope(entry map[string]any, want string) bool {
	refs, _ := entry["scope_refs"].([]any)
	for _, ref := range refs {
		if stringValue(ref) == want {
			return true
		}
	}
	return false
}

func evidenceEntryByID(state map[string]any, id string) map[string]any {
	for _, raw := range stateEvidence(state) {
		entry, _ := raw.(map[string]any)
		if entry != nil && stringValue(entry["id"]) == id {
			return entry
		}
	}
	return nil
}
