package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/assignment"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/team"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func s5ManifestQuestionnairePath(root string, state map[string]any) string {
	life := mapFieldCLI(state, "lifecycle")
	if life["state"] != "document_verification" || !runtime.PacketEnabled(state) {
		return ""
	}
	req := stringValue(mapFieldCLI(state, "bound_req")["id"])
	if req == "" {
		return ""
	}
	return filepath.Join(root, ".claude/workgroups", req, fmt.Sprintf("review-e%d", stageAnswerEntry(life["phase_revision"])), "manifest.json")
}

// The original manifest is both the prepared Main interface and registration
// input. There is no second form, registry or delegated execution here.
func s5ManifestProjection(ctx context.Context, root string, state map[string]any) (map[string]any, string, string, error) {
	service, err := submission.NewContext(ctx, root, false)
	if err != nil {
		return nil, "", "", err
	}
	sources, err := runtime.S5FormalInputs(root, state, service.Files)
	if err != nil {
		return nil, "", "", err
	}
	paths := []string{}
	for _, ref := range sources.References {
		data, readErr := service.Files.ReadFile(filepath.Join(root, ref.Path))
		if readErr != nil {
			return nil, "", "", fmt.Errorf("restore accepted S5 source %s: %w", ref.Path, readErr)
		}
		if workpacket.Digest(data) != ref.SHA256 {
			return nil, "", "", fmt.Errorf("accepted S5 source differs: %s; restore its accepted bytes, do not refresh SHA", ref.Path)
		}
		live, readErr := workpacket.ReadFile(root, ref.Path, 20<<20)
		if readErr != nil {
			return nil, "", "", fmt.Errorf("restore accepted S5 source %s: %w", ref.Path, readErr)
		}
		if workpacket.Digest(live) != ref.SHA256 {
			return nil, "", "", fmt.Errorf("accepted S5 source differs: %s; restore its accepted bytes, do not refresh SHA", ref.Path)
		}
		paths = append(paths, ref.Path)
	}
	sort.Strings(paths)
	req := stringValue(mapFieldCLI(state, "bound_req")["id"])
	generation := stageAnswerEntry(mapFieldCLI(state, "baseline")["generation"])
	entry := stageAnswerEntry(mapFieldCLI(state, "lifecycle")["phase_revision"])
	// The existing S4 plan owns TASK order and identity. Legacy packet fixtures
	// without a waves-v1 plan retain their uniquely registered TASK anchor.
	plan, err := semantic.LoadDispatchPlan(root, service.Files, req)
	if err != nil {
		return nil, "", "", fmt.Errorf("restore accepted S4 dispatch plan: %w", err)
	}
	taskID, taskPath := "", ""
	if plan != nil {
		if len(plan.Problems) > 0 {
			return nil, "", "", fmt.Errorf("accepted S4 dispatch plan: %s", strings.Join(plan.Problems, "; "))
		}
		if len(plan.Tasks) > 0 {
			taskID, taskPath = plan.Tasks[0].ID, plan.Tasks[0].Path
		}
	}
	docs := []any{}
	for _, raw := range anySlice(state["documents"]) {
		d, _ := raw.(map[string]any)
		if stageAnswerEntry(d["generation"]) != generation || (d["status"] != "registered" && d["status"] != "locked") {
			continue
		}
		docs = append(docs, map[string]any{"id": d["id"], "path": d["path"], "version": d["version"], "sha256": d["sha256"]})
		if taskID == "" && d["kind"] == "task" {
			taskID, taskPath = stringValue(d["id"]), stringValue(d["path"])
		}
	}
	if taskID == "" || taskPath == "" {
		return nil, "", "", fmt.Errorf("S5 preparation requires the original S4 TASK anchor; Main restores the accepted plan, not internal metadata")
	}
	suffix := fmt.Sprintf("%s-g%d-dv-e%d", req, generation, entry)
	dispositions, assignments := []any{}, []any{}
	ids := []string{}
	for i, responsibility := range []string{"DV-SPEC-CONSISTENCY", "DV-TASK-EXECUTABILITY"} {
		dimension := []string{"spec", "task"}[i]
		id := "assignment-" + suffix + "-" + dimension
		ids = append(ids, id)
		output := fmt.Sprintf("docs/reports/review/%s/%s.json", suffix, dimension)
		scope := []string{"当前完整规格链的一致性、完整性与需求忠实度"}
		if i == 1 {
			scope = []string{"当前全部任务及整体计划的可执行性、依赖、自检与收尾"}
		}
		assignments = append(assignments, map[string]any{"assignment_id": id, "responsibility_id": responsibility, "role_family": "document-verifier", "scope": scope, "agent_id": "reviewer-" + suffix + "-" + dimension, "agent_definition_ref": ".claude/agents/document-verifier.md", "skill_refs": agentcontext.RequiredSkills(responsibility), "read_paths": paths, "write_paths": []string{}, "output_paths": []string{output}, "depends_on": []string{}, "reuse_decision": "create", "grouping_rationale": scope[0] + "；与另一审查维度由独立原生上下文分别判断。", "status": "planned", "required_checks": []string{}, "done_when": []string{"读取系统提供的完整准确材料，在本人原答卷填写真实 conclusion/findings 并原生返回"}})
		dispositions = append(dispositions, map[string]any{"responsibility_id": responsibility, "disposition": "assigned", "trigger": "S5 必需独立审查维度", "assignment_ids": []string{id}, "na_rationale": nil, "evidence_ref": nil})
	}
	value := map[string]any{"schema_version": "1.0.0", "manifest_id": "team-manifest-" + suffix, "version": "v1.0.0", "runtime_id": state["runtime_id"], "req_id": req, "baseline_generation": generation, "review_round": nil, "platform_team_id": "logical-" + suffix, "workgroup_id": "workgroup-" + suffix, "workgroup_kind": "document_verifier", "status": "planned", "documents": docs, "risk_tags": []any{}, "responsibility_dispositions": dispositions, "assignments": assignments, "separation_edges": []any{map[string]any{"left_assignment_id": ids[0], "right_assignment_id": ids[1], "reason": "independence"}}, "planned_agent_count": 2, "max_parallel_agents": 2, "quantity_rationale": "两项必需专业维度分别由两个独立原生上下文判断；owner 仅为工程标签。", "validation": map[string]any{"result": "pass", "missing_responsibilities": []string{}, "unresolved_conflicts": []string{}, "warnings": []string{}, "validated_at": time.Now().UTC().Format(time.RFC3339Nano)}}
	// Normalize JSON numbers/arrays for comparisons with the actual author file.
	data, _ := json.Marshal(value)
	json.Unmarshal(data, &value)
	return value, taskID, taskPath, nil
}

func prepareS5ManifestQuestionnaire(ctx context.Context, root string, state map[string]any) error {
	path := s5ManifestQuestionnairePath(root, state)
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	draft, _, _, err := s5ManifestProjection(ctx, root, state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if os.IsExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(data, '\n'))
	return err
}

func s5RegisteredManifest(state map[string]any, path, root string) bool {
	rel, _ := filepath.Rel(root, path)
	for _, raw := range anySlice(mapFieldCLI(state, "entities")["teams"]) {
		row, _ := raw.(map[string]any)
		if row["kind"] == "document_verifier" && row["manifest_ref"] == filepath.ToSlash(rel) {
			return true
		}
	}
	return false
}

// Fill absent machine members while preserving an explicit conflicting
// association as an error. This is only S5's original DTO adaptation.
func completeS5MachineValue(got, want any) (any, error) {
	if got == nil && want != nil {
		return want, nil
	}
	switch expected := want.(type) {
	case map[string]any:
		actual, ok := got.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("expected prepared object")
		}
		for key, value := range expected {
			prior, present := actual[key]
			if !present {
				actual[key] = value
				continue
			}
			completed, err := completeS5MachineValue(prior, value)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			actual[key] = completed
		}
		return actual, nil
	case []any:
		actual, ok := got.([]any)
		if !ok || len(actual) != len(expected) {
			return nil, fmt.Errorf("prepared set differs")
		}
		for i, value := range expected {
			completed, err := completeS5MachineValue(actual[i], value)
			if err != nil {
				return nil, fmt.Errorf("%d: %w", i, err)
			}
			actual[i] = completed
		}
		return actual, nil
	default:
		if !reflect.DeepEqual(got, want) {
			return nil, fmt.Errorf("differs from the prepared original fact")
		}
		return want, nil
	}
}

// Missing system fields are filled from original facts; explicit conflicting
// source/identity fields are rejected, rather than silently rebound.
func completeS5Manifest(input, expected map[string]any) error {
	for key, want := range expected {
		switch key {
		case "risk_tags", "quantity_rationale", "max_parallel_agents", "validation", "assignments":
			continue
		}
		if got, present := input[key]; present {
			completed, err := completeS5MachineValue(got, want)
			if err != nil {
				return fmt.Errorf("S5 /%s: %w; restore the original association", key, err)
			}
			input[key] = completed
		} else {
			input[key] = want
		}
	}
	if _, present := input["risk_tags"]; !present {
		input["risk_tags"] = expected["risk_tags"]
	}
	if _, present := input["quantity_rationale"]; !present {
		input["quantity_rationale"] = expected["quantity_rationale"]
	}
	if _, present := input["max_parallel_agents"]; !present {
		input["max_parallel_agents"] = expected["max_parallel_agents"]
	}
	actual, ok := input["assignments"].([]any)
	if !ok {
		return fmt.Errorf("S5 /assignments: retain the two actual independent responsibility choices")
	}
	defaults := expected["assignments"].([]any)
	if len(actual) != len(defaults) {
		return fmt.Errorf("S5 /assignments: both mandatory independent review dimensions are required")
	}
	for i, raw := range actual {
		row, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("S5 /assignments/%d must be an object", i)
		}
		base := defaults[i].(map[string]any)
		for key, want := range base {
			switch key {
			case "scope", "grouping_rationale", "agent_id", "skill_refs":
				if _, ok := row[key]; !ok {
					row[key] = want
				}
				continue
			}
			if got, present := row[key]; present {
				completed, err := completeS5MachineValue(got, want)
				if err != nil {
					return fmt.Errorf("S5 /assignments/%d/%s: %w", i, key, err)
				}
				row[key] = completed
			} else {
				row[key] = want
			}
		}
	}
	// This assertion is produced by actual schema/semantic validation below.
	input["validation"] = expected["validation"]
	return nil
}

// prepareS5ReviewResponsibilities performs only known engineering preparation:
// it neither starts a platform Agent nor records a professional review verdict.
func prepareS5ReviewResponsibilities(ctx context.Context, root string, state map[string]any) (err error) {
	defer func() {
		message := ""
		if err != nil {
			message = err.Error()
		}
		submission.RecordPlanningDiagnostic(root, state, "preparation", message)
	}()
	return prepareS5ReviewResponsibilitiesOriginal(ctx, root, state)
}

func prepareS5ReviewResponsibilitiesOriginal(ctx context.Context, root string, state map[string]any) error {
	if s5ManifestQuestionnairePath(root, state) == "" {
		return nil
	}
	current, err := observedS5Packets(state)
	if err != nil {
		return err
	}
	if len(current) > 0 {
		service, err := submission.NewContext(ctx, root, false)
		if err != nil {
			return err
		}
		_, err = service.IssueContext(ctx, "")
		return err
	}
	if err := prepareS5ManifestQuestionnaire(ctx, root, state); err != nil {
		return err
	}
	service, err := submission.NewContext(ctx, root, false)
	if err != nil {
		return err
	}
	snapshot, err := runtime.NewStore(service.StatePath, service.JournalPath).Snapshot()
	if err != nil {
		return err
	}
	_, err = registerPreparedS5Manifest(ctx, root, snapshot)
	return err
}

func registerPreparedS5Manifest(ctx context.Context, root string, snapshot runtime.Snapshot) ([]workpacket.Packet, error) {
	service, err := submission.NewContext(ctx, root, false)
	if err != nil {
		return nil, err
	}
	path := s5ManifestQuestionnairePath(root, snapshot.State)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !s5RegisteredManifest(snapshot.State, path, root) {
		var value map[string]any
		if err := json.Unmarshal(data, &value); err != nil {
			return nil, err
		}
		if value == nil {
			return nil, fmt.Errorf("S5 manifest must be an object")
		}
		expected, taskID, taskPath, err := s5ManifestProjection(ctx, root, snapshot.State)
		if err != nil {
			return nil, err
		}
		if err := completeS5Manifest(value, expected); err != nil {
			return nil, err
		}
		data, err = json.MarshalIndent(value, "", "  ")
		if err != nil {
			return nil, err
		}
		if err := schema.NewValidator(root).ValidateBytes("team-manifest.schema.json", data); err != nil {
			return nil, err
		}
		if err := team.ValidateBytes(data); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, append(data, '\n'), 0600); err != nil {
			return nil, err
		}
		_, err = assignment.Register(root, service.StatePath, service.JournalPath, assignment.Request{Context: ctx, ExpectedRevision: snapshot.Revision, ManifestPath: path, TaskID: taskID, TaskPath: filepath.Join(root, taskPath)})
		if err != nil {
			return nil, err
		}
	}
	// The original issuer checks the registration digest even before its
	// first packet, and existing packet pins afterwards. Do not normalize or
	// re-sign an already registered manifest here.
	return service.IssueContext(ctx, "")
}

func runSavedS5ManifestQuestionnaire(root string, request policy.Input, stdout, stderr io.Writer) (bool, int) {
	if !mainQuestionnaireViewer(request) {
		return false, 0
	}
	path, saved := savedQuestionnairePath(request)
	if !saved {
		return false, 0
	}
	snapshot, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		return false, 0
	}
	current := s5ManifestQuestionnairePath(root, snapshot.State)
	if current == "" || !samePath(path, current) {
		return false, 0
	}
	packets, err := registerPreparedS5Manifest(contextForHook(request), root, snapshot)
	if err != nil {
		fmt.Fprintln(stderr, "S5 Main manifest:", err)
		nextAction := "Main 只修正尚未登记的真实专业选择；机器字段由系统准备，原来源缺失或漂移由系统恢复，不填写或刷新 SHA。"
		if s5RegisteredManifest(snapshot.State, path, root) {
			nextAction = "Main 恢复原登记 manifest 字节并沿原发行接续；已登记分工不能原位编辑或重新签源。原字节不可恢复时明确报告来源缺口；正式基线确需改变时，仅由真实审查发现沿原 TR-004 返规划、新 S5 入口重建，不伪造发现。"
		}
		return true, renderStageAnswerHook(stdout, path, submission.Receipt{ResponsibilityStatus: "not_accepted", CurrentStage: "S5", Error: err.Error(), NextAction: nextAction}, "S5")
	}
	var next strings.Builder
	for _, p := range packets {
		fmt.Fprintf(&next, "%s/%s: view %s; private answer %s. Main carries these exact references into a separate native Agent; reviewer fills conclusion/findings and returns the answer reference.\n", p.Binding.Owner, p.Binding.Responsibility, filepath.Join(root, p.ViewPath), filepath.Join(root, p.AnswerPath))
	}
	// Registration is not a professional packet answer. Report its existing
	// facts and exact issued references, without forging packet_id or claiming
	// that either review has completed.
	context := "S5 原分工登记与原问卷发行已就绪；原清单保持冻结，这不代表审查通过。Main 直接分别派发真实独立原生上下文。\n" + strings.TrimSpace(next.String())
	_ = json.NewEncoder(stdout).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PostToolUse", "additionalContext": context}})
	return true, 0
}
