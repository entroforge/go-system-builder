package review

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/metrics"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
)

// Exercise the real Store's atomic publisher in another process so the
// original S7 receivers can run while its metrics lock is held. The wrapper
// records actual Git status bytes and can delay returning them until rename.
func TestReviewConcurrentReceiversAcrossMetricsPublication(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the controlled Git wrapper requires a POSIX shell")
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	git, err = filepath.Abs(git)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "metrics-test")
	if out, err := exec.Command("go", "test", "-c", "-o", bin, "../metrics").CombinedOutput(); err != nil {
		t.Fatalf("build controlled metrics publisher: %v %s", err, out)
	}
	wrapperDir := t.TempDir()
	wrapper := filepath.Join(wrapperDir, "git")
	script := fmt.Sprintf(`#!/bin/sh
case "$*" in
  *"status --porcelain"*)
    out="$GSB_STATUS_DIR/status-$$"
    %q "$@" > "$out"
    result=$?
    if tr '\000' '\n' < "$out" | grep -F -q -- "$GSB_STATUS_NAME"; then
      printf 'seen\n' >> "$GSB_STATUS_DIR/seen"
      if [ "$GSB_STATUS_HOLD" = 1 ]; then
        while [ ! -e "$GSB_STATUS_DIR/release" ]; do sleep 0.01; done
      fi
    fi
    cat "$out"
    rm "$out"
    exit "$result"
    ;;
esac
exec %q "$@"
`, git, git)
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	for _, window := range []string{"created", "partial", "rename"} {
		t.Run(window, func(t *testing.T) {
			root, sp, jp, before, packets, requests := issuedConcurrentResultsFixture(t)
			statusDir := t.TempDir()
			t.Setenv("PATH", wrapperDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("GSB_STATUS_DIR", statusDir)
			t.Setenv("GSB_STATUS_HOLD", map[bool]string{true: "1", false: "0"}[window == "rename"])
			defer os.WriteFile(filepath.Join(statusDir, "release"), nil, 0600)

			procCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(procCtx, bin, "-test.run=^TestMetricsControlledPublisher$")
			cmd.Env = append(os.Environ(), "GSB_METRICS_TEST_ROOT="+root)
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			waited := false
			defer func() {
				if !waited {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
			}()
			reader := bufio.NewReader(stdout)
			readStage := func(want string) string {
				t.Helper()
				line, err := reader.ReadString('\n')
				prefix := "STAGE " + want + " "
				if err != nil || !strings.HasPrefix(line, prefix) {
					t.Fatalf("publisher stage %s: %q %v stderr=%s", want, line, err, stderr.String())
				}
				return strings.TrimSuffix(strings.TrimPrefix(line, prefix), "\n")
			}
			step := func() {
				t.Helper()
				if _, err := io.WriteString(stdin, "next\n"); err != nil {
					t.Fatal(err)
				}
			}
			temp := readStage("created")
			if window != "created" {
				step()
				if next := readStage("partial"); next != temp {
					t.Fatalf("producer changed temporary path: %q %q", temp, next)
				}
			}
			if window == "rename" {
				step()
				if next := readStage("rename"); next != temp {
					t.Fatalf("producer changed temporary path: %q %q", temp, next)
				}
			}
			rel, err := filepath.Rel(root, temp)
			if err != nil || !metrics.IsTemporaryPath(filepath.ToSlash(rel)) {
				t.Fatalf("actual publisher name %q: %v", rel, err)
			}
			partial, err := os.ReadFile(temp)
			if err != nil || (window == "created" && len(partial) != 0) || (window == "partial" && (len(partial) == 0 || json.Valid(partial))) || (window == "rename" && !json.Valid(partial)) {
				t.Fatalf("unexpected %s publisher bytes: size=%d valid=%v err=%v", window, len(partial), json.Valid(partial), err)
			}
			t.Setenv("GSB_STATUS_NAME", filepath.ToSlash(rel))
			// The environment is inherited by Git children of this process, not
			// the already running publisher.
			var workers sync.WaitGroup
			start := make(chan struct{})
			outcomes := make([]error, 2)
			for i := range requests {
				requestCtx, requestCancel := context.WithTimeout(context.Background(), 6*time.Second)
				defer requestCancel()
				requests[i].Context = requestCtx
				workers.Add(1)
				go func(i int) {
					defer workers.Done()
					<-start
					_, outcomes[i] = SubmitIssuedResult(root, sp, jp, requests[i])
				}(i)
			}
			close(start)
			deadline := time.Now().Add(5 * time.Second)
			for {
				if data, err := os.ReadFile(filepath.Join(statusDir, "seen")); err == nil && len(data) > 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("Git status did not actually observe the producer temporary")
				}
				time.Sleep(5 * time.Millisecond)
			}
			switch window {
			case "created":
				step()
				readStage("partial")
				step()
				readStage("rename")
				step()
			case "partial":
				step()
				readStage("rename")
				step()
			case "rename":
				step()
			}
			if err := cmd.Wait(); err != nil {
				waited = true
				t.Fatalf("controlled publisher: %v %s", err, stderr.String())
			}
			waited = true
			if _, err := os.Stat(temp); !os.IsNotExist(err) {
				t.Fatalf("rename did not remove temporary leaf: %v", err)
			}
			if err := os.WriteFile(filepath.Join(statusDir, "release"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			workers.Wait()
			for i, err := range outcomes {
				if err != nil {
					t.Fatalf("Reviewer %d lost original reception: %v", i, err)
				}
			}
			current, err := loopruntime.NewStore(sp, jp).Snapshot()
			if err != nil || current.Revision != before.Revision+2 || PlanPointerFromState(current.State).Status != "clean" {
				t.Fatalf("Writer state after two receptions: revision=%d plan=%v err=%v", current.Revision, PlanPointerFromState(current.State), err)
			}
			for i, p := range packets {
				if _, found, err := IssuedResultReceipt(root, current.State, p.ID, p.Binding.OwnerAgentID); err != nil || !found {
					t.Fatalf("original receipt %d: %v %v", i, found, err)
				}
				requests[i].Context = context.Background()
				if _, err := SubmitIssuedResult(root, sp, jp, requests[i]); err != nil {
					t.Fatalf("same-answer replay %d: %v", i, err)
				}
			}
			final, err := loopruntime.NewStore(sp, jp).Snapshot()
			if err != nil || final.Revision != current.Revision {
				t.Fatalf("replay changed Writer revision: %d %v", final.Revision, err)
			}
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(metrics.DefaultRelativePath)))
			snap, readErr := metrics.NewStore(root).Read()
			if err != nil || !json.Valid(data) || readErr != nil || snap.S7ResultSubmits["controlled"] != 1 {
				t.Fatalf("metrics publication lost bytes: read=%v decode=%v controlled=%d", err, readErr, snap.S7ResultSubmits["controlled"])
			}
		})
	}
}
