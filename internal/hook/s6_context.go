package hook

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/missingtokens"
	"github.com/entroforge/go-system-builder/internal/policy"
)

// PreToolUseS6Context presents a native responsibility once, alongside its
// original gate observation. Errors and committed transitions keep the full
// recovery renderer. This changes presentation, never gate or tool decisions.
func PreToolUseS6Context(decision policy.Decision, result controller.ControlResult) ([]byte, int, error) {
	qg := normalizeQualityGate(result.QualityGate)
	g := decision.Guidance
	if g == nil || g.Stage != "S6" || decision.Decision != "allow" ||
		decision.AdditionalContext == "" || result.Error != "" || qg.TransitionCommitted ||
		(qg.Status != controller.StatusNotReady && qg.Status != controller.StatusSatisfied) ||
		len(qg.Conflicts) > 0 || qg.ErrorCode != "" {
		return PreToolUseWithQualityGate(decision, result)
	}
	projection, _ := json.Marshal(qg)
	var body strings.Builder
	fmt.Fprintf(&body, "QUALITY_GATE %s\nLOOP RECOVERY — Stage S6 building.\n%s", projection, decision.AdditionalContext)
	if g.Action != "" && !strings.Contains(decision.AdditionalContext, g.Action) {
		fmt.Fprintf(&body, "\nNext: %s", g.Action)
	}
	if legend := missingtokens.RenderMissingTokenLegend(qg.GateID, qg.Missing); legend != "" {
		fmt.Fprintf(&body, "\n%s", legend)
	}
	if len(g.Questions) > 0 {
		fmt.Fprintf(&body, "\nPreflight questions: %s", strings.Join(g.Questions, " | "))
	}
	if len(g.Integration) > 0 {
		fmt.Fprintf(&body, "\nIntegration: %s", strings.Join(g.Integration, "; "))
	}
	if g.Blocked {
		fmt.Fprintf(&body, "\nBlocked: %s", g.Blocker)
	}
	if g.HumanRequired {
		body.WriteString("\nHuman Gateway required; stop automation.")
	}
	fmt.Fprintf(&body, "\nStage contract: %s", g.ProtocolRef)
	payload := map[string]any{
		"hookSpecificOutput": map[string]any{"hookEventName": "PreToolUse", "additionalContext": body.String()},
		"systemMessage":      qualityNotice(decision, qg),
	}
	data, err := json.Marshal(payload)
	return data, 0, err
}

// Revision/fingerprint churn alone is not a new responsibility. Preserve all
// actual missing items, conflicts and evidence in the comparison. The delivered
// projection itself stays intact for existing diagnostic readers.
func responsibilityContextStamp(stage, context string) string {
	if stage != "S6" && stage != "S7" {
		return context
	}
	line, rest, found := strings.Cut(context, "\n")
	if !found || !strings.HasPrefix(line, "QUALITY_GATE ") {
		return context
	}
	var gate map[string]any
	if json.Unmarshal([]byte(strings.TrimPrefix(line, "QUALITY_GATE ")), &gate) != nil {
		return context
	}
	delete(gate, "observed_revision")
	delete(gate, "fingerprint")
	stable, _ := json.Marshal(gate)
	if stage == "S7" {
		rest = noticeRevisionFields.ReplaceAllString(rest, "")
	}
	return string(stable) + "\n" + rest
}
