package repair

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// FindNativeS9StagedCandidate checks the original content-addressed Candidate
// artifacts only for a changed author original or a missing typed input. A
// match is a fixed input, not an acceptance receipt. A bounded or unreadable
// search is inconclusive and must never authorize replacing the original typed
// input. The caller holds the original workspace lock throughout the check and
// any subsequent typed write/consumer call.
func FindNativeS9StagedCandidate(ctx context.Context, root string, packet workpacket.RepairResultPacket, e workspace.Execution) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("cannot verify original staged S9 Candidate within recovery budget: %w", err)
	}
	dir, _, err := workpacket.OpenParent(root, ".claude/review/repair/candidates/.lookup", false)
	if errors.Is(err, os.ErrNotExist) {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("cannot verify original staged S9 Candidate within recovery budget: %w", err)
		}
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer dir.Close()
	files, err := dir.Open(".")
	if err != nil {
		return "", err
	}
	defer files.Close()
	const maxFiles = 256
	const maxBytes int64 = 64 << 20
	count := 0
	var size int64
	for {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("cannot verify original staged S9 Candidate within recovery budget: %w", err)
		}
		entries, readErr := files.ReadDir(16)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return "", readErr
		}
		for _, entry := range entries {
			name := entry.Name()
			count++
			info, err := entry.Info()
			if err != nil {
				return "", fmt.Errorf("cannot verify original S9 Candidate %s: %v", name, err)
			}
			if info.Mode().IsRegular() {
				size += info.Size()
			}
			if count > maxFiles || size > maxBytes {
				return "", fmt.Errorf("cannot verify original staged S9 Candidate within recovery budget")
			}
			// CreateWorkspaceCandidate has only ever published regular files
			// named by the complete lowercase content SHA under this directory.
			// Other entries still consume the scan budget above.
			if entry.IsDir() || len(name) != 69 || !strings.HasSuffix(name, ".json") {
				continue
			}
			digest := strings.TrimSuffix(name, ".json")
			if _, err := hex.DecodeString(digest); err != nil || digest != strings.ToLower(digest) {
				continue
			}
			if !info.Mode().IsRegular() || info.Size() > repairResultAnswerLimit {
				return "", fmt.Errorf("cannot verify original S9 Candidate %s: non-regular or oversized", name)
			}
			if err := ctx.Err(); err != nil {
				return "", fmt.Errorf("cannot verify original staged S9 Candidate within recovery budget: %w", err)
			}
			path := ".claude/review/repair/candidates/" + name
			raw, err := workpacket.ReadFile(root, path, repairResultAnswerLimit)
			if err != nil || workpacket.Digest(raw) != digest {
				return "", fmt.Errorf("cannot verify original S9 Candidate %s: unreadable or hash differs: %v", path, err)
			}
			var candidate WorkspaceCandidate
			if err := json.Unmarshal(raw, &candidate); err != nil {
				return "", fmt.Errorf("cannot identify original S9 Candidate %s: %w", path, err)
			}
			p := candidate.Provenance
			if p == nil || p.PacketID != packet.ID {
				continue
			}
			if err := workpacket.ValidateUniqueJSON(raw); err != nil {
				return "", fmt.Errorf("original staged S9 Candidate %s: %w", path, err)
			}
			if err := schema.NewEmbeddedValidator().ValidateBytes("repair-workspace-candidate.schema.json", raw); err != nil {
				return "", fmt.Errorf("original staged S9 Candidate %s: %w", path, err)
			}
			if err := runtimepkg.ValidateRepairResultProvenanceBinding(packet, *p); err != nil {
				return "", fmt.Errorf("original staged S9 Candidate %s: %w", path, err)
			}
			if candidate.RuntimeID != e.RuntimeID || candidate.BaselineGeneration != e.BaselineGeneration || candidate.ExecutionGeneration != e.Generation || candidate.AssignmentID != e.AssignmentID || candidate.BaseCommit != e.BaseCommit || candidate.SourceHead != p.SourceHead || p.OwnerAgentID != e.AgentID || p.ExecutionRoot != e.Path || p.ReworkRef != e.ReworkRef || candidate.Proposal.ProducerAgentID != e.AgentID || candidate.Proposal.AssignmentID != packet.Binding.Sources.RepairAssignmentID {
				return "", fmt.Errorf("original staged S9 Candidate %s differs from the current execution", path)
			}
			draft := packet.Draft
			draft.Answer = p.SubmittedAnswer
			encoded, err := json.Marshal(draft)
			if err != nil {
				return "", err
			}
			answer, problems := workpacket.ValidateRepairResultAnswer(packet, encoded)
			if len(problems) != 0 || workpacket.Digest(answer) != p.AnswerSHA256 || !json.Valid(p.SubmittedAnswer) {
				return "", fmt.Errorf("original staged S9 Candidate %s differs from its issued professional answer", path)
			}
			if err := ctx.Err(); err != nil {
				return "", fmt.Errorf("cannot verify original staged S9 Candidate within recovery budget: %w", err)
			}
			return filepath.ToSlash(path), nil
		}
		if errors.Is(readErr, io.EOF) {
			if err := ctx.Err(); err != nil {
				return "", fmt.Errorf("cannot verify original staged S9 Candidate within recovery budget: %w", err)
			}
			return "", nil
		}
	}
}
