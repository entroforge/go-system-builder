package hookctx_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/hookctx"
)

func TestLoadFullPrefersRegisteredAgentAndTeamManifestRefs(t *testing.T) {
	root := t.TempDir()
	taskID := "TASK-MANIFEST-REF"
	agentID := "builder-manifest-ref"
	assignmentID := "assignment-manifest-ref"
	agentManifest := filepath.ToSlash(filepath.Join(".claude", "registered", "agent-manifest.json"))
	teamManifest := filepath.ToSlash(filepath.Join(".claude", "registered", "team-manifest.json"))
	canonicalManifest := filepath.ToSlash(filepath.Join(".claude", "workgroups", "REQ-039", taskID, "manifest.json"))

	writeManifest := func(path, scope string) {
		t.Helper()
		writeJSONL(t, filepath.Join(root, filepath.FromSlash(path)), `{"assignments":[{"assignment_id":"`+assignmentID+`","responsibility_id":"BUILD-WORK-PACKAGE","role_family":"backend-builder","agent_id":"`+agentID+`","write_paths":["`+scope+`"],"status":"active"}]}`)
	}
	writeManifest(agentManifest, "internal/from-agent-ref/")
	writeManifest(teamManifest, "internal/from-team-ref/")
	writeManifest(canonicalManifest, "internal/from-canonical-fallback/")

	state := map[string]any{
		"runtime_id": "loop-REQ-039", "revision": 1,
		"baseline": map[string]any{"generation": 1},
		"entities": map[string]any{
			"agents": []any{map[string]any{
				"id": agentID, "state": "working", "task_ids": []any{taskID},
				"team_id": "team-manifest-ref", "prompt_ref": agentManifest + "#" + assignmentID,
			}},
			"tasks": []any{map[string]any{
				"id": taskID, "state": "in_progress", "owner_agent_ids": []any{agentID},
			}},
			"teams": []any{map[string]any{
				"id": "team-manifest-ref", "manifest_ref": teamManifest,
			}},
		},
	}
	stateBytes, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	writeJSONL(t, filepath.Join(root, ".claude", "loop-state.json"), string(stateBytes))

	loaded, err := hookctx.LoadFull(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Assignments) != 1 {
		t.Fatalf("expected one assignment, got %d (%+v)", len(loaded.Assignments), loaded.Assignments)
	}
	row := loaded.Assignments[0]
	if row.ManifestRef != agentManifest {
		t.Fatalf("Agent prompt manifest must win over Team/canonical paths: got %q", row.ManifestRef)
	}
	if len(row.WritePaths) != 1 || row.WritePaths[0] != "internal/from-agent-ref/" {
		t.Fatalf("Agent prompt manifest scope leaked or was ignored: %+v", row.WritePaths)
	}
}

func TestLoadFullUsesRegisteredTeamManifestWhenPromptIsLegacyLabel(t *testing.T) {
	root := t.TempDir()
	taskID := "TASK-TEAM-REF"
	agentID := "builder-team-ref"
	assignmentID := "assignment-team-ref"
	teamManifest := filepath.ToSlash(filepath.Join("evidence", "registered-team.json"))
	canonicalManifest := filepath.ToSlash(filepath.Join(".claude", "workgroups", "REQ-039", taskID, "manifest.json"))
	manifest := func(scope string) string {
		return `{"assignments":[{"assignment_id":"` + assignmentID + `","responsibility_id":"BUILD-WORK-PACKAGE","role_family":"backend-builder","agent_id":"` + agentID + `","write_paths":["` + scope + `"],"status":"active"}]}`
	}
	writeJSONL(t, filepath.Join(root, filepath.FromSlash(teamManifest)), manifest("internal/from-registered-team/"))
	writeJSONL(t, filepath.Join(root, filepath.FromSlash(canonicalManifest)), manifest("internal/from-canonical-fallback/"))
	state := map[string]any{
		"runtime_id": "loop-REQ-039", "revision": 1,
		"baseline": map[string]any{"generation": 1},
		"entities": map[string]any{
			"agents": []any{map[string]any{
				"id": agentID, "state": "working", "task_ids": []any{taskID},
				"team_id": "team-ref", "prompt_ref": "manifest#" + assignmentID,
			}},
			"tasks": []any{map[string]any{
				"id": taskID, "state": "in_progress", "owner_agent_ids": []any{agentID},
			}},
			"teams": []any{map[string]any{"id": "team-ref", "manifest_ref": teamManifest}},
		},
	}
	stateBytes, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	writeJSONL(t, filepath.Join(root, ".claude", "loop-state.json"), string(stateBytes))

	loaded, err := hookctx.LoadFull(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Assignments) != 1 {
		t.Fatalf("expected one assignment, got %d (%+v)", len(loaded.Assignments), loaded.Assignments)
	}
	row := loaded.Assignments[0]
	if row.ManifestRef != teamManifest || len(row.WritePaths) != 1 || row.WritePaths[0] != "internal/from-registered-team/" {
		t.Fatalf("registered Team manifest was not used: %+v", row)
	}
	// CLI callers may pass a relative --root. The registered ref must remain
	// consumable after the loader normalizes that root internally.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relativeRoot, err := filepath.Rel(cwd, root)
	if err != nil {
		t.Fatal(err)
	}
	relativeLoaded, err := hookctx.LoadFull(relativeRoot, "")
	if err != nil {
		t.Fatalf("load with relative root %q: %v", relativeRoot, err)
	}
	if len(relativeLoaded.Assignments) != 1 || relativeLoaded.Assignments[0].ManifestRef != teamManifest {
		t.Fatalf("registered Team manifest failed with relative root: %+v", relativeLoaded.Assignments)
	}
}

func TestLoadFullUsesTeamAgentIDsWhenAgentRowIsMissing(t *testing.T) {
	root := t.TempDir()
	taskID := "TASK-TEAM-AGENT-ID"
	agentID := "builder-team-agent-id"
	assignmentID := "assignment-team-agent-id"
	teamManifest := filepath.ToSlash(filepath.Join("registered", "team.json"))
	writeJSONL(t, filepath.Join(root, filepath.FromSlash(teamManifest)), `{"assignments":[{"assignment_id":"`+assignmentID+`","agent_id":"`+agentID+`","write_paths":["internal/from-team-agent-id/"],"status":"active"}]}`)
	state := map[string]any{
		"runtime_id": "loop-REQ-039", "revision": 1,
		"baseline": map[string]any{"generation": 1},
		"entities": map[string]any{
			"agents": []any{},
			"tasks":  []any{map[string]any{"id": taskID, "state": "in_progress", "owner_agent_ids": []any{agentID}}},
			"teams":  []any{map[string]any{"id": "team-agent-id", "agent_ids": []any{agentID}, "manifest_ref": teamManifest}},
		},
	}
	stateBytes, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	writeJSONL(t, filepath.Join(root, ".claude", "loop-state.json"), string(stateBytes))

	loaded, err := hookctx.LoadFull(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Assignments) != 1 {
		t.Fatalf("Team agent_ids should recover the owner assignment: %+v", loaded.Assignments)
	}
	row := loaded.Assignments[0]
	if row.ManifestRef != teamManifest || row.AssignmentID != assignmentID || len(row.WritePaths) != 1 || row.WritePaths[0] != "internal/from-team-agent-id/" {
		t.Fatalf("Team agent_ids binding was not applied: %+v", row)
	}
}

func TestLoadFullDoesNotGuessCanonicalManifestAfterFormalRefBreaks(t *testing.T) {
	root := t.TempDir()
	taskID := "TASK-FORMAL-REF-BROKEN"
	agentID := "builder-formal-ref-broken"
	assignmentID := "assignment-formal-ref-broken"
	formalManifest := filepath.ToSlash(filepath.Join(".claude", "registered", "missing.json"))
	canonicalManifest := filepath.ToSlash(filepath.Join(".claude", "workgroups", "REQ-039", taskID, "manifest.json"))
	writeJSONL(t, filepath.Join(root, filepath.FromSlash(canonicalManifest)), `{"assignments":[{"assignment_id":"`+assignmentID+`","agent_id":"`+agentID+`","write_paths":["internal/wrong-owner/"],"status":"active"}]}`)
	state := map[string]any{
		"runtime_id": "loop-REQ-039", "revision": 1,
		"baseline": map[string]any{"generation": 1},
		"entities": map[string]any{
			"agents": []any{map[string]any{
				"id": agentID, "state": "working", "task_ids": []any{taskID},
				"prompt_ref": formalManifest + "#" + assignmentID,
			}},
			"tasks": []any{map[string]any{"id": taskID, "state": "in_progress", "owner_agent_ids": []any{agentID}}},
			"teams": []any{},
		},
	}
	stateBytes, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	writeJSONL(t, filepath.Join(root, ".claude", "loop-state.json"), string(stateBytes))

	loaded, err := hookctx.LoadFull(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Assignments) != 0 {
		t.Fatalf("broken formal manifest ref must not fall through to canonical owner data: %+v", loaded.Assignments)
	}
}
