package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/submission"
)

func TestSavedStageAnswerHookObservesCallsWithoutInventingFirstSubmit(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	before := stageSnapshot(t, f.root)
	if r := saveStageHook(t, f.root, f.root, p.Binding.Owner, p.AnswerPath, p.Draft); r.Accepted || len(r.FieldErrors) == 0 {
		t.Fatalf("draft became accepted: %+v", r)
	}
	if stageSnapshot(t, f.root).Revision != before.Revision {
		t.Fatal("draft metric mutated Runtime")
	}
	// Ordinary S5 plan_checkpoint assignments have no generic PLAN/activation
	// step. The exact issued packet and its original consumer authorize the
	// professional answer while the Agent is still reading.
	var owner map[string]any
	for _, raw := range stageSnapshot(t, f.root).State["entities"].(map[string]any)["agents"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == p.Binding.Owner {
			owner = row
			break
		}
	}
	// Registration may pre-stage a capability envelope in activation_ref;
	// only the original committed activation revision proves activation.
	if owner == nil || owner["state"] != "reading" || owner["activation_revision"] != nil {
		t.Fatalf("fixture unexpectedly performed generic activation: %+v", owner)
	}
	if r := saveStageHook(t, f.root, f.root, p.Binding.Owner, p.AnswerPath, completedDraft(p, "pass")); !r.Accepted || !r.WorkComplete {
		t.Fatalf("valid issued S5 answer was not accepted before generic activation: %+v", r)
	}
	accepted := stageSnapshot(t, f.root)
	if r := saveStageHook(t, f.root, f.root, p.Binding.Owner, p.AnswerPath, completedDraft(p, "pass")); !r.Accepted || stageSnapshot(t, f.root).Revision != accepted.Revision {
		t.Fatal("observed replay created a second Runtime reception")
	}
	if r := saveStageHook(t, f.root, f.root, p.Binding.Owner, p.AnswerPath, completedDraft(p, "fix_required")); r.Accepted || stageSnapshot(t, f.root).Revision != accepted.Revision {
		t.Fatal("observation allowed changed accepted answer")
	}
	want := map[string]int64{"incomplete": 1, "complete_rejected": 1, "received_pass": 2}
	snap, err := metrics.NewStore(f.root).Read()
	if err != nil || !reflect.DeepEqual(snap.SubmissionSaves, want) {
		t.Fatalf("save calls miscounted (replay is a call, not unique delivery): %v %v", snap.SubmissionSaves, err)
	}
	for outcome, count := range want {
		if duration := snap.SubmissionSaveDuration[outcome]; duration.Count != count || duration.SumMS < 0 {
			t.Fatalf("counter/duration publication differs: %s %+v", outcome, duration)
		}
	}
	s, err := submission.New(f.root)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := s.Receipt(p.Binding.Owner, p.AnswerPath); err != nil || !r.Accepted {
		t.Fatalf("original durable receipt unavailable: %+v %v", r, err)
	}
	if err := metrics.CompactObservations(f.root); err != nil {
		t.Fatal(err)
	}
	after, err := metrics.NewStore(f.root).Read()
	if err != nil || !reflect.DeepEqual(after.SubmissionSaves, want) || !reflect.DeepEqual(after.SubmissionSaveDuration, snap.SubmissionSaveDuration) {
		t.Fatalf("receipt read/compaction counted another save: %+v %v", after, err)
	}
	text := stageCLI(t, "health", "--root", f.root)
	if !strings.Contains(text, `outcome="received_pass"`) || !strings.Contains(text, "first-submit rate unavailable") {
		t.Fatalf("CLI health lost live consumer: %s", text)
	}
	entities := stageSnapshot(t, f.root).State["entities"].(map[string]any)
	if len(entities["submissions"].([]any)) != 1 {
		t.Fatal("save count was treated as unique domain submissions")
	}
}

func TestSubmissionMetricFailureCannotRejectDurableReception(t *testing.T) {
	f := newStageAnswerFixture(t)
	p := f.packets[0]
	f.begin(t, p, nil)
	// Preserve existing telemetry. A file at the spool path makes every new
	// metrics publication fail, without changing the business authority.
	spool := filepath.Join(f.root, ".claude/hook-metrics")
	if err := os.Rename(spool, spool+"-preserved"); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.WriteFile(spool, []byte("fault injection: spool unavailable\n"), 0600); err != nil {
		t.Fatal(err)
	}
	r := saveStageHook(t, f.root, f.root, p.Binding.Owner, p.AnswerPath, completedDraft(p, "pass"))
	if !r.Accepted || !r.WorkComplete {
		t.Fatalf("telemetry failure changed original Hook receipt: %+v", r)
	}
	s, err := submission.New(f.root)
	if err != nil {
		t.Fatal(err)
	}
	if replay, err := s.Save(context.Background(), p.Binding.Owner, p.AnswerPath); err != nil || !replay.Accepted {
		t.Fatalf("telemetry failure prevented replay: %+v %v", replay, err)
	}
	entities := stageSnapshot(t, f.root).State["entities"].(map[string]any)
	if len(entities["submissions"].([]any)) != 1 {
		t.Fatal("failed telemetry split original reception")
	}
}
