package repair

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/checkexec"
	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// These tests exercise only the current registered attempt association and typed
// target matcher. They are mechanical helper coverage, not evidence of the
// original S9 writer or Hook chain.
func TestRepairResultAttemptStillCurrentProjection(t *testing.T) {
	root := t.TempDir()
	base := repairResultAttemptTestExecution(root)
	provenance, candidateRef, candidateSHA := repairResultAttemptTestProvenance(t, base)

	cases := []struct {
		name   string
		mutate func(*workspace.Execution)
		want   bool
	}{
		{name: "ready current attempt", want: true},
		{name: "complete current attempt", mutate: func(e *workspace.Execution) { e.Status = "complete" }, want: true},
		{name: "rework attempt changed", mutate: func(e *workspace.Execution) { e.ReworkRef = "runtime/evidence/rework/next.json" }, want: false},
		{name: "delivery path changed", mutate: func(e *workspace.Execution) { e.DeliveryRef = filepath.Join(root, "candidate-next.json") }, want: false},
		{name: "delivery SHA changed", mutate: func(e *workspace.Execution) { e.DeliverySHA256 = strings.Repeat("b", 64) }, want: false},
		{name: "profile changed", mutate: func(e *workspace.Execution) { e.CheckProfiles[0].Command = "go test ./other" }, want: true},
		{name: "scope changed", mutate: func(e *workspace.Execution) { e.WritePaths = []string{"internal/other"} }, want: true},
		{name: "base changed", mutate: func(e *workspace.Execution) { e.BaseCommit = strings.Repeat("b", 40) }, want: true},
		{name: "platform session changed", mutate: func(e *workspace.Execution) { e.PlatformSessionID = "session-next" }, want: true},
		{name: "bootstrap changed", mutate: func(e *workspace.Execution) { e.BootstrapSHA256 = strings.Repeat("b", 64) }, want: true},
		{name: "execution left current state", mutate: func(e *workspace.Execution) { e.Status = "rework" }, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			execution := base
			execution.CheckProfiles = append([]checkexec.Profile(nil), base.CheckProfiles...)
			execution.WritePaths = append([]string(nil), base.WritePaths...)
			execution.Checks = append([]string(nil), base.Checks...)
			execution.Inputs = map[string]string{}
			for key, value := range base.Inputs {
				execution.Inputs[key] = value
			}
			if tc.mutate != nil {
				tc.mutate(&execution)
			}
			state := repairResultAttemptTestState(t, root, execution)
			got := repairResultAttemptStillCurrent(state, provenance, candidateRef, candidateSHA)
			if got != tc.want {
				t.Fatalf("repairResultAttemptStillCurrent() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRepairResultTypedLegacyTargetGuardProjection(t *testing.T) {
	plan := RepairPlan{PlanID: "plan-current"}
	planRef := ArtifactRef{Path: "runtime/evidence/repair/plan.json", SHA256: strings.Repeat("a", 64)}
	canonicalID := "repair-assignment-unit-api"
	manifestID := manifestAssignmentAlias(canonicalID)
	base := workpacket.RepairResultPacket{Binding: workpacket.RepairResultBinding{Sources: workpacket.RepairPlanReportBinding{
		RuntimeID: "loop-s9-attempt", BaselineGeneration: 2, ReviewRound: 4,
		SessionID: "session-current", PlanID: plan.PlanID, PlanRef: planRef.Path, PlanSHA256: planRef.SHA256,
		RepairAssignmentID: canonicalID, ManifestAssignmentID: manifestID,
	}}}

	cases := []struct {
		name       string
		packet     workpacket.RepairResultPacket
		assignment string
		runtimeID  string
		generation int
		round      int
		sessionID  string
		plan       RepairPlan
		planRef    ArtifactRef
		hasPacket  bool
		wantBlock  bool
	}{
		{name: "canonical Assignment packet blocks legacy", packet: base, assignment: canonicalID, runtimeID: "loop-s9-attempt", generation: 2, round: 4, sessionID: "session-current", plan: plan, planRef: planRef, hasPacket: true, wantBlock: true},
		{name: "same Assignment manifest spelling blocks legacy", packet: base, assignment: manifestID, runtimeID: "loop-s9-attempt", generation: 2, round: 4, sessionID: "session-current", plan: plan, planRef: planRef, hasPacket: true, wantBlock: true},
		{name: "different session leaves legacy available", packet: base, assignment: canonicalID, runtimeID: "loop-s9-attempt", generation: 2, round: 4, sessionID: "session-other", plan: plan, planRef: planRef, hasPacket: true},
		{name: "different plan leaves legacy available", packet: base, assignment: canonicalID, runtimeID: "loop-s9-attempt", generation: 2, round: 4, sessionID: "session-current", plan: RepairPlan{PlanID: "plan-other"}, planRef: planRef, hasPacket: true},
		{name: "different plan artifact leaves legacy available", packet: base, assignment: canonicalID, runtimeID: "loop-s9-attempt", generation: 2, round: 4, sessionID: "session-current", plan: plan, planRef: ArtifactRef{Path: planRef.Path, SHA256: strings.Repeat("b", 64)}, hasPacket: true},
		{name: "different generation leaves legacy available", packet: base, assignment: canonicalID, runtimeID: "loop-s9-attempt", generation: 1, round: 4, sessionID: "session-current", plan: plan, planRef: planRef, hasPacket: true},
		{name: "genuinely unissued target leaves legacy available", assignment: canonicalID, runtimeID: "loop-s9-attempt", generation: 2, round: 4, sessionID: "session-current", plan: plan, planRef: planRef},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := false
			if tc.hasPacket {
				got = repairResultPacketMatchesLegacyTarget(tc.packet, tc.runtimeID, tc.generation, tc.round, tc.sessionID, tc.plan, tc.planRef, tc.assignment)
			}
			if got != tc.wantBlock {
				t.Fatalf("repairResultPacketMatchesLegacyTarget() = %v, want %v", got, tc.wantBlock)
			}
		})
	}
}

func TestRepairResultExecutionSelectorOnlySkipsTrulyUnregisteredTargets(t *testing.T) {
	root := t.TempDir()
	binding := workpacket.RepairPlanReportBinding{
		RuntimeID: "loop-s9-selector", BaselineGeneration: 2,
		RepairAssignmentID: "repair-assignment-unit-api", ManifestAssignmentID: "assignment-unit-api",
		OwnerAgentID: "agent-builder-s9",
	}
	stateFor := func(executions map[string]workspace.Execution) map[string]any {
		state := map[string]any{
			"runtime_id": binding.RuntimeID,
			"baseline":   map[string]any{"generation": binding.BaselineGeneration},
			"bound_req": map[string]any{"workspace": map[string]any{
				"project_root": root, "dev_branch": "main", "release_upstream": "origin/main", "bound_commit": strings.Repeat("f", 40),
			}},
		}
		if executions != nil {
			state["workspace"] = workspace.Encode(&workspace.ExecutionRegistry{
				Version: 1, MainRoot: root, CommonDir: filepath.Join(root, ".git"), Branch: "main",
				Executions: executions,
			})
		}
		return state
	}

	t.Run("no registered target is skippable", func(t *testing.T) {
		_, err := repairResultExecutionFromState(t.Context(), stateFor(map[string]workspace.Execution{}), binding, root)
		if !errors.Is(err, errRepairResultExecutionNotRegistered) {
			t.Fatalf("missing target error = %v, want the unregistered-target sentinel", err)
		}
	})
	t.Run("registered identity with wrong owner is not skippable", func(t *testing.T) {
		e := workspace.Execution{
			AssignmentID: binding.RepairAssignmentID, RuntimeID: binding.RuntimeID,
			BaselineGeneration: binding.BaselineGeneration, AgentID: "agent-someone-else", TargetBranch: "main",
		}
		_, err := repairResultExecutionFromState(t.Context(), stateFor(map[string]workspace.Execution{e.AssignmentID: e}), binding, root)
		if err == nil || errors.Is(err, errRepairResultExecutionNotRegistered) {
			t.Fatalf("mismatched registered identity error = %v, want a hard identity mismatch", err)
		}
	})
	t.Run("registered identity with wrong map key is not skippable", func(t *testing.T) {
		e := workspace.Execution{
			AssignmentID: binding.RepairAssignmentID, RuntimeID: binding.RuntimeID,
			BaselineGeneration: binding.BaselineGeneration, AgentID: binding.OwnerAgentID, TargetBranch: "main",
		}
		_, err := repairResultExecutionFromState(t.Context(), stateFor(map[string]workspace.Execution{"wrong-key": e}), binding, root)
		if err == nil || errors.Is(err, errRepairResultExecutionNotRegistered) {
			t.Fatalf("mismatched registered key error = %v, want a hard identity mismatch", err)
		}
	})
}

func repairResultAttemptTestExecution(root string) workspace.Execution {
	return workspace.Execution{
		AssignmentID: "repair-assignment-unit-api", RuntimeID: "loop-s9-attempt", BaselineGeneration: 2,
		Generation: 3, AgentID: "agent-builder-s9", Path: filepath.Join(root, ".worktrees", "builder"),
		Branch: "worker/s9-unit-api", TargetBranch: "main", BaseCommit: strings.Repeat("a", 40),
		Status: "ready", WritePaths: []string{"internal/repair"}, Checks: []string{"go test ./internal/repair"},
		Inputs: map[string]string{"docs/dev/tasks/s9.md": strings.Repeat("c", 64)}, BootstrapSHA256: strings.Repeat("d", 64),
		DeliveryRef:    filepath.Join(root, ".claude/review/repair/candidates/"+strings.Repeat("e", 64)+".json"),
		DeliverySHA256: strings.Repeat("e", 64), PlatformSessionID: "session-s9-current", CheckLocation: "worker",
		CheckProfiles: []checkexec.Profile{{ID: "s9-main-check", Version: "1", CheckIndex: 0, Command: "go test ./internal/repair", Location: "worker", TimeoutSeconds: 30, EnvironmentPolicy: "declared_only", Environment: []checkexec.EnvFact{}, Dependencies: []checkexec.FileRef{}, Fixtures: []checkexec.FileRef{}, InputClosure: "complete", SideEffects: "none", ReusePolicy: "never"}},
	}
}

func repairResultAttemptTestState(t *testing.T, root string, execution workspace.Execution) map[string]any {
	t.Helper()
	registry := &workspace.ExecutionRegistry{
		Version: 1, MainRoot: root, CommonDir: filepath.Join(root, ".git"), Branch: "main", BoundHead: strings.Repeat("f", 40),
		Executions: map[string]workspace.Execution{execution.AssignmentID: execution},
	}
	state := map[string]any{
		"runtime_id": execution.RuntimeID,
		"baseline":   map[string]any{"generation": execution.BaselineGeneration},
		"workspace":  workspace.Encode(registry),
		"bound_req": map[string]any{"workspace": map[string]any{
			"project_root": root, "dev_branch": "main", "release_upstream": "origin/main", "bound_commit": strings.Repeat("f", 40),
		}},
	}
	return state
}

func repairResultAttemptTestProvenance(t *testing.T, execution workspace.Execution) (runtimepkg.RepairResultCandidateProvenance, string, string) {
	t.Helper()
	encoded, err := json.Marshal(execution)
	if err != nil {
		t.Fatal(err)
	}
	var definition map[string]any
	if err := json.Unmarshal(encoded, &definition); err != nil {
		t.Fatal(err)
	}
	projection, err := runtimepkg.ProjectRepairResultExecution(definition)
	if err != nil {
		t.Fatal(err)
	}
	candidateSHA := execution.DeliverySHA256
	candidateRef := execution.DeliveryRef
	return runtimepkg.RepairResultCandidateProvenance{
		OwnerAgentID: execution.AgentID, RuntimeID: execution.RuntimeID, BaselineGeneration: execution.BaselineGeneration,
		ExecutionAssignmentID: execution.AssignmentID, ExecutionGeneration: execution.Generation,
		ExecutionSHA256: workpacket.Digest(projection), ExecutionRoot: execution.Path, ReworkRef: execution.ReworkRef,
	}, candidateRef, candidateSHA
}
