package submission

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/review"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

const reviewResultAnswerLimit int64 = 20 << 20

// ReviewResultReceiptError reports a receipt failure after the original S7
// SubmitIssuedResult explicitly succeeded. Its coordinates identify the
// committed submission for Main's receipt recovery; they are not a verified
// receipt or proof that the round or stage has completed.
type ReviewResultReceiptError struct {
	PacketID, OwnerAgentID, AnswerPath, SourceRoot string
	Revision                                       int
	Cause                                          error
}

func (e *ReviewResultReceiptError) Error() string {
	return fmt.Sprintf("S7 packet %s, owner %s, answer %s, source %s: original submission succeeded at Runtime revision %d, but receipt verification failed: %v", e.PacketID, e.OwnerAgentID, e.AnswerPath, e.SourceRoot, e.Revision, e.Cause)
}

func (e *ReviewResultReceiptError) Unwrap() error { return e.Cause }

// IssueReviewResults is the Main-side adapter to the original ReviewPlan
// issuer. It publishes only immutable views and authoring drafts for the exact
// currently registered ReviewResult packets. A ready Worker gets the same
// packet at its assignment-relative path; the execution registry, not the
// packet, remains the authority for that source root.
func (s *Service) IssueReviewResults(ctx context.Context, ownerFilter string) ([]workpacket.ReviewResultPacket, error) {
	s = s.WithContext(ctx)
	ctx = loopruntime.WithPacketBodyRoot(s.Context(), s.Root)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshot, err := s.snapshot()
	if err != nil {
		return nil, err
	}
	packets, err := review.IssueResultPacketsContext(ctx, s.Root, s.StatePath, s.JournalPath, snapshot.Revision, ownerFilter)
	if err != nil {
		return nil, err
	}
	for _, packet := range packets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		latest, err := s.snapshot()
		if err != nil {
			return nil, err
		}
		registered, found, err := loopruntime.FindReviewResultPacketContext(ctx, latest.State, packet.ID)
		if err != nil || !found || !sameReviewResultPacket(registered, packet) {
			return nil, fmt.Errorf("S7 issued packet %s is no longer the exact Runtime packet: %v", packet.ID, err)
		}
		current, err := loopruntime.CurrentReviewResultPacketsContext(ctx, latest.State, packet.Binding.OwnerAgentID)
		if err != nil || !hasReviewResultPacket(current, packet) {
			return nil, fmt.Errorf("S7 issued packet %s no longer matches the current Plan/Assignment tuple: %v", packet.ID, err)
		}
		if err := publishReviewResultView(s.Root, packet); err != nil {
			return nil, err
		}
		executions, err := currentReviewExecutions(latest.State, packet)
		if err != nil {
			return nil, err
		}
		for _, execution := range executions {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			registry, err := workspace.Decode(latest.State)
			if err != nil || registry == nil {
				return nil, fmt.Errorf("decode registered S7 Reviewer execution: %v", err)
			}
			if err := registry.ValidateAuthority(latest.State); err != nil {
				return nil, err
			}
			if err := registry.ValidateExecution(ctx, execution, false); err != nil {
				return nil, err
			}
			if err := publishReviewResultAt(execution.Path, packet); err != nil {
				return nil, err
			}
		}
	}
	return packets, nil
}

// ReviewResultForPath resolves only a current issued answer path. It is a
// selector; the original consumer rechecks the registered packet, owner and
// execution source before the domain Writer accepts the answer.
func (s *Service) ReviewResultForPath(path string) (workpacket.ReviewResultPacket, bool, error) {
	snapshot, err := s.snapshot()
	if err != nil {
		return workpacket.ReviewResultPacket{}, false, err
	}
	packet, _, found, err := currentReviewResultSourceForPathContext(s.Context(), s.Root, snapshot.State, path)
	return packet, found, err
}

// ReviewResultSourceForPath also returns the exact current Main or ready
// Worker root containing the authoring bytes. The registered execution and
// original Result consumer own source binding; no Hook identity callback is
// required to save the professional answer.
func (s *Service) ReviewResultSourceForPath(path string) (workpacket.ReviewResultPacket, string, bool, error) {
	snapshot, err := s.snapshot()
	if err != nil {
		return workpacket.ReviewResultPacket{}, "", false, err
	}
	return currentReviewResultSourceForPathContext(s.Context(), s.Root, snapshot.State, path)
}

// SaveReviewResult accepts the Main-root file through the same original S7
// consumer. The CLI's --agent value supplies no authority by itself; the
// original consumer checks the persisted owner, packet and approval mode.
func (s *Service) SaveReviewResult(ctx context.Context, owner, path string) (Receipt, error) {
	s = s.WithContext(ctx)
	ctx = loopruntime.WithPacketBodyRoot(s.Context(), s.Root)
	return s.SaveReviewResultFrom(ctx, owner, path, s.Root, nil)
}

// SaveReviewResultFrom accepts the exact registered Main/Worker source. The
// Runtime packet and original consumer bind owner, Assignment and source root.
// authorize remains an optional compatibility callback for older callers;
// ordinary saves do not require Hook actor/session authorization.
func (s *Service) SaveReviewResultFrom(ctx context.Context, owner, path, sourceRoot string, authorize func(root string, state map[string]any) error) (Receipt, error) {
	s = s.WithContext(ctx)
	ctx = loopruntime.WithPacketBodyRoot(s.Context(), s.Root)
	snapshot, err := s.snapshot()
	if err != nil {
		return Receipt{}, err
	}
	if err := ctx.Err(); err != nil {
		return rejected(snapshot, "", err), err
	}
	packet, actualRoot, found, err := currentReviewResultSourceForPathContext(ctx, s.Root, snapshot.State, path)
	if err != nil {
		return rejected(snapshot, "", err), err
	}
	if !found || packet.Binding.OwnerAgentID != owner || owner == "" {
		err := errors.New("S7 answer path has no current issuance for this trusted Reviewer")
		return rejected(snapshot, "", err), err
	}
	if sourceRoot != "" {
		want, err := filepath.Abs(sourceRoot)
		if err != nil {
			return rejected(snapshot, packet.ID, err), err
		}
		got, err := filepath.Abs(actualRoot)
		if err != nil || filepath.Clean(want) != filepath.Clean(got) {
			err := errors.New("S7 answer source root differs from its current registered path")
			return rejected(snapshot, packet.ID, err), err
		}
	}
	input, issues, err := review.ReadIssuedResultInput(ctx, actualRoot, packet, owner)
	if err != nil {
		return rejected(snapshot, packet.ID, err), err
	}
	if len(issues) != 0 {
		err := fmt.Errorf("S7 answer %s: %s (%s)", issues[0].Pointer, issues[0].Expected, issues[0].Observed)
		r := rejected(snapshot, packet.ID, err)
		r.FieldErrors = issues
		return r, err
	}
	if err := ctx.Err(); err != nil {
		return rejected(snapshot, packet.ID, err), err
	}
	next, submitErr := review.SubmitIssuedResult(s.Root, s.StatePath, s.JournalPath, review.IssuedSubmitRequest{
		Context:          ctx,
		ExpectedRevision: snapshot.Revision,
		PacketID:         packet.ID,
		AnswerPath:       packet.AnswerPath,
		OwnerAgentID:     owner,
		SourceRoot:       actualRoot,
		AuthorizeSource:  authorize,
		Input:            input,
		OccurredAt:       time.Now().UTC(),
	})
	if submitErr != nil {
		if errors.Is(submitErr, loopruntime.ErrStaleRevision) {
			submitErr = fmt.Errorf("S7 packet %s, owner %s, answer %s: the original receiver could not settle concurrent Runtime changes within this attempt; retain the original delivery and any accepted receipt. This is a framework reception gap, not a request for answer correction, another native dispatch or repeated submission: %w", packet.ID, owner, packet.AnswerPath, submitErr)
			r := rejected(snapshot, packet.ID, submitErr)
			r.NextAction = "原接收器的有限并发续跑仍未确认接收；保留原交付与已有回执，按具名责任处理系统接收缺口。此反馈不要求作者更改专业内容、重存或再次派发。"
			return r, submitErr
		}
		return rejected(snapshot, packet.ID, submitErr), submitErr
	}
	completionCtx, cancel := review.ResultCompletionContext(ctx)
	defer cancel()
	return submittedReviewResultReceipt(s.Root, actualRoot, next, packet, owner, completionCtx)
}

// Only the successful SubmitIssuedResult branch may call this helper. Receipt
// lookup on its own cannot establish that this attempt committed a submission.
func submittedReviewResultReceipt(root, sourceRoot string, snapshot loopruntime.Snapshot, packet workpacket.ReviewResultPacket, owner string, ctx context.Context) (Receipt, error) {
	receipt, err := issuedReviewResultReceipt(root, snapshot, packet, owner, ctx)
	if err == nil && receipt.Accepted {
		return receipt, nil
	}
	if err == nil {
		err = errors.New("original ReviewResult consumer did not record an accepted receipt")
	}
	gap := &ReviewResultReceiptError{PacketID: packet.ID, OwnerAgentID: owner, AnswerPath: packet.AnswerPath, SourceRoot: sourceRoot, Revision: snapshot.Revision, Cause: err}
	receipt = emptyReceipt(snapshot, packet.ID)
	receipt.Error = gap.Error()
	receipt.NextAction = "原 S7 提交已成功，但回执校验未完成；Main 沿准确 packet、owner 和原来源复核原回执及出口，不要求作者改稿、重存或重派。"
	return receipt, gap
}

// ReviewResultReceipt rebuilds a receipt from the original accepted S7
// Assignment receipt and immutable Result evidence. ref is an exact issued
// answer path, a path under its registered execution, or a packet ID.
func (s *Service) ReviewResultReceipt(owner, ref string) (Receipt, error) {
	snapshot, err := s.snapshot()
	if err != nil {
		return Receipt{}, err
	}
	packet, found, err := reviewResultByReferenceContext(s.Context(), s.Root, snapshot.State, ref)
	if err != nil {
		return Receipt{}, err
	}
	if !found || owner == "" || packet.Binding.OwnerAgentID != owner {
		return rejected(snapshot, "", errors.New("S7 packet reference has no matching registered Reviewer")), nil
	}
	resultReceipt, accepted, err := review.IssuedResultReceiptContext(s.Context(), s.Root, snapshot.State, packet.ID, owner)
	if err != nil {
		return Receipt{}, err
	}
	if !accepted {
		return rejected(snapshot, packet.ID, errors.New("ReviewResult has not been accepted")), nil
	}
	return acceptedReviewResultReceipt(snapshot, packet, resultReceipt), nil
}

func currentReviewResultForPath(root string, state map[string]any, path string) (workpacket.ReviewResultPacket, bool, error) {
	packet, _, found, err := currentReviewResultSourceForPath(root, state, path)
	return packet, found, err
}

func currentReviewResultSourceForPath(root string, state map[string]any, path string) (workpacket.ReviewResultPacket, string, bool, error) {
	return currentReviewResultSourceForPathContext(context.Background(), root, state, path)
}

func currentReviewResultSourceForPathContext(ctx context.Context, root string, state map[string]any, path string) (workpacket.ReviewResultPacket, string, bool, error) {
	if err := ctx.Err(); err != nil {
		return workpacket.ReviewResultPacket{}, "", false, err
	}
	index, err := loopruntime.ProjectCurrentReviewResultPacketIndex(state, "")
	if err != nil {
		return workpacket.ReviewResultPacket{}, "", false, err
	}
	if err := ctx.Err(); err != nil {
		return workpacket.ReviewResultPacket{}, "", false, err
	}
	// Index coordinates select only exact path candidates. They neither stand
	// in for the canonical packet nor grant permission to consume an answer.
	clean := filepath.ToSlash(filepath.Clean(path))
	var candidates []workpacket.ReviewResultPacket
	for _, entry := range index {
		if clean == entry.AnswerPath || strings.HasSuffix(clean, "/"+entry.AnswerPath) {
			candidates = append(candidates, workpacket.ReviewResultPacket{ID: entry.ID, Binding: entry.Binding, ViewPath: entry.ViewPath, AnswerPath: entry.AnswerPath})
		}
	}
	selected, _, found, err := reviewResultPathInRoots(root, state, path, candidates, true)
	if err != nil || !found {
		return workpacket.ReviewResultPacket{}, "", false, err
	}
	packet, found, err := loopruntime.FindReviewResultPacketContext(loopruntime.WithPacketBodyRoot(ctx, root), state, selected.ID)
	if err != nil {
		return workpacket.ReviewResultPacket{}, "", false, err
	}
	if !found {
		return workpacket.ReviewResultPacket{}, "", false, fmt.Errorf("selected S7 packet %s is unavailable", selected.ID)
	}
	return reviewResultPathInRoots(root, state, path, []workpacket.ReviewResultPacket{packet}, true)
}

func reviewResultByReference(root string, state map[string]any, ref string) (workpacket.ReviewResultPacket, bool, error) {
	return reviewResultByReferenceContext(context.Background(), root, state, ref)
}

func reviewResultByReferenceContext(ctx context.Context, root string, state map[string]any, ref string) (workpacket.ReviewResultPacket, bool, error) {
	ctx = loopruntime.WithPacketBodyRoot(ctx, root)
	if packet, found, err := loopruntime.FindReviewResultPacketContext(ctx, state, ref); err != nil || found {
		return packet, found, err
	}
	// The initial lookup checked the complete lightweight namespace. Read
	// bodies only for exact answer-path candidates; source authority and
	// ambiguity are still checked by the original path resolver below.
	clean := filepath.ToSlash(filepath.Clean(ref))
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	var packets []workpacket.ReviewResultPacket
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["template_id"] != "s7-review-result-v1" {
			continue
		}
		answer, _ := row["answer_path"].(string)
		if answer == "" || clean != answer && !strings.HasSuffix(clean, "/"+answer) {
			continue
		}
		id, _ := row["packet_id"].(string)
		p, found, err := loopruntime.FindReviewResultPacketContext(ctx, state, id)
		if err != nil {
			return workpacket.ReviewResultPacket{}, false, err
		}
		if found {
			packets = append(packets, p)
		}
	}
	packet, _, found, err := reviewResultPathInRoots(root, state, ref, packets, false)
	return packet, found, err
}

func reviewResultPathInRoots(root string, state map[string]any, inputPath string, packets []workpacket.ReviewResultPacket, currentOnly bool) (workpacket.ReviewResultPacket, string, bool, error) {
	abs, err := filepath.Abs(inputPath)
	if err != nil {
		return workpacket.ReviewResultPacket{}, "", false, err
	}
	if !filepath.IsAbs(inputPath) {
		abs, err = filepath.Abs(filepath.Join(root, inputPath))
		if err != nil {
			return workpacket.ReviewResultPacket{}, "", false, err
		}
	}
	registry, err := workspace.Decode(state)
	if err != nil {
		return workpacket.ReviewResultPacket{}, "", false, err
	}
	var selected *workpacket.ReviewResultPacket
	selectedRoot := ""
	for _, packet := range packets {
		roots := []string{root}
		if registry != nil {
			if err := registry.ValidateAuthority(state); err != nil {
				return workpacket.ReviewResultPacket{}, "", false, err
			}
			if execution, found := registry.Execution(packet.Binding.AssignmentID, packet.Binding.RuntimeID, packet.Binding.BaselineGeneration); found {
				if execution.AgentID != packet.Binding.OwnerAgentID || execution.Status != "ready" || (currentOnly && !reviewExecutionMatchesState(state, execution)) {
					return workpacket.ReviewResultPacket{}, "", false, errors.New("S7 packet's registered execution owner or status differs")
				}
				roots = []string{execution.Path}
			}
		}
		for _, candidateRoot := range roots {
			rootAbs, err := filepath.Abs(candidateRoot)
			if err != nil {
				return workpacket.ReviewResultPacket{}, "", false, err
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
				return workpacket.ReviewResultPacket{}, "", false, errors.New("S7 answer path is ambiguous across registered sources")
			}
			copyPacket := packet
			selected = &copyPacket
			selectedRoot = rootAbs
		}
	}
	if selected == nil {
		return workpacket.ReviewResultPacket{}, "", false, nil
	}
	return *selected, selectedRoot, true, nil
}

func reviewExecutionMatchesState(state map[string]any, execution workspace.Execution) bool {
	baseline, _ := state["baseline"].(map[string]any)
	return execution.Status == "ready" && execution.RuntimeID == text(state["runtime_id"]) &&
		execution.BaselineGeneration == number(baseline["generation"])
}

func currentReviewExecutions(state map[string]any, packet workpacket.ReviewResultPacket) ([]workspace.Execution, error) {
	registry, err := workspace.Decode(state)
	if err != nil {
		return nil, err
	}
	if registry == nil {
		return nil, nil
	}
	result := []workspace.Execution{}
	for _, execution := range registry.AllExecutions() {
		if reviewExecutionMatchesState(state, execution) && execution.AgentID == packet.Binding.OwnerAgentID &&
			execution.AssignmentID == packet.Binding.AssignmentID {
			result = append(result, execution)
		}
	}
	if len(result) > 1 {
		return nil, fmt.Errorf("S7 Reviewer has multiple ready execution roots for one current Assignment")
	}
	return result, nil
}

func publishReviewResultView(root string, packet workpacket.ReviewResultPacket) error {
	data, err := json.MarshalIndent(packet, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	stored, err := workpacket.PublishBounded(root, packet.ViewPath, data, reviewResultAnswerLimit)
	if err != nil {
		return err
	}
	if !bytesEqual(stored, data) {
		return fmt.Errorf("S7 issued view differs from the Runtime packet: %s", packet.ViewPath)
	}
	return nil
}

func publishReviewResultAt(root string, packet workpacket.ReviewResultPacket) error {
	if err := publishReviewResultView(root, packet); err != nil {
		return err
	}
	draft, err := json.MarshalIndent(packet.Draft, "", "  ")
	if err != nil {
		return err
	}
	_, err = workpacket.PublishBounded(root, packet.AnswerPath, append(draft, '\n'), reviewResultAnswerLimit)
	return err
}

func sameReviewResultPacket(a, b workpacket.ReviewResultPacket) bool {
	left, err := workpacket.ReviewResultPacketDigest(a)
	if err != nil {
		return false
	}
	right, err := workpacket.ReviewResultPacketDigest(b)
	return err == nil && left == right
}

func hasReviewResultPacket(packets []workpacket.ReviewResultPacket, expected workpacket.ReviewResultPacket) bool {
	for _, packet := range packets {
		if packet.ID == expected.ID && sameReviewResultPacket(packet, expected) {
			return true
		}
	}
	return false
}

func issuedReviewResultReceipt(root string, snapshot loopruntime.Snapshot, packet workpacket.ReviewResultPacket, owner string, contexts ...context.Context) (Receipt, error) {
	ctx := context.Background()
	if len(contexts) > 0 && contexts[0] != nil {
		ctx = contexts[0]
	}
	resultReceipt, accepted, err := review.IssuedResultReceiptContext(ctx, root, snapshot.State, packet.ID, owner)
	if err != nil {
		return Receipt{}, err
	}
	if !accepted {
		return rejected(snapshot, packet.ID, errors.New("original ReviewResult consumer did not record an accepted receipt")), nil
	}
	return acceptedReviewResultReceipt(snapshot, packet, resultReceipt), nil
}

func acceptedReviewResultReceipt(snapshot loopruntime.Snapshot, packet workpacket.ReviewResultPacket, accepted review.ResultReceipt) Receipt {
	result := emptyReceipt(snapshot, packet.ID)
	result.SubmissionKey = accepted.SubmissionKey
	result.Accepted = true
	result.ResponsibilityStatus = "accepted"
	result.WorkComplete = true
	result.DomainRefs = []string{accepted.ResultRef}
	result.WaitingOn = []Waiting{}
	result.NextAction = "Canonical ReviewResult 已由原 S7 接收器登记；本人的 Review Assignment 已交付。"
	return result
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
