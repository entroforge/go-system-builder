package checkexec

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The real runner's receipt is associated with its original responsibility;
// neither observing nor persisting the invocation needs a content digest.
func TestOriginalReceiptWithoutDigests(t *testing.T) {
	root := t.TempDir()
	p := Profile{ID: "check", Version: "1", Command: "true", Environment: []EnvFact{}}
	observed, err := ObserveProfile(context.Background(), root, p)
	if err != nil {
		t.Fatal(err)
	}
	binding := Binding{RuntimeID: "r", Generation: 1, AssignmentID: "a", Owner: "worker", ExecutionGeneration: 1, Responsibility: "owned_check"}
	output := filepath.Join(root, "output.log")
	if err := os.WriteFile(output, []byte("actual output"), 0600); err != nil {
		t.Fatal(err)
	}
	row := map[string]any{"version": 2, "command": "true", "status": "pass", "output": output, "check_binding": binding, "profile_observation": observed}
	data, err := SealReceipt(row)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sha256") || strings.Contains(string(data), "digest") || strings.Contains(string(data), "receipt_integrity") {
		t.Fatalf("new receipt contains a digest: %s", data)
	}
	receiptPath := filepath.Join(root, "receipt.json")
	if err := os.WriteFile(receiptPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	var got ToolReceipt
	ctx := WithReceiptCapture(WithProfile(WithBinding(context.Background(), binding), p), func(r ToolReceipt) { got = r })
	if err := CaptureReceipt(ctx, receiptPath, root, data); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "sha256") || strings.Contains(string(encoded), "digest") {
		t.Fatalf("new reference contains a digest: %s", encoded)
	}
	v, err := VerifyToolReceipt(root, got)
	if err != nil || !v.Passed {
		t.Fatalf("original result: %#v, %v", v, err)
	}
}
