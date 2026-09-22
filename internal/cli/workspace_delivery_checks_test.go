package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestDomainChecksRequireActualPassingReceiptsAtTestedHead(t *testing.T) {
	root := t.TempDir()
	e := workspace.Execution{RuntimeID: "loop-test", BaselineGeneration: 1, AssignmentID: "assignment", Checks: []string{"test product"}}
	path := filepath.Join(filepath.Dir(integration.CheckpointPath(root, e.RuntimeID, 1, e.AssignmentID)), "checks/receipt.json")
	os.MkdirAll(filepath.Dir(path), 0700)
	cp := integration.Checkpoint{TestedHead: "tested-sha", CheckReceipts: []string{path}}
	for _, kind := range []string{"failed", "other-head", "other-command", "pass"} {
		t.Run(kind, func(t *testing.T) {
			r := map[string]any{"command": "test product", "cwd": root, "head_before": "tested-sha", "head_after": "tested-sha", "status": "pass", "exit_code": 0}
			switch kind {
			case "failed":
				r["status"] = "fail"
				r["exit_code"] = 1
			case "other-head":
				r["head_after"] = "other"
			case "other-command":
				r["command"] = "fabricated"
			}
			data, _ := json.Marshal(r)
			os.WriteFile(path, data, 0600)
			checks, err := verifiedDomainChecks(root, e, cp)
			if kind != "pass" {
				if err == nil {
					t.Fatal("unverified check promoted")
				}
				return
			}
			if err != nil || len(checks) != 1 || !strings.Contains(checks[0].EvidenceRefs[0], "#sha256=") {
				t.Fatalf("checks=%+v err=%v", checks, err)
			}
		})
	}
}
