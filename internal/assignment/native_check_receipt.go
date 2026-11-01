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
	Record                  assignmentrecord.RecordReference `json:"record"`
	RuntimeID               string                           `json:"runtime_id"`
	BaselineGeneration      int                              `json:"baseline_generation"`
	AssignmentID            string                           `json:"assignment_id"`
	OwnerAgentID            string                           `json:"owner_agent_id"`
	ExecutionGeneration     int                              `json:"execution_generation"`
	NativeContextGeneration int                              `json:"native_context_generation"`
	NativeContextRef        string                           `json:"native_context_ref"`
	ActivationRef           string                           `json:"activation_ref,omitempty"`
	Checkout                string                           `json:"checkout"`
	SourceBranch            string                           `json:"source_branch"`
	TargetBranch            string                           `json:"target_branch"`
	ToolReceipt             checkexec.ToolReceipt            `json:"tool_receipt"`
}

// ValidateNativeOwnedCheckReceipt links the recorded check to the original
// responsibility, command, context and captured receipt location. Historical
// digest fields and Git OIDs do not authenticate those facts.
func ValidateNativeOwnedCheckReceipt(record assignmentrecord.Record, contextGeneration int, contextRef, activationRef string, source NativeCheckSource, receipt NativeOwnedCheckReceipt) error {
	if err := assignmentrecord.ValidateRecord(record); err != nil {
		return err
	}
	if record.ClosingContract == nil || receipt.CheckIndex < 0 || receipt.CheckIndex >= len(record.ClosingContract.WorkerChecks) {
		return fmt.Errorf("native check index is outside the frozen WorkerCheck contract")
	}
	check := record.ClosingContract.WorkerChecks[receipt.CheckIndex]
	if receipt.Record != record.Reference() || receipt.RuntimeID != record.RuntimeID || receipt.BaselineGeneration != record.BaselineGeneration || receipt.AssignmentID != record.AssignmentID || receipt.OwnerAgentID != record.OwnerAgentID || receipt.ExecutionGeneration != 0 {
		return fmt.Errorf("native check receipt differs from the original Assignment identity")
	}
	if receipt.NativeContextGeneration != contextGeneration || receipt.NativeContextRef != contextRef || receipt.NativeContextGeneration < 1 || !nativeReceiptRef(receipt.NativeContextRef) {
		return fmt.Errorf("native check receipt differs from the currently fixed context")
	}
	if receipt.ActivationRef != activationRef || (receipt.ActivationRef != "" && !nativeReceiptRef(receipt.ActivationRef)) {
		return fmt.Errorf("native check receipt differs from the currently validated PLAN activation")
	}
	if receipt.Command != check.Command || receipt.Checkout != source.Checkout || receipt.SourceBranch != source.Branch || receipt.TargetBranch != source.TargetBranch {
		return fmt.Errorf("native check receipt differs from the frozen WorkerCheck or current source")
	}
	tool := receipt.ToolReceipt
	wantBinding := checkexec.Binding{RuntimeID: record.RuntimeID, Generation: record.BaselineGeneration, AssignmentID: record.AssignmentID, Owner: record.OwnerAgentID, ExecutionGeneration: 0, Responsibility: "owned_check"}
	if tool.Binding != wantBinding || tool.Command != check.Command || tool.Checkout != source.Checkout || !filepath.IsAbs(tool.Path) || filepath.Clean(tool.Path) != tool.Path {
		return fmt.Errorf("captured native ToolReceipt differs from the original WorkerCheck binding")
	}
	return nil
}

func nativeReceiptRef(value string) bool {
	return value != "" && !filepath.IsAbs(value) && filepath.IsLocal(filepath.FromSlash(value)) && filepath.ToSlash(filepath.Clean(filepath.FromSlash(value))) == value && !strings.ContainsAny(value, "\\\x00\r\n")
}
