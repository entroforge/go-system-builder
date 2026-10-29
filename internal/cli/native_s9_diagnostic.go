package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// Display only: neither this file nor its absence grants reception or recovery.
// Original responsibility/source coordinates and exact file observations keep a rejection from being
// projected as the current gap after the answer, source or responsibility changes.
type nativeS9Diagnostic struct {
	Scope           string                   `json:"scope"`
	Runtime         string                   `json:"runtime"`
	Message         string                   `json:"message"`
	Files           map[string]string        `json:"files"`
	Attempt         *nativeS9Attempt         `json:"attempt,omitempty"`
	ConsumerFailure *nativeS9ConsumerFailure `json:"consumer_failure,omitempty"`
}

// One preserved consumer rejection, separate from a later prerequisite attempt.
type nativeS9ConsumerFailure struct {
	Message string            `json:"message"`
	Files   map[string]string `json:"files"`
	Attempt *nativeS9Attempt  `json:"attempt"`
}

func (d nativeS9Diagnostic) historicalMessage() string {
	if d.Message == "" && d.ConsumerFailure == nil {
		return ""
	}
	message := "Original reception attempt (observed then, not revalidated): " + d.Message
	if d.ConsumerFailure != nil {
		message += " Preserved original consumer failure (observed then, not revalidated): " + d.ConsumerFailure.Message
	}
	return message
}

func nativeS9DiagnosticPath(key string) string {
	return ".claude/diagnostics/native-s9/" + workpacket.Digest([]byte(key)) + ".json"
}

func nativeS9FileObservation(path string) string {
	data, err := workpacket.ReadFile(filepath.Dir(path), filepath.Base(path), 20<<20)
	if os.IsNotExist(err) {
		return "missing"
	}
	if err != nil {
		return "unreadable: " + err.Error()
	}
	return workpacket.Digest(data)
}

func recordNativeS9Diagnostic(root string, snap runtime.Snapshot, key, owner string, paths []string, failure error) {
	recordNativeS9DiagnosticContext(context.Background(), root, snap, key, owner, paths, failure)
}

func recordNativeS9DiagnosticContext(ctx context.Context, root string, snap runtime.Snapshot, key, owner string, paths []string, failure error) {
	if ctx.Err() != nil {
		return
	}
	kind := "Builder"
	if filepath.IsAbs(key) {
		kind = "Targeted"
	}
	capture := newNativeS9AttemptCapture(ctx, root, snap, key, owner, kind)
	if failure == nil {
		_ = capture.Publish(ctx, nil)
		return
	}
	for _, path := range paths {
		if ctx.Err() != nil {
			return
		}
		capture.value.Files[path] = nativeS9FileObservation(path)
	}
	_ = capture.Publish(ctx, failure)
}

func nativeS9ReturnDiagnostic(root string, snap runtime.Snapshot, key string) string {
	return nativeS9ReturnDiagnosticContext(context.Background(), root, snap, key)
}

func nativeS9ReturnDiagnosticContext(ctx context.Context, root string, snap runtime.Snapshot, key string) string {
	value, ok := nativeS9ReturnDiagnosticRecordContext(ctx, root, snap, key)
	if !ok {
		return ""
	}
	return value.historicalMessage()
}

func nativeS9ReturnDiagnosticRecordContext(ctx context.Context, root string, snap runtime.Snapshot, key string) (nativeS9Diagnostic, bool) {
	if ctx.Err() != nil {
		return nativeS9Diagnostic{}, false
	}
	// Builder Stop records under its original PlanReport, including failures
	// after the Result packet was issued. A Result ID is not a new attempt.
	key, keyErr := nativeS9PlanDiagnosticKey(snap.State, key)
	if keyErr != nil || key == "" || ctx.Err() != nil {
		return nativeS9Diagnostic{}, false
	}
	data, err := workpacket.ReadFile(root, nativeS9DiagnosticPath(key), 64<<10)
	if err != nil {
		return nativeS9Diagnostic{}, false
	}
	var value nativeS9Diagnostic
	if json.Unmarshal(data, &value) != nil || value.Scope == "" || value.Runtime != stringValue(snap.State["runtime_id"]) {
		return nativeS9Diagnostic{}, false
	}
	scope := nativeS9DiagnosticScopeContext(ctx, root, snap.State, key)
	if value.Scope != scope && (value.Attempt != nil || value.Scope != nativeS9LegacyDiagnosticScopeContext(ctx, root, snap.State, key)) {
		return nativeS9Diagnostic{}, false
	}
	if ctx.Err() != nil {
		return nativeS9Diagnostic{}, false
	}
	return value, true
}

// Compare recorded identities, not the global write counter: receiving another
// Builder's work must not erase this responsibility's unanswered Stop. These
// are display coordinates only; source validation stays with the consumer.
func nativeS9DiagnosticScope(root string, state map[string]any, key string) string {
	return nativeS9DiagnosticScopeContext(context.Background(), root, state, key)
}

func nativeS9LegacyDiagnosticScopeContext(ctx context.Context, root string, state map[string]any, key string) string {
	if ctx.Err() != nil {
		return ""
	}
	pointer := repairPointerCLI(state)
	source := map[string]any{}
	for _, field := range []string{"case_id", "case_ref", "case_sha256", "contract_id", "contract_ref", "contract_sha256", "session_id", "session_ref", "session_sha256", "plan_id", "plan_ref", "plan_sha256"} {
		source[field] = pointer[field]
	}
	var responsibility any
	packet, found, err := runtime.FindRepairPlanReportPacketContext(runtime.WithPacketBodyRoot(ctx, root), state, key)
	if err != nil {
		return ""
	}
	if found {
		b := packet.Binding
		if b.RuntimeID != state["runtime_id"] || b.BaselineGeneration != workspace.Generation(state) || b.SessionID != pointer["session_id"] || b.PlanRef != pointer["plan_ref"] || stringMapCLI(pointer["assignment_owners"])[b.RepairAssignmentID] != b.OwnerAgentID {
			return ""
		}
		responsibility = packet
	} else {
		// A Targeted answer path is determined by the original source coordinates
		// and one recorded assignment. Resolve it without re-reading Plan/Impact or
		// rerunning the independent-verification source closure.
		var chosen []string
		for assignment, owner := range stringMapCLI(pointer["assignment_owners"]) {
			if path, err := repairTargetedQuestionnairePath(root, state, assignment); err == nil && path == key {
				if chosen != nil {
					return ""
				}
				chosen = []string{assignment, owner}
			}
		}
		if chosen == nil {
			return ""
		}
		responsibility = chosen
		source["impact_ref"], source["impact_sha256"] = pointer["impact_ref"], pointer["impact_sha256"]
	}
	encoded, err := json.Marshal([]any{state["runtime_id"], workspace.Generation(state), lifecycleState(state), nestedStateValue(state, "review", "round"), source, responsibility})
	if err != nil {
		return ""
	}
	return workpacket.Digest(encoded)
}

// Resolve only existing index bindings. Do not load a packet body or infer an
// owner from the platform actor, and do not select an arbitrary same-owner Plan.
func nativeS9PlanDiagnosticKey(state map[string]any, key string) (string, error) {
	rows := objectSliceAny(mapFieldCLI(state, "entities")["work_packets"])
	var sources *workpacket.RepairPlanReportBinding
	for _, row := range rows {
		if row["template_id"] != "s9-repair-result-v1" || row["packet_id"] != key {
			continue
		}
		var binding workpacket.RepairResultBinding
		data, err := json.Marshal(row["binding"])
		if err != nil {
			return "", err
		}
		if err := json.Unmarshal(data, &binding); err != nil {
			return "", err
		}
		copy := binding.Sources
		sources = &copy
	}
	if sources == nil {
		return key, nil
	}
	selected := ""
	for _, row := range rows {
		if row["template_id"] != "s9-plan-report-v1" {
			continue
		}
		var binding workpacket.RepairPlanReportBinding
		data, err := json.Marshal(row["binding"])
		if err != nil {
			return "", err
		}
		if err := json.Unmarshal(data, &binding); err != nil {
			return "", err
		}
		if binding != *sources {
			continue
		}
		if selected != "" {
			return "", fmt.Errorf("Result has multiple original PlanReport diagnostic sources")
		}
		selected = stringValue(row["packet_id"])
	}
	if selected == "" {
		return "", fmt.Errorf("Result original PlanReport diagnostic source is unavailable")
	}
	return selected, nil
}
