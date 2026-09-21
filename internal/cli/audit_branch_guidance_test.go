package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/cli"
)

// TestAuditS6StatusDoesNotAdvertiseBeforeDocumentPass reproduces the
// S4→S5→S6 source-boundary check. TR-002 registers the completed plan and
// TASK documents before S5; an S5 runtime must not make the S6 projection
// claim that the plan is approved or select a Builder batch.
func TestAuditS6StatusDoesNotAdvertiseBeforeDocumentPass(t *testing.T) {

	root := auditCLIPlanFixture(t)
	statePath := filepath.Join(root, ".claude", "loop-state.json")
	stateBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		t.Fatal(err)
	}
	state["lifecycle"] = map[string]any{
		"state": "document_verification",
		"phase": nil,
	}
	updated, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, append(updated, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := cli.Run([]string{"s6", "status", "--root", root, "--capacity", "1"}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("s6 status failed before the assertion: code=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "S6 dispatch plan:") || strings.Contains(stdout.String(), "Next batch:") {
		t.Fatalf("S5 runtime was advertised as an approved S6 dispatch source:\n%s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = cli.Run([]string{"s6", "status", "--root", root, "--capacity", "1", "--json"}, strings.NewReader(""), &stdout, &stderr)
	var projection struct {
		Available bool     `json:"dispatch_available"`
		Next      []string `json:"next"`
		State     string   `json:"lifecycle_state"`
	}
	if code != 0 || json.Unmarshal(stdout.Bytes(), &projection) != nil || projection.Available || len(projection.Next) != 0 || projection.State != "document_verification" {
		t.Fatalf("S5 JSON advertised dispatch: %d %s %s", code, stdout.String(), stderr.String())
	}

}

// TestAuditGuidanceUsesREQScopedPlanPath catches stale template links left
// behind by the waves-v1 migration. The current S4 contract is
// docs/dev/tasks/index-REQ-<id>.md; there is no docs/dev/tasks/index.md entry point.
func TestAuditGuidanceUsesREQScopedPlanPath(t *testing.T) {

	templatePaths := []string{
		"docs/dev/contracts/CONTRACTS-template.md",
		"docs/dev/contracts/FE-contract-template.md",
		"docs/dev/contracts/BE-contract-template.md",
		"docs/dev/contracts/SYNC-contract-template.md",
		"docs/requirements/REQ-template.md",
		"docs/project-map-template.md",
		"docs/reports/acceptance/ACC-template.md",
		"docs/reports/release-audits/TEMPLATE.md",
	}
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate audit test source")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(sourceFile)))
	for _, rel := range templatePaths {
		data, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if strings.Contains(string(data), "docs/dev/tasks/index.md") || strings.Contains(string(data), "../tasks/index.md") {
			t.Errorf("%s still points at the absent unscoped plan index", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "docs/dev/tasks/index.md")); !os.IsNotExist(err) {
		t.Fatalf("docs/dev/tasks/index.md unexpectedly exists: %v", err)
	}
}
