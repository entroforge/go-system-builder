package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
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
	// Encode the existing responsibility reversibly; split long IDs to keep
	// filesystem components bounded without truncating identity.
	encoded := base64.RawURLEncoding.EncodeToString([]byte(key))
	parts := []string{".claude", "diagnostics", "native-s9", "responsibility"}
	for len(encoded) > 120 {
		parts = append(parts, encoded[:120])
		encoded = encoded[120:]
	}
	if encoded == "" {
		encoded = "_"
	}
	parts = append(parts, encoded, "observation.json")
	return filepath.ToSlash(filepath.Join(parts...))
}

func nativeS9FileObservation(path string) string {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return "missing"
	}
	if err != nil {
		return "unreadable: " + err.Error()
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil {
		return "unreadable: " + err.Error()
	} else if !info.Mode().IsRegular() {
		return "not a regular file"
	}
	return "readable"
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
	if value.Scope != scope {
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
		if !sameNativeS9PlanResponsibility(binding, *sources) {
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

// sameNativeS9PlanResponsibility uses original responsibility and source
// coordinates; historical SHA fields are not proof or matching criteria.
func sameNativeS9PlanResponsibility(a, b workpacket.RepairPlanReportBinding) bool {
	return a.RuntimeID == b.RuntimeID && a.BaselineGeneration == b.BaselineGeneration &&
		a.CaseID == b.CaseID && a.CaseRef == b.CaseRef && a.ContractID == b.ContractID && a.ContractRef == b.ContractRef &&
		a.SessionID == b.SessionID && a.SessionRef == b.SessionRef && a.PlanID == b.PlanID && a.PlanRef == b.PlanRef &&
		a.RepairAssignmentID == b.RepairAssignmentID && a.ManifestAssignmentID == b.ManifestAssignmentID &&
		a.TeamID == b.TeamID && a.TaskID == b.TaskID && a.TaskRef == b.TaskRef && a.ManifestRef == b.ManifestRef && a.OwnerAgentID == b.OwnerAgentID
}
