package transition

import (
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/acceptance"
	"os"
	"path/filepath"
	"strings"
)

// Normal S11 commands carry the human's decision; an explicit evidence-only
// request remains the separate compatibility entry for existing callers.
func normalHumanReleaseRequest(request Request) bool {
	switch request.TransitionID {
	case "TR-025", "TR-026", "TR-027", "TR-028", "TR-029", "TR-030":
		return strings.TrimSpace(stringValue(request.Params["disposition"])) != ""
	}
	return false
}

func validateNormalHumanRelease(request Request) error {
	if strings.TrimSpace(request.Actor) == "" {
		return fmt.Errorf("human decision caller is required")
	}
	disposition := stringValue(request.Params["disposition"])
	events := map[string]string{"TR-025": "approve", "TR-026": "defer", "TR-027": "reject_defect", "TR-028": "reject_acceptance", "TR-029": "reject_release_audit", "TR-030": "abort"}
	if events[request.TransitionID] != disposition {
		return fmt.Errorf("human disposition %q does not select %s", disposition, request.TransitionID)
	}
	if strings.TrimSpace(stringValue(request.Params["decision_id"])) == "" || humanReleaseDecisionPath(request) == "" {
		return fmt.Errorf("current human decision application identity and path are missing")
	}
	return nil
}

func humanReleaseDecisionPath(request Request) string {
	path := stringValue(request.Params["decision_path"])
	if path == "" {
		path = stringValue(request.Params["path"])
	}
	return strings.TrimSpace(path)
}

func humanReleaseLocalPath(root string, request Request) (string, error) {
	path := humanReleaseDecisionPath(request)
	if filepath.IsAbs(path) {
		var err error
		projectRoot, rootErr := filepath.Abs(root)
		if rootErr != nil {
			return "", rootErr
		}
		path, err = filepath.Rel(projectRoot, path)
		if err != nil {
			return "", err
		}
	}
	path = filepath.Clean(path)
	if path == "." || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("human decision path must be within the current project")
	}
	return path, nil
}

func humanReleaseReplay(root string, state map[string]any, request Request) (bool, error) {
	if err := validateNormalHumanRelease(request); err != nil {
		return false, err
	}
	items, _ := state["evidence"].([]any)
	for _, raw := range items {
		entry, _ := raw.(map[string]any)
		if stringValue(entry["id"]) != stringValue(request.Params["decision_id"]) {
			continue
		}
		path, err := humanReleaseLocalPath(root, request)
		if err != nil {
			return false, err
		}
		if stringValue(entry["kind"]) != "human_decision" || filepath.Clean(stringValue(entry["path"])) != path {
			return false, fmt.Errorf("human decision identity already records different content")
		}
		// The consumed transition is the durable original decision fact. A later
		// edit or deletion of the private application cannot revoke it or create
		// a new human decision. Do not reopen the authoring file as a replay gate.
		if stringValue(entry["consumed_by"]) != request.TransitionID {
			return false, fmt.Errorf("human decision has not completed this transition")
		}
		return true, nil
	}
	return false, nil
}

// Only the latest registered audit is needed to keep the CAS attached to the
// release the caller saw. This reads its identity, not its historical proof.
func latestReleaseAuditID(state map[string]any) string {
	items, _ := state["evidence"].([]any)
	baseline, _ := state["baseline"].(map[string]any)
	for i := len(items) - 1; i >= 0; i-- {
		item, _ := items[i].(map[string]any)
		kind := stringValue(item["kind"])
		if (kind == "release_audit" || kind == "release_audit_record") && item["status"] == "valid" && item["invalidated_by"] == nil && integer(item["baseline_generation"]) == integer(baseline["generation"]) && acceptance.CurrentReviewArtifact(state, stringValue(item["review_plan_id"])) {
			return stringValue(item["id"])
		}
	}
	return ""
}

// Called only inside the original transition Writer mutation. The artifact is
// the original decision format; registration and consumption share its CAS.
func recordHumanReleaseDecision(root string, state map[string]any, request Request) error {
	path, err := humanReleaseLocalPath(root, request)
	if err != nil {
		return err
	}
	id := stringValue(request.Params["decision_id"])
	items, _ := state["evidence"].([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if stringValue(item["id"]) == id {
			return fmt.Errorf("human decision already recorded; read the existing result")
		}
	}
	answer := map[string]any{
		"decision_id": id, "decision": stringValue(request.Params["disposition"]),
		"disposition": stringValue(request.Params["disposition"]), "reason": stringValue(request.Params["reason"]),
		"approved_by": request.Actor, "runtime_id": state["runtime_id"],
		"target_cursor":    map[string]any{"state": "awaiting_human_release", "phase": nil},
		"scope":            "runtime_release:" + stringValue(state["runtime_id"]),
		"release_audit_id": latestReleaseAuditID(state),
	}
	data, err := json.MarshalIndent(answer, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, path), data, 0o644); err != nil {
		return err
	}
	baseline, _ := state["baseline"].(map[string]any)
	state["evidence"] = append(items, map[string]any{
		"id": id, "kind": "human_decision", "path": filepath.ToSlash(path), "status": "valid",
		"baseline_generation": baseline["generation"],
		"produced_by":         []any{request.Actor}, "responsibility_id": "Human Release Decision",
		"scope_refs":     []any{"runtime_release:" + stringValue(state["runtime_id"])},
		"invalidated_by": nil, "invalidation_rule": nil, "invalidation_reason": nil,
	})
	rows, _ := state["evidence"].([]any)
	acceptance.StampReviewIdentity(state, rows[len(rows)-1].(map[string]any))
	return nil
}
