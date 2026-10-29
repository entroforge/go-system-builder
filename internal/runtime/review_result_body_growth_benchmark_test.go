package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/packetbody"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// BenchmarkCurrentReviewResultPacketBodyGrowth fixes the current responsibility
// and frozen question/input content while increasing unrelated history. Only
// the historical issuance revision and its derived identities vary. Setup,
// publication, serialization for size measurements, and schema initialization
// are excluded. Every measured lookup has a new request cache and reads the
// selected body from disk; this is not a warmed packet-cache lookup.
// Short runs describe this local reader's growth, not platform Hook latency.
func BenchmarkCurrentReviewResultPacketBodyGrowth(b *testing.B) {
	for _, history := range []int{1, 64, 256} {
		b.Run(fmt.Sprintf("history=%d/current=1", history), func(b *testing.B) {
			b.StopTimer()
			root := b.TempDir()
			current := checkpointTestReviewPacket(b, root)
			state, _ := checkpointTestReviewState(b, current, nil)
			entities := state["entities"].(map[string]any)
			inlineRows := entities["work_packets"].([]any)
			for i := 0; i < history; i++ {
				binding := current.Binding
				binding.ReviewPlanRevision += i + 1
				packet, err := workpacket.BuildReviewResult(binding, current.Inputs)
				if err != nil {
					b.Fatal(err)
				}
				data, err := json.Marshal(packet)
				if err != nil {
					b.Fatal(err)
				}
				var row map[string]any
				if err := json.Unmarshal(data, &row); err != nil {
					b.Fatal(err)
				}
				inlineRows = append(inlineRows, row)
			}
			entities["work_packets"] = inlineRows
			inlineState, err := jsonDocumentBytes(state)
			if err != nil {
				b.Fatal(err)
			}
			indexRows := make([]any, 0, len(inlineRows))
			var currentRef packetbody.Reference
			for i, raw := range inlineRows {
				index, err := packetbody.Publish(context.Background(), root, raw.(map[string]any))
				if err != nil {
					b.Fatal(err)
				}
				if i == 0 {
					currentRef, _, err = packetbody.ReferenceFromIndex(index)
					if err != nil {
						b.Fatal(err)
					}
				}
				indexRows = append(indexRows, index)
			}
			entities["work_packets"] = indexRows
			indexState, err := jsonDocumentBytes(state)
			if err != nil {
				b.Fatal(err)
			}
			// Schema compilation is process-wide, unlike packet reconstruction.
			// Initialize it in setup so short samples compare the reader path.
			if err := validatePacketBodyIndexRow(indexRows[0].(map[string]any)); err != nil {
				b.Fatal(err)
			}
			base, timings := metrics.WithCallTimings(WithPacketBodyRoot(context.Background(), root))
			var reads int64
			base = packetbody.WithResolver(base, func(ctx context.Context, ref packetbody.Reference) ([]byte, error) {
				reads++
				if ref.Path != currentRef.Path {
					return nil, fmt.Errorf("current reader opened unrelated historical body %s", ref.Path)
				}
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				return workpacket.ReadFile(root, ref.Path, packetbody.Limit)
			})
			b.ReportAllocs()
			b.ResetTimer()
			b.StartTimer()
			for range b.N {
				ctx := WithReviewResultReadCache(base)
				packets, err := CurrentReviewResultPacketsContext(ctx, state, current.Binding.OwnerAgentID)
				if err != nil || len(packets) != 1 || packets[0].ID != current.ID {
					b.Fatalf("current responsibility changed: count=%d error=%v", len(packets), err)
				}
			}
			b.StopTimer()
			spans := timings.Snapshot()
			rebuilds := spans["runtime/review_packet_selected_build"].Count
			hits := spans["runtime/review_packet_selected_hit"].Count
			if reads != int64(b.N) || rebuilds != int64(b.N) || hits != 0 || spans["runtime/review_packet_index_build"].Count != 0 {
				b.Fatalf("lookup rebuilt history or reused another request: reads=%d builds=%d hits=%d full-index-builds=%d iterations=%d", reads, rebuilds, hits, spans["runtime/review_packet_index_build"].Count, b.N)
			}
			b.ReportMetric(float64(len(inlineState)), "state-inline-bytes")
			b.ReportMetric(float64(len(indexState)), "state-index-bytes")
			b.ReportMetric(float64(currentRef.SizeBytes), "current-body-bytes")
			b.ReportMetric(float64(reads)/float64(b.N), "body-read/op")
			b.ReportMetric(float64(rebuilds)/float64(b.N), "selected-rebuild/op")
			b.ReportMetric(float64(hits)/float64(b.N), "selected-cache-hit/op")
		})
	}
}
