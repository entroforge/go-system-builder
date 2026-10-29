package assignment

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/assignmentinputs"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/team"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// NativeCheckSource is the exact owner checkout declared in the Record-frozen
// workgroup manifest. It deliberately has no sidecar or caller-supplied path
// fallback: native checks require the checkout coordinate to be part of the
// immutable manifest source.
type NativeCheckSource struct {
	Checkout       string
	Branch         string
	TargetBranch   string
	Head           string
	ManifestRef    string
	ManifestSHA256 string
}

// ResolveFrozenNativeCheckSource resolves the native owner's source checkout
// only from bytes covered by the current Assignment Record. It does not read
// .claude/assignments sidecars or historical integration checkpoints.
func ResolveFrozenNativeCheckSource(root string, state map[string]any, record assignmentrecord.Record) (NativeCheckSource, error) {
	var out NativeCheckSource
	if err := assignmentrecord.ValidateRecord(record); err != nil {
		return out, err
	}
	current, found, err := assignmentrecord.ForOwner(state, record.OwnerAgentID)
	if err != nil {
		return out, err
	}
	if !found {
		return out, fmt.Errorf("native check has no current canonical Assignment Record")
	}
	currentSHA, err := current.Digest()
	if err != nil {
		return out, err
	}
	recordSHA, err := record.Digest()
	if err != nil || current.Reference() != record.Reference() || currentSHA != recordSHA {
		return out, fmt.Errorf("native check Assignment Record is not the current original Record")
	}
	if record.ClosingContract == nil || len(record.ClosingContract.WorkerChecks) == 0 {
		return out, fmt.Errorf("native check requires an explicitly frozen WorkerCheck")
	}

	mainRoot, err := workspace.Canonical(root)
	if err != nil {
		return out, err
	}
	manifestBytes, err := assignmentinputs.Read(mainRoot, record.Manifest.Ref)
	if err != nil {
		return out, fmt.Errorf("read Record-frozen workgroup manifest: %w", err)
	}
	if workpacket.Digest(manifestBytes) != record.Manifest.SHA256 {
		return out, fmt.Errorf("Record-frozen workgroup manifest bytes changed")
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("team-manifest.schema.json", manifestBytes); err != nil {
		return out, err
	}
	if err := team.ValidateBytes(manifestBytes); err != nil {
		return out, err
	}
	var manifest struct {
		RuntimeID          string `json:"runtime_id"`
		BaselineGeneration int    `json:"baseline_generation"`
		WorkgroupID        string `json:"workgroup_id"`
		Assignments        []struct {
			AssignmentID string `json:"assignment_id"`
			AgentID      string `json:"agent_id"`
			WorktreePath string `json:"worktree_path"`
			Branch       string `json:"branch"`
			TargetBranch string `json:"target_branch"`
		} `json:"assignments"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return out, err
	}
	if manifest.RuntimeID != record.RuntimeID || manifest.BaselineGeneration != record.BaselineGeneration || manifest.WorkgroupID != record.TeamID {
		return out, fmt.Errorf("frozen manifest Runtime/generation/Team differs from Assignment Record")
	}
	var coordinate *struct {
		AssignmentID string `json:"assignment_id"`
		AgentID      string `json:"agent_id"`
		WorktreePath string `json:"worktree_path"`
		Branch       string `json:"branch"`
		TargetBranch string `json:"target_branch"`
	}
	for i := range manifest.Assignments {
		row := &manifest.Assignments[i]
		if row.AssignmentID != record.AssignmentID {
			continue
		}
		if coordinate != nil || row.AgentID != record.OwnerAgentID {
			return out, fmt.Errorf("frozen manifest has duplicate or misowned Assignment coordinates")
		}
		coordinate = row
	}
	if coordinate == nil {
		return out, fmt.Errorf("frozen manifest omits the current Assignment")
	}
	if strings.TrimSpace(coordinate.WorktreePath) == "" || strings.TrimSpace(coordinate.Branch) == "" || strings.TrimSpace(coordinate.TargetBranch) == "" {
		return out, fmt.Errorf("native check requires worktree_path, branch and target_branch in the Record-frozen manifest; sidecar-only coordinates are not accepted")
	}
	if strings.ContainsAny(coordinate.WorktreePath, "\x00\r\n") || strings.ContainsAny(coordinate.Branch+coordinate.TargetBranch, "\x00\r\n") {
		return out, fmt.Errorf("frozen native worktree coordinates contain control characters")
	}

	path := coordinate.WorktreePath
	if !filepath.IsAbs(path) {
		path = filepath.Join(mainRoot, filepath.FromSlash(path))
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return out, err
	}
	if info, statErr := os.Lstat(path); statErr != nil {
		return out, fmt.Errorf("frozen native worktree is unavailable: %w", statErr)
	} else if info.Mode()&os.ModeSymlink != 0 {
		return out, fmt.Errorf("frozen native worktree root is a symlink")
	}
	checkout, err := workspace.Canonical(path)
	if err != nil {
		return out, err
	}
	if checkout == mainRoot {
		return out, fmt.Errorf("native Worker check cannot use the Main checkout")
	}
	if err := workspace.ValidateCheckout(mainRoot, checkout); err != nil {
		return out, fmt.Errorf("Record-frozen native worktree is not a checkout of the bound repository: %w", err)
	}
	branch, err := workspace.Git(context.Background(), checkout, "branch", "--show-current")
	if err != nil || strings.TrimSpace(branch) != coordinate.Branch {
		return out, fmt.Errorf("native worktree branch differs from its Record-frozen coordinate")
	}
	devBranch, err := fileview.DevelopmentRef(state)
	if err != nil {
		return out, err
	}
	devBranch = strings.TrimPrefix(devBranch, "refs/heads/")
	targetBranch := strings.TrimPrefix(coordinate.TargetBranch, "refs/heads/")
	if targetBranch == "" || targetBranch != devBranch {
		return out, fmt.Errorf("native worktree target differs from the current REQ development branch")
	}
	head, err := workspace.Git(context.Background(), checkout, "rev-parse", "HEAD")
	if err != nil {
		return out, fmt.Errorf("resolve native worktree source HEAD: %w", err)
	}
	if strings.TrimSpace(head) == "" {
		return out, fmt.Errorf("native worktree source HEAD is empty")
	}
	out = NativeCheckSource{
		Checkout:       checkout,
		Branch:         coordinate.Branch,
		TargetBranch:   targetBranch,
		Head:           strings.TrimSpace(head),
		ManifestRef:    record.Manifest.Ref,
		ManifestSHA256: record.Manifest.SHA256,
	}
	return out, nil
}
