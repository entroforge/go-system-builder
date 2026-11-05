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
	// The canonical S6 recovery fixture owns the original TR-003 common Main
	// baseline of the current generation, established by the real TR-003 Writer
	// (internal/cli/s6_observation_test.go:27 newS6MainBaselineFixture). The S6
	// batch recheck needs that baseline before it may read the current batch,
	// so the recovery path is exercised against a real baseline instead of a
	// compressed fixture without one.
	root, worker, r := nativeDeliveryFixtureWithMainBaseline(t)
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
	// A missing workspace is an observation failure, not a new reception or
	// stage gate: the diagnostic must name the unobservable path and the
	// unknown ownership instead of blocking the integration
	// (L3-S6-build.md:361, L4-workflow-convergence.md:430).
	for _, want := range []string{"could not be observed", "unknown", worker} {
		if !strings.Contains(diagnostic, want) {
			t.Fatalf("missing %q: %s", want, diagnostic)
		}
	}
	for _, unwanted := range []string{"blocker: worktree inspection failed:", "No integration checkpoint exists", "checkpoint_state=absent", "--retry-preserved"} {
		if strings.Contains(diagnostic, unwanted) {
			t.Fatalf("observation failure became a stage gate (%q): %s", unwanted, diagnostic)
		}
	}
	if payload["state"] != "integrated" || payload["blocked"] != false {
		t.Fatalf("missing workspace blocked the integration: %+v %s", payload, diagnostic)
	}
	slot := assignmentrecord.IntegrationCheckpointPath(root, r.Reference())
	cp, found, err := integration.DefaultCheckpointStore().Load(slot)
	if err != nil || !found || cp.State != integration.StateComplete || cp.CompletionReportPath == "" {
		t.Fatalf("branch integration did not complete without the workspace: %+v %v %s", cp, err, diagnostic)
	}
	// --retry-preserved belongs to a real check failure that left a
	// preserved/blocked checkpoint (L4-agent-guidance.md:318). This
	// integration already completed, so the feedback must name the real
	// original entry command and say to continue without the parameter.
	var invalidRetryOut, invalidRetryDiagnostic bytes.Buffer
	if code := cli.Run([]string{"runtime", "task-integrate", "--root", root, "--assignment-id", r.AssignmentID, "--agent-id", r.OwnerAgentID, "--retry-preserved"}, nil, &invalidRetryOut, &invalidRetryDiagnostic); code != 1 ||
		!strings.Contains(invalidRetryDiagnostic.String(), "retry requires an existing preserved/blocked/merged checkpoint") ||
		!strings.Contains(invalidRetryDiagnostic.String(), "current checkpoint_state=complete") ||
		!strings.Contains(invalidRetryDiagnostic.String(), "loop-harness runtime task-integrate --root") ||
		!strings.Contains(invalidRetryDiagnostic.String(), "without --retry-preserved") ||
		strings.Contains(invalidRetryDiagnostic.String(), "no integration checkpoint exists") {
		t.Fatalf("unsupported retry omitted the real original entry: %d %s %s", code, &invalidRetryOut, &invalidRetryDiagnostic)
	}
	if _, err := workspace.Git(t.Context(), root, "worktree", "add", worker, "native/delivery"); err != nil {
		t.Fatal(err)
	}
	payload, diagnostic = s6RecoveryCommand(t, root, r, false)
	cp, found, err = integration.DefaultCheckpointStore().Load(slot)
	if err != nil || !found || cp.State != integration.StateComplete || cp.CompletionReportPath == "" || payload["blocked"] != false {
		t.Fatalf("original entry did not recover accepted source: %+v %v %s", cp, err, diagnostic)
	}
}

func TestS6MainPreservedCheckpointNamesActualRetryAndRetainsSource(t *testing.T) {
	for _, historicalBlocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "cleanup-pending", true: "legacy-blocked"}[historicalBlocked], func(t *testing.T) {
			root, worker, r, _ := s6RecoveryAccepted(t)
			proof := filepath.Join(worker, "src/pkg/proof.txt")
			if err := os.WriteFile(proof, []byte("Uncommitted work must survive the failed integration.\n"), 0600); err != nil {
				t.Fatal(err)
			}
			payload, diagnostic := s6RecoveryCommand(t, root, r, false)
			// Retained material is a resource to keep, not an integration
			// failure: the integration still completes and the leftover
			// material is reported as cleanup_pending
			// (L4-workflow-convergence.md:435, L4-workflow-convergence.md:302).
			for _, want := range []string{"integration verified", "completion_ack recorded", "checkpoint_state=cleanup_pending", "preserve worktree", worker, "src/pkg/proof.txt"} {
				if !strings.Contains(diagnostic, want) {
					t.Fatalf("missing %q: %s", want, diagnostic)
				}
			}
			for _, unwanted := range []string{"checkpoint_state=preserved", "checkpoint_state=blocked", "--retry-preserved", "blocker:"} {
				if strings.Contains(diagnostic, unwanted) {
					t.Fatalf("retained material became a stage gate (%q): %s", unwanted, diagnostic)
				}
			}
			if payload["state"] != "integrated" || payload["blocked"] != false {
				t.Fatalf("retained material blocked the integration: %+v %s", payload, diagnostic)
			}
			cp, found, err := integration.DefaultCheckpointStore().Load(assignmentrecord.IntegrationCheckpointPath(root, r.Reference()))
			if err != nil || !found || cp.State != integration.StateCleanupPending {
				t.Fatalf("retained material was not left as cleanup_pending: %+v %v %s", cp, err, diagnostic)
			}
			if _, err := os.Stat(proof); err != nil {
				t.Fatal("failed integration removed dirty source", err)
			}
			if _, err := workspace.Git(t.Context(), worker, "restore", "src/pkg/proof.txt"); err != nil {
				t.Fatal(err)
			}
			if historicalBlocked {
				// A legacy blocked checkpoint is the recorded shape of a real
				// check failure: it keeps the failure and the completed
				// boundary it resumes from (L4-worktree-governance.md:129).
				store := integration.DefaultCheckpointStore()
				slot := assignmentrecord.IntegrationCheckpointPath(root, r.Reference())
				prior, found, err := store.Load(slot)
				if err != nil || !found {
					t.Fatalf("load original failed checkpoint: %v", err)
				}
				next := prior
				next.State = integration.StateBlocked
				next.ResumeState = integration.StateMerged
				next.FailureReason = "recorded check failure"
				if _, err := store.CompareAndSwap(slot, prior, next); err != nil {
					t.Fatal(err)
				}
				blocked, text := s6RecoveryCommand(t, root, r, false)
				// The bare command keeps the recorded failure and names the
				// retry condition together with its parameter
				// (L4-agent-guidance.md:318).
				if blocked["state"] != integration.StateBlocked || blocked["blocked"] != true ||
					!strings.Contains(text, "checkpoint_state=blocked") || !strings.Contains(text, "--retry-preserved") ||
					strings.Contains(text, "task-integrate: preserved") {
					t.Fatalf("legacy blocked checkpoint misrepresented: %+v %s", blocked, text)
				}
			}
			// The cleanup follow-up needs no author action and no
			// --retry-preserved (L4-workflow-convergence.md:435); only a real
			// check failure that left a preserved/blocked checkpoint does.
			payload, diagnostic = s6RecoveryCommand(t, root, r, historicalBlocked)
			// Reception, checks and ACK stay established and the stage
			// successor is not gated on the retained resource
			// (L4-workflow-convergence.md:302).
			//
			// A cleanup follow-up is NOT a stage gate, so it names no stage
			// transition of its own: the earlier S7 handoff (TR-006) is
			// already established, and re-naming it here would present the
			// retained worktree as a pending stage obligation.
			for _, want := range []string{"integration verified", "completion_ack recorded", "checkpoint_state=cleanup_pending"} {
				if !strings.Contains(diagnostic, want) {
					t.Fatalf("missing %q: %s", want, diagnostic)
				}
			}
			// The worktree is preserved because it holds unknown/untracked
			// material; cleanup is retried separately and never force-deleted
			// to make the check pass (L4-workflow-convergence.md:435).
			if strings.Contains(diagnostic, "TR-006") {
				t.Fatalf("cleanup follow-up re-named a stage transition: %s", diagnostic)
			}
			for _, unwanted := range []string{"blocker:", "--retry-preserved"} {
				if strings.Contains(diagnostic, unwanted) {
					t.Fatalf("cleanup follow-up became a stage gate (%q): %s", unwanted, diagnostic)
				}
			}
			if payload["state"] != "integrated" || payload["blocked"] != false {
				t.Fatalf("cleanup follow-up blocked the integration: %+v %s", payload, diagnostic)
			}
			cp, found, err = integration.DefaultCheckpointStore().Load(assignmentrecord.IntegrationCheckpointPath(root, r.Reference()))
			if err != nil || !found || cp.State != integration.StateCleanupPending {
				t.Fatalf("retained material was not left as cleanup_pending: %+v %v %s", cp, err, diagnostic)
			}
			// The retained private material survives: the stage passes without
			// deleting the author's files.
			if _, err := os.Stat(filepath.Join(worker, ".claude")); err != nil {
				t.Fatalf("cleanup follow-up deleted retained private material: %v", err)
			}
		})
	}
}
