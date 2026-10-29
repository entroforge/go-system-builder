package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	fixtures "github.com/entroforge/go-system-builder/tests/fixtures/req039"
)

func TestRuntimeEvidenceAddPacketPlanningKindRequiresTypedConsumer(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*testing.T) (string, string, string)
		wantAction string
	}{
		{
			name: "S2 no current packet",
			setup: func(t *testing.T) (string, string, string) {
				root := newPlanningEvidenceRoot(t, "design", true)
				return root, "planning_design", "real stage-entry preparation is missing"
			},
			wantAction: "not manually issue or consume",
		},
		{
			name: "S3 no current packet",
			setup: func(t *testing.T) (string, string, string) {
				root := newPlanningEvidenceRoot(t, "contracts", true)
				return root, "planning_contract", "The current S3 responsibility has no registered questionnaire"
			},
			wantAction: "not manually issue or consume",
		},
		{
			name: "S4 no current packet",
			setup: func(t *testing.T) (string, string, string) {
				root := newPlanningEvidenceRoot(t, "tasks", true)
				return root, "planning_task", "The current S4 responsibility has no registered questionnaire"
			},
			wantAction: "not manually issue or consume",
		},
		{
			name: "current issued packet",
			setup: func(t *testing.T) (string, string, string) {
				root, packet := newDesignFixture(t, "none")
				command := filepath.Join(root, filepath.FromSlash(packet.AnswerPath))
				return root, "planning_design", command
			},
			wantAction: "Save the professional answer normally",
		},
		{name: "current S3 issued packet", setup: func(t *testing.T) (string, string, string) {
			root, packet := newContractAnswerFixture(t)
			return root, "planning_contract", filepath.Join(root, packet.AnswerPath)
		}, wantAction: "Save the professional answer normally"},
		{name: "current S4 issued packet", setup: func(t *testing.T) (string, string, string) {
			root, packet := newTaskPlanningAnswerFixture(t)
			return root, "planning_task", filepath.Join(root, packet.AnswerPath)
		}, wantAction: "Save the professional answer normally"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root, kind, expected := tc.setup(t)
			statePath := filepath.Join(root, ".claude", "loop-state.json")
			journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
			beforeState, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			beforeJournal, err := os.ReadFile(journalPath)
			if err != nil {
				t.Fatal(err)
			}

			var stdout, stderr bytes.Buffer
			code := cli.Run(runtimeEvidenceAddArgs(root, kind, "missing-evidence.json"), nil, &stdout, &stderr)
			if code == 0 {
				t.Fatalf("packet-mode planning evidence add unexpectedly succeeded: %s", stdout.String())
			}
			message := stderr.String()
			if !strings.Contains(message, tc.wantAction) || !strings.Contains(message, expected) {
				t.Fatalf("missing actionable typed-consumer guidance: %s", message)
			}
			if strings.Contains(message, "runtime stage-answer issue") || strings.Contains(message, "runtime stage-answer consume") {
				t.Fatalf("refusal introduced manual planning procedures: %s", message)
			}
			if strings.Contains(message, "read evidence artifact") {
				t.Fatalf("guard should reject before reading a guessed artifact path: %s", message)
			}
			afterState, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			afterJournal, err := os.ReadFile(journalPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(beforeState, afterState) || !bytes.Equal(beforeJournal, afterJournal) {
				t.Fatal("refused packet-mode planning evidence add changed Runtime state or journal")
			}
		})
	}
}

func TestRuntimeEvidenceAddPreservesLegacyAndNonPlanningKinds(t *testing.T) {
	t.Run("packet off planning evidence", func(t *testing.T) {
		root := newLegacyDesignEvidenceRoot(t)
		addRuntimeEvidenceArtifact(t, root, "legacy-design.json", `{"schema_version":"1.0.0"}`)
		var stdout, stderr bytes.Buffer
		code := cli.Run(runtimeEvidenceAddArgs(root, "planning_design", "legacy-design.json"), nil, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("legacy planning evidence behavior changed: code=%d stderr=%s", code, stderr.String())
		}
		var result map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result["recorded"] != true {
			t.Fatalf("legacy planning evidence was not recorded: result=%#v err=%v", result, err)
		}
	})

	t.Run("packet mode non-planning evidence", func(t *testing.T) {
		root := newUnissuedDesignEvidenceRoot(t)
		addRuntimeEvidenceArtifact(t, root, "team-manifest.json", `{"schema_version":"1.0.0"}`)
		var stdout, stderr bytes.Buffer
		code := cli.Run([]string{
			"runtime", "evidence", "add", "--root", root,
			"--id", "EV-TEAM-MANIFEST", "--kind", "team_manifest",
			"--path", "team-manifest.json", "--produced-by", "Main",
		}, nil, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("non-planning evidence was blocked by the planning guard: code=%d stderr=%s", code, stderr.String())
		}
		var result map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result["recorded"] != true {
			t.Fatalf("non-planning evidence was not recorded: result=%#v err=%v", result, err)
		}
	})
}

func TestRuntimeEvidenceAddPacketGuardDoesNotRecoverPendingRuntime(t *testing.T) {
	root := newUnissuedDesignEvidenceRoot(t)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	markerPath := statePath + ".commit-pending.json"
	marker := []byte("{\"transaction_id\":\"pending-test\"}")
	if err := os.WriteFile(markerPath, marker, 0o600); err != nil {
		t.Fatal(err)
	}
	beforeState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	beforeJournal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run(runtimeEvidenceAddArgs(root, "planning_design", "missing-evidence.json"), nil, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("runtime evidence add unexpectedly crossed a pending Runtime operation: %s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "pending") {
		t.Fatalf("pending Runtime operation was not reported: %s", stderr.String())
	}
	afterState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	afterJournal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	afterMarker, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeState, afterState) || !bytes.Equal(beforeJournal, afterJournal) || !bytes.Equal(marker, afterMarker) {
		t.Fatal("refused runtime evidence add recovered or changed the pending Runtime state, journal, or marker")
	}
}

func runtimeEvidenceAddArgs(root, kind, path string) []string {
	return []string{
		"runtime", "evidence", "add", "--root", root,
		"--id", "EV-PLANNING-GUARD", "--kind", kind,
		"--path", path, "--produced-by", "Main", "--responsibility", "Orchestrator",
	}
}

func newUnissuedDesignEvidenceRoot(t *testing.T) string {
	t.Helper()
	return newPlanningEvidenceRoot(t, "design", true)
}

func newLegacyDesignEvidenceRoot(t *testing.T) string {
	t.Helper()
	root := fixtures.FreshRoot(t)
	state := fixtures.BaseState(t, root, "planning", "design", 0)
	fixtures.WriteState(t, root, state)
	return root
}

func newPlanningEvidenceRoot(t *testing.T, phase string, packetMode bool) string {
	t.Helper()
	root := fixtures.FreshRoot(t)
	state := fixtures.BaseState(t, root, "planning", phase, 0)
	state["schema_version"] = "1.2.0"
	state["review"].(map[string]any)["round"] = 0
	state["configuration"].(map[string]any)["optimization"] = map[string]any{"packet": packetMode}
	entities := state["entities"].(map[string]any)
	entities["work_packets"], entities["submissions"] = []any{}, []any{}
	bound := state["bound_req"].(map[string]any)
	documents := []any{map[string]any{
		"id": bound["id"], "kind": "req", "path": bound["path"], "version": bound["version"],
		"sha256": bound["sha256"], "status": "locked", "generation": 1,
	}}
	if phase == "contracts" {
		documents = append(documents, map[string]any{
			"id": "ARCHITECTURE-001", "kind": "design", "path": "docs/architecture/ARCHITECTURE-001.md",
			"version": "v1.0.0", "sha256": strings.Repeat("b", 64), "status": "locked", "generation": 1,
		})
	}
	state["documents"] = documents
	fixtures.WriteState(t, root, state)
	return root
}

func addRuntimeEvidenceArtifact(t *testing.T, root, path, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
