package assignment

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
)

// NativeOwnedCheckReceipt is the original Runtime's append-only receipt index
// for a native WorkerCheck. It wraps the process-captured ToolReceipt with the
// frozen Assignment Record, fixed native context, and actual source coordinates
// that selected the runner. It is machine evidence only; it is not a PLAN,
// professional Result, or PASS conclusion.
type NativeOwnedCheckReceipt struct {
	CheckIndex              int                              `json:"check_index"`
	Command                 string                           `json:"command"`
	ProfileDigest           string                           `json:"profile_digest"`
	Record                  assignmentrecord.RecordReference `json:"record"`
	RecordSHA256            string                           `json:"record_sha256"`
	RuntimeID               string                           `json:"runtime_id"`
	BaselineGeneration      int                              `json:"baseline_generation"`
	AssignmentID            string                           `json:"assignment_id"`
	OwnerAgentID            string                           `json:"owner_agent_id"`
	ExecutionGeneration     int                              `json:"execution_generation"`
	NativeContextGeneration int                              `json:"native_context_generation"`
	NativeContextRef        string                           `json:"native_context_ref"`
	NativeContextSHA256     string                           `json:"native_context_sha256"`
	ActivationRef           string                           `json:"activation_ref"`
	ActivationSHA256        string                           `json:"activation_sha256"`
	Checkout                string                           `json:"checkout"`
	SourceBranch            string                           `json:"source_branch"`
	TargetBranch            string                           `json:"target_branch"`
	SourceHead              string                           `json:"source_head"`
	SourceSnapshotSHA256    string                           `json:"source_snapshot_sha256"`
	ToolReceipt             checkexec.ToolReceipt            `json:"tool_receipt"`
}

// ValidateNativeOwnedCheckReceipt checks the local Runtime index against the
// immutable Record and the captured receipt identity. Byte-level seal,
// observation, and adjacent-log validation remains the responsibility of the
// original Result consumer after it resolves those exact captured references.
func ValidateNativeOwnedCheckReceipt(record assignmentrecord.Record, contextGeneration int, contextRef, contextSHA256, activationRef, activationSHA256 string, source NativeCheckSource, receipt NativeOwnedCheckReceipt) error {
	if err := assignmentrecord.ValidateRecord(record); err != nil {
		return err
	}
	if record.ClosingContract == nil || receipt.CheckIndex < 0 || receipt.CheckIndex >= len(record.ClosingContract.WorkerChecks) {
		return fmt.Errorf("native check index is outside the frozen WorkerCheck contract")
	}
	check := record.ClosingContract.WorkerChecks[receipt.CheckIndex]
	sha, err := record.Digest()
	if err != nil {
		return err
	}
	if receipt.Record != record.Reference() || receipt.RecordSHA256 != sha || receipt.RuntimeID != record.RuntimeID || receipt.BaselineGeneration != record.BaselineGeneration || receipt.AssignmentID != record.AssignmentID || receipt.OwnerAgentID != record.OwnerAgentID || receipt.ExecutionGeneration != 0 {
		return fmt.Errorf("native check receipt differs from the frozen Assignment Record identity")
	}
	if receipt.NativeContextGeneration != contextGeneration || receipt.NativeContextRef != contextRef || receipt.NativeContextSHA256 != contextSHA256 || receipt.NativeContextGeneration < 1 || !nativeReceiptRef(receipt.NativeContextRef) || !nativeReceiptSHA(receipt.NativeContextSHA256) {
		return fmt.Errorf("native check receipt differs from the currently fixed context")
	}
	if receipt.ActivationRef != activationRef || receipt.ActivationSHA256 != activationSHA256 || !nativeReceiptRef(receipt.ActivationRef) || !nativeReceiptSHA(receipt.ActivationSHA256) {
		return fmt.Errorf("native check receipt differs from the currently validated PLAN activation")
	}
	if receipt.Command != check.Command || receipt.ProfileDigest != check.Profile.Digest() || receipt.Checkout != source.Checkout || receipt.SourceBranch != source.Branch || receipt.TargetBranch != source.TargetBranch || receipt.SourceHead != source.Head || !nativeReceiptSHA(receipt.SourceSnapshotSHA256) {
		return fmt.Errorf("native check receipt differs from the frozen WorkerCheck or current source")
	}
	tool := receipt.ToolReceipt
	wantBinding := checkexec.Binding{RuntimeID: record.RuntimeID, Generation: record.BaselineGeneration, AssignmentID: record.AssignmentID, Owner: record.OwnerAgentID, ExecutionGeneration: 0, Responsibility: "owned_check"}
	if tool.Binding != wantBinding || tool.Command != check.Command || tool.ProfileDigest != check.Profile.Digest() || tool.Checkout != source.Checkout || !filepath.IsAbs(tool.Path) || filepath.Clean(tool.Path) != tool.Path || !nativeReceiptSHA(tool.SHA256) || !nativeReceiptSHA(tool.LogSHA256) {
		return fmt.Errorf("captured native ToolReceipt differs from the original WorkerCheck binding")
	}
	return nil
}

func nativeReceiptRef(value string) bool {
	return value != "" && !filepath.IsAbs(value) && filepath.IsLocal(filepath.FromSlash(value)) && filepath.ToSlash(filepath.Clean(filepath.FromSlash(value))) == value && !strings.ContainsAny(value, "\\\x00\r\n")
}

func nativeReceiptSHA(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
