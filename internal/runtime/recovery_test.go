package runtime_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
		PlanSHA:          "plan-sha-001",
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
	if got := mustReadRecoveryFile(t, journalPath); string(got) != string(candidateJournal) {
		t.Fatalf("active journal = %q, want %q", got, candidateJournal)
	}
	if got := mustReadRecoveryFile(t, filepath.Join(result.QuarantineDir, "loop-state.json")); string(got) != string(activeState) {
		t.Fatalf("quarantined state = %q, want original bytes %q", got, activeState)
	}
	if got := mustReadRecoveryFile(t, filepath.Join(result.QuarantineDir, "loop-events.jsonl")); string(got) != string(activeJournal) {
		t.Fatalf("quarantined journal = %q, want original bytes %q", got, activeJournal)
	}
	if result.Manifest.SourceState.SHA256 != digest(activeState) || result.Manifest.SourceJournal.SHA256 != digest(activeJournal) {
		t.Fatalf("source hashes = %+v, want raw-byte hashes", result.Manifest)
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
	req.SourcePendingSHA256 = map[string]string{commitPendingPath: digest(commitPending)}

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
	req.SourcePendingSHA256 = map[string]string{commitPendingPath: digest(commitPending)}
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
	if got := mustReadRecoveryFile(t, statePath); string(got) != string(candidateState) {
		t.Fatalf("recovered missing state = %q, want candidate", got)
	}
	if got := mustReadRecoveryFile(t, journalPath); string(got) != string(candidateJournal) {
		t.Fatalf("recovered missing journal = %q, want candidate", got)
	}
}

func TestApplyRecoveryRejectsUnexpectedSourceCreatedAfterPlan(t *testing.T) {
	root, statePath, journalPath := recoveryPaths(t)
	unexpectedState := []byte("created-after-plan\n")
	writeRecoveryFile(t, statePath, unexpectedState)
	candidateState, candidateJournal := recoveryCandidates(t)
	expectedMissing := false
	req := recoveryRequest(root, statePath, journalPath, candidateState, candidateJournal, "plan-missing-source", "plan-sha-missing-source")
	req.SourceStateExists = &expectedMissing
	req.SourceJournalExists = &expectedMissing

	if _, err := runtime.ApplyRecovery(req); !errors.Is(err, runtime.ErrRecoveryInputDrift) {
		t.Fatalf("unexpected source creation error = %v, want ErrRecoveryInputDrift", err)
	}
	if got := mustReadRecoveryFile(t, statePath); string(got) != string(unexpectedState) {
		t.Fatalf("unexpected source drift changed active state: %q", got)
	}
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
		PlanSHA:          "plan-sha-invalid-candidate",
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
	if got := mustReadRecoveryFile(t, statePath); string(got) != string(candidateState) {
		t.Fatalf("state after interruption = %q, want candidate", got)
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
	if got := mustReadRecoveryFile(t, journalPath); string(got) != string(candidateJournal) {
		t.Fatalf("journal after retry = %q, want candidate", got)
	}
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
	retry.CandidateStateMap = nil
	retry.CandidateJournal = nil
	retry.CandidateStateSHA256 = ""
	retry.CandidateJournalSHA256 = ""
	result, err := runtime.ApplyRecovery(retry)
	if err != nil {
		t.Fatalf("marker-only retry error = %v", err)
	}
	if !result.Applied || !result.Retried {
		t.Fatalf("marker-only retry result = %+v, want applied retry", result)
	}
	if got := mustReadRecoveryFile(t, statePath); string(got) != string(candidateState) {
		t.Fatalf("marker-only retry state = %q, want candidate", got)
	}
	if got := mustReadRecoveryFile(t, journalPath); string(got) != string(candidateJournal) {
		t.Fatalf("marker-only retry journal = %q, want candidate", got)
	}
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
	marker["candidate_state_sha256"] = digest(invalidBytes)
	marker["manifest"].(map[string]any)["candidate_state_sha256"] = digest(invalidBytes)
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

func TestApplyRecoveryQuarantinesAndRetiresFingerprintPendingMarker(t *testing.T) {
	root, statePath, journalPath := recoveryPaths(t)
	writeRecoveryFile(t, statePath, []byte("damaged-state\n"))
	writeRecoveryFile(t, journalPath, []byte("damaged-journal\n"))
	pendingPath := statePath + ".fingerprint-pending.json"
	pendingBytes := []byte(`{"old":"fingerprint-pending"}`)
	writeRecoveryFile(t, pendingPath, pendingBytes)
	candidateState, candidateJournal := recoveryCandidates(t)
	req := recoveryRequest(root, statePath, journalPath, candidateState, candidateJournal, "plan-fingerprint-pending", "plan-sha-fingerprint-pending")
	req.SourcePendingSHA256 = map[string]string{pendingPath: digest(pendingBytes)}

	result, err := runtime.ApplyRecovery(req)
	if err != nil {
		t.Fatalf("ApplyRecovery() error = %v", err)
	}
	if _, err := os.Stat(pendingPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fingerprint pending marker remains: %v", err)
	}
	if len(result.Manifest.SourcePending) != 1 {
		t.Fatalf("source pending artifacts = %#v, want fingerprint marker", result.Manifest.SourcePending)
	}
	if got := mustReadRecoveryFile(t, result.Manifest.SourcePending[0].QuarantinePath); string(got) != string(pendingBytes) {
		t.Fatalf("quarantined fingerprint pending bytes = %q, want %q", got, pendingBytes)
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
	if _, err := runtime.ApplyRecovery(reqWithoutFault(req)); err != nil {
		t.Fatalf("output mismatch retry error = %v", err)
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

	if _, err := runtime.ApplyRecovery(reqWithoutFault(req)); err != nil {
		t.Fatalf("first manifest retry error = %v", err)
	}
	manifestPath := filepath.Join(root, ".claude", "recovery", "manifests", "plan-manifest-001-plan-sha-manifest-001.json")
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

	other := recoveryRequest(root, statePath, journalPath, candidateState, candidateJournal, "plan-idempotent-002", "plan-sha-idempotent-002")
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
	firstRequest.SourceStateSHA256 = digest(oldState)
	firstRequest.SourceJournalSHA256 = digest(oldJournal)
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

	candidateB := candidateStateVariant(t, candidateA, "2026-08-13T11:00:00Z")
	secondRequest := recoveryRequest(root, statePath, journalPath, candidateB, candidateJournal, "plan-epoch-002", "plan-sha-epoch-002")
	secondRequest.SourceStateSHA256 = digest(candidateA)
	secondRequest.SourceJournalSHA256 = digest(candidateJournal)
	second, err := runtime.ApplyRecovery(secondRequest)
	if err != nil {
		t.Fatalf("new plan apply after completed epoch error = %v", err)
	}
	if !second.Applied || second.Idempotent || second.ManifestPath == first.ManifestPath || second.QuarantineDir == first.QuarantineDir {
		t.Fatalf("new plan result = %+v, want a new applied recovery epoch", second)
	}
}

func TestApplyRecoveryRejectsInputAndCandidateOutputHashMismatchWithoutWritingActivePair(t *testing.T) {
	root, statePath, journalPath := recoveryPaths(t)
	activeState := []byte("active-state\n")
	activeJournal := []byte("active-journal\n")
	writeRecoveryFile(t, statePath, activeState)
	writeRecoveryFile(t, journalPath, activeJournal)
	candidateState, candidateJournal := recoveryCandidates(t)
	req := recoveryRequest(root, statePath, journalPath, candidateState, candidateJournal, "plan-hash-001", "plan-sha-hash-001")
	req.SourceStateSHA256 = digest([]byte("different-source"))
	if _, err := runtime.ApplyRecovery(req); !errors.Is(err, runtime.ErrRecoveryInputDrift) {
		t.Fatalf("source hash error = %v, want ErrRecoveryInputDrift", err)
	}
	if got := mustReadRecoveryFile(t, statePath); string(got) != string(activeState) {
		t.Fatalf("state changed on source drift: %q", got)
	}

	req = recoveryRequest(root, statePath, journalPath, candidateState, candidateJournal, "plan-hash-002", "plan-sha-hash-002")
	req.CandidateStateSHA256 = digest([]byte("different-candidate"))
	if _, err := runtime.ApplyRecovery(req); !errors.Is(err, runtime.ErrRecoveryInputDrift) {
		t.Fatalf("candidate hash error = %v, want ErrRecoveryInputDrift", err)
	}
	if got := mustReadRecoveryFile(t, journalPath); string(got) != string(activeJournal) {
		t.Fatalf("journal changed on candidate drift: %q", got)
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
		PlanSHA:          planSHA,
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
	document, err := os.ReadFile(filepath.Join("..", "..", "docs", "loop-definition.json"))
	if err != nil {
		t.Fatal(err)
	}
	docsDir := filepath.Join(root, "docs")
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

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
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
