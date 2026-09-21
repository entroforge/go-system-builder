package integration

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrCompletionReportChanged means that the Builder Result observed by the
// caller is no longer the bytes that were inspected. The caller must inspect
// and verify the new Result before it can be acknowledged or cleaned up.
var ErrCompletionReportChanged = errors.New("completion report changed after inspection")

type completionReportIdentity struct {
	Path   string
	SHA256 string
	Bound  bool
}

// resolveCompletionReportBinding validates the report supplied by Inspect (or
// recovers the report binding from an existing checkpoint when the controller
// intentionally skipped a post-merge inspection). changed is true only when
// a previously verified checkpoint would otherwise be reused for different
// report bytes.
func resolveCompletionReportBinding(root string, inspection Inspection, current Checkpoint, found bool) (completionReportIdentity, bool, error) {
	inPath := strings.TrimSpace(inspection.CompletionReportPath)
	inSHA := strings.TrimSpace(inspection.CompletionReportSHA256)
	if (inPath == "") != (inSHA == "") {
		return completionReportIdentity{}, false, fmt.Errorf("completion report path and sha256 must be provided together")
	}

	var incoming completionReportIdentity
	if inPath != "" {
		actual, err := readCompletionReportBinding(root, inPath)
		if err != nil {
			return completionReportIdentity{}, false, err
		}
		if actual.SHA256 != inSHA {
			return completionReportIdentity{}, false, fmt.Errorf("%w: inspected %s has sha256 %s, current file is %s", ErrCompletionReportChanged, actual.Path, inSHA, actual.SHA256)
		}
		incoming = actual
	}

	checkpointPath := strings.TrimSpace(current.CompletionReportPath)
	checkpointSHA := strings.TrimSpace(current.CompletionReportSHA256)
	if (checkpointPath == "") != (checkpointSHA == "") {
		// A pre-binding checkpoint with only one half of the identity cannot
		// safely authorize ack/cleanup. If it at least has a path, read it and
		// treat the result as a migration re-verification; a hash without a
		// path has no file to verify and must fail closed.
		if checkpointPath == "" {
			return completionReportIdentity{}, false, fmt.Errorf("checkpoint completion report sha256 has no path")
		}
		checkpointSHA = ""
	}

	var checkpointBinding completionReportIdentity
	if checkpointPath != "" {
		actual, err := readCompletionReportBinding(root, checkpointPath)
		if err != nil {
			return completionReportIdentity{}, false, err
		}
		checkpointBinding = actual
	}

	if incoming.Bound {
		changed := false
		if found && checkpointBinding.Bound {
			changed = checkpointBinding.Path != incoming.Path || checkpointSHA != incoming.SHA256
		} else if found && checkpointPath == "" {
			// A verified/complete legacy checkpoint has no content identity. A
			// newly inspected report must establish one through verification.
			changed = true
		}
		return incoming, changed, nil
	}

	if checkpointBinding.Bound {
		// Controller ack/cleanup recovery may provide an Inspection with no
		// report fields. Re-read the checkpoint's path and bind to the bytes
		// actually present; a changed file therefore enters re-verification.
		changed := checkpointSHA != checkpointBinding.SHA256 || checkpointPath != checkpointBinding.Path
		return checkpointBinding, changed, nil
	}

	// No report was inspected and no prior binding exists. This preserves the
	// legacy/SkipCompletionCheck behavior; a planned quality gate will reject
	// such a checkpoint when it requires a current report identity.
	return completionReportIdentity{}, false, nil
}

func readCompletionReportBinding(root, reportPath string) (completionReportIdentity, error) {
	rel, err := completionReportRootPath(root, reportPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return completionReportIdentity{}, fmt.Errorf("%w: %s: %v", ErrMissingCompletion, reportPath, err)
		}
		return completionReportIdentity{}, fmt.Errorf("completion report path: %w", err)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return completionReportIdentity{}, fmt.Errorf("%w: %s: %v", ErrMissingCompletion, rel, err)
	}
	return completionReportIdentity{Path: rel, SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), Bound: true}, nil
}

// completionReportRootPath canonicalizes a report path while retaining its
// lexical repository-relative spelling for task.completion_report_ref. Both
// lexical and resolved containment checks prevent an absolute or symlinked
// path from becoming a checkpoint authority outside the repository.
func completionReportRootPath(root, reportPath string) (string, error) {
	if strings.TrimSpace(reportPath) == "" {
		return "", fmt.Errorf("path is empty")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	reportInput := reportPath
	if !filepath.IsAbs(reportInput) {
		reportInput = filepath.Join(rootAbs, reportInput)
	}
	reportAbs, err := filepath.Abs(reportInput)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, reportAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path %q is outside %q", reportPath, root)
	}
	resolvedRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	resolvedReport, err := filepath.EvalSymlinks(reportAbs)
	if err != nil {
		return "", fmt.Errorf("resolve report: %w", err)
	}
	resolvedRel, err := filepath.Rel(resolvedRoot, resolvedReport)
	if err != nil || resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)) || filepath.IsAbs(resolvedRel) {
		return "", fmt.Errorf("resolved path %q is outside %q", reportPath, root)
	}
	return filepath.ToSlash(filepath.Clean(rel)), nil
}

func reportBindingMatches(a, b completionReportIdentity) bool {
	return a.Bound && b.Bound && a.Path == b.Path && a.SHA256 == b.SHA256
}

func checkpointHasVerifiedStage(cp Checkpoint) bool {
	if isVerifiedOrLater(cp.State) {
		return true
	}
	return cp.State == StatePreserved && isVerifiedOrLater(cp.ResumeState)
}

func isVerifiedOrLater(state string) bool {
	switch state {
	case StateVerified, StateAcknowledged, StateCleanupPending, StateComplete:
		return true
	default:
		return false
	}
}

// RefreshCompletionBinding reads the current canonical Result during recovery.
// Integrate compares it with the durable binding and reruns checks on changes.
func RefreshCompletionBinding(root, runtimeID, explicitRef string, in Inspection) (Inspection, error) {
	path := explicitRef
	if path == "" {
		path = in.CompletionReportPath
	}
	if path == "" {
		path = completionReportPath(root, in.AssignmentID, runtimeID, "")
	}
	binding, err := readCompletionReportBinding(root, path)
	if err != nil {
		return in, err
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(binding.Path)))
	if err != nil {
		return in, err
	}
	var envelope map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		return in, err
	}
	if kind, _ := envelope["message_type"].(string); kind != "" && kind != "completion_report" {
		return in, fmt.Errorf("invalid completion report type %q", kind)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != binding.SHA256 {
		return in, ErrCompletionReportChanged
	}
	in.CompletionReportPath, in.CompletionReportSHA256 = binding.Path, binding.SHA256
	return in, nil
}
