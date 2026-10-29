package assignmentresult

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
)

func TestExplicitEmptyWorkerClosureHasStableIdentityAndLegacyRemainsUnassessed(t *testing.T) {
	record := workerFactRecord(t, nil)
	record.ClosingContract.WorkerChecks = []assignmentrecord.WorkerCheck{}
	if err := assignmentrecord.ValidateRecord(record); err != nil {
		t.Fatal(err)
	}
	result := AcceptedResult{}
	digest, err := EffectiveToolClosureDigest(record, result)
	if err != nil || digest == "" || !WorkerClosureComplete(record, result) {
		t.Fatalf("explicit empty WorkerChecks identity = %q, %v", digest, err)
	}
	if result.ToolClosureDigest != "" || result.WorkerCheckFacts != nil {
		t.Fatal("effective identity derivation must not rewrite an old accepted envelope")
	}

	legacy := workerFactRecord(t, nil)
	legacy.ClosingContract = nil
	legacy.IntegrationCheckMode = ""
	legacyDigest, err := EffectiveToolClosureDigest(legacy, AcceptedResult{})
	if err != nil || legacyDigest != "" || WorkerClosureComplete(legacy, AcceptedResult{}) {
		t.Fatalf("legacy missing contract gained Worker closure: %q, %v", legacyDigest, err)
	}
	if _, err := EffectiveToolClosureDigest(legacy, AcceptedResult{ToolClosureDigest: strings.Repeat("a", 64)}); err == nil {
		t.Fatal("legacy Result accepted a fabricated tool closure")
	}
}

func TestWorkerClosureDigestRequiresSortedFullFrozenIndexSet(t *testing.T) {
	record := workerFactRecord(t, []string{"true", "false"})
	facts := []WorkerCheckFact{
		missingWorkerFact(record, 0, "workspace"),
		missingWorkerFact(record, 1, "workspace"),
	}
	if _, err := WorkerClosureDigest(record, facts); err != nil {
		t.Fatalf("full sorted set rejected: %v", err)
	}
	if _, err := WorkerClosureDigest(record, facts[:1]); err == nil {
		t.Fatal("missing frozen index was accepted")
	}
	if _, err := WorkerClosureDigest(record, []WorkerCheckFact{facts[1], facts[0]}); err == nil {
		t.Fatal("unsorted fact set was accepted")
	}
	if _, err := WorkerClosureDigest(record, []WorkerCheckFact{facts[0], {
		Index: 1, Command: "false", ProfileDigest: strings.Repeat("f", 64), Status: "missing",
		ExecutionKind: "workspace", Record: record.Reference(), RecordSHA256: mustRecordDigest(t, record),
		RuntimeID: record.RuntimeID, BaselineGeneration: record.BaselineGeneration,
		OwnerAgentID: record.OwnerAgentID, ExecutionGeneration: 2,
	}}); err == nil {
		t.Fatal("fact with a different frozen profile was accepted")
	}
}

func TestWorkerClosureDurableReplayValidatesOriginalReceiptAndLog(t *testing.T) {
	for _, kind := range []string{"workspace", "native"} {
		t.Run(kind, func(t *testing.T) {
			record := workerFactRecord(t, []string{"true"})
			resultID := "ev-completion-test"
			fact, files := passingWorkerFact(t, record, resultID, kind)
			result := AcceptedResult{EvidenceID: resultID, WorkerCheckFacts: []WorkerCheckFact{fact}}
			result.ToolClosureDigest, _ = WorkerClosureDigest(record, result.WorkerCheckFacts)
			read := func(ref string) ([]byte, error) {
				data, ok := files[ref]
				if !ok {
					return nil, fmt.Errorf("not found")
				}
				return data, nil
			}
			if err := ValidateWorkerClosure(record, result, read); err != nil {
				t.Fatalf("valid durable %s proof rejected: %v", kind, err)
			}
			if !WorkerClosureComplete(record, result) {
				t.Fatal("fully passing closure was not recognized")
			}

			changed := append([]byte{}, files[fact.LogArtifact.Path]...)
			files[fact.LogArtifact.Path] = append(changed, []byte("tampered")...)
			if err := ValidateWorkerClosure(record, result, read); err == nil {
				t.Fatal("changed durable log was accepted")
			}
		})
	}
}

func TestWorkerClosureAcceptsExplicitMissingButNeverCompletes(t *testing.T) {
	record := workerFactRecord(t, []string{"true"})
	fact := missingWorkerFact(record, 0, "workspace")
	result := AcceptedResult{EvidenceID: "ev-missing-check", WorkerCheckFacts: []WorkerCheckFact{fact}}
	result.ToolClosureDigest, _ = WorkerClosureDigest(record, result.WorkerCheckFacts)
	if err := ValidateWorkerClosure(record, result, nil); err != nil {
		t.Fatalf("honest missing check fact rejected: %v", err)
	}
	if WorkerClosureComplete(record, result) {
		t.Fatal("missing Worker check was treated as complete")
	}
	result.WorkerCheckFacts[0].Status = "unavailable"
	result.ToolClosureDigest, _ = WorkerClosureDigest(record, result.WorkerCheckFacts)
	if err := ValidateWorkerClosure(record, result, nil); err != nil || WorkerClosureComplete(record, result) {
		t.Fatalf("unavailable Worker check result = %v, complete=%v", err, WorkerClosureComplete(record, result))
	}
}

func TestCurrentSourceSnapshotChangesUnprovenClosureWithoutCompleting(t *testing.T) {
	for _, status := range []string{"missing", "stale", "unavailable"} {
		t.Run(status, func(t *testing.T) {
			record := workerFactRecord(t, []string{"true"})
			fact := missingWorkerFact(record, 0, "workspace")
			fact.Status = status
			fact.CurrentSourceSnapshotSHA256 = strings.Repeat("a", 64)
			firstDigest, err := WorkerClosureDigest(record, []WorkerCheckFact{fact})
			if err != nil {
				t.Fatalf("current source metadata rejected: %v", err)
			}
			result := AcceptedResult{EvidenceID: "ev-current-source", WorkerCheckFacts: []WorkerCheckFact{fact}}
			result.ToolClosureDigest = firstDigest
			if err := ValidateWorkerClosure(record, result, nil); err != nil {
				t.Fatalf("unproven source fact rejected: %v", err)
			}
			if WorkerClosureComplete(record, result) {
				t.Fatal("current source snapshot metadata completed a check without proof")
			}

			fact.CurrentSourceSnapshotSHA256 = strings.Repeat("b", 64)
			secondDigest, err := WorkerClosureDigest(record, []WorkerCheckFact{fact})
			if err != nil || secondDigest == firstDigest {
				t.Fatalf("source change did not change closure identity: first=%s second=%s err=%v", firstDigest, secondDigest, err)
			}
			fact.CurrentSourceSnapshotSHA256 = "not-a-sha"
			if _, err := WorkerClosureDigest(record, []WorkerCheckFact{fact}); err == nil {
				t.Fatal("malformed current source snapshot digest was accepted")
			}
		})
	}
}

func TestUnavailableWorkerCheckCanDeclareUnknownExecutionIdentity(t *testing.T) {
	for _, kind := range []string{"workspace", "native"} {
		t.Run(kind, func(t *testing.T) {
			record := workerFactRecord(t, []string{"true"})
			fact := missingWorkerFact(record, 0, kind)
			fact.Status = "unavailable"
			if kind == "workspace" {
				fact.ExecutionGeneration = 0
			} else {
				fact.ContextGeneration, fact.ContextRef, fact.ContextSHA256 = 0, "", ""
				fact.ActivationRef, fact.ActivationSHA256 = "", ""
			}
			result := AcceptedResult{EvidenceID: "ev-unavailable-identity", WorkerCheckFacts: []WorkerCheckFact{fact}}
			result.ToolClosureDigest, _ = WorkerClosureDigest(record, result.WorkerCheckFacts)
			if err := ValidateWorkerClosure(record, result, nil); err != nil {
				t.Fatalf("unavailable check with an unknown %s execution identity was rejected: %v", kind, err)
			}
			if WorkerClosureComplete(record, result) {
				t.Fatal("unavailable check with an unknown execution identity became complete")
			}

			for _, status := range []string{"missing", "stale", "pass", "fail"} {
				t.Run("reject incomplete "+status, func(t *testing.T) {
					candidate := fact
					candidate.Status = status
					invalid := AcceptedResult{EvidenceID: result.EvidenceID, WorkerCheckFacts: []WorkerCheckFact{candidate}}
					if _, err := WorkerClosureDigest(record, invalid.WorkerCheckFacts); err == nil {
						t.Fatalf("%s Worker fact borrowed unavailable's incomplete identity", status)
					}
				})
			}
		})
	}
}

func TestHistoricalNonemptyWorkerResultRemainsUnassessed(t *testing.T) {
	record := workerFactRecord(t, []string{"true"})
	result := AcceptedResult{EvidenceID: "ev-before-worker-closure"}
	digest, err := EffectiveToolClosureDigest(record, result)
	if err != nil || digest == "" {
		t.Fatalf("legacy closure identity = %q, %v", digest, err)
	}
	if err := ValidateWorkerClosure(record, result, nil); err != nil {
		t.Fatalf("historical accepted envelope should remain readable: %v", err)
	}
	if WorkerClosureComplete(record, result) {
		t.Fatal("historical envelope without owned check facts became complete")
	}
	if digest == mustEmptyClosureDigest(t, record) {
		t.Fatal("unproven historical identity collapsed into explicit empty checks")
	}
}

func mustEmptyClosureDigest(t *testing.T, record assignmentrecord.Record) string {
	t.Helper()
	empty := record
	empty.ClosingContract = &assignmentrecord.ClosingContract{Contract: S6ClosingContract, WorkerChecks: []assignmentrecord.WorkerCheck{}}
	empty.IntegrationCheckMode = "post_merge"
	digest, err := EffectiveToolClosureDigest(empty, AcceptedResult{})
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func workerFactRecord(t *testing.T, commands []string) assignmentrecord.Record {
	t.Helper()
	record := resultTestRecord(t)
	checks := make([]assignmentrecord.WorkerCheck, 0, len(commands))
	for i, command := range commands {
		profile := checkexec.Profile{
			ID: fmt.Sprintf("worker-profile-%d", i), Version: "1.0.0", CheckIndex: i,
			Command: command, Location: "worker", TimeoutSeconds: 30,
			EnvironmentPolicy: "declared_only", Environment: []checkexec.EnvFact{},
			Dependencies: []checkexec.FileRef{}, Fixtures: []checkexec.FileRef{},
			InputClosure: "whole_tree", SideEffects: "none", ReusePolicy: "never",
		}
		checks = append(checks, assignmentrecord.WorkerCheck{Index: i, Command: command, Profile: profile})
	}
	record.ClosingContract = &assignmentrecord.ClosingContract{Contract: S6ClosingContract, WorkerChecks: checks}
	record.IntegrationCheckMode = "post_merge"
	if err := assignmentrecord.ValidateRecord(record); err != nil {
		t.Fatalf("test record invalid: %v", err)
	}
	return record
}

func missingWorkerFact(record assignmentrecord.Record, index int, kind string) WorkerCheckFact {
	check := record.ClosingContract.WorkerChecks[index]
	fact := WorkerCheckFact{
		Index: index, Command: check.Command, ProfileDigest: check.Profile.Digest(), Status: "missing",
		ExecutionKind: kind, Record: record.Reference(), RuntimeID: record.RuntimeID,
		BaselineGeneration: record.BaselineGeneration, OwnerAgentID: record.OwnerAgentID,
		RecordSHA256: mustDigest(record),
	}
	if kind == "workspace" {
		fact.ExecutionGeneration = 2
	} else {
		fact.ContextGeneration = 3
		fact.ContextRef = ".claude/assignment-context.json"
		fact.ContextSHA256 = strings.Repeat("c", 64)
		fact.ActivationRef = ".claude/evidence/runtime/g3/assignments/agent/activation.json"
		fact.ActivationSHA256 = strings.Repeat("9", 64)
	}
	return fact
}

func passingWorkerFact(t *testing.T, record assignmentrecord.Record, resultID, kind string) (WorkerCheckFact, map[string][]byte) {
	t.Helper()
	profile := record.ClosingContract.WorkerChecks[0].Profile
	root := t.TempDir()
	checkout := filepath.Join(root, "checkout")
	if err := os.MkdirAll(checkout, 0700); err != nil {
		t.Fatal(err)
	}
	originalDir := filepath.Join(root, ".claude", "evidence", record.RuntimeID, fmt.Sprintf("g%d", record.BaselineGeneration), "worktree", record.AssignmentID, "checks", "check-0")
	if err := os.MkdirAll(originalDir, 0700); err != nil {
		t.Fatal(err)
	}
	originalReceiptPath := filepath.Join(originalDir, "receipt.json")
	logPath := filepath.Join(originalDir, "output.log")
	logBytes := []byte("check output\n")
	if err := os.WriteFile(logPath, logBytes, 0600); err != nil {
		t.Fatal(err)
	}
	executionGeneration := 3
	contextGeneration, contextRef, contextSHA := 0, "", ""
	activationRef, activationSHA := "", ""
	if kind == "native" {
		executionGeneration = 0
		contextGeneration, contextRef, contextSHA = 9, ".claude/assignment-context.json", strings.Repeat("c", 64)
		activationRef, activationSHA = ".claude/evidence/runtime/g3/assignments/agent/activation.json", strings.Repeat("9", 64)
	}
	binding := checkexec.Binding{RuntimeID: record.RuntimeID, Generation: record.BaselineGeneration, AssignmentID: record.AssignmentID, Owner: record.OwnerAgentID, ExecutionGeneration: executionGeneration, Responsibility: "owned_check"}
	permission := &checkexec.PermissionObservation{
		Version: "1.0.0", Runner: "worker_bubblewrap_seccomp_v1", Platform: "linux/amd64", Kernel: "test-kernel",
		UID: 1000, GID: 1000, Groups: []int{}, BinarySHA256: strings.Repeat("1", 64), ShellSHA256: strings.Repeat("2", 64),
		LibrariesSHA256: strings.Repeat("3", 64), FilterSHA256: strings.Repeat("4", 64), PolicySHA256: strings.Repeat("5", 64),
		AuthoritySHA256: strings.Repeat("6", 64), MainRoot: root, CommonDir: root, WorkerRoot: checkout, CWD: checkout, Available: true,
	}
	observation := &checkexec.Observation{
		ProfileID: profile.ID, ProfileVersion: profile.Version, ProfileDigest: profile.Digest(),
		SubjectDigest: strings.Repeat("a", 64), RunnerDigest: strings.Repeat("b", 64), EnvironmentDigest: strings.Repeat("c", 64),
		DependencyDigest: strings.Repeat("d", 64), FixtureDigest: strings.Repeat("e", 64), Environment: []checkexec.EnvFact{},
		VerificationScope: "whole disposable checked tree", ReuseEligible: false,
		Permissions: permission, PermissionDigest: permission.Digest(),
	}
	snapshotSHA := strings.Repeat("7", 64)
	raw := map[string]any{
		"version": 2, "runtime_id": record.RuntimeID, "baseline_generation": record.BaselineGeneration,
		"assignment_id": record.AssignmentID, "execution_generation": executionGeneration,
		"check_index": 0, "command": profile.Command, "status": "pass", "snapshot_sha256": snapshotSHA,
		"check_binding": binding, "profile_observation": observation, "output": logPath, "disposable_copy": true,
	}
	branch, targetBranch, sourceHead := "", "", ""
	if kind == "native" {
		branch, targetBranch, sourceHead = "worker/assignment", "main", strings.Repeat("8", 40)
		raw["record"] = record.Reference()
		raw["record_sha256"] = mustDigest(record)
		raw["native_context"] = map[string]any{"generation": contextGeneration, "ref": contextRef, "sha256": contextSHA}
		raw["activation"] = map[string]any{"ref": activationRef, "sha256": activationSHA}
		raw["source_branch"], raw["target_branch"], raw["source_head"] = branch, targetBranch, sourceHead
	} else {
		bootstrapSHA := strings.Repeat("f", 64)
		raw["assignment_record"] = record.Reference()
		raw["assignment_record_sha256"] = mustDigest(record)
		raw["worker_check_index"] = 0
		raw["worker_check_contract"] = assignmentrecord.ClosingContractVersion
		raw["worker_check_snapshot"] = map[string]any{
			"runtime_id": record.RuntimeID, "baseline_generation": record.BaselineGeneration,
			"assignment_id": record.AssignmentID, "owner_agent_id": record.OwnerAgentID,
			"execution_generation": executionGeneration, "bootstrap_sha256": bootstrapSHA,
		}
	}
	receiptBytes, err := checkexec.SealReceipt(raw, logPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(originalReceiptPath, receiptBytes, 0600); err != nil {
		t.Fatal(err)
	}
	receipt := &checkexec.ToolReceipt{
		Path: originalReceiptPath, SHA256: checkexec.Hash(receiptBytes), LogSHA256: checkexec.Hash(logBytes),
		Checkout: checkout, Command: profile.Command, ProfileDigest: profile.Digest(), Binding: binding,
	}
	recordSHA := mustDigest(record)
	fact := WorkerCheckFact{
		Index: 0, Command: profile.Command, ProfileDigest: profile.Digest(), Status: "pass", ExecutionKind: kind,
		Record: record.Reference(), RecordSHA256: recordSHA, RuntimeID: record.RuntimeID,
		BaselineGeneration: record.BaselineGeneration, OwnerAgentID: record.OwnerAgentID,
		ExecutionGeneration: executionGeneration, ContextGeneration: contextGeneration,
		ContextRef: contextRef, ContextSHA256: contextSHA, ActivationRef: activationRef,
		ActivationSHA256: activationSHA, Checkout: checkout,
		SourceBranch: branch, TargetBranch: targetBranch, SourceHead: sourceHead,
		SourceSnapshotSHA256: snapshotSHA, Observation: observation, OriginalReceipt: receipt,
	}
	if kind == "workspace" {
		fact.BootstrapSHA256 = strings.Repeat("f", 64)
	}
	fact.ReceiptArtifact = &ResolvedEvidence{Ref: WorkerCheckEvidenceRef(0, "receipt"), Path: WorkerCheckEvidenceArtifactPath(record, resultID, 0, "receipt", receipt.SHA256), SHA256: receipt.SHA256}
	fact.LogArtifact = &ResolvedEvidence{Ref: WorkerCheckEvidenceRef(0, "log"), Path: WorkerCheckEvidenceArtifactPath(record, resultID, 0, "log", receipt.LogSHA256), SHA256: receipt.LogSHA256}
	files := map[string][]byte{fact.ReceiptArtifact.Path: receiptBytes, fact.LogArtifact.Path: logBytes}
	return fact, files
}

func TestWorkspaceWorkerReceiptBindsRecordCheckAndBootstrapSnapshot(t *testing.T) {
	record := workerFactRecord(t, []string{"true"})
	fact, files := passingWorkerFact(t, record, "ev-workspace-binding", "workspace")
	result := AcceptedResult{EvidenceID: "ev-workspace-binding", WorkerCheckFacts: []WorkerCheckFact{fact}}
	result.ToolClosureDigest, _ = WorkerClosureDigest(record, result.WorkerCheckFacts)
	read := func(ref string) ([]byte, error) { return files[ref], nil }
	if err := ValidateWorkerClosure(record, result, read); err != nil {
		t.Fatalf("valid workspace record/bootstrap binding rejected: %v", err)
	}
	matchingSnapshot := fact
	matchingSnapshot.CurrentSourceSnapshotSHA256 = fact.SourceSnapshotSHA256
	matchingResult := AcceptedResult{EvidenceID: result.EvidenceID, WorkerCheckFacts: []WorkerCheckFact{matchingSnapshot}}
	matchingResult.ToolClosureDigest, _ = WorkerClosureDigest(record, matchingResult.WorkerCheckFacts)
	if err := ValidateWorkerClosure(record, matchingResult, read); err != nil {
		t.Fatalf("matching current and receipt-backed source snapshots rejected: %v", err)
	}
	driftingSnapshot := fact
	driftingSnapshot.CurrentSourceSnapshotSHA256 = strings.Repeat("9", 64)
	driftingResult := AcceptedResult{EvidenceID: result.EvidenceID, WorkerCheckFacts: []WorkerCheckFact{driftingSnapshot}}
	driftingResult.ToolClosureDigest, _ = WorkerClosureDigest(record, driftingResult.WorkerCheckFacts)
	if err := ValidateWorkerClosure(record, driftingResult, read); err == nil {
		t.Fatal("pass receipt with a different current source snapshot was accepted")
	}
	withoutBootstrap := fact
	withoutBootstrap.BootstrapSHA256 = ""
	if _, err := WorkerClosureDigest(record, []WorkerCheckFact{withoutBootstrap}); err == nil {
		t.Fatal("receipt-backed workspace fact without a bootstrap digest was accepted")
	}

	mutations := []struct {
		name string
		edit func(map[string]any)
	}{
		{name: "record reference", edit: func(raw map[string]any) {
			raw["assignment_record"].(map[string]any)["assignment_id"] = "assignment-other"
		}},
		{name: "record digest", edit: func(raw map[string]any) { raw["assignment_record_sha256"] = strings.Repeat("0", 64) }},
		{name: "worker index", edit: func(raw map[string]any) { raw["worker_check_index"] = float64(1) }},
		{name: "worker contract", edit: func(raw map[string]any) { raw["worker_check_contract"] = "s6-closing-v0" }},
		{name: "execution generation", edit: func(raw map[string]any) {
			raw["worker_check_snapshot"].(map[string]any)["execution_generation"] = float64(99)
		}},
		{name: "bootstrap digest", edit: func(raw map[string]any) {
			raw["worker_check_snapshot"].(map[string]any)["bootstrap_sha256"] = strings.Repeat("0", 64)
		}},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			candidate := fact
			candidate.OriginalReceipt = cloneWorkerToolReceipt(fact.OriginalReceipt)
			candidate.ReceiptArtifact = cloneWorkerResolvedEvidence(fact.ReceiptArtifact)
			candidate.LogArtifact = cloneWorkerResolvedEvidence(fact.LogArtifact)
			candidateFiles := make(map[string][]byte, len(files)+1)
			for name, data := range files {
				candidateFiles[name] = append([]byte(nil), data...)
			}
			oldPath := candidate.ReceiptArtifact.Path
			var raw map[string]any
			if err := json.Unmarshal(candidateFiles[oldPath], &raw); err != nil {
				t.Fatal(err)
			}
			mutation.edit(raw)
			logPath := filepath.Join(filepath.Dir(candidate.OriginalReceipt.Path), "output.log")
			resealed, err := checkexec.SealReceipt(raw, logPath)
			if err != nil {
				t.Fatal(err)
			}
			candidate.OriginalReceipt.SHA256 = checkexec.Hash(resealed)
			candidate.ReceiptArtifact.SHA256 = checkexec.Hash(resealed)
			candidate.ReceiptArtifact.Path = WorkerCheckEvidenceArtifactPath(record, result.EvidenceID, candidate.Index, "receipt", candidate.ReceiptArtifact.SHA256)
			delete(candidateFiles, oldPath)
			candidateFiles[candidate.ReceiptArtifact.Path] = resealed
			candidateResult := AcceptedResult{EvidenceID: result.EvidenceID, WorkerCheckFacts: []WorkerCheckFact{candidate}}
			candidateResult.ToolClosureDigest, err = WorkerClosureDigest(record, candidateResult.WorkerCheckFacts)
			if err != nil {
				t.Fatalf("mutated receipt fact should still form an identity: %v", err)
			}
			candidateRead := func(ref string) ([]byte, error) { return candidateFiles[ref], nil }
			if err := ValidateWorkerClosure(record, candidateResult, candidateRead); err == nil {
				t.Fatal("receipt with a changed Workspace tuple was accepted")
			}
		})
	}
}

func cloneWorkerToolReceipt(source *checkexec.ToolReceipt) *checkexec.ToolReceipt {
	if source == nil {
		return nil
	}
	copy := *source
	return &copy
}

func cloneWorkerResolvedEvidence(source *ResolvedEvidence) *ResolvedEvidence {
	if source == nil {
		return nil
	}
	copy := *source
	return &copy
}

func mustDigest(record assignmentrecord.Record) string {
	digest, _ := record.Digest()
	return digest
}

func TestWorkerClosureDigestChangesWithToolFactsButNotArtifactPaths(t *testing.T) {
	record := workerFactRecord(t, []string{"true"})
	fact, _ := passingWorkerFact(t, record, "ev-first", "workspace")
	one, err := WorkerClosureDigest(record, []WorkerCheckFact{fact})
	if err != nil {
		t.Fatal(err)
	}
	fact.ReceiptArtifact.Path = WorkerCheckEvidenceArtifactPath(record, "ev-second", fact.Index, "receipt", fact.ReceiptArtifact.SHA256)
	fact.LogArtifact.Path = WorkerCheckEvidenceArtifactPath(record, "ev-second", fact.Index, "log", fact.LogArtifact.SHA256)
	two, err := WorkerClosureDigest(record, []WorkerCheckFact{fact})
	if err != nil || one != two {
		t.Fatalf("result-specific artifact relocation changed tool identity: %s %s %v", one, two, err)
	}
	fact.SourceSnapshotSHA256 = strings.Repeat("9", 64)
	three, err := WorkerClosureDigest(record, []WorkerCheckFact{fact})
	if err != nil || three == one {
		t.Fatalf("changed source snapshot retained closure identity: %s %s %v", one, three, err)
	}
	fact.SourceSnapshotSHA256 = strings.Repeat("7", 64)
	fact.BootstrapSHA256 = strings.Repeat("8", 64)
	four, err := WorkerClosureDigest(record, []WorkerCheckFact{fact})
	if err != nil || four == one {
		t.Fatalf("changed workspace bootstrap retained closure identity: %s %s %v", one, four, err)
	}
}

func TestNativeActivationBindingIsPartOfDurableWorkerClosure(t *testing.T) {
	record := workerFactRecord(t, []string{"true"})
	fact, files := passingWorkerFact(t, record, "ev-native-activation", "native")
	result := AcceptedResult{EvidenceID: "ev-native-activation", WorkerCheckFacts: []WorkerCheckFact{fact}}
	result.ToolClosureDigest, _ = WorkerClosureDigest(record, result.WorkerCheckFacts)
	read := func(ref string) ([]byte, error) { return files[ref], nil }
	if err := ValidateWorkerClosure(record, result, read); err != nil {
		t.Fatalf("valid native activation binding rejected: %v", err)
	}

	fact.ActivationSHA256 = strings.Repeat("8", 64)
	result.WorkerCheckFacts[0] = fact
	result.ToolClosureDigest, _ = WorkerClosureDigest(record, result.WorkerCheckFacts)
	if err := ValidateWorkerClosure(record, result, read); err == nil {
		t.Fatal("fact with an activation hash different from the sealed native receipt was accepted")
	}
}

func TestWorkerReceiptRejectsStaleOrForgedBindings(t *testing.T) {
	record := workerFactRecord(t, []string{"true"})
	fact, files := passingWorkerFact(t, record, "ev-invalid", "workspace")
	result := AcceptedResult{EvidenceID: "ev-invalid", WorkerCheckFacts: []WorkerCheckFact{fact}}
	result.ToolClosureDigest, _ = WorkerClosureDigest(record, result.WorkerCheckFacts)
	read := func(ref string) ([]byte, error) { return files[ref], nil }
	fact.OriginalReceipt.Binding.Owner = "someone-else"
	result.WorkerCheckFacts[0] = fact
	result.ToolClosureDigest, _ = WorkerClosureDigest(record, result.WorkerCheckFacts)
	if err := ValidateWorkerClosure(record, result, read); err == nil {
		t.Fatal("forged original tool binding was accepted")
	}
}

func TestWorkerReceiptEnvelopeCanRoundTrip(t *testing.T) {
	record := workerFactRecord(t, []string{"true"})
	fact, files := passingWorkerFact(t, record, "ev-json", "native")
	data, err := json.Marshal(fact)
	if err != nil {
		t.Fatal(err)
	}
	var decoded WorkerCheckFact
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	result := AcceptedResult{EvidenceID: "ev-json", WorkerCheckFacts: []WorkerCheckFact{decoded}}
	result.ToolClosureDigest, _ = WorkerClosureDigest(record, result.WorkerCheckFacts)
	if err := ValidateWorkerClosure(record, result, func(ref string) ([]byte, error) { return files[ref], nil }); err != nil {
		t.Fatalf("round-tripped native fact failed durable verification: %v", err)
	}
}

func TestAcceptedHistoryUsesAnswerAndToolClosureIdentity(t *testing.T) {
	state, baseRecord, firstRef, files := acceptedFixture(t)
	record := baseRecord
	profileRecord := workerFactRecord(t, []string{"true"})
	record.ClosingContract = profileRecord.ClosingContract
	record.IntegrationCheckMode = "post_merge"
	if err := assignmentrecord.ValidateRecord(record); err != nil {
		t.Fatal(err)
	}
	state["entities"].(map[string]any)["assignments"] = []any{mustRecordMap(t, record)}

	var first AcceptedResult
	if err := json.Unmarshal(files[firstRef], &first); err != nil {
		t.Fatal(err)
	}
	first.AssignmentRecordSHA256 = mustRecordDigest(t, record)
	first.Answer.ClosingAssessment = validClosingAssessment()
	first.AnswerDigest, _ = AnswerDigest(first.Answer)
	plan := map[string]any{
		"message_type": "plan_report", "agent_id": record.OwnerAgentID, "runtime_id": record.RuntimeID,
		"assignment_id": record.AssignmentID, "assignment_revision": record.Revision,
		"assignment_record_sha256": mustRecordDigest(t, record),
		"assertion_checks": []any{map[string]any{
			"assertion":         first.Answer.AssertionEvidence[0].Assertion,
			"oracle":            first.Answer.AssertionEvidence[0].Oracle,
			"done_when_indexes": []int{0},
		}},
	}
	planBytes, _ := json.Marshal(plan)
	first.PlanSHA256 = digestBytes(planBytes)
	files[first.PlanRef] = planBytes
	first.WorkerCheckFacts = []WorkerCheckFact{missingWorkerFact(record, 0, "workspace")}
	first.ToolClosureDigest, _ = WorkerClosureDigest(record, first.WorkerCheckFacts)
	first.ResultRevision, first.SubmissionKey, first.EvidenceID = 1, "message-one", "ev-result-1"
	firstBody, _ := json.Marshal(first)
	files[firstRef] = firstBody
	state["evidence"].([]any)[0].(map[string]any)["sha256"] = digestBytes(firstBody)

	second := first
	second.ResultRevision, second.SubmissionKey, second.EvidenceID = 2, "message-two", "ev-result-2"
	second.WorkerCheckFacts = append([]WorkerCheckFact{}, first.WorkerCheckFacts...)
	second.WorkerCheckFacts[0].Status = "unavailable"
	second.ToolClosureDigest, _ = WorkerClosureDigest(record, second.WorkerCheckFacts)
	secondRef := strings.TrimSuffix(firstRef, "result-1.json") + "result-2.json"
	secondBody, _ := json.Marshal(second)
	files[secondRef] = secondBody
	state["evidence"] = append(state["evidence"].([]any), map[string]any{
		"id": second.EvidenceID, "kind": "completion_report", "path": secondRef,
		"sha256": digestBytes(secondBody), "status": "valid", "baseline_generation": record.BaselineGeneration,
		"responsibility_id": "BUILD-WORK-PACKAGE", "produced_by": []any{record.OwnerAgentID},
	})
	read := func(ref string) ([]byte, error) {
		body, ok := files[ref]
		if !ok {
			return nil, fmt.Errorf("missing %s", ref)
		}
		return body, nil
	}
	history, err := AcceptedHistory(state, record, read)
	if err != nil || len(history) != 2 || history[0].AnswerDigest != history[1].AnswerDigest || history[0].ToolClosureDigest == history[1].ToolClosureDigest {
		t.Fatalf("new machine closure should form a distinct Result revision: history=%d err=%v", len(history), err)
	}

	third := second
	third.ResultRevision, third.SubmissionKey, third.EvidenceID = 3, "message-three", "ev-result-3"
	thirdRef := strings.TrimSuffix(firstRef, "result-1.json") + "result-3.json"
	thirdBody, _ := json.Marshal(third)
	files[thirdRef] = thirdBody
	state["evidence"] = append(state["evidence"].([]any), map[string]any{
		"id": third.EvidenceID, "kind": "completion_report", "path": thirdRef,
		"sha256": digestBytes(thirdBody), "status": "valid", "baseline_generation": record.BaselineGeneration,
		"responsibility_id": "BUILD-WORK-PACKAGE", "produced_by": []any{record.OwnerAgentID},
	})
	if _, err := AcceptedHistory(state, record, read); err == nil {
		t.Fatal("duplicate answer plus identical tool closure was accepted as another revision")
	}
}
