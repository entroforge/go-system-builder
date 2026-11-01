package runtime_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
)

func TestApplyRecoveryQuarantinesMalformedBOMStateAndAppliesCandidate(t *testing.T) {
	root, statePath, journalPath := recoveryPaths(t)
	activeState := append([]byte{0xef, 0xbb, 0xbf}, []byte(`{"broken":`)...)
	activeJournal := []byte("not-json\n")
	writeRecoveryFile(t, statePath, activeState)
	writeRecoveryFile(t, journalPath, activeJournal)

	candidateState, candidateJournal := recoveryCandidates(t)
	result, err := runtime.ApplyRecovery(runtime.RecoveryRequest{
		Root:             root,
		StatePath:        statePath,
		JournalPath:      journalPath,
		PlanID:           "plan-rb-001",
		CandidateState:   candidateState,
		CandidateJournal: candidateJournal,
		Approver:         "operator@example.test",
		OccurredAt:       time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC),
		Validator:        recoveryValidator{},
	})
	if err != nil {
		t.Fatalf("ApplyRecovery() error = %v", err)
	}
	if !result.Applied || result.Idempotent {
		t.Fatalf("result = %+v, want first apply", result)
	}
	if result.QuarantineDir == "" || result.ManifestPath == "" {
		t.Fatalf("result = %+v, want quarantine and manifest paths", result)
	}

	if got := mustReadRecoveryFile(t, statePath); !json.Valid(got) {
		t.Fatalf("active state is not valid JSON: %q", got)
	}
	assertRecoveredPair(t, statePath, journalPath, candidateState, candidateJournal, "plan-rb-001")
	if got := mustReadRecoveryFile(t, filepath.Join(result.QuarantineDir, "loop-state.json")); string(got) != string(activeState) {
		t.Fatalf("quarantined state = %q, want original bytes %q", got, activeState)
	}
	if got := mustReadRecoveryFile(t, filepath.Join(result.QuarantineDir, "loop-events.jsonl")); string(got) != string(activeJournal) {
		t.Fatalf("quarantined journal = %q, want original bytes %q", got, activeJournal)
	}
	if _, err := os.Stat(statePath + ".recovery-pending.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovery marker remains: %v", err)
	}
}

func TestApplyRecoveryQuarantinesAndRetiresStaleRuntimePendingMarkers(t *testing.T) {
	root, statePath, journalPath := recoveryPaths(t)
	writeRecoveryFile(t, statePath, []byte("damaged-state\n"))
	writeRecoveryFile(t, journalPath, []byte("damaged-journal\n"))
	commitPendingPath := statePath + ".commit-pending.json"
	commitPending := []byte(`{"old":"commit-pending"}`)
	writeRecoveryFile(t, commitPendingPath, commitPending)
	candidateState, candidateJournal := recoveryCandidates(t)
	req := recoveryRequest(root, statePath, journalPath, candidateState, candidateJournal, "plan-stale-pending", "plan-sha-stale-pending")

	result, err := runtime.ApplyRecovery(req)
	if err != nil {
		t.Fatalf("ApplyRecovery() error = %v", err)
	}
	if _, err := os.Stat(commitPendingPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale commit pending marker remains: %v", err)
	}
	if len(result.Manifest.SourcePending) != 1 {
		t.Fatalf("source pending artifacts = %#v, want one", result.Manifest.SourcePending)
	}
	quarantined := result.Manifest.SourcePending[0].QuarantinePath
	if got := mustReadRecoveryFile(t, quarantined); string(got) != string(commitPending) {
		t.Fatalf("quarantined pending bytes = %q, want %q", got, commitPending)
	}
}

func TestApplyRecoveryRejectsTamperedSourcePendingManifest(t *testing.T) {
	root, statePath, journalPath := recoveryPaths(t)
	writeRecoveryFile(t, statePath, []byte("damaged-state\n"))
	writeRecoveryFile(t, journalPath, []byte("damaged-journal\n"))
	commitPendingPath := statePath + ".commit-pending.json"
	commitPending := []byte(`{"old":"commit-pending"}`)
	writeRecoveryFile(t, commitPendingPath, commitPending)
	candidateState, candidateJournal := recoveryCandidates(t)
	req := recoveryRequest(root, statePath, journalPath, candidateState, candidateJournal, "plan-pending-manifest", "plan-sha-pending-manifest")
	req.FailureInjector = recoveryFailureFunc(func(step runtime.RecoveryFailureStep) error {
		if step == runtime.RecoveryBeforeStateReplace {
			return errors.New("simulated interruption before state replacement")
		}
		return nil
	})
	if _, err := runtime.ApplyRecovery(req); !errors.Is(err, runtime.ErrRecoveryInjectedFailure) {
		t.Fatalf("initial apply error = %v, want injected interruption", err)
	}

	markerPath := statePath + ".recovery-pending.json"
	marker := readRecoveryJSONMap(t, markerPath)
	markerManifest, ok := marker["manifest"].(map[string]any)
	if !ok {
		t.Fatalf("pending marker manifest = %#v, want object", marker["manifest"])
	}
	markerManifest["source_pending"] = []any{}
	writeRecoveryJSONMap(t, markerPath, marker)

	_, err := runtime.ApplyRecovery(reqWithoutFault(req))
	if err == nil || !errors.Is(err, runtime.ErrRecoveryPending) || !strings.Contains(err.Error(), "source_pending") {
		t.Fatalf("tampered source pending manifest error = %v, want pending source manifest rejection", err)
	}
	if _, err := os.Stat(commitPendingPath); err != nil {
		t.Fatalf("source pending marker was retired after manifest tampering: %v", err)
	}
}

func TestApplyRecoveryCreatesMissingActivePair(t *testing.T) {
	root := t.TempDir()
	writeRecoveryDefinition(t, root)
	statePath := filepath.Join(root, "missing", "loop-state.json")
	journalPath := filepath.Join(root, "missing", "loop-events.jsonl")
	candidateState, candidateJournal := recoveryCandidates(t)

	result, err := runtime.ApplyRecovery(recoveryRequest(root, statePath, journalPath, candidateState, candidateJournal, "plan-missing-001", "plan-sha-missing-001"))
	if err != nil {
		t.Fatalf("ApplyRecovery() error = %v", err)
	}
	if !result.Applied {
		t.Fatalf("result = %+v, want applied recovery", result)
	}
	assertRecoveredPair(t, statePath, journalPath, candidateState, candidateJournal, "plan-missing-001")
}

func TestApplyRecoveryRejectsInvalidCandidateBeforeQuarantineOrActiveWrite(t *testing.T) {
	root, statePath, journalPath := recoveryPaths(t)
	activeState := []byte("active-state\n")
	activeJournal := []byte("active-journal\n")
	writeRecoveryFile(t, statePath, activeState)
	writeRecoveryFile(t, journalPath, activeJournal)
	candidateState, candidateJournal := recoveryCandidates(t)

	var state map[string]any
	if err := json.Unmarshal(candidateState, &state); err != nil {
		t.Fatal(err)
	}
	state["lifecycle"].(map[string]any)["phase"] = "invalid_semantic_phase"
	invalidState, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}

	_, err = runtime.ApplyRecovery(runtime.RecoveryRequest{
		Root:             root,
		StatePath:        statePath,
		JournalPath:      journalPath,
		PlanID:           "plan-invalid-candidate",
		CandidateState:   invalidState,
		CandidateJournal: candidateJournal,
		Approver:         "operator@example.test",
		OccurredAt:       time.Now().UTC(),
		Validator:        recoveryValidator{},
	})
	if !errors.Is(err, runtime.ErrRecoveryCandidateInvalid) {
		t.Fatalf("error = %v, want ErrRecoveryCandidateInvalid", err)
	}
	if got := mustReadRecoveryFile(t, statePath); string(got) != string(activeState) {
		t.Fatalf("active state changed after invalid candidate: %q", got)
	}
	if got := mustReadRecoveryFile(t, journalPath); string(got) != string(activeJournal) {
		t.Fatalf("active journal changed after invalid candidate: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "recovery", "quarantine")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("quarantine created for invalid candidate: %v", err)
	}
}

func TestApplyRecoveryRetriesAfterStateReplacementBeforeJournalReplacement(t *testing.T) {
	root, statePath, journalPath := recoveryPaths(t)
	writeRecoveryFile(t, statePath, []byte("old-state\n"))
	writeRecoveryFile(t, journalPath, []byte("old-journal\n"))
	candidateState, candidateJournal := recoveryCandidates(t)
	req := recoveryRequest(root, statePath, journalPath, candidateState, candidateJournal, "plan-retry-001", "plan-sha-retry-001")
	req.FailureInjector = recoveryFailureFunc(func(step runtime.RecoveryFailureStep) error {
		if step == runtime.RecoveryBeforeStateReplace {
			if _, statErr := os.Stat(statePath + ".recovery-pending.json"); statErr != nil {
				return statErr
			}
		}
		if step == runtime.RecoveryAfterStateReplace {
			return errors.New("simulated interruption after state replacement")
		}
		return nil
	})

	_, err := runtime.ApplyRecovery(req)
	if !errors.Is(err, runtime.ErrRecoveryInjectedFailure) {
		t.Fatalf("first apply error = %v, want injected failure", err)
	}
	var interrupted, original map[string]any
	if err := json.Unmarshal(mustReadRecoveryFile(t, statePath), &interrupted); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(candidateState, &original); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(interrupted["entities"], original["entities"]) || !reflect.DeepEqual(interrupted["bound_req"], original["bound_req"]) {
		t.Fatal("interrupted recovery did not retain original responsibilities")
	}
	if got := mustReadRecoveryFile(t, journalPath); string(got) != "old-journal\n" {
		t.Fatalf("journal after interruption = %q, want old journal", got)
	}
	if _, err := os.Stat(statePath + ".recovery-pending.json"); err != nil {
		t.Fatalf("pending marker missing after interruption: %v", err)
	}

	retry, err := runtime.ApplyRecovery(reqWithoutFault(req))
	if err != nil {
		t.Fatalf("retry error = %v", err)
	}
	if !retry.Applied || !retry.Retried {
		t.Fatalf("retry result = %+v, want applied retry", retry)
	}
	assertRecoveredPair(t, statePath, journalPath, candidateState, candidateJournal, req.PlanID)
	if _, err := os.Stat(statePath + ".recovery-pending.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending marker remains after retry: %v", err)
	}
}

func TestApplyRecoveryPendingRetryUsesDurableMarkerWithoutRequestCandidate(t *testing.T) {
	root, statePath, journalPath := recoveryPaths(t)
	writeRecoveryFile(t, statePath, []byte("old-state\n"))
	writeRecoveryFile(t, journalPath, []byte("old-journal\n"))
	candidateState, candidateJournal := recoveryCandidates(t)
	req := recoveryRequest(root, statePath, journalPath, candidateState, candidateJournal, "plan-marker-only", "plan-sha-marker-only")
	req.FailureInjector = recoveryFailureFunc(func(step runtime.RecoveryFailureStep) error {
		if step == runtime.RecoveryBeforeStateReplace {
			return errors.New("simulated interruption after durable marker")
		}
		return nil
	})
	if _, err := runtime.ApplyRecovery(req); !errors.Is(err, runtime.ErrRecoveryInjectedFailure) {
		t.Fatalf("initial apply error = %v, want injected interruption", err)
	}

	retry := reqWithoutFault(req)
	retry.CandidateState = nil
	retry.CandidateJournal = nil
	result, err := runtime.ApplyRecovery(retry)
	if err != nil {
		t.Fatalf("marker-only retry error = %v", err)
	}
	if !result.Applied || !result.Retried {
		t.Fatalf("marker-only retry result = %+v, want applied retry", result)
	}
	assertRecoveredPair(t, statePath, journalPath, candidateState, candidateJournal, req.PlanID)
}

func TestApplyRecoveryPendingRetryRevalidatesMarkerCandidate(t *testing.T) {
	root, statePath, journalPath := recoveryPaths(t)
	oldState := []byte("old-state\n")
	oldJournal := []byte("old-journal\n")
	writeRecoveryFile(t, statePath, oldState)
	writeRecoveryFile(t, journalPath, oldJournal)
	candidateState, candidateJournal := recoveryCandidates(t)
	req := recoveryRequest(root, statePath, journalPath, candidateState, candidateJournal, "plan-marker-validation", "plan-sha-marker-validation")
	req.FailureInjector = recoveryFailureFunc(func(step runtime.RecoveryFailureStep) error {
		if step == runtime.RecoveryBeforeStateReplace {
			return errors.New("simulated interruption after durable marker")
		}
		return nil
	})
	if _, err := runtime.ApplyRecovery(req); !errors.Is(err, runtime.ErrRecoveryInjectedFailure) {
		t.Fatalf("initial apply error = %v, want injected interruption", err)
	}

	var invalid map[string]any
	if err := json.Unmarshal(candidateState, &invalid); err != nil {
		t.Fatal(err)
	}
	invalid["lifecycle"].(map[string]any)["phase"] = "invalid_semantic_phase"
	invalidBytes, err := json.Marshal(invalid)
	if err != nil {
		t.Fatal(err)
	}
	markerPath := statePath + ".recovery-pending.json"
	marker := readRecoveryJSONMap(t, markerPath)
	marker["candidate_state_base64"] = base64.StdEncoding.EncodeToString(invalidBytes)
	writeRecoveryJSONMap(t, markerPath, marker)

	retry := reqWithoutFault(req)
	retry.CandidateState = nil
	retry.CandidateJournal = nil
	_, err = runtime.ApplyRecovery(retry)
	if !errors.Is(err, runtime.ErrRecoveryCandidateInvalid) {
		t.Fatalf("tampered marker candidate error = %v, want ErrRecoveryCandidateInvalid", err)
	}
	if got := mustReadRecoveryFile(t, statePath); string(got) != string(oldState) {
		t.Fatalf("invalid marker changed active state: %q", got)
	}
	if got := mustReadRecoveryFile(t, journalPath); string(got) != string(oldJournal) {
		t.Fatalf("invalid marker changed active journal: %q", got)
	}
}

func TestApplyRecoveryRejectsActivePathsOutsideRepository(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	statePath := filepath.Join(outside, "loop-state.json")
	journalPath := filepath.Join(outside, "loop-events.jsonl")
	candidateState, candidateJournal := recoveryCandidates(t)
	_, err := runtime.ApplyRecovery(recoveryRequest(root, statePath, journalPath, candidateState, candidateJournal, "plan-outside-root", "plan-sha-outside-root"))
	if !errors.Is(err, runtime.ErrRecoveryCandidateInvalid) {
		t.Fatalf("outside-root paths error = %v, want ErrRecoveryCandidateInvalid", err)
	}
	if _, statErr := os.Stat(statePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("outside-root state was touched: %v", statErr)
	}
	if _, statErr := os.Stat(journalPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("outside-root journal was touched: %v", statErr)
	}
}

func TestApplyRecoveryLeavesRetiredFingerprintFileUntouched(t *testing.T) {
	root, statePath, journalPath := recoveryPaths(t)
	writeRecoveryFile(t, statePath, []byte("damaged-state\n"))
	writeRecoveryFile(t, journalPath, []byte("damaged-journal\n"))
	old := statePath + ".fingerprint-pending.json"
	original := []byte(`{"old":"private historical material"}`)
	writeRecoveryFile(t, old, original)
	candidateState, candidateJournal := recoveryCandidates(t)
	req := recoveryRequest(root, statePath, journalPath, candidateState, candidateJournal, "plan-retired-file", "plan-recovery")
	result, err := runtime.ApplyRecovery(req)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustReadRecoveryFile(t, old); string(got) != string(original) {
		t.Fatal("recovery changed historical file")
	}
	if len(result.Manifest.SourcePending) != 0 {
		t.Fatalf("historical file treated as active pending: %#v", result.Manifest.SourcePending)
	}
}

func TestApplyRecoveryRetriesAfterQuarantineWithDifferentCallTime(t *testing.T) {
	root, statePath, journalPath := recoveryPaths(t)
	writeRecoveryFile(t, statePath, []byte("old-state\n"))
	writeRecoveryFile(t, journalPath, []byte("old-journal\n"))
	candidateState, candidateJournal := recoveryCandidates(t)
	req := recoveryRequest(root, statePath, journalPath, candidateState, candidateJournal, "plan-quarantine-retry", "plan-sha-quarantine-retry")
	req.FailureInjector = recoveryFailureFunc(func(step runtime.RecoveryFailureStep) error {
		if step == runtime.RecoveryBeforePendingMarker {
			return errors.New("simulated interruption after quarantine")
		}
		return nil
	})
	if _, err := runtime.ApplyRecovery(req); !errors.Is(err, runtime.ErrRecoveryInjectedFailure) {
		t.Fatalf("first apply error = %v, want injected failure", err)
	}

	retry := reqWithoutFault(req)
	retry.OccurredAt = retry.OccurredAt.Add(time.Hour)
	if _, err := runtime.ApplyRecovery(retry); err != nil {
		t.Fatalf("retry after durable quarantine with new call time: %v", err)
	}
}

func TestApplyRecoveryRejectsDifferentPlanWhilePendingAndRepairsOutputMismatch(t *testing.T) {
	root, statePath, journalPath := recoveryPaths(t)
	writeRecoveryFile(t, statePath, []byte("old-state\n"))
	writeRecoveryFile(t, journalPath, []byte("old-journal\n"))
	candidateState, candidateJournal := recoveryCandidates(t)
	req := recoveryRequest(root, statePath, journalPath, candidateState, candidateJournal, "plan-pending-001", "plan-sha-pending-001")
	req.FailureInjector = recoveryFailureFunc(func(step runtime.RecoveryFailureStep) error {
		if step == runtime.RecoveryBeforeStateReplace {
			return errors.New("simulated interruption before state replacement")
		}
		return nil
	})
	if _, err := runtime.ApplyRecovery(req); !errors.Is(err, runtime.ErrRecoveryInjectedFailure) {
		t.Fatalf("pending setup error = %v, want injected failure", err)
	}

	other := recoveryRequest(root, statePath, journalPath, candidateState, candidateJournal, "plan-pending-002", "plan-sha-pending-002")
	if _, err := runtime.ApplyRecovery(other); !errors.Is(err, runtime.ErrRecoveryConflict) {
		t.Fatalf("different pending plan error = %v, want ErrRecoveryConflict", err)
	}

	req = reqWithoutFault(req)
	req.FailureInjector = recoveryFailureFunc(func(step runtime.RecoveryFailureStep) error {
		if step == runtime.RecoveryAfterStateReplace {
			writeRecoveryFile(t, statePath, []byte("corrupted-after-write\n"))
		}
		return nil
	})
	if _, err := runtime.ApplyRecovery(req); !errors.Is(err, runtime.ErrRecoveryOutputMismatch) {
		t.Fatalf("output mismatch error = %v, want ErrRecoveryOutputMismatch", err)
	}
	retry, err := runtime.ApplyRecovery(reqWithoutFault(req))
	if err != nil || !retry.Applied || len(retry.Manifest.UncertainSources) == 0 {
		t.Fatalf("retry did not preserve unknown state: %+v %v", retry, err)
	}
	found := false
	for _, item := range retry.Manifest.UncertainSources {
		if string(mustReadRecoveryFile(t, item.QuarantinePath)) == "corrupted-after-write\n" {
			found = true
		}
	}
	if !found {
		t.Fatal("unknown state was not preserved")
	}
}

func TestApplyRecoveryIsIdempotentWhenManifestWriteCompletedBeforeMarkerClear(t *testing.T) {
	root, statePath, journalPath := recoveryPaths(t)
	writeRecoveryFile(t, statePath, []byte("old-state\n"))
	writeRecoveryFile(t, journalPath, []byte("old-journal\n"))
	candidateState, candidateJournal := recoveryCandidates(t)
	req := recoveryRequest(root, statePath, journalPath, candidateState, candidateJournal, "plan-manifest-001", "plan-sha-manifest-001")
	req.FailureInjector = recoveryFailureFunc(func(step runtime.RecoveryFailureStep) error {
		if step == runtime.RecoveryBeforeManifest {
			return errors.New("simulated interruption before manifest")
		}
		return nil
	})
	if _, err := runtime.ApplyRecovery(req); !errors.Is(err, runtime.ErrRecoveryInjectedFailure) {
		t.Fatalf("manifest setup error = %v, want injected failure", err)
	}

	result, err := runtime.ApplyRecovery(reqWithoutFault(req))
	if err != nil {
		t.Fatalf("first manifest retry error = %v", err)
	}
	manifestPath := result.ManifestPath
	manifestBefore := mustReadRecoveryFile(t, manifestPath)
	if _, err := runtime.ApplyRecovery(reqWithoutFault(req)); err != nil {
		t.Fatalf("second manifest retry error = %v", err)
	}
	if manifestAfter := mustReadRecoveryFile(t, manifestPath); string(manifestAfter) != string(manifestBefore) {
		t.Fatal("idempotent retry rewrote the applied manifest")
	}
}

func TestApplyRecoveryIsIdempotentAndAllowsDifferentCompletedPlan(t *testing.T) {
	root, statePath, journalPath := recoveryPaths(t)
	writeRecoveryFile(t, statePath, []byte("damaged\n"))
	writeRecoveryFile(t, journalPath, []byte("damaged-journal\n"))
	candidateState, candidateJournal := recoveryCandidates(t)
	req := recoveryRequest(root, statePath, journalPath, candidateState, candidateJournal, "plan-idempotent-001", "plan-sha-idempotent-001")

	first, err := runtime.ApplyRecovery(req)
	if err != nil {
		t.Fatalf("first apply error = %v", err)
	}
	second, err := runtime.ApplyRecovery(req)
	if err != nil {
		t.Fatalf("same-plan apply error = %v", err)
	}
	if !second.Applied || !second.Idempotent {
		t.Fatalf("same-plan result = %+v, want idempotent success", second)
	}
	if second.ManifestPath != first.ManifestPath || second.QuarantineDir != first.QuarantineDir {
		t.Fatalf("same-plan result paths changed: first=%+v second=%+v", first, second)
	}

	other := recoveryRequest(root, statePath, journalPath, mustReadRecoveryFile(t, statePath), mustReadRecoveryFile(t, journalPath), "plan-idempotent-002", "plan-sha-idempotent-002")
	otherResult, err := runtime.ApplyRecovery(other)
	if err != nil {
		t.Fatalf("different completed plan error = %v", err)
	}
	if !otherResult.Applied || otherResult.Idempotent || otherResult.ManifestPath == first.ManifestPath {
		t.Fatalf("different completed plan result = %+v, want a new applied epoch", otherResult)
	}
}

func TestApplyRecoveryAllowsNewPlanAfterAppliedEpochAndIgnoresSourceDriftForSamePlan(t *testing.T) {
	root, statePath, journalPath := recoveryPaths(t)
	oldState := []byte("damaged-before-first-recovery\n")
	oldJournal := []byte("damaged-journal-before-first-recovery\n")
	writeRecoveryFile(t, statePath, oldState)
	writeRecoveryFile(t, journalPath, oldJournal)
	candidateA, candidateJournal := recoveryCandidates(t)
	firstRequest := recoveryRequest(root, statePath, journalPath, candidateA, candidateJournal, "plan-epoch-001", "plan-sha-epoch-001")
	first, err := runtime.ApplyRecovery(firstRequest)
	if err != nil {
		t.Fatalf("first plan apply error = %v", err)
	}

	samePlan, err := runtime.ApplyRecovery(firstRequest)
	if err != nil {
		t.Fatalf("same plan apply after source drift error = %v", err)
	}
	if !samePlan.Idempotent || samePlan.ManifestPath != first.ManifestPath {
		t.Fatalf("same plan result = %+v, want idempotent result", samePlan)
	}

	candidateB := candidateStateVariant(t, mustReadRecoveryFile(t, statePath), "2026-08-13T11:00:00Z")
	secondRequest := recoveryRequest(root, statePath, journalPath, candidateB, mustReadRecoveryFile(t, journalPath), "plan-epoch-002", "plan-sha-epoch-002")
	second, err := runtime.ApplyRecovery(secondRequest)
	if err != nil {
		t.Fatalf("new plan apply after completed epoch error = %v", err)
	}
	if !second.Applied || second.Idempotent || second.ManifestPath == first.ManifestPath || second.QuarantineDir == first.QuarantineDir {
		t.Fatalf("new plan result = %+v, want a new applied recovery epoch", second)
	}
}

func TestApplyRecoverySerializesOnSharedRuntimeLock(t *testing.T) {
	root, statePath, journalPath := recoveryPaths(t)
	writeRecoveryFile(t, statePath, []byte("damaged\n"))
	writeRecoveryFile(t, journalPath, []byte("damaged-journal\n"))
	candidateState, candidateJournal := recoveryCandidates(t)
	req := recoveryRequest(root, statePath, journalPath, candidateState, candidateJournal, "plan-lock-001", "plan-sha-lock-001")
	req.LockTimeout = 20 * time.Millisecond
	if err := os.WriteFile(statePath+".lock", []byte("held-by-test"), 0o600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(statePath + ".lock")
	if _, err := runtime.ApplyRecovery(req); err == nil {
		t.Fatal("ApplyRecovery succeeded while shared runtime lock was held")
	}
}

func recoveryRequest(root, statePath, journalPath string, candidateState, candidateJournal []byte, planID, planSHA string) runtime.RecoveryRequest {
	return runtime.RecoveryRequest{
		Root:             root,
		StatePath:        statePath,
		JournalPath:      journalPath,
		PlanID:           planID,
		CandidateState:   candidateState,
		CandidateJournal: candidateJournal,
		Approver:         "operator@example.test",
		OccurredAt:       time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC),
		Validator:        recoveryValidator{},
	}
}

func reqWithoutFault(req runtime.RecoveryRequest) runtime.RecoveryRequest {
	req.FailureInjector = nil
	return req
}

func recoveryPaths(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	claudeDir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRecoveryDefinition(t, root)
	return root, filepath.Join(claudeDir, "loop-state.json"), filepath.Join(claudeDir, "loop-events.jsonl")
}

func writeRecoveryDefinition(t *testing.T, root string) {
	t.Helper()
	document, err := os.ReadFile(filepath.Join("..", "..", "docs", "control", "loop-definition.json"))
	if err != nil {
		t.Fatal(err)
	}
	docsDir := filepath.Join(root, "docs", "control")
	if err := os.MkdirAll(docsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "loop-definition.json"), document, 0o644); err != nil {
		t.Fatal(err)
	}
}

func recoveryCandidates(t *testing.T) ([]byte, []byte) {
	t.Helper()
	state, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		t.Fatal(err)
	}
	journal, err := schema.ReadAsset("loop-event.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal(journal, &event); err != nil {
		t.Fatal(err)
	}
	stateValue := map[string]any{}
	if err := json.Unmarshal(state, &stateValue); err != nil {
		t.Fatal(err)
	}
	state, err = json.MarshalIndent(stateValue, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	state = append(state, '\n')
	journal, err = json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	return state, append(journal, '\n')
}

func candidateStateVariant(t *testing.T, candidate []byte, updatedAt string) []byte {
	t.Helper()
	var state map[string]any
	if err := json.Unmarshal(candidate, &state); err != nil {
		t.Fatal(err)
	}
	state["updated_at"] = updatedAt
	variant, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(variant, '\n')
}

func writeRecoveryFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustReadRecoveryFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readRecoveryJSONMap(t *testing.T, path string) map[string]any {
	t.Helper()
	data := mustReadRecoveryFile(t, path)
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func writeRecoveryJSONMap(t *testing.T, path string, value map[string]any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeRecoveryFile(t, path, append(data, '\n'))
}

type recoveryValidator struct{}

func (recoveryValidator) ValidateCandidate(_ string, state map[string]any) error {
	if state == nil || state["runtime_id"] == nil {
		return errors.New("candidate runtime is empty")
	}
	lifecycle, ok := state["lifecycle"].(map[string]any)
	if !ok {
		return errors.New("candidate lifecycle is missing")
	}
	if lifecycle["phase"] == "invalid_semantic_phase" {
		return errors.New("candidate phase is semantically invalid")
	}
	return nil
}

type recoveryFailureFunc func(runtime.RecoveryFailureStep) error

func (f recoveryFailureFunc) Inject(step runtime.RecoveryFailureStep) error {
	return f(step)
}

var _ runtime.RecoveryFailureInjector = recoveryFailureFunc(nil)

func TestApplyRecoveryRejectsRetiredRequirementBeforeQuarantine(t *testing.T) {
	for _, location := range []string{"active", "candidate"} {
		t.Run(location, func(t *testing.T) {
			root, statePath, journalPath := recoveryPaths(t)
			candidate, journal := recoveryCandidates(t)
			var state map[string]any
			if err := json.Unmarshal(candidate, &state); err != nil {
				t.Fatal(err)
			}
			state["bound_req"].(map[string]any)["path"] = "docs/product/requirements/REQ-002-loop-engineering-self-evolution.md"
			old, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			active := []byte("broken active state\n")
			if location == "active" {
				active = old
			} else {
				candidate = old
			}
			writeRecoveryFile(t, statePath, active)
			writeRecoveryFile(t, journalPath, []byte("journal unchanged\n"))
			_, err = runtime.ApplyRecovery(recoveryRequest(root, statePath, journalPath, candidate, journal, "retired-req", "retired-req-sha"))
			if err == nil || !strings.Contains(err.Error(), "layout migration required") {
				t.Fatalf("retired path accepted: %v", err)
			}
			if string(mustReadRecoveryFile(t, statePath)) != string(active) {
				t.Fatal("active state changed")
			}
			if string(mustReadRecoveryFile(t, journalPath)) != "journal unchanged\n" {
				t.Fatal("journal changed")
			}
			if _, err := os.Stat(filepath.Join(root, ".claude/recovery/quarantine")); !os.IsNotExist(err) {
				t.Fatalf("quarantine created: %v", err)
			}
		})
	}
}

func TestApplyRecoveryWithoutDigestsResumesSavedCandidate(t *testing.T) {
	root, statePath, journalPath := recoveryPaths(t)
	originalState, originalJournal := []byte("old-state\n"), []byte("old-journal\n")
	writeRecoveryFile(t, statePath, originalState)
	writeRecoveryFile(t, journalPath, originalJournal)
	state, journal := recoveryCandidates(t)
	req := recoveryRequest(root, statePath, journalPath, state, journal, "plan-no-digest", "")
	req.FailureInjector = recoveryFailureFunc(func(step runtime.RecoveryFailureStep) error {
		if step == runtime.RecoveryBeforeStateReplace {
			return errors.New("interruption")
		}
		return nil
	})
	if _, err := runtime.ApplyRecovery(req); !errors.Is(err, runtime.ErrRecoveryInjectedFailure) {
		t.Fatalf("expected interruption: %v", err)
	}
	marker := mustReadRecoveryFile(t, statePath+".recovery-pending.json")
	for _, field := range []string{"plan_sha256", "candidate_state_sha256", "candidate_journal_sha256"} {
		if strings.Contains(string(marker), `"`+field+`"`) {
			t.Fatalf("marker contains obsolete digest %s", field)
		}
	}
	retry := reqWithoutFault(req)
	retry.CandidateState, retry.CandidateJournal = nil, nil
	result, err := runtime.ApplyRecovery(retry)
	if err != nil || !result.Applied || !result.Retried {
		t.Fatalf("resume: %#v, %v", result, err)
	}
	if got := mustReadRecoveryFile(t, filepath.Join(result.QuarantineDir, "loop-state.json")); string(got) != string(originalState) {
		t.Fatal("source state not preserved")
	}
	if got := mustReadRecoveryFile(t, filepath.Join(result.QuarantineDir, "loop-events.jsonl")); string(got) != string(originalJournal) {
		t.Fatal("source journal not preserved")
	}
	assertRecoveredPair(t, statePath, journalPath, state, journal, req.PlanID)
}

func TestRecoveryQuarantinesCurrentSourceWithoutFullContentGate(t *testing.T) {
	root, statePath, journalPath := recoveryPaths(t)
	currentState, currentJournal := []byte("current-damaged-state"), []byte("current-damaged-journal")
	writeRecoveryFile(t, statePath, currentState)
	writeRecoveryFile(t, journalPath, currentJournal)
	state, journal := recoveryCandidates(t)
	req := recoveryRequest(root, statePath, journalPath, state, journal, "plan-current-original", "")
	result, err := runtime.ApplyRecovery(req)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustReadRecoveryFile(t, filepath.Join(result.QuarantineDir, "loop-state.json")); string(got) != string(currentState) {
		t.Fatal("current original state was not preserved")
	}
	if got := mustReadRecoveryFile(t, filepath.Join(result.QuarantineDir, "loop-events.jsonl")); string(got) != string(currentJournal) {
		t.Fatal("current original journal was not preserved")
	}
	assertRecoveredPair(t, statePath, journalPath, state, journal, req.PlanID)
}

func TestRecoveryGuardProtectsActiveWriterAndResumesPending(t *testing.T) {
	root, statePath, journalPath := recoveryPaths(t)
	originalState, originalJournal := []byte("old-state\n"), []byte("old-journal\n")
	writeRecoveryFile(t, statePath, originalState)
	writeRecoveryFile(t, journalPath, originalJournal)
	state, journal := recoveryCandidates(t)
	req := recoveryRequest(root, statePath, journalPath, state, journal, "plan-guard", "")
	guardErr := errors.New("original worker owns its lease")
	req.ValidateRestore = func(map[string]any, []byte) error { return guardErr }
	if _, err := runtime.ApplyRecovery(req); !errors.Is(err, guardErr) {
		t.Fatalf("active original writer must remain protected: %v", err)
	}
	if got := mustReadRecoveryFile(t, statePath); string(got) != string(originalState) {
		t.Fatal("active source was overwritten")
	}
	calls := 0
	req.ValidateRestore = func(map[string]any, []byte) error { calls++; return nil }
	req.FailureInjector = recoveryFailureFunc(func(step runtime.RecoveryFailureStep) error {
		if step == runtime.RecoveryAfterStateReplace {
			return errors.New("interruption")
		}
		return nil
	})
	if _, err := runtime.ApplyRecovery(req); !errors.Is(err, runtime.ErrRecoveryInjectedFailure) {
		t.Fatalf("expected interruption: %v", err)
	}
	retry := reqWithoutFault(req)
	retry.CandidateState, retry.CandidateJournal = nil, nil
	result, err := runtime.ApplyRecovery(retry)
	if err != nil || !result.Applied || !result.Retried || calls != 2 {
		t.Fatalf("pending must resume under guard: %#v, calls %d, err %v", result, calls, err)
	}
}

// The recovery Writer records one completion event in the same Runtime pair.
func assertRecoveredPair(t *testing.T, statePath, journalPath string, candidateState, candidateJournal []byte, planID string) {
	t.Helper()
	var expected, actual map[string]any
	if err := json.Unmarshal(candidateState, &expected); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(mustReadRecoveryFile(t, statePath), &actual); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"runtime_id", "baseline", "bound_req", "lifecycle", "entities"} {
		if !reflect.DeepEqual(expected[key], actual[key]) {
			t.Fatalf("recovered %s differs from original responsibility", key)
		}
	}
	journal := mustReadRecoveryFile(t, journalPath)
	if !strings.HasPrefix(string(journal), string(candidateJournal)) || !strings.Contains(string(journal), `"transition_id":"RECOVERY-APPLIED"`) || !strings.Contains(string(journal), `"idempotency_key":"recovery:`+planID+`"`) {
		t.Fatal("original journal and Writer completion were not retained")
	}
}
