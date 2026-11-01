package assignmentresult

import (
	"fmt"
	"path"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
)

// WorkerCheckFact is machine evidence captured by the original Worker check
// receiver. It is never decoded from the professional Result answer. The
// durable artifacts are byte-for-byte copies of the original captured v2
// receipt and adjacent output log.
type WorkerCheckFact struct {
	Index               int                              `json:"index"`
	Command             string                           `json:"command"`
	Status              string                           `json:"status"`
	ExecutionKind       string                           `json:"execution_kind"`
	Record              assignmentrecord.RecordReference `json:"assignment_record"`
	RuntimeID           string                           `json:"runtime_id"`
	BaselineGeneration  int                              `json:"baseline_generation"`
	OwnerAgentID        string                           `json:"owner_agent_id"`
	ExecutionGeneration int                              `json:"execution_generation"`
	ContextGeneration   int                              `json:"context_generation,omitempty"`
	ContextRef          string                           `json:"context_ref,omitempty"`
	ActivationRef       string                           `json:"activation_ref,omitempty"`
	Checkout            string                           `json:"checkout,omitempty"`
	SourceBranch        string                           `json:"source_branch,omitempty"`
	TargetBranch        string                           `json:"target_branch,omitempty"`
	Observation         *checkexec.Observation           `json:"observation,omitempty"`
	OriginalReceipt     *checkexec.ToolReceipt           `json:"original_receipt,omitempty"`
	ReceiptArtifact     *ResolvedEvidence                `json:"receipt_artifact,omitempty"`
	LogArtifact         *ResolvedEvidence                `json:"log_artifact,omitempty"`
}

// WorkerCheckEvidenceArtifactPath returns the immutable per-Result path used
// for a captured check artifact. kind is "receipt" or "log". The Result ID
// separates retries without certifying artifact contents.
func WorkerCheckEvidenceArtifactPath(record assignmentrecord.Record, resultID string, index int, kind string) string {
	if resultID == "" || resultID == "." || resultID == ".." || strings.ContainsAny(resultID, "/\\\x00\r\n") || index < 0 || (kind != "receipt" && kind != "log") {
		return ""
	}
	return path.Join(workerOwnerArtifactPrefix(record), resultID+"-worker-checks", fmt.Sprintf("check-%d-%s.bin", index, kind))
}

// WorkerCheckEvidenceRef is the stable logical reference paired with a
// durable Worker check artifact.
func WorkerCheckEvidenceRef(index int, kind string) string {
	if index < 0 || (kind != "receipt" && kind != "log") {
		return ""
	}
	return fmt.Sprintf("worker-check:%d:%s", index, kind)
}

func workerOwnerArtifactPrefix(record assignmentrecord.Record) string {
	return fmt.Sprintf(".claude/evidence/%s/g%d/assignments/%s/", record.RuntimeID, record.BaselineGeneration, record.OwnerAgentID)
}

// ValidateWorkerClosure checks only supplied original responsibilities and
// check conclusions. Missing check material stays unknown and is not a new
// condition on receiving the professional answer.
func ValidateWorkerClosure(record assignmentrecord.Record, result AcceptedResult, read func(string) ([]byte, error)) error {
	if err := assignmentrecord.ValidateRecord(record); err != nil {
		return err
	}
	seen := make(map[int]bool)
	for _, fact := range result.WorkerCheckFacts {
		if record.ClosingContract == nil || fact.Index < 0 || fact.Index >= len(record.ClosingContract.WorkerChecks) || seen[fact.Index] {
			return fmt.Errorf("Worker check does not identify an original responsibility")
		}
		seen[fact.Index] = true
		original := record.ClosingContract.WorkerChecks[fact.Index]
		if fact.Command != original.Command || fact.Record != record.Reference() || fact.RuntimeID != record.RuntimeID || fact.BaselineGeneration != record.BaselineGeneration || fact.OwnerAgentID != record.OwnerAgentID {
			return fmt.Errorf("Worker check %d differs from its original responsibility", fact.Index)
		}
		switch fact.Status {
		case "pass", "fail", "missing", "stale", "unavailable":
		default:
			return fmt.Errorf("Worker check %d has an unknown status", fact.Index)
		}
	}
	return nil
}

// WorkerClosureComplete is a metadata-only predicate. Callers must first
// load/validate the original accepted Result with LoadAccepted; this function
// does not read durable bytes or establish runner provenance itself.
func WorkerClosureComplete(record assignmentrecord.Record, result AcceptedResult) bool {
	if record.ClosingContract == nil {
		return false
	}
	if len(result.WorkerCheckFacts) != len(record.ClosingContract.WorkerChecks) {
		return false
	}
	for i, fact := range result.WorkerCheckFacts {
		check := record.ClosingContract.WorkerChecks[i]
		if fact.Status != "pass" || fact.Index != i || fact.Command != check.Command ||
			fact.Record != record.Reference() || fact.RuntimeID != record.RuntimeID ||
			fact.BaselineGeneration != record.BaselineGeneration || fact.OwnerAgentID != record.OwnerAgentID {
			return false
		}
	}
	return true
}
