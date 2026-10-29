package review

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/metrics"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

func TestProductDriftUsesSelectedPacketIndexAndRequestCache(t *testing.T) {
	root, statePath, journalPath, snapshot, plan := packetModeFixture(t)
	packets, err := IssueResultPackets(root, statePath, journalPath, snapshot.Revision, "agent-qa-1")
	if err != nil {
		t.Fatal(err)
	}
	issued, err := loopruntime.NewStore(statePath, journalPath).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	entities := issued.State["entities"].(map[string]any)
	rows := entities["work_packets"].([]any)
	entities["work_packets"] = append(rows, map[string]any{"packet_id": "unrelated-history", "template_id": "s7-review-result-v1", "view_path": ".claude/work-packets/unrelated-history.json"})
	ctx, timings := metrics.WithCallTimings(loopruntime.WithReviewResultReadCache(context.Background()))
	for range 2 {
		drift, err := detectUndeclaredProductDriftWithState(root, plan, issued.State, root, ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range drift {
			if path == packets[0].ViewPath || path == packets[0].AnswerPath {
				t.Fatalf("exact issued path reported as drift: %s", path)
			}
		}
	}
	if timings.Snapshot()["runtime/review_packet_selected_build"].Count != 1 || timings.Snapshot()["runtime/review_packet_selected_hit"].Count < 1 || timings.Snapshot()["runtime/review_packet_index_build"].Count != 0 || timings.Snapshot()["runtime/journal_decode_build"].Count != 1 {
		t.Fatal(timings.Snapshot())
	}
	// Once the view no longer needs an exemption, the answer alone must still
	// require issuance; the older generic submissions row cannot bypass it.
	if err := os.Remove(filepath.Join(root, packets[0].ViewPath)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, []byte("broken\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := detectUndeclaredProductDriftWithState(root, plan, issued.State, root, ctx); !errors.Is(err, errResultPacketDriftProof) {
		t.Fatalf("answer-only exemption bypassed original issuance: %v", err)
	}
}

func TestFrozenSubjectAndDriftCancellationRemainVisible(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	plan := &Plan{}
	if _, err := detectUndeclaredProductDriftWithState(t.TempDir(), plan, nil, "", ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("drift cancellation: %v", err)
	}
	if err := verifyFrozenSubjects(t.TempDir(), plan, nil, ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("legacy frozen-subject path swallowed cancellation: %v", err)
	}
}

func TestProductDriftGitUsesParentDeadline(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nexec sleep 5\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := detectUndeclaredProductDriftWithState(t.TempDir(), &Plan{}, nil, "", ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Git did not preserve parent deadline: %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Git exceeded parent budget: %s", elapsed)
	}
}
