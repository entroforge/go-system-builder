package checkexec

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/filelock"
)

// RunRecord is a bounded diagnostic, never a source of PASS or authorization.
type RunRecord struct {
	Version    string  `json:"version"`
	Key        string  `json:"key"`
	Binding    Binding `json:"binding"`
	PID        int     `json:"pid"`
	State      string  `json:"state"`
	StartedAt  string  `json:"started_at"`
	UpdatedAt  string  `json:"updated_at"`
	ReceiptRef string  `json:"receipt_ref,omitempty"`
	Diagnostic string  `json:"diagnostic,omitempty"`
}
type RunBusy struct {
	Ref   string
	Cause error
}

func (e *RunBusy) Error() string {
	return fmt.Sprintf("check already running or awaiting original reception; observe %s: %v", e.Ref, e.Cause)
}
func (e *RunBusy) Unwrap() error { return e.Cause }

type RunAttempt struct {
	Path    string
	Record  RunRecord
	release func()
}

// RunKey names the existing check responsibility and index without scanning or
// authenticating the checkout. The encoding is reversible and deterministic.
func RunKey(binding Binding, index int, kind string) string {
	raw, _ := json.Marshal([]any{binding.RuntimeID, binding.Generation, binding.AssignmentID,
		binding.Owner, binding.ExecutionGeneration, binding.Responsibility, index, kind})
	return "r-" + base64.RawURLEncoding.EncodeToString(raw)
}

// BeginInvocation gives each actual check invocation its own diagnostic
// attempt. A concurrent check must not be blocked merely because it shares an
// owner and index: its inputs may have changed while another check is running.
// The original responsibility remains in the record; no input digest or
// author-supplied key is needed.
func BeginInvocation(ctx context.Context, base string, binding Binding, index int, kind string) (*RunAttempt, error) {
	if err := os.MkdirAll(base, 0700); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(base, "check-")
	if err != nil {
		return nil, err
	}
	attempt, err := BeginRun(ctx, base, filepath.Base(dir), binding)
	if err != nil {
		return nil, err
	}
	attempt.Record.Key = RunKey(binding, index, kind)
	if err := writeRunRecord(attempt.Path, attempt.Record); err != nil {
		attempt.Close()
		return nil, err
	}
	return attempt, nil
}

// BeginRun serializes identical requests until the original writer has accepted
// the actual receipt. An abandoned diagnostic cannot certify an interrupted run.
func BeginRun(ctx context.Context, base, key string, binding Binding) (*RunAttempt, error) {
	if key == "" || key == "." || key == ".." || strings.ContainsAny(key, "/\\\x00") {
		return nil, fmt.Errorf("invalid check running key")
	}
	dir := filepath.Join(base, key)
	if len(key) > 220 {
		// A filesystem component limit must not turn a valid responsibility
		// into a gate. Long identities get a separate diagnostic attempt.
		if err := os.MkdirAll(base, 0700); err != nil {
			return nil, err
		}
		var err error
		dir, err = os.MkdirTemp(base, "check-")
		if err != nil {
			return nil, err
		}
	}
	path := filepath.Join(dir, "run.json")
	budget, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	release, err := filelock.Acquire(budget, filepath.Join(dir, "run.lock"))
	if err != nil {
		return nil, &RunBusy{Ref: path, Cause: err}
	}
	ok := false
	defer func() {
		if !ok {
			release()
		}
	}()
	previous, found, err := ReadRunRecord(path)
	if err != nil {
		// Keep damaged diagnostics, without accepting them as a receipt.
		if renameErr := os.Rename(path, filepath.Join(dir, "damaged-run.json")); renameErr != nil && !errors.Is(renameErr, os.ErrNotExist) {
			return nil, renameErr
		}
	} else if found && (previous.State == "running" || previous.State == "waiting") {
		previous.State = "interrupted"
		previous.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		previous.Diagnostic = "previous process released its lock without finishing; no PASS inferred"
		if err := writeRunRecord(filepath.Join(dir, "interrupted-run.json"), previous); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	record := RunRecord{Version: "1.0.0", Key: key, Binding: binding, PID: os.Getpid(), State: "waiting", StartedAt: now, UpdatedAt: now}
	if err := writeRunRecord(path, record); err != nil {
		return nil, err
	}
	ok = true
	return &RunAttempt{Path: path, Record: record, release: release}, nil
}

func (r *RunAttempt) Start() error {
	r.Record.State = "running"
	r.Record.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return writeRunRecord(r.Path, r.Record)
}
func (r *RunAttempt) Finish(receipt string, runErr error) error {
	r.Record.State = "finished"
	r.Record.ReceiptRef = receipt
	r.Record.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if runErr != nil {
		r.Record.Diagnostic = runErr.Error()
		if len(r.Record.Diagnostic) > 2048 {
			r.Record.Diagnostic = r.Record.Diagnostic[:2048]
		}
	}
	return writeRunRecord(r.Path, r.Record)
}
func (r *RunAttempt) Close() { r.release() }

func ReadRunRecord(path string) (RunRecord, bool, error) {
	var r RunRecord
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return r, false, nil
	}
	if err != nil {
		return r, false, err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return r, false, fmt.Errorf("running diagnostic is not a bounded regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return r, false, err
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return r, false, err
	}
	if r.Version != "1.0.0" || r.Key == "" || (r.State != "waiting" && r.State != "running" && r.State != "finished" && r.State != "interrupted") {
		return r, false, fmt.Errorf("invalid running diagnostic")
	}
	return r, true, nil
}

func writeRunRecord(path string, r RunRecord) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".run-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
