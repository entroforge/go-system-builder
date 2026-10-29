package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

func TestReviewPacketReadCacheTracksExactRegistryAndLiveAssignments(t *testing.T) {
	p := checkpointTestReviewPacket(t, t.TempDir())
	state, _ := checkpointTestReviewState(t, p, nil)
	ctx, timings := metrics.WithCallTimings(WithReviewResultReadCache(context.Background()))
	got, err := CurrentReviewResultPacketsContext(ctx, state, p.Binding.OwnerAgentID)
	if err != nil || len(got) != 1 || !reflect.DeepEqual(got[0], p) {
		t.Fatalf("initial packet: %v %v", got, err)
	}
	got[0].Binding.ClaimIDs[0] = "claim-corrupt"
	got[0].Inputs.Claims[0] = 'X'
	got[0].Draft.Answer[0] = 'X'
	got[0].Draft.Read[0].Path = "corrupt"
	delete(got[0].Draft.Questions, "verdict")
	again, found, err := FindReviewResultPacketContext(ctx, state, p.ID)
	if err != nil || !found || !reflect.DeepEqual(again, p) {
		t.Fatalf("caller mutated cached packet: %v %v", found, err)
	}
	if timings.Snapshot()["runtime/review_packet_selected_build"].Count != 1 || timings.Snapshot()["runtime/review_packet_selected_hit"].Count != 1 {
		t.Fatal(timings.Snapshot())
	}
	assignment := state["review"].(map[string]any)["assignments"].(map[string]any)[p.Binding.AssignmentID].(map[string]any)
	assignment["agent_id"] = "different-owner"
	got, err = CurrentReviewResultPacketsContext(ctx, state, p.Binding.OwnerAgentID)
	if err != nil || len(got) != 0 {
		t.Fatalf("cached mutable assignment: %v %v", got, err)
	}
	assignment["agent_id"] = p.Binding.OwnerAgentID
	entities := state["entities"].(map[string]any)
	rows := entities["work_packets"].([]any)
	row := rows[0].(map[string]any)
	row["unknown"] = true
	if _, _, err := FindReviewResultPacketContext(ctx, state, p.ID); err == nil {
		t.Fatal("unknown row field hidden by cache")
	}
	delete(row, "unknown")
	entities["work_packets"] = append(rows, map[string]any{"packet_id": p.ID, "template_id": "other-template"})
	if _, _, err := FindReviewResultPacketContext(ctx, state, p.ID); err == nil {
		t.Fatal("cross-subtype duplicate hidden by cache")
	}
	entities["work_packets"] = []map[string]any{row}
	if _, _, err := FindReviewResultPacketContext(ctx, state, p.ID); err == nil {
		t.Fatal("JSON-equivalent malformed registry hidden by cache")
	}
	entities["work_packets"] = rows
	state["configuration"].(map[string]any)["optimization"].(map[string]any)["packet"] = false
	if _, _, err := FindReviewResultPacketContext(ctx, state, p.ID); err == nil {
		t.Fatal("packet marker change hidden by cache")
	}
	state["configuration"].(map[string]any)["optimization"].(map[string]any)["packet"] = true
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := FindReviewResultPacketContext(canceled, state, p.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation hidden: %v", err)
	}
}

func TestReviewPacketReadCacheReReadsJournalAndPinnedEvidence(t *testing.T) {
	root := t.TempDir()
	p := checkpointTestReviewPacket(t, root)
	state, journal := checkpointTestReviewState(t, p, nil)
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	if err := os.WriteFile(journalPath, journal, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, timings := metrics.WithCallTimings(WithReviewResultReadCache(context.Background()))
	for range 2 {
		if err := VerifyReviewResultPacketIssueContext(ctx, root, state, p); err != nil {
			t.Fatal(err)
		}
	}
	if timings.Snapshot()["runtime/journal_decode_build"].Count != 1 {
		t.Fatalf("redecoded exact journal: %v", timings.Snapshot())
	}
	if err := os.WriteFile(journalPath, append(append([]byte(nil), journal...), []byte("broken\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyReviewResultPacketIssueContext(ctx, root, state, p); err == nil {
		t.Fatal("journal corruption hidden by cache")
	}
	if err := os.WriteFile(journalPath, journal, 0600); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(root, p.Binding.ReviewPlanRef)
	if err := os.WriteFile(planPath, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyReviewResultPacketIssueContext(ctx, root, state, p); err == nil {
		t.Fatal("pinned evidence drift hidden by cache")
	}
}

func TestReviewPacketReadCacheConcurrent(t *testing.T) {
	p := checkpointTestReviewPacket(t, t.TempDir())
	state, _ := checkpointTestReviewState(t, p, nil)
	ctx, timings := metrics.WithCallTimings(WithReviewResultReadCache(context.Background()))
	var readers sync.WaitGroup
	for range 16 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			got, found, err := FindReviewResultPacketContext(ctx, state, p.ID)
			if err != nil || !found || !reflect.DeepEqual(got, p) {
				t.Errorf("concurrent lookup: %v %v", found, err)
				return
			}
			got.Binding.ClaimIDs[0] = "caller-owned"
			got.Inputs.Claims[0] = 'X'
		}()
	}
	readers.Wait()
	if timings.Snapshot()["runtime/review_packet_selected_build"].Count != 1 {
		t.Fatal(timings.Snapshot())
	}
}

func TestReviewPacketReadCacheCancelsWhileAnotherReaderBuilds(t *testing.T) {
	p := checkpointTestReviewPacket(t, t.TempDir())
	state, _ := checkpointTestReviewState(t, p, nil)
	ctx := WithReviewResultReadCache(context.Background())
	cache := ctx.Value(reviewResultReadCacheKey{}).(*reviewResultReadCache)
	cache.gate <- struct{}{}
	defer func() { <-cache.gate }()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, _, err := FindReviewResultPacketContext(ctx, state, p.ID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting lookup: %v", err)
	}
}

func BenchmarkCurrentReviewResultPacketRegistry(b *testing.B) {
	for _, size := range []int{1, 64} {
		p := checkpointTestReviewPacket(b, b.TempDir())
		state, _ := checkpointTestReviewState(b, p, nil)
		rows := state["entities"].(map[string]any)["work_packets"].([]any)
		for i := 1; i < size; i++ {
			binding := p.Binding
			binding.ReviewPlanRevision += i
			historical, err := workpacket.BuildReviewResult(binding, p.Inputs)
			if err != nil {
				b.Fatal(err)
			}
			data, err := json.Marshal(historical)
			if err != nil {
				b.Fatal(err)
			}
			var row map[string]any
			if err := json.Unmarshal(data, &row); err != nil {
				b.Fatal(err)
			}
			rows = append(rows, row)
		}
		state["entities"].(map[string]any)["work_packets"] = rows
		for _, cached := range []bool{false, true} {
			b.Run(fmt.Sprintf("history=%d/cache=%t/request4", size, cached), func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					ctx := context.Background()
					if cached {
						ctx = WithReviewResultReadCache(ctx)
					}
					for range 4 {
						packets, err := CurrentReviewResultPacketsContext(ctx, state, p.Binding.OwnerAgentID)
						if err != nil || len(packets) != 1 {
							b.Fatalf("lookup: %d %v", len(packets), err)
						}
					}
				}
			})
		}
	}
}
