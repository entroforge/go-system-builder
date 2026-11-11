package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/repair"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

// nativeS9AcceptedPlanParseFailure carries the accepted PlanReport receipt and
// the unfinished Result's concrete parse reason in one error.
//
// The two coordinates stay the preserved fact. The parse reason is the
// original consumer's own diagnostic and travels with them, so the reader
// sees which actual file failed and why instead of a bare coordinate
// (L3-S9-repair.md:238 "反馈实际文件与字段"; :255 "沿原准备恢复必要引导").
type nativeS9AcceptedPlanParseFailure struct {
	partial *nativeS9PlanAcceptedIncomplete
	author  error
}

// Error reports the original consumer's own concrete cause. The accepted
// coordinates stay in the preserved PlanReport error, which the handoff
// renders next to this diagnostic.
func (e *nativeS9AcceptedPlanParseFailure) Error() string { return e.author.Error() }

// Unwrap exposes the accepted-plan error, so every existing handoff branch
// keeps reporting the preserved PlanReport receipt and both coordinates, and
// the original author failure keeps its own classification.
func (e *nativeS9AcceptedPlanParseFailure) Unwrap() error { return e.partial }

func receiveNativeS9OriginalBuilder(ctx context.Context, root string, state map[string]any, p workpacket.RepairPlanReportPacket, source string, diagnostic io.Writer, capture *nativeS9AttemptCapture) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	source, err := workspace.CanonicalProspective(source)
	if err != nil {
		return err
	}
	if source == "" || samePath(source, root) {
		return fmt.Errorf("S9 actual Worker authoring location is missing; Main must locate this original responsibility")
	}
	service := (&submission.Service{Root: root, StatePath: filepath.Join(root, ".claude/loop-state.json"), JournalPath: filepath.Join(root, ".claude/loop-events.jsonl"), Validator: semantic.RuntimeCandidateValidator{}}).WithContext(ctx)
	registry, err := workspace.Decode(state)
	if err != nil {
		return err
	}
	if registry != nil {
		if e, ok := registry.Executions[p.Binding.ManifestAssignmentID]; ok && e.DeliveryRef != "" {
			// The original Result is already accepted. The accepted Final and
			// its original ACK/cleanup are preserved. A live authoring location
			// is compared with the accepted object in the original consumer's
			// professional terms: an equal original is idempotent, a changed
			// original is a conflict that leaves the accepted facts untouched.
			return compareNativeS9AcceptedOriginal(ctx, root, state, p, e, source, service, diagnostic)
		}
	}
	plan, result, err := nativeS9AuthorPaths(root, p)
	if err != nil {
		return err
	}
	stop, _ := ctx.Value(nativeS9PlanStopKey{}).(bool)
	// A draft remains quiet during ordinary work. A real return must preserve
	// the selected responsibility and say who continues its unfinished work.
	pendingResult := func() error {
		if !stop {
			return nativeS9ProgressPending
		}
		return &nativeS9PlanAcceptedIncomplete{
			error: nativeS9ProgressPending,
			plan:  filepath.Join(source, plan), result: filepath.Join(source, result),
		}
	}
	// A genuine blocked result can be delivered even if the independent Plan
	// has not been written. Read this original once and reuse it for reception.
	raw, resultErr := workpacket.ReadFile(source, result, 20<<20)
	var verdict struct {
		Verdict string `json:"verdict"`
	}
	if resultErr == nil && json.Valid(raw) {
		_ = json.Unmarshal(raw, &verdict)
	}
	blocked := verdict.Verdict == "blocked" || verdict.Verdict == "contract_conflict"
	// Plan is its own contribution. Receiving it creates neither a Result nor
	// a product execution, and the Worker continues the original authorization.
	if err := receiveNativeS9ReadyPlan(ctx, root, source, p, service, diagnostic, capture); err != nil && !blocked {
		if stop && err == nativeS9ProgressPending {
			return fmt.Errorf("original PlanReport is missing or unfinished at %s for %s; Result is not received at %s. Main owns continuation of this original responsibility; no repair completion is established", filepath.Join(source, plan), p.Binding.OwnerAgentID, filepath.Join(source, result))
		}
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if os.IsNotExist(resultErr) {
		return pendingResult()
	}
	if resultErr != nil {
		return resultErr
	}
	if !json.Valid(raw) {
		// The original file exists but its bytes are not a valid original
		// document. Report the original validator's own parse problem against
		// the actual file instead of treating it as an unfinished submission.
		parseErr := workpacket.ValidateUniqueJSON(raw)
		if parseErr == nil {
			parseErr = fmt.Errorf("the original Result at %s is not a complete original document", filepath.Join(source, result))
		}
		// The concrete parse problem travels with the unfinished Result as the
		// original consumer's own diagnostic. The consumer diagnostic names the
		// actual file and the actual parse reason, so the reader learns which
		// material failed and why instead of seeing the coordinates alone
		// (L3-S9-repair.md:238 "反馈实际文件与字段"; :255 "沿原准备恢复必要引导").
		authorErr := nativeS9ArtifactAuthorError("Result", filepath.Join(source, result), parseErr)
		// An already accepted PlanReport receipt is a preserved fact. The
		// unfinished Result does not erase it, so the original responsibility
		// keeps both coordinates and the consumer's own parse diagnostic.
		if receipt, receiptErr := service.RepairPlanReportReceipt(p.Binding.OwnerAgentID, p.ID); receiptErr == nil && receipt.Accepted {
			return &nativeS9AcceptedPlanParseFailure{
				partial: &nativeS9PlanAcceptedIncomplete{
					error:  authorErr,
					plan:   filepath.Join(source, plan),
					result: filepath.Join(source, result),
				},
				author: authorErr,
			}
		}
		return authorErr
	}
	if err := json.Unmarshal(raw, &verdict); err != nil {
		return nativeS9ArtifactAuthorError("Result", filepath.Join(source, result), err)
	}
	if strings.TrimSpace(verdict.Verdict) == "" {
		return pendingResult()
	}
	if capture != nil {
		capture.ObserveInput(filepath.Join(source, result), raw)
		capture.SetInput(filepath.Join(source, result))
		capture.EnterConsumer("original_result_consumer")
	}
	// Only author-owned professional fields belong in this private file. The
	// canonical consumer stays lenient about unrelated extensions, so this
	// entry point keeps the strictness the original flat entry owned.
	var resultAnswer workpacket.RepairResultProfessionalAnswer
	if err := nativeS9FlatUnknownAuthorField(raw, &resultAnswer); err != nil {
		return nativeS9ArtifactAuthorError("Result", filepath.Join(source, result), err)
	}
	if err := recordNativeS9OriginalLocation(ctx, root, p, source); err != nil {
		return err
	}
	packets, err := repair.IssueRepairResultPackets(ctx, root, service.StatePath, service.JournalPath, -1, p.Binding.OwnerAgentID)
	if err != nil {
		return err
	}
	for _, packet := range packets {
		if packet.Binding.Sources.RepairAssignmentID != p.Binding.RepairAssignmentID {
			continue
		}
		receipt, err := service.SaveRepairResultBytes(ctx, p.Binding.OwnerAgentID, packet.ID, source, raw)
		if err != nil {
			return nativeS9FlatArtifactError("Result", filepath.Join(source, result), err)
		}
		if !receipt.Accepted {
			return fmt.Errorf("original S9 Result remains unreceived: %s", receipt.Error)
		}
		if capture != nil {
			capture.ConsumerSucceeded()
			capture.SetStage("result_received")
		}
		if blocked {
			fmt.Fprintln(diagnostic, "Original S9 blocked Result received. "+receipt.NextAction)
			nativeS9WorkerMessage(ctx, "Your original S9 blocked Result is received. Main will handle the reported blocker; this does not claim repair or integration completion.")
			return nil
		}
		// Preparing Main's original question does not record Main completion.
		if current, snapshotErr := runtime.NewStore(service.StatePath, service.JournalPath).Snapshot(); snapshotErr != nil {
			fmt.Fprintln(diagnostic, "Original Result remains received; Main Impact preparation: "+snapshotErr.Error())
		} else if path, prepareErr := prepareRepairImpactQuestionnaire(root, current.State); prepareErr != nil {
			fmt.Fprintln(diagnostic, "Original Result remains received; Main Impact preparation: "+prepareErr.Error())
		} else {
			fmt.Fprintln(diagnostic, "Main original ChangeImpact answer: "+path+". Main performs actual integration/checks and saves its original impact judgment; reception does not certify that work.")
		}
		fmt.Fprintln(diagnostic, "Original S9 Result received. "+receipt.NextAction)
		nativeS9WorkerMessage(ctx, "Your original S9 Result is received. Main owns the actual integration, necessary checks, impact judgment and independent verification.")
		return nil
	}
	return fmt.Errorf("original S9 Result responsibility was not prepared for %s", p.Binding.RepairAssignmentID)
}

func recordNativeS9OriginalLocation(ctx context.Context, root string, p workpacket.RepairPlanReportPacket, source string) error {
	store := runtime.NewWriter(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), root, semantic.RuntimeCandidateValidator{}).WithContext(ctx)
	snapshot, err := store.Snapshot()
	if err != nil {
		return err
	}
	registry, err := workspace.Decode(snapshot.State)
	if err != nil {
		return err
	}
	if registry == nil {
		registry, err = workspace.NewNativeReceipt(snapshot.State, root)
		if err != nil {
			return fmt.Errorf("Main authority for this original responsibility is unavailable: %w", err)
		}
	}
	if registry.Executions == nil {
		registry.Executions = map[string]workspace.Execution{}
	}
	b := p.Binding
	recorded, exists := registry.Executions[b.ManifestAssignmentID]
	if exists {
		if recorded.AgentID != b.OwnerAgentID || !samePath(recorded.Path, source) {
			return fmt.Errorf("S9 original responsibility has another recorded authoring location")
		}
		// The recorded responsibility coordinate is authoritative once it exists.
		// Only a missing branch is completed; a recorded branch is never
		// recomputed or overwritten.
		if recorded.Branch != "" {
			return nil
		}
	}
	// The original reception does not qualify the author's answer by the
	// repository state of its authoring location. Git is an integration
	// coordinate that Main resolves when the actual integration begins
	// (internal/cli/workspace_delivery.go), so an unresolvable branch is left
	// unknown here instead of withholding the already accepted Result
	// (L4-hook-platform-wiring.md:81 "不另遍历 Git worktree、比对 HEAD／branch／pin
	// 或认证 actor 与来源根来授予答卷资格"; L4-agent-dispatch-governance.md:57
	// "S7～S9 接收也不核 Git／ignore 状态"; L3-S9-repair.md:136 "原稿是否
	// tracked、staged、ignored 或 Git `??` 均不属于答卷接收资格").
	branch, branchErr := workspace.Git(ctx, source, "branch", "--show-current")
	if branchErr != nil {
		branch = ""
	}
	eventID := "native-s9-location-" + p.ID
	if exists {
		if recorded.Branch == branch {
			// The recorded authoring location already stands. An unknown
			// branch adds no fact, so nothing is written.
			return nil
		}
		recorded.Branch = branch
		registry.Executions[b.ManifestAssignmentID] = recorded
		eventID = "native-s9-location-branch-" + p.ID
	} else {
		var assignment repair.RepairAssignment
		if err := json.Unmarshal(p.Inputs.RepairAssignment, &assignment); err != nil {
			return err
		}
		e := workspace.Execution{Purpose: workspace.NativeDeliveryPurpose, AssignmentID: b.ManifestAssignmentID, RuntimeID: b.RuntimeID, BaselineGeneration: b.BaselineGeneration, Generation: 1, AgentID: b.OwnerAgentID, Path: source, Branch: branch, TargetBranch: registry.Branch, Status: "ready", WritePaths: append([]string{}, assignment.Scope...), Checks: []string{}, Inputs: map[string]string{}, CheckLocation: "main"}
		registry.Executions[e.AssignmentID] = e
	}
	baseline, _ := snapshot.State["baseline"].(map[string]any)
	_, err = store.Update(snapshot.Revision, runtime.Mutation{EventID: eventID, TransitionID: "WORKSPACE", Event: "workspace_updated", Actor: "Main", RuntimeID: b.RuntimeID, BaselineGeneration: integerValue(baseline["generation"]), IdempotencyKey: eventID, RetainLastTransition: true, OccurredAt: time.Now().UTC(), Apply: func(state map[string]any) error {
		pointer := repairPointerCLI(state)
		if state["runtime_id"] != b.RuntimeID || stringValue(pointer["session_id"]) != b.SessionID || stringMapCLI(pointer["assignment_owners"])[b.RepairAssignmentID] != b.OwnerAgentID {
			return fmt.Errorf("S9 original responsibility changed before reception")
		}
		state["workspace"] = workspace.Encode(registry)
		return nil
	}})
	return err
}

// nativeS9ReadableProfessionalResult reports whether the current bytes are a
// readable professional original: they parse into the original consumer's own
// Result answer and carry an actual conclusion.
//
// A file that is absent, still a draft or damaged carries no professional
// expression, so it can neither be a new submission nor a professional change.
func nativeS9ReadableProfessionalResult(raw []byte) bool {
	var answer workpacket.RepairResultProfessionalAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		return false
	}
	return strings.TrimSpace(answer.Verdict) != ""
}

// compareNativeS9AcceptedOriginal compares the current original at the
// recorded authoring location with the object this responsibility already
// received.
//
// The comparison reads the author's own professional fields only — the Plan
// judgment (plan, red checks, proposed paths) and the Result conclusion
// (verdict, unit results, evidence, residual risks and the migration or
// rollback references). A machine-generated ID, a timestamp, a Runtime
// revision, a counter or a digest never takes part in it, and an equivalent
// `null` is read with the same meaning as the consumer's own default
// (L4-workflow-convergence.md:112 "以同一原消费者的专业语义比较当前原卷与其
// 当前已收对象：覆盖作者可改变且实际保存的全部专业内容，包含变化路径、影响
// 处置和说明；消费者已有缺省与等价 `null` 按同一语义解释，不以机器生成的 ID、
// 时间或 Runtime revision 判作新判断").
//
// An equivalent current original is idempotent: the accepted facts are reused
// and nothing is received again. A current original with a real professional
// change is a conflict: the accepted facts stay exactly as they are and Main
// routes the original revision or rework path
// (L4-hook-platform-wiring.md:132 "已收交接直接复用原事实，不重接收或重转阶段；
// 私人文件后改不覆盖已收事实，真实专业变化走原修订／返工或未完成责任的新尝试").
func compareNativeS9AcceptedOriginal(ctx context.Context, root string, state map[string]any, p workpacket.RepairPlanReportPacket, e workspace.Execution, source string, service *submission.Service, diagnostic io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// The recorded authoring location is the only source this responsibility
	// may read. A different live location is not this original, and the
	// accepted facts stay untouched until Main restores this exact source.
	if !samePath(source, e.Path) {
		return fmt.Errorf("S9 original responsibility has another recorded authoring location; the accepted Result at %s is preserved and Main restores this exact source", e.Path)
	}
	plan, result, err := nativeS9AuthorPaths(root, p)
	if err != nil {
		return err
	}
	acceptedResult, hasAcceptedResult := nativeS9AcceptedResultAnswer(root, e)
	acceptedPlan, hasAcceptedPlan := nativeS9AcceptedPlanAnswer(root, service, p)
	currentPlan, planErr := workpacket.ReadFile(source, plan, 20<<20)
	currentResult, resultErr := workpacket.ReadFile(source, result, 20<<20)
	if planErr != nil && resultErr != nil {
		// The live source no longer holds this original. The accepted facts
		// stand and no live source is reopened
		// (L4-hook-platform-wiring.md:126 "已收且责任已结束、没有新尝试和未完
		// 后继时不读私人原稿并快速返回").
		fmt.Fprintln(diagnostic, "Original S9 Result is already received; Main preserves the accepted Final and resumes the original checkpoint ACK/cleanup independently of the current repair responsibility; no live source is reopened.")
		return nil
	}
	// The comparison runs on an explicit return or an explicit recovery, where
	// the author's current professional content is the final word of this turn.
	// During ordinary observation the author may still be editing: the accepted
	// fact stands, the later edit is a revision or a new attempt on the same
	// original responsibility, and nobody is asked to resubmit
	// (C02; L3-S9-repair.md:437 "真实专业变化走原修订／返工或未完成责任的新尝试";
	// L4-hook-platform-wiring.md:132 "私人文件后改不覆盖已收事实").
	stop, _ := ctx.Value(nativeS9PlanStopKey{}).(bool)
	explicit, _ := ctx.Value(nativeS9ExplicitOriginalKey{}).(bool)
	if stop || explicit {
		if resultErr == nil && hasAcceptedResult {
			// Only a readable professional document can carry a new professional
			// expression. An absent, unfinished or damaged current original says
			// nothing about the author's conclusion, so it is not a professional
			// change and never becomes the author's duty to resubmit
			// (L3-S9-repair.md:238 "保留原稿"; L4-hook-platform-wiring.md:373
			// "明确缺失与已有事实").
			if nativeS9ReadableProfessionalResult(currentResult) {
				equal, equalErr := nativeS9ResultProfessionalEqual(currentResult, acceptedResult)
				if equalErr != nil || !equal {
					return nativeS9AcceptedOriginalConflict("Result", filepath.Join(source, result))
				}
			}
		}
		if planErr == nil && hasAcceptedPlan {
			// The accepted PlanReport receipt is its own preserved fact. A
			// current Plan at the authoring location that no longer agrees with
			// it is a different Plan, not the accepted one.
			equal, equalErr := nativeS9PlanProfessionalEqual(currentPlan, acceptedPlan)
			if equalErr != nil || !equal {
				return nativeS9AcceptedOriginalConflict("PlanReport", filepath.Join(source, plan))
			}
		}
	}
	fmt.Fprintln(diagnostic, "Original S9 Result is already received; Main preserves the accepted Final and resumes the original checkpoint ACK/cleanup independently of the current repair responsibility; no live source is reopened.")
	return nil
}

// nativeS9AcceptedOriginalConflict reports that the current original no longer
// expresses the object this responsibility already received. The accepted
// facts are preserved; Main and the author own the original revision or
// rework path.
func nativeS9AcceptedOriginalConflict(kind, path string) error {
	return fmt.Errorf("original S9 %s at %s differs from the content this responsibility already received; the accepted facts are preserved and Main routes the original revision or rework path: %w", kind, path, runtime.ErrSubmissionConflict)
}

// nativeS9AcceptedResultAnswer reads the professional answer this
// responsibility already received from its accepted Candidate.
func nativeS9AcceptedResultAnswer(root string, e workspace.Execution) ([]byte, bool) {
	if e.DeliveryRef == "" {
		return nil, false
	}
	data, err := workpacket.ReadFile(root, e.DeliveryRef, 20<<20)
	if err != nil {
		return nil, false
	}
	candidate, err := repair.DecodeWorkspaceCandidate(data)
	if err != nil || candidate.Provenance == nil || len(candidate.Provenance.SubmittedAnswer) == 0 {
		return nil, false
	}
	return candidate.Provenance.SubmittedAnswer, true
}

// nativeS9AcceptedPlanAnswer reads the professional answer this
// responsibility already received from its accepted PlanReport receipt.
func nativeS9AcceptedPlanAnswer(root string, service *submission.Service, p workpacket.RepairPlanReportPacket) ([]byte, bool) {
	receipt, err := service.RepairPlanReportReceipt(p.Binding.OwnerAgentID, p.ID)
	if err != nil || !receipt.Accepted || len(receipt.DomainRefs) == 0 {
		return nil, false
	}
	data, err := workpacket.ReadFile(root, receipt.DomainRefs[0], 20<<20)
	if err != nil {
		return nil, false
	}
	var report repair.PlanReport
	if json.Unmarshal(data, &report) != nil || report.Receipt == nil || len(report.Receipt.SubmittedAnswer) == 0 {
		return nil, false
	}
	return report.Receipt.SubmittedAnswer, true
}

// nativeS9ResultProfessionalEqual compares the current flat Result with the
// accepted Result in the original consumer's professional terms.
func nativeS9ResultProfessionalEqual(current, accepted []byte) (bool, error) {
	var left, right workpacket.RepairResultProfessionalAnswer
	if err := json.Unmarshal(current, &left); err != nil {
		return false, err
	}
	if err := json.Unmarshal(accepted, &right); err != nil {
		return false, err
	}
	return nativeS9ProfessionalEqual(left, right), nil
}

// nativeS9PlanProfessionalEqual compares the current flat PlanReport with the
// accepted PlanReport in the original consumer's professional terms.
func nativeS9PlanProfessionalEqual(current, accepted []byte) (bool, error) {
	var left, right workpacket.RepairPlanReportProfessionalAnswer
	if err := json.Unmarshal(current, &left); err != nil {
		return false, err
	}
	if err := json.Unmarshal(accepted, &right); err != nil {
		return false, err
	}
	return nativeS9ProfessionalEqual(left, right), nil
}

// nativeS9ProfessionalEqual compares two author professional answers. Only the
// author-owned professional fields take part in it; an equivalent `null` is
// read with the same meaning as the consumer's own default.
func nativeS9ProfessionalEqual(current, accepted any) bool {
	left, err := nativeS9ProfessionalContent(current)
	if err != nil {
		return false
	}
	right, err := nativeS9ProfessionalContent(accepted)
	if err != nil {
		return false
	}
	leftBytes, err := json.Marshal(left)
	if err != nil {
		return false
	}
	rightBytes, err := json.Marshal(right)
	if err != nil {
		return false
	}
	return string(leftBytes) == string(rightBytes)
}

// nativeS9ProfessionalContent renders one author professional answer into its
// comparable content. Key order and formatting do not take part in it.
func nativeS9ProfessionalContent(value any) (any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, err
	}
	return nativeS9NormalizeProfessional(decoded), nil
}

// nativeS9NormalizeProfessional gives the consumer's own default the same
// meaning as an equivalent `null`.
func nativeS9NormalizeProfessional(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = nativeS9NormalizeProfessional(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, nativeS9NormalizeProfessional(item))
		}
		return out
	case nil:
		return []any{}
	default:
		return value
	}
}
