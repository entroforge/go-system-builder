package investigation_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/investigation"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
)

type approvalKillRequest struct {
	Root, StatePath, JournalPath string
	Request                      investigation.ContractRequest
	Step                         runtime.CommitFailureStep
}

type approvalKillBoundary approvalKillRequest

func (r approvalKillBoundary) Inject(step runtime.CommitFailureStep) error {
	if step != r.Step {
		return nil
	}
	if err := os.WriteFile(filepath.Join(r.Root, "approval-child-ready"), []byte(step), 0600); err != nil {
		return err
	}
	for {
		time.Sleep(time.Hour)
	}
}

func TestContractApprovalBoundaryChild(t *testing.T) {
	path := os.Getenv("GSB_APPROVAL_BOUNDARY_CHILD")
	if path == "" {
		t.Skip("real SIGKILL child helper")
	}
	var r approvalKillRequest
	if err := json.Unmarshal(mustRead(t, path), &r); err != nil {
		t.Fatal(err)
	}
	_, err := investigation.ApproveContractWithTestWriter(r.Root, r.StatePath, r.JournalPath, r.Request, func(s *runtime.Store) *runtime.Store { return s.WithCommitFailureInjector(approvalKillBoundary(r)) })
	t.Fatalf("approval child returned before SIGKILL: %v", err)
}

// These are real domain approval transactions. The child runs the production
// approval pipeline with the existing writer's explicit failure seam; recovery
// and retry use the public CLI, optionally its separately compiled executable.
func TestContractApprovalSIGKILLRecoveryAndSingleConsumption(t *testing.T) {
	for _, step := range []runtime.CommitFailureStep{runtime.CommitBeforePendingMarker, runtime.CommitAfterPendingMarker, runtime.CommitAfterArtifactPublish, runtime.CommitAfterStateWrite, runtime.CommitAfterJournalAppend, runtime.CommitAfterMarkerClear} {
		t.Run(string(step), func(t *testing.T) {
			f, request := approvalTransactionFixture(t)
			journalBefore := mustRead(t, f.journalPath)
			r := approvalKillRequest{Root: f.root, StatePath: f.statePath, JournalPath: f.journalPath, Request: request, Step: step}
			data, _ := json.Marshal(r)
			path := filepath.Join(f.root, "approval-kill-request.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			binary, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(binary, "-test.run=^TestContractApprovalBoundaryChild$")
			cmd.Env = append(os.Environ(), "GSB_APPROVAL_BOUNDARY_CHILD="+path)
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			var childErr error
			go func() { childErr = cmd.Wait(); close(done) }()
			t.Cleanup(func() { _ = cmd.Process.Kill(); <-done })
			deadline := time.NewTimer(10 * time.Second)
			defer deadline.Stop()
			tick := time.NewTicker(10 * time.Millisecond)
			defer tick.Stop()
			for {
				if ready, err := os.ReadFile(filepath.Join(f.root, "approval-child-ready")); err == nil {
					if string(ready) != string(step) {
						t.Fatal("wrong boundary")
					}
					break
				}
				select {
				case <-done:
					t.Fatalf("child exited early: %v %s", childErr, output.String())
				case <-deadline.C:
					t.Fatal("approval child missed boundary")
				case <-tick.C:
				}
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			<-done
			var exited *exec.ExitError
			if !errors.As(childErr, &exited) {
				t.Fatalf("child was not killed: %v", childErr)
			}
			status, ok := exited.Sys().(syscall.WaitStatus)
			if !ok || status.Signal() != syscall.SIGKILL {
				t.Fatalf("wrong signal: %v", childErr)
			}
			if step != runtime.CommitBeforePendingMarker && step != runtime.CommitAfterMarkerClear {
				marker := mustRead(t, f.statePath+".commit-pending.json")
				var pending map[string]any
				_ = json.Unmarshal(marker, &pending)
				if pending["schema_version"] != "2.2.0" {
					t.Fatalf("approval used wrong pending version: %v", pending["schema_version"])
				}
				pending["schema_version"] = "2.1.0"
				downgrade, _ := json.Marshal(pending)
				if err := schema.NewEmbeddedValidator().ValidateBytes("runtime-commit-pending-v2.1.schema.json", downgrade); err == nil {
					t.Fatal("old Repair-only pending schema accepted approval bundle")
				}
				before := mustRead(t, f.statePath)
				if _, err := runtime.NewStore(f.statePath, f.journalPath).Snapshot(); !errors.Is(err, runtime.ErrPendingRuntimeOperation) {
					t.Fatalf("reader recovered pending: %v", err)
				}
				if !bytes.Equal(before, mustRead(t, f.statePath)) || !bytes.Equal(marker, mustRead(t, f.statePath+".commit-pending.json")) {
					t.Fatal("read-only inspection changed pending")
				}
			}
			code, out, diagnostic := approvalCLI(t, f, request)
			if code != 0 {
				t.Fatalf("recover/retry: %d %s %s", code, out, diagnostic)
			}
			var result struct {
				Receipt  *runtime.OperationReceipt `json:"operation_receipt"`
				Replayed bool                      `json:"operation_replayed"`
			}
			if err := json.Unmarshal(out, &result); err != nil || result.Receipt == nil || len(result.Receipt.Artifacts) != 2 {
				t.Fatalf("missing recovered receipt: %v %s", err, out)
			}
			if result.Replayed != (step != runtime.CommitBeforePendingMarker) {
				t.Fatalf("wrong replay=%v", result.Replayed)
			}
			for _, a := range result.Receipt.Artifacts {
				if hash(mustRead(t, filepath.Join(f.root, a.Path))) != a.SHA256 {
					t.Fatal("recovered approval artifact drifted")
				}
			}
			journal := mustRead(t, f.journalPath)
			if bytes.Count(journal, []byte("\n")) != bytes.Count(journalBefore, []byte("\n"))+1 {
				t.Fatal("approval committed more than once")
			}
			state := mustRead(t, f.statePath)
			code, _, diagnostic = approvalCLI(t, f, request)
			if code != 0 || !bytes.Equal(state, mustRead(t, f.statePath)) || !bytes.Equal(journal, mustRead(t, f.journalPath)) {
				t.Fatalf("repeat consumed approval: %d %s", code, diagnostic)
			}
			if _, err := os.Stat(f.statePath + ".commit-pending.json"); !os.IsNotExist(err) {
				t.Fatalf("pending not resolved: %v", err)
			}
			if !strings.Contains(string(journal), "S8-REPAIR-CONTRACT-APPROVAL") {
				t.Fatal("no authority event")
			}
			t.Logf("actual_signal=SIGKILL domain=S8-REPAIR-CONTRACT-APPROVAL boundary=%s durable_artifacts=2 approval_consumptions=1", step)
		})
	}
}
