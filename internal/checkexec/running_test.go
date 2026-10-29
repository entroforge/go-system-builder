package checkexec

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestRunAttemptSerializesAndNeverPublishesPass(t *testing.T) {
	base := t.TempDir()
	key := Hash([]byte("same actual request"))
	binding := Binding{RuntimeID: "runtime", Generation: 1, AssignmentID: "assignment", Owner: "builder", ExecutionGeneration: 1, Responsibility: "owned_check"}
	first, err := BeginRun(context.Background(), base, key, binding)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	_, err = BeginRun(context.Background(), base, key, binding)
	var busy *RunBusy
	if !errors.As(err, &busy) || busy.Ref != first.Path {
		t.Fatal("concurrent request lacked bounded original run ref", err)
	}
	r, found, err := ReadRunRecord(busy.Ref)
	if err != nil || !found || r.State != "running" {
		t.Fatal("running observation missing", err)
	}
	if err := first.Finish("/original/receipt.json", nil); err != nil {
		t.Fatal(err)
	}
	first.Close()
	second, err := BeginRun(context.Background(), base, key, binding)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if second.Record.State != "waiting" || second.Record.ReceiptRef != "" {
		t.Fatal("diagnostic receipt became completion or PASS")
	}
}

func TestReleasedUnfinishedAttemptIsArchivedAsInterrupted(t *testing.T) {
	base := t.TempDir()
	key := Hash([]byte("interrupted"))
	first, err := BeginRun(context.Background(), base, key, Binding{})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	first.Close() // Models process-owned lock release without a finish record.
	second, err := BeginRun(context.Background(), base, key, Binding{})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	r, found, err := ReadRunRecord(filepath.Join(filepath.Dir(second.Path), "interrupted-run.json"))
	if err != nil || !found || r.State != "interrupted" || r.ReceiptRef != "" {
		t.Fatal("interruption inferred receipt acceptance", r, err)
	}
	if second.Record.State != "waiting" {
		t.Fatal("new attempt did not restart")
	}
}

func TestRunLockProcessDeathDoesNotAcceptUnfinishedDiagnostic(t *testing.T) {
	if base := os.Getenv("GSB_CHECK_RUN_CHILD"); base != "" {
		attempt, err := BeginRun(context.Background(), base, Hash([]byte("killed child")), Binding{})
		if err != nil {
			t.Fatal(err)
		}
		if err := attempt.Start(); err != nil {
			t.Fatal(err)
		}
		for {
			time.Sleep(time.Second)
		}
	}
	base := t.TempDir()
	key := Hash([]byte("killed child"))
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(executable, "-test.run=^TestRunLockProcessDeathDoesNotAcceptUnfinishedDiagnostic$", "-test.timeout=30s")
	child.Env = append(os.Environ(), "GSB_CHECK_RUN_CHILD="+base)
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	path := filepath.Join(base, key, "run.json")
	deadline := time.Now().Add(5 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		r, found, _ := ReadRunRecord(path)
		if found && r.State == "running" {
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("child did not acquire original process-owned lock")
	}
	if _, err := BeginRun(context.Background(), base, key, Binding{}); err == nil {
		t.Fatal("live process lock was ignored")
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	recovered, err := BeginRun(context.Background(), base, key, Binding{})
	if err != nil {
		t.Fatal("dead process retained lock", err)
	}
	defer recovered.Close()
	previous, found, err := ReadRunRecord(filepath.Join(filepath.Dir(path), "interrupted-run.json"))
	if err != nil || !found || previous.State != "interrupted" || previous.ReceiptRef != "" {
		t.Fatal("process death manufactured receipt acceptance", previous, err)
	}
}
