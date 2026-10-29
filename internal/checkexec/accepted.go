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
	Path          string  `json:"path"`
	SHA256        string  `json:"sha256"`
	LogSHA256     string  `json:"log_sha256"`
	Checkout      string  `json:"checkout"`
	Command       string  `json:"command"`
	ProfileDigest string  `json:"profile_digest"`
	Binding       Binding `json:"binding"`
}

type captureKey struct{}

// WithReceiptCapture is process-local: no file or CLI argument can supply the
// captured digest. The caller must still commit it through its original writer.
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
	if !ok || !profiled || binding != r.Binding || p.Digest() != r.Observation.ProfileDigest || p.Command != r.Command {
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
	capture(ToolReceipt{Path: path, SHA256: Hash(data), LogSHA256: r.LogSHA256, Checkout: checkout, Command: r.Command, ProfileDigest: p.Digest(), Binding: binding})
	return nil
}

type receiptFacts struct {
	Version     int          `json:"version"`
	Command     string       `json:"command"`
	Status      string       `json:"status"`
	Output      string       `json:"output"`
	LogSHA256   string       `json:"log_sha256"`
	Integrity   string       `json:"receipt_integrity"`
	Binding     Binding      `json:"check_binding"`
	Observation *Observation `json:"profile_observation"`
}

type VerifiedReceipt struct {
	Reference   ToolReceipt
	Observation Observation
	Passed      bool
}

// VerifyToolReceipt requires the original domain's captured reference. A
// self-consistent seal alone cannot satisfy this function's provenance check.
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
	if Hash(data) != ref.SHA256 {
		return out, fmt.Errorf("tool receipt differs from original captured digest")
	}
	var r receiptFacts
	if err := json.Unmarshal(data, &r); err != nil {
		return out, err
	}
	if r.Version != 2 || r.Observation == nil || r.Binding != ref.Binding || r.Command != ref.Command || r.Observation.ProfileDigest != ref.ProfileDigest || r.LogSHA256 != ref.LogSHA256 {
		return out, fmt.Errorf("tool receipt differs from original registered facts")
	}
	observationBytes, _ := json.Marshal(r.Observation)
	if err := schema.NewEmbeddedValidator().ValidateBytes("check-observation.schema.json", observationBytes); err != nil {
		return out, err
	}
	if r.Observation.Permissions != nil && r.Observation.PermissionDigest != r.Observation.Permissions.Digest() {
		return out, fmt.Errorf("tool receipt permissions digest differs")
	}
	integrity, err := ReceiptIntegrity(data)
	if err != nil || integrity != r.Integrity {
		return out, fmt.Errorf("tool receipt integrity differs")
	}
	if !filepath.IsAbs(r.Output) || filepath.Base(r.Output) != "output.log" {
		return out, fmt.Errorf("tool receipt has no original output log")
	}
	// Explicit domain relocation changes only evidence coordinates. Signed
	// receipt bytes retain the original absolute output path and checkout; the
	// same captured log digest is checked beside the relocated receipt.
	log, err := containedRegular(filepath.Dir(path), filepath.Join(filepath.Dir(path), "output.log"))
	if err != nil {
		return out, err
	}
	digest, err := FileDigest(log)
	if err != nil || digest != ref.LogSHA256 {
		return out, fmt.Errorf("tool receipt log differs from original captured digest")
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
