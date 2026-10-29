package checkexec

import (
	"context"
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

// BeginRun serializes identical requests until the original writer has accepted
// the actual receipt. An abandoned diagnostic cannot certify an interrupted run.
func BeginRun(ctx context.Context, base, key string, binding Binding) (*RunAttempt, error) {
	if len(key) != 64 || strings.Trim(key, "0123456789abcdef") != "" {
		return nil, fmt.Errorf("invalid check running key")
	}
	dir := filepath.Join(base, key)
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
	if r.Version != "1.0.0" || len(r.Key) != 64 || (r.State != "waiting" && r.State != "running" && r.State != "finished" && r.State != "interrupted") {
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
