package targetedauthoring

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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
	ContractRef            string `json:"contract_ref"`
	OriginalAssignmentID   string `json:"original_assignment_id"`
	PerformingAssignmentID string `json:"performing_assignment_id"`
	Owner                  string `json:"owner"`
	QuestionnaireRoot      string `json:"questionnaire_root,omitempty"`
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
	taskID, err := registeredTaskID(scope, root, agent, expected.SessionID, performing)
	if err != nil {
		return false, err
	}
	matches := 0
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
		if err := json.Unmarshal(declaration, &contract); err != nil {
			return false, fmt.Errorf("Targeted authoring declaration: %w", err)
		}
		contract.QuestionnaireRoot = ""
		expected.QuestionnaireRoot = ""
		if contract != expected || contract.Protocol != SharedTargetedAuthoringProtocol || original == "" || owner == "" {
			return false, fmt.Errorf("Targeted authoring declaration differs from current original responsibility")
		}
		if contract.RuntimeID == "" || contract.BaselineGeneration < 1 || contract.SessionID == "" || contract.PlanRef == "" || contract.ContractRef == "" {
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
	data, err := scope.read(root, ref)
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

// TaskID encodes existing responsibility coordinates; new dispatch normally
// assigns and records a TASK ID directly and consumers read that recorded ID.
func TaskID(sessionID, performing string) string {
	data, _ := json.Marshal([]string{sessionID, performing})
	return "TASK-9" + base64.RawURLEncoding.EncodeToString(data)
}

// registeredTaskID selects the original TASK using the agent's already issued
// responsibilities and the actual session/assignment recorded in each TASK.
var errNoOriginalTask = errors.New("no issued TASK for this responsibility")

func registeredTaskID(scope *readScope, root string, agent map[string]any, session, performing string) (string, error) {
	found := ""
	unreadable := false
	ids := stringSliceFromAny(agent["task_ids"])
	for _, id := range ids {
		row, err := scope.lookup("tasks", id)
		if err != nil {
			return "", err
		}
		if row == nil {
			continue
		}
		fields, err := scope.task(root, row)
		if err != nil {
			unreadable = true
			continue
		}
		var s, a string
		_ = json.Unmarshal(fields["session_id"], &s)
		_ = json.Unmarshal(fields["assignment_id"], &a)
		if s != session || a != performing {
			continue
		}
		if found != "" {
			return "", fmt.Errorf("Targeted original responsibility is ambiguous")
		}
		found = id
	}
	if found == "" {
		if unreadable {
			return "", fmt.Errorf("Targeted original TASK is unavailable")
		}
		return "", errNoOriginalTask
	}
	return found, nil
}

// RegisteredTask reads only TASKs actually issued to the given original owner.
func RegisteredTask(root string, state map[string]any, session, performing, owner string, contexts ...context.Context) (map[string]json.RawMessage, error) {
	scope, err := newReadScope(state, sourceContext(contexts))
	if err != nil {
		return nil, err
	}
	agent, err := scope.lookup("agents", owner)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, nil
	}
	if len(stringSliceFromAny(agent["task_ids"])) == 0 {
		return nil, nil
	}
	id, err := registeredTaskID(scope, root, agent, session, performing)
	if err != nil {
		if errors.Is(err, errNoOriginalTask) {
			return nil, nil
		}
		return nil, err
	}
	row, err := scope.lookup("tasks", id)
	if err != nil {
		return nil, err
	}
	return scope.task(root, row)
}

// RegisteredTaskID finds the task by the original owner's recorded task IDs
// and that task's actual session and assignment, including historical IDs.
func RegisteredTaskID(root string, state map[string]any, session, performing, owner string, contexts ...context.Context) (string, error) {
	scope, err := newReadScope(state, sourceContext(contexts))
	if err != nil {
		return "", err
	}
	agent, err := scope.lookup("agents", owner)
	if err != nil || agent == nil {
		return "", err
	}
	return registeredTaskID(scope, root, agent, session, performing)
}

// TargetedSharedAnswerPath maps the already supplied original Main question
// path into the shared authoring surface without deriving a content identity.
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
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, "targeted-reverification-") {
			parts[i] = strings.Replace(part, "targeted-reverification-", "targeted-", 1)
			break
		}
	}
	return filepath.Join(root, ".loop-submissions/s9", filepath.FromSlash(strings.Join(parts, "/"))), nil
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
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if strings.HasPrefix(path, ".loop-submissions/s9/") {
			wanted[path] = true
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
		encoded, _ := json.Marshal([]string{performing})
		candidateID := base64.RawURLEncoding.EncodeToString(encoded)
		candidate := false
		for path := range wanted {
			joined := strings.ReplaceAll(path, "/", "")
			if strings.Contains(joined, "targeted-"+candidateID) || strings.Contains(joined, "targeted-"+slug(performing)) {
				candidate = true
				break
			}
		}
		if !candidate {
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
				continue
			}
			raw, ok := fields["targeted_authoring"]
			if !ok {
				continue
			}
			var contract TargetedAuthoring
			if err := json.Unmarshal(raw, &contract); err != nil {
				return nil, err
			}
			bare := strings.TrimPrefix(contract.OriginalAssignmentID, "repair-assignment-")
			if contract.Owner != owner || contract.PerformingAssignmentID != performing || performing != "repair-assignment-verification-"+slug(bare) {
				return nil, fmt.Errorf("Targeted original performing identity differs")
			}
			relative, pathErr := contract.AnswerRelativePath()
			if pathErr != nil || !wanted[relative] {
				// Older TASKs may contain the exact originally issued path in their
				// instruction; never reconstruct it from a historical digest.
				var instruction string
				_ = json.Unmarshal(fields["instruction"], &instruction)
				relative = ""
				for path := range wanted {
					if strings.Contains(filepath.ToSlash(instruction), path) {
						if relative != "" {
							return nil, fmt.Errorf("Targeted original paths are ambiguous")
						}
						relative = path
					}
				}
				if relative == "" {
					continue
				}
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
	return TargetedAuthoring{Protocol: SharedTargetedAuthoringProtocol, RuntimeID: stringField(state["runtime_id"]), BaselineGeneration: generation, CaseID: stringField(p["case_id"]), ContractID: stringField(p["contract_id"]), SessionID: stringField(p["session_id"]), PlanRef: stringField(p["plan_ref"]), ContractRef: stringField(p["contract_ref"]), OriginalAssignmentID: original, PerformingAssignmentID: performing, Owner: owner, QuestionnaireRoot: stringField(p["questionnaire_root"])}
}

func AnswerRelativePath(state map[string]any, performing string) (string, error) {
	return FromState(state, "", performing, "").AnswerRelativePath()
}

func (b TargetedAuthoring) AnswerRelativePath() (string, error) {
	if b.QuestionnaireRoot == "" || b.PlanRef == "" || b.PerformingAssignmentID == "" {
		return "", fmt.Errorf("Targeted original questionnaire location is unavailable")
	}
	root := filepath.Clean(filepath.FromSlash(b.QuestionnaireRoot))
	if filepath.IsAbs(root) || root == ".." || strings.HasPrefix(root, ".."+string(filepath.Separator)) || !strings.HasPrefix(filepath.ToSlash(root), ".claude/submissions/s9/") {
		return "", fmt.Errorf("Targeted original questionnaire location is invalid")
	}
	encode := func(v string) string {
		data, _ := json.Marshal([]string{v})
		return base64.RawURLEncoding.EncodeToString(data)
	}
	appendParts := func(parts []string, value string) []string {
		for len(value) > 120 {
			parts = append(parts, value[:120])
			value = value[120:]
		}
		return append(parts, value)
	}
	root = strings.TrimPrefix(filepath.ToSlash(root), ".claude/submissions/s9/")
	parts := []string{".loop-submissions/s9", root, "plan"}
	parts = appendParts(parts, encode(b.PlanRef))
	name := "targeted-reverification-" + encode(b.PerformingAssignmentID) + ".json"
	if len(name) > 200 {
		parts = append(parts, "n")
		for len(name) > 120 {
			parts = append(parts, name[:120])
			name = name[120:]
		}
	}
	parts = append(parts, name)
	for i, part := range parts {
		if strings.HasPrefix(part, "targeted-reverification-") {
			parts[i] = strings.Replace(part, "targeted-reverification-", "targeted-", 1)
			break
		}
	}
	return filepath.ToSlash(filepath.Join(parts...)), nil
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
