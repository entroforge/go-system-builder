package repair

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// NativeS9SourceReference uses the existing execution inputs contract:
// immutable source path -> SHA256. No new Runtime schema or accepted ledger.
func NativeS9SourceReference(e workspace.Execution) ArtifactRef {
	var ref ArtifactRef
	for path, sha := range e.Inputs {
		if strings.HasPrefix(path, ".claude/evidence/s9-source/") && strings.HasSuffix(path, "/source-v1.json") {
			if ref.Path != "" {
				return ArtifactRef{Path: "invalid-multiple-native-sources"}
			}
			ref = ArtifactRef{Path: path, SHA256: sha}
		}
	}
	return ref
}

// NativeS9Source is a technical capture, never an acceptance receipt. Its
// digest is frozen in the original execution definition before Plan reception.
// Original byte slices deliberately preserve whitespace and JSON spelling.
type NativeS9Source struct {
	Version       int                                    `json:"version"`
	PacketID      string                                 `json:"packet_id"`
	PacketSHA     string                                 `json:"packet_sha256"`
	Binding       workpacket.RepairPlanReportBinding     `json:"binding"`
	Actor         string                                 `json:"actor"`
	Execution     workspace.Execution                    `json:"execution"`
	PreparedBase  string                                 `json:"prepared_base"`
	Head          string                                 `json:"head"`
	NoChange      bool                                   `json:"nochange"`
	ProductStatus string                                 `json:"product_status"`
	GitRef        string                                 `json:"git_ref"`
	Plan          []byte                                 `json:"plan_bytes"`
	Result        []byte                                 `json:"result_bytes"`
	PlanPath      string                                 `json:"plan_path"`
	ResultPath    string                                 `json:"result_path"`
	Session       RepairSession                          `json:"session"`
	Evidence      []runtimepkg.RepairResultLocalEvidence `json:"local_evidence"`
}

// validateRetainedNativeCheckout preserves the shared-repository/root check
// using the request context for every Git subprocess.
func validateRetainedNativeCheckout(ctx context.Context, root, source string) error {
	common, err := workspace.Git(ctx, root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	workerCommon, err := workspace.Git(ctx, source, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	common, err = workspace.Canonical(common)
	if err != nil {
		return err
	}
	workerCommon, err = workspace.Canonical(workerCommon)
	if err != nil {
		return err
	}
	top, err := workspace.Git(ctx, source, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	top, err = workspace.Canonical(top)
	if err != nil {
		return err
	}
	if common != workerCommon || top != source {
		return errors.New("native retained source is not its original checkout in the Main repository")
	}
	return ctx.Err()
}

// CaptureNativeS9Source may only be called after the adapter has resolved a
// real Stop actor and exact original brief/Result source. New version opt-in is
// checked by that adapter; legacy sources never acquire this capability.
func CaptureNativeS9Source(ctx context.Context, root string, p workpacket.RepairPlanReportPacket, e workspace.Execution, actor, preparedBase, resultPath string, professional []byte) (captured ArtifactRef, err error) {
	defer func() {
		if canceled := ctx.Err(); canceled != nil {
			captured = ArtifactRef{}
			err = canceled
		}
	}()
	if actor == "" || actor == "Main" || e.Purpose != workspace.NativeDeliveryPurpose || e.Generation != 1 || e.ReworkRef != "" || e.BootstrapSHA256 != "" || e.PlatformSessionID != "" {
		return ArtifactRef{}, errors.New("native source capture requires a real new Stop execution")
	}
	canonical, err := workspace.Canonical(e.Path)
	if err != nil || canonical != e.Path || sameAbsolutePath(root, e.Path) {
		return ArtifactRef{}, errors.New("retained native source requires original canonical Worker root")
	}
	b := p.Binding
	if e.RuntimeID != b.RuntimeID || e.BaselineGeneration != b.BaselineGeneration || e.AgentID != b.OwnerAgentID || e.AssignmentID != b.ManifestAssignmentID {
		return ArtifactRef{}, errors.New("retained native source responsibility differs")
	}
	if err := validateRetainedNativeCheckout(ctx, root, e.Path); err != nil {
		return ArtifactRef{}, err
	}
	head, err := workspace.Git(ctx, e.Path, "rev-parse", "HEAD")
	if err != nil {
		return ArtifactRef{}, err
	}
	if head != e.BaseCommit {
		return ArtifactRef{}, nil
	}
	if preparedBase == "" {
		return ArtifactRef{}, errors.New("native retained source missing frozen prepared baseline")
	}
	if _, err := workspace.Git(ctx, root, "merge-base", "--is-ancestor", preparedBase, head); err != nil {
		return ArtifactRef{}, errors.New("native retained source does not contain prepared baseline")
	}

	var answer workpacket.RepairResultProfessionalAnswer
	if err := json.Unmarshal(professional, &answer); err != nil {
		return ArtifactRef{}, err
	}
	if answer.Verdict != "completed" {
		return ArtifactRef{}, nil
	}
	plan, err := workpacket.ReadFile(e.Path, p.AnswerPath, repairResultAnswerLimit)
	if err != nil {
		return ArtifactRef{}, err
	}
	actual, err := workpacket.ReadFile(e.Path, resultPath, repairResultAnswerLimit)
	if err != nil {
		return ArtifactRef{}, err
	}
	if !bytes.Equal(actual, professional) {
		return ArtifactRef{}, runtimepkg.ErrSubmissionConflict
	}
	evidence, err := captureS9LocalEvidence(root, e.Path, p.ID, professional)
	if err != nil {
		return ArtifactRef{}, err
	}
	var session RepairSession
	if err := json.Unmarshal(p.Inputs.Session, &session); err != nil {
		return ArtifactRef{}, err
	}
	proposal := RepairResultRequest{issued: &issuedRepairResultCapability{provenance: runtimepkg.RepairResultCandidateProvenance{LocalEvidence: evidence}}}
	status, err := candidateProductStatus(ctx, root, e, session, proposal)
	if err != nil {
		return ArtifactRef{}, err
	}
	if status != "" {
		return ArtifactRef{}, fmt.Errorf("native source capture requires clean product: %s", status)
	}
	sha, err := workpacket.RepairPlanReportPacketDigest(p)
	if err != nil {
		return ArtifactRef{}, err
	}
	c := NativeS9Source{Version: 1, PacketID: p.ID, PacketSHA: sha, Binding: p.Binding, Actor: actor, Execution: e, PreparedBase: preparedBase, Head: head, NoChange: true, ProductStatus: status, Plan: plan, Result: professional, PlanPath: p.AnswerPath, ResultPath: resultPath, Session: session, Evidence: evidence}
	c.GitRef = "refs/loop-harness/s9-source/" + workpacket.Digest([]byte(p.ID+"\x00"+e.Path))
	// compare-and-create never moves a retained reference, even across retries.
	if old, err := workspace.Git(ctx, root, "rev-parse", "--verify", c.GitRef); err == nil {
		if old != head {
			return ArtifactRef{}, errors.New("native retained Git reference conflicts")
		}
	} else if _, err := workspace.Git(ctx, root, "update-ref", c.GitRef, head, ""); err != nil {
		// A simultaneous identical capture may have won compare-and-create.
		// Verify its target; never move a conflicting existing reference.
		actual, readErr := workspace.Git(ctx, root, "rev-parse", "--verify", c.GitRef)
		if readErr != nil || actual != head {
			return ArtifactRef{}, fmt.Errorf("retain native Git source: %w", err)
		}
	}
	body, err := json.Marshal(c)
	if err != nil {
		return ArtifactRef{}, err
	}
	ref := ArtifactRef{Path: ".claude/evidence/s9-source/" + p.ID + "/source-v1.json", SHA256: workpacket.Digest(body)}
	stored, err := workpacket.PublishBounded(root, ref.Path, body, 64<<20)
	if err != nil {
		return ArtifactRef{}, err
	}
	if !bytes.Equal(stored, body) {
		return ArtifactRef{}, runtimepkg.ErrSubmissionConflict
	}
	return ref, nil
}

// RetainedNativeS9Source validates a capability anchored by current Runtime
// execution inputs. A filename or unregistered capture grants no recovery.
func RetainedNativeS9Source(ctx context.Context, root string, e workspace.Execution, b workpacket.RepairPlanReportBinding) (retained *NativeS9Source, err error) {
	defer func() {
		if canceled := ctx.Err(); canceled != nil {
			retained = nil
			err = canceled
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reference := NativeS9SourceReference(e)
	ref, sha := reference.Path, reference.SHA256
	if ref == "" && sha == "" {
		return nil, nil
	}
	if e.Purpose != workspace.NativeDeliveryPurpose || e.BootstrapSHA256 != "" || e.PlatformSessionID != "" || ref == "" || sha == "" {
		return nil, errors.New("invalid retained native source capability")
	}
	body, err := workpacket.ReadFile(root, ref, 64<<20)
	if err != nil {
		return nil, err
	}
	if workpacket.Digest(body) != sha {
		return nil, errors.New("retained native source digest differs")
	}
	var c NativeS9Source
	if err := json.Unmarshal(body, &c); err != nil {
		return nil, err
	}
	original := c.Execution
	if c.Version != 1 || !c.NoChange || c.ProductStatus != "" || c.Actor == "" || c.Actor == "Main" || c.Head != e.BaseCommit || c.Binding != b || original.Path != e.Path || original.Branch != e.Branch || original.TargetBranch != e.TargetBranch || original.CheckLocation != e.CheckLocation || original.BaseCommit != e.BaseCommit || original.Generation != e.Generation || original.ReworkRef != e.ReworkRef || original.AgentID != e.AgentID || original.AssignmentID != e.AssignmentID || original.RuntimeID != e.RuntimeID || original.BaselineGeneration != e.BaselineGeneration || !reflect.DeepEqual(original.WritePaths, e.WritePaths) {
		return nil, errors.New("retained native source differs from current responsibility/attempt")
	}
	// Freeze all original definition fields, including check profiles and
	// scope; only progress and this newly registered source input may differ.
	originalDefinition := original
	currentDefinition := e
	currentDefinition.Inputs = map[string]string{}
	for path, sha := range e.Inputs {
		if path != ref {
			currentDefinition.Inputs[path] = sha
		}
	}
	project := func(value workspace.Execution) ([]byte, error) {
		body, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		var row map[string]any
		if err := json.Unmarshal(body, &row); err != nil {
			return nil, err
		}
		return runtimepkg.ProjectRepairResultExecution(row)
	}
	before, err := project(originalDefinition)
	if err != nil {
		return nil, err
	}
	after, err := project(currentDefinition)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(before, after) {
		return nil, errors.New("retained native source execution definition changed")
	}
	// Packet identity is revalidated from the original issued source below by
	// the caller's exact binding; canonical packet digest is also retained.
	if c.PacketID == "" || c.PacketSHA == "" || c.GitRef != "refs/loop-harness/s9-source/"+workpacket.Digest([]byte(c.PacketID+"\x00"+e.Path)) {
		return nil, errors.New("retained native source identity missing")
	}
	if got, err := workspace.Git(ctx, root, "rev-parse", "--verify", c.GitRef); err != nil || got != c.Head {
		return nil, errors.New("retained native Git reference unavailable or moved")
	}
	if _, err := workspace.Git(ctx, root, "cat-file", "-e", c.Head+"^{commit}"); err != nil {
		return nil, err
	}
	if c.PreparedBase == "" {
		return nil, errors.New("retained prepared baseline missing")
	}
	if _, err := workspace.Git(ctx, root, "merge-base", "--is-ancestor", c.PreparedBase, c.Head); err != nil {
		return nil, errors.New("retained Git ancestry differs from frozen preparation")
	}

	if err := verifyS9CapturedEvidence(root, runtimepkg.RepairResultCandidateProvenance{ExecutionRoot: e.Path, LocalEvidence: c.Evidence}); err != nil {
		return nil, err
	}
	// If the original tree survives, changed inputs cannot silently select the
	// old capture. Missing trees use only the anchored immutable bytes.
	if _, err := os.Lstat(e.Path); err == nil {
		canonical, err := workspace.Canonical(e.Path)
		if err != nil || canonical != e.Path {
			return nil, errors.New("retained native Worker root is no longer canonical")
		}
		branch, err := workspace.Git(ctx, e.Path, "branch", "--show-current")
		if err != nil || branch != c.Execution.Branch {
			return nil, runtimepkg.ErrSubmissionConflict
		}
		if err := validateRetainedNativeCheckout(ctx, root, e.Path); err != nil {
			return nil, err
		}
		for path, want := range map[string][]byte{c.PlanPath: c.Plan, c.ResultPath: c.Result} {
			actual, err := workpacket.ReadFile(e.Path, path, repairResultAnswerLimit)
			if err != nil || !bytes.Equal(actual, want) {
				return nil, runtimepkg.ErrSubmissionConflict
			}
		}
		for _, local := range c.Evidence {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			actual, err := workpacket.ReadFile(e.Path, local.SourcePath, repairResultAnswerLimit)
			if err != nil || workpacket.Digest(actual) != local.SHA256 {
				return nil, runtimepkg.ErrSubmissionConflict
			}
		}
		proposal := RepairResultRequest{issued: &issuedRepairResultCapability{provenance: runtimepkg.RepairResultCandidateProvenance{LocalEvidence: c.Evidence}}}
		status, err := candidateProductStatus(ctx, root, e, c.Session, proposal)
		if err != nil {
			return nil, err
		}
		if status != "" {
			return nil, runtimepkg.ErrSubmissionConflict
		}
		head, err := workspace.Git(ctx, e.Path, "rev-parse", "HEAD")
		if err != nil || head != c.Head {
			return nil, runtimepkg.ErrSubmissionConflict
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return &c, nil
}

// NativeS9TypedInputPath names the system wrapper separately from its real
// Worker routing coordinate. The cache is never represented as a checkout.
func NativeS9TypedInputPath(source *NativeS9Source, packet workpacket.RepairResultPacket) string {
	return ".claude/evidence/s9-source/" + source.PacketID + "/typed/" + packet.ID + ".json"
}

// ReadRepairResultInput retains the true Worker root while resolving bytes
// through a separately verified immutable technical source when present.
func ReadRepairResultInput(ctx context.Context, root string, state map[string]any, p workpacket.RepairResultPacket, e workspace.Execution) (input []byte, err error) {
	defer func() {
		if canceled := ctx.Err(); canceled != nil {
			input = nil
			err = canceled
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c, err := RetainedNativeS9Source(ctx, root, e, p.Binding.Sources)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return workpacket.ReadFile(e.Path, p.AnswerPath, repairResultAnswerLimit)
	}
	if _, err := runtimepkg.RepairResultExecution(state, p); err != nil {
		return nil, err
	}
	pointer := repairPointer(state)
	b := p.Binding.Sources
	if workspace.RuntimeID(state) != b.RuntimeID || workspace.Generation(state) != b.BaselineGeneration || pointer["session_id"] != b.SessionID || pointer["plan_ref"] != b.PlanRef || pointer["contract_ref"] != b.ContractRef || pointer["contract_sha256"] != b.ContractSHA256 || stringMapField(pointer["assignment_owners"])[b.RepairAssignmentID] != b.OwnerAgentID {
		return nil, errors.New("retained S9 source is outside current approved responsibility")
	}

	draft := p.Draft
	draft.Answer = append(json.RawMessage(nil), c.Result...)
	body, err := json.MarshalIndent(draft, "", "  ")
	if err != nil {
		return nil, err
	}
	body = append(body, '\n')
	path := NativeS9TypedInputPath(c, p)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stored, err := workpacket.PublishBounded(root, path, body, repairResultAnswerLimit)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(stored, body) {
		return nil, runtimepkg.ErrSubmissionConflict
	}
	return stored, nil
}
