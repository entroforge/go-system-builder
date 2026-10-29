package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/filelock"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func latestStageCheckpoint(t *testing.T, root string) (string, runtime.CheckpointPair) {
	t.Helper()
	path, err := runtime.LatestRuntimeCheckpoint(root)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := runtime.ReadRuntimeCheckpoint(root, path, semantic.RuntimeCandidateValidator{})
	if err != nil {
		t.Fatal(err)
	}
	return path, pair
}

func checkpointPlan(t *testing.T, root, path string, state map[string]any) string {
	t.Helper()
	bound := state["bound_req"].(map[string]any)
	binding := bound["workspace"].(map[string]any)
	out := stageCLI(t, "runtime", "recover", "plan", "--root", root, "--req", bound["path"].(string), "--dev-branch", binding["dev_branch"].(string), "--release-upstream", binding["release_upstream"].(string), "--checkpoint", path)
	var result struct {
		Path string         `json:"recovery_plan"`
		Plan map[string]any `json:"plan"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	if result.Plan["confidence"] != "original_pair" || result.Plan["base_mode"] != "checkpoint_restoration" || result.Plan["replay_trace"] != nil {
		t.Fatalf("checkpoint was reconstructed/replayed: %s", out)
	}
	return result.Path
}

func damageStagePair(t *testing.T, root string) {
	t.Helper()
	for _, name := range []string{"loop-state.json", "loop-events.jsonl"} {
		if err := os.WriteFile(filepath.Join(root, ".claude", name), []byte("damaged original "+name+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCheckpointRecoverPreservesNamedRevisionAndExactlyOnceRoute(t *testing.T) {
	f := newStageAnswerFixture(t)
	old, peer := f.packets[0], f.packets[1]
	f.begin(t, old, nil)
	f.begin(t, peer, nil)
	first := saveStageHook(t, f.root, f.root, old.Binding.Owner, old.AnswerPath, completedDraft(old, "pass"))
	var next workpacket.Packet
	if err := json.Unmarshal([]byte(stageCLI(t, "runtime", "stage-answer", "revise", "--root", f.root, "--previous", first.SubmissionKey, "--reason", "Recheck the concrete missing dependency before advancing")), &next); err != nil {
		t.Fatal(err)
	}
	if !saveStageHook(t, f.root, f.root, peer.Binding.Owner, peer.AnswerPath, completedDraft(peer, "pass")).WorkComplete {
		t.Fatal("peer was not received")
	}
	path, pair := latestStageCheckpoint(t, f.root)
	before := stageSnapshot(t, f.root)
	if !reflect.DeepEqual(pair.State, before.State) {
		t.Fatal("checkpoint lost latest original facts")
	}
	// Split the original complete journal as rotation does. A reviewed recovery
	// must quarantine and retire this segment, not duplicate its events.
	lines := bytes.SplitAfter(pair.JournalBytes, []byte{'\n'})
	segment := filepath.Join(f.root, ".claude/loop-events.jsonl.archive.1.jsonl")
	if err := os.WriteFile(segment, bytes.Join(lines[:len(lines)/2], nil), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, ".claude/loop-events.jsonl"), bytes.Join(lines[len(lines)/2:], nil), 0600); err != nil {
		t.Fatal(err)
	}
	damageStagePair(t, f.root)
	plan := checkpointPlan(t, f.root, path, pair.State)
	stageCLI(t, "runtime", "recover", "apply", "--root", f.root, "--plan", plan, "--approved-by", "operator")
	restored := stageSnapshot(t, f.root)
	if !reflect.DeepEqual(restored.State, before.State) {
		t.Fatal("complete recovery changed identity/activation/revision/indices")
	}
	if _, err := os.Stat(segment); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("old journal segment survived full pair replacement")
	}
	s, err := submission.New(f.root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.ReceiptPacket(old.Binding.Owner, old.ID)
	if err != nil || r.WorkComplete || r.ResponsibilityStatus != "replaced" {
		t.Fatalf("old PASS revived: %+v %v", r, err)
	}
	if r, err := s.Save(context.Background(), next.Binding.Owner, next.AnswerPath); err == nil || r.WorkComplete {
		t.Fatal("recovery invented an answer for the new attempt")
	}
	if s := stageSnapshot(t, f.root); s.State["lifecycle"].(map[string]any)["state"] != "document_verification" {
		t.Fatal("pending revision was skipped")
	}
	r = saveStageHook(t, f.root, f.root, next.Binding.Owner, next.AnswerPath, completedDraft(next, "pass"))
	if !r.WorkComplete {
		t.Fatalf("restored activation/source did not receive: %+v", r)
	}
	final := stageSnapshot(t, f.root)
	if final.State["lifecycle"].(map[string]any)["state"] != "building" {
		t.Fatal("restored review did not use original TR-003")
	}
	transitionCount := 0
	journal, err := checkpointJournalEvents(f.root)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range journal {
		if event["transition_id"] == "TR-003" {
			transitionCount++
		}
	}
	if transitionCount != 1 {
		t.Fatalf("TR-003 count=%d", transitionCount)
	}
	finalPath, finalPair := latestStageCheckpoint(t, f.root)
	damageStagePair(t, f.root)
	finalPlan := checkpointPlan(t, f.root, finalPath, finalPair.State)
	stageCLI(t, "runtime", "recover", "apply", "--root", f.root, "--plan", finalPlan, "--approved-by", "operator")
	again := stageSnapshot(t, f.root)
	if !reflect.DeepEqual(again.State, final.State) {
		t.Fatal("advanced followup lost original transition stamp")
	}
	s, err = submission.New(f.root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Recover(context.Background(), "", 2); err != nil {
		t.Fatal(err)
	}
	if stageSnapshot(t, f.root).Revision != again.Revision {
		t.Fatal("recovery repeated a processed followup")
	}
	replay := stageCLI(t, "runtime", "recover", "apply", "--root", f.root, "--plan", finalPlan, "--approved-by", "operator")
	if !strings.Contains(replay, "LOOP_RECOVERY_ALREADY_APPLIED") {
		t.Fatal("full pair apply was not idempotent")
	}
}

func TestCheckpointPublishFailureRetainsPendingAndRetriesOnce(t *testing.T) {
	f := newStageAnswerFixture(t)
	path, prior := latestStageCheckpoint(t, f.root)
	index := filepath.Join(f.root, ".claude/runtime-checkpoints/index.json")
	saved := index + ".saved"
	if err := os.Rename(index, saved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(saved, index); err != nil {
		t.Fatal(err)
	}
	writer := runtime.NewWriter(filepath.Join(f.root, ".claude/loop-state.json"), filepath.Join(f.root, ".claude/loop-events.jsonl"), f.root, semantic.RuntimeCandidateValidator{})
	_, err := writer.UpdateCurrent(runtime.Mutation{EventID: "evt-checkpoint-failure", TransitionID: "CHECKPOINT-TEST", Event: "checkpoint_test", Actor: "orchestrator", RuntimeID: prior.Manifest.RuntimeID, IdempotencyKey: "checkpoint-failure", RetainLastTransition: true, OccurredAt: time.Now().UTC(), Apply: func(map[string]any) error { return nil }})
	if err == nil {
		t.Fatal("symlink checkpoint index was replaced")
	}
	if _, err := os.Stat(filepath.Join(f.root, ".claude/loop-state.json.commit-pending.json")); err != nil {
		t.Fatal("durable pending was discarded after checkpoint failure")
	}
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(saved, index); err != nil {
		t.Fatal(err)
	}
	completed, err := writer.RecoverPendingOperations()
	if err != nil || !completed {
		t.Fatalf("pending retry: %v", err)
	}
	newer, pair := latestStageCheckpoint(t, f.root)
	if newer == path || pair.Manifest.Revision != prior.Manifest.Revision+1 {
		t.Fatal("checkpoint retry lost/duplicated the committed revision")
	}
	journal, err := checkpointJournalEvents(f.root)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, event := range journal {
		if event["event_id"] == "evt-checkpoint-failure" {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("checkpoint recovery duplicated original event: %d", found)
	}
	dirs, err := os.ReadDir(filepath.Dir(index))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, entry := range dirs {
		if entry.IsDir() {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("checkpoint retained %d directories; want bounded latest two", n)
	}
}

func TestCheckpointRecoverRejectsLiveIndependentWorker(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	e := f.prepare(t, p)
	f.begin(t, p, &e)
	if !saveStageHook(t, f.root, e.Path, p.Binding.Owner, workpacket.WorkerAnswerPath, completedDraft(p, "pass")).WorkComplete {
		t.Fatal("Worker conclusion not received")
	}
	path, pair := latestStageCheckpoint(t, f.root)
	damageStagePair(t, f.root)
	plan := checkpointPlan(t, f.root, path, pair.State)
	leasePath := filepath.Join(f.root, ".claude/workspace-launch", e.RuntimeID, fmt.Sprintf("g%d-%s-e%d.lock", e.BaselineGeneration, e.AssignmentID, e.Generation))
	release, err := filelock.Acquire(context.Background(), leasePath)
	if err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	code := cli.Run([]string{"runtime", "recover", "apply", "--root", f.root, "--plan", plan, "--approved-by", "operator"}, nil, &out, &errout)
	release()
	if code == 0 || !strings.Contains(errout.String(), "launch lease") {
		t.Fatalf("live Worker restored: %d %s", code, errout.String())
	}
	data, err := os.ReadFile(filepath.Join(f.root, ".claude/loop-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if json.Valid(data) {
		t.Fatal("lease rejection changed the active original")
	}
	stageCLI(t, "runtime", "recover", "apply", "--root", f.root, "--plan", plan, "--approved-by", "operator")
	if !reflect.DeepEqual(stageSnapshot(t, f.root).State, pair.State) {
		t.Fatal("independent source/activation changed")
	}
}

func TestCheckpointRejectsOlderPairAndForgedReceptionFacts(t *testing.T) {
	f := newStageAnswerFixture(t)
	oldPath, _ := latestStageCheckpoint(t, f.root)
	p := f.packets[0]
	f.begin(t, p, nil)
	first := saveStageHook(t, f.root, f.root, p.Binding.Owner, p.AnswerPath, completedDraft(p, "pass"))
	stageCLI(t, "runtime", "stage-answer", "revise", "--root", f.root, "--previous", first.SubmissionKey, "--reason", "Recheck the frozen dependency")
	_, pair := latestStageCheckpoint(t, f.root)
	if _, err := runtime.ReadRuntimeCheckpoint(f.root, oldPath, semantic.RuntimeCandidateValidator{}); err == nil {
		t.Fatal("old checkpoint could discard reception/revision")
	}
	for _, test := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"old-pass", func(state map[string]any) { state["evidence"].([]any)[0].(map[string]any)["status"] = "valid" }},
		{"missing-lineage", func(state map[string]any) {
			state["entities"].(map[string]any)["work_packets"] = state["entities"].(map[string]any)["work_packets"].([]any)[:2]
		}},
		{"fake-transition", func(state map[string]any) {
			r := state["entities"].(map[string]any)["submissions"].([]any)[0].(map[string]any)
			r["control_status"] = "advanced"
			r["transition_event_id"] = "evt-invented"
		}},
		{"duplicate-key", func(state map[string]any) {
			e := state["entities"].(map[string]any)
			rows := e["submissions"].([]any)
			e["submissions"] = append(rows, rows[0])
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, _ := json.Marshal(pair.State)
			var changed map[string]any
			json.Unmarshal(data, &changed)
			test.change(changed)
			if err := runtime.ValidateCheckpointIndices(f.root, changed, pair.JournalBytes); err == nil {
				t.Fatal("forged indexed facts accepted")
			}
		})
	}
}

func checkpointJournalEvents(root string) ([]map[string]any, error) {
	path, err := runtime.LatestRuntimeCheckpoint(root)
	if err != nil {
		return nil, err
	}
	pair, err := runtime.ReadRuntimeCheckpoint(root, path, semantic.RuntimeCandidateValidator{})
	if err != nil {
		return nil, err
	}
	result := []map[string]any{}
	for _, line := range bytes.Split(pair.JournalBytes, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, nil
}

type checkpointFailure struct{ step runtime.RecoveryFailureStep }

func (f checkpointFailure) Inject(step runtime.RecoveryFailureStep) error {
	if step == f.step {
		return errors.New("injected complete-pair interruption")
	}
	return nil
}

func TestCheckpointSealedRecoveryResumesAfterPairReplacementAndSourceLoss(t *testing.T) {
	for _, step := range []runtime.RecoveryFailureStep{runtime.RecoveryAfterStateReplace, runtime.RecoveryAfterJournalReplace, runtime.RecoveryBeforeManifest} {
		t.Run(string(step), func(t *testing.T) {
			f := newStageAnswerFixture(t)
			p := f.packets[0]
			f.begin(t, p, nil)
			if !saveStageHook(t, f.root, f.root, p.Binding.Owner, p.AnswerPath, completedDraft(p, "pass")).WorkComplete {
				t.Fatal("domain reception failed")
			}
			path, pair := latestStageCheckpoint(t, f.root)
			damageStagePair(t, f.root)
			planPath := checkpointPlan(t, f.root, path, pair.State)
			var plan map[string]any
			data, err := os.ReadFile(planPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &plan); err != nil {
				t.Fatal(err)
			}
			_, err = runtime.ApplyRecovery(runtime.RecoveryRequest{Root: f.root, StatePath: filepath.Join(f.root, ".claude/loop-state.json"), JournalPath: filepath.Join(f.root, ".claude/loop-events.jsonl"), PlanID: plan["plan_id"].(string), PlanSHA: plan["document_sha256"].(string), CandidateState: pair.StateBytes, CandidateJournal: pair.JournalBytes, Approver: "operator", OccurredAt: time.Now().UTC(), Validator: semantic.RuntimeCandidateValidator{}, ValidateRestore: func(state map[string]any, journal []byte) error {
				return runtime.ValidateCheckpointRestore(f.root, runtime.CheckpointPair{State: state, JournalBytes: journal, Manifest: pair.Manifest})
			}, FailureInjector: checkpointFailure{step}})
			if !errors.Is(err, runtime.ErrRecoveryInjectedFailure) {
				t.Fatalf("fault did not interrupt: %v", err)
			}
			if err := os.RemoveAll(filepath.Dir(planPath)); err != nil {
				t.Fatal(err)
			}
			if err := os.RemoveAll(filepath.Join(f.root, ".claude/runtime-checkpoints")); err != nil {
				t.Fatal(err)
			}
			// The validated sealed marker outranks vanished mutable plan/source files.
			stageCLI(t, "runtime", "recover", "apply", "--root", f.root, "--plan", planPath, "--approved-by", "operator")
			if !reflect.DeepEqual(stageSnapshot(t, f.root).State, pair.State) {
				t.Fatal("marker retry changed accepted/control facts")
			}
			service, err := submission.New(f.root)
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := service.ReceiptPacket(p.Binding.Owner, p.ID)
			if err != nil || !receipt.WorkComplete {
				t.Fatalf("restored durable receipt lost: %+v %v", receipt, err)
			}
			// The next original Writer remains usable and restores bounded backups.
			writer := runtime.NewWriter(filepath.Join(f.root, ".claude/loop-state.json"), filepath.Join(f.root, ".claude/loop-events.jsonl"), f.root, semantic.RuntimeCandidateValidator{})
			if _, err := writer.UpdateCurrent(runtime.Mutation{EventID: "evt-after-sealed-recovery", TransitionID: "RECOVERY-TEST", Event: "recovery_test", Actor: "orchestrator", RuntimeID: pair.Manifest.RuntimeID, IdempotencyKey: "after-sealed-recovery", RetainLastTransition: true}); err != nil {
				t.Fatal(err)
			}
			_, latest := latestStageCheckpoint(t, f.root)
			if latest.Manifest.Revision != pair.Manifest.Revision+1 {
				t.Fatal("next Writer lost journal continuity")
			}
		})
	}
}

func TestCheckpointRecoveryHelpExposesRealFlagsWithoutRuntime(t *testing.T) {
	for _, tc := range []struct{ verb, flag string }{{"plan", "-checkpoint"}, {"apply", "-approved-by"}, {"inspect", "-req"}} {
		t.Run(tc.verb, func(t *testing.T) {
			root := t.TempDir()
			var out, errout bytes.Buffer
			code := cli.Run([]string{"runtime", "recover", tc.verb, "--root", root, "--help"}, nil, &out, &errout)
			if code != 0 || errout.Len() != 0 || !strings.Contains(out.String(), tc.flag) {
				t.Fatalf("help %d: %s %s", code, out.String(), errout.String())
			}
			if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
				t.Fatal("help mutated the workspace")
			}
		})
	}
}
