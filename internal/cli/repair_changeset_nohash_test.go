package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeRepairChangesetComputeExplicitPathWithoutDigests(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "product.txt"), []byte("simulation\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runRuntimeRepairChangesetCompute([]string{"--root", root, "--session-id", "session-1", "--paths", "product.txt"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("changeset compute exit %d: %s", code, stderr.String())
	}
	var response struct {
		Changeset map[string]any `json:"changeset"`
		Reference struct {
			Path string `json:"path"`
		} `json:"artifact_ref"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"digest", "base_ref", "head_ref"} {
		if _, exists := response.Changeset[key]; exists {
			t.Fatalf("response contains %s: %s", key, stdout.String())
		}
	}
	artifacts := response.Changeset["artifacts"].([]any)
	if len(artifacts) != 1 || artifacts[0].(map[string]any)["path"] != "product.txt" {
		t.Fatalf("response artifacts = %#v", artifacts)
	}
	if _, exists := artifacts[0].(map[string]any)["sha256"]; exists {
		t.Fatalf("response contains SHA: %s", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(response.Reference.Path))); err != nil {
		t.Fatalf("original persisted changeset missing: %v", err)
	}
}

func TestRuntimeRepairChangesetComputeSessionUsesKnownResultOrReportsUnknown(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, ".claude", "review", "repair", "sessions")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	session := []byte(`{"schema_version":"1.0.0","record_type":"repair_session","session_id":"repair-session-1","contract_ref":"contract.json","runtime_id":"loop-REQ-001","req_id":"REQ-001","baseline_generation":1,"status":"planned","created_by":"main","created_at":"2026-11-01T12:00:00Z"}`)
	if err := os.WriteFile(filepath.Join(sessionDir, "repair-session-1.json"), session, 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	args := []string{"--root", root, "--session-id", "repair-session-1"}
	if code := runRuntimeRepairChangesetCompute(args, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "unknown") {
		t.Fatalf("missing original facts should be unknown: exit %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	state := []byte(`{"review":{"repair":{"session_id":"repair-session-1","plan_ref":"plan.json","result_refs":[{"path":"result.json"}]}}}`)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-state.json"), state, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plan.json"), []byte(`{"contract_id":"contract-1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "result.json"), []byte(`{"assignment_id":"a","changed_artifacts":[{"path":"product.txt","status":"modified"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := runRuntimeRepairChangesetCompute(args, &stdout, &stderr); code != 0 {
		t.Fatalf("accepted result projection: exit %d stderr=%s", code, stderr.String())
	}
	var response struct {
		Changeset map[string]any `json:"changeset"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Changeset["source"] != "accepted_results" {
		t.Fatalf("source is not the accepted result: %s", stdout.String())
	}
	artifacts := response.Changeset["artifacts"].([]any)
	if len(artifacts) != 1 || artifacts[0].(map[string]any)["path"] != "product.txt" {
		t.Fatalf("original contribution not projected: %s", stdout.String())
	}
}
