package req039_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestFinalAuditActivationUsesAuthorityRoot(t *testing.T) {
	for _, verb := range []string{"agent-begin", "agent-event"} {
		t.Run(verb, func(t *testing.T) {
			root := s4AuditRegistrationRoot(t)
			registerRecheckTask(t, root, "root", "TASK-042-01", "packages/validation/source", "packages/validation/generated/result.json")
			plan := filepath.Join(root, ".claude", "root-plan.json")
			writeRecheckPlan(t, plan, recheckWorkerSpec{suffix: "root", taskID: "TASK-042-01", agentID: "builder-audit-root", assignment: "assignment-audit-root"})
			args := []string{"runtime", verb, "--root", root, "--agent-id", "builder-audit-root"}
			if verb == "agent-begin" {
				args = append(args, "--plan", plan)
			} else {
				args = append(args, "--event", "readback_submitted", "--message", plan)
			}
			var out, errs bytes.Buffer
			if code := runCLI(t, args, strings.NewReader(""), &out, &errs); code != 0 {
				t.Fatalf("%s with --root and default Runtime paths: code=%d stdout=%s stderr=%s", verb, code, out.String(), errs.String())
			}
		})
	}
}
