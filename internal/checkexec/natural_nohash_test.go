package checkexec

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRunKeyUsesOriginalResponsibilityWithoutTreeDigest(t *testing.T) {
	b := Binding{RuntimeID: "loop-REQ-001", Generation: 1, AssignmentID: "assignment-1", Owner: "builder", ExecutionGeneration: 1, Responsibility: "owned_check"}
	key := RunKey(b, 0, "workspace")
	if key != RunKey(b, 0, "workspace") || key == RunKey(b, 1, "workspace") {
		t.Fatal("same responsibility must retain identity and separate real check indices")
	}
	other := b
	other.Owner = "other"
	if key == RunKey(other, 0, "workspace") {
		t.Fatal("different owner reused the original check identity")
	}
	base := t.TempDir()
	first, err := BeginRun(context.Background(), base, key, b)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	first.Close()
	retry, err := BeginRun(context.Background(), base, key, b)
	if err != nil {
		t.Fatal(err)
	}
	defer retry.Close()
	previous, found, err := ReadRunRecord(filepath.Join(filepath.Dir(retry.Path), "interrupted-run.json"))
	if err != nil || !found || previous.State != "interrupted" || previous.ReceiptRef != "" {
		t.Fatalf("interrupted work must not become a receipt: %#v %v", previous, err)
	}
	// A legitimate long responsibility must not be rejected by filesystem
	// component limits. Its diagnostic need not establish a prior outcome.
	long := b
	long.Owner = strings.Repeat("owner", 100)
	attempt, err := BeginRun(context.Background(), base, RunKey(long, 0, "workspace"), long)
	if err != nil {
		t.Fatal(err)
	}
	attempt.Close()
}

func TestConcurrentOriginalCheckInvocationsDoNotBlockOrShareReceipt(t *testing.T) {
	base := t.TempDir()
	b := Binding{RuntimeID: "loop-REQ-001", Generation: 1, AssignmentID: "assignment-1", Owner: "builder", ExecutionGeneration: 1, Responsibility: "owned_check"}
	first, err := BeginInvocation(context.Background(), base, b, 0, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if err := first.Start(); err != nil {
		t.Fatal(err)
	}
	second, err := BeginInvocation(context.Background(), base, b, 0, "workspace")
	if err != nil {
		t.Fatalf("same-responsibility invocation was blocked: %v", err)
	}
	defer second.Close()
	if err := second.Start(); err != nil {
		t.Fatal(err)
	}
	if first.Path == second.Path || first.Record.Key != second.Record.Key {
		t.Fatal("invocation or responsibility identity was lost")
	}
	if err := first.Finish("/original/receipt.json", nil); err != nil {
		t.Fatal(err)
	}
	ongoing, found, err := ReadRunRecord(second.Path)
	if err != nil || !found || ongoing.State != "running" || ongoing.ReceiptRef != "" {
		t.Fatalf("one invocation supplied a result to another: %#v %v", ongoing, err)
	}
}

func TestOriginalCheckReceiptIgnoresHistoricalMachineDigests(t *testing.T) {
	for name, old := range map[string]any{"null": nil, "text": "historical", "number": 42, "object": map[string]any{"old": true}} {
		t.Run(name, func(t *testing.T) {
			root, checkout := t.TempDir(), t.TempDir()
			log := filepath.Join(root, "output.log")
			if err := os.WriteFile(log, []byte("actual check output"), 0600); err != nil {
				t.Fatal(err)
			}
			binding := Binding{RuntimeID: "runtime", Generation: 1, AssignmentID: "assignment", Owner: "builder", ExecutionGeneration: 1, Responsibility: "owned_check"}
			p := testProfile()
			obs, err := ObserveProfile(context.Background(), checkout, p)
			if err != nil {
				t.Fatal(err)
			}
			ob, _ := json.Marshal(obs)
			var observation map[string]any
			if err := json.Unmarshal(ob, &observation); err != nil {
				t.Fatal(err)
			}
			observation["profile_digest"], observation["subject_digest"] = old, old
			row := map[string]any{"version": 2, "command": p.Command, "status": "pass", "output": log, "check_binding": binding, "profile_observation": observation, "log_sha256": old, "receipt_integrity": old}
			data, _ := json.Marshal(row)
			path := filepath.Join(root, "receipt.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			var ref ToolReceipt
			ctx := WithReceiptCapture(WithBinding(WithProfile(context.Background(), p), binding), func(r ToolReceipt) { ref = r })
			if err := CaptureReceipt(ctx, path, checkout, data); err != nil {
				t.Fatal(err)
			}
			result, err := VerifyToolReceipt(root, ref)
			if err != nil || !result.Passed {
				t.Fatalf("original receipt rejected: %#v %v", result, err)
			}
			stored, err := os.ReadFile(path)
			if err != nil || !reflect.DeepEqual(data, stored) {
				t.Fatal("original receipt changed")
			}
			if got := DecideReuse(root, ReuseRequest{Profile: p, Binding: binding, Checkout: checkout}, []ToolReceipt{ref}); got.Action != "run" || got.WouldReuse {
				t.Fatal("unproven equality was reused", got)
			}
			wrong := ref
			wrong.Binding.Owner = "another"
			if _, err := VerifyToolReceipt(root, wrong); err == nil {
				t.Fatal("different owner accepted")
			}
		})
	}
}
