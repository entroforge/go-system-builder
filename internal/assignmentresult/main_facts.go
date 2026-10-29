package assignmentresult

import (
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
)

// ResultBinding names original accepted evidence, never a caller's verdict.
type ResultBinding struct {
	AssignmentRecord       assignmentrecord.RecordReference `json:"assignment_record"`
	AssignmentRecordSHA256 string                           `json:"assignment_record_sha256"`
	ResultPath             string                           `json:"result_path"`
	ResultSHA256           string                           `json:"result_sha256"`
	ResultRevision         int                              `json:"result_revision"`
	AnswerDigest           string                           `json:"answer_digest"`
}

type MainCheckFact struct {
	CheckIndex    int    `json:"check_index"`
	Command       string `json:"command"`
	Status        string `json:"status"`
	ReceiptRef    string `json:"receipt_ref"`
	ReceiptSHA256 string `json:"receipt_sha256"`
	LogRef        string `json:"log_ref"`
	LogSHA256     string `json:"log_sha256"`
	TestedHead    string `json:"tested_head"`
	ProfileDigest string `json:"profile_digest,omitempty"`
}

// MainFacts is the wire projection persisted by the original Integrator's
// checkpoint CAS. Its validation proves machine coverage and byte bindings;
// it cannot decide a professional assertion or independent S7 verdict.
type MainFacts struct {
	Binding             ResultBinding   `json:"binding"`
	MainRoot            string          `json:"main_root"`
	SourceHead          string          `json:"source_head"`
	MergeBase           string          `json:"merge_base"`
	TestedHead          string          `json:"tested_head"`
	TargetBranch        string          `json:"target_branch"`
	MergeCommit         string          `json:"merge_commit"`
	ExecutionGeneration int             `json:"execution_generation"`
	ChangedPaths        []string        `json:"changed_paths"`
	ChangedPathsSHA256  string          `json:"changed_paths_sha256"`
	Checks              []MainCheckFact `json:"checks"`
}

// ChangedPathsDigest preserves literal Git paths (including whitespace).
// The original Git producer must sort its full set and disable rename folding.
func ChangedPathsDigest(paths []string) (string, error) {
	if paths == nil {
		return "", fmt.Errorf("actual changed paths must be a JSON array")
	}
	for i, file := range paths {
		if file == "" || file == "." || file == ".." || strings.HasPrefix(file, "/") || strings.HasPrefix(file, "../") || path.Clean(file) != file || strings.ContainsAny(file, "\\\x00") || i > 0 && paths[i-1] >= file {
			return "", fmt.Errorf("actual changed paths must be safe, sorted and unique")
		}
	}
	data, err := json.Marshal(paths)
	if err != nil {
		return "", err
	}
	return digestBytes(data), nil
}

// ValidateMainFacts checks full original RequiredCheck coverage, actual
// receipt/log bytes and the tested head. read accepts the Integrator's
// persisted physical receipt refs; the caller must enforce their authority
// directory and the original checkpoint state/reachability separately.
func ValidateMainFacts(record assignmentrecord.Record, result AcceptedResult, resultRef, resultSHA string, facts MainFacts, read func(string) ([]byte, error)) error {
	sha, err := record.Digest()
	if err != nil {
		return err
	}
	expected := ResultBinding{AssignmentRecord: record.Reference(), AssignmentRecordSHA256: sha, ResultPath: resultRef, ResultSHA256: resultSHA, ResultRevision: result.ResultRevision, AnswerDigest: result.AnswerDigest}
	if facts.Binding != expected || result.AssignmentRecord != record.Reference() || result.AssignmentRecordSHA256 != sha || facts.MainRoot == "" || facts.SourceHead == "" || facts.MergeBase == "" || facts.TestedHead == "" || facts.TargetBranch == "" || facts.MergeCommit == "" || read == nil {
		return fmt.Errorf("Main facts do not bind the original Record, Result and tested Git coordinates")
	}
	changedSHA, err := ChangedPathsDigest(facts.ChangedPaths)
	if err != nil || changedSHA != facts.ChangedPathsSHA256 {
		return fmt.Errorf("Main actual diff digest changed")
	}
	for _, file := range facts.ChangedPaths {
		if !assignmentrecord.AllowsPath(record, file) {
			return fmt.Errorf("Main actual diff escapes canonical scope: %s", file)
		}
	}
	required := map[int]string{}
	for index, command := range record.RequiredChecks {
		if !strings.HasPrefix(command, "locked:") {
			required[index] = command
		}
	}
	if len(facts.Checks) != len(required) {
		return fmt.Errorf("Main facts lack full original RequiredCheck coverage")
	}
	seen := map[int]bool{}
	for _, fact := range facts.Checks {
		command, exists := required[fact.CheckIndex]
		if !exists || seen[fact.CheckIndex] || command != fact.Command || fact.Status != "pass" || fact.TestedHead != facts.TestedHead || fact.ReceiptRef == "" || fact.LogRef == "" {
			return fmt.Errorf("Main check differs from its original required responsibility")
		}
		seen[fact.CheckIndex] = true
		body, err := read(fact.ReceiptRef)
		if err != nil || digestBytes(body) != fact.ReceiptSHA256 {
			return fmt.Errorf("Main check receipt changed: %s", fact.Command)
		}
		var receipt struct {
			Command    string            `json:"command"`
			CWD        string            `json:"cwd"`
			Head       string            `json:"head"`
			HeadBefore string            `json:"head_before"`
			HeadAfter  string            `json:"head_after"`
			Status     string            `json:"status"`
			ExitCode   *int              `json:"exit_code"`
			Output     string            `json:"output"`
			Binding    checkexec.Binding `json:"check_binding"`
		}
		if err := json.Unmarshal(body, &receipt); err != nil || receipt.Command != fact.Command || receipt.Status != "pass" || receipt.ExitCode == nil || *receipt.ExitCode != 0 || receipt.Head != facts.TestedHead || receipt.HeadBefore != facts.TestedHead || receipt.HeadAfter != facts.TestedHead || filepath.Clean(receipt.CWD) != filepath.Clean(facts.MainRoot) || normalizedPhysicalRef(facts.MainRoot, receipt.Output) != normalizedPhysicalRef(facts.MainRoot, fact.LogRef) {
			return fmt.Errorf("Main check receipt does not prove the original tested command/head")
		}
		if receipt.Binding.RuntimeID != record.RuntimeID || receipt.Binding.Generation != record.BaselineGeneration || receipt.Binding.AssignmentID != record.AssignmentID || receipt.Binding.Owner != "Main" || receipt.Binding.Responsibility != "main_required_integration" || receipt.Binding.ExecutionGeneration != facts.ExecutionGeneration {
			return fmt.Errorf("Main check receipt has no original Main responsibility binding")
		}
		log, err := read(fact.LogRef)
		if err != nil || digestBytes(log) != fact.LogSHA256 {
			return fmt.Errorf("Main check log changed: %s", fact.Command)
		}
		profileDigest := ""
		for _, profile := range record.CheckProfiles {
			if profile.CheckIndex == fact.CheckIndex {
				profileDigest = profile.Digest()
			}
		}
		if fact.ProfileDigest != profileDigest {
			return fmt.Errorf("Main check differs from the original CheckProfile")
		}
	}
	return nil
}

func normalizedPhysicalRef(root, ref string) string {
	if filepath.IsAbs(ref) {
		return filepath.Clean(ref)
	}
	return filepath.Join(root, filepath.FromSlash(ref))
}

// CanonicalChangedPaths is for actual Git producer output, not Worker paths.
func CanonicalChangedPaths(paths []string) ([]string, string, error) {
	copy := append([]string{}, paths...)
	sort.Strings(copy)
	sha, err := ChangedPathsDigest(copy)
	return copy, sha, err
}
