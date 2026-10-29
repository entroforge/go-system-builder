package targetedauthoring

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const SharedTargetedAuthoringProtocol = "shared-checkout-targeted-v1"

// TargetedAuthoring is a machine declaration in the original registered TASK.
// It does not record acceptance, platform execution or a future Final HEAD.
type TargetedAuthoring struct {
	Protocol               string `json:"protocol"`
	RuntimeID              string `json:"runtime_id"`
	BaselineGeneration     int    `json:"baseline_generation"`
	CaseID                 string `json:"case_id"`
	ContractID             string `json:"contract_id"`
	SessionID              string `json:"session_id"`
	PlanRef                string `json:"plan_ref"`
	PlanSHA256             string `json:"plan_sha256"`
	ContractRef            string `json:"contract_ref"`
	ContractSHA256         string `json:"contract_sha256"`
	OriginalAssignmentID   string `json:"original_assignment_id"`
	PerformingAssignmentID string `json:"performing_assignment_id"`
	Owner                  string `json:"owner"`
}

// Verify proves the exact original TASK and manifest authorities. A missing
// actor/TASK is unknown source, never evidence for a legacy protocol.
func Verify(root string, state map[string]any, expected TargetedAuthoring, manifestAssignment string, contexts ...context.Context) (bool, error) {
	scope, err := newReadScope(state, sourceContext(contexts))
	if err != nil {
		return false, err
	}
	return verify(scope, root, expected, manifestAssignment)
}

func verify(scope *readScope, root string, expected TargetedAuthoring, manifestAssignment string) (bool, error) {
	if err := scope.ctx.Err(); err != nil {
		return false, err
	}
	original, performing, owner := expected.OriginalAssignmentID, expected.PerformingAssignmentID, expected.Owner
	agent, err := scope.lookup("agents", owner)
	if err != nil {
		return false, err
	}
	if agent == nil {
		return false, fmt.Errorf("Targeted original registered Agent %s is missing", owner)
	}
	taskID := TaskID(expected.SessionID, performing)
	matches := 0
	for _, id := range stringSliceFromAny(agent["task_ids"]) {
		if err := scope.ctx.Err(); err != nil {
			return false, err
		}
		if id == taskID {
			matches++
		}
	}
	if matches != 1 {
		return false, fmt.Errorf("Targeted exact original TASK %s association missing or duplicated", taskID)
	}
	task, err := scope.lookup("tasks", taskID)
	if err != nil {
		return false, err
	}
	if task == nil {
		return false, fmt.Errorf("Targeted exact original TASK %s is missing", taskID)
	}
	fields, err := scope.task(root, task)
	if err != nil {
		return false, err
	}
	ownerMatches := 0
	for _, registered := range stringSliceFromAny(task["owner_agent_ids"]) {
		if registered == owner {
			ownerMatches++
		}
	}
	if ownerMatches != 1 {
		return false, fmt.Errorf("Targeted original TASK owner differs")
	}
	declaration, hasDeclaration := fields["targeted_authoring"]
	if hasDeclaration {
		var contract TargetedAuthoring
		decoder := json.NewDecoder(bytes.NewReader(declaration))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&contract); err != nil {
			return false, fmt.Errorf("Targeted authoring declaration: %w", err)
		}
		if contract != expected || contract.Protocol != SharedTargetedAuthoringProtocol || original == "" || owner == "" {
			return false, fmt.Errorf("Targeted authoring declaration differs from current original responsibility")
		}
		if contract.RuntimeID == "" || contract.BaselineGeneration < 1 || contract.SessionID == "" || contract.PlanRef == "" || contract.PlanSHA256 == "" || contract.ContractRef == "" || contract.ContractSHA256 == "" {
			return false, fmt.Errorf("Targeted authoring source binding is incomplete")
		}
		var actualTask, actualSession, actualAssignment string
		_ = json.Unmarshal(fields["task_id"], &actualTask)
		_ = json.Unmarshal(fields["session_id"], &actualSession)
		_ = json.Unmarshal(fields["assignment_id"], &actualAssignment)
		if actualTask != taskID || actualSession != contract.SessionID || actualAssignment != performing {
			return false, fmt.Errorf("Targeted original TASK binding differs")
		}
	}
	team, err := scope.lookup("teams", stringField(agent["team_id"]))
	if err != nil {
		return false, err
	}
	if team == nil {
		return false, fmt.Errorf("Targeted registered Team is missing")
	}
	ref := stringField(team["manifest_ref"])
	data, err := scope.read(root, ref, stringField(team["manifest_sha256"]))
	if err != nil {
		return false, fmt.Errorf("Targeted original manifest: %w", err)
	}
	if err := scope.ctx.Err(); err != nil {
		return false, err
	}
	if err := workpacket.ValidateUniqueJSON(data); err != nil {
		return false, err
	}
	var manifest struct {
		RuntimeID          string `json:"runtime_id"`
		BaselineGeneration int    `json:"baseline_generation"`
		WorkgroupID        string `json:"workgroup_id"`
		Assignments        []struct {
			AssignmentID string   `json:"assignment_id"`
			AgentID      string   `json:"agent_id"`
			WritePaths   []string `json:"write_paths"`
		} `json:"assignments"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return false, err
	}
	if err := scope.ctx.Err(); err != nil {
		return false, err
	}
	if manifest.RuntimeID != expected.RuntimeID || manifest.BaselineGeneration != expected.BaselineGeneration || manifest.WorkgroupID != stringField(team["id"]) {
		return false, fmt.Errorf("Targeted original manifest identity differs")
	}
	matches = 0
	for _, a := range manifest.Assignments {
		if err := scope.ctx.Err(); err != nil {
			return false, err
		}
		if a.AssignmentID == manifestAssignment && a.AgentID == owner {
			matches++
			if len(a.WritePaths) != 0 || stringField(agent["prompt_ref"]) != ref+"#"+a.AssignmentID {
				return false, fmt.Errorf("Targeted manifest author or product write scope differs")
			}
		}
	}
	if matches != 1 {
		return false, fmt.Errorf("Targeted original manifest does not bind its independent owner")
	}
	return hasDeclaration, nil
}

// TaskID is the existing S9 dispatch's exact deterministic TASK identity.
func TaskID(sessionID, performing string) string {
	sum := sha256.Sum256([]byte(sessionID + "|" + performing))
	number := uint64(sum[0])<<24 | uint64(sum[1])<<16 | uint64(sum[2])<<8 | uint64(sum[3])
	return fmt.Sprintf("TASK-9%d", number)
}

// TargetedSharedAnswerPath is deterministic after authority verification by the
// caller. The source digest uses the same original Case/Contract/Session/Plan tuple
// as the legacy questionnaire, so attempts do not depend on Runtime revision.
func TargetedSharedAnswerPath(root, relativeLegacy string) (string, error) {
	prefix := ".claude/submissions/s9/"
	rel, err := filepath.Rel(root, relativeLegacy)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if !strings.HasPrefix(rel, prefix) {
		return "", fmt.Errorf("Targeted original relative path differs")
	}
	rel = strings.TrimPrefix(rel, prefix)
	dir, name := filepath.Split(rel)
	name = strings.Replace(name, "targeted-reverification-", "targeted-", 1)
	return filepath.Join(root, ".loop-submissions/s9", dir, name), nil
}

func stringField(v any) string { s, _ := v.(string); return s }
func stringSliceFromAny(v any) []string {
	if s, ok := v.([]string); ok {
		return s
	}
	var result []string
	rows, _ := v.([]any)
	for _, raw := range rows {
		if s, ok := raw.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
func stringMapField(v any) map[string]string {
	if m, ok := v.(map[string]string); ok {
		return m
	}
	result := map[string]string{}
	rows, _ := v.(map[string]any)
	for k, raw := range rows {
		result[k] = stringField(raw)
	}
	return result
}

// ControlPaths examines only actual observed ordinary Targeted candidates.
// Agent prompt references preselect the original performing assignment without
// reading unrelated TASK bodies. Original source bytes are reused only within
// this invocation; a new consumer call reopens and verifies them again.
func ControlPaths(root string, state map[string]any, paths []string, contexts ...context.Context) ([]string, error) {
	ctx := sourceContext(contexts)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	wanted := map[string]bool{}
	performers := map[string]bool{}
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if strings.HasPrefix(path, ".loop-submissions/s9/") {
			name := filepath.Base(path)
			if strings.HasPrefix(name, "targeted-") && strings.HasSuffix(name, ".json") {
				wanted[path] = true
				performers[strings.TrimSuffix(strings.TrimPrefix(name, "targeted-"), ".json")] = true
			}
		}
	}
	if len(wanted) == 0 {
		return nil, nil
	}
	scope, err := newReadScope(state, ctx)
	if err != nil {
		return nil, err
	}
	result := map[string]string{}
	for owner, agent := range scope.rows["agents"] {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		_, alias, ok := strings.Cut(stringField(agent["prompt_ref"]), "#")
		if !ok || !strings.HasPrefix(alias, "assignment-s9-verification-") {
			continue
		}
		performing := "repair-assignment-verification-" + strings.TrimPrefix(alias, "assignment-s9-verification-")
		if !performers[slug(performing)] {
			continue
		}
		if _, err := scope.lookup("agents", owner); err != nil {
			return nil, err
		}
		for _, id := range stringSliceFromAny(agent["task_ids"]) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			task, err := scope.lookup("tasks", id)
			if err != nil {
				return nil, err
			}
			if task == nil {
				return nil, fmt.Errorf("Targeted original registered TASK %s is missing", id)
			}
			fields, err := scope.task(root, task)
			if err != nil {
				return nil, err
			}
			raw, ok := fields["targeted_authoring"]
			if !ok {
				continue
			}
			var contract TargetedAuthoring
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&contract); err != nil {
				return nil, err
			}
			relative, err := contract.AnswerRelativePath()
			if err != nil {
				return nil, err
			}
			if !wanted[relative] {
				continue
			}
			bare := strings.TrimPrefix(contract.OriginalAssignmentID, "repair-assignment-")
			if contract.Owner != owner || contract.PerformingAssignmentID != performing || performing != "repair-assignment-verification-"+slug(bare) {
				return nil, fmt.Errorf("Targeted original performing identity differs")
			}
			normal, err := verify(scope, root, contract, alias)
			if err != nil {
				return nil, err
			}
			if normal {
				if prior := result[relative]; prior != "" && prior != owner {
					return nil, fmt.Errorf("Targeted original path has ambiguous registered authors")
				}
				result[relative] = owner
			}
		}
	}
	var out []string
	for path := range result {
		out = append(out, path)
	}
	sort.Strings(out)
	return out, ctx.Err()
}

func FromState(state map[string]any, original, performing, owner string) TargetedAuthoring {
	review, _ := state["review"].(map[string]any)
	p, _ := review["repair"].(map[string]any)
	baseline, _ := state["baseline"].(map[string]any)
	generation := 0
	switch n := baseline["generation"].(type) {
	case int:
		generation = n
	case float64:
		generation = int(n)
	case json.Number:
		v, _ := n.Int64()
		generation = int(v)
	}
	return TargetedAuthoring{Protocol: SharedTargetedAuthoringProtocol, RuntimeID: stringField(state["runtime_id"]), BaselineGeneration: generation, CaseID: stringField(p["case_id"]), ContractID: stringField(p["contract_id"]), SessionID: stringField(p["session_id"]), PlanRef: stringField(p["plan_ref"]), PlanSHA256: stringField(p["plan_sha256"]), ContractRef: stringField(p["contract_ref"]), ContractSHA256: stringField(p["contract_sha256"]), OriginalAssignmentID: original, PerformingAssignmentID: performing, Owner: owner}
}

func AnswerRelativePath(state map[string]any, performing string) (string, error) {
	return FromState(state, "", performing, "").AnswerRelativePath()
}

func (b TargetedAuthoring) AnswerRelativePath() (string, error) {
	source := []string{b.CaseID, b.ContractID, b.ContractSHA256, b.SessionID, b.PlanRef, b.PlanSHA256}
	if b.RuntimeID == "" || b.BaselineGeneration < 1 || b.CaseID == "" || b.SessionID == "" || b.PlanRef == "" || b.PlanSHA256 == "" || b.PerformingAssignmentID == "" {
		return "", fmt.Errorf("Targeted original path source is incomplete")
	}
	digest := workpacket.Digest([]byte(strings.Join(source, "\x00")))
	return filepath.ToSlash(filepath.Join(".loop-submissions/s9", slug(b.RuntimeID), fmt.Sprintf("g%d", b.BaselineGeneration), digest, "targeted-"+slug(b.PerformingAssignmentID)+".json")), nil
}

func slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	dash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
