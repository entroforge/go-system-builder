package policy_test

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
)

func TestLockedArtifactExactPathBlocksEdit(t *testing.T) {
	var input policy.Input
	if err := json.Unmarshal([]byte(`{
		"hook_event_name": "PreToolUse",
		"tool_name": "Edit",
		"tool_input": {
			"file_path": "docs/contracts/BE-039-loop-controller.md"
		},
		"runtime_context": {
			"current_stage": "S6",
			"locked_artifacts": [{
				"id": "BE-039",
				"kind": "contracts",
				"path": "docs/contracts/BE-039-loop-controller.md",
				"version": "v1.0.2",
				"sha256": "fbd5f1df",
				"locked_from_stage": "S6",
				"baseline_generation": 1
			}]
		}
	}`), &input); err != nil {
		t.Fatalf("decode input: %v", err)
	}

	engine, err := policy.Load(filepath.Join("..", "..", "docs", "hook-policy.json"))
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	decision, err := engine.Evaluate(input)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if decision.Decision != "block" || decision.Reason != "locked_artifact_write" {
		t.Fatalf("exact locked path must block with the retained reason: %#v", decision)
	}
}

func TestCandidateArtifactBeforeLockStageAllowsEdit(t *testing.T) {
	engine, err := policy.Load(filepath.Join("..", "..", "docs", "hook-policy.json"))
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	decision, err := engine.Evaluate(policy.Input{
		Event:     "PreToolUse",
		ToolName:  "Edit",
		ToolInput: map[string]any{"file_path": "docs/contracts/versions/REQ-039/g2/BE-039-loop-controller.md"},
		Runtime: policy.RuntimeContext{
			CurrentStage: "S5",
		},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if decision.Decision != "allow" {
		t.Fatalf("candidate before its lock stage must remain editable: %#v", decision)
	}
}

func TestIncompleteManifestIdentityAllowsEdit(t *testing.T) {
	engine, err := policy.Load(filepath.Join("..", "..", "docs", "hook-policy.json"))
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	decision, err := engine.Evaluate(policy.Input{
		Event:     "PreToolUse",
		ToolName:  "Edit",
		ToolInput: map[string]any{"file_path": "docs/contracts/BE-039-loop-controller.md"},
		Runtime: policy.RuntimeContext{
			CurrentStage: "S6",
			LockedArtifacts: []policy.LockedArtifact{{
				ID:                 "BE-039",
				Kind:               "contracts",
				Path:               "docs/contracts/BE-039-loop-controller.md",
				Version:            "v1.0.2",
				LockedFromStage:    "S6",
				BaselineGeneration: 1,
			}},
		},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if decision.Decision != "allow" {
		t.Fatalf("incomplete manifest identity cannot prove a locked write: %#v", decision)
	}
}

func TestProvenBashMutationOfLockedArtifactBlocks(t *testing.T) {
	engine, err := policy.Load(filepath.Join("..", "..", "docs", "hook-policy.json"))
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	const lockedPath = "docs/contracts/BE-039-loop-controller.md"
	decision, err := engine.Evaluate(policy.Input{
		Event:     "PreToolUse",
		ToolName:  "Bash",
		ToolInput: map[string]any{"command": "sed -i 's/old/new/' " + lockedPath},
		Runtime: policy.RuntimeContext{
			CurrentStage: "S6",
			LockedArtifacts: []policy.LockedArtifact{{
				ID:                 "BE-039",
				Kind:               "contracts",
				Path:               lockedPath,
				Version:            "v1.0.2",
				SHA256:             "fbd5f1df",
				LockedFromStage:    "S6",
				BaselineGeneration: 1,
			}},
		},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if decision.Decision != "block" || decision.Reason != "locked_artifact_write" {
		t.Fatalf("proven Bash mutation of a locked path must block: %#v", decision)
	}
}

func TestUnknownBashWithoutProvenLockedMutationAllows(t *testing.T) {
	engine, err := policy.Load(filepath.Join("..", "..", "docs", "hook-policy.json"))
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	decision, err := engine.Evaluate(policy.Input{
		Event:     "PreToolUse",
		AgentID:   "agent-reading",
		ToolName:  "Bash",
		ToolInput: map[string]any{"command": "custom-tool 'unterminated"},
		Runtime: policy.RuntimeContext{
			CurrentStage: "S6",
			LockedArtifacts: []policy.LockedArtifact{{
				ID:                 "BE-039",
				Kind:               "contracts",
				Path:               "docs/contracts/BE-039-loop-controller.md",
				Version:            "v1.0.2",
				SHA256:             "fbd5f1df",
				LockedFromStage:    "S6",
				BaselineGeneration: 1,
			}},
		},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if decision.Decision != "allow" {
		t.Fatalf("unknown Bash without proven retained violation must allow: %#v", decision)
	}
}

func TestGitSquashMergeBlocks(t *testing.T) {
	engine, err := policy.Load(filepath.Join("..", "..", "docs", "hook-policy.json"))
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	decision, err := engine.Evaluate(policy.Input{
		Event:     "PreToolUse",
		ToolName:  "Bash",
		ToolInput: map[string]any{"command": "git merge --squash feature/req-039"},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if decision.Decision != "block" || decision.Reason != "squash_merge" {
		t.Fatalf("git squash merge must block with the retained reason: %#v", decision)
	}
}

func TestGitHubPRSquashMergeBlocks(t *testing.T) {
	engine, err := policy.Load(filepath.Join("..", "..", "docs", "hook-policy.json"))
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	decision, err := engine.Evaluate(policy.Input{
		Event:     "PreToolUse",
		ToolName:  "Bash",
		ToolInput: map[string]any{"command": "gh pr merge 39 --squash"},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if decision.Decision != "block" || decision.Reason != "squash_merge" {
		t.Fatalf("GitHub PR squash merge must block with the retained reason: %#v", decision)
	}
}

func TestLockedArtifactRecoveryUsesNextGenerationPath(t *testing.T) {
	engine, err := policy.Load(filepath.Join("..", "..", "docs", "hook-policy.json"))
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	const lockedPath = "docs/contracts/BE-039-loop-controller.md"
	var input policy.Input
	if err := json.Unmarshal([]byte(`{
		"hook_event_name": "PreToolUse",
		"tool_name": "Write",
		"tool_input": {"file_path": "docs/contracts/BE-039-loop-controller.md"},
		"runtime_context": {
			"bound_req_id": "REQ-039",
			"current_stage": "S6",
			"locked_artifacts": [{
				"id": "BE-039",
				"kind": "contracts",
				"path": "docs/contracts/BE-039-loop-controller.md",
				"version": "v1.0.2",
				"sha256": "fbd5f1df",
				"locked_from_stage": "S6",
				"baseline_generation": 1
			}]
		}
	}`), &input); err != nil {
		t.Fatalf("decode input: %v", err)
	}
	decision, err := engine.Evaluate(input)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	const reworkPath = "docs/contracts/versions/REQ-039/g2/BE-039-loop-controller.md"
	if !slices.Contains(decision.Recovery, reworkPath) {
		t.Fatalf("recovery must include exact next-generation path %q: %#v", reworkPath, decision)
	}
}

func TestLockedArtifactBlockCarriesExactPath(t *testing.T) {
	engine, err := policy.Load(filepath.Join("..", "..", "docs", "hook-policy.json"))
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	const lockedPath = "docs/contracts/BE-039-loop-controller.md"
	decision, err := engine.Evaluate(policy.Input{
		Event:     "PreToolUse",
		ToolName:  "MultiEdit",
		ToolInput: map[string]any{"file_path": lockedPath},
		Runtime: policy.RuntimeContext{
			CurrentStage: "S6",
			LockedArtifacts: []policy.LockedArtifact{{
				ID:                 "BE-039",
				Kind:               "contracts",
				Path:               lockedPath,
				Version:            "v1.0.2",
				SHA256:             "fbd5f1df",
				LockedFromStage:    "S6",
				BaselineGeneration: 1,
			}},
		},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	encoded, err := json.Marshal(decision)
	if err != nil {
		t.Fatalf("encode decision: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("decode decision: %v", err)
	}
	if payload["affected_path"] != lockedPath {
		t.Fatalf("locked block must carry exact affected path %q: %s", lockedPath, encoded)
	}
}

func TestSquashBlockCarriesParsedCommand(t *testing.T) {
	engine, err := policy.Load(filepath.Join("..", "..", "docs", "hook-policy.json"))
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	decision, err := engine.Evaluate(policy.Input{
		Event:     "PreToolUse",
		ToolName:  "Bash",
		ToolInput: map[string]any{"command": "git merge feature/req-039 --squash"},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	encoded, err := json.Marshal(decision)
	if err != nil {
		t.Fatalf("encode decision: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("decode decision: %v", err)
	}
	if payload["parsed_command"] != "git merge --squash" {
		t.Fatalf("squash block must carry canonical parsed command: %s", encoded)
	}
}

func TestLockedArtifactBlockCarriesCurrentStage(t *testing.T) {
	engine, err := policy.Load(filepath.Join("..", "..", "docs", "hook-policy.json"))
	if err != nil {
		t.Fatalf("load policy: %v", err)
	}
	const lockedPath = "docs/contracts/BE-039-loop-controller.md"
	decision, err := engine.Evaluate(policy.Input{
		Event:     "PreToolUse",
		ToolName:  "NotebookEdit",
		ToolInput: map[string]any{"notebook_path": lockedPath},
		Runtime: policy.RuntimeContext{
			CurrentStage: "S6",
			LockedArtifacts: []policy.LockedArtifact{{
				ID:                 "BE-039",
				Kind:               "contracts",
				Path:               lockedPath,
				Version:            "v1.0.2",
				SHA256:             "fbd5f1df",
				LockedFromStage:    "S6",
				BaselineGeneration: 1,
			}},
		},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	encoded, err := json.Marshal(decision)
	if err != nil {
		t.Fatalf("encode decision: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("decode decision: %v", err)
	}
	if payload["stage"] != "S6" {
		t.Fatalf("locked block must carry current stage: %s", encoded)
	}
}

func TestLockedArtifactDirectMutationToolsBlock(t *testing.T) {
	engine := loadPolicyEngine(t)
	const lockedPath = "docs/contracts/BE-039-loop-controller.md"
	artifact := policy.LockedArtifact{
		ID:                 "BE-039",
		Kind:               "contracts",
		Path:               lockedPath,
		Version:            "v1.0.2",
		SHA256:             "fbd5f1df",
		LockedFromStage:    "S6",
		BaselineGeneration: 1,
	}
	cases := map[string]map[string]any{
		"Write":        {"file_path": lockedPath},
		"Edit":         {"file_path": lockedPath},
		"MultiEdit":    {"file_path": lockedPath},
		"NotebookEdit": {"notebook_path": lockedPath},
	}
	for toolName, toolInput := range cases {
		t.Run(toolName, func(t *testing.T) {
			decision, err := engine.Evaluate(policy.Input{
				Event:     "PreToolUse",
				ToolName:  toolName,
				ToolInput: toolInput,
				Runtime: policy.RuntimeContext{
					CurrentStage:    "S6",
					LockedArtifacts: []policy.LockedArtifact{artifact},
				},
			})
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if decision.Decision != "block" || decision.Reason != "locked_artifact_write" {
				t.Fatalf("%s exact locked write must block: %#v", toolName, decision)
			}
		})
	}
}

func TestUnlockedSiblingAndGenerationBehavior(t *testing.T) {
	engine := loadPolicyEngine(t)
	const oldPath = "docs/contracts/BE-039-loop-controller.md"
	const candidatePath = "docs/contracts/versions/REQ-039/g2/BE-039-loop-controller.md"
	oldArtifact := policy.LockedArtifact{
		ID:                 "BE-039",
		Kind:               "contracts",
		Path:               oldPath,
		Version:            "v1.0.2",
		SHA256:             "generation-one-sha",
		LockedFromStage:    "S6",
		BaselineGeneration: 1,
	}
	candidateArtifact := policy.LockedArtifact{
		ID:                 "BE-039",
		Kind:               "contracts",
		Path:               candidatePath,
		Version:            "v2.0.0",
		SHA256:             "generation-two-sha",
		LockedFromStage:    "S6",
		BaselineGeneration: 2,
	}
	cases := []struct {
		name      string
		stage     string
		path      string
		artifacts []policy.LockedArtifact
		want      string
	}{
		{
			name:      "unlocked sibling",
			stage:     "S6",
			path:      "docs/contracts/BE-039-loop-controller-notes.md",
			artifacts: []policy.LockedArtifact{oldArtifact},
			want:      "allow",
		},
		{
			name:      "generation one remains immutable",
			stage:     "S6",
			path:      oldPath,
			artifacts: []policy.LockedArtifact{oldArtifact, candidateArtifact},
			want:      "block",
		},
		{
			name:      "generation two candidate is editable",
			stage:     "S5",
			path:      candidatePath,
			artifacts: []policy.LockedArtifact{oldArtifact},
			want:      "allow",
		},
		{
			name:      "generation two blocks after lock",
			stage:     "S6",
			path:      candidatePath,
			artifacts: []policy.LockedArtifact{oldArtifact, candidateArtifact},
			want:      "block",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision, err := engine.Evaluate(policy.Input{
				Event:     "PreToolUse",
				ToolName:  "Edit",
				ToolInput: map[string]any{"file_path": tc.path},
				Runtime: policy.RuntimeContext{
					CurrentStage:    tc.stage,
					LockedArtifacts: tc.artifacts,
				},
			})
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if decision.Decision != tc.want {
				t.Fatalf("decision = %q, want %q: %#v", decision.Decision, tc.want, decision)
			}
		})
	}
}

func TestEnforceBlockReasonsAreExactlyRetainedReasons(t *testing.T) {
	engine := loadPolicyEngine(t)
	const lockedPath = "docs/contracts/BE-039-loop-controller.md"
	inputs := []policy.Input{
		{
			Event:     "PreToolUse",
			ToolName:  "Edit",
			ToolInput: map[string]any{"file_path": lockedPath},
			Runtime: policy.RuntimeContext{
				CurrentStage: "S6",
				LockedArtifacts: []policy.LockedArtifact{{
					ID:                 "BE-039",
					Kind:               "contracts",
					Path:               lockedPath,
					Version:            "v1.0.2",
					SHA256:             "fbd5f1df",
					LockedFromStage:    "S6",
					BaselineGeneration: 1,
				}},
			},
		},
		{
			Event:     "PreToolUse",
			ToolName:  "Bash",
			ToolInput: map[string]any{"command": "git merge --squash feature/req-039"},
		},
	}
	want := []string{"locked_artifact_write", "squash_merge"}
	for index, input := range inputs {
		decision, err := engine.Evaluate(input)
		if err != nil {
			t.Fatalf("evaluate: %v", err)
		}
		if decision.Decision != "block" || decision.Reason != want[index] {
			t.Fatalf("retained block[%d] = %#v, want reason %q", index, decision, want[index])
		}
	}
}

func TestOldGenerationRemainsImmutableDuringRework(t *testing.T) {
	engine := loadPolicyEngine(t)
	const oldPath = "docs/contracts/BE-039-loop-controller.md"
	decision, err := engine.Evaluate(policy.Input{
		Event:     "PreToolUse",
		ToolName:  "Edit",
		ToolInput: map[string]any{"file_path": oldPath},
		Runtime: policy.RuntimeContext{
			CurrentStage: "S5",
			LockedArtifacts: []policy.LockedArtifact{{
				ID:                 "BE-039",
				Kind:               "contracts",
				Path:               oldPath,
				Version:            "v1.0.2",
				SHA256:             "generation-one-sha",
				LockedFromStage:    "S6",
				BaselineGeneration: 1,
			}},
		},
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if decision.Decision != "block" || decision.Reason != "locked_artifact_write" {
		t.Fatalf("old locked generation must remain immutable during rework: %#v", decision)
	}
}
