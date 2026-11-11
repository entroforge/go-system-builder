package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestFrozenS6TwoBuilderPathlessPostKeepsOriginalOwners(t *testing.T) {
	// Both work packages are registered through the existing Main Writer for
	// the same reviewed TASK. Never seed owners, sessions or accepted reports.
	f := coreBuilderFixtureChecks(t, []string{})
	manifestPath := ".claude/workgroups/REQ-039/core-builder/manifest.json"
	data, err := os.ReadFile(filepath.Join(f.root, manifestPath))
	if err != nil {
		t.Fatal(err)
	}
	var peerManifest map[string]any
	if err := json.Unmarshal([]byte(strings.ReplaceAll(string(data), "core-builder", "peer-builder")), &peerManifest); err != nil {
		t.Fatal(err)
	}
	peerManifest["quantity_rationale"] = "A second independently owned contribution to the reviewed TASK"
	peerPath := ".claude/workgroups/REQ-039/peer-builder/manifest.json"
	stageJSON(t, f.root, peerPath, peerManifest)
	stageCLI(t, "runtime", "register-workgroup", "--root", f.root, "--manifest", peerPath, "--task-id", f.taskID, "--task", f.taskPath)
	first := prepareCoreBuilder(t, f, "backend-core")
	var second workspace.Execution
	prepared := stageCLI(t, "runtime", "workspace", "prepare", "--root", f.root, "--assignment", "assignment-peer-builder", "--agent", "peer-builder", "--check-location", "main", "--context-profile", "backend-core")
	if err := json.Unmarshal([]byte(prepared), &second); err != nil {
		t.Fatal(err)
	}
	for _, execution := range []workspace.Execution{first, second} {
		beginCoreBuilder(t, execution)
	}
	// The tiny process stands in for a platform launch only. The real launcher
	// reserves and records each session against its actual workspace.
	fakeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(fakeDir, "claude"), []byte("#!/bin/sh\nprintf '{}\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, execution := range []workspace.Execution{first, second} {
		stageCLI(t, "runtime", "workspace", "launch", "--root", f.root, "--assignment", execution.AssignmentID, "--agent", execution.AgentID)
	}
	registry, err := workspace.Decode(stageSnapshot(t, f.root).State)
	if err != nil {
		t.Fatal(err)
	}
	first, second = registry.Executions[first.AssignmentID], registry.Executions[second.AssignmentID]
	if first.PlatformSessionID == "" || second.PlatformSessionID == "" || first.PlatformSessionID == second.PlatformSessionID || first.Path == second.Path {
		t.Fatal("original launches did not establish two distinct execution/session relationships")
	}
	type authorFiles struct {
		planPath, resultPath string
		plan, result         []byte
	}
	files := map[string]authorFiles{}
	examples, err := schema.ReadAsset("agent-message.examples.json")
	if err != nil {
		t.Fatal(err)
	}
	for index, execution := range []workspace.Execution{first, second} {
		paths, err := registry.WorkerSubmissionPaths(execution)
		if err != nil {
			t.Fatal(err)
		}
		product := fmt.Sprintf("src/owner_%d.go", index)
		// Produce a real scoped change and commit, with no invented test PASS.
		if err := os.WriteFile(filepath.Join(execution.Path, product), []byte(fmt.Sprintf("package src\nconst Owner%d = %d\n", index, index)), 0600); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"add", product}, {"commit", "-m", "Independent Builder contribution"}} {
			if _, err := workspace.Git(t.Context(), execution.Path, args...); err != nil {
				t.Fatal(err)
			}
		}
		var messages []map[string]any
		if err := json.Unmarshal(examples, &messages); err != nil {
			t.Fatal(err)
		}
		var result map[string]any
		for _, message := range messages {
			if message["message_type"] == "completion_report" {
				result = message
			}
		}
		result["runtime_id"], result["agent_id"], result["agent_definition_ref"] = execution.RuntimeID, execution.AgentID, ".claude/agents/backend-builder.md"
		result["task_id"], result["team_id"] = f.taskID, []string{"workgroup-core-builder", "workgroup-peer-builder"}[index]
		result["message_id"] = "msg-completion-" + execution.AgentID
		result["summary"] = "Private authored Result for " + execution.AgentID
		result["changed_paths"], result["checks"], result["evidence_refs"] = []string{product}, []any{}, []string{product}
		stageJSON(t, execution.Path, paths.Completion, result)
		planPath, resultPath := filepath.Join(execution.Path, paths.Plan), filepath.Join(execution.Path, paths.Completion)
		planBytes, err := os.ReadFile(planPath)
		if err != nil {
			t.Fatal(err)
		}
		resultBytes, err := os.ReadFile(resultPath)
		if err != nil {
			t.Fatal(err)
		}
		files[execution.AgentID] = authorFiles{planPath, resultPath, planBytes, resultBytes}
	}
	invoke := func(actor, session, cwd string, failed bool) string {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{
			"hook_event_name": "PostToolUse", "agent_id": actor, "session_id": session, "cwd": cwd,
			"tool_name": "Bash", "tool_input": map[string]any{"command": "script completed"},
			"tool_response": map[string]any{"is_error": failed},
		})
		var output, diagnostic bytes.Buffer
		if code := cli.Run([]string{"hook", "--root", f.root, "--event", "PostToolUse"}, bytes.NewReader(payload), &output, &diagnostic); code != 0 {
			t.Fatalf("no-path Post: %d %s", code, &diagnostic)
		}
		return output.String() + diagnostic.String()
	}
	ownerRow := func(owner string) []byte {
		t.Helper()
		for _, raw := range stageSnapshot(t, f.root).State["entities"].(map[string]any)["agents"].([]any) {
			if row := raw.(map[string]any); row["id"] == owner {
				data, _ := json.Marshal(row)
				return data
			}
		}
		t.Fatalf("original registered owner %s is absent", owner)
		return nil
	}
	assertFiles := func(execution workspace.Execution) {
		t.Helper()
		original := files[execution.AgentID]
		for _, file := range []struct {
			path string
			data []byte
		}{{original.planPath, original.plan}, {original.resultPath, original.result}} {
			actual, err := os.ReadFile(file.path)
			if err != nil || !bytes.Equal(actual, file.data) {
				t.Fatalf("event rewrote %s's original author file %s: %v", execution.AgentID, file.path, err)
			}
		}
	}
	for _, request := range []struct {
		name, actor, session, cwd string
		failed                    bool
	}{
		{"Main", "Main", "actual-main", f.root, false},
		{"unknown", "unknown-builder", "unknown-session", f.root, false},
		{"failed A", first.AgentID, first.PlatformSessionID, first.Path, true},
		{"failed B", second.AgentID, second.PlatformSessionID, second.Path, true},
	} {
		beforeA, beforeB := ownerRow(first.AgentID), ownerRow(second.AgentID)
		out := invoke(request.actor, request.session, request.cwd, request.failed)
		if strings.Contains(out, "S6 Completion questionnaire saved") || !bytes.Equal(beforeA, ownerRow(first.AgentID)) || !bytes.Equal(beforeB, ownerRow(second.AgentID)) {
			t.Fatalf("%s received an existing Builder's answer: %s", request.name, out)
		}
		assertFiles(first)
		assertFiles(second)
	}
	for index, execution := range []workspace.Execution{first, second} {
		peer := []workspace.Execution{second, first}[index]
		beforePeer := ownerRow(peer.AgentID)
		peerExecution := registry.Executions[peer.AssignmentID]
		out := invoke(execution.AgentID, execution.PlatformSessionID, execution.Path, false)
		if !strings.Contains(out, "S6 Completion questionnaire saved") || !strings.Contains(out, files[execution.AgentID].resultPath) || strings.Contains(out, files[peer.AgentID].resultPath) || strings.Contains(out, files[peer.AgentID].planPath) || strings.Contains(out, "Private authored Result for "+peer.AgentID) {
			t.Fatalf("owner %s lost its own Result or received its peer's material: %s", execution.AgentID, out)
		}
		if !bytes.Equal(beforePeer, ownerRow(peer.AgentID)) {
			t.Fatalf("owner %s's Post changed peer %s", execution.AgentID, peer.AgentID)
		}
		currentRegistry, err := workspace.Decode(stageSnapshot(t, f.root).State)
		if err != nil {
			t.Fatal(err)
		}
		before, _ := json.Marshal(peerExecution)
		after, _ := json.Marshal(currentRegistry.Executions[peer.AssignmentID])
		if !bytes.Equal(before, after) {
			t.Fatal("owner Post changed the peer's registered execution")
		}
		var currentOwner map[string]any
		if err := json.Unmarshal(ownerRow(execution.AgentID), &currentOwner); err != nil {
			t.Fatal(err)
		}
		if ref, _ := currentOwner["completion_reported_ref"].(string); ref == "" {
			t.Fatalf("owner %s's successful no-path Post did not record its original Result: %s", execution.AgentID, out)
		}
		assertFiles(first)
		assertFiles(second)
		registry = currentRegistry
	}
}
