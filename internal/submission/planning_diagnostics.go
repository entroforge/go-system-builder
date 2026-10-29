package submission

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// Planning diagnostics describe the last attempted outlet, never its authority.
// Scope excludes Runtime revision: unrelated writes must not erase the cause.
// A new entry cannot inherit an old failure. Reads never rerun a source closure.
func planningDiagnosticPath(state map[string]any, kind string) string {
	life, _ := state["lifecycle"].(map[string]any)
	baseline, _ := state["baseline"].(map[string]any)
	scope, _ := json.Marshal([]any{state["runtime_id"], baseline["generation"], life["state"], life["phase"], life["phase_revision"], kind})
	return ".claude/diagnostics/planning/" + workpacket.Digest(scope) + ".json"
}

// RecordPlanningDiagnostic is best-effort display storage. Failure cannot turn
// an accepted answer or committed transition into a failed business result.
func RecordPlanningDiagnostic(root string, state map[string]any, kind, message string) {
	parent, name, err := workpacket.OpenParent(root, planningDiagnosticPath(state, kind), message != "")
	if err != nil {
		return
	}
	defer parent.Close()
	if message == "" {
		_ = parent.Remove(name)
		return
	}
	data, err := json.Marshal(struct {
		Message string `json:"message"`
		At      string `json:"at"`
	}{message, time.Now().UTC().Format(time.RFC3339Nano)})
	if err != nil || len(data) > 64<<10 {
		return
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return
	}
	tmp := fmt.Sprintf(".diagnostic-%x.tmp", random)
	f, err := parent.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer parent.Remove(tmp)
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr == nil && closeErr == nil {
		_ = parent.Rename(tmp, name)
	}
}

// PlanningDiagnostic returns only a bounded last-attempt diagnostic. Missing or
// damaged display storage is ignored; receipts and gates remain authoritative.
func PlanningDiagnostic(root string, state map[string]any, kind string) string {
	return PlanningDiagnosticContext(context.Background(), root, state, kind)
}

func PlanningDiagnosticContext(ctx context.Context, root string, state map[string]any, kind string) string {
	if ctx.Err() != nil {
		return ""
	}
	data, err := workpacket.ReadFile(root, planningDiagnosticPath(state, kind), 64<<10)
	if err != nil {
		return ""
	}
	var diagnostic struct {
		Message string `json:"message"`
	}
	if ctx.Err() != nil || json.Unmarshal(data, &diagnostic) != nil || diagnostic.Message == "" {
		return ""
	}
	return " Last " + kind + " attempt: " + diagnostic.Message
}

// PlanningRecoveryCommand names the existing Main technical recovery entry.
// It never invents a new packet or asks the author to copy identity fields.
func PlanningRecoveryCommand(root, action string) string {
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'" }
	return quote(filepath.Join(root, ".claude/bin/loop-harness")) + " runtime stage-answer " + action + " --root " + quote(root) + " --agent Main"
}

// Keep the field, reason and actionable repair together in every delivery.
func planningFieldDiagnostic(field workpacket.FieldError) string {
	bounded := func(value string) string {
		if len(value) > 2048 {
			value = strings.ToValidUTF8(value[:2048], "") + " [truncated]"
		}
		return value
	}
	return bounded(field.Pointer) + ": expected " + bounded(field.Expected) + "; observed " + bounded(field.Observed) + "; repair: " + bounded(field.Repair)
}

// Professional changes use the named rework route; recovery retains accepted content.
func PlanningProfessionalReworkGuidance(root string) string {
	return " For a substantive change to frozen S2 design, Main runs " + PlanningRecoveryCommand(root, "rework") + " --target design --reason '<author-written concrete design correction>'; for a substantive correction to already accepted contracts with S2 unchanged, Main runs " + PlanningRecoveryCommand(root, "rework") + " --target contracts --reason '<author-written concrete contract correction>'. Replace the reason placeholder with the professional cause; do not copy machine identity fields. Only while still in S4, a professional correction to already accepted TASKs or the dispatch index with REQ/S2/S3 unchanged uses " + PlanningRecoveryCommand(root, "rework") + " --target tasks --reason '<author-written concrete planning correction>'. After entering S5 use its original review and document-rework responsibility. Unreceived outputs can be edited normally. These are professional rework entries; recover only continues the original system outlet."
}

func planningAnswerPathValues(root string, paths []string, field, indexPrefix string) string {
	values := map[string]any{field: []string{}}
	for _, path := range paths {
		if root != "" {
			relative, err := filepath.Rel(root, path)
			if err != nil {
				continue
			}
			path = filepath.ToSlash(relative)
		}
		if strings.HasPrefix(filepath.Base(path), indexPrefix) {
			values["index_path"] = path
		} else {
			values[field] = append(values[field].([]string), path)
		}
	}
	encoded, _ := json.Marshal(values)
	return " Copyable safe draft answer path fields (complete other answers separately): " + string(encoded) + "."
}
