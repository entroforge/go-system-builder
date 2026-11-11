package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func nativeS9Routes(ctx context.Context, state map[string]any) ([]runtime.RepairPlanReportRoute, error) {
	if stringValue(repairPointerCLI(state)["session_id"]) == "" {
		return nil, nil
	}

	return runtime.CurrentRepairPlanReportRoutesContext(ctx, state)
}

func nativeS9RouteBrief(route runtime.RepairPlanReportRoute) string {
	return route.BriefPath
}

func nativeS9RoutePacket(ctx context.Context, root string, state map[string]any, route runtime.RepairPlanReportRoute) (workpacket.RepairPlanReportPacket, error) {
	packet, found, err := runtime.FindRepairPlanReportPacketContext(runtime.WithPacketBodyRoot(ctx, root), state, route.ID)
	if err != nil {
		return packet, err
	}
	if !found {
		return packet, fmt.Errorf("S9 original packet %s is missing", route.ID)
	}
	if packet.Binding != route.Binding || packet.ViewPath != route.ViewPath || packet.AnswerPath != route.AnswerPath {
		return packet, fmt.Errorf("S9 original packet %s differs from routing index", route.ID)
	}
	return packet, nil
}

// Routing errors retain the S9 route so that the real event can report unknown
// responsibility; they must not make a damaged registry look like no work.
func nativeS9RoutingAvailable(ctx context.Context, root string, state map[string]any, worker bool) bool {
	routes, err := nativeS9Routes(ctx, state)
	if err != nil {
		return true
	}
	for _, route := range routes {
		if worker {
			return true
		}
		if route.BriefPath == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, nativeS9RouteBrief(route))); err == nil {
			return true
		}
	}
	pointer := repairPointerCLI(state)
	status := stringValue(pointer["status"])
	return stringValue(pointer["session_id"]) != "" && (status == "targeted_reverification" || status == "blocked" && pointer["failure_route"] == "blocked" && len(repairArtifactRefs(pointer, "targeted_reverification_artifacts", "")) > 0 || worker && status == "closed" && stringValue(pointer["impact_ref"]) != "")
}

// Responsibility references identify only an original registered S9 task. They
// never prove an actor, authoring checkout, Result source, or reception.
func nativeS9ResponsibilityRefs(root, relative string) []string {
	if !filepath.IsAbs(root) || relative == "" || filepath.IsAbs(relative) || strings.Contains(relative, "\\") || filepath.ToSlash(filepath.Clean(relative)) != relative || relative == "." || relative == ".." || strings.HasPrefix(relative, "../") {
		return nil
	}
	return []string{filepath.Join(root, filepath.FromSlash(relative)), relative, "./" + relative}
}

func nativeS9ResponsibilityMatch(text, root, relative string) bool {
	for _, ref := range nativeS9ResponsibilityRefs(root, relative) {
		if nativeS9BriefReferenceMatch(text, ref) {
			return true
		}
	}
	return false
}

// Only a registered Builder brief in actual task text may take one sentence
// period. Check the complete token without stripping suffixes from paths.
func nativeS9BriefReferenceMatch(text, ref string) bool {
	if nativeS6ExactReference(text, ref) {
		return true
	}
	for offset := 0; offset < len(text); {
		index := strings.Index(text[offset:], ref+".")
		if index < 0 {
			return false
		}
		index += offset
		end := index + len(ref) + 1
		before := index == 0 || strings.ContainsRune(" \t\r\n`\"'(<[{=", rune(text[index-1]))
		after := end == len(text) || strings.ContainsRune(" \t\r\n", rune(text[end]))
		if before && after {
			return true
		}
		offset = end
	}
	return false
}

// Only callers holding an original registered responsibility may convert its
// absolute reference. Paths outside the already resolved control root fail closed.
func nativeS9RegisteredResponsibilityRelative(root, ref string) string {
	if filepath.IsAbs(ref) {
		relative, err := filepath.Rel(root, ref)
		if err != nil || filepath.Join(root, relative) != ref {
			return ""
		}
		ref = filepath.ToSlash(relative)
	}
	if len(nativeS9ResponsibilityRefs(root, ref)) == 0 {
		return ""
	}
	return ref
}

func nativeS9RegisteredResponsibilityMatch(text, root, ref string) bool {
	for _, alias := range nativeS9ResponsibilityRefs(root, nativeS9RegisteredResponsibilityRelative(root, ref)) {
		if nativeS6ExactReference(text, alias) {
			return true
		}
	}
	return false
}

func nativeS9TaskResponsibilityMatch(text, root, ref string, briefRefs []string) bool {
	for _, brief := range briefRefs {
		if ref == brief {
			return nativeS9ResponsibilityMatch(text, root, nativeS9RegisteredResponsibilityRelative(root, ref))
		}
	}
	return nativeS9RegisteredResponsibilityMatch(text, root, ref)
}

// The final retains its original absolute-reference contract. Only actual user
// task text is searched with the registered three-spelling responsibility set.
func nativeS9TaskSources(in policy.Input, root string, refs []string, briefRefs ...string) nativeTaskSources {
	sources, _ := nativeTaskSourcesMatchingDiagnostic(in, refs, true, func(task, ref string) bool {
		return nativeS9TaskResponsibilityMatch(task, root, ref, briefRefs)
	})
	return sources
}

// This projection is for responsibility selection and its guards only. Source
// resolution continues to receive the original task/final text via text().
func (s nativeTaskSources) s9ResponsibilityText(root string, refs []string, briefRefs ...string) string {
	text := s.text()
	for _, ref := range refs {
		relative := nativeS9RegisteredResponsibilityRelative(root, ref)
		if nativeS9TaskResponsibilityMatch(s.Task, root, ref, briefRefs) {
			text += "\n" + filepath.Join(root, filepath.FromSlash(relative))
		}
	}
	return text
}
