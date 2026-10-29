package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/cli"
	"github.com/entroforge/go-system-builder/internal/dispatch"
	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

func TestActualMainCheckBusyObservationDoesNotStartAnotherIntegration(t *testing.T) {
	testActualMainIntegrationBarrier(t, false)
}

func TestActualMainIntegrationBarrierCleanupAfterEarlyReturn(t *testing.T) {
	testActualMainIntegrationBarrier(t, true)
}

func testActualMainIntegrationBarrier(t *testing.T, returnWhileBusy bool) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX barrier wrapper requires native Windows equivalent; production probe still cross-builds")
	}
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	realGo, err = filepath.Abs(realGo)
	if err != nil {
		t.Fatal(err)
	}
	f := coreBuilderFixture(t)
	barrier := t.TempDir()
	started := filepath.Join(barrier, "started")
	released := filepath.Join(barrier, "released")
	armed := filepath.Join(barrier, "armed")
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'" }
	shim := filepath.Join(barrier, "go")
	// This wrapper only holds the real original Main check at a test barrier.
	// It always executes the actual Go binary; it does not synthesize PASS.
	script := "#!/bin/sh\nif [ \"$(pwd -P)\" = " + quote(f.root) + " ] && [ \"$1\" = test ] && [ -f " + quote(armed) + " ]; then\n printf 'start\\n' >> " + quote(started) + "\n while [ ! -f " + quote(released) + " ]; do\n  [ -d " + quote(barrier) + " ] || exit 125\n  sleep 0.01\n done\nfi\nexec " + quote(realGo) + " \"$@\"\n"
	if err := os.WriteFile(shim, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", barrier+string(os.PathListSeparator)+os.Getenv("PATH"))
	done := make(chan struct{})
	// Cleanup runs before TempDir removal. The integration goroutine must
	// observe release and finish before its barrier/root can disappear, even
	// when an assertion below fails. The wrapper also exits if a killed test
	// process loses its barrier directory.
	t.Cleanup(func() {
		if err := os.WriteFile(released, []byte("release"), 0600); err != nil {
			t.Error(err)
		}
		select {
		case <-done:
		case <-time.After(90 * time.Second):
			t.Error("original integration did not finish during barrier cleanup")
		}
	})
	go func() {
		defer close(done)
		runCoreBuilderIntegration(t, f, func(e workspace.Execution) {
			if err := os.WriteFile(armed, []byte("arm"), 0600); err != nil {
				t.Error(err)
			}
		})
	}()
	deadline := time.Now().Add(90 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		select {
		case <-done:
			t.Fatal("original integration ended before real Main check barrier")
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("original Main check did not reach barrier")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if returnWhileBusy {
		// Exercise the same registered cleanup used by a failing assertion:
		// the original check is still live when the test body returns.
		return
	}
	before := stageSnapshot(t, f.root)
	view := stageCLI(t, "runtime", "workspace", "integration-status", "--root", f.root)
	if err := schema.NewEmbeddedValidator().ValidateBytes("integration-observation.schema.json", []byte(view)); err != nil {
		t.Fatal(err)
	}
	var observed integration.WorkspaceObservation
	if err := json.Unmarshal([]byte(view), &observed); err != nil || observed.LockState != "busy" || observed.Authorizes {
		t.Fatal(view, err)
	}
	adviceJSON := stageCLI(t, "s6", "status", "--root", f.root, "--advice", "--capacity", "1", "--json")
	var board struct {
		Advice dispatch.ReceptionAdvice `json:"reception_advice"`
	}
	if err := json.Unmarshal([]byte(adviceJSON), &board); err != nil || board.Advice.Action.Kind != "observe_integration" || len(board.Advice.Pending) != 1 {
		t.Fatal(adviceJSON, err)
	}
	if stageSnapshot(t, f.root).Revision != before.Revision {
		t.Fatal("observation changed original Runtime")
	}
	guidance := cli.BuildGuidanceForState(f.root, before.State, "SessionStart", policy.Input{})
	context := hook.BuildLifecycleAdditionalContext("SessionStart", policy.Input{}, policy.Decision{Guidance: &guidance}, nil)
	if !strings.Contains(context, "S6 current Main action: observe_integration") {
		t.Fatal("real busy signal missing from native Main SessionStart", context)
	}
	var stdout, stderr bytes.Buffer
	if code := cli.Run([]string{"runtime", "task-integrate", "--root", f.root, "--assignment-id", "assignment-core-builder", "--agent-id", "core-builder"}, nil, &stdout, &stderr); code == 0 || !strings.Contains(stderr.String(), "integration workspace busy") {
		t.Fatalf("second original request was not rejected: %d %s", code, stderr.String())
	}
	starts, err := os.ReadFile(started)
	if err != nil || string(starts) != "start\n" {
		t.Fatal("duplicate Main required check started", string(starts), err)
	}
	if err := os.WriteFile(released, []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(90 * time.Second):
		t.Fatal("original integration did not finish after actual Go check release")
	}
	if o := integration.ObserveWorkspace(f.root); o.LockState != "available" {
		t.Fatal(o)
	}
	assertCoreBuilderReceipt(t, f.root, "core-builder", "assignment-core-builder", true, true)
}
