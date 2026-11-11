package runtime_test

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/runtime"
)

func TestRecoveryWriterSkipsUnknownManifestAndReplayPreservesProgress(t *testing.T) {
	root, statePath, journalPath := recoveryPaths(t)
	writeRecoveryFile(t, statePath, []byte("damaged state"))
	writeRecoveryFile(t, journalPath, []byte("damaged journal"))
	state, journal := recoveryCandidates(t)
	planID := "plan-unknown-manifest"
	path := filepath.Join(root, ".claude", "recovery", "manifests", "plan", base64.RawURLEncoding.EncodeToString([]byte(planID)), "manifest.json")
	unknown := []byte(`{"status":"applied","plan_id":"plan-unknown-manifest"}`)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	writeRecoveryFile(t, path, unknown)
	req := recoveryRequest(root, statePath, journalPath, state, journal, planID, "")
	first, err := runtime.ApplyRecovery(req)
	if err != nil || !first.Applied || first.Idempotent {
		t.Fatalf("first apply: %+v %v", first, err)
	}
	if first.ManifestPath == path {
		t.Fatal("claimed an unknown manifest")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != string(unknown) {
		t.Fatalf("unknown manifest changed: %q %v", got, err)
	}
	writer := runtime.NewWriter(statePath, journalPath, root, recoveryValidator{})
	_, err = writer.UpdateCurrent(runtime.Mutation{EventID: "evt-after-recovery", TransitionID: "TEST-AFTER-RECOVERY", Event: "test", Actor: "orchestrator", IdempotencyKey: "after-recovery", OccurredAt: time.Date(2026, 8, 13, 11, 0, 0, 0, time.UTC), RetainLastTransition: true})
	if err != nil {
		t.Fatalf("subsequent original Writer: %v", err)
	}
	progressedState, progressedJournal := mustReadRecoveryFile(t, statePath), mustReadRecoveryFile(t, journalPath)
	replay, err := runtime.ApplyRecovery(req)
	if err != nil || !replay.Idempotent || replay.ManifestPath != first.ManifestPath {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	if string(mustReadRecoveryFile(t, statePath)) != string(progressedState) || string(mustReadRecoveryFile(t, journalPath)) != string(progressedJournal) {
		t.Fatal("replay rolled back later original Writer progress")
	}
}

func TestRecoveryPendingPreservesUnparseableCurrentStateAndContinues(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprintf("changed_%v", changed), func(t *testing.T) {
			root, statePath, journalPath := recoveryPaths(t)
			original := []byte("broken-before-pending")
			current := original
			writeRecoveryFile(t, statePath, original)
			writeRecoveryFile(t, journalPath, []byte("broken-journal"))
			state, journal := recoveryCandidates(t)
			req := recoveryRequest(root, statePath, journalPath, state, journal, "plan-unparseable", "")
			req.FailureInjector = recoveryFailureFunc(func(step runtime.RecoveryFailureStep) error {
				if step == runtime.RecoveryBeforeStateReplace {
					return errors.New("interrupt")
				}
				return nil
			})
			if _, err := runtime.ApplyRecovery(req); !errors.Is(err, runtime.ErrRecoveryInjectedFailure) {
				t.Fatalf("initial interruption: %v", err)
			}
			if changed {
				current = []byte("different-unparseable-external-state")
				writeRecoveryFile(t, statePath, current)
			}
			result, err := runtime.ApplyRecovery(reqWithoutFault(req))
			if err != nil || !result.Applied || !result.Retried {
				t.Fatalf("pending resume: %+v %v", result, err)
			}
			if len(result.Manifest.UncertainSources) == 0 {
				t.Fatal("current source was not reported as unknown")
			}
			if got := mustReadRecoveryFile(t, result.Manifest.UncertainSources[0].QuarantinePath); string(got) != string(current) {
				t.Fatalf("current unknown source lost: %q", got)
			}
			if got := mustReadRecoveryFile(t, filepath.Join(result.QuarantineDir, "loop-state.json")); string(got) != string(original) {
				t.Fatalf("initial original source lost: %q", got)
			}
			assertRecoveredPair(t, statePath, journalPath, state, journal, req.PlanID)
		})
	}
}

func TestRecoveryManifestParentOccupantDoesNotBlockOrGetClaimed(t *testing.T) {
	root, statePath, journalPath := recoveryPaths(t)
	writeRecoveryFile(t, statePath, []byte("damaged"))
	writeRecoveryFile(t, journalPath, []byte("damaged"))
	state, journal := recoveryCandidates(t)
	planID := "plan-parent-occupant"
	parent := filepath.Join(root, ".claude", "recovery", "manifests", "plan")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	occupied := filepath.Join(parent, base64.RawURLEncoding.EncodeToString([]byte(planID)))
	writeRecoveryFile(t, occupied, []byte("unknown parent"))
	req := recoveryRequest(root, statePath, journalPath, state, journal, planID, "")
	result, err := runtime.ApplyRecovery(req)
	if err != nil || !result.Applied {
		t.Fatalf("recovery with occupied parent: %+v %v", result, err)
	}
	if got := mustReadRecoveryFile(t, occupied); string(got) != "unknown parent" {
		t.Fatal("unknown parent changed")
	}
	if !strings.Contains(filepath.ToSlash(result.ManifestPath), "recovery-manifests-alt-") {
		t.Fatalf("actual alternate manifest missing: %s", result.ManifestPath)
	}
	replay, err := runtime.ApplyRecovery(req)
	if err != nil || !replay.Idempotent || replay.ManifestPath != result.ManifestPath {
		t.Fatalf("registered alternate replay: %+v %v", replay, err)
	}
}
