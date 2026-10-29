package assignmentresult

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
)

const (
	workerReceiptReadLimit int64 = 4 << 20
	workerLogReadLimit     int64 = 20 << 20
)

// WorkerCheckFact is machine evidence captured by the original Worker check
// receiver. It is never decoded from the professional Result answer. The
// durable artifacts are byte-for-byte copies of the original captured v2
// receipt and adjacent output log.
type WorkerCheckFact struct {
	Index                       int                              `json:"index"`
	Command                     string                           `json:"command"`
	ProfileDigest               string                           `json:"profile_digest"`
	Status                      string                           `json:"status"`
	ExecutionKind               string                           `json:"execution_kind"`
	Record                      assignmentrecord.RecordReference `json:"assignment_record"`
	RecordSHA256                string                           `json:"assignment_record_sha256"`
	RuntimeID                   string                           `json:"runtime_id"`
	BaselineGeneration          int                              `json:"baseline_generation"`
	OwnerAgentID                string                           `json:"owner_agent_id"`
	ExecutionGeneration         int                              `json:"execution_generation"`
	ContextGeneration           int                              `json:"context_generation,omitempty"`
	ContextRef                  string                           `json:"context_ref,omitempty"`
	ContextSHA256               string                           `json:"context_sha256,omitempty"`
	ActivationRef               string                           `json:"activation_ref,omitempty"`
	ActivationSHA256            string                           `json:"activation_sha256,omitempty"`
	BootstrapSHA256             string                           `json:"bootstrap_sha256,omitempty"`
	Checkout                    string                           `json:"checkout,omitempty"`
	SourceBranch                string                           `json:"source_branch,omitempty"`
	TargetBranch                string                           `json:"target_branch,omitempty"`
	SourceHead                  string                           `json:"source_head,omitempty"`
	SourceSnapshotSHA256        string                           `json:"source_snapshot_sha256,omitempty"`
	CurrentSourceSnapshotSHA256 string                           `json:"current_source_snapshot_sha256,omitempty"`
	Observation                 *checkexec.Observation           `json:"observation,omitempty"`
	OriginalReceipt             *checkexec.ToolReceipt           `json:"original_receipt,omitempty"`
	ReceiptArtifact             *ResolvedEvidence                `json:"receipt_artifact,omitempty"`
	LogArtifact                 *ResolvedEvidence                `json:"log_artifact,omitempty"`
}

type workerClosureItem struct {
	Index                       int                              `json:"index"`
	Command                     string                           `json:"command"`
	ProfileDigest               string                           `json:"profile_digest"`
	Status                      string                           `json:"status"`
	ExecutionKind               string                           `json:"execution_kind"`
	Record                      assignmentrecord.RecordReference `json:"assignment_record"`
	RecordSHA256                string                           `json:"assignment_record_sha256"`
	RuntimeID                   string                           `json:"runtime_id"`
	BaselineGeneration          int                              `json:"baseline_generation"`
	OwnerAgentID                string                           `json:"owner_agent_id"`
	ExecutionGeneration         int                              `json:"execution_generation"`
	ContextGeneration           int                              `json:"context_generation,omitempty"`
	ContextRef                  string                           `json:"context_ref,omitempty"`
	ContextSHA256               string                           `json:"context_sha256,omitempty"`
	ActivationRef               string                           `json:"activation_ref,omitempty"`
	ActivationSHA256            string                           `json:"activation_sha256,omitempty"`
	BootstrapSHA256             string                           `json:"bootstrap_sha256,omitempty"`
	Checkout                    string                           `json:"checkout,omitempty"`
	SourceBranch                string                           `json:"source_branch,omitempty"`
	TargetBranch                string                           `json:"target_branch,omitempty"`
	SourceHead                  string                           `json:"source_head,omitempty"`
	SourceSnapshotSHA256        string                           `json:"source_snapshot_sha256,omitempty"`
	CurrentSourceSnapshotSHA256 string                           `json:"current_source_snapshot_sha256,omitempty"`
	Observation                 *checkexec.Observation           `json:"observation,omitempty"`
	OriginalReceipt             *checkexec.ToolReceipt           `json:"original_receipt,omitempty"`
	ReceiptRef                  string                           `json:"receipt_ref,omitempty"`
	ReceiptSHA256               string                           `json:"receipt_sha256,omitempty"`
	LogRef                      string                           `json:"log_ref,omitempty"`
	LogSHA256                   string                           `json:"log_sha256,omitempty"`
}

type workerClosureIdentity struct {
	Contract               string                           `json:"contract"`
	AssignmentRecord       assignmentrecord.RecordReference `json:"assignment_record"`
	AssignmentRecordSHA256 string                           `json:"assignment_record_sha256"`
	RuntimeID              string                           `json:"runtime_id"`
	BaselineGeneration     int                              `json:"baseline_generation"`
	OwnerAgentID           string                           `json:"owner_agent_id"`
	Checks                 []workerClosureItem              `json:"checks"`
}

// WorkerCheckEvidenceArtifactPath returns the immutable per-Result path used
// for a captured check artifact. kind is "receipt" or "log". The Result ID
// separates retries while ToolClosureDigest remains independent of this
// packaging coordinate.
func WorkerCheckEvidenceArtifactPath(record assignmentrecord.Record, resultID string, index int, kind, sha string) string {
	if resultID == "" || resultID == "." || resultID == ".." || strings.ContainsAny(resultID, "/\\\x00\r\n") || index < 0 || (kind != "receipt" && kind != "log") || !validSHA256(sha) {
		return ""
	}
	return path.Join(workerOwnerArtifactPrefix(record), resultID+"-worker-checks", fmt.Sprintf("check-%d-%s-%s.bin", index, kind, sha))
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

// WorkerClosureDigest computes the stable identity of all frozen Worker
// checks. Per-Result snapshot paths are excluded; original captured references,
// bytes, status, binding and observations remain part of the identity.
func WorkerClosureDigest(record assignmentrecord.Record, facts []WorkerCheckFact) (string, error) {
	if record.ClosingContract == nil {
		return "", fmt.Errorf("Assignment has no frozen closing contract")
	}
	if err := validateWorkerFactSet(record, facts); err != nil {
		return "", err
	}
	sha, err := record.Digest()
	if err != nil {
		return "", err
	}
	items := make([]workerClosureItem, 0, len(facts))
	for _, fact := range facts {
		item := workerClosureItem{
			Index: fact.Index, Command: fact.Command, ProfileDigest: fact.ProfileDigest,
			Status: fact.Status, ExecutionKind: fact.ExecutionKind, Record: fact.Record,
			RecordSHA256: fact.RecordSHA256, RuntimeID: fact.RuntimeID,
			BaselineGeneration: fact.BaselineGeneration, OwnerAgentID: fact.OwnerAgentID,
			ExecutionGeneration: fact.ExecutionGeneration, ContextGeneration: fact.ContextGeneration,
			ContextRef: fact.ContextRef, ContextSHA256: fact.ContextSHA256,
			ActivationRef: fact.ActivationRef, ActivationSHA256: fact.ActivationSHA256,
			BootstrapSHA256: fact.BootstrapSHA256,
			Checkout:        fact.Checkout,
			SourceBranch:    fact.SourceBranch, TargetBranch: fact.TargetBranch, SourceHead: fact.SourceHead,
			SourceSnapshotSHA256:        fact.SourceSnapshotSHA256,
			CurrentSourceSnapshotSHA256: fact.CurrentSourceSnapshotSHA256,
			Observation:                 fact.Observation,
			OriginalReceipt:             fact.OriginalReceipt,
		}
		if fact.ReceiptArtifact != nil {
			item.ReceiptRef, item.ReceiptSHA256 = fact.ReceiptArtifact.Ref, fact.ReceiptArtifact.SHA256
		}
		if fact.LogArtifact != nil {
			item.LogRef, item.LogSHA256 = fact.LogArtifact.Ref, fact.LogArtifact.SHA256
		}
		items = append(items, item)
	}
	body, err := json.Marshal(workerClosureIdentity{
		Contract: record.ClosingContract.Contract, AssignmentRecord: record.Reference(),
		AssignmentRecordSHA256: sha, RuntimeID: record.RuntimeID,
		BaselineGeneration: record.BaselineGeneration, OwnerAgentID: record.OwnerAgentID,
		Checks: items,
	})
	if err != nil {
		return "", err
	}
	return digestBytes(body), nil
}

// EffectiveToolClosureDigest returns the closure identity carried by a
// Result. Legacy Records remain outside this identity. For an explicit empty
// WorkerChecks contract, an older envelope may omit the digest; its empty
// identity is derived without rewriting the original envelope.
func EffectiveToolClosureDigest(record assignmentrecord.Record, result AcceptedResult) (string, error) {
	if err := assignmentrecord.ValidateRecord(record); err != nil {
		return "", err
	}
	if record.ClosingContract == nil {
		if len(result.WorkerCheckFacts) != 0 || result.ToolClosureDigest != "" {
			return "", fmt.Errorf("legacy Assignment Result cannot claim a Worker tool closure")
		}
		return "", nil
	}
	if len(record.ClosingContract.WorkerChecks) > 0 && len(result.WorkerCheckFacts) == 0 && result.ToolClosureDigest == "" {
		// Result envelopes accepted by the preceding S6 slice predate machine
		// closure fields. Preserve them as submitted-but-unassessed history;
		// this identity cannot be mistaken for a complete or explicit missing
		// fact set and is never rewritten into the original envelope.
		return legacyUnprovenToolClosureDigest(record)
	}
	want, err := WorkerClosureDigest(record, result.WorkerCheckFacts)
	if err != nil {
		return "", err
	}
	if len(record.ClosingContract.WorkerChecks) > 0 && result.ToolClosureDigest == "" {
		return "", fmt.Errorf("non-empty frozen WorkerChecks require a tool_closure_digest")
	}
	if result.ToolClosureDigest != "" && result.ToolClosureDigest != want {
		return "", fmt.Errorf("tool_closure_digest differs from the frozen Worker check facts")
	}
	return want, nil
}

// ValidateWorkerClosure validates Result facts and the durable copies of the
// original captured receipt/log. It never consults the current workspace,
// native checkout, or live runner registry; those are validated at receipt
// collection time by the original writer.
func ValidateWorkerClosure(record assignmentrecord.Record, result AcceptedResult, read func(string) ([]byte, error)) error {
	digest, err := EffectiveToolClosureDigest(record, result)
	if err != nil {
		return err
	}
	if record.ClosingContract == nil || len(record.ClosingContract.WorkerChecks) == 0 {
		return nil
	}
	if len(result.WorkerCheckFacts) == 0 && result.ToolClosureDigest == "" {
		// Legacy accepted Result from before the machine-facts extension.
		return nil
	}
	if result.ToolClosureDigest == "" || result.ToolClosureDigest != digest {
		return fmt.Errorf("non-empty frozen WorkerChecks are not closed by the original Result")
	}
	for _, fact := range result.WorkerCheckFacts {
		if fact.Status == "missing" || fact.Status == "stale" && fact.OriginalReceipt == nil || fact.Status == "unavailable" && fact.OriginalReceipt == nil {
			continue
		}
		if read == nil {
			return fmt.Errorf("durable Worker check evidence reader is required")
		}
		if err := validateWorkerCheckArtifacts(record, result.EvidenceID, fact, read); err != nil {
			return fmt.Errorf("Worker check %d: %w", fact.Index, err)
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
	if _, err := EffectiveToolClosureDigest(record, result); err != nil {
		return false
	}
	if len(result.WorkerCheckFacts) != len(record.ClosingContract.WorkerChecks) {
		return false
	}
	for _, fact := range result.WorkerCheckFacts {
		if fact.Status != "pass" {
			return false
		}
	}
	return true
}

func legacyUnprovenToolClosureDigest(record assignmentrecord.Record) (string, error) {
	sha, err := record.Digest()
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(struct {
		Contract               string                           `json:"contract"`
		AssignmentRecord       assignmentrecord.RecordReference `json:"assignment_record"`
		AssignmentRecordSHA256 string                           `json:"assignment_record_sha256"`
		RuntimeID              string                           `json:"runtime_id"`
		BaselineGeneration     int                              `json:"baseline_generation"`
		OwnerAgentID           string                           `json:"owner_agent_id"`
		State                  string                           `json:"state"`
	}{
		Contract: record.ClosingContract.Contract, AssignmentRecord: record.Reference(),
		AssignmentRecordSHA256: sha, RuntimeID: record.RuntimeID,
		BaselineGeneration: record.BaselineGeneration, OwnerAgentID: record.OwnerAgentID,
		State: "legacy_worker_checks_unproven",
	})
	if err != nil {
		return "", err
	}
	return digestBytes(body), nil
}

func validateWorkerFactSet(record assignmentrecord.Record, facts []WorkerCheckFact) error {
	if record.ClosingContract == nil {
		if len(facts) != 0 {
			return fmt.Errorf("Assignment has no frozen closing contract")
		}
		return nil
	}
	if len(facts) != len(record.ClosingContract.WorkerChecks) {
		return fmt.Errorf("Worker check facts must cover every frozen index exactly once")
	}
	sha, err := record.Digest()
	if err != nil {
		return err
	}
	kind := ""
	for i, fact := range facts {
		check := record.ClosingContract.WorkerChecks[i]
		if fact.Index != i || fact.Index != check.Index || fact.Command != check.Command || fact.ProfileDigest != check.Profile.Digest() {
			return fmt.Errorf("Worker check facts must be sorted and match frozen index, command and profile")
		}
		if fact.Status != "pass" && fact.Status != "fail" && fact.Status != "missing" && fact.Status != "stale" && fact.Status != "unavailable" {
			return fmt.Errorf("Worker check %d has an unknown status", i)
		}
		if fact.Record != record.Reference() || fact.RecordSHA256 != sha || fact.RuntimeID != record.RuntimeID || fact.BaselineGeneration != record.BaselineGeneration || fact.OwnerAgentID != record.OwnerAgentID {
			return fmt.Errorf("Worker check %d differs from the frozen Record, owner or generation", i)
		}
		if fact.ExecutionKind != "workspace" && fact.ExecutionKind != "native" {
			return fmt.Errorf("Worker check %d has no supported original execution kind", i)
		}
		if fact.CurrentSourceSnapshotSHA256 != "" && !validSHA256(fact.CurrentSourceSnapshotSHA256) {
			return fmt.Errorf("Worker check %d has an invalid current source snapshot digest", i)
		}
		if kind == "" {
			kind = fact.ExecutionKind
		} else if kind != fact.ExecutionKind {
			return fmt.Errorf("Worker check facts mix workspace and native execution identities")
		}
		unknownUnavailable := fact.Status == "unavailable" && fact.OriginalReceipt == nil
		if fact.ExecutionKind == "workspace" {
			unknownExecution := fact.ExecutionGeneration == 0 && unknownUnavailable
			if (!unknownExecution && fact.ExecutionGeneration < 1) || fact.ContextGeneration != 0 || fact.ContextRef != "" || fact.ContextSHA256 != "" || fact.ActivationRef != "" || fact.ActivationSHA256 != "" {
				return fmt.Errorf("workspace Worker check %d has an invalid or incomplete execution/context identity", i)
			}
			if fact.OriginalReceipt != nil && !validSHA256(fact.BootstrapSHA256) {
				return fmt.Errorf("workspace Worker check %d receipt requires its captured bootstrap digest", i)
			}
		} else {
			if fact.BootstrapSHA256 != "" {
				return fmt.Errorf("native Worker check %d cannot claim a workspace bootstrap digest", i)
			}
			contextKnown := fact.ContextGeneration >= 1 && validRelative(fact.ContextRef) && validSHA256(fact.ContextSHA256)
			contextUnknown := fact.ContextGeneration == 0 && fact.ContextRef == "" && fact.ContextSHA256 == ""
			activationKnown := validRelative(fact.ActivationRef) && validSHA256(fact.ActivationSHA256)
			activationUnknown := fact.ActivationRef == "" && fact.ActivationSHA256 == ""
			identityWellFormed := (contextKnown || contextUnknown) && (activationKnown || activationUnknown)
			identityComplete := contextKnown && activationKnown
			if fact.ExecutionGeneration != 0 || !identityWellFormed || !identityComplete && !unknownUnavailable {
				return fmt.Errorf("native Worker check %d has no persisted context and activated PLAN identity", i)
			}
		}
		if fact.Status == "pass" || fact.Status == "fail" || (fact.Status == "stale" || fact.Status == "unavailable") && fact.OriginalReceipt != nil {
			if fact.Observation == nil || fact.OriginalReceipt == nil || fact.ReceiptArtifact == nil || fact.LogArtifact == nil || !filepath.IsAbs(fact.Checkout) || filepath.Clean(fact.Checkout) != fact.Checkout || !validSHA256(fact.SourceSnapshotSHA256) {
				return fmt.Errorf("Worker check %d status %s requires captured receipt, log, observation and source snapshot", i, fact.Status)
			}
			if fact.ExecutionKind == "native" && (fact.SourceBranch == "" || fact.TargetBranch == "" || fact.SourceHead == "") {
				return fmt.Errorf("native Worker check %d lacks original source coordinates", i)
			}
		} else if fact.Observation != nil || fact.OriginalReceipt != nil || fact.ReceiptArtifact != nil || fact.LogArtifact != nil || fact.Checkout != "" || fact.SourceSnapshotSHA256 != "" {
			return fmt.Errorf("Worker check %d status %s cannot present partial passing proof", i, fact.Status)
		}
	}
	return nil
}

func validateWorkerCheckArtifacts(record assignmentrecord.Record, resultID string, fact WorkerCheckFact, read func(string) ([]byte, error)) error {
	if fact.OriginalReceipt == nil || fact.Observation == nil || fact.ReceiptArtifact == nil || fact.LogArtifact == nil {
		return fmt.Errorf("captured original proof is incomplete")
	}
	profile := record.ClosingContract.WorkerChecks[fact.Index].Profile
	receipt := fact.OriginalReceipt
	expectedBinding := checkexec.Binding{
		RuntimeID: record.RuntimeID, Generation: record.BaselineGeneration,
		AssignmentID: record.AssignmentID, Owner: record.OwnerAgentID,
		ExecutionGeneration: fact.ExecutionGeneration, Responsibility: "owned_check",
	}
	if receipt.Binding != expectedBinding || receipt.Command != profile.Command || receipt.ProfileDigest != profile.Digest() || receipt.Checkout != fact.Checkout || !filepath.IsAbs(receipt.Path) || filepath.Clean(receipt.Path) != receipt.Path || !validSHA256(receipt.SHA256) || !validSHA256(receipt.LogSHA256) {
		return fmt.Errorf("captured ToolReceipt differs from original owner, execution or frozen profile")
	}
	if fact.ReceiptArtifact.Ref != WorkerCheckEvidenceRef(fact.Index, "receipt") || fact.LogArtifact.Ref != WorkerCheckEvidenceRef(fact.Index, "log") || fact.ReceiptArtifact.SHA256 != receipt.SHA256 || fact.LogArtifact.SHA256 != receipt.LogSHA256 {
		return fmt.Errorf("durable receipt/log references differ from the captured ToolReceipt")
	}
	expectedReceiptPath := WorkerCheckEvidenceArtifactPath(record, resultID, fact.Index, "receipt", receipt.SHA256)
	expectedLogPath := WorkerCheckEvidenceArtifactPath(record, resultID, fact.Index, "log", receipt.LogSHA256)
	if expectedReceiptPath == "" || fact.ReceiptArtifact.Path != expectedReceiptPath || fact.LogArtifact.Path != expectedLogPath {
		return fmt.Errorf("durable receipt/log are outside the exact Result artifact paths")
	}
	receiptBytes, err := read(fact.ReceiptArtifact.Path)
	if err != nil || len(receiptBytes) == 0 || int64(len(receiptBytes)) > workerReceiptReadLimit || checkexec.Hash(receiptBytes) != receipt.SHA256 {
		return fmt.Errorf("durable original receipt bytes are missing, oversized or changed")
	}
	logBytes, err := read(fact.LogArtifact.Path)
	if err != nil || int64(len(logBytes)) > workerLogReadLimit || checkexec.Hash(logBytes) != receipt.LogSHA256 {
		return fmt.Errorf("durable original log bytes are missing, oversized or changed")
	}
	var raw workerReceiptEnvelope
	decoder := json.NewDecoder(bytes.NewReader(receiptBytes))
	if err := decoder.Decode(&raw); err != nil {
		return fmt.Errorf("decode captured v2 receipt: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("captured receipt must contain exactly one JSON object")
	}
	statusMatches := raw.Status == fact.Status || (fact.Status == "stale" || fact.Status == "unavailable") && (raw.Status == "pass" || raw.Status == "fail")
	if raw.Version != 2 || raw.RuntimeID != record.RuntimeID || raw.BaselineGeneration != record.BaselineGeneration || raw.AssignmentID != record.AssignmentID || raw.ExecutionGeneration != fact.ExecutionGeneration || raw.CheckIndex != fact.Index || raw.Command != fact.Command || !statusMatches || !raw.DisposableCopy || raw.SnapshotSHA256 != fact.SourceSnapshotSHA256 || raw.CheckBinding != expectedBinding || raw.Observation == nil || !reflect.DeepEqual(*raw.Observation, *fact.Observation) {
		return fmt.Errorf("captured receipt differs from the frozen check, binding, status, observation or source snapshot")
	}
	if fact.ExecutionKind == "native" && (raw.Record == nil || *raw.Record != record.Reference() || raw.RecordSHA256 != fact.RecordSHA256 || raw.NativeContext == nil || raw.NativeContext.Generation != fact.ContextGeneration || raw.NativeContext.Ref != fact.ContextRef || raw.NativeContext.SHA256 != fact.ContextSHA256 || raw.Activation == nil || raw.Activation.Ref != fact.ActivationRef || raw.Activation.SHA256 != fact.ActivationSHA256 || raw.SourceBranch != fact.SourceBranch || raw.TargetBranch != fact.TargetBranch || raw.SourceHead != fact.SourceHead) {
		return fmt.Errorf("native captured receipt differs from its frozen Record, context, PLAN activation or source coordinates")
	}
	if fact.ExecutionKind == "workspace" && (raw.AssignmentRecord == nil || *raw.AssignmentRecord != record.Reference() || raw.AssignmentRecordSHA256 != fact.RecordSHA256 || raw.WorkerCheckIndex == nil || *raw.WorkerCheckIndex != fact.Index || raw.WorkerCheckContract != assignmentrecord.ClosingContractVersion || raw.WorkerCheckSnapshot == nil || raw.WorkerCheckSnapshot.RuntimeID != record.RuntimeID || raw.WorkerCheckSnapshot.BaselineGeneration != record.BaselineGeneration || raw.WorkerCheckSnapshot.AssignmentID != record.AssignmentID || raw.WorkerCheckSnapshot.OwnerAgentID != record.OwnerAgentID || raw.WorkerCheckSnapshot.ExecutionGeneration != fact.ExecutionGeneration || raw.WorkerCheckSnapshot.BootstrapSHA256 != fact.BootstrapSHA256) {
		return fmt.Errorf("workspace captured receipt differs from its frozen Record, Worker check or bootstrap snapshot")
	}
	if (fact.Status == "pass" || fact.Status == "fail") && fact.CurrentSourceSnapshotSHA256 != "" && fact.CurrentSourceSnapshotSHA256 != fact.SourceSnapshotSHA256 {
		return fmt.Errorf("current source snapshot differs from the receipt-backed check snapshot")
	}
	output, err := filepath.Abs(raw.Output)
	if err != nil || output != filepath.Join(filepath.Dir(receipt.Path), "output.log") || !filepath.IsAbs(raw.Output) {
		return fmt.Errorf("captured receipt output does not name its original adjacent log")
	}
	if fact.Status == "pass" && (raw.Status != "pass" || raw.Observation.Permissions == nil || !raw.Observation.Permissions.Known() || raw.Observation.PermissionDigest != raw.Observation.Permissions.Digest()) {
		return fmt.Errorf("passing Worker receipt has no known available isolation observation")
	}
	// VerifyToolReceipt rechecks the original v2 schema/seal and the log SHA.
	// The temporary directory contains only bytes already read through the
	// original Result authority reader, not live runner state.
	tmp, err := os.MkdirTemp("", "s6-worker-result-replay-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := os.WriteFile(filepath.Join(tmp, "receipt.json"), receiptBytes, 0600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tmp, "output.log"), logBytes, 0600); err != nil {
		return err
	}
	verifyRef := *receipt
	verifyRef.Path = filepath.Join(tmp, "receipt.json")
	verified, err := checkexec.VerifyToolReceipt(tmp, verifyRef)
	if err != nil || verified.Passed != (raw.Status == "pass") || !reflect.DeepEqual(verified.Observation, *fact.Observation) {
		return fmt.Errorf("durable captured receipt does not verify: %v", err)
	}
	return nil
}

type workerReceiptEnvelope struct {
	Version                int                               `json:"version"`
	RuntimeID              string                            `json:"runtime_id"`
	BaselineGeneration     int                               `json:"baseline_generation"`
	AssignmentID           string                            `json:"assignment_id"`
	ExecutionGeneration    int                               `json:"execution_generation"`
	CheckIndex             int                               `json:"check_index"`
	Command                string                            `json:"command"`
	Status                 string                            `json:"status"`
	SnapshotSHA256         string                            `json:"snapshot_sha256"`
	Output                 string                            `json:"output"`
	DisposableCopy         bool                              `json:"disposable_copy"`
	CheckBinding           checkexec.Binding                 `json:"check_binding"`
	Observation            *checkexec.Observation            `json:"profile_observation"`
	Record                 *assignmentrecord.RecordReference `json:"record,omitempty"`
	RecordSHA256           string                            `json:"record_sha256,omitempty"`
	NativeContext          *workerNativeContext              `json:"native_context,omitempty"`
	Activation             *workerNativeActivation           `json:"activation,omitempty"`
	AssignmentRecord       *assignmentrecord.RecordReference `json:"assignment_record,omitempty"`
	AssignmentRecordSHA256 string                            `json:"assignment_record_sha256,omitempty"`
	WorkerCheckIndex       *int                              `json:"worker_check_index,omitempty"`
	WorkerCheckContract    string                            `json:"worker_check_contract,omitempty"`
	WorkerCheckSnapshot    *workerCheckSnapshot              `json:"worker_check_snapshot,omitempty"`
	SourceBranch           string                            `json:"source_branch,omitempty"`
	TargetBranch           string                            `json:"target_branch,omitempty"`
	SourceHead             string                            `json:"source_head,omitempty"`
}

type workerCheckSnapshot struct {
	RuntimeID           string `json:"runtime_id"`
	BaselineGeneration  int    `json:"baseline_generation"`
	AssignmentID        string `json:"assignment_id"`
	OwnerAgentID        string `json:"owner_agent_id"`
	ExecutionGeneration int    `json:"execution_generation"`
	BootstrapSHA256     string `json:"bootstrap_sha256"`
}

type workerNativeContext struct {
	Generation int    `json:"generation"`
	Ref        string `json:"ref"`
	SHA256     string `json:"sha256"`
}

type workerNativeActivation struct {
	Ref    string `json:"ref"`
	SHA256 string `json:"sha256"`
}

func validSHA256(value string) bool {
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
