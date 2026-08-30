package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestRuntimeTransitionMissingEvidenceExplainsRecoveryBinding(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	definition, err := os.ReadFile(filepath.Join("..", "..", "docs", "loop-definition.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "loop-definition.json"), definition, 0o644); err != nil {
		t.Fatal(err)
	}

	stateData, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatal(err)
	}
	state["revision"] = float64(1)
	state["lifecycle"].(map[string]any)["state"] = "building"
	state["lifecycle"].(map[string]any)["phase"] = nil
	stateData, err = json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, "loop-state.json")
	journalPath := filepath.Join(root, "loop-events.jsonl")
	if err := os.WriteFile(statePath, stateData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"runtime", "transition", "--root", root,
		"--state", statePath, "--journal", journalPath,
		"--id", "TR-006", "--expected-revision", "1", "--actor", "orchestrator",
	}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 {
		t.Fatal("transition without required evidence must fail")
	}
	if !strings.Contains(stderr.String(), "--evidence builder_report_record=<reference>") {
		t.Fatalf("missing-binding error must explain the recovery command, got %s", stderr.String())
	}
}

func TestExplainListsEligibleCurrentEvidenceCandidates(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	definition, err := os.ReadFile(filepath.Join("..", "..", "docs", "loop-definition.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "loop-definition.json"), definition, 0o644); err != nil {
		t.Fatal(err)
	}

	writeArtifact := func(relative, contents string) string {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		data := []byte(contents)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		return fmt.Sprintf("%x", sum[:])
	}

	builderPath := "evidence/builder-report.md"
	teamPath := "evidence/team-manifest.md"
	builderSHA := writeArtifact(builderPath, "eligible builder report")
	teamSHA := writeArtifact(teamPath, "eligible team manifest")
	staleSHA := writeArtifact("evidence/stale-builder-report.md", "stale builder report")
	invalidSHA := writeArtifact("evidence/invalid-builder-report.md", "invalid builder report")

	stateData, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatal(err)
	}
	state["revision"] = float64(7)
	state["lifecycle"].(map[string]any)["state"] = "building"
	state["lifecycle"].(map[string]any)["phase"] = nil
	state["baseline"].(map[string]any)["generation"] = float64(2)
	state["review"].(map[string]any)["round"] = float64(3)
	state["evidence"] = []any{
		map[string]any{
			"id": "ev-builder-current", "kind": "builder_report", "path": builderPath,
			"sha256": builderSHA, "status": "valid", "baseline_generation": float64(2), "review_round": float64(3),
		},
		map[string]any{
			"id": "ev-team-current", "kind": "team_manifest", "path": teamPath,
			"sha256": teamSHA, "status": "valid", "baseline_generation": float64(2), "review_round": float64(3),
		},
		map[string]any{
			"id": "ev-builder-stale", "kind": "builder_report", "path": "evidence/stale-builder-report.md",
			"sha256": staleSHA, "status": "valid", "baseline_generation": float64(1), "review_round": float64(3),
		},
		map[string]any{
			"id": "ev-builder-invalid", "kind": "builder_report", "path": "evidence/invalid-builder-report.md",
			"sha256": invalidSHA, "status": "invalid", "baseline_generation": float64(2), "review_round": float64(3),
		},
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(root, "loop-state.json")
	if err := os.WriteFile(statePath, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(root, "loop-events.jsonl")
	if err := os.WriteFile(journalPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{
		"explain", "TR-006", "--root", root, "--state", statePath,
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("explain must succeed, code=%d stderr=%s", code, stderr.String())
	}
	output := stdout.String()
	for _, want := range []string{
		"builder_report_record",
		"ev-builder-current",
		builderPath,
		"builder_report",
		"status: valid",
		"baseline_generation: 2",
		"review_round: 3",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("explain output must contain %q, got:\n%s", want, output)
		}
	}
	// L3-S6 §8.3: TR-006 no longer requires team_manifest_record — the S7
	// workgroup cannot be registered during building, so demanding its
	// evidence here forced placeholder records.
	if strings.Contains(output, "team_manifest_record") {
		t.Fatalf("explain output must not list team_manifest_record for TR-006, got:\n%s", output)
	}
	for _, unwanted := range []string{"ev-builder-stale", "ev-builder-invalid", "ev-team-current"} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("ineligible candidate %q must not be listed, got:\n%s", unwanted, output)
		}
	}
	// L3-S6 complexity pass: explain carries the gate's missing-token
	// legend up front so the vocabulary is readable before the first
	// not_ready packet.
	for _, want := range []string{
		"GATE MISSING-TOKEN LEGEND (GATE-BUILDER-BATCH-READY):",
		"`integration_checkpoint:<id>`",
		"run `runtime task-integrate --assignment-id <id>`",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("explain TR-006 must carry the token legend (%q missing), got:\n%s", want, output)
		}
	}
}
