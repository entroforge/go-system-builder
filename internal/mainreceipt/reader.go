// Package mainreceipt verifies the immutable, read-only portion of an S6 Main
// integration receipt. It deliberately does not resolve current Runtime,
// PLAN, activation, execution or journal authority. Callers must establish
// those bindings before trusting a successful proof returned here.
package mainreceipt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const (
	checkpointLimit = 16 << 20
	checkFileLimit  = 4 << 20
	gitBlobLimit    = 16 << 20
)

var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// ErrNotVerified identifies an exactly bound, preserved integration attempt which has not
// completed Main verification. It carries no successful contribution proof.
// A caller may omit it from historical verified contributions; a current
// Assignment still requires Verify to succeed.
var ErrNotVerified = errors.New("preserved Main merge has not completed verification")

// Input contains data already resolved by the original authority layer. In
// particular, Accepted must be the result of LoadAccepted, LoadContribution or
// LoadSupersededAccepted, ExecutionGeneration must come from the caller's
// current or historical Runtime execution registry, AuthorizedMainRoots must
// come from caller-validated Runtime relocation state, and FinalTargetHead
// must be pinned from the caller-authorized development branch.
//
// These fields select evidence to inspect; they do not grant integration or
// acceptance authority.
type Input struct {
	AuthorityRoot        string
	Record               assignmentrecord.Record
	Accepted             assignmentresult.AcceptedResult
	ResultRef            string
	ResultSHA256         string
	ExecutionGeneration  int
	AuthorizedMainRoots  []string
	ExpectedTargetBranch string
	FinalTargetHead      string
}

// ChangedFile describes a file path from the original MainFacts Git diff.
// Each digest is SHA-256 over actual blob bytes at the named commit; empty
// means that commit's tree has no blob at Path. Status is the original Git
// status from MergeBase..SourceHead with rename detection disabled.
type ChangedFile struct {
	Path             string `json:"path"`
	Status           string `json:"status"`
	BaseBlobSHA256   string `json:"base_blob_sha256,omitempty"`
	TargetBlobSHA256 string `json:"target_blob_sha256,omitempty"`
	SourceBlobSHA256 string `json:"source_blob_sha256,omitempty"`
	MergeBlobSHA256  string `json:"merge_blob_sha256,omitempty"`
	TestedBlobSHA256 string `json:"tested_blob_sha256,omitempty"`
	FinalBlobSHA256  string `json:"final_blob_sha256,omitempty"`
}

// VerifiedContribution is produced only by Verify. It exposes a read-only
// copy of the facts and per-file Git objects after all receipt, scope and
// commit checks succeed. It does not mean that the current PLAN/activation or
// Runtime registration is authorized, nor that a professional Result passed.
type VerifiedContribution struct {
	recordRef       assignmentrecord.RecordReference
	recordSHA256    string
	resultBinding   assignmentresult.ResultBinding
	checkpointPath  string
	checkpointSHA   string
	finalTargetHead string
	facts           assignmentresult.MainFacts
	changedFiles    []ChangedFile
}

func (v VerifiedContribution) RecordReference() assignmentrecord.RecordReference { return v.recordRef }
func (v VerifiedContribution) RecordSHA256() string                              { return v.recordSHA256 }
func (v VerifiedContribution) ResultBinding() assignmentresult.ResultBinding     { return v.resultBinding }
func (v VerifiedContribution) CheckpointPath() string                            { return v.checkpointPath }
func (v VerifiedContribution) CheckpointSHA256() string                          { return v.checkpointSHA }
func (v VerifiedContribution) FinalTargetHead() string                           { return v.finalTargetHead }

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
	AssignmentRecord       *assignmentrecord.RecordReference `json:"assignment_record,omitempty"`
	AssignmentRecordSHA256 string                            `json:"assignment_record_sha256,omitempty"`
	TaskID                 string                            `json:"task_id"`
	AssignmentID           string                            `json:"assignment_id"`
	Generation             int                               `json:"baseline_generation"`
	State                  string                            `json:"state"`
	VerifiedAt             string                            `json:"verified_at"`
	ReportPath             string                            `json:"completion_report_path"`
	ReportSHA              string                            `json:"completion_report_sha256"`
	SourceHead             string                            `json:"source_head"`
	TargetHead             string                            `json:"target_head"`
	MergeBase              string                            `json:"merge_base"`
	TargetBranch           string                            `json:"target_branch"`
	MergeCommit            string                            `json:"merge_commit"`
	ChangedPaths           []string                          `json:"changed_paths"`
	ChangedPathsSHA256     string                            `json:"changed_paths_sha256"`
	TestedHead             string                            `json:"tested_head"`
	PreviousMainRoots      []string                          `json:"previous_main_roots,omitempty"`
	CheckReceipts          []string                          `json:"check_receipts,omitempty"`
	ToolCheckReceipts      []checkexec.ToolReceipt           `json:"tool_check_receipts,omitempty"`
	CanonicalMainFacts     *assignmentresult.MainFacts       `json:"canonical_main_facts"`
	Revision               int64                             `json:"revision"`
}

// Verify reads the deterministic Record-specific checkpoint and proves its
// immutable Main check and Git contribution. It intentionally does not read
// Runtime state or journal and does not resolve the current accepted Result
// pointer; callers retain those authority checks.
func Verify(ctx context.Context, in Input) (out VerifiedContribution, err error) {
	if ctx == nil {
		ctx = context.Background()
	}
	defer func() {
		if ctx.Err() != nil {
			out, err = VerifiedContribution{}, ctx.Err()
		}
	}()
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if err := assignmentrecord.ValidateRecord(in.Record); err != nil {
		return out, fmt.Errorf("validate Assignment Record: %w", err)
	}
	recordSHA, err := in.Record.Digest()
	if err != nil {
		return out, err
	}
	recordRef := in.Record.Reference()
	if in.Accepted.AssignmentRecord != recordRef || in.Accepted.AssignmentRecordSHA256 != recordSHA || in.Accepted.Conclusion != "submitted" || in.ResultRef == "" || !validRelativeRef(in.ResultRef) || !sha256Pattern.MatchString(in.ResultSHA256) || in.ExecutionGeneration < 0 {
		return out, fmt.Errorf("input Result or Record binding is incomplete")
	}
	if in.ExpectedTargetBranch == "" || in.FinalTargetHead == "" {
		return out, fmt.Errorf("caller must pin the authorized target branch and final target head")
	}
	root, err := filepath.Abs(in.AuthorityRoot)
	if err != nil || strings.TrimSpace(in.AuthorityRoot) == "" {
		return out, fmt.Errorf("Runtime authority root is invalid")
	}
	if err := requireAuthorizedRoots(root, in.AuthorizedMainRoots); err != nil {
		return out, err
	}
	checkpointRel := filepath.ToSlash(assignmentrecord.IntegrationCheckpointPath("", recordRef))
	if checkpointRel == "" {
		return out, fmt.Errorf("Assignment Record has no deterministic checkpoint location")
	}
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
	bindingErr := validateCheckpointBinding(cp, in, recordSHA)
	if bindingErr != nil && !errors.Is(bindingErr, ErrNotVerified) {
		return out, bindingErr
	}
	if bindingErr != nil && cp.CanonicalMainFacts == nil {
		// A precisely bound pre-merge failure has no Main proof to read.
		// Historical callers may omit it; current callers still reject it.
		return out, bindingErr
	}
	facts := *cp.CanonicalMainFacts
	if err := validateAuthorizedMainRoots(root, in.AuthorizedMainRoots, cp.PreviousMainRoots, facts.MainRoot); err != nil {
		return out, err
	}
	if bindingErr != nil {
		if err := verifyMerge(ctx, root, cp, in.FinalTargetHead); err != nil {
			return out, fmt.Errorf("verify preserved Main merge: %w", err)
		}
		return out, bindingErr
	}
	checksDirRel := path.Join(path.Dir(checkpointRel), "checks")
	reader := func(ref string) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rel, err := checkpointArtifactRel(root, checksDirRel, facts.MainRoot, cp.PreviousMainRoots, in.AuthorizedMainRoots, ref)
		if err != nil {
			return nil, err
		}
		return workpacket.ReadFile(root, rel, checkFileLimit)
	}
	if err := assignmentresult.ValidateMainFacts(in.Record, in.Accepted, in.ResultRef, in.ResultSHA256, facts, reader); err != nil {
		return out, fmt.Errorf("validate original MainFacts and runner receipts: %w", err)
	}
	if err := validateCheckReceiptSet(root, checksDirRel, in, cp, facts, reader); err != nil {
		return out, err
	}
	files, err := verifyGitContribution(ctx, root, cp, facts, in.FinalTargetHead)
	if err != nil {
		return out, fmt.Errorf("verify original Main Git contribution: %w", err)
	}
	out = VerifiedContribution{
		recordRef: recordRef, recordSHA256: recordSHA,
		resultBinding: facts.Binding, checkpointPath: filepath.Join(root, filepath.FromSlash(checkpointRel)),
		checkpointSHA: checkexec.Hash(checkpointBytes), finalTargetHead: in.FinalTargetHead,
		facts: cloneMainFacts(facts), changedFiles: files,
	}
	return out, nil
}

func validateCheckpointBinding(cp checkpoint, in Input, recordSHA string) error {
	record := in.Record
	if cp.AssignmentRecord == nil && cp.AssignmentRecordSHA256 != "" || cp.AssignmentRecord != nil && (*cp.AssignmentRecord != record.Reference() || cp.AssignmentRecordSHA256 != recordSHA) {
		return fmt.Errorf("checkpoint Record reference or digest differs from the supplied Record")
	}
	if cp.CanonicalMainFacts == nil && cp.AssignmentRecord != nil &&
		cp.TaskID == record.Task.ID && cp.AssignmentID == record.AssignmentID && cp.Generation == record.BaselineGeneration &&
		cp.TargetBranch == in.ExpectedTargetBranch && cp.State == "preserved" && cp.Revision > 0 &&
		cp.MergeCommit == "" && cp.TestedHead == "" && cp.VerifiedAt == "" && cp.ReportPath == "" && cp.ReportSHA == "" &&
		cp.ChangedPaths == nil && cp.ChangedPathsSHA256 == "" && len(cp.CheckReceipts) == 0 && len(cp.ToolCheckReceipts) == 0 &&
		unverifiedInspectionCoordinates(cp) {
		return ErrNotVerified // Original pre-merge failure; never a verified contribution.
	}
	if cp.CanonicalMainFacts == nil || cp.TaskID != record.Task.ID || cp.AssignmentID != record.AssignmentID || cp.Generation != record.BaselineGeneration || cp.TargetBranch != in.ExpectedTargetBranch || cp.SourceHead != cp.CanonicalMainFacts.SourceHead || cp.MergeBase != cp.CanonicalMainFacts.MergeBase || cp.TestedHead != cp.CanonicalMainFacts.TestedHead || cp.MergeCommit != cp.CanonicalMainFacts.MergeCommit || cp.CanonicalMainFacts.TargetBranch != cp.TargetBranch || cp.ChangedPaths != nil || cp.ChangedPathsSHA256 != "" {
		return fmt.Errorf("checkpoint does not preserve the exact canonical Result and MainFacts coordinates")
	}
	// Before verification, the original Integrator stores the Result in the
	// canonical binding; the duplicate completion fields are populated later.
	unverified := cp.State == "preserved" && cp.TestedHead == ""
	if cp.ReportPath != in.ResultRef || cp.ReportSHA != in.ResultSHA256 {
		if !unverified || cp.ReportPath != "" || cp.ReportSHA != "" {
			return fmt.Errorf("checkpoint completion fields differ from the exact accepted Result")
		}
	}
	if cp.CanonicalMainFacts.Binding.ResultPath != in.ResultRef || cp.CanonicalMainFacts.Binding.ResultSHA256 != in.ResultSHA256 || cp.CanonicalMainFacts.Binding.ResultRevision != in.Accepted.ResultRevision || cp.CanonicalMainFacts.Binding.AnswerDigest != in.Accepted.AnswerDigest || cp.CanonicalMainFacts.Binding.AssignmentRecord != record.Reference() || cp.CanonicalMainFacts.Binding.AssignmentRecordSHA256 != recordSHA {
		return fmt.Errorf("MainFacts ResultBinding differs from the exact accepted Result")
	}
	if cp.CanonicalMainFacts.ExecutionGeneration != in.ExecutionGeneration {
		return fmt.Errorf("MainFacts execution generation differs from caller-resolved Runtime authority")
	}
	if unverified && cp.Revision > 0 && fullObjectID(cp.TargetHead) && fullObjectID(cp.SourceHead) && fullObjectID(cp.MergeBase) && fullObjectID(cp.MergeCommit) {
		return ErrNotVerified
	}
	if cp.TestedHead == "" {
		return fmt.Errorf("checkpoint has no verified Main head")
	}
	switch cp.State {
	case "verified", "acknowledged", "cleanup_pending", "complete":
	default:
		return fmt.Errorf("checkpoint state %q is not a durable Main-verified state", cp.State)
	}
	if cp.Revision < 1 {
		return fmt.Errorf("checkpoint has no durable revision")
	}
	created, createdErr := time.Parse(time.RFC3339Nano, in.Accepted.CreatedAt)
	verified, verifiedErr := time.Parse(time.RFC3339Nano, cp.VerifiedAt)
	if createdErr != nil || verifiedErr != nil || verified.Before(created) {
		return fmt.Errorf("checkpoint verification time does not follow accepted Result")
	}
	return nil
}

// Inspect resolves source, target and merge base in that order and may stop
// before any of them. Its preserved failure contains only the coordinates
// actually observed. This classification grants no successful Main proof:
// current work still fails, and only the historical caller with the verified
// revision chain may omit it from successful contributions.
func unverifiedInspectionCoordinates(cp checkpoint) bool {
	for _, value := range []string{cp.SourceHead, cp.TargetHead, cp.MergeBase} {
		if value != "" && !fullObjectID(value) {
			return false
		}
	}
	return (cp.TargetHead == "" || cp.SourceHead != "") && (cp.MergeBase == "" || cp.SourceHead != "" && cp.TargetHead != "")
}

func validateCheckReceiptSet(root, checksDirRel string, in Input, cp checkpoint, facts assignmentresult.MainFacts, read func(string) ([]byte, error)) error {
	if len(cp.CheckReceipts) != len(facts.Checks) || len(cp.ToolCheckReceipts) != len(in.Record.CheckProfiles) {
		return fmt.Errorf("checkpoint does not preserve complete Main runner and CheckProfile receipt coverage")
	}
	factByIndex := make(map[int]assignmentresult.MainCheckFact, len(facts.Checks))
	factRefs := make(map[string]bool, len(facts.Checks))
	for _, fact := range facts.Checks {
		if factRefs[fact.ReceiptRef] {
			return fmt.Errorf("MainFacts duplicate a receipt reference")
		}
		factRefs[fact.ReceiptRef] = true
		factByIndex[fact.CheckIndex] = fact
	}
	checkpointRefs := make(map[string]bool, len(cp.CheckReceipts))
	for _, ref := range cp.CheckReceipts {
		if ref == "" || checkpointRefs[ref] || !factRefs[ref] {
			return fmt.Errorf("checkpoint Main receipt list differs from exact MainFacts coverage")
		}
		checkpointRefs[ref] = true
	}
	if len(checkpointRefs) != len(factRefs) {
		return fmt.Errorf("checkpoint omits a MainFacts receipt")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	mainBinding := checkexec.Binding{
		RuntimeID: in.Record.RuntimeID, Generation: in.Record.BaselineGeneration,
		AssignmentID: in.Record.AssignmentID, Owner: "Main",
		ExecutionGeneration: facts.ExecutionGeneration, Responsibility: "main_required_integration",
	}
	profileByIndex := make(map[int]checkexec.Profile, len(in.Record.CheckProfiles))
	for _, profile := range in.Record.CheckProfiles {
		if profile.Location != "main" {
			return fmt.Errorf("canonical Main CheckProfile %s has non-Main location", profile.ID)
		}
		if _, exists := profileByIndex[profile.CheckIndex]; exists {
			return fmt.Errorf("canonical Main CheckProfiles duplicate an index")
		}
		profileByIndex[profile.CheckIndex] = profile
	}
	seenProfiles := map[int]bool{}
	seenPaths := map[string]bool{}
	for _, original := range cp.ToolCheckReceipts {
		index, profile, found := matchingProfile(in.Record, original)
		if !found || seenProfiles[index] || seenPaths[original.Path] || original.Binding != mainBinding || original.Command != profile.Command || original.ProfileDigest != profile.Digest() {
			return fmt.Errorf("captured Main tool receipt differs from the frozen CheckProfile or binding")
		}
		fact, found := factByIndex[index]
		if !found || fact.Command != profile.Command || fact.ProfileDigest != profile.Digest() || fact.ReceiptRef != original.Path {
			return fmt.Errorf("captured Main tool receipt is not the exact MainFacts check")
		}
		if err := checkpointArtifactRelOnly(root, checksDirRel, facts.MainRoot, cp.PreviousMainRoots, in.AuthorizedMainRoots, original.Path); err != nil {
			return err
		}
		body, err := read(original.Path)
		if err != nil || checkexec.Hash(body) != original.SHA256 {
			return fmt.Errorf("captured Main tool receipt bytes changed: %s", profile.Command)
		}
		current := original
		current.Path, err = physicalCheckpointArtifact(rootAbs, checksDirRel, facts.MainRoot, cp.PreviousMainRoots, in.AuthorizedMainRoots, original.Path)
		if err != nil {
			return err
		}
		verified, err := checkexec.VerifyToolReceipt(filepath.Join(rootAbs, filepath.FromSlash(checksDirRel)), current)
		if err != nil || !verified.Passed || verified.Observation.ProfileDigest != profile.Digest() {
			return fmt.Errorf("captured Main tool receipt is not the passing original receipt: %s", profile.Command)
		}
		checkout := facts.MainRoot
		if resolved, resolveErr := filepath.EvalSymlinks(checkout); resolveErr == nil {
			checkout = resolved
		}
		if original.Checkout != checkout {
			return fmt.Errorf("captured Main tool receipt checkout differs from its original Main root")
		}
		seenProfiles[index], seenPaths[original.Path] = true, true
	}
	if len(seenProfiles) != len(profileByIndex) {
		return fmt.Errorf("checkpoint omits an original Main CheckProfile receipt")
	}
	return nil
}

func matchingProfile(record assignmentrecord.Record, ref checkexec.ToolReceipt) (int, checkexec.Profile, bool) {
	var match checkexec.Profile
	found := false
	for _, profile := range record.CheckProfiles {
		if profile.Command != ref.Command || profile.Digest() != ref.ProfileDigest {
			continue
		}
		if found {
			return -1, checkexec.Profile{}, false
		}
		match, found = profile, true
	}
	return match.CheckIndex, match, found
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

func checkpointArtifactRel(root, checksDirRel string, factsRoot string, previous, authorized []string, ref string) (string, error) {
	return checkpointArtifactRelative(root, checksDirRel, factsRoot, previous, authorized, ref)
}

func checkpointArtifactRelOnly(root, checksDirRel string, factsRoot string, previous, authorized []string, ref string) error {
	_, err := checkpointArtifactRelative(root, checksDirRel, factsRoot, previous, authorized, ref)
	return err
}

func physicalCheckpointArtifact(root, checksDirRel string, factsRoot string, previous, authorized []string, ref string) (string, error) {
	rel, err := checkpointArtifactRelative(root, checksDirRel, factsRoot, previous, authorized, ref)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, filepath.FromSlash(rel)), nil
}

func checkpointArtifactRelative(authorityRoot, checksDirRel, factsRoot string, previous, authorized []string, ref string) (string, error) {
	if strings.TrimSpace(ref) == "" || strings.ContainsAny(ref, "\\\x00\r\n") {
		return "", fmt.Errorf("Main check artifact reference is invalid")
	}
	allowedOrigins := []string{}
	allowedOrigins = append(allowedOrigins, authorityRoot)
	allowedOrigins = append(allowedOrigins, factsRoot)
	allowedOrigins = append(allowedOrigins, previous...)
	allowedOrigins = append(allowedOrigins, authorized...)
	wantPrefix := path.Clean(checksDirRel) + "/"
	var rel string
	if filepath.IsAbs(ref) {
		if filepath.Clean(ref) != ref {
			return "", fmt.Errorf("Main check artifact absolute reference is not normalized")
		}
		refAbs := filepath.Clean(ref)
		for _, origin := range allowedOrigins {
			if !isExactAllowedOrigin(origin, authorityRoot, factsRoot, previous, authorized) {
				continue
			}
			rootAbs, err := filepath.Abs(origin)
			if err != nil {
				continue
			}
			candidate, err := filepath.Rel(rootAbs, refAbs)
			if err == nil && filepath.IsLocal(candidate) {
				candidate = filepath.ToSlash(candidate)
				if strings.HasPrefix(candidate, wantPrefix) {
					rel = candidate
					break
				}
			}
		}
	} else {
		candidate := filepath.ToSlash(filepath.Clean(filepath.FromSlash(ref)))
		if candidate == ref && strings.HasPrefix(candidate, wantPrefix) {
			rel = candidate
		}
	}
	if rel == "" {
		return "", fmt.Errorf("Main check artifact is outside the exact Assignment checkpoint checks directory")
	}
	parts := strings.Split(strings.TrimPrefix(rel, wantPrefix), "/")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "loop-integration-check-") || len(parts[0]) == len("loop-integration-check-") || (parts[1] != "receipt.json" && parts[1] != "output.log") {
		return "", fmt.Errorf("Main check artifact does not use the original runner receipt layout")
	}
	if rel == "" || !filepath.IsLocal(filepath.FromSlash(rel)) {
		return "", fmt.Errorf("Main check artifact path is not local")
	}
	return rel, nil
}

func isExactAllowedOrigin(origin, authorityRoot, factsRoot string, previous, authorized []string) bool {
	if !filepath.IsAbs(origin) {
		return false
	}
	allowed := append(append(append(append([]string(nil), authorized...), previous...), factsRoot), authorityRoot)
	for _, candidate := range allowed {
		if !filepath.IsAbs(candidate) {
			continue
		}
		if filepath.Clean(origin) == filepath.Clean(candidate) {
			return true
		}
		originAbs, originErr := filepath.Abs(origin)
		candidateAbs, candidateErr := filepath.Abs(candidate)
		if originErr == nil && candidateErr == nil && filepath.Clean(originAbs) == filepath.Clean(candidateAbs) {
			return true
		}
	}
	return false
}

// hashBytes is kept local so Git-blob digests use SHA-256 without importing a
// higher integration layer.
func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
