package workspace

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentinputs"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
)

// ValidateAssignmentRecord checks this execution's original registration.
// Historical executions retain their historical binding for recovery.
func ValidateAssignmentRecord(state map[string]any, e Execution) error {
	if e.RuntimeID != RuntimeID(state) || e.BaselineGeneration != Generation(state) {
		return nil
	}
	record, canonical, err := assignmentrecord.ForOwner(state, e.AgentID)
	if err != nil {
		return err
	}
	if !canonical {
		if e.AssignmentRecord != nil || e.AssignmentRecordSHA256 != "" {
			return fmt.Errorf("execution has no original canonical Assignment registration")
		}
		return nil
	}
	if record.ClosingContract != nil {
		if record.IntegrationCheckMode != "post_merge" {
			return fmt.Errorf("canonical closing Assignment must preserve post_merge integration checks")
		}
		for _, profile := range record.CheckProfiles {
			if profile.Location != "main" {
				return fmt.Errorf("canonical closing Assignment required-check profiles must remain Main-owned")
			}
		}
		if e.CheckLocation != "main" {
			return fmt.Errorf("canonical closing Assignment execution must use Main for required checks")
		}
	}
	expectedWorkerChecks := []assignmentrecord.WorkerCheck(nil)
	if record.ClosingContract != nil {
		expectedWorkerChecks = record.ClosingContract.WorkerChecks
	}
	if !sameWorkerChecks(e.WorkerChecks, expectedWorkerChecks) {
		return fmt.Errorf("execution WorkerChecks differ from the current canonical Assignment closing contract")
	}
	if record.ClosingContract == nil && len(e.OwnedCheckReceipts) != 0 {
		return fmt.Errorf("legacy Assignment execution cannot contain owner-check receipts")
	}
	for _, row := range e.OwnedCheckReceipts {
		if row.WorkerCheckIndex < 0 || row.WorkerCheckIndex >= len(expectedWorkerChecks) {
			return fmt.Errorf("execution owned-check receipt index is outside the frozen WorkerChecks")
		}
		if row.Receipt.Binding != CheckBinding(e) || row.Receipt.Command != expectedWorkerChecks[row.WorkerCheckIndex].Command || row.Receipt.ProfileDigest != expectedWorkerChecks[row.WorkerCheckIndex].Profile.Digest() || row.Receipt.Path == "" || row.Receipt.SHA256 == "" {
			return fmt.Errorf("execution owned-check receipt differs from its frozen WorkerCheck")
		}
	}
	sha, err := record.Digest()
	if err != nil {
		return err
	}
	if e.AssignmentRecord == nil || *e.AssignmentRecord != record.Reference() || e.AssignmentRecordSHA256 != sha || e.AssignmentID != record.AssignmentID || !reflect.DeepEqual(e.WritePaths, record.EffectiveWritePaths) || !reflect.DeepEqual(e.Checks, record.RequiredChecks) {
		return fmt.Errorf("execution differs from the current canonical Assignment")
	}
	if len(e.CheckProfiles) != len(record.CheckProfiles) {
		return fmt.Errorf("execution CheckProfiles differ from the canonical Assignment")
	}
	for i, profile := range record.CheckProfiles {
		if e.CheckProfiles[i].Digest() != profile.Digest() {
			return fmt.Errorf("execution CheckProfiles differ from the canonical Assignment")
		}
	}
	for _, source := range record.ContextSources {
		if strings.HasPrefix(source.Kind, "embedded_") {
			data, err := assignmentinputs.Read("", source.Ref)
			if err != nil || checkexec.Hash(data) != source.SHA256 {
				return fmt.Errorf("execution embedded Assignment source changed: %s", source.Ref)
			}
			continue
		}
		if source.Kind == "check_profile" {
			continue
		}
		if e.Inputs[source.Ref] != source.SHA256 {
			return fmt.Errorf("execution lacks canonical Assignment source %s", source.Ref)
		}
	}
	return nil
}

func (b *ExecutionRegistry) bindAssignmentRecord(state map[string]any, e Execution) (Execution, error) {
	record, canonical, err := assignmentrecord.ForOwner(state, e.AgentID)
	if err != nil || !canonical {
		return e, err
	}
	if e.AssignmentID != record.AssignmentID || !reflect.DeepEqual(e.WritePaths, record.EffectiveWritePaths) || !reflect.DeepEqual(e.Checks, record.RequiredChecks) {
		return e, fmt.Errorf("execution scope/checks do not match the canonical Assignment")
	}
	if err := assignmentrecord.VerifySources(record, func(ref string) ([]byte, error) {
		return assignmentinputs.Read(b.MainRoot, ref)
	}); err != nil {
		return e, err
	}
	ref := record.Reference()
	e.AssignmentRecord = &ref
	e.WorkerChecks = copyWorkerChecks(nil)
	if record.ClosingContract != nil {
		e.WorkerChecks = copyWorkerChecks(record.ClosingContract.WorkerChecks)
	}
	if record.ClosingContract == nil && (len(e.OwnedCheckReceipts) != 0 || len(e.WorkerChecks) != 0) {
		return e, fmt.Errorf("legacy Assignment cannot acquire owner-run checks")
	}
	if record.IntegrationCheckMode == "post_merge" {
		// The original Record binds post-merge integration checks to Main. This
		// never changes a legacy Record with no explicit mode.
		e.CheckLocation = "main"
	}
	e.AssignmentRecordSHA256, err = record.Digest()
	if err != nil {
		return e, err
	}
	for _, source := range record.ContextSources {
		if strings.Contains(source.Ref, "#") || strings.HasPrefix(source.Kind, "embedded_") {
			continue
		}
		e.Inputs[source.Ref] = source.SHA256
	}
	// The original CLI freezes the declared CheckProfiles immediately after
	// Plan and before its Writer commit. ValidateAuthority checks that final
	// projection, including profile digests, when the candidate is persisted.
	return e, nil
}

func sameWorkerChecks(a, b []assignmentrecord.WorkerCheck) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Index != b[i].Index || a[i].Command != b[i].Command || a[i].Profile.Digest() != b[i].Profile.Digest() {
			return false
		}
	}
	return true
}

func copyWorkerChecks(source []assignmentrecord.WorkerCheck) []assignmentrecord.WorkerCheck {
	out := make([]assignmentrecord.WorkerCheck, len(source))
	copy(out, source)
	for i := range out {
		profile := source[i].Profile
		profile.Environment = append([]checkexec.EnvFact{}, profile.Environment...)
		profile.Dependencies = append([]checkexec.FileRef{}, profile.Dependencies...)
		profile.Fixtures = append([]checkexec.FileRef{}, profile.Fixtures...)
		out[i].Profile = profile
	}
	return out
}
