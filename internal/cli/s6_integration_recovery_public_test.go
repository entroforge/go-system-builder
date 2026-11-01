package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func s6RecoveryCommand(t *testing.T, root string, r assignmentrecord.Record, retry bool) (map[string]any, string) {
	t.Helper()
	args := []string{"runtime", "task-integrate", "--root", root, "--assignment-id", r.AssignmentID, "--agent-id", r.OwnerAgentID}
	if retry {
		args = append(args, "--retry-preserved")
	}
	var out, diagnostic bytes.Buffer
	if code := cli.Run(args, nil, &out, &diagnostic); code != 0 {
		t.Fatalf("Main integration: %d %s %s", code, &out, &diagnostic)
	}
	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatal(err, out.String())
	}
	return payload, diagnostic.String()
}

func s6RecoveryAccepted(t *testing.T) (string, string, assignmentrecord.Record, string) {
	t.Helper()
	root, worker, r := nativeDeliveryFixture(t)
	nativeCommit(t, worker)
	nativeAnswer(t, worker, r)
	brief, _, result := assignmentrecord.NativePaths(r)
	if code, out := nativeEvent(t, root, worker, "SubagentStop", filepath.Join(root, brief)+"\n"+filepath.Join(worker, result), false); code != 0 {
		t.Fatalf("receive original Result: %d %s", code, out)
	}
	head, err := workspace.Git(t.Context(), worker, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return root, worker, r, head
}

func TestS6MainInspectFailureWithoutCheckpointRestoresOriginalSource(t *testing.T) {
	root, worker, r, _ := s6RecoveryAccepted(t)
	if _, err := workspace.Git(t.Context(), root, "worktree", "remove", "--force", worker); err != nil {
		t.Fatal(err)
	}
	payload, diagnostic := s6RecoveryCommand(t, root, r, false)
	for _, want := range []string{"blocker: worktree inspection failed:", "is missing", worker, "No integration checkpoint exists", "checkpoint_state=absent", "--root", r.AssignmentID, r.OwnerAgentID} {
		if !strings.Contains(diagnostic, want) {
			t.Fatalf("missing %q: %s", want, diagnostic)
		}
	}
	if payload["state"] != "blocked" || payload["blocked"] != true || strings.Contains(diagnostic, "--retry-preserved") || strings.Contains(diagnostic, "preserved/blocked") {
		t.Fatalf("missing checkpoint advertised as retryable: %+v %s", payload, diagnostic)
	}
	slot := assignmentrecord.IntegrationCheckpointPath(root, r.Reference())
	if _, found, err := integration.DefaultCheckpointStore().Load(slot); err != nil || found {
		t.Fatalf("Inspect error wrote checkpoint: %v %v", found, err)
	}
	var invalidRetryOut, invalidRetryDiagnostic bytes.Buffer
	if code := cli.Run([]string{"runtime", "task-integrate", "--root", root, "--assignment-id", r.AssignmentID, "--agent-id", r.OwnerAgentID, "--retry-preserved"}, nil, &invalidRetryOut, &invalidRetryDiagnostic); code != 1 || !strings.Contains(invalidRetryDiagnostic.String(), "no integration checkpoint exists") || !strings.Contains(invalidRetryDiagnostic.String(), "then run `loop-harness runtime task-integrate --root") {
		t.Fatalf("unsupported retry omitted the real original entry: %d %s %s", code, &invalidRetryOut, &invalidRetryDiagnostic)
	}
	if _, err := workspace.Git(t.Context(), root, "worktree", "add", worker, "native/delivery"); err != nil {
		t.Fatal(err)
	}
	payload, diagnostic = s6RecoveryCommand(t, root, r, false)
	cp, found, err := integration.DefaultCheckpointStore().Load(slot)
	if err != nil || !found || cp.State != integration.StateComplete || cp.CompletionReportPath == "" || payload["blocked"] != false {
		t.Fatalf("original entry did not recover accepted source: %+v %v %s", cp, err, diagnostic)
	}
}

func TestS6MainPreservedCheckpointNamesActualRetryAndRetainsSource(t *testing.T) {
	for _, historicalBlocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "preserved", true: "legacy-blocked"}[historicalBlocked], func(t *testing.T) {
			root, worker, r, _ := s6RecoveryAccepted(t)
			proof := filepath.Join(worker, "src/pkg/proof.txt")
			if err := os.WriteFile(proof, []byte("Uncommitted work must survive the failed integration.\n"), 0600); err != nil {
				t.Fatal(err)
			}
			payload, diagnostic := s6RecoveryCommand(t, root, r, false)
			for _, want := range []string{"checkpoint_state=preserved", "--retry-preserved", "blocker:", "--root", r.AssignmentID} {
				if !strings.Contains(diagnostic, want) {
					t.Fatalf("missing %q: %s", want, diagnostic)
				}
			}
			if payload["state"] != integration.StatePreserved || payload["blocked"] != true {
				t.Fatalf("incorrect preserved result: %+v", payload)
			}
			if _, err := os.Stat(proof); err != nil {
				t.Fatal("failed integration removed dirty source", err)
			}
			if _, err := workspace.Git(t.Context(), worker, "restore", "src/pkg/proof.txt"); err != nil {
				t.Fatal(err)
			}
			if historicalBlocked {
				// blocked is an already-issued checkpoint state accepted by the
				// original recovery path; current inspection failures write preserved.
				store := integration.DefaultCheckpointStore()
				slot := assignmentrecord.IntegrationCheckpointPath(root, r.Reference())
				prior, found, err := store.Load(slot)
				if err != nil || !found {
					t.Fatalf("load original failed checkpoint: %v", err)
				}
				next := prior
				next.State = integration.StateBlocked
				if _, err := store.CompareAndSwap(slot, prior, next); err != nil {
					t.Fatal(err)
				}
				blocked, text := s6RecoveryCommand(t, root, r, false)
				if blocked["state"] != integration.StateBlocked || !strings.Contains(text, "checkpoint_state=blocked") || !strings.Contains(text, "--retry-preserved") || strings.Contains(text, "task-integrate: preserved") {
					t.Fatalf("legacy blocked checkpoint misrepresented: %+v %s", blocked, text)
				}
			}
			payload, diagnostic = s6RecoveryCommand(t, root, r, true)
			cp, found, err := integration.DefaultCheckpointStore().Load(assignmentrecord.IntegrationCheckpointPath(root, r.Reference()))
			if err != nil || !found || cp.State != integration.StateComplete || cp.CompletionReportPath == "" || payload["blocked"] != false {
				t.Fatalf("retry did not retain original accepted source: %+v %v %s", cp, err, diagnostic)
			}
		})
	}
}
