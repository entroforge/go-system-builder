package checkexec

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/entroforge/go-system-builder/internal/schema"
)

// ToolReceipt is captured from the runner's own bytes, then committed by the
// original domain writer. It is not a gate, conclusion or a user import format.
type ToolReceipt struct {
	Path     string  `json:"path"`
	Checkout string  `json:"checkout"`
	Command  string  `json:"command"`
	Binding  Binding `json:"binding"`
}

type captureKey struct{}

// WithReceiptCapture records the original runner's result for its domain writer.
func WithReceiptCapture(ctx context.Context, capture func(ToolReceipt)) context.Context {
	return context.WithValue(ctx, captureKey{}, capture)
}

func CaptureReceipt(ctx context.Context, path, checkout string, data []byte) error {
	capture, _ := ctx.Value(captureKey{}).(func(ToolReceipt))
	if capture == nil {
		return nil
	}
	var r receiptFacts
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	if r.Version != 2 || r.Observation == nil {
		return nil
	}
	binding, ok := CurrentBinding(ctx)
	p, profiled := CurrentProfile(ctx)
	if !ok || !profiled || binding != r.Binding || p.Command != r.Command {
		return fmt.Errorf("tool receipt differs from actual invocation")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if r.Output != filepath.Join(filepath.Dir(path), "output.log") {
		return fmt.Errorf("tool receipt log differs from actual local output path")
	}
	checkout, err = filepath.Abs(checkout)
	if err != nil {
		return err
	}
	checkout, err = filepath.EvalSymlinks(checkout)
	if err != nil {
		return err
	}
	capture(ToolReceipt{Path: path, Checkout: checkout, Command: r.Command, Binding: binding})
	return nil
}

type receiptFacts struct {
	Version     int          `json:"version"`
	Command     string       `json:"command"`
	Status      string       `json:"status"`
	Output      string       `json:"output"`
	Binding     Binding      `json:"check_binding"`
	Observation *Observation `json:"profile_observation"`
}

type VerifiedReceipt struct {
	Reference   ToolReceipt
	Observation Observation
	Passed      bool
}

// VerifyToolReceipt reads the original receipt associated with the actual
// responsibility. It does not certify historical bytes against a digest.
func VerifyToolReceipt(evidenceRoot string, ref ToolReceipt) (VerifiedReceipt, error) {
	out := VerifiedReceipt{Reference: ref}
	path, err := containedRegular(evidenceRoot, ref.Path)
	if err != nil {
		return out, err
	}
	f, err := os.Open(path)
	if err != nil {
		return out, err
	}
	data, readErr := io.ReadAll(io.LimitReader(f, (4<<20)+1))
	f.Close()
	if readErr != nil || len(data) > 4<<20 {
		return out, fmt.Errorf("tool receipt is not bounded readable JSON")
	}
	var r receiptFacts
	if err := json.Unmarshal(data, &r); err != nil {
		return out, err
	}
	if r.Version != 2 || r.Observation == nil || r.Binding != ref.Binding || r.Command != ref.Command {
		return out, fmt.Errorf("tool receipt differs from original registered facts")
	}
	observationBytes, _ := json.Marshal(r.Observation)
	if err := schema.NewEmbeddedValidator().ValidateBytes("check-observation.schema.json", observationBytes); err != nil {
		return out, err
	}
	out.Observation = *r.Observation
	out.Passed = r.Status == "pass"
	return out, nil
}

func containedRegular(root, path string) (string, error) {
	base, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	actual, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, actual)
	if err != nil || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("tool receipt or log escapes evidence directory")
	}
	info, err := os.Stat(actual)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("tool receipt or log must be a regular file")
	}
	return actual, nil
}
