// Package mainreceipt reads recorded S6 Main integration facts for their
// existing responsibility. It does not authenticate file contents or Git state.
package mainreceipt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const checkpointLimit = 16 << 20

// ErrNotVerified identifies an exactly bound, preserved integration attempt which has not
// completed Main verification. It carries no successful contribution proof.
// A caller may omit it from historical verified contributions; a current
// Assignment still requires Verify to succeed.
var ErrNotVerified = errors.New("preserved Main merge has not completed verification")

// Input contains data already resolved by the original authority layer. In
// particular, Accepted must be the result of LoadAccepted, LoadContribution or
// LoadSupersededAccepted, ExecutionGeneration must come from the caller's
// current or historical Runtime execution registry, AuthorizedMainRoots must
// come from caller-validated Runtime relocation state.
//
// These fields select evidence to inspect; they do not grant integration or
// acceptance authority.
type Input struct {
	AuthorityRoot        string
	Record               assignmentrecord.Record
	Accepted             assignmentresult.AcceptedResult
	ResultRef            string
	ExecutionGeneration  int
	AuthorizedMainRoots  []string
	ExpectedTargetBranch string
}

// ChangedFile describes a file path from the original MainFacts Git diff.
// Status is the actual change reported by the integration.
type ChangedFile struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

// VerifiedContribution is produced only by Verify. It exposes a read-only
// copy of the recorded responsibility and integration facts. It does not mean that the current PLAN/activation or
// Runtime registration is authorized, nor that a professional Result passed.
type VerifiedContribution struct {
	recordRef      assignmentrecord.RecordReference
	resultBinding  assignmentresult.ResultBinding
	checkpointPath string
	facts          assignmentresult.MainFacts
	changedFiles   []ChangedFile
}

func (v VerifiedContribution) RecordReference() assignmentrecord.RecordReference { return v.recordRef }
func (v VerifiedContribution) ResultBinding() assignmentresult.ResultBinding     { return v.resultBinding }
func (v VerifiedContribution) CheckpointPath() string                            { return v.checkpointPath }

func (v VerifiedContribution) MainFacts() assignmentresult.MainFacts {
	out := v.facts
	out.ChangedPaths = append([]string(nil), v.facts.ChangedPaths...)
	out.Checks = append([]assignmentresult.MainCheckFact(nil), v.facts.Checks...)
	return out
}

func (v VerifiedContribution) ChangedFiles() []ChangedFile {
	return append([]ChangedFile(nil), v.changedFiles...)
}

type checkpoint struct {
	AssignmentRecord   *assignmentrecord.RecordReference `json:"assignment_record,omitempty"`
	TaskID             string                            `json:"task_id"`
	AssignmentID       string                            `json:"assignment_id"`
	Generation         int                               `json:"baseline_generation"`
	State              string                            `json:"state"`
	VerifiedAt         string                            `json:"verified_at"`
	ReportPath         string                            `json:"completion_report_path"`
	TargetBranch       string                            `json:"target_branch"`
	ChangedPaths       []string                          `json:"changed_paths"`
	PreviousMainRoots  []string                          `json:"previous_main_roots,omitempty"`
	CheckReceipts      []string                          `json:"check_receipts,omitempty"`
	ToolCheckReceipts  []checkexec.ToolReceipt           `json:"tool_check_receipts,omitempty"`
	CanonicalMainFacts *assignmentresult.MainFacts       `json:"canonical_main_facts"`
	Revision           int64                             `json:"revision"`
}

// Verify reads the recorded Main integration facts for the original
// responsibility. Callers resolve the current Runtime and accepted Result.
func Verify(ctx context.Context, in Input) (VerifiedContribution, error) {
	var out VerifiedContribution
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if err := assignmentrecord.ValidateRecord(in.Record); err != nil {
		return out, fmt.Errorf("validate Assignment Record: %w", err)
	}
	recordRef := in.Record.Reference()
	if in.Accepted.AssignmentRecord != recordRef || in.Accepted.Conclusion != "submitted" || !validRelativeRef(in.ResultRef) || in.ExecutionGeneration < 0 {
		return out, fmt.Errorf("input Result or Record binding is incomplete")
	}
	root, err := filepath.Abs(in.AuthorityRoot)
	if err != nil || strings.TrimSpace(in.AuthorityRoot) == "" {
		return out, fmt.Errorf("Runtime authority root is invalid")
	}
	if err := requireAuthorizedRoots(root, in.AuthorizedMainRoots); err != nil {
		return out, err
	}
	checkpointRel := filepath.ToSlash(assignmentrecord.IntegrationCheckpointPath("", recordRef))
	checkpointBytes, err := workpacket.ReadFile(root, checkpointRel, checkpointLimit)
	if err != nil {
		return out, fmt.Errorf("read Record-specific Main checkpoint: %w", err)
	}
	var cp checkpoint
	decoder := json.NewDecoder(strings.NewReader(string(checkpointBytes)))
	if err := decoder.Decode(&cp); err != nil {
		return out, fmt.Errorf("decode Main checkpoint: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return out, fmt.Errorf("Main checkpoint must contain exactly one JSON object")
	}
	if cp.AssignmentRecord != nil && *cp.AssignmentRecord != recordRef {
		return out, fmt.Errorf("checkpoint belongs to another original Record")
	}
	if cp.AssignmentID != in.Record.AssignmentID || cp.Generation != in.Record.BaselineGeneration || (cp.TaskID != "" && cp.TaskID != in.Record.Task.ID) {
		return out, fmt.Errorf("checkpoint differs from the original responsibility")
	}
	switch cp.State {
	case "verified", "acknowledged", "cleanup_pending", "complete":
	default:
		return out, ErrNotVerified
	}
	if cp.CanonicalMainFacts == nil {
		return out, fmt.Errorf("verified checkpoint has no recorded Main facts")
	}
	facts := cloneMainFacts(*cp.CanonicalMainFacts)
	if cp.ReportPath != "" && cp.ReportPath != in.ResultRef {
		return out, fmt.Errorf("checkpoint names a different original Result")
	}
	if in.ExpectedTargetBranch != "" && facts.TargetBranch != in.ExpectedTargetBranch {
		return out, fmt.Errorf("Main facts name a different target branch")
	}
	if facts.ExecutionGeneration != in.ExecutionGeneration {
		return out, fmt.Errorf("Main facts name a different original execution")
	}
	if err := validateAuthorizedMainRoots(root, in.AuthorizedMainRoots, cp.PreviousMainRoots, facts.MainRoot); err != nil {
		return out, err
	}
	if err := assignmentresult.ValidateMainFacts(in.Record, in.Accepted, in.ResultRef, facts); err != nil {
		return out, fmt.Errorf("validate original Main facts: %w", err)
	}
	// Preserve the actual results without copying legacy authentication fields
	// into a new projection. A recorded check does not prove later bytes.
	files := make([]ChangedFile, 0, len(facts.ChangedPaths))
	for _, file := range facts.ChangedPaths {
		files = append(files, ChangedFile{Path: file})
	}
	return VerifiedContribution{recordRef: recordRef, resultBinding: facts.Binding,
		checkpointPath: filepath.Join(root, filepath.FromSlash(checkpointRel)), facts: facts, changedFiles: files}, nil
}

func cloneMainFacts(facts assignmentresult.MainFacts) assignmentresult.MainFacts {
	facts.ChangedPaths = append([]string(nil), facts.ChangedPaths...)
	facts.Checks = append([]assignmentresult.MainCheckFact(nil), facts.Checks...)
	return facts
}

func requireAuthorizedRoots(authorityRoot string, roots []string) error {
	if len(roots) == 0 {
		return fmt.Errorf("caller must provide Runtime-validated Main root coordinates")
	}
	allowed := map[string]bool{}
	for _, item := range append(append([]string(nil), roots...), authorityRoot) {
		if strings.TrimSpace(item) == "" {
			return fmt.Errorf("caller supplied an empty authorized Main root")
		}
		if !filepath.IsAbs(item) {
			return fmt.Errorf("caller-authorized Main roots must be absolute")
		}
		abs, err := filepath.Abs(item)
		if err != nil {
			return fmt.Errorf("resolve authorized Main root: %w", err)
		}
		allowed[filepath.Clean(abs)] = true
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			allowed[filepath.Clean(resolved)] = true
		}
	}
	if !allowed[filepath.Clean(authorityRoot)] {
		return fmt.Errorf("Runtime authority root is not an authorized Main root")
	}
	return nil
}

func validateAuthorizedMainRoots(authorityRoot string, authorized, previous []string, factsRoot string) error {
	allowed := map[string]bool{}
	for _, item := range append(append([]string(nil), authorized...), authorityRoot) {
		abs, err := filepath.Abs(item)
		if err != nil {
			return err
		}
		allowed[filepath.Clean(abs)] = true
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			allowed[filepath.Clean(resolved)] = true
		}
	}
	if !isAuthorizedRoot(factsRoot, allowed) {
		return fmt.Errorf("MainFacts root is not authorized by caller-validated Main roots")
	}
	seenPrevious := map[string]bool{}
	for _, old := range previous {
		if !filepath.IsAbs(old) || !isAuthorizedRoot(old, allowed) || seenPrevious[filepath.Clean(old)] {
			return fmt.Errorf("checkpoint PreviousMainRoot is not caller-authorized")
		}
		seenPrevious[filepath.Clean(old)] = true
	}
	return nil
}

func isAuthorizedRoot(value string, allowed map[string]bool) bool {
	if !filepath.IsAbs(value) || filepath.Clean(value) != value {
		return false
	}
	if allowed[filepath.Clean(value)] {
		return true
	}
	if resolved, err := filepath.EvalSymlinks(value); err == nil && allowed[filepath.Clean(resolved)] {
		return true
	}
	return false
}

func validRelativeRef(ref string) bool {
	if ref == "" || filepath.IsAbs(ref) || strings.ContainsAny(ref, "\\\x00\r\n") {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(ref)))
	return clean == ref && ref != "." && ref != ".." && !strings.HasPrefix(ref, "../")
}
