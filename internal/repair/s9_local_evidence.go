package repair

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/evidence"
	runtimepkg "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// captureS9LocalEvidence preserves the author payload and binds actual Worker
// bytes separately. Bare identifiers remain opaque legacy references. Explicit
// path:, absolute paths and slash-containing relative paths name local files.
func captureS9LocalEvidence(mainRoot, workerRoot, packetID string, raw json.RawMessage) ([]runtimepkg.RepairResultLocalEvidence, error) {
	var answer workpacket.RepairResultProfessionalAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		return nil, err
	}
	refs := append([]string{}, answer.EvidenceRefs...)
	for _, unit := range answer.UnitResults {
		refs = append(refs, unit.EvidenceRefs...)
	}
	for _, result := range answer.BeforeFixResults {
		refs = append(refs, result.EvidenceRefs...)
	}
	seen := map[string]bool{}
	var captured []runtimepkg.RepairResultLocalEvidence
	for _, ref := range refs {
		if seen[ref] {
			continue
		}
		seen[ref] = true
		parsed, _ := url.Parse(ref)
		if (!strings.HasPrefix(ref, "path:") && parsed != nil && parsed.Scheme != "") || (!strings.HasPrefix(ref, "path:") && !filepath.IsAbs(ref) && !strings.Contains(ref, "/")) {
			continue
		}
		source, want, err := evidence.ParsePathReference(ref)
		if err != nil {
			return nil, fmt.Errorf("S9 local evidence source gap for %q: %w", ref, err)
		}
		if filepath.IsAbs(source) {
			source, err = filepath.Rel(workerRoot, source)
		}
		if err == nil {
			source, _, err = evidence.LocalPathReference("path:" + source)
		}
		if err != nil {
			return nil, fmt.Errorf("S9 local evidence source gap for %q in Builder root %s: %w", ref, workerRoot, err)
		}
		if err := evidence.ValidateLocalCaptureSource(source); err != nil {
			return nil, err
		}
		data, err := workpacket.ReadFile(workerRoot, source, repairResultAnswerLimit)
		if err != nil {
			return nil, fmt.Errorf("S9 local evidence source gap for %q in Builder root %s: %w; restore the original source and retry reception", ref, workerRoot, err)
		}
		digest := workpacket.Digest(data)
		if want != "" && want != digest {
			return nil, fmt.Errorf("S9 local evidence source gap for %q: source hash differs from actual Builder bytes", ref)
		}
		target := ".claude/evidence/s9/" + packetID + "/" + workpacket.Digest([]byte(source)) + "/" + digest
		saved, err := workpacket.PublishBounded(mainRoot, target, data, repairResultAnswerLimit)
		if err != nil {
			return nil, fmt.Errorf("S9 local evidence handoff gap for %q: %w", ref, err)
		}
		if !bytes.Equal(saved, data) {
			return nil, fmt.Errorf("S9 local evidence handoff gap for %q: immutable captured bytes differ", ref)
		}
		captured = append(captured, runtimepkg.RepairResultLocalEvidence{AuthoredRef: ref, SourcePath: source, Path: target, SHA256: digest})
	}
	return captured, nil
}

// verifyS9CapturedEvidence checks the immutable Main handoff before Final; it
// never reopens a retired Worker and is not run by reminder projections.
func verifyS9CapturedEvidence(mainRoot string, provenance runtimepkg.RepairResultCandidateProvenance) error {
	for _, capture := range provenance.LocalEvidence {
		data, err := workpacket.ReadFile(mainRoot, capture.Path, repairResultAnswerLimit)
		if err != nil || workpacket.Digest(data) != capture.SHA256 {
			return fmt.Errorf("S9 local evidence handoff gap for %q from Builder root %s: restore original capture %s before Final", capture.AuthoredRef, provenance.ExecutionRoot, capture.Path)
		}
	}
	return nil
}
