package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/filelock"
	"github.com/entroforge/go-system-builder/internal/metrics"
)

func auditRecord(id string) map[string]any {
	return map[string]any{"decision_id": id, "decision": "audit"}
}
func auditLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func TestDecisionIndexReusesHistoryAndRepairsCacheOnReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hook-decisions.jsonl")
	out := NewOutbox(path)
	ctx, timings := metrics.WithCallTimings(context.Background())
	if err := out.AppendContext(ctx, auditRecord("a")); err != nil {
		t.Fatal(err)
	}
	if err := out.AppendContext(ctx, auditRecord("a")); err != nil {
		t.Fatal(err)
	}
	if spans := timings.Snapshot(); spans["audit/index_hit"].Count != 1 || spans["audit/index_rebuild"].Count != 1 {
		t.Fatalf("hot path did not reuse index: %+v", spans)
	}
	for _, damage := range []string{"missing", "corrupt", "oversized", "wrong decisions"} {
		t.Run(damage, func(t *testing.T) {
			switch damage {
			case "missing":
				if err := os.Remove(path + ".index"); err != nil {
					t.Fatal(err)
				}
			case "corrupt":
				if err := os.WriteFile(path+".index", []byte("broken"), 0600); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				f, err := os.OpenFile(path+".index", os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				if err := f.Truncate(decisionIndexLimit + 1); err != nil {
					t.Fatal(err)
				}
				f.Close()
			case "wrong decisions":
				data, err := os.ReadFile(path + ".index")
				if err != nil {
					t.Fatal(err)
				}
				var envelope decisionIndexEnvelope
				if err := json.Unmarshal(data, &envelope); err != nil {
					t.Fatal(err)
				}
				envelope.Payload = []byte(strings.ReplaceAll(string(envelope.Payload), `"a":true`, `"x":true`))
				data, err = json.Marshal(envelope)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path+".index", data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := out.AppendContext(ctx, auditRecord("a")); err != nil {
				t.Fatalf("replay failed to repair cache: %v", err)
			}
			if lines := auditLines(t, path); len(lines) != 1 {
				t.Fatalf("cache damage duplicated event: %v", lines)
			}
			stat, err := os.Stat(path + ".index")
			if err != nil || stat.Size() > decisionIndexLimit {
				t.Fatalf("index was not repaired: %v %v", stat, err)
			}
		})
	}
}

func TestDecisionIndexDetectsSameSizeSameMtimeLogRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hook-decisions.jsonl")
	out := NewOutbox(path)
	if err := out.Append(auditRecord("a")); err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.ReplaceAll(string(data), `"decision_id":"a"`, `"decision_id":"b"`))
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, stat.ModTime(), stat.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := out.Append(auditRecord("a")); err != nil {
		t.Fatal(err)
	}
	if lines := auditLines(t, path); len(lines) != 2 || !strings.Contains(lines[0], `"decision_id":"b"`) || !strings.Contains(lines[1], `"decision_id":"a"`) {
		t.Fatalf("metadata-only cache accepted altered prefix: %v", lines)
	}
}

func TestDecisionIndexRebuildsAfterLogTruncateReplaceOrCrossRootCopy(t *testing.T) {
	for _, mode := range []string{"truncate", "replace", "copy index"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "hook-decisions.jsonl")
			out := NewOutbox(path)
			if err := out.Append(auditRecord("a")); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "truncate":
				if err := os.Truncate(path, 0); err != nil {
					t.Fatal(err)
				}
			case "replace":
				temp := path + ".replacement"
				if err := os.WriteFile(temp, []byte("{\"decision_id\":\"b\"}\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(temp, path); err != nil {
					t.Fatal(err)
				}
			case "copy index":
				other := filepath.Join(t.TempDir(), "hook-decisions.jsonl")
				if err := NewOutbox(other).Append(auditRecord("b")); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(other + ".index")
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path+".index", data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := out.Append(auditRecord("a")); err != nil {
				t.Fatal(err)
			}
			lines := auditLines(t, path)
			want := 1
			if mode == "replace" {
				want = 2
			}
			if len(lines) != want {
				t.Fatalf("stale index changed real dedup: %v", lines)
			}
		})
	}
}

func TestDecisionIndexRejectsMalformedOrTruncatedLog(t *testing.T) {
	for _, tail := range []string{"{bad json}\n", `{"decision_id":"unfinished"}`, `{"decision_id":"partial`} {
		t.Run(tail, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "hook-decisions.jsonl")
			out := NewOutbox(path)
			if err := out.Append(auditRecord("a")); err != nil {
				t.Fatal(err)
			}
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString(tail); err != nil {
				t.Fatal(err)
			}
			f.Close()
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := out.Append(auditRecord("new")); err == nil {
				t.Fatal("malformed history was waived by cache")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(before) {
				t.Fatalf("failed audit rewrote authoritative history: %v", err)
			}
		})
	}
}

func TestDecisionIndexFailureAfterDurableAppendReplaysOriginalEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hook-decisions.jsonl")
	if err := os.Mkdir(path+".index", 0700); err != nil {
		t.Fatal(err)
	}
	out := NewOutbox(path)
	err := out.Append(auditRecord("a"))
	if err == nil || !strings.Contains(err.Error(), "appended; decision index update incomplete") {
		t.Fatalf("durable event/index failure not distinguished: %v", err)
	}
	if lines := auditLines(t, path); len(lines) != 1 {
		t.Fatalf("event not durable: %v", lines)
	}
	if err := os.Remove(path + ".index"); err != nil {
		t.Fatal(err)
	}
	if err := out.Append(auditRecord("a")); err != nil {
		t.Fatal(err)
	}
	if lines := auditLines(t, path); len(lines) != 1 {
		t.Fatalf("recovery duplicated durable event: %v", lines)
	}
}

func TestAppendContextCancelsLockWaitAndDoesNotAppend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hook-decisions.jsonl")
	release, err := filelock.Acquire(context.Background(), path+".lock.process")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	started := time.Now()
	if err := NewOutbox(path).AppendContext(ctx, auditRecord("a")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock did not inherit request deadline: %v", err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("lock waited for its detached local budget")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("canceled request created log: %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if err := NewOutbox(path).AppendContext(ctx, auditRecord("a")); !errors.Is(err, context.Canceled) {
		t.Fatalf("explicit cancellation lost: %v", err)
	}
}

func TestConcurrentOutboxesPreserveAllDistinctDecisions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hook-decisions.jsonl")
	const count = 24
	var group sync.WaitGroup
	errors := make(chan error, count*2)
	for i := 0; i < count*2; i++ {
		group.Add(1)
		go func(i int) { defer group.Done(); errors <- NewOutbox(path).Append(auditRecord(fmt.Sprint(i % count))) }(i)
	}
	group.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	lines := auditLines(t, path)
	if len(lines) != count {
		t.Fatalf("concurrent append lost/duplicated decisions: %d", len(lines))
	}
	decisions := map[string]bool{}
	for _, line := range lines {
		var row struct {
			DecisionID string `json:"decision_id"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		if decisions[row.DecisionID] {
			t.Fatalf("duplicate decision %s", row.DecisionID)
		}
		decisions[row.DecisionID] = true
	}
	if err := NewOutbox(path).Append(auditRecord("0")); err != nil {
		t.Fatal(err)
	}
	if len(auditLines(t, path)) != count {
		t.Fatal("last concurrent index was not consistent")
	}
}

func BenchmarkOutboxHistoryReplay(b *testing.B) {
	for _, count := range []int{64, 1024, 8192} {
		b.Run(fmt.Sprintf("events=%d", count), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "hook-decisions.jsonl")
			var history strings.Builder
			for i := 0; i < count; i++ {
				fmt.Fprintf(&history, "{\"decision_id\":\"%d\",\"body\":\"%s\"}\n", i, strings.Repeat("x", 2048))
			}
			if err := os.WriteFile(path, []byte(history.String()), 0600); err != nil {
				b.Fatal(err)
			}
			out := NewOutbox(path)
			record := auditRecord(fmt.Sprint(count - 1))
			if err := out.Append(record); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(history.Len()))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := out.AppendContext(context.Background(), record); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestAppendRejectsUnterminatedOriginalAuditLineWithoutChangingBytes(t *testing.T) {
	for _, test := range []struct {
		name, original string
		accepted       bool
	}{
		{"complete JSON without newline", `{"decision_id":"original"}`, false},
		{"partial JSON tail", `{"decision_id":"partial`, false},
		{"terminated complete JSON", "{\"decision_id\":\"original\"}\n", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "hook-decisions.jsonl")
			if err := os.WriteFile(path, []byte(test.original), 0600); err != nil {
				t.Fatal(err)
			}
			err := NewOutbox(path).Append(auditRecord("next"))
			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !test.accepted {
				if err == nil || !strings.Contains(err.Error(), "audit outbox has an incomplete trailing line") {
					t.Fatalf("unfinished original log not diagnosed: %v", err)
				}
				if string(after) != test.original {
					t.Fatalf("Append repaired/discarded original unfinished bytes: %q", after)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			appended, err := json.Marshal(auditRecord("next"))
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != test.original+string(appended)+"\n" {
				t.Fatalf("normal append changed original terminated event: %q", after)
			}
		})
	}
}
