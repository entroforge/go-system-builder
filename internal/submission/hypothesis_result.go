package submission

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/investigation"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// IssueHypothesisResults delegates issuance to the original Case/Runtime
// issuer. The packet registry and original PACKET-ISSUE event remain the only
// durable issuance authority; the service adds no submissions or result rows.
func (s *Service) IssueHypothesisResults(ctx context.Context, ownerFilter string) ([]workpacket.HypothesisResultPacket, error) {
	s = s.WithContext(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshot, err := s.snapshot()
	if err != nil {
		return nil, err
	}
	packets, err := investigation.IssueHypothesisResultPacketsContext(ctx, s.Root, s.StatePath, s.JournalPath, snapshot.Revision, ownerFilter)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return packets, nil
}

// HypothesisResultForPath resolves a current issued S8 path. It is only a
// selector; the original Case consumer rechecks the registered packet, owner,
// dispatch and source tuple under its CAS.
func (s *Service) HypothesisResultForPath(path string) (workpacket.HypothesisResultPacket, bool, error) {
	snapshot, err := s.snapshot()
	if err != nil {
		return workpacket.HypothesisResultPacket{}, false, err
	}
	p, _, found, err := currentHypothesisResultSourceForPath(s.Root, snapshot.State, path)
	return p, found, err
}

// HypothesisResultSourceForPath returns the exact Main or registered Worker
// checkout that owns an issued answer file. It grants no author identity.
func (s *Service) HypothesisResultSourceForPath(path string) (workpacket.HypothesisResultPacket, string, bool, error) {
	snapshot, err := s.snapshot()
	if err != nil {
		return workpacket.HypothesisResultPacket{}, "", false, err
	}
	return currentHypothesisResultSourceForPath(s.Root, snapshot.State, path)
}

// SaveHypothesisResultFrom accepts a bounded S8 answer through the original
// InvestigationCase writer. The original packet and registered execution
// bind the owner and source; authorize is an optional compatibility callback,
// not a normal Hook identity/session requirement.
func (s *Service) SaveHypothesisResultFrom(ctx context.Context, owner, path, sourceRoot string, authorize func(root string, state map[string]any) error) (Receipt, error) {
	return s.ObserveHypothesisResultFrom(ctx, owner, path, sourceRoot, authorize, true)
}

// ObserveHypothesisResultFrom receives an original active delivery or reuses
// its accepted fact. It does not add an author signal or persist a cache.
func (s *Service) ObserveHypothesisResultFrom(ctx context.Context, owner, path, sourceRoot string, authorize func(root string, state map[string]any) error, continueUnresolved bool) (Receipt, error) {
	s = s.WithContext(ctx)
	snapshot, err := s.snapshot()
	if err != nil {
		return Receipt{}, err
	}
	if err := ctx.Err(); err != nil {
		return rejected(snapshot, "", err), err
	}
	packet, actualRoot, found, err := currentHypothesisResultSourceForPath(s.Root, snapshot.State, path)
	if err != nil {
		return rejected(snapshot, "", err), err
	}
	if !found || owner == "" || packet.Binding.OwnerAgentID != owner {
		err := errors.New("S8 answer path has no current issuance for this trusted Investigator")
		return rejected(snapshot, "", err), err
	}
	if sourceRoot != "" {
		want, err := filepath.Abs(sourceRoot)
		if err != nil {
			return rejected(snapshot, packet.ID, err), err
		}
		got, err := filepath.Abs(actualRoot)
		if err != nil || filepath.Clean(want) != filepath.Clean(got) {
			err := errors.New("S8 answer source root differs from its current registered path")
			return rejected(snapshot, packet.ID, err), err
		}
	}
	if err := ctx.Err(); err != nil {
		return rejected(snapshot, packet.ID, err), err
	}
	next, accepted, submitErr := investigation.ReceiveIssuedHypothesisResult(s.Root, s.StatePath, s.JournalPath, investigation.IssuedHypothesisResultRequest{
		Context:          ctx,
		ExpectedRevision: -1,
		PacketID:         packet.ID,
		AnswerPath:       packet.AnswerPath,
		OwnerAgentID:     owner,
		SourceRoot:       actualRoot,
		AuthorizeSource:  authorize,
		OccurredAt:       time.Now().UTC(),
	}, continueUnresolved)
	if submitErr != nil {
		return rejected(snapshot, packet.ID, submitErr), submitErr
	}
	return acceptedHypothesisResultReceipt(next, packet, accepted)
}

// HypothesisResultReceipt reconstructs an accepted receipt from the original
// Case pointer/history and journal proof. It never reads the authoring file.
// ref may be an issued packet ID or its exact registered source path.
func (s *Service) HypothesisResultReceipt(owner, ref string) (Receipt, error) {
	snapshot, err := s.snapshot()
	if err != nil {
		return Receipt{}, err
	}
	packet, found, err := hypothesisResultByReference(s.Root, snapshot.State, ref)
	if err != nil {
		return Receipt{}, err
	}
	if !found || owner == "" || packet.Binding.OwnerAgentID != owner {
		return rejected(snapshot, "", errors.New("S8 packet reference has no matching registered Investigator")), nil
	}
	accepted, ok, err := investigation.IssuedHypothesisResultReceipt(s.Root, snapshot.State, packet.ID, owner)
	if err != nil {
		return Receipt{}, err
	}
	if !ok {
		result := rejected(snapshot, packet.ID, errors.New("HypothesisResult has not been accepted"))
		result.NextAction = "Main 查看本次原生返回和 Stop 的具名作者、准确答案路径及具体字段/来源诊断；恢复缺失来源或判别前提，必要时续跑原 Investigator 补充本人答案。不能把已结束但未接收的责任当作仍在后台运行，也不能代填结论或仅凭返回宣称完成。"
		return result, nil
	}
	return acceptedHypothesisResultReceipt(snapshot, packet, accepted)
}

func currentHypothesisResultSourceForPath(root string, state map[string]any, inputPath string) (workpacket.HypothesisResultPacket, string, bool, error) {
	packets, err := loopruntime.CurrentHypothesisResultPackets(root, state, "")
	if err != nil {
		return workpacket.HypothesisResultPacket{}, "", false, err
	}
	return hypothesisResultPathInRoots(root, state, inputPath, packets)
}

func hypothesisResultByReference(root string, state map[string]any, ref string) (workpacket.HypothesisResultPacket, bool, error) {
	if packet, found, err := loopruntime.FindHypothesisResultPacket(state, ref); err != nil || found {
		return packet, found, err
	}
	packets, err := loopruntime.HypothesisResultPackets(state)
	if err != nil {
		return workpacket.HypothesisResultPacket{}, false, err
	}
	packet, _, found, err := hypothesisResultPathInRoots(root, state, ref, packets)
	return packet, found, err
}

func hypothesisResultPathInRoots(root string, state map[string]any, inputPath string, packets []workpacket.HypothesisResultPacket) (workpacket.HypothesisResultPacket, string, bool, error) {
	abs := inputPath
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, filepath.FromSlash(inputPath))
	}
	abs, err := filepath.Abs(abs)
	if err != nil {
		return workpacket.HypothesisResultPacket{}, "", false, err
	}
	registry, err := workspace.Decode(state)
	if err != nil {
		return workpacket.HypothesisResultPacket{}, "", false, err
	}
	if registry != nil {
		if err := registry.ValidateAuthority(state); err != nil {
			return workpacket.HypothesisResultPacket{}, "", false, err
		}
	}
	var selected *workpacket.HypothesisResultPacket
	selectedRoot := ""
	for _, packet := range packets {
		candidateRoot := root
		if registry != nil {
			if execution, declared := registry.Executions[packet.Binding.AssignmentID]; declared {
				if execution.AssignmentID != packet.Binding.AssignmentID || execution.RuntimeID != packet.Binding.RuntimeID ||
					execution.BaselineGeneration != packet.Binding.BaselineGeneration || execution.AgentID != packet.Binding.OwnerAgentID || execution.Status != "ready" {
					return workpacket.HypothesisResultPacket{}, "", false, errors.New("S8 packet registered execution owner, generation or status differs")
				}
				candidateRoot = execution.Path
			}
		}
		rootAbs, err := filepath.Abs(candidateRoot)
		if err != nil {
			return workpacket.HypothesisResultPacket{}, "", false, err
		}
		rel, err := filepath.Rel(rootAbs, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		rel = filepath.ToSlash(filepath.Clean(rel))
		if rel != packet.AnswerPath || workpacket.CheckPath(rootAbs, rel) != nil {
			continue
		}
		if selected != nil && (selected.ID != packet.ID || filepath.Clean(selectedRoot) != filepath.Clean(rootAbs)) {
			return workpacket.HypothesisResultPacket{}, "", false, errors.New("S8 answer path is ambiguous across registered sources")
		}
		copyPacket := packet
		selected = &copyPacket
		selectedRoot = rootAbs
	}
	if selected == nil {
		return workpacket.HypothesisResultPacket{}, "", false, nil
	}
	return *selected, selectedRoot, true, nil
}

func sameHypothesisResultPacket(a, b workpacket.HypothesisResultPacket) bool {
	return a.ID != "" && a.ID == b.ID && a.Binding.RuntimeID == b.Binding.RuntimeID &&
		a.Binding.CaseID == b.Binding.CaseID && a.Binding.AssignmentID == b.Binding.AssignmentID &&
		a.Binding.OwnerAgentID == b.Binding.OwnerAgentID
}

func containsHypothesisResultPacket(values []workpacket.HypothesisResultPacket, expected workpacket.HypothesisResultPacket) bool {
	for _, packet := range values {
		if packet.ID == expected.ID && sameHypothesisResultPacket(packet, expected) {
			return true
		}
	}
	return false
}

func acceptedHypothesisResultReceipt(snapshot loopruntime.Snapshot, packet workpacket.HypothesisResultPacket, accepted loopruntime.HypothesisResultReceipt) (Receipt, error) {
	result := emptyReceipt(snapshot, packet.ID)
	if accepted.PacketID != packet.ID || accepted.OwnerAgentID != packet.Binding.OwnerAgentID || accepted.SuccessorCaseRef == "" {
		return result, errors.New("original HypothesisResult receipt differs from its issued packet")
	}
	result.SubmissionKey = accepted.AcceptedIdempotencyKey
	result.Accepted = true
	result.ResponsibilityStatus = "accepted"
	// This receipt completes this delivery, not the investigation or the
	// Agent/Task lifecycle. An inconclusive finding does not undo that work.
	result.WorkComplete = true
	result.DomainRefs = []string{accepted.SuccessorCaseRef}
	result.WaitingOn = []Waiting{}
	result.NextAction = "原 HypothesisResult 已接收，本次原答卷交付完成；结论不替代本人进度或 Case 完成。Main 判断未知及下一责任；实际未完成时可沿原责任自然继续，不要求重交旧答案。"
	return result, nil
}
