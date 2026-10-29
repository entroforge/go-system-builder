// Package submission connects saved authoring inputs to existing domain
// Writers and the existing Controller. It owns no lifecycle state.
package submission

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/agentauth"
	"github.com/entroforge/go-system-builder/internal/controller"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/metrics"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
)

type Service struct {
	ctx                          context.Context
	Root, StatePath, JournalPath string
	Files                        fileview.Reader
	Validator                    runtime.CandidateValidator
	readOnly                     bool
}

// New uses the same declared Git/disk source contract as production gates.
// Legacy runtimes are observed without resolving or upgrading their schema.
func New(root string) (*Service, error) {
	return NewContext(context.Background(), root, false)
}

// NewReadOnly builds a query service whose Runtime snapshots never recover a
// pending write. Receipt/status callers can report the pending boundary
// without turning a read into a mutation path.
func NewReadOnly(root string) (*Service, error) {
	return NewContext(context.Background(), root, true)
}

func NewContext(ctx context.Context, root string, readOnly bool) (*Service, error) {
	ctx = runtime.WithReviewResultReadCache(fileview.WithReadCache(ctx))
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	ctx = runtime.WithPacketBodyRoot(ctx, abs)
	s := &Service{ctx: ctx, Root: abs, StatePath: filepath.Join(abs, ".claude/loop-state.json"), JournalPath: filepath.Join(abs, ".claude/loop-events.jsonl"), Validator: semantic.RuntimeCandidateValidator{}, readOnly: readOnly}
	snapshot, err := s.snapshot()
	if err != nil {
		return nil, err
	}
	if !runtime.PacketEnabled(snapshot.State) {
		return s, nil
	}
	life, _ := snapshot.State["lifecycle"].(map[string]any)
	if life["state"] == "inactive" && snapshot.State["bound_req"] == nil {
		if err := runtime.ValidateBindEligibleState(snapshot.State); err != nil {
			return nil, err
		}
		// A fresh S0 has no formal S5 subject to resolve. Observing it does
		// not issue a packet or grant authoring; bound consumers stay strict.
		return s, nil
	}
	if err := fileview.ValidateAuthority(abs, snapshot.State); err != nil {
		return nil, err
	}
	catalog, err := transition.LoadCatalog(abs)
	if err != nil {
		return nil, err
	}
	ref, err := fileview.DevelopmentRef(snapshot.State)
	if err != nil {
		return nil, err
	}
	s.Files, err = fileview.NewContext(s.Context(), abs, "refs/heads/"+strings.TrimPrefix(ref, "refs/heads/"), catalog.Definition.FileSources)
	return s, err
}

func (s *Service) snapshot() (runtime.Snapshot, error) {
	if s.readOnly {
		return runtime.NewStore(s.StatePath, s.JournalPath).WithContext(s.Context()).Snapshot()
	}
	return runtime.NewWriter(s.StatePath, s.JournalPath, s.Root, s.Validator).WithContext(s.Context()).Snapshot()
}

// Issue publishes only registered current packets. Existing authoring bytes
// are preserved, including incomplete drafts and already accepted answers.
func (s *Service) Issue(owner string) ([]workpacket.Packet, error) {
	return s.IssueContext(s.Context(), owner)
}

func (s *Service) IssueContext(ctx context.Context, owner string) ([]workpacket.Packet, error) {
	s = s.WithContext(ctx)
	snapshot, err := s.snapshot()
	if err != nil {
		return nil, err
	}
	packets, err := runtime.S5PacketsWithFiles(s.Root, snapshot.State, owner, s.Files)
	if err != nil {
		return nil, err
	}
	analysis := map[string]*workpacket.TaskAnalysisRef{}
	for i, p := range packets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, issued, findErr := runtime.FindWorkPacket(snapshot.State, p.ID); findErr != nil {
			return nil, findErr
		} else if !issued {
			bound, _ := snapshot.State["bound_req"].(map[string]any)
			// This call pins every packet through the original Runtime writer.
			// Analysis itself is identical for exactly the same frozen read set.
			keyBytes, _ := json.Marshal(p.Inputs)
			key := string(keyBytes)
			if ref := analysis[key]; ref != nil {
				p, err = workpacket.BindTaskAnalysis(p, ref)
			} else {
				p, err = s.freezeTaskAnalysis(p, text(bound["id"]))
				if err == nil {
					analysis[key] = p.TaskAnalysis
				}
			}
			if err != nil {
				return nil, err
			}

			packets[i] = p
		}
		if _, err := runtime.RecordWorkPacketContext(ctx, s.Root, s.StatePath, s.JournalPath, p, s.Files, s.Validator); err != nil {
			return nil, err
		}
		view, err := json.MarshalIndent(p, "", "  ")
		if err != nil {
			return nil, err
		}
		view = append(view, '\n')
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		current, err := workpacket.Publish(s.Root, p.ViewPath, view)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(current, view) {
			return nil, fmt.Errorf("issued packet view differs from Runtime: %s", p.ViewPath)
		}
		latest, err := s.snapshot()
		if err != nil {
			return nil, err
		}
		if _, declared, err := workspace.ReviewExecution(latest.State, p); err != nil {
			return nil, err
		} else if declared {
			continue
		}
		draft, err := json.MarshalIndent(p.Draft, "", "  ")
		if err != nil {
			return nil, err
		}
		if _, err := workpacket.Publish(s.Root, p.AnswerPath, append(draft, '\n')); err != nil {
			return nil, err
		}
	}
	return packets, nil
}

// Save accepts an exact issued authoring path for its packet-bound owner. It
// freezes the bytes once before calling the original domain Writer.
func (s *Service) Save(ctx context.Context, owner, path string) (Receipt, error) {
	defer metrics.Span(ctx, "reception/s5")()
	s = s.WithContext(ctx)
	snapshot, err := s.snapshot()
	if err != nil {
		return Receipt{}, err
	}
	p, source, err := s.source(ctx, snapshot.State, owner, path)
	if err != nil {
		return rejected(snapshot, "", err), err
	}
	started := time.Now()
	outcome := "read_error"
	defer func() {
		// Diagnostics never alter the original receipt or business result.
		// Save owns no Writer lock at this return boundary. Replays are calls,
		// not additional unique receptions or first-submit successes.
		_ = metrics.ObserveSubmissionSave(s.Root, outcome, time.Since(started).Milliseconds())
	}()
	if err := ctx.Err(); err != nil {
		outcome = "interrupted"
		return rejected(snapshot, p.ID, err), err
	}
	data, err := workpacket.ReadFile(source.Root, source.Path, 256<<10)
	if err != nil {
		return rejected(snapshot, p.ID, err), err
	}
	answer, issues := workpacket.ValidateAnswer(p, data)
	if len(issues) > 0 {
		outcome = "incomplete"
		err := fmt.Errorf("answer %s: %s", issues[0].Pointer, issues[0].Expected)
		r := rejected(snapshot, p.ID, err)
		r.FieldErrors = issues
		return r, err
	}
	outcome = "complete_rejected"
	digest, err := workpacket.AnswerDigest(answer)
	if err != nil {
		return rejected(snapshot, p.ID, err), err
	}
	prior, accepted, err := runtime.FindSubmission(snapshot.State, p.ID)
	if err != nil {
		return rejected(snapshot, p.ID, err), err
	}
	if accepted && prior.RevisionPacketID != "" {
		return rejected(snapshot, p.ID, runtime.ErrSubmissionSuperseded), runtime.ErrSubmissionSuperseded
	}
	if accepted && prior.ContentHash != digest {
		return rejected(snapshot, p.ID, runtime.ErrSubmissionConflict), runtime.ErrSubmissionConflict
	}
	id := "ev-submission-" + workpacket.Digest([]byte("submission:"+p.ID))
	envelopePath := ".claude/evidence/submissions/" + p.ID + "/" + digest + ".json"
	if accepted {
		id = prior.EvidenceID
		var ok bool
		for _, ev := range evidenceRows(snapshot.State) {
			if ev["id"] == id {
				envelopePath, _ = ev["path"].(string)
				ok = true
				break
			}
		}
		if !ok {
			err := fmt.Errorf("accepted submission has no domain evidence")
			return rejected(snapshot, p.ID, err), err
		}
	} else {
		// Preflight protects the artifact spool. Both checks repeat under the
		// original domain Writer; publication alone never proves acceptance.
		if err := runtime.ValidateWorkPacket(s.Root, snapshot.State, p, s.Files); err != nil {
			return rejected(snapshot, p.ID, err), err
		}
		if err := validateS5ApprovalMode(s.Root, snapshot.State, owner); err != nil {
			return rejected(snapshot, p.ID, err), err
		}
		projected, err := json.MarshalIndent(workpacket.DocumentReview(p, id, answer, time.Now().UTC()), "", "  ")
		if err != nil {
			return rejected(snapshot, p.ID, err), err
		}
		stored, err := workpacket.Publish(s.Root, envelopePath, append(projected, '\n'))
		if err != nil {
			return rejected(snapshot, p.ID, err), err
		}
		if err := workpacket.ValidateDocumentReview(p, id, answer, stored); err != nil {
			return rejected(snapshot, p.ID, err), err
		}
	}
	if err := ctx.Err(); err != nil {
		return rejected(snapshot, p.ID, err), err
	}
	next, err := runtime.RecordEvidence(s.Root, s.StatePath, s.JournalPath, runtime.EvidenceRequest{Context: ctx, ExpectedRevision: -1, ID: id, Kind: "document_review", Path: envelopePath, ProducedBy: []string{owner}, ResponsibilityID: p.Binding.Responsibility, Validator: s.Validator, Submission: &runtime.SubmissionRequest{PacketID: p.ID, Owner: owner, AnswerJSON: data, Files: s.Files, Authorize: func(root string, state map[string]any) error {
		if err := s.validateSource(ctx, state, p, source); err != nil {
			return err
		}
		return validateS5ApprovalMode(root, state, owner)
	}}})
	if err != nil {
		return rejected(snapshot, p.ID, err), err
	}
	record, ok, err := runtime.FindSubmission(next.State, p.ID)
	if err != nil || !ok {
		return Receipt{}, fmt.Errorf("domain reception has no durable submission: %v", err)
	}
	outcome = "received_" + *answer.Conclusion
	if record.ControlStatus != "pending" {
		return receipt(s.Root, next, p, record)
	}
	// No Writer lock survives this call. A crash here leaves a durable pending
	// event for bounded recovery, without asking the author to resubmit work.
	result, controlErr := s.control(ctx, record.FollowupID, owner)
	var designAnswerPath string
	if controlErr == nil && result.Snapshot.State != nil {
		next = result.Snapshot
		life, _ := next.State["lifecycle"].(map[string]any)
		if result.Error == "" && result.QualityGate.TransitionCommitted && result.QualityGate.CandidateTransition == "TR-004" && life["state"] == "planning" && life["phase"] == "design" && runtime.PacketEnabled(next.State) {
			if design, err := runtime.CurrentDesignPacket(next.State); err == nil {
				if _, found, err := runtime.FindDesignPacket(next.State, design.ID); err == nil && found {
					designAnswerPath = design.AnswerPath
				}
			}
		}
	}
	r, err := receipt(s.Root, next, p, record)
	if err != nil {
		return Receipt{}, err
	}
	if designAnswerPath != "" {
		r.NextAction = "完成本轮实际设计后保存当前S2答卷：" + filepath.Join(s.Root, designAnswerPath)
	}
	if controlErr != nil || result.Error != "" {
		if !r.ConsumerPending {
			r.WaitingOn = append(r.WaitingOn, Waiting{Reason: "system_followup", Owner: "Main", Ref: record.FollowupID})
			r.NextAction = "本人交付已接收，可以结束本责任；阶段出口接续尚未完成，Main 检查原接续 " + record.FollowupID + " 与后继准备，无需重交已接收答案。"
		}
		r.ConsumerOwner, r.ConsumerPending = "main", true
		if controlErr != nil {
			r.NextAction += " 原因：" + controlErr.Error()
		}
		if result.Error != "" {
			r.NextAction += " 原因：" + result.Error
		}
	}
	if result.QualityGate.Status == controller.StatusNotReady && len(r.WaitingOn) == 0 {
		r.WaitingOn = append(r.WaitingOn, Waiting{Reason: "stage_gate_not_ready", Owner: "Main", Ref: result.QualityGate.GateID})
	}
	return r, nil
}

func validateS5ApprovalMode(root string, state map[string]any, owner string) error {
	entities, _ := state["entities"].(map[string]any)
	agents, _ := entities["agents"].([]any)
	for _, raw := range agents {
		agent, _ := raw.(map[string]any)
		if agent["id"] != owner {
			continue
		}
		mode, _ := agent["dispatch_mode"].(string)
		if mode == "plan_checkpoint" || mode == "one_shot" {
			return nil
		}
		// Approval-mode and legacy dispatches still require their original
		// human-approved activation/readback chain. Continuous plan_checkpoint
		// work uses the issued professional S5 questionnaire directly.
		return agentauth.ValidateWorkingAuthorization(root, state, owner)
	}
	return fmt.Errorf("S5 questionnaire owner %s is not registered", owner)
}

func (s *Service) control(ctx context.Context, event, owner string) (controller.ControlResult, error) {
	s = s.WithContext(ctx)
	result, err := controller.RunControlCycle(ctx, controller.ControlRequest{Root: s.Root, StatePath: s.StatePath, JournalPath: s.JournalPath, CWD: s.Root, Event: "PostToolUse", ToolName: "Write", AgentID: owner, SubmissionFollowupID: event, Files: s.Files})
	life, _ := result.Snapshot.State["lifecycle"].(map[string]any)
	if err == nil && result.Error == "" && result.QualityGate.TransitionCommitted && result.QualityGate.CandidateTransition == "TR-004" && life["state"] == "planning" && life["phase"] == "design" && runtime.PacketEnabled(result.Snapshot.State) {
		_, prepareErr := s.IssueDesign(ctx)
		if prepareErr == nil {
			var prepared runtime.Snapshot
			prepared, prepareErr = s.snapshot()
			if prepareErr == nil {
				result.Snapshot = prepared
			}
		}
		if prepareErr != nil {
			result.Error = "TR-004已完成并返回S2；设计问卷准备未完成：" + prepareErr.Error()
		}
	}
	return result, err
}

func evidenceRows(state map[string]any) []map[string]any {
	raw, _ := state["evidence"].([]any)
	rows := make([]map[string]any, 0, len(raw))
	for _, value := range raw {
		if row, ok := value.(map[string]any); ok {
			rows = append(rows, row)
		}
	}
	return rows
}
func text(value any) string { s, _ := value.(string); return s }

func (s *Service) Context() context.Context {
	if s.ctx != nil {
		return s.ctx
	}
	return context.Background()
}
func (s *Service) WithContext(ctx context.Context) *Service {
	ctx = runtime.WithPacketBodyRoot(runtime.WithReviewResultReadCache(fileview.WithReadCache(ctx)), s.Root)
	copy := *s
	copy.ctx = ctx
	if v, ok := copy.Files.(*fileview.View); ok {
		copy.Files = v.WithContext(ctx)
	}
	return &copy
}
