package assignment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentinputs"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/plancheckpoint"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// CollectWorkerCheckFacts reads only original registered machine receipts.
// It leaves artifact paths unset: the original CompleteTask Apply publishes
// these exact bytes and fills the immutable per-Result paths under its lock.
func CollectWorkerCheckFacts(ctx context.Context, root string, state map[string]any, record assignmentrecord.Record) ([]assignmentresult.WorkerCheckFact, map[string][]byte, error) {
	artifacts := map[string][]byte{}
	if record.ClosingContract == nil || len(record.ClosingContract.WorkerChecks) == 0 {
		return nil, artifacts, nil
	}
	current, found, err := assignmentrecord.ForOwner(state, record.OwnerAgentID)
	if err != nil || !found {
		return nil, nil, fmt.Errorf("resolve Worker closure Assignment: %v", err)
	}
	sha, err := record.Digest()
	if err != nil {
		return nil, nil, err
	}
	currentSHA, _ := current.Digest()
	if current.Reference() != record.Reference() || currentSHA != sha {
		return nil, nil, fmt.Errorf("Worker closure uses a stale Assignment")
	}
	facts := make([]assignmentresult.WorkerCheckFact, len(record.ClosingContract.WorkerChecks))
	for i, check := range record.ClosingContract.WorkerChecks {
		facts[i] = assignmentresult.WorkerCheckFact{Index: i, Command: check.Command, ProfileDigest: check.Profile.Digest(), Status: "unavailable", ExecutionKind: "native", Record: record.Reference(), RecordSHA256: sha, RuntimeID: record.RuntimeID, BaselineGeneration: record.BaselineGeneration, OwnerAgentID: record.OwnerAgentID}
	}
	registry, err := workspace.Decode(state)
	if err != nil {
		return nil, nil, fmt.Errorf("decode original Worker execution: %w", err)
	}
	if registry != nil {
		if execution, exists := registry.Execution(record.AssignmentID, record.RuntimeID, record.BaselineGeneration); exists {
			if execution.AgentID != record.OwnerAgentID || execution.AssignmentRecord == nil || *execution.AssignmentRecord != record.Reference() || execution.AssignmentRecordSHA256 != sha {
				return nil, nil, fmt.Errorf("Worker closure execution differs from its Record")
			}
			for i := range facts {
				facts[i].ExecutionKind = "workspace"
				facts[i].ExecutionGeneration = execution.Generation
				facts[i].BootstrapSHA256 = execution.BootstrapSHA256
				facts[i].Checkout = execution.Path
			}
			if execution.Status != "ready" || execution.BootstrapSHA256 == "" {
				clearUnprovedWorkerCoordinates(facts)
				return facts, artifacts, nil
			}
			if err := registry.Validate(ctx, root); err != nil {
				return nil, nil, err
			}
			if err := registry.ValidateExecution(ctx, execution, false); err != nil {
				return nil, nil, err
			}
			if err := registry.ValidateInputs(execution); err != nil {
				return nil, nil, err
			}
			if err := registry.ValidateBootstrap(execution); err != nil {
				return nil, nil, err
			}
			snapshotSHA, err := workspace.CheckSnapshotDigest(ctx, execution.Path)
			if err != nil {
				clearUnprovedWorkerCoordinates(facts)
				return facts, artifacts, nil
			}
			for i := range facts {
				facts[i].CurrentSourceSnapshotSHA256 = snapshotSHA
			}
			for i, check := range record.ClosingContract.WorkerChecks {
				facts[i].Status = "missing"
				for j := len(execution.OwnedCheckReceipts) - 1; j >= 0; j-- {
					entry := execution.OwnedCheckReceipts[j]
					if entry.WorkerCheckIndex != i {
						continue
					}
					collectWorkerReceipt(ctx, root, check, entry.Receipt, &facts[i], artifacts)
					break // Never select an older PASS around a later failure.
				}
			}
			clearUnprovedWorkerCoordinates(facts)
			return facts, artifacts, nil
		}
	}
	agent, exists := findCompletionAgent(state, record.OwnerAgentID)
	if !exists {
		return nil, nil, fmt.Errorf("Worker closure native owner is missing")
	}
	var binding struct {
		Version        int    `json:"version"`
		Generation     int    `json:"generation"`
		ProfileVersion string `json:"profile_version"`
		ProfileSHA256  string `json:"profile_sha256"`
		Mode           string `json:"mode"`
		Context        struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"context_ref"`
	}
	bindingBytes, _ := json.Marshal(agent["context_binding"])
	if err := json.Unmarshal(bindingBytes, &binding); err != nil || binding.Version != 1 || binding.Generation < 1 || binding.Context.Path == "" {
		return facts, artifacts, nil
	}
	contextBytes, err := workpacket.ReadFile(root, binding.Context.Path, 2<<20)
	if err != nil || checkexec.Hash(contextBytes) != binding.Context.SHA256 {
		return facts, artifacts, nil
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("native-assignment-context.schema.json", contextBytes); err != nil {
		return facts, artifacts, nil
	}
	var nativeContext struct {
		RuntimeID  string            `json:"runtime_id"`
		Generation int               `json:"baseline_generation"`
		SourceRoot string            `json:"source_root"`
		Inputs     map[string]string `json:"frozen_inputs"`
		Selection  struct {
			ProfileVersion string `json:"profile_version"`
			ProfileSHA256  string `json:"profile_sha256"`
			Mode           string `json:"mode"`
		} `json:"context_selection"`
		Dispatch struct {
			Record          *assignmentrecord.RecordReference `json:"assignment_record"`
			SHA256          string                            `json:"assignment_record_sha256"`
			Owner           string                            `json:"owner"`
			ClosingContract *assignmentrecord.ClosingContract `json:"closing_contract"`
		} `json:"dispatch"`
	}
	if err := json.Unmarshal(contextBytes, &nativeContext); err != nil || nativeContext.Dispatch.Record == nil || *nativeContext.Dispatch.Record != record.Reference() || nativeContext.Dispatch.SHA256 != sha || nativeContext.Dispatch.Owner != record.OwnerAgentID {
		return facts, artifacts, nil
	}
	canonicalRoot, rootErr := workspace.Canonical(root)
	if rootErr != nil || nativeContext.SourceRoot != canonicalRoot || nativeContext.RuntimeID != record.RuntimeID || nativeContext.Generation != record.BaselineGeneration || !reflect.DeepEqual(nativeContext.Dispatch.ClosingContract, record.ClosingContract) || nativeContext.Selection.ProfileVersion != binding.ProfileVersion || nativeContext.Selection.ProfileSHA256 != binding.ProfileSHA256 || nativeContext.Selection.Mode != binding.Mode || len(nativeContext.Inputs) == 0 {
		return facts, artifacts, nil
	}
	for ref, expected := range nativeContext.Inputs {
		data, err := assignmentinputs.Read(root, ref)
		if err != nil || len(expected) != 64 || checkexec.Hash(data) != expected {
			return facts, artifacts, nil
		}
	}
	planRef, err := plancheckpoint.ActivatedRef(root, loopruntime.Snapshot{State: state}, record.OwnerAgentID)
	if err != nil || planRef == "" {
		return facts, artifacts, nil
	}
	activationRef, _ := agent["activation_ref"].(string)
	activation, err := workpacket.ReadFile(root, activationRef, 1<<20)
	if err != nil {
		return facts, artifacts, nil
	}
	activationSHA := checkexec.Hash(activation)
	source, err := ResolveFrozenNativeCheckSource(root, state, record)
	if err != nil {
		return facts, artifacts, nil
	}
	snapshotSHA, err := workspace.CheckSnapshotDigest(ctx, source.Checkout)
	if err != nil {
		return facts, artifacts, nil
	}
	for i := range facts {
		facts[i].CurrentSourceSnapshotSHA256 = snapshotSHA
		facts[i].ContextGeneration = binding.Generation
		facts[i].ContextRef = binding.Context.Path
		facts[i].ContextSHA256 = binding.Context.SHA256
		facts[i].ActivationRef = activationRef
		facts[i].ActivationSHA256 = activationSHA
		facts[i].Checkout = source.Checkout
		facts[i].SourceBranch = source.Branch
		facts[i].TargetBranch = source.TargetBranch
		facts[i].SourceHead = source.Head
		facts[i].Status = "missing"
	}
	if raw, exists := agent["owned_check_receipts"]; exists {
		data, err := json.Marshal(raw)
		if err != nil {
			return nil, nil, err
		}
		var entries []NativeOwnedCheckReceipt
		if err := json.Unmarshal(data, &entries); err != nil || len(entries) > 1024 {
			return nil, nil, fmt.Errorf("native receipt index is invalid or exceeds its budget")
		}
		for i, check := range record.ClosingContract.WorkerChecks {
			for j := len(entries) - 1; j >= 0; j-- {
				entry := entries[j]
				if entry.CheckIndex != i {
					continue
				}
				if err := ValidateNativeOwnedCheckReceipt(record, binding.Generation, binding.Context.Path, binding.Context.SHA256, activationRef, activationSHA, source, entry); err != nil {
					facts[i].Status = "stale"
					break
				}
				collectWorkerReceipt(ctx, root, check, entry.ToolReceipt, &facts[i], artifacts)
				break
			}
		}
	}
	clearUnprovedWorkerCoordinates(facts)
	return facts, artifacts, nil
}

// CurrentWorkerClosureDigest uses the same registered receipt collector and
// immutable artifact coordinates as canonical Result acceptance. It does not
// run a Worker check or infer a PASS from the professional answer.
func CurrentWorkerClosureDigest(ctx context.Context, root string, state map[string]any, record assignmentrecord.Record) (string, error) {
	if record.ClosingContract == nil {
		return "", nil
	}
	if len(record.ClosingContract.WorkerChecks) == 0 {
		return assignmentresult.WorkerClosureDigest(record, []assignmentresult.WorkerCheckFact{})
	}
	facts, artifacts, err := CollectWorkerCheckFacts(ctx, root, state, record)
	if err != nil {
		return "", err
	}
	if err := prepareWorkerCheckArtifactRefs(record, "closure-preflight", facts, artifacts); err != nil {
		return "", err
	}
	return assignmentresult.WorkerClosureDigest(record, facts)
}

func clearUnprovedWorkerCoordinates(facts []assignmentresult.WorkerCheckFact) {
	for i := range facts {
		if facts[i].OriginalReceipt == nil {
			facts[i].Checkout = ""
		}
	}
}

func collectWorkerReceipt(ctx context.Context, root string, check assignmentrecord.WorkerCheck, ref checkexec.ToolReceipt, fact *assignmentresult.WorkerCheckFact, artifacts map[string][]byte) {
	fact.Status = "unavailable"
	want := checkexec.Binding{RuntimeID: fact.RuntimeID, Generation: fact.BaselineGeneration, AssignmentID: fact.Record.AssignmentID, Owner: fact.OwnerAgentID, ExecutionGeneration: fact.ExecutionGeneration, Responsibility: "owned_check"}
	if ref.Binding != want || ref.Checkout != fact.Checkout || ref.Command != check.Command || ref.ProfileDigest != check.Profile.Digest() {
		fact.Status = "stale"
		return
	}
	verified, err := checkexec.VerifyToolReceipt(filepath.Join(root, ".claude/evidence/workspace-checks"), ref)
	if err != nil {
		return
	}
	rel, err := filepath.Rel(root, ref.Path)
	if err != nil || !filepath.IsLocal(rel) {
		return
	}
	data, err := workpacket.ReadFile(root, filepath.ToSlash(rel), 4<<20)
	if err != nil || checkexec.Hash(data) != ref.SHA256 {
		return
	}
	var raw struct {
		Snapshot string `json:"snapshot_sha256"`
		Index    int    `json:"check_index"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(data, &raw); err != nil || raw.Index != check.Index || (raw.Status != "pass" && raw.Status != "fail") {
		return
	}
	currentSHA, current, err := workspace.ObserveOwnedCheckSource(ctx, fact.Checkout, check.Profile)
	if err != nil {
		return
	}
	if fact.CurrentSourceSnapshotSHA256 != "" && fact.CurrentSourceSnapshotSHA256 != currentSHA {
		return
	}
	fact.CurrentSourceSnapshotSHA256 = currentSHA
	previous := verified.Observation
	if raw.Snapshot != currentSHA || previous.SubjectDigest != current.SubjectDigest || previous.DependencyDigest != current.DependencyDigest || previous.FixtureDigest != current.FixtureDigest || previous.RunnerDigest != current.RunnerDigest || previous.EnvironmentDigest != current.EnvironmentDigest {
		fact.Status = "stale"
		return
	}
	logRel := filepath.ToSlash(filepath.Join(filepath.Dir(rel), "output.log"))
	log, err := workpacket.ReadFile(root, logRel, 16<<20)
	if err != nil || checkexec.Hash(log) != ref.LogSHA256 {
		return
	}
	if verified.Passed && (previous.Permissions == nil || !previous.Permissions.Known() || !previous.Permissions.Available || previous.PermissionDigest != previous.Permissions.Digest()) {
		return
	}
	fact.Status = raw.Status
	fact.SourceSnapshotSHA256 = raw.Snapshot
	fact.Observation = &previous
	fact.OriginalReceipt = &ref
	artifacts[assignmentresult.WorkerCheckEvidenceRef(check.Index, "receipt")] = data
	artifacts[assignmentresult.WorkerCheckEvidenceRef(check.Index, "log")] = log
}

func publishWorkerCheckArtifacts(root string, record assignmentrecord.Record, resultID string, facts []assignmentresult.WorkerCheckFact, artifacts map[string][]byte) ([]stagedCanonicalResultArtifact, error) {
	var staged []stagedCanonicalResultArtifact
	for i := range facts {
		if facts[i].Status != "pass" && facts[i].Status != "fail" {
			continue
		}
		for _, kind := range []string{"receipt", "log"} {
			logical := assignmentresult.WorkerCheckEvidenceRef(i, kind)
			body, exists := artifacts[logical]
			if !exists {
				return staged, fmt.Errorf("Worker check %d lacks captured %s bytes", i, kind)
			}
			sha := checkexec.Hash(body)
			path := assignmentresult.WorkerCheckEvidenceArtifactPath(record, resultID, i, kind, sha)
			if path == "" || strings.TrimSpace(path) == "" {
				return staged, fmt.Errorf("Worker evidence path is invalid")
			}
			staged = append(staged, stagedCanonicalResultArtifact{Path: path, SHA: sha})
			published, err := workpacket.PublishBounded(root, path, body, canonicalResultAssetLimit)
			if err != nil {
				return staged, err
			}
			if !bytes.Equal(published, body) {
				return staged, fmt.Errorf("immutable Worker evidence path conflicts")
			}
			resolved := &assignmentresult.ResolvedEvidence{Ref: logical, Path: path, SHA256: sha}
			if kind == "receipt" {
				facts[i].ReceiptArtifact = resolved
			} else {
				facts[i].LogArtifact = resolved
			}
		}
	}
	return staged, nil
}

func prepareWorkerCheckArtifactRefs(record assignmentrecord.Record, resultID string, facts []assignmentresult.WorkerCheckFact, artifacts map[string][]byte) error {
	for i := range facts {
		if facts[i].Status != "pass" && facts[i].Status != "fail" {
			continue
		}
		for _, kind := range []string{"receipt", "log"} {
			logical := assignmentresult.WorkerCheckEvidenceRef(facts[i].Index, kind)
			body, exists := artifacts[logical]
			if !exists {
				return fmt.Errorf("Worker check %d lacks captured %s bytes", facts[i].Index, kind)
			}
			sha := checkexec.Hash(body)
			ref := &assignmentresult.ResolvedEvidence{Ref: logical, Path: assignmentresult.WorkerCheckEvidenceArtifactPath(record, resultID, facts[i].Index, kind, sha), SHA256: sha}
			if kind == "receipt" {
				facts[i].ReceiptArtifact = ref
			} else {
				facts[i].LogArtifact = ref
			}
		}
	}
	return nil
}
