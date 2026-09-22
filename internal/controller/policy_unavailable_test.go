package controller

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestFinalSafetyRechecksPolicyAndPreservesQualitySeparation(t *testing.T) {
	for _, corruption := range []string{"missing", "invalid", "valid"} {
		for _, tool := range []string{"Write", "Bash", "Read"} {
			t.Run(corruption+"/"+tool, func(t *testing.T) {
				root := t.TempDir()
				if err := os.MkdirAll(filepath.Join(root, "docs", "control"), 0755); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(root, "docs/control/hook-policy.json")
				data, err := os.ReadFile("../../docs/control/hook-policy.json")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				// The transport's initial load succeeded. The final safety pass
				// must still notice deletion/corruption during the control cycle.
				if _, err := policy.Load(path); err != nil {
					t.Fatal(err)
				}
				if corruption == "missing" {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				}
				if corruption == "invalid" {
					if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				result := applyFinalSafety(ControlResult{QualityGate: QualityGateResult{Status: StatusNotReady}}, ControlRequest{Root: root, Event: "PreToolUse", ToolName: tool}, runtime.Snapshot{}, nil)
				if corruption != "valid" && tool != "Read" {
					if result.Decision.Decision != "deny" || result.Decision.RuleID != policy.RulePolicyUnavailable || result.QualityGate.Status != StatusBlocked {
						t.Fatalf("unsafe fallback: %+v", result)
					}
				} else if result.Decision.Decision == "deny" || result.QualityGate.Status != StatusNotReady {
					t.Fatalf("read/quality progress incorrectly blocked: %+v", result)
				}
			})
		}
	}
}
