package policy

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/classifier"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

const RuleNativeOwnedCheck = "native_owned_check_binding"

// NativeOwnedCheckInvocation renders the only shell spelling accepted by the
// native WorkerCheck PreToolUse rule. Root is the original Main control root;
// the caller's CWD is checked independently against the registered owner.
func NativeOwnedCheckInvocation(root, owner string, index int) string {
	return workspace.NativeOwnedCheckInvocation(root, owner, index)
}

// NativeOwnedCheckDecision recognizes attempts to invoke the native check
// adapter. It permits only a byte-for-byte command generated from the current
// trusted Assignment projection and only from Main or that registered
// Worker's original checkout. In particular, --root and --agent-id are
// compared with hook/runtime facts and do not establish either identity.
func NativeOwnedCheckDecision(input Input) (Decision, bool) {
	if input.Event != "PreToolUse" || input.ToolName != "Bash" {
		return Decision{}, false
	}
	command, _ := input.ToolInput["command"].(string)
	if !nativeOwnedCheckIntent(command) {
		return Decision{}, false
	}
	deny := func(reason string) (Decision, bool) {
		return Decision{Decision: "deny", RuleID: RuleNativeOwnedCheck, Reason: reason,
			Recovery: []string{"use the exact owned_check_commands from the current frozen native Assignment context and retry from its registered owner checkout"},
			Retry:    RetryAfterRecoveryValidation}, true
	}
	if strings.TrimSpace(input.SessionID) == "" || input.CWD == "" || input.Runtime.ProjectRoot == "" {
		return deny("native WorkerCheck requires the original platform session, CWD and trusted Runtime root")
	}
	root, err := workspace.Canonical(input.Runtime.ProjectRoot)
	if err != nil || root != input.Runtime.ProjectRoot {
		return deny("native WorkerCheck Runtime root is not the canonical trusted Main root")
	}
	cwd, err := workspace.Canonical(input.CWD)
	if err != nil {
		return deny("native WorkerCheck platform CWD is unavailable")
	}
	if input.Runtime.CurrentState != "building" || input.Runtime.CurrentStage != "S6" || input.Runtime.RuntimeID == "" || input.Runtime.CurrentBaselineGeneration < 1 {
		return deny("native WorkerCheck is limited to the current S6 Builder Runtime")
	}
	if len(input.Runtime.NativeOwnedAssignments) == 0 {
		return deny("current trusted Runtime context contains no frozen native WorkerCheck Assignment")
	}
	for _, assignment := range input.Runtime.NativeOwnedAssignments {
		if !assignment.ContextBound || !assignment.PlanActivated || assignment.OwnerState != "working" && assignment.OwnerState != "reported" || assignment.AssignmentID == "" || assignment.OwnerAgentID == "" || assignment.AssignmentRecordSHA256 == "" || len(assignment.Checks) == 0 {
			continue
		}
		expectedCWD := root
		agentID := input.EffectiveAgentID()
		if agentID != "" {
			// Native Worker identity comes from the platform payload and the
			// hookctx-resolved Agent, never from --agent-id.
			if input.Runtime.Agent == nil || agentID != assignment.OwnerAgentID || input.Runtime.Agent.ID != assignment.OwnerAgentID || input.Runtime.Agent.AssignmentID != assignment.AssignmentID || input.Runtime.Agent.LifecycleActivity() != assignment.OwnerState || input.Runtime.Agent.DispatchMode != "plan_checkpoint" || input.Runtime.Agent.PlanReportedRef == "" {
				continue
			}
			if assignment.WorktreePath == "" {
				continue
			}
			path := assignment.WorktreePath
			if !filepath.IsAbs(path) {
				path = filepath.Join(root, filepath.FromSlash(path))
			}
			expectedCWD, err = workspace.Canonical(path)
			if err != nil {
				continue
			}
		} else if input.Runtime.Agent != nil {
			// The authenticated Main session may request an already-authorized
			// owner's check from Main; it does not become the Worker receipt owner.
			continue
		}
		if cwd != expectedCWD {
			continue
		}
		for _, check := range assignment.Checks {
			if check.Index < 0 || check.Command == "" || command != NativeOwnedCheckInvocation(root, assignment.OwnerAgentID, check.Index) {
				continue
			}
			return Decision{}, false
		}
	}
	return deny("native WorkerCheck command, owner, check index, session or CWD differs from the current frozen Assignment")
}

func nativeOwnedCheckIntent(command string) bool {
	tokens, err := classifier.Tokenize(command)
	if err != nil {
		return strings.Contains(command, "assignment-context") && strings.Contains(command, "check")
	}
	for i := 0; i+2 < len(tokens); i++ {
		if tokens[i].Value == "runtime" && tokens[i+1].Value == "assignment-context" && tokens[i+2].Value == "check" {
			return true
		}
	}
	return false
}

// NativeOwnedCheckProjection constructs the trusted projection from one
// current canonical Assignment. Hook context loading owns source verification.
func NativeOwnedCheckProjection(recordSHA, assignmentID, owner, worktree, ownerState string, contextBound, planActivated bool, checks []NativeOwnedCheck) (NativeOwnedAssignment, error) {
	if recordSHA == "" || assignmentID == "" || owner == "" {
		return NativeOwnedAssignment{}, fmt.Errorf("native WorkerCheck projection has incomplete Record identity")
	}
	return NativeOwnedAssignment{
		AssignmentID: assignmentID, OwnerAgentID: owner, WorktreePath: worktree,
		AssignmentRecordSHA256: recordSHA, OwnerState: ownerState,
		ContextBound: contextBound, PlanActivated: planActivated,
		Checks: append([]NativeOwnedCheck(nil), checks...),
	}, nil
}
