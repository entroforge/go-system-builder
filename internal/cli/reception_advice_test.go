package cli

import (
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/dispatch"
)

func TestReceptionAdviceTextShowsOriginalRetryReworkAndPendingCommands(t *testing.T) {
	advice := dispatch.ReceptionAdvice{Action: dispatch.ReceptionAction{
		Kind:           "recover_integration",
		Subject:        "assignment-build-TASK-002-g1",
		Reason:         "required check failed",
		Argv:           []string{"loop-harness", "runtime", "task-integrate", "--root", "/project", "--assignment-id", "assignment-build-TASK-002-g1", "--retry-preserved"},
		ReworkCommand:  []string{"loop-harness", "runtime", "register-workgroup", "--root", "/project", "--manifest", "<fresh-manifest>", "--task-id", "TASK-002", "--task", "docs/dev/tasks/TASK-002.md", "--revise-assignment", "assignment-build-TASK-002-g1", "--revision-intent-key", "<new-key>"},
		PendingCommand: []string{"loop-harness", "runtime", "workspace", "pending", "--root", "/project"},
	}}
	text := strings.Join(receptionAdviceText(advice), "\n")
	for _, want := range []string{
		"Original action argv: 'loop-harness' 'runtime' 'task-integrate'",
		"--retry-preserved",
		"Prepare this revision draft first: 'loop-harness' 's6' 'prepare' '--root' '/project' '--task' 'TASK-002' '--role' '<Builder-role>' '--agent-id' '<new-platform-Agent-ID>' '--platform-team-id' '<new-platform-Team-ID>' '--revise-assignment' 'assignment-build-TASK-002-g1' '--revision-intent-key' '<new-key>'",
		"Original rework argv: 'loop-harness' 'runtime' 'register-workgroup'",
		"--revise-assignment' 'assignment-build-TASK-002-g1'",
		"--task' 'docs/dev/tasks/TASK-002.md'",
		"Pending diagnostic argv: 'loop-harness' 'runtime' 'workspace' 'pending' '--root' '/project'",
		"fixed manifest-s6-r<N>.json draft and returns the full register command to review and use",
		"root, TASK ID/path and Assignment are carried from the original rework argv and current Record",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("reception advice omitted %q:\n%s", want, text)
		}
	}
}

func TestReceptionAdviceTextDoesNotInventReworkRoute(t *testing.T) {
	advice := dispatch.ReceptionAdvice{Action: dispatch.ReceptionAction{
		Kind:    "receive_integration",
		Subject: "assignment-build-TASK-002-g1",
		Argv:    []string{"loop-harness", "runtime", "task-integrate", "--assignment-id", "assignment-build-TASK-002-g1"},
	}}
	text := strings.Join(receptionAdviceText(advice), "\n")
	if strings.Contains(text, "Prepare this revision draft first:") || strings.Contains(text, "Original rework argv:") || strings.Contains(text, "Pending diagnostic argv:") || strings.Contains(text, "full register command") {
		t.Fatalf("non-rework action received synthetic recovery guidance:\n%s", text)
	}
}

func TestReceptionAdviceTextDoesNotPrepareLegacyReworkAsCanonicalRevision(t *testing.T) {
	advice := dispatch.ReceptionAdvice{Action: dispatch.ReceptionAction{
		Kind:          "recover_integration",
		Subject:       "legacy-assignment",
		Argv:          []string{"loop-harness", "runtime", "workspace", "integrate", "--root", "/project", "--assignment", "legacy-assignment", "--retry-preserved"},
		ReworkCommand: []string{"loop-harness", "runtime", "workspace", "rework", "--root", "/project", "--assignment", "legacy-assignment", "--reason", "<describe required code correction>"},
	}}
	text := strings.Join(receptionAdviceText(advice), "\n")
	if strings.Contains(text, "Prepare this revision draft first:") || strings.Contains(text, "register-workgroup") {
		t.Fatalf("legacy rework received canonical revision instructions:\n%s", text)
	}
}
