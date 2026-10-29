package hook

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/workspace"
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
func BuildLifecycleAdditionalContext(event string, input policy.Input, decision policy.Decision, assignments []hookctx.AssignmentContext, delivery ...string) string {
	entrance := lifecycleEntrance(input)
	var essential, detail string
	var delivered []string
	for _, extra := range delivery {
		extra = strings.TrimSpace(strings.ToValidUTF8(extra, "�"))
		if extra != "" {
			delivered = append(delivered, extra)
		}
	}
	deliveryContext := strings.Join(delivered, "\n\n")
	hasDelivery := deliveryContext != ""
	switch event {
	case "SessionStart":
		if decision.Guidance != nil {
			g := decision.Guidance
			action := g.Action
			if hasDelivery {
				action = "follow the responsibility delivery above"
			}
			essential = fmt.Sprintf("LOOP RECOVERY — source=%s; stage=%s @ rev=%d; next=%s; read=%s", sanitizeContextValue(firstNonEmpty(input.Source, "startup")), sanitizeContextValue(g.Stage), g.Revision, sanitizeContextValue(action), sanitizeContextValue(g.ProtocolRef))
			if g.ManualRef != "" {
				essential += "; if blocked, read " + sanitizeContextValue(g.ManualRef)
			}
			essential += "; Next: follow the registered next operation above."
			if g.HumanRequired {
				essential += " Human Gateway required; stop automation."
			}
			if utf8.RuneCountInString(essential) > lifecycleContextLimit {
				essential = fmt.Sprintf("LOOP CONTEXT — stage=%s @ rev=%d; next=read the full current operation at %s; read=%s", sanitizeContextValue(g.Stage), g.Revision, entrance, entrance)
			}
		}
	case "SubagentStart":
		assignment, ok := matchLifecycleAssignment(input, assignments)
		if ok && assignment.AssignmentID != "" {
			next := "register the Assignment Result before stopping"
			read := assignment.ManifestRef
			if read == "" {
				read = ".claude/loop-state.json"
			}
			if decision.Guidance != nil && decision.Guidance.Action != "" {
				next = decision.Guidance.Action
			}
			if hasDelivery {
				next = "follow the responsibility delivery above"
			}
			essential = fmt.Sprintf("LOOP ASSIGNMENT — assignment_id=%s; task_id=%s; role=%s; state=%s; next=%s; read=%s", sanitizeContextValue(assignment.AssignmentID), sanitizeContextValue(assignment.TaskID), sanitizeContextValue(firstNonEmpty(assignment.RoleFamily, "unspecified")), sanitizeContextValue(assignment.State), sanitizeContextValue(next), sanitizeContextValue(read))
			if utf8.RuneCountInString(essential) > lifecycleContextLimit {
				essential = "LOOP ASSIGNMENT — next=read your complete registered Assignment and operation at " + entrance + "; read=" + entrance
			}
			scope := firstNonEmpty(strings.Join(assignment.WritePaths, ","), "(none declared; remain read-only)")
			checks := firstNonEmpty(strings.Join(assignment.RequiredChecks, ","), "(none declared)")
			done := firstNonEmpty(strings.Join(assignment.DoneWhen, "; "), "manifest does not declare done_when; read the Assignment contract before writing or stopping")
			detail = fmt.Sprintf("scope=%s; done_when=%s; required_checks=%s", sanitizeContextValue(scope), sanitizeContextValue(done), sanitizeContextValue(checks))
		} else if decision.Guidance != nil && len(decision.Guidance.Questions) > 0 {
			essential = "LOOP PREFLIGHT — next=verify the registered subagent dispatch; read=" + entrance
			detail = "Preflight questions: " + sanitizeContextValue(strings.Join(decision.Guidance.Questions, " | "))
		}
	default:
		return ""
	}
	// The stage-answer delivery is already the authoritative projection of the
	// issued questionnaire and its actual answer path. Keep it whole and ahead
	// of optional lifecycle guidance so a path cannot be lost to the 800-rune
	// background budget.
	var context string
	if deliveryContext != "" {
		context = ComposeLifecycleContext(deliveryContext, essential, entrance)
	} else {
		context = ComposeLifecycleContext(essential, "", entrance)
	}
	if event == "SessionStart" && input.EffectiveAgentID() == "" && decision.Guidance != nil {
		// Reception is the current Main action; include it before optional
		// candidate lists. Append keeps every command whole within one budget.
		for _, line := range decision.Guidance.Automation {
			for _, prefix := range []string{"S6 current Main action:", "Original action argv:", "Dispatch budget unknown;", "Advisory dispatch budget:", "S6 reception projection unavailable;", "Describe the required correction", "Configured S6 backpressure:", "S6 integration observation unavailable:"} {
				if strings.HasPrefix(line, prefix) {
					context = AppendLifecycleContext(context, line)
					break
				}
			}
		}
		for _, line := range decision.Guidance.Automation {
			for _, prefix := range []string{"eligible tasks", "results awaiting verified integration", "waiting:", "S6 approved dispatch plan:", "S6 dispatch projection unavailable"} {
				if strings.HasPrefix(line, prefix) {
					context = AppendLifecycleContext(context, line)
					break
				}
			}
		}
	}
	return AppendLifecycleContext(context, detail)
}

// ComposeLifecycleContext retains its first section whole. Optional details
// fit only within the remaining lifecycle budget; they never displace a
// complete responsibility delivery or path.
func ComposeLifecycleContext(essential, detail string, entrances ...string) string {
	entrance := ".claude/loop-state.json"
	if len(entrances) > 0 {
		entrance = entrances[0]
	}
	essential = strings.TrimSpace(strings.ToValidUTF8(essential, "�"))
	detail = strings.TrimSpace(strings.ToValidUTF8(detail, "�"))
	if essential == "" {
		if utf8.RuneCountInString(detail) <= lifecycleContextLimit {
			return detail
		}
		return "LOOP CONTEXT — next=read the complete current guidance at " + entrance + "; read=" + entrance
	}
	if detail != "" && utf8.RuneCountInString(essential)+2+utf8.RuneCountInString(detail) <= lifecycleContextLimit {
		return essential + "\n\n" + detail
	}
	return essential
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

func sanitizeContextValue(value string) string {
	return strings.Join(strings.Fields(strings.ToValidUTF8(value, "�")), " ")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func lifecycleEntrance(input policy.Input) string {
	if b := input.Runtime.Workspace; b != nil && input.CWD != "" {
		for _, e := range b.Executions {
			if e.AgentID != input.EffectiveAgentID() || e.Status != "ready" {
				continue
			}
			relative, err := filepath.Rel(e.Path, input.CWD)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				continue
			}
			if workspace.HasDispatchContext(e) {
				return workspace.DispatchContextPath
			}
			return ".claude/loop-workspace.json"
		}
	}
	return ".claude/loop-state.json"
}

// AppendLifecycleContext accepts only complete optional sections within the
// background budget. An oversized responsibility delivery remains untouched.
func AppendLifecycleContext(current string, sections ...string) string {
	current = strings.TrimSpace(strings.ToValidUTF8(current, "�"))
	for _, section := range sections {
		section = strings.TrimSpace(strings.ToValidUTF8(section, "�"))
		if section != "" && utf8.RuneCountInString(current)+2+utf8.RuneCountInString(section) <= lifecycleContextLimit {
			if current != "" {
				current += "\n\n"
			}
			current += section
		}
	}
	return current
}
