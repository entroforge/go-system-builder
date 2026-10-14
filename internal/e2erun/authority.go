package e2erun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/entroforge/go-system-builder/internal/fileview"
)

const ProfilePath = "docs/control/e2e-runner.json"

// FormalRequest is the adapter's only execution entry. Runtime still owns
// stage/assignment/approval checks and the final locked CAS. A formal Git
// source and a pinned profile are prerequisites, not substitutes for those
// checks or for an independent review of the assertions in project code.
type FormalRequest struct {
	Files           *fileview.View
	ProfileRef      FileDigest
	Binding         Binding
	Mode            string
	Collection      *Receipt
	SelectedTestIDs []string
	capture         *Capture
}

// ReadFormalProfile never reads caller-supplied profile JSON or falls back to
// the working tree. A locally changed tool path cannot alter an approved run.
func ReadFormalProfile(files *fileview.View, ref FileDigest) (Profile, error) {
	if files == nil || files.Commit == "" || ref.Path != ProfilePath || len(ref.SHA256) != 64 {
		return Profile{}, fmt.Errorf("E2E requires a formal Git view and pinned %s", ProfilePath)
	}
	data, err := (formalInputs{files}).ReadFile(ref.Path)
	if err != nil {
		return Profile{}, err
	}
	if digest(data) != ref.SHA256 {
		return Profile{}, fmt.Errorf("formal E2E profile fingerprint drifted")
	}
	return DecodeProfile(data)
}

// RunFormal returns actual observations, including failed invocations. A
// changing source branch invalidates the observation but does not discard it.
// Only the Runtime producer may subsequently index the private Observation.
func RunFormal(ctx context.Context, request FormalRequest) (*Observation, error) {
	request.Files = request.Files.WithContext(ctx)
	profile, err := ReadFormalProfile(request.Files, request.ProfileRef)
	if err != nil {
		return nil, err
	}
	if request.Binding.SourceCommit != request.Files.Commit {
		return nil, fmt.Errorf("E2E binding source_commit differs from formal Git view")
	}
	if err := request.Files.Verify(); err != nil {
		return nil, err
	}
	observation, runErr := run(ctx, Request{Files: formalInputs{request.Files}, Profile: profile, Binding: request.Binding, Mode: request.Mode, Collection: request.Collection, SelectedTestIDs: request.SelectedTestIDs, capture: request.capture})
	if observation == nil {
		return nil, runErr
	}
	receipt, ok := observation.Receipt()
	if !ok {
		return nil, fmt.Errorf("adapter failed to encode its observation")
	}
	ref := request.ProfileRef
	receipt.ProfileRef = &ref
	if err := request.Files.Verify(); err != nil {
		receipt.Problems = append(receipt.Problems, err.Error())
		receipt.Complete, receipt.Outcome = false, "unknown"
		runErr = errors.Join(runErr, err)
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		return nil, errors.Join(runErr, err)
	}
	if _, err := DecodeReceipt(data); err != nil {
		return nil, errors.Join(runErr, fmt.Errorf("adapter encoded an invalid observation: %w", err))
	}
	return &Observation{data: data}, runErr
}

// InspectFormalInputs uses exactly the source policy used during invocation.
// It is suitable for the Runtime producer's locked recheck of an observation.
func InspectFormalInputs(ctx context.Context, files *fileview.View, roots []string) ([]FileDigest, string, error) {
	if files == nil || files.Commit == "" {
		return nil, "", fmt.Errorf("formal Git view is required")
	}
	return InspectInputs(ctx, formalInputs{files.WithContext(ctx)}, roots)
}

func ReadFormalOracles(files *fileview.View, modules []string) ([]Oracle, error) {
	if files == nil || files.Commit == "" {
		return nil, fmt.Errorf("formal Git view is required")
	}
	return ReadOracles(formalInputs{files}, modules)
}

// More-specific disk rules under a source root are rejected on every read;
// checking only the directory's rule would permit a disk-backed config/spec.
type formalInputs struct{ files *fileview.View }

func (f formalInputs) check(path string) error {
	source, err := f.files.Source(path)
	if err != nil {
		return err
	}
	if source != "git_tree" {
		return fmt.Errorf("E2E executable input %s must come from the formal Git tree", path)
	}
	return nil
}
func (f formalInputs) ReadFile(path string) ([]byte, error) {
	if err := f.check(path); err != nil {
		return nil, err
	}
	return f.files.ReadFile(path)
}
func (f formalInputs) ReadDir(path string) ([]os.DirEntry, error) {
	if err := f.check(path); err != nil {
		return nil, err
	}
	return f.files.ReadDir(path)
}
