package review

import (
	"context"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"testing"
	"time"

	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

// The context is consulted when the original mutation Writer derives its lock
// budget. Change evidence there, after preflight and before Apply, without
// timing races, a second receiver, or a production-only testing hook.
type changeEvidenceAtReviewWriter struct {
	context.Context
	once    sync.Once
	action  func()
	changed bool
}

func (c *changeEvidenceAtReviewWriter) Deadline() (time.Time, bool) {
	callers := make([]uintptr, 64)
	count := goruntime.Callers(2, callers)
	frames := goruntime.CallersFrames(callers[:count])
	for {
		frame, more := frames.Next()
		if strings.Contains(frame.Function, "/internal/runtime.(*Store).Update") {
			c.once.Do(func() { c.action(); c.changed = true })
			break
		}
		if !more {
			break
		}
	}
	return c.Context.Deadline()
}

func TestReviewResultRechecksActualEvidenceAfterPreflight(t *testing.T) {
	for _, mode := range []string{"rewrite", "remove"} {
		t.Run(mode, func(t *testing.T) {
			root, sp, jp, before, plan := packetModeFixture(t)
			const id = "review-result-evidence-freshness"
			input := writeResultFile(t, root, plan, "assignment-qa-1", id, "agent-qa-1", "pass", map[string]string{"claim-qa-1": "pass", "claim-qa-2": "pass"}, nil)
			proof := filepath.Join(root, "docs/reports/claim-qa-1-observed.md")
			ctx := &changeEvidenceAtReviewWriter{Context: context.Background()}
			ctx.action = func() {
				var err error
				if mode == "remove" {
					err = os.Remove(proof)
				} else {
					err = os.WriteFile(proof, []byte("changed after preflight\n"), 0600)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			_, err := SubmitResult(root, sp, jp, SubmitRequest{Context: ctx, ExpectedRevision: before.Revision, AssignmentID: "assignment-qa-1", ResultPath: input})
			if !ctx.changed {
				t.Fatal("test never reached original mutation Writer after preflight")
			}
			if err == nil || !strings.Contains(err.Error(), "evidence") {
				t.Fatalf("changed disk evidence was accepted: %v", err)
			}
			after, err := loopruntime.NewStore(sp, jp).Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			if after.Revision != before.Revision {
				t.Fatalf("evidence drift committed a reception: %d -> %d", before.Revision, after.Revision)
			}
			artifact := filepath.Join(root, ".claude/evidence", before.State["runtime_id"].(string), "g1/reviews/agent-qa-1", id+".json")
			if _, err := os.Stat(artifact); !os.IsNotExist(err) {
				t.Fatalf("mutable evidence failed after publication instead of before: %v", err)
			}
		})
	}
}
