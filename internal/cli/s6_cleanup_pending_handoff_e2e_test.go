package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestS6CleanupPendingKeepsAcknowledgementAndOriginalSuccessor(t *testing.T) {
	for _, explicitOwner := range []bool{false, true} {
		name := "assignment-only"
		if explicitOwner {
			name = "explicit-owner"
		}
		t.Run(name, func(t *testing.T) {
			root, source, r := s6HandoffFixture(t)
			unknown := filepath.Join(source, ".claude", "retained-private-note.txt")
			retained := []byte("unclassified private material\n")
			if err := os.WriteFile(unknown, retained, 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"runtime", "task-integrate", "--root", root, "--assignment-id", r.AssignmentID}
			if explicitOwner {
				args = append(args, "--agent-id", r.OwnerAgentID)
			}
			var firstHead string
			var firstRevision int
			for attempt := 0; attempt < 2; attempt++ {
				var output, diagnostic bytes.Buffer
				if code := cli.Run(args, nil, &output, &diagnostic); code != 0 {
					t.Fatalf("integration %d: %s %s", code, &output, &diagnostic)
				}
				var result struct {
					State       string   `json:"state"`
					Blocked     bool     `json:"blocked"`
					Integration []string `json:"integration"`
					Handoff     string   `json:"handoff"`
				}
				if err := json.Unmarshal(output.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				facts := strings.Join(result.Integration, " ")
				if result.Blocked || result.State != "integrated" || !strings.Contains(facts, "cleanup_pending") || !strings.Contains(facts, "completion_ack recorded") {
					t.Fatalf("lost completed integration facts: %s %s", &output, &diagnostic)
				}
				if attempt == 0 && (!strings.Contains(result.Handoff, "TR-006") || !strings.Contains(result.Handoff, "ReviewPlan questionnaire:")) {
					t.Fatalf("cleanup suppressed original successor: %s", &output)
				}
				snapshot := stageSnapshot(t, root)
				if snapshot.State["lifecycle"].(map[string]any)["state"] != "verification" {
					t.Fatalf("batch successor not reached: %s", &output)
				}
				data, err := os.ReadFile(unknown)
				if err != nil || !bytes.Equal(data, retained) {
					t.Fatalf("retained file changed: %q %v", data, err)
				}
				head, err := workspace.Git(t.Context(), root, "rev-parse", "HEAD")
				if err != nil {
					t.Fatal(err)
				}
				if attempt == 0 {
					firstHead, firstRevision = head, snapshot.Revision
				} else if head != firstHead || snapshot.Revision != firstRevision {
					t.Fatalf("cleanup replay changed completed integration: head %s -> %s, revision %d -> %d", firstHead, head, firstRevision, snapshot.Revision)
				}
			}
		})
	}
}

func TestS6CleanupPendingRecoversBeforeFirstRuntimeProjection(t *testing.T) {
	root, source, record := s6HandoffFixture(t)
	unknown := filepath.Join(source, ".claude", "retained-private-note.txt")
	retained := []byte("unclassified private material\n")
	if err := os.WriteFile(unknown, retained, 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"runtime", "task-integrate", "--root", root, "--assignment-id", record.AssignmentID}
	slot := assignmentrecord.IntegrationCheckpointPath(root, record.Reference())
	store := integration.DefaultCheckpointStore()
	const interrupted = "cleanup checkpoint saved before Runtime projection"
	caught := false
	func() {
		originalClock := store.Clock
		defer func() { store.Clock = originalClock }()
		store.Clock = func() time.Time {
			cp, found, err := store.Load(slot)
			if err == nil && found && cp.State == integration.StateCleanupPending {
				// The first pending checkpoint and ACK are already durable.
				// Interrupt the cleanup failure write before the controller can
				// publish any SUBAGENT-INTEGRATION Runtime projection.
				panic(interrupted)
			}
			return originalClock()
		}
		defer func() {
			if value := recover(); value != nil {
				if value != interrupted {
					panic(value)
				}
				caught = true
			}
		}()
		cli.Run(args, nil, &bytes.Buffer{}, &bytes.Buffer{})
	}()
	before, found, err := store.Load(slot)
	if !caught || err != nil || !found || before.State != integration.StateCleanupPending || before.VerifiedAt == "" {
		t.Fatalf("pending interruption not reached: caught=%t cp=%+v err=%v", caught, before, err)
	}
	journalPath := filepath.Join(root, ".claude", "loop-events.jsonl")
	journal, err := os.ReadFile(journalPath)
	if err != nil || bytes.Contains(journal, []byte("SUBAGENT-INTEGRATION")) {
		t.Fatalf("interruption already published Runtime projection: %s %v", journal, err)
	}
	head, err := workspace.Git(t.Context(), root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	var output, diagnostic bytes.Buffer
	if code := cli.Run(args, nil, &output, &diagnostic); code != 0 {
		t.Fatalf("pending recovery: %d %s %s", code, &output, &diagnostic)
	}
	// Git summarizes ignored directories; report the actual worktree and
	// retained directory rather than claiming it enumerated every private file.
	for _, fact := range []string{"cleanup_pending", "completion_ack recorded", source, ".claude/", "cleanup authorization", "TR-006", "ReviewPlan questionnaire:"} {
		if !strings.Contains(output.String()+diagnostic.String(), fact) {
			t.Fatalf("first recovery omitted %q: %s %s", fact, &output, &diagnostic)
		}
	}
	after, found, err := store.Load(slot)
	if err != nil || !found || after.VerifiedAt != before.VerifiedAt || after.CompletionReportPath != before.CompletionReportPath || strings.Join(after.CheckReceipts, "\n") != strings.Join(before.CheckReceipts, "\n") {
		t.Fatalf("pending recovery changed completed integration: before=%+v after=%+v err=%v", before, after, err)
	}
	if currentHead, err := workspace.Git(t.Context(), root, "rev-parse", "HEAD"); err != nil || currentHead != head {
		t.Fatalf("pending recovery repeated merge: %s -> %s: %v", head, currentHead, err)
	}
	if data, err := os.ReadFile(unknown); err != nil || !bytes.Equal(data, retained) {
		t.Fatalf("pending recovery changed retained file: %q %v", data, err)
	}
	journal, err = os.ReadFile(journalPath)
	if err != nil || bytes.Contains(journal, []byte("SUBAGENT-INTEGRATION")) {
		t.Fatalf("pending recovery invented Runtime integration projection: %s %v", journal, err)
	}
	if lifecycle := stageSnapshot(t, root).State["lifecycle"].(map[string]any)["state"]; lifecycle != "verification" {
		t.Fatalf("pending recovery omitted original successor: %v", lifecycle)
	}
}
