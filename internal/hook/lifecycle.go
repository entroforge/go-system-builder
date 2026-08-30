package hook

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/policy"
)

const lifecycleContextLimit = 800

// BuildLifecycleAdditionalContext creates the small amount of context that
// must be present at a lifecycle boundary. It is intentionally a projection,
// not a second instruction document: the runtime and the full Guidance
// packet remain authoritative.
//
// SubagentStart only receives an Assignment brief when the platform payload
// can be matched to exactly one runtime assignment. An ambiguous match is
// returned as empty rather than guessing and injecting another agent's scope.
func BuildLifecycleAdditionalContext(event string, input policy.Input, decision policy.Decision, assignments []hookctx.AssignmentContext) string {
	switch event {
	case "SessionStart":
		return sessionStartContext(input, decision)
	case "SubagentStart":
		return subagentStartContext(input, decision, assignments)
	default:
		return ""
	}
}

func sessionStartContext(input policy.Input, decision policy.Decision) string {
	if decision.Guidance == nil {
		return ""
	}
	source := strings.TrimSpace(input.Source)
	if source == "" {
		source = "startup"
	}
	guidance := decision.Guidance
	context := fmt.Sprintf(
		"LOOP CONTEXT — source=%s; stage=%s @ rev=%d; next=%s; read=%s",
		sanitizeContextValue(source),
		sanitizeContextValue(guidance.Stage),
		guidance.Revision,
		sanitizeContextValue(guidance.Action),
		sanitizeContextValue(guidance.ProtocolRef),
	)
	return boundLifecycleContext(context)
}

func subagentStartContext(input policy.Input, decision policy.Decision, assignments []hookctx.AssignmentContext) string {
	assignment, ok := matchLifecycleAssignment(input, assignments)
	if !ok || assignment.AssignmentID == "" {
		return ""
	}
	scope := strings.Join(assignment.WritePaths, ",")
	if scope == "" {
		scope = "(none declared; remain read-only)"
	}
	checks := strings.Join(assignment.RequiredChecks, ",")
	if checks == "" {
		checks = "(none declared)"
	}
	doneWhen := strings.Join(assignment.DoneWhen, "; ")
	if doneWhen == "" {
		// Legacy manifests may not have the field yet. Be explicit about the
		// missing contract so the Worker reads the authoritative manifest
		// instead of mistaking a generic sentence for a real completion gate.
		doneWhen = "manifest does not declare done_when; read the Assignment contract before writing or stopping"
	}
	next := "register the Assignment Result before stopping"
	read := ""
	if decision.Guidance != nil {
		if decision.Guidance.Action != "" {
			next = decision.Guidance.Action
		}
		read = decision.Guidance.ProtocolRef
	}
	context := fmt.Sprintf(
		"LOOP ASSIGNMENT — assignment_id=%s; task_id=%s; role=%s; scope=%s; done_when=%s; required_checks=%s; next=%s",
		sanitizeContextValue(assignment.AssignmentID),
		sanitizeContextValue(assignment.TaskID),
		sanitizeContextValue(firstNonEmpty(assignment.RoleFamily, "unspecified")),
		sanitizeContextValue(scope),
		sanitizeContextValue(doneWhen),
		sanitizeContextValue(checks),
		sanitizeContextValue(next),
	)
	if read != "" {
		context += "; read=" + sanitizeContextValue(read)
	}
	return boundLifecycleContext(context)
}

func matchLifecycleAssignment(input policy.Input, assignments []hookctx.AssignmentContext) (hookctx.AssignmentContext, bool) {
	var candidates []hookctx.AssignmentContext
	for _, assignment := range assignments {
		if input.AgentID != "" {
			if assignment.OwnerAgentID == input.AgentID {
				candidates = append(candidates, assignment)
			}
			continue
		}
		if input.AgentType != "" && lifecycleAgentTypeMatches(input.AgentType, assignment) {
			candidates = append(candidates, assignment)
		}
	}
	if input.AgentID == "" && input.AgentType == "" && len(assignments) == 1 {
		candidates = append(candidates, assignments[0])
	}
	if len(candidates) != 1 {
		return hookctx.AssignmentContext{}, false
	}
	return candidates[0], true
}

func lifecycleAgentTypeMatches(agentType string, assignment hookctx.AssignmentContext) bool {
	want := strings.ToLower(strings.TrimSpace(agentType))
	if want == "" {
		return false
	}
	if strings.ToLower(strings.TrimSpace(assignment.RoleFamily)) == want {
		return true
	}
	ref := strings.TrimSuffix(filepath.Base(assignment.AgentDefinitionRef), filepath.Ext(assignment.AgentDefinitionRef))
	return strings.ToLower(ref) == want
}

func boundLifecycleContext(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= lifecycleContextLimit {
		return value
	}
	return value[:lifecycleContextLimit-3] + "..."
}

func sanitizeContextValue(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
