package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/metrics"
)

func TestReviewResultControlPathsSelectExactRowsAndRetainGlobalIdentityChecks(t *testing.T) {
	root := t.TempDir()
	p := checkpointTestReviewPacket(t, root)
	state, journal := checkpointTestReviewState(t, p, nil)
	if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), journal, 0600); err != nil {
		t.Fatal(err)
	}
	entities := state["entities"].(map[string]any)
	rows := entities["work_packets"].([]any)
	// This unrelated historical body's missing material is deliberately not
	// needed for the current path classification. Full-history validation
	// still diagnoses it.
	history := map[string]any{"packet_id": "history", "template_id": reviewResultPacketTemplateID, "view_path": ".claude/work-packets/history.json"}
	entities["work_packets"] = append(rows, history)
	ctx, timings := metrics.WithCallTimings(WithReviewResultReadCache(context.Background()))
	wanted := []string{p.ViewPath, p.AnswerPath, filepath.ToSlash(filepath.Join(filepath.Dir(p.AnswerPath), "unknown.json"))}
	for range 2 {
		got, err := VerifyReviewResultPacketControlPathsContext(ctx, root, state, wanted)
		if err != nil || !reflect.DeepEqual(got, wanted[:2]) {
			t.Fatalf("exact controls: %v %v", got, err)
		}
	}
	if timings.Snapshot()["runtime/review_packet_selected_build"].Count != 1 || timings.Snapshot()["runtime/review_packet_selected_hit"].Count != 1 || timings.Snapshot()["runtime/journal_decode_build"].Count != 1 {
		t.Fatal(timings.Snapshot())
	}
	if _, err := ReviewResultPackets(state); err == nil {
		t.Fatal("full-history diagnostic stopped validating unrelated bodies")
	}
	if _, err := VerifyReviewResultPacketControlPathsContext(ctx, root, state, []string{history["view_path"].(string)}); err == nil {
		t.Fatal("selected malformed historical packet accepted")
	}
	history["packet_id"] = p.ID
	history["template_id"] = "other-template"
	if _, err := VerifyReviewResultPacketControlPathsContext(ctx, root, state, nil); err == nil || !strings.Contains(err.Error(), "duplicate Runtime work packet ID") {
		t.Fatalf("global cross-subtype uniqueness: %v", err)
	}
	entities["work_packets"] = []any{false}
	if _, err := VerifyReviewResultPacketControlPathsContext(ctx, root, state, nil); err == nil || !strings.Contains(err.Error(), "not an object") {
		t.Fatalf("global registry shape: %v", err)
	}
}

func TestReviewResultControlPathsCacheRechecksSelectedBytesSourcesAndCancellation(t *testing.T) {
	root := t.TempDir()
	p := checkpointTestReviewPacket(t, root)
	state, journal := checkpointTestReviewState(t, p, nil)
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	if err := os.WriteFile(journalPath, journal, 0600); err != nil {
		t.Fatal(err)
	}
	ctx := WithReviewResultReadCache(context.Background())
	check := func() error {
		_, err := VerifyReviewResultPacketControlPathsContext(ctx, root, state, []string{p.AnswerPath})
		return err
	}
	if err := check(); err != nil {
		t.Fatal(err)
	}
	row := state["entities"].(map[string]any)["work_packets"].([]any)[0].(map[string]any)
	row["unknown"] = true
	if err := check(); err == nil {
		t.Fatal("selected row mutation hidden")
	}
	delete(row, "unknown")
	if err := os.WriteFile(journalPath, append(journal, []byte("broken\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if err := check(); err == nil {
		t.Fatal("journal mutation hidden")
	}
	if err := os.WriteFile(journalPath, journal, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, p.Binding.ReviewPlanRef), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := check(); err == nil {
		t.Fatal("pinned source mutation hidden")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := VerifyReviewResultPacketControlPathsContext(canceled, root, state, []string{p.AnswerPath}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestReviewResultControlPathsHistoryGrowthBuildsOnlySelectedPacket(t *testing.T) {
	for _, historySize := range []int{1, 64, 256} {
		t.Run(fmt.Sprint(historySize), func(t *testing.T) {
			root := t.TempDir()
			p := checkpointTestReviewPacket(t, root)
			state, journal := checkpointTestReviewState(t, p, nil)
			if err := os.WriteFile(filepath.Join(root, ".claude", "loop-events.jsonl"), journal, 0600); err != nil {
				t.Fatal(err)
			}
			entities := state["entities"].(map[string]any)
			rows := entities["work_packets"].([]any)
			for i := 0; i < historySize; i++ {
				rows = append(rows, map[string]any{"packet_id": fmt.Sprintf("history-%d", i), "template_id": reviewResultPacketTemplateID, "unused_body": strings.Repeat("x", 32<<10)})
			}
			entities["work_packets"] = rows
			ctx, timings := metrics.WithCallTimings(WithReviewResultReadCache(context.Background()))
			if _, err := VerifyReviewResultPacketControlPathsContext(ctx, root, state, []string{p.ViewPath, p.AnswerPath}); err != nil {
				t.Fatal(err)
			}
			if timings.Snapshot()["runtime/review_packet_selected_build"].Count != 1 || timings.Snapshot()["runtime/review_packet_index_build"].Count != 0 || timings.Snapshot()["runtime/journal_decode_build"].Count != 1 {
				t.Fatalf("history rebuilt on drift path: %v", timings.Snapshot())
			}
		})
	}
}
