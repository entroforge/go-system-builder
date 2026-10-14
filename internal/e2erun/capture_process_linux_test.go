package e2erun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/entroforge/go-system-builder/internal/fileview"
)

type captureChildRequest struct {
	ProfileRef FileDigest `json:"profile_ref"`
	Collection Receipt    `json:"collection"`
}

// This subprocess helper never runs in the ordinary suite. Its request and
// synthetic Git authority are constructed by the parent integration fixture.
func TestCaptureBrowserChild(t *testing.T) {
	root := os.Getenv("GSB_E2E_CAPTURE_CHILD_ROOT")
	if root == "" {
		t.Skip("subprocess helper for real SIGKILL capture test")
	}
	data, err := os.ReadFile(filepath.Join(root, "child-request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var input captureChildRequest
	if err := json.Unmarshal(data, &input); err != nil {
		t.Fatal(err)
	}
	view, err := fileview.New(root, "refs/heads/development", []fileview.Rule{{Path: ".", Source: "git_tree"}})
	if err != nil {
		t.Fatal(err)
	}
	selected := []string{}
	for _, test := range input.Collection.Tests {
		selected = append(selected, test.ID)
	}
	request := FormalRequest{Files: view, ProfileRef: input.ProfileRef, Mode: "execution", Binding: input.Collection.Binding, Collection: &input.Collection, SelectedTestIDs: selected}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	capture, err := PrepareCapture(ctx, root, request)
	if err != nil {
		t.Fatal(err)
	}
	defer capture.Close()
	// The test handshakes intent bytes with its parent. This file is not a
	// Runtime registration; full producer/journal crash tests are still needed.
	if err := os.WriteFile(filepath.Join(root, "child-intent.json"), capture.IntentBytes(), 0600); err != nil {
		t.Fatal(err)
	}
	observation, err := capture.Run(ctx)
	t.Fatalf("child should be killed while the second test is active: %v %s", err, observation.Bytes())
}

func exerciseCapturedSIGKILL(t *testing.T, ctx context.Context, profile Profile, config string) {
	t.Helper()
	root := t.TempDir()
	spec := `const {test,expect}=require('playwright/test');
test('first actual browser failure', async({page})=>{
 await page.setContent('<h1>Wrong</h1>');
 expect(await page.textContent('h1')).toBe('Saved');
});
test('second still running', async({page})=>{
 await page.setContent('<h1>Waiting</h1>');
 await new Promise(resolve=>setTimeout(resolve, 60000));
});`
	for name, content := range map[string]string{"playwright.config.cjs": config, "settings.spec.cjs": spec} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	view, ref := testFormalView(t, root, profile)
	request := FormalRequest{Files: view, ProfileRef: ref, Mode: "collection", Binding: Binding{RuntimeID: "loop-killed-fixture", Generation: 1, Round: 1, SourceCommit: view.Commit, SubjectDigest: strings.Repeat("a", 64)}}
	observed, err := RunFormal(ctx, request)
	if err != nil {
		t.Fatalf("SIGKILL fixture collection: %v %s", err, observed.Bytes())
	}
	collection, ok := observed.Receipt()
	if !ok || len(collection.Tests) != 2 {
		t.Fatal("SIGKILL fixture did not collect both tests")
	}
	input, _ := json.Marshal(captureChildRequest{ProfileRef: ref, Collection: collection})
	if err := os.WriteFile(filepath.Join(root, "child-request.json"), input, 0600); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "-test.run=^TestCaptureBrowserChild$")
	command.Env = append(os.Environ(), "GSB_E2E_CAPTURE_CHILD_ROOT="+root)
	var diagnostic bytes.Buffer
	command.Stdout, command.Stderr = &diagnostic, &diagnostic
	command.WaitDelay = time.Second
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var childErr error
	go func() { childErr = command.Wait(); close(done) }()
	t.Cleanup(func() { _ = command.Process.Kill(); <-done })
	deadline := time.NewTimer(45 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	var intentData []byte
	var intent CaptureIntent
	var raw []byte
	for {
		intentData, _ = os.ReadFile(filepath.Join(root, "child-intent.json"))
		if json.Unmarshal(intentData, &intent) == nil && intent.RunID != "" {
			raw, _ = os.ReadFile(filepath.Join(root, captureBase, intent.RunID, "events.jsonl"))
			failed, begins := false, 0
			decoder := json.NewDecoder(bytes.NewReader(raw))
			for {
				var event map[string]any
				if err := decoder.Decode(&event); err != nil {
					if err != io.EOF {
						break // A concurrent append may leave a partial tail.
					}
					break
				}
				failed = failed || (event["type"] == "test_end" && event["status"] == "failed")
				if event["type"] == "test_begin" {
					begins++
				}
			}
			if failed && begins == 2 {
				break
			}
		}
		select {
		case <-done:
			t.Fatalf("child exited before kill: %v %s", childErr, diagnostic.String())
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-deadline.C:
			t.Fatalf("first failed browser attempt did not reach persistent capture: %s", raw)
		case <-tick.C:
		}
	}
	descendants := captureDescendants(t, command.Process.Pid)
	if len(descendants) < 3 {
		t.Fatalf("browser process tree was not observable: %v", descendants)
	}
	// Identity includes /proc start time to avoid mistaking a reused PID for
	// the child under test. Cleanup only targets this observed fixture tree.
	t.Cleanup(func() {
		for pid, identity := range descendants {
			if current, ok := readCaptureProcess(pid); ok && current.start == identity.start && current.state != "Z" {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	<-done
	var exited *exec.ExitError
	if !errors.As(childErr, &exited) {
		t.Fatalf("child was not actually killed: %v", childErr)
	}
	status, ok := exited.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("expected SIGKILL, got %v", childErr)
	}
	stopDeadline := time.NewTimer(5 * time.Second)
	defer stopDeadline.Stop()
	for {
		live := []int{}
		for pid, identity := range descendants {
			if current, ok := readCaptureProcess(pid); ok && current.start == identity.start && current.state != "Z" {
				live = append(live, pid)
			}
		}
		if len(live) == 0 {
			break
		}
		select {
		case <-stopDeadline.C:
			t.Fatalf("adapter death left live browser/isolator descendants: %v", live)
		case <-tick.C:
		}
	}
	recovered, err := InspectInterruptedCapture(root, intentData)
	if err != nil || recovered.Outcome != "unknown" || !strings.Contains(recovered.RawEvents, `"status":"failed"`) {
		t.Fatalf("actual first failure vanished after process death: %+v %v", recovered, err)
	}
	if directory := os.Getenv("GSB_E2E_RECEIPT_DIR"); directory != "" {
		data, _ := json.MarshalIndent(recovered, "", "  ")
		if err := os.WriteFile(filepath.Join(directory, "sigkill-interrupted-capture.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Log(fmt.Sprintf("actual_signal=%s captured_first_failure=true recovery_outcome=%s observed_descendants=%d live_descendants_after_kill=0; adapter-only, no Runtime producer", status.Signal(), recovered.Outcome, len(descendants)))
}

type captureProcess struct {
	parent int
	start  string
	state  string
}

func readCaptureProcess(pid int) (captureProcess, bool) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return captureProcess{}, false
	}
	// comm may itself contain spaces or parentheses. The final ')' ends it.
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return captureProcess{}, false
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return captureProcess{}, false
	}
	parent, err := strconv.Atoi(fields[1])
	return captureProcess{parent: parent, start: fields[19], state: fields[0]}, err == nil
}

func captureDescendants(t *testing.T, parent int) map[int]captureProcess {
	t.Helper()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatal(err)
	}
	all := map[int]captureProcess{}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		if identity, ok := readCaptureProcess(pid); ok {
			all[pid] = identity
		}
	}
	found := map[int]captureProcess{}
	for changed := true; changed; {
		changed = false
		for pid, identity := range all {
			_, already := found[pid]
			_, knownParent := found[identity.parent]
			if !already && (identity.parent == parent || knownParent) {
				found[pid] = identity
				changed = true
			}
		}
	}
	return found
}
