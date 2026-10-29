package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/assignmentresult"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/hook"
	"github.com/entroforge/go-system-builder/internal/hookctx"
	"github.com/entroforge/go-system-builder/internal/plancheckpoint"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/review"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/submission"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

// The terminal command consumes only the exact answer selected from original
// Runtime issuance. Owner and source are projected from that registration;
// --agent is only a compatibility hint for issue/receipt commands.
func runStageAnswer(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "runtime stage-answer requires <issue|revise|rework|consume|receipt|recover>")
		return 2
	}
	action := args[0]
	switch action {
	case "issue", "revise", "rework", "consume", "receipt", "recover":
	default:
		fmt.Fprintln(stderr, "unknown stage-answer action")
		return 2
	}
	flags := flag.NewFlagSet("runtime stage-answer "+action, flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "Main authority root")
	owner := flags.String("agent", "", "registered owner (optional compatibility hint)")
	path := flags.String("path", "", "exact original input path: issued typed answer, native S9 Result, or Targeted answer (consume/receipt)")
	previous := flags.String("previous", "", "previous receipt submission_key (Main revise)")
	reason := flags.String("reason", "", "concrete named revision reason")
	target := flags.String("target", "", "professional rework target: design|contracts|tasks")
	packetID := flags.String("packet", "", "historical issued packet ID (receipt)")
	if err := parseWorkspaceFlags(flags, args[1:]); err != nil {
		return 2
	}
	if action == "consume" && *path == "" {
		fmt.Fprintln(stderr, "consume requires the exact --path from issuance")
		return 2
	}
	if action == "receipt" && (*owner == "" || (*path == "" && *packetID == "")) {
		fmt.Fprintln(stderr, "receipt requires --agent and the exact --path or historical --packet from issuance")
		return 2
	}
	if err := workspace.RequireMain(*root); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	budget := 6 * time.Second
	if action == "consume" || action == "recover" || action == "issue" {
		budget = 12 * time.Second
	}
	ctx, cancel := context.WithTimeout(fileview.WithReadCache(context.Background()), budget)
	defer cancel()
	// Initialization may recover the original pending state/journal pair; it
	// shares this same named operation budget with the receiver itself.
	s, err := submission.NewContext(ctx, *root, action == "receipt")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var result any
	switch action {
	case "issue":
		// Issuance is a named Main mutation path. Route it from the original
		// Runtime Writer snapshot so a pending packet issuance can be recovered
		// before the selected domain issuer performs its own source checks/CAS.
		snapshot, readErr := runtime.NewWriter(s.StatePath, s.JournalPath, s.Root, semantic.RuntimeCandidateValidator{}).Snapshot()
		if readErr != nil {
			err = readErr
			break
		}
		life, _ := snapshot.State["lifecycle"].(map[string]any)
		if life["state"] == "planning" && (life["phase"] == "design" || life["phase"] == "contracts" || life["phase"] == "tasks") {
			if *owner != "" && *owner != "Main" {
				err = fmt.Errorf("planning issue requires the actual Main responsibility")
				break
			}
			if life["phase"] == "tasks" {
				result, err = s.IssueTaskPlanning(ctx)
			} else if life["phase"] == "contracts" {
				result, err = s.IssueContracts(ctx)
			} else {
				result, err = s.IssueDesign(ctx)
			}
			if life["phase"] == "contracts" || life["phase"] == "tasks" {
				message := ""
				if err != nil {
					message = err.Error()
				}
				submission.RecordPlanningDiagnostic(s.Root, snapshot.State, "preparation", message)
			}
		} else if life["state"] == "verification" && runtime.PacketEnabled(snapshot.State) {
			if *owner != "" && *owner != "Main" {
				err = fmt.Errorf("S7 ReviewResult issuance requires the original Main entry")
				break
			}
			result, err = s.IssueReviewResults(ctx, "")
		} else if life["state"] == "bug_resolution" && life["phase"] == "investigation" && runtime.PacketEnabled(snapshot.State) {
			if *owner != "" && *owner != "Main" {
				err = fmt.Errorf("S8 HypothesisResult issuance requires the original Main entry")
				break
			}
			result, err = s.IssueHypothesisResults(ctx, "")
		} else if life["state"] == "bug_resolution" && (life["phase"] == "planning" || life["phase"] == "reproducing") && runtime.PacketEnabled(snapshot.State) {
			if *owner != "" && *owner != "Main" {
				err = fmt.Errorf("S9 PlanReport packet issuance requires the original Main entry")
				break
			}
			result, err = s.IssueRepairPlanReports(ctx, "")
		} else if life["state"] == "bug_resolution" && life["phase"] == "fixing" && runtime.PacketEnabled(snapshot.State) {
			if *owner != "" && *owner != "Main" {
				err = fmt.Errorf("S9 RepairResult packet issuance requires the original Main entry")
				break
			}
			result, err = s.IssueRepairResults(ctx, "")
		} else if life["state"] == "document_verification" && runtime.PacketEnabled(snapshot.State) && (*owner == "" || *owner == "Main") {
			// The named recovery entry reuses the original S5 preparation.
			// Ordinary Main observations never register or issue responsibilities.
			err = prepareS5ReviewResponsibilities(ctx, s.Root, snapshot.State)
			// Preparation already ran the original issuer. Project its durable
			// publication facts once, without issuing or validating sources again.
			packets, guidance, observeErr := s5IssueHandoffContext(ctx, s.Root, err)
			fmt.Fprintln(stderr, guidance)
			if err == nil {
				err = observeErr
				if err == nil {
					result = packets
				}
			}
		} else {
			result, err = s.IssueContext(ctx, *owner)
		}
	case "rework":
		if *owner != "" && *owner != "Main" {
			err = fmt.Errorf("planning rework requires the actual Main responsibility")
			break
		}
		result, err = s.ReworkPlanning(ctx, *target, *reason)
	case "revise":
		result, err = s.Revise(ctx, *previous, *reason)
	case "consume":
		result, err = consumeRegisteredStageAnswer(ctx, s, *path)
	case "receipt":
		if isS9RepairResultResolutionPath(*path) {
			if *owner != "Main" {
				err = fmt.Errorf("S9 resolution receipt belongs to Main's recovery decision")
			} else {
				result, err = s.RepairResultResolutionReceipt(*path)
			}
			break
		}
		if *packetID != "" {
			found, lookupErr := isRegisteredRepairResultPacket(s, *packetID)
			if lookupErr != nil {
				err = lookupErr
				break
			} else if found {
				result, err = s.RepairResultReceipt(*owner, *packetID)
				break
			}
		}
		if isS9RepairResultPath(*path) {
			result, err = s.RepairResultReceipt(*owner, *path)
			break
		}
		if *packetID != "" {
			found, lookupErr := isRegisteredRepairPlanReportPacket(s, *packetID)
			if lookupErr != nil {
				err = lookupErr
				break
			} else if found {
				result, err = s.RepairPlanReportReceipt(*owner, *packetID)
				break
			}
		}
		if isS9RepairPlanReportPath(*path) {
			result, err = s.RepairPlanReportReceipt(*owner, *path)
			break
		}
		if *packetID != "" {
			found, lookupErr := isRegisteredHypothesisResultPacket(s, *packetID)
			if lookupErr != nil {
				err = lookupErr
				break
			} else if found {
				result, err = s.HypothesisResultReceipt(*owner, *packetID)
				break
			}
		}
		if isS8HypothesisResultPath(*path) {
			result, err = s.HypothesisResultReceipt(*owner, *path)
			break
		}
		if *owner == "Main" {
			if *packetID != "" {
				found, lookupErr := isRegisteredReviewResultPacket(s, *packetID)
				if lookupErr != nil {
					err = lookupErr
					break
				} else if found {
					result, err = s.ReviewResultReceipt(*owner, *packetID)
					break
				}
			}
			if isS7ReviewResultPath(*path) {
				result, err = s.ReviewResultReceipt(*owner, *path)
				break
			}
			result, err = s.MainPlanningReceipt(*path, *packetID)
			break
		}
		if *packetID != "" {
			found, lookupErr := isRegisteredReviewResultPacket(s, *packetID)
			if lookupErr != nil {
				err = lookupErr
				break
			} else if found {
				result, err = s.ReviewResultReceipt(*owner, *packetID)
				break
			}
		}
		if isS7ReviewResultPath(*path) {
			result, err = s.ReviewResultReceipt(*owner, *path)
			break
		}
		if *packetID != "" {
			result, err = s.ReceiptPacket(*owner, *packetID)
		} else {
			result, err = s.DiagnoseReceiptContext(ctx, *owner, *path)
		}
	case "recover":
		result, err = s.Recover(ctx, *owner, 2)
	}
	// CLI and Hook share exactly the same successor projection/preparation.
	if action == "consume" || action == "recover" {
		switch value := result.(type) {
		case submission.Receipt:
			continueS5PlanningHandoff(context.Background(), s.Root, &value)
			result = value
		case []submission.Receipt:
			for i := range value {
				continueS5PlanningHandoff(context.Background(), s.Root, &value[i])
			}
			result = value
		}
	}

	if action == "recover" && err == nil {
		// Keep the existing receipt-array JSON contract. Human/model CLI
		// feedback carries the exact original S9 successor immediately after
		// the explicit Writer recovery, within this same operation context.
		if snap, readErr := runtime.NewStore(s.StatePath, s.JournalPath).WithContext(ctx).Snapshot(); readErr == nil {
			life := mapFieldCLI(snap.State, "lifecycle")
			if life["state"] == "bug_resolution" && s9ProfessionalPhase(stringValue(life["phase"])) {
				if next := s9KnownMainHandoffsContext(ctx, s.Root, snap); next != "" {
					fmt.Fprintln(stderr, next)
				}
			}
		} else {
			fmt.Fprintln(stderr, "Original Writer recovery completed; current successor feedback could not be read:", readErr)
		}
	}
	if result != nil {
		if e := json.NewEncoder(stdout).Encode(result); e != nil {
			fmt.Fprintln(stderr, e)
			return 1
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// consumeRegisteredStageAnswer routes by the exact current issued path and
// supplies the original packet's owner/source to its existing consumer.
func consumeRegisteredStageAnswer(ctx context.Context, service *submission.Service, inputPath string) (any, error) {
	path := inputPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(service.Root, filepath.FromSlash(path))
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if result, handled, err := consumeNativeS9Original(ctx, service, path); handled {
		return result, err
	}
	switch {
	case isS9RepairResultResolutionPath(path):
		if _, found, err := service.RepairResultResolutionForPath(path); err != nil {
			return nil, err
		} else if !found {
			return nil, fmt.Errorf("S9 Main resolution path has no exact original issuance")
		}
		return service.SaveRepairResultResolutionFrom(ctx, path, nil)
	case isS7ReviewResultPath(path):
		packet, sourceRoot, found, err := service.ReviewResultSourceForPath(path)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("S7 answer path has no current registered issuance")
		}
		return service.SaveReviewResultFrom(ctx, packet.Binding.OwnerAgentID, path, sourceRoot, nil)
	case isS8HypothesisResultPath(path):
		packet, sourceRoot, found, err := service.HypothesisResultSourceForPath(path)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("S8 answer path has no current registered issuance")
		}
		return service.SaveHypothesisResultFrom(ctx, packet.Binding.OwnerAgentID, path, sourceRoot, nil)
	case isS9RepairPlanReportPath(path):
		packet, sourceRoot, found, err := service.RepairPlanReportSourceForPath(path)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("S9 PlanReport answer path has no current registered issuance")
		}
		return service.SaveRepairPlanReportFrom(ctx, packet.Binding.OwnerAgentID, path, sourceRoot, nil)
	case isS9RepairResultPath(path):
		packet, sourceRoot, found, err := service.RepairResultSourceForPath(path)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("S9 RepairResult answer path has no current registered Worker issuance")
		}
		return service.SaveRepairResultFrom(ctx, packet.Binding.Sources.OwnerAgentID, path, sourceRoot, nil)
	}
	if _, found, err := service.TaskPlanningForPath(path); err != nil {
		return nil, err
	} else if found {
		return service.SaveTaskPlanning(ctx, path)
	}
	if _, found, err := service.ContractForPath(path); err != nil {
		return nil, err
	} else if found {
		return service.SaveContracts(ctx, path)
	}
	if _, found, err := service.DesignForPath(path); err != nil {
		return nil, err
	} else if found {
		return service.SaveDesign(ctx, path)
	}
	owner, found, err := registeredS5PacketOwnerForPath(service.Root, path)
	if err != nil {
		return nil, err
	}
	if found {
		return service.Save(ctx, owner, path)
	}
	return nil, fmt.Errorf("answer path has no exact current registered issuance")
}

// A successful save with an actual file_path selects its registered answer.
// Saves without a host path are reconciled by runObservedQuestionnaireSaves;
// command text and guessed owners never select a consumer.
func runSavedStageAnswerHook(root string, request policy.Input, stdout, stderr io.Writer) (bool, int) {
	path, saved := savedQuestionnairePath(request)
	if !saved {
		return false, 0
	}
	if isS7ReviewResultPath(path) {
		return saveReviewResultHook(root, request, path, stdout, stderr)
	}
	if isS8HypothesisResultPath(path) {
		return saveHypothesisResultHook(root, request, path, stdout, stderr)
	}
	if isS9RepairPlanReportPath(path) {
		return saveRepairPlanReportHook(root, request, path, stdout, stderr)
	}
	if isS9RepairResultPath(path) {
		return saveRepairResultHook(root, request, path, stdout, stderr)
	}
	if isS9RepairResultResolutionPath(path) {
		return saveRepairResultResolutionHook(root, request, path, stdout, stderr)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false, 0
	}
	rel = filepath.ToSlash(rel)
	// The path identifies a candidate responsibility, not a write. In the
	// S2/S3 boundary an unknown tool must have changed this exact answer in
	// its matching Pre/Post observation; reading an existing PASS is inert.
	if (strings.Contains(rel, "/S2/") || strings.Contains(rel, "/S3/")) && !actualPlanningAnswerSave(request, path) {
		writePlanningPostOutput(stdout, nil, planningUnobservedSaveGuidance(root, request, path))
		return true, 0
	}
	workerAnswer := filepath.ToSlash(path)
	if rel != ".claude/submissions" && !strings.HasPrefix(rel, ".claude/submissions/") &&
		!strings.HasSuffix(workerAnswer, "/"+workpacket.WorkerAnswerPath) && !strings.HasSuffix(workerAnswer, "/"+workpacket.LegacyWorkerAnswerPath) {
		return false, 0
	}
	// Legacy plan/completion inputs keep their original adapters.
	if rel == ".claude/submissions/plan.json" || rel == ".claude/submissions/completion.json" {
		return false, 0
	}
	// An exact Main answer path locates work; it does not turn a Worker's
	// ordinary tool event into Main's professional submission. This routing
	// boundary leaves the underlying tool allowed and emits no permission gate.
	if !mainQuestionnaireViewer(request) && (strings.Contains(rel, "/S2/") || strings.Contains(rel, "/S3/") || strings.Contains(rel, "/S4/")) {
		return true, 0
	}
	if strings.Contains(rel, "/S2/") {
		ctx, cancel := context.WithTimeout(contextForHook(request), 6*time.Second)
		defer cancel()
		service := &submission.Service{Root: root, StatePath: filepath.Join(root, ".claude/loop-state.json"), JournalPath: filepath.Join(root, ".claude/loop-events.jsonl"), Validator: semantic.RuntimeCandidateValidator{}}
		r, saveErr := service.SaveDesign(ctx, path)
		if saveErr != nil {
			fmt.Fprintf(stderr, "design not accepted: %v\n", saveErr)
			if r.Error == "" {
				r.Error = saveErr.Error()
			}
		}
		return true, renderStageAnswerHook(stdout, path, r, "S2")
	}
	// The original packet at this exact path selects S2/S3/S4 Main authoring;
	// native Worker events were excluded above. CWD does not invent ownership.
	s, err := submission.New(root)
	if err == nil {
		if _, found, findErr := s.TaskPlanningForPath(path); findErr == nil && found {
			ctx, cancel := context.WithTimeout(contextForHook(request), 12*time.Second)
			defer cancel()
			r, saveErr := s.SaveTaskPlanning(ctx, path)
			if saveErr != nil {
				fmt.Fprintf(stderr, "TASK planning save attempt not accepted: %v\n", saveErr)
				if r.Error == "" {
					r.Error = saveErr.Error()
				}
			}
			continueS5PlanningHandoff(contextForHook(request), root, &r)
			return true, renderStageAnswerHook(stdout, path, r, "S4")
		}
		if _, found, findErr := s.ContractForPath(path); findErr == nil && found {
			ctx, cancel := context.WithTimeout(contextForHook(request), 6*time.Second)
			defer cancel()
			r, saveErr := s.SaveContracts(ctx, path)
			if saveErr != nil {
				fmt.Fprintf(stderr, "contract not accepted: %v\n", saveErr)
				if r.Error == "" {
					r.Error = saveErr.Error()
				}
			}
			return true, renderStageAnswerHook(stdout, path, r, "S3")
		}
		if _, found, findErr := s.DesignForPath(path); findErr == nil && found {
			ctx, cancel := context.WithTimeout(contextForHook(request), 6*time.Second)
			defer cancel()
			r, saveErr := s.SaveDesign(ctx, path)
			if saveErr != nil {
				fmt.Fprintf(stderr, "design not accepted: %v\n", saveErr)
				if r.Error == "" {
					r.Error = saveErr.Error()
				}
			}
			return true, renderStageAnswerHook(stdout, path, r, "S2")
		}
	}
	if mainQuestionnaireViewer(request) {
		return false, 0 // Main cannot receive a delegated S5 answer through an ordinary save.
	}
	owner, found, err := registeredS5PacketOwnerForPath(root, path)
	if err != nil {
		fmt.Fprintf(stderr, "stage-answer not accepted: %v\n", err)
		return true, renderStageAnswerHook(stdout, path, submission.Receipt{ResponsibilityStatus: "not_accepted", CurrentStage: "S5", Error: err.Error()})
	}
	if !found {
		owner, found, err = registeredAcceptedS5PacketOwnerForPath(root, path)
		if err != nil {
			fmt.Fprintf(stderr, "stage-answer receipt unavailable: %v\n", err)
			return true, renderStageAnswerHook(stdout, path, submission.Receipt{ResponsibilityStatus: "not_accepted", CurrentStage: "S5", Error: err.Error()})
		}
		if !found {
			return false, 0
		}
	}
	if s == nil {
		s, err = submission.New(root)
		if err != nil {
			return true, renderStageAnswerHook(stdout, path, submission.Receipt{ResponsibilityStatus: "not_accepted", NextAction: "由 Main 恢复控制 Runtime 与冻结输入。", Error: err.Error()})
		}
	}
	ctx, cancel := context.WithTimeout(contextForHook(request), 6*time.Second)
	defer cancel()
	r, err := s.Save(ctx, owner, path)
	if err != nil {
		fmt.Fprintf(stderr, "stage-answer not accepted: %v\n", err)
		if r.Error == "" {
			r.Error = err.Error()
		}
	}
	return true, renderStageAnswerHook(stdout, path, r)
}

// registeredS5PacketOwnerForPath projects the owner from an original Runtime
// issuance and its current registered execution paths. It never infers an
// owner from Hook metadata or an unregistered filename.
func registeredS5PacketOwnerForPath(root, path string) (string, bool, error) {
	service, err := submission.NewReadOnly(root)
	if err != nil {
		return "", false, err
	}
	snapshot, err := runtime.NewStore(service.StatePath, service.JournalPath).Snapshot()
	if err != nil || !runtime.PacketEnabled(snapshot.State) {
		return "", false, err
	}
	packets, err := runtime.S5Packets(service.Root, snapshot.State, "")
	if err != nil {
		return "", false, err
	}
	registry, registryErr := workspace.Decode(snapshot.State)
	owner := ""
	for _, packet := range packets {
		answerPath := filepath.Join(service.Root, filepath.FromSlash(packet.AnswerPath))
		matches := filepath.Clean(path) == filepath.Clean(answerPath)
		if !matches {
			execution, declared, err := workspace.ReviewExecution(snapshot.State, packet)
			if err != nil {
				return "", false, err
			}
			if declared && execution.AssignmentID != "" {
				if registryErr != nil || registry == nil {
					return "", false, fmt.Errorf("S5 registered execution paths are unavailable: %v", registryErr)
				}
				paths, err := registry.WorkerSubmissionPaths(execution)
				if err != nil {
					return "", false, err
				}
				workerAnswer := filepath.Join(execution.Path, filepath.FromSlash(paths.Answer))
				matches = filepath.Clean(path) == filepath.Clean(workerAnswer)
			}
		}
		if !matches {
			continue
		}
		if owner != "" {
			return "", false, fmt.Errorf("S5 answer path matches multiple original issuances")
		}
		owner = packet.Binding.Owner
	}
	return owner, owner != "", nil
}

// Historical exact paths are only a fallback after current S5 tips. The
// original Save consumer still decides replay/conflict/supersession.
func registeredAcceptedS5PacketOwnerForPath(root, path string) (string, bool, error) {
	service, err := submission.NewReadOnly(root)
	if err != nil {
		return "", false, err
	}
	snapshot, err := runtime.NewStore(service.StatePath, service.JournalPath).Snapshot()
	if err != nil || !runtime.PacketEnabled(snapshot.State) {
		return "", false, err
	}
	registry, err := workspace.Decode(snapshot.State)
	if err != nil {
		return "", false, err
	}
	entities, _ := snapshot.State["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	owner := ""
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["template_id"] != "s5-document-review-v1" {
			continue
		}
		packet, ok, err := runtime.FindWorkPacket(snapshot.State, stringValue(row["packet_id"]))
		if err != nil || !ok {
			return "", false, fmt.Errorf("read registered S5 issuance: %v", err)
		}
		_, accepted, err := runtime.FindSubmission(snapshot.State, packet.ID)
		if err != nil {
			return "", false, err
		}
		if !accepted {
			continue
		}
		matches, hasExecution := false, false
		if registry != nil {
			for _, execution := range registry.AllExecutions() {
				if execution.WorkPacketID != packet.ID {
					continue
				}
				hasExecution = true
				if execution.Purpose != workspace.DocumentReviewPurpose || execution.AssignmentID != packet.Binding.AssignmentID || execution.AgentID != packet.Binding.Owner || execution.RuntimeID != packet.Binding.RuntimeID || execution.BaselineGeneration != packet.Binding.Generation || execution.StageEntry == nil || *execution.StageEntry != packet.Binding.StageEntry {
					return "", false, fmt.Errorf("historical S5 execution binding differs from packet %s", packet.ID)
				}
				paths, err := registry.WorkerSubmissionPaths(execution)
				if err != nil {
					return "", false, err
				}
				candidate := filepath.Join(execution.Path, filepath.FromSlash(paths.Answer))
				if filepath.Clean(path) == filepath.Clean(candidate) {
					matches = true
				}
			}
		}
		if !hasExecution && !packet.Binding.ExecutionRequired {
			matches = filepath.Clean(path) == filepath.Clean(filepath.Join(service.Root, filepath.FromSlash(packet.AnswerPath)))
		}
		if !matches {
			continue
		}
		if owner != "" {
			return "", false, fmt.Errorf("historical S5 answer path matches multiple accepted issuances")
		}
		owner = packet.Binding.Owner
	}
	return owner, owner != "", nil
}

func isS7ReviewResultPath(path string) bool {
	clean := "/" + strings.Trim(filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))), "/") + "/"
	return ((strings.Contains(clean, "/.claude/submissions/") && strings.Contains(clean, "/S7/")) || strings.Contains(clean, "/.loop-submissions/s7/")) && strings.HasSuffix(clean, "/answer.json/")
}

func saveReviewResultHook(root string, request policy.Input, path string, stdout, stderr io.Writer) (bool, int) {
	if mainQuestionnaireViewer(request) {
		return false, 0 // Native professional reception belongs to SubagentStop.
	}
	service, err := submission.New(root)
	if err != nil {
		return true, renderStageAnswerHook(stdout, path, submission.Receipt{ResponsibilityStatus: "not_accepted", Error: err.Error()}, "S7")
	}
	packet, sourceRoot, found, err := service.ReviewResultSourceForPath(path)
	if err != nil {
		return true, renderStageAnswerHook(stdout, path, submission.Receipt{ResponsibilityStatus: "not_accepted", Error: err.Error()}, "S7")
	}
	if !found {
		return true, renderStageAnswerHook(stdout, path, submission.Receipt{ResponsibilityStatus: "not_accepted", Error: "S7 answer path has no current registered issuance"}, "S7")
	}
	if workpacket.ReviewResultUsesNormalAuthoring(packet) {
		return true, 0 // New native originals are drafts until their actual Stop.
	}
	ctx, cancel := context.WithTimeout(contextForHook(request), 6*time.Second)
	defer cancel()
	owner := packet.Binding.OwnerAgentID
	receipt, saveErr := service.SaveReviewResultFrom(ctx, owner, path, sourceRoot, nil)
	return true, renderReviewResultSaveHook(stdout, stderr, path, receipt, saveErr)
}

func renderReviewResultSaveHook(stdout, stderr io.Writer, path string, receipt submission.Receipt, saveErr error) int {
	if saveErr != nil {
		writeReviewResultSaveDiagnostic(stderr, saveErr)
		var gap *submission.ReviewResultReceiptError
		if errors.As(saveErr, &gap) {
			// A verified receipt is unavailable. Do not pass this committed
			// submission through the generic not_accepted receipt default.
			message := "S7 original submission succeeded; receipt verification remains incomplete: " + saveErr.Error() + ". " + receipt.NextAction + " Main verifies the exact original ReviewResult receipt and then resumes only its pending original outlet with runtime review-continue, or runtime audit-prepare after TR-009 entered S10. No author correction, additional save, Stop or re-dispatch is requested."
			_ = json.NewEncoder(stdout).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PostToolUse", "additionalContext": message}})
			return 0
		}
		if receipt.Error == "" {
			receipt.Error = saveErr.Error()
		}
	}
	return renderStageAnswerHook(stdout, path, receipt, "S7")
}

func writeReviewResultSaveDiagnostic(stderr io.Writer, saveErr error) {
	var gap *submission.ReviewResultReceiptError
	if errors.As(saveErr, &gap) {
		fmt.Fprintf(stderr, "S7 original submission succeeded; receipt verification remains incomplete: %v. Main verifies the exact original receipt and resumes its pending outlet; no author correction, additional save or re-dispatch is requested.\n", saveErr)
		return
	}
	fmt.Fprintf(stderr, "S7 original submission is unconfirmed: %v\n", saveErr)
}

func isS8HypothesisResultPath(path string) bool {
	clean := "/" + strings.Trim(filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))), "/") + "/"
	return strings.Contains(clean, "/.claude/submissions/") && strings.Contains(clean, "/S8/") && strings.HasSuffix(clean, "/answer.json/")
}

func isS9RepairPlanReportPath(path string) bool {
	clean := "/" + strings.Trim(filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))), "/") + "/"
	return strings.Contains(clean, "/.claude/submissions/") && strings.Contains(clean, "/S9-plan/") && strings.HasSuffix(clean, "/answer.json/")
}

func isS9RepairResultPath(path string) bool {
	clean := "/" + strings.Trim(filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))), "/") + "/"
	return strings.Contains(clean, "/.claude/submissions/") && strings.Contains(clean, "/S9-result/") && strings.HasSuffix(clean, "/answer.json/")
}

// runSavedS9RepairResultHook is the narrow ordinary-save adapter used before
// broad native quiet routes. The path identifies a candidate, never a grant.
func runSavedS9RepairResultHook(root string, in policy.Input, out, diagnostic io.Writer) (bool, int) {
	if mainQuestionnaireViewer(in) {
		return false, 0
	}
	path, saved := savedQuestionnairePath(in)
	if !saved {
		if returned, ok := in.ToolResponse["file_path"].(string); ok {
			located := in
			located.ToolInput = map[string]any{"file_path": returned}
			path, saved = savedQuestionnairePath(located)
		}
	}
	if !saved || !isS9RepairResultPath(path) {
		return false, 0
	}
	return saveRepairResultHook(root, in, path, out, diagnostic)
}

func saveRepairResultHook(root string, request policy.Input, path string, stdout, stderr io.Writer) (bool, int) {
	if mainQuestionnaireViewer(request) {
		return false, 0 // Native professional reception belongs to SubagentStop.
	}
	ctx, cancel := context.WithTimeout(contextForHook(request), 6*time.Second)
	defer cancel()
	service := &submission.Service{
		Root: root, StatePath: filepath.Join(root, ".claude/loop-state.json"),
		JournalPath: filepath.Join(root, ".claude/loop-events.jsonl"),
		Validator:   semantic.RuntimeCandidateValidator{},
	}
	receipt, receive, saveErr := service.SaveRepairResultFromOrdinaryHook(ctx, path)
	if !receive && saveErr == nil {
		return true, 0
	}
	if saveErr != nil {
		fmt.Fprintf(stderr, "S9 RepairResult not accepted: %v\n", saveErr)
		if receipt.Error == "" {
			receipt.Error = saveErr.Error()
		}
	}
	return true, renderStageAnswerHook(stdout, path, receipt, "S9")
}

func saveRepairPlanReportHook(root string, request policy.Input, path string, stdout, stderr io.Writer) (bool, int) {
	if mainQuestionnaireViewer(request) {
		return false, 0 // Native professional reception belongs to SubagentStop.
	}
	ctx, cancel := context.WithTimeout(contextForHook(request), 6*time.Second)
	defer cancel()
	service := &submission.Service{
		Root: root, StatePath: filepath.Join(root, ".claude/loop-state.json"),
		JournalPath: filepath.Join(root, ".claude/loop-events.jsonl"),
		Validator:   semantic.RuntimeCandidateValidator{},
	}
	receipt, saveErr := service.SaveRepairPlanReportFrom(ctx, "", path, "", nil)
	if saveErr != nil {
		fmt.Fprintf(stderr, "S9 PlanReport not accepted: %v\n", saveErr)
		if receipt.Error == "" {
			receipt.Error = saveErr.Error()
		}
	}
	return true, renderStageAnswerHook(stdout, path, receipt, "S9")
}

func saveHypothesisResultHook(root string, request policy.Input, path string, stdout, stderr io.Writer) (bool, int) {
	if mainQuestionnaireViewer(request) {
		return false, 0 // Native professional reception belongs to SubagentStop.
	}
	service, err := submission.New(root)
	if err != nil {
		return true, renderStageAnswerHook(stdout, path, submission.Receipt{ResponsibilityStatus: "not_accepted", Error: err.Error()}, "S8")
	}
	packet, sourceRoot, found, err := service.HypothesisResultSourceForPath(path)
	if err != nil {
		return true, renderStageAnswerHook(stdout, path, submission.Receipt{ResponsibilityStatus: "not_accepted", Error: err.Error()}, "S8")
	}
	if !found {
		return true, renderStageAnswerHook(stdout, path, submission.Receipt{ResponsibilityStatus: "not_accepted", Error: "S8 answer path has no current registered issuance"}, "S8")
	}
	owner := packet.Binding.OwnerAgentID
	ctx, cancel := context.WithTimeout(contextForHook(request), 6*time.Second)
	defer cancel()
	receipt, saveErr := service.SaveHypothesisResultFrom(ctx, owner, path, sourceRoot, nil)
	if saveErr != nil {
		fmt.Fprintf(stderr, "S8 HypothesisResult not accepted: %v\n", saveErr)
		if receipt.Error == "" {
			receipt.Error = saveErr.Error()
		}
	}
	return true, renderStageAnswerHook(stdout, path, receipt, "S8")
}

func isRegisteredReviewResultPacket(s *submission.Service, id string) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("S7 submission service is unavailable")
	}
	snapshot, err := runtime.NewStore(s.StatePath, s.JournalPath).Snapshot()
	if err != nil {
		return false, err
	}
	_, found, err := runtime.FindReviewResultPacketContext(runtime.WithPacketBodyRoot(s.Context(), s.Root), snapshot.State, id)
	return found, err
}

func isRegisteredHypothesisResultPacket(s *submission.Service, id string) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("S8 submission service is unavailable")
	}
	snapshot, err := runtime.NewStore(s.StatePath, s.JournalPath).Snapshot()
	if err != nil {
		return false, err
	}
	_, found, err := runtime.FindHypothesisResultPacket(snapshot.State, id)
	return found, err
}

func isRegisteredRepairPlanReportPacket(s *submission.Service, id string) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("S9 submission service is unavailable")
	}
	snapshot, err := runtime.NewStore(s.StatePath, s.JournalPath).Snapshot()
	if err != nil {
		return false, err
	}
	_, found, err := runtime.FindRepairPlanReportPacketContext(runtime.WithPacketBodyRoot(s.Context(), s.Root), snapshot.State, id)
	return found, err
}

func isRegisteredRepairResultPacket(s *submission.Service, id string) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("S9 submission service is unavailable")
	}
	snapshot, err := runtime.NewStore(s.StatePath, s.JournalPath).Snapshot()
	if err != nil {
		return false, err
	}
	_, found, err := runtime.FindRepairResultPacketContext(runtime.WithPacketBodyRoot(s.Context(), s.Root), snapshot.State, id)
	return found, err
}

func renderStageAnswerHook(stdout io.Writer, path string, r submission.Receipt, stages ...string) int {
	if r.ResponsibilityStatus == "" {
		r.ResponsibilityStatus = "not_accepted"
	}
	if r.NextAction == "" {
		r.NextAction = "由 Main 恢复本次接收所需的控制状态。"
	}
	if path != "" {
		label := "Questionnaire: "
		if r.Accepted && r.WorkComplete || len(stages) > 0 && r.CurrentStage != "" && r.CurrentStage != stages[0] {
			label = "Submitted questionnaire (not the current pending answer): "
		}
		r.NextAction = label + path + ". " + r.NextAction
	}
	if r.CurrentStage == "" {
		r.CurrentStage = "unknown"
	}
	if r.DomainRefs == nil {
		r.DomainRefs = []string{}
	}
	if r.WaitingOn == nil {
		r.WaitingOn = []submission.Waiting{}
	}
	if r.FieldErrors == nil {
		r.FieldErrors = []workpacket.FieldError{}
	}
	data, err := json.Marshal(r)
	if err != nil {
		return 0
	}
	// A PostToolUse cannot undo a save. Rejection is feedback about domain
	// acceptance, never a platform deny or a synthetic completion fact.
	stage := "S5"
	if len(stages) > 0 {
		stage = stages[0]
	}
	_ = json.NewEncoder(stdout).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PostToolUse", "additionalContext": stage + " SUBMISSION RECEIPT: " + string(data)}})
	return 0
}

// Lifecycle boundaries recover short pending receptions and publish current
// projections. PreToolUse also publishes the current authoring question for
// graph-listener guidance, but does not run generic submission recovery.
func stageAnswerLifecycleContext(root string, request policy.Input) string {
	stopFeedback := request.Event == "Stop" || request.Event == "SubagentStop" || request.Event == "TeammateIdle"
	if request.Event != "SessionStart" && request.Event != "SubagentStart" && request.Event != "PreCompact" && request.Event != "PreToolUse" && !stopFeedback {
		return ""
	}
	if !request.Runtime.PacketMode {
		return ""
	}
	if request.Event == "SubagentStart" && request.EffectiveAgentID() == "" {
		return ""
	}
	// S2 feedback reads only its existing entry and answer. Entry completion
	// prepares the questionnaire; ordinary observation never issues it.
	if snapshot, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot(); err == nil {
		life, _ := snapshot.State["lifecycle"].(map[string]any)
		if life["state"] == "document_verification" && mainQuestionnaireViewer(request) {
			return stageS5ObservedContext(root, snapshot.State)
		}
		if life["state"] == "planning" && life["phase"] == "design" {
			if owner := request.EffectiveAgentID(); owner != "" && owner != "Main" {
				return ""
			} // Display the current Main responsibility, not a Worker task.
			return stageDesignQuestionnaireContext(root, snapshot)
		}
		if life["state"] == "planning" && (life["phase"] == "contracts" || life["phase"] == "tasks") {
			if owner := request.EffectiveAgentID(); owner != "" && owner != "Main" {
				return ""
			}
			return stageCurrentPlanningContext(root, snapshot)
		}
	}
	newSubmission := submission.New
	if request.Event == "PreToolUse" || stopFeedback {
		// The graph-listener read must not invoke the generic reception
		// recovery path. The original issuer still performs its own named
		// packet mutation after it reads the current snapshot.
		newSubmission = submission.NewReadOnly
	}
	s, err := newSubmission(root)
	if err != nil {
		return "stage-answer context deferred to Main: " + err.Error()
	}
	owner := request.EffectiveAgentID()
	ctx, cancel := context.WithTimeout(contextForHook(request), 4*time.Second)
	defer cancel()
	if request.Event == "SessionStart" || request.Event == "PreCompact" {
		if _, err := s.Recover(ctx, owner, 2); err != nil {
			return "S5 accepted work remains recorded; Main must recover the pending system followup: " + err.Error()
		}
	}
	snapshot, err := runtime.NewStore(s.StatePath, s.JournalPath).Snapshot()
	if err != nil {
		return "S7 ReviewResult context deferred to Main: " + err.Error()
	}
	lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
	if lifecycle["state"] == "planning" && (lifecycle["phase"] == "contracts" || lifecycle["phase"] == "tasks") {
		if owner != "" && owner != "Main" {
			return ""
		}
		return stageCurrentPlanningContext(root, snapshot)
	}

	if lifecycle["state"] == "planning" && (lifecycle["phase"] == "design" || lifecycle["phase"] == "contracts" || lifecycle["phase"] == "tasks") {
		if owner != "" && owner != "Main" {
			return "" // Planning belongs to Main; keep this Worker on its own responsibility.
		}
		if stopFeedback {
			path, issued, verified, err := currentIssuedPlanningAnswerPath(snapshot.State, lifecycle["phase"].(string))
			if err == nil && issued && verified {
				if lifecycle["phase"] == "contracts" {
					if receipt, err := s.ContractReceipt(path, ""); err == nil && receipt.Accepted {
						return "Current S3 answer: " + filepath.Join(root, filepath.FromSlash(path)) + ". Already received; do not resubmit. " + receipt.NextAction
					}
				}
				return "Current planning questionnaire: " + filepath.Join(root, filepath.FromSlash(path)) + ". Complete its professional answers and save; the listener handles stage progression."
			}
			return "The current planning questionnaire is not prepared; the next tool listener must provide its exact path. Main owns the preparation failure; do not invent a path or evidence envelope."
		}
		return stagePlanningQuestionnaireLifecycleContext(ctx, s, lifecycle["phase"].(string))
	}
	if lifecycle["state"] == "bug_resolution" && lifecycle["phase"] == "fixing" {
		return stageRepairResultQuestionnaireContext(ctx, root, request, s, snapshot, owner)
	}
	if lifecycle["state"] == "bug_resolution" && (lifecycle["phase"] == "planning" || lifecycle["phase"] == "reproducing") && runtime.PacketEnabled(snapshot.State) {
		return stageRepairPlanReportLifecycleContext(ctx, root, request, s, snapshot, owner)
	}
	if lifecycle["state"] == "bug_resolution" && lifecycle["phase"] == "investigation" {
		return stageHypothesisResultLifecycleContext(ctx, root, request, s, snapshot, owner)
	}
	if lifecycle["state"] == "verification" && runtime.PacketEnabled(snapshot.State) {
		return stageReviewResultLifecycleContext(ctx, root, request, s, snapshot, owner)
	}
	if (request.Event == "PreToolUse" || stopFeedback) && lifecycle["state"] == "building" {
		return stageBuilderQuestionLifecycleContext(root, request, snapshot)
	}
	if owner != "" && submission.CompletedOwner(root, owner) {
		return "S5 work_complete=true: your recorded document review has been received. Main owns system followup and later-stage dispatch; you can end this responsibility."
	}
	var packets []workpacket.Packet
	if stopFeedback {
		packets, err = runtime.S5Packets(root, snapshot.State, owner)
		if err == nil {
			issued := packets[:0]
			for _, packet := range packets {
				if _, found, lookupErr := runtime.FindWorkPacket(snapshot.State, packet.ID); lookupErr == nil && found {
					issued = append(issued, packet)
				}
			}
			packets = issued
		}
	} else {
		packets, err = s.IssueContext(ctx, owner)
	}
	if err != nil {
		return "S5 packet issuance deferred to Main: " + err.Error()
	}

	if len(packets) == 0 {
		if lifecycle["state"] == "document_verification" {
			if owner == "" || owner == "Main" {
				return stageS5PreparationGuidance(root, snapshot.State)
			}
			return fmt.Sprintf("S5 entry %d has no current questionnaire for %s. Main owns current review dispatch; keep historical answers unchanged and do not guess a new answer path.", stageAnswerEntry(lifecycle["phase_revision"]), owner)
		}
		return ""
	}
	var b strings.Builder
	for _, p := range packets {
		viewPath := filepath.Join(root, filepath.FromSlash(p.ViewPath))
		answerPath := filepath.Join(root, filepath.FromSlash(p.AnswerPath))
		registry, state, err := workspace.Load(root)
		if err != nil {
			fmt.Fprintf(&b, "S5 %s/%s: execution registry unavailable: %v\n", p.Binding.Owner, p.Binding.Responsibility, err)
			continue
		}
		if registry == nil && p.Binding.ExecutionRequired {
			fmt.Fprintf(&b, "S5 %s/%s: registered independent execution is missing.\n", p.Binding.Owner, p.Binding.Responsibility)
			continue
		}
		var e workspace.Execution
		declared := false
		if registry != nil {
			e, declared, err = workspace.ReviewExecution(state, p)
			if err != nil {
				fmt.Fprintf(&b, "S5 %s/%s: registered execution unavailable: %v\n", p.Binding.Owner, p.Binding.Responsibility, err)
				continue
			}
		}
		if !declared && p.Binding.ExecutionRequired {
			fmt.Fprintf(&b, "S5 %s/%s: registered independent execution is missing.\n", p.Binding.Owner, p.Binding.Responsibility)
			continue
		}
		if declared {
			if e.Status != "ready" {
				fmt.Fprintf(&b, "S5 %s/%s: Main must finish the independent execution; shared Main authoring is retired.\n", p.Binding.Owner, p.Binding.Responsibility)
				continue
			}
			submissionPaths, err := registry.WorkerSubmissionPaths(e)
			if err != nil {
				fmt.Fprintf(&b, "S5 %s/%s: frozen Worker authoring path unavailable: %v\n", p.Binding.Owner, p.Binding.Responsibility, err)
				continue
			}
			viewPath, answerPath = filepath.Join(e.Path, workpacket.WorkerViewPath), filepath.Join(e.Path, filepath.FromSlash(submissionPaths.Answer))
			if workspace.HasDispatchContext(e) {
				fmt.Fprintf(&b, "S5 %s/%s: first read %s; ", p.Binding.Owner, p.Binding.Responsibility, filepath.Join(e.Path, workspace.DispatchContextPath))
			}
		}
		if r, err := s.ReceiptContext(ctx, p.Binding.Owner, answerPath); err == nil && r.WorkComplete {
			fmt.Fprintf(&b, "S5 Reviewer %s/%s work_complete=true; questionnaire %s; answer %s; %s\n", p.Binding.Owner, p.Binding.Responsibility, viewPath, answerPath, r.NextAction)
			continue
		}
		if !declared {
			fmt.Fprintf(&b, "S5 Reviewer %s/%s: Main carries exact view %s and private answer %s into a separate native Agent context. Reviewer reads the actual frozen inputs, fills its professional answer, saves and returns that reference; SubagentStop invokes the original domain consumer. No workspace prepare/launch or generic activation is required.\n", p.Binding.Owner, p.Binding.Responsibility, viewPath, answerPath)
		} else {
			fmt.Fprintf(&b, "S5 Reviewer %s/%s: read %s; fill the professional answer in %s and save it. The frozen execution's original domain consumer returns the receipt.\n", p.Binding.Owner, p.Binding.Responsibility, viewPath, answerPath)
		}
	}
	return strings.TrimSpace(b.String())
}

func stageS5PreparationGuidance(root string, state map[string]any) string {
	life := mapFieldCLI(state, "lifecycle")
	return fmt.Sprintf("S5 entry %d: DV-SPEC-CONSISTENCY and DV-TASK-EXECUTABILITY 原独立责任尚未准备完成。两项责任及 manifest/issue 由系统原入口自动准备；Main 不手工组装或登记。Main 恢复原准备：%s；manifest %s。保留 accepted S4 及已发行问卷和答案，不重交 S4；普通读取与 Stop 不补登记。方法入口 %s；准备后携带准确 view/answer/source root 派入两个独立原生上下文，登记不证明平台已启动。", stageAnswerEntry(life["phase_revision"]), submission.PlanningRecoveryCommand(root, "issue"), s5ManifestQuestionnairePath(root, state), filepath.Join(root, ".claude/skills/team-planning/SKILL.md")) + submission.PlanningDiagnostic(root, state, "preparation")
}

// stageDesignQuestionnaireContext projects current work, not issuance proof.
func stageDesignQuestionnaireContext(root string, snapshot runtime.Snapshot) string {
	life, _ := snapshot.State["lifecycle"].(map[string]any)
	baseline, _ := snapshot.State["baseline"].(map[string]any)
	entities, _ := snapshot.State["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["template_id"] != "s2-design-v1" {
			continue
		}
		packet, found, err := runtime.FindDesignPacket(snapshot.State, stringValue(row["packet_id"]))
		if err != nil || !found || packet.Binding.RuntimeID != snapshot.State["runtime_id"] || packet.Binding.Generation != integerValue(baseline["generation"]) || packet.Binding.StageEntry != integerValue(life["phase_revision"]) {
			continue
		}
		view := filepath.Join(root, packet.ViewPath)
		answer := filepath.Join(root, packet.AnswerPath)
		if record, accepted, err := runtime.FindSubmission(snapshot.State, packet.ID); err == nil && accepted {
			service := &submission.Service{Root: root, StatePath: filepath.Join(root, ".claude/loop-state.json"), JournalPath: filepath.Join(root, ".claude/loop-events.jsonl")}
			if receipt, err := service.DesignReceiptForSnapshot(snapshot, packet.ID); err == nil {
				message := "S2 design report is already recorded at " + answer + ". " + receipt.NextAction
				if record.ControlStatus == "pending" || record.ControlStatus == "no_transition" {
					bound, _ := snapshot.State["bound_req"].(map[string]any)
					metadata, _ := bound["metadata"].(map[string]any)
					if metadata["ui_impact"] == "unknown" {
						message += " Current actual gap: REQ UI impact remains unknown; resolve that professional question before progressing."
					}
				}
				return message + stageDesignSourceDelivery(snapshot.State) + stageDesignScopeContext(root, snapshot.State)
			}
			return "S2 design report is already recorded. Its current material reference cannot be read; Main must restore that material, not repeat the report."
		}
		var missing []string
		missingFiles := false
		for _, path := range []string{view, answer} {
			if _, err := os.Stat(path); err != nil {
				missingFiles = true
				missing = append(missing, path+": "+err.Error())
			}
		}
		data, err := os.ReadFile(answer)
		if err == nil {
			professional, issues := workpacket.ValidateDesignAnswer(packet, data)
			if len(issues) > 0 {
				missing = append(missing, issues[0].Pointer+": "+issues[0].Expected)
			} else {
				if professional.Conclusion == nil || strings.TrimSpace(*professional.Conclusion) == "" {
					missing = append(missing, "actual self-review conclusion is unfinished; leave it empty until ready")
				}
				if professional.ArchitecturePath == nil || *professional.ArchitecturePath == "" {
					missing = append(missing, "actual architecture_path is unanswered")
				} else if _, err := runtime.DesignArchitecture(root, *professional.ArchitecturePath, nil); err != nil {
					missing = append(missing, err.Error())
				}
			}
		}
		bound, _ := snapshot.State["bound_req"].(map[string]any)
		metadata, _ := bound["metadata"].(map[string]any)
		if metadata["ui_impact"] == "unknown" {
			missing = append(missing, "current REQ UI impact remains unknown; resolve the actual design/requirement question before progressing")
		}
		recovery := "No routine issue/register/recovery command is required."
		if missingFiles {
			recovery = "The registered files above are missing. A successful material change retries the original preparation; for this technical gap, run " + submission.PlanningRecoveryCommand(root, "issue") + " to restore missing files while preserving existing answers. Do not construct a replacement questionnaire."
		}
		delivery := stageDesignSourceDelivery(snapshot.State)
		return "S2 current design questionnaire: read " + view + "; fill only conclusion and architecture_path in " + answer + " and save. Complete the actual architecture, relevant ADR/human direction judgment and applicable module/scenario work described in the view. Templates: " + filepath.Join(root, "docs/architecture/ARCHITECTURE-template.md") + ", " + filepath.Join(root, "docs/design/decisions/ADR-template.md") + ", " + filepath.Join(root, "docs/design/prototypes/README.md") + ". Current missing work/material: " + strings.Join(missing, "; ") + ". Empty conclusion is a draft, not failure or PASS. " + recovery + delivery + stageDesignScopeContext(root, snapshot.State)
	}
	return "S2 entry questionnaire has not been prepared by the system entry outlet. A successful material change retries the original entry; for this technical gap, run " + submission.PlanningRecoveryCommand(root, "issue") + " to prepare its actual view/answer paths. Do not guess a path or invent evidence. Existing requirement material: " + filepath.Join(root, stringValue(mapFieldCLI(snapshot.State, "bound_req")["path"])) + "."
}

func stagePlanningQuestionnaireLifecycleContext(ctx context.Context, service *submission.Service, phase string) string {
	var stage, viewPath, answerPath, delivery string
	var preparationErr error
	snapshot, snapshotErr := runtime.NewStore(service.StatePath, service.JournalPath).Snapshot()
	if phase == "contracts" || phase == "tasks" {
		defer func() {
			if snapshotErr == nil {
				message := ""
				if preparationErr != nil {
					message = preparationErr.Error()
				}
				submission.RecordPlanningDiagnostic(service.Root, snapshot.State, "preparation", message)
			}
		}()
	}
	switch phase {
	case "design":
		if snapshotErr != nil {
			return "S2 current design context could not be read: " + snapshotErr.Error()
		}
		return stageDesignQuestionnaireContext(service.Root, snapshot)
	case "contracts":
		packet, err := service.IssueContracts(ctx)
		preparationErr = err
		if err != nil {
			if packet.AnswerPath != "" {
				return fmt.Sprintf("S3 original questionnaire: read %s; answer %s. Preparation needs repair: %s. Keep the original answer; do not invent a new path or envelope.", filepath.Join(service.Root, filepath.FromSlash(packet.ViewPath)), filepath.Join(service.Root, filepath.FromSlash(packet.AnswerPath)), err)
			}
			return "S3 Contracts questionnaire could not be ensured from the current Main planning state: " + err.Error()
		}
		stage, viewPath, answerPath = "S3 Contracts", packet.ViewPath, packet.AnswerPath
		if receipt, err := service.ContractReceipt("", packet.ID); err == nil && receipt.Accepted {
			return fmt.Sprintf("S3 current questionnaire: %s; answer: %s. Already received; do not resubmit. %s", filepath.Join(service.Root, filepath.FromSlash(viewPath)), filepath.Join(service.Root, filepath.FromSlash(answerPath)), receipt.NextAction)
		}
		delivery = contractDeliveryGuidance(service.Root, packet, service.Files)
	case "tasks":
		packet, err := service.IssueTaskPlanning(ctx)
		preparationErr = err
		if err != nil {
			if packet.AnswerPath != "" {
				return fmt.Sprintf("S4 original questionnaire: read %s; answer %s. Preparation needs repair: %s. Keep the original answer; do not invent a new path or envelope.", filepath.Join(service.Root, filepath.FromSlash(packet.ViewPath)), filepath.Join(service.Root, filepath.FromSlash(packet.AnswerPath)), err)
			}
			return "S4 Task Planning questionnaire could not be ensured from the current Main planning state: " + err.Error()
		}
		stage, viewPath, answerPath = "S4 Task Planning", packet.ViewPath, packet.AnswerPath
		if receipt, err := service.TaskPlanningReceipt("", packet.ID); err == nil && receipt.Accepted {
			return fmt.Sprintf("S4 current questionnaire: %s; answer: %s. Already received; do not resubmit. %s", filepath.Join(service.Root, filepath.FromSlash(viewPath)), filepath.Join(service.Root, filepath.FromSlash(answerPath)), receipt.NextAction)
		}
		delivery = taskPlanningDeliveryGuidance(service.Root, packet, service.Files)
	default:
		return ""
	}
	// The packet API returns the exact paths in the original issuance. Resolve
	// those paths against the canonical Main root instead of guessing from the
	// lifecycle phase or the current working directory.
	view := filepath.Join(service.Root, filepath.FromSlash(viewPath))
	answer := filepath.Join(service.Root, filepath.FromSlash(answerPath))
	return fmt.Sprintf("%s current questionnaire: read %s, fill only the answer in %s, then save it. The original save Hook records the domain receipt.%s", stage, view, answer, delivery)
}

func taskPlanningDeliveryGuidance(root string, packet workpacket.TaskPlanningPacket, files fileview.Reader) string {
	return submission.TaskPlanningDeliveryGuidance(root, packet, files, nil)
}

func contractDeliveryGuidance(root string, packet workpacket.ContractPacket, files fileview.Reader) string {
	return submission.ContractDeliveryGuidance(root, packet, files, nil)
}

type s6BuilderQuestionnaire struct {
	AgentID, AssignmentID                         string
	TaskID                                        string
	ContextPath, SchemaPath, PlanPath, ResultPath string
	PlatformSessionID                             string
	WorkerRoot                                    string
	PlanQuestions, ResultQuestions                map[string]string
	Workspace                                     bool
	CanonicalRecord                               bool
}

func projectS6BuilderQuestionnaires(root, owner string, snapshot runtime.Snapshot) ([]s6BuilderQuestionnaire, []string) {
	var questions []s6BuilderQuestionnaire
	var notices []string
	lifecycle, _ := snapshot.State["lifecycle"].(map[string]any)
	if lifecycle["state"] != "building" {
		return nil, nil
	}
	mainSession := owner == "" || owner == "Main"
	registry, err := workspace.Decode(snapshot.State)
	if err != nil {
		return nil, []string{"S6 current Builder question lookup deferred to Main: " + err.Error()}
	}
	workspaceAssignments := map[string]bool{}
	if registry != nil {
		for _, e := range registry.Executions {
			if e.Status != "ready" || e.RuntimeID != workspace.RuntimeID(snapshot.State) || e.BaselineGeneration != workspace.Generation(snapshot.State) || e.Purpose == workspace.DocumentReviewPurpose || (!mainSession && e.AgentID != owner) {
				continue
			}
			workspaceAssignments[e.AssignmentID] = true
			paths, err := registry.WorkerSubmissionPaths(e)
			if err != nil {
				notices = append(notices, fmt.Sprintf("S6 Builder %s assignment=%s: frozen submission paths unavailable: %v", e.AgentID, e.AssignmentID, err))
				continue
			}
			q := s6BuilderQuestionnaire{AgentID: e.AgentID, AssignmentID: e.AssignmentID, Workspace: true, WorkerRoot: e.Path,
				PlatformSessionID: e.PlatformSessionID,
				CanonicalRecord:   e.AssignmentRecord != nil || e.AssignmentRecordSHA256 != "",
				ContextPath:       filepath.Join(e.Path, filepath.FromSlash(workspace.DispatchContextPath)), SchemaPath: filepath.Join(e.Path, filepath.FromSlash(workspace.PlanSchemaPath))}
			if paths.Plan != "" {
				q.PlanPath = filepath.Join(e.Path, filepath.FromSlash(paths.Plan))
			}
			if paths.Completion != "" {
				q.ResultPath = filepath.Join(e.Path, filepath.FromSlash(paths.Completion))
			}
			if workspace.HasDispatchContext(e) {
				dispatch, err := registry.ReadDispatchContext(e)
				if err != nil {
					notices = append(notices, fmt.Sprintf("S6 Builder %s assignment=%s: frozen questionnaire context unavailable: %v", e.AgentID, e.AssignmentID, err))
					continue
				}
				q.TaskID = dispatch.Dispatch.TaskID
				q.PlanQuestions, q.ResultQuestions = dispatch.PlanQuestions, dispatch.ResultQuestions
			}
			if questions, err := currentS6ResultQuestions(snapshot, e.AgentID); err == nil {
				q.ResultQuestions = questions
			}
			questions = append(questions, q)
		}
	}
	lookupOwner := owner
	if mainSession {
		lookupOwner = ""
	}
	loaded, err := hookctx.FromSnapshot(root, lookupOwner, snapshot)
	if err == nil && loaded != nil {
		for _, a := range loaded.Assignments {
			if record, found, _ := assignmentrecord.ForOwner(snapshot.State, a.OwnerAgentID); found && record.Contract == assignmentrecord.NativeDeliveryContract {
				continue
			}
			if (!mainSession && a.OwnerAgentID != owner) || workspaceAssignments[a.AssignmentID] {
				continue
			}
			binding, found, err := decodeContextBinding(contextAgent(snapshot.State, a.OwnerAgentID))
			if err != nil {
				notices = append(notices, fmt.Sprintf("S6 native Builder %s assignment=%s: frozen questionnaire context unavailable: %v", a.OwnerAgentID, a.AssignmentID, err))
				continue
			}
			if !found {
				continue
			}
			c, err := verifyFixedContext(root, snapshot.State, a, binding)
			if err != nil {
				notices = append(notices, fmt.Sprintf("S6 native Builder %s assignment=%s: frozen questionnaire context unavailable: %v", a.OwnerAgentID, a.AssignmentID, err))
				continue
			}
			q := s6BuilderQuestionnaire{AgentID: a.OwnerAgentID, AssignmentID: a.AssignmentID, TaskID: a.TaskID, WorkerRoot: root,
				CanonicalRecord: a.AssignmentRecord != nil || a.AssignmentRecordSHA256 != "",
				ContextPath:     filepath.Join(root, filepath.FromSlash(binding.Context.Path)), SchemaPath: filepath.Join(root, filepath.FromSlash(c.Schema.Path)),
				PlanQuestions: c.PlanQuestions, ResultQuestions: c.ResultQuestions}
			if c.PlanPath != "" {
				q.PlanPath = filepath.Join(root, filepath.FromSlash(c.PlanPath))
			}
			if c.ResultPath != "" {
				q.ResultPath = filepath.Join(root, filepath.FromSlash(c.ResultPath))
			}
			if questions, err := currentS6ResultQuestions(snapshot, a.OwnerAgentID); err == nil {
				q.ResultQuestions = questions
			}
			questions = append(questions, q)
		}
	}
	return questions, notices
}

func stageBuilderQuestionLifecycleContext(root string, request policy.Input, snapshot runtime.Snapshot) string {
	owner := request.EffectiveAgentID()
	mainSession := owner == "" || owner == "Main"
	questions, notices := projectS6BuilderQuestionnaires(root, owner, snapshot)
	if mainSession && len(questions) == 0 && len(nativeS6Records(snapshot.State)) > 0 {
		return nativeS6MainGuidance(root, snapshot.State)
	}
	var b strings.Builder
	if mainSession {
		if native := nativeS6MainGuidance(root, snapshot.State); native != "" {
			fmt.Fprintln(&b, native)
		}
	}
	for _, notice := range notices {
		fmt.Fprintln(&b, notice)
	}
	for _, q := range questions {
		activity := workstate.AgentActivity(contextAgent(snapshot.State, q.AgentID))
		if mainSession && activity != "done" && activity != "stopped" && activity != "reported" && activity != "blocked" {
			fmt.Fprintf(&b, "S6 registered Builder %s assignment=%s", q.AgentID, q.AssignmentID)
			if q.TaskID != "" {
				fmt.Fprintf(&b, " TASK=%s", q.TaskID)
			}
			if q.PlanPath != "" {
				fmt.Fprintf(&b, ": PLAN path %s", q.PlanPath)
			}
			if q.ResultPath != "" {
				separator := ";"
				if q.PlanPath == "" {
					separator = ":"
				}
				fmt.Fprintf(&b, "%s Result path %s", separator, q.ResultPath)
			}
			if q.Workspace && q.PlatformSessionID != "" {
				launchLogDir := filepath.Join(root, ".claude/workspace-launch", q.PlatformSessionID)
				fmt.Fprintf(&b, ". The original workspace execution has recorded platform session %s; this ID is reserved before process start. Main tracks the original launch under %s and the current Builder receipt. Wait while that original process runs; only if it stopped before reporting, resume the same session with `runtime workspace launch --root %q --assignment %s --agent %s --resume`. Do not start a second Agent for TASK %s. The registered Builder owns TASK implementation and both questionnaire answers.\n", q.PlatformSessionID, launchLogDir, root, q.AssignmentID, q.AgentID, q.TaskID)
			} else if q.Workspace {
				fmt.Fprintf(&b, ". No platform session is recorded yet. Main starts this ready execution once with `runtime workspace launch --root %q --assignment %s --agent %s`; the launch supplies its exact questionnaire paths. The registered Builder owns TASK implementation and both answers.\n", root, q.AssignmentID, q.AgentID)
			} else {
				b.WriteString(". Main coordinates the registered Builder execution and waits for its original answer receipt; the Builder owns TASK implementation and both answers.\n")
			}
			continue
		}
		label := "Builder"
		if !q.Workspace {
			label = "native Builder"
		}
		fmt.Fprintf(&b, "S6 %s %s assignment=%s: read %s and %s.", label, q.AgentID, q.AssignmentID, q.ContextPath, q.SchemaPath)
		if activity == "done" {
			if progress, _ := s6AcceptedResultProgress(root, snapshot, q); progress != "" {
				fmt.Fprintf(&b, " %s\n", progress)
			} else {
				fmt.Fprintf(&b, " This responsibility is marked done, but its original Builder receipt could not be verified. Main owns receipt inspection and the next action; no further Result should be issued at %s.\n", q.ResultPath)
			}
			continue
		}
		if activity == "stopped" {
			fmt.Fprintf(&b, " This original Builder responsibility is stopped. Do not submit its historical Result path %s; Main owns the next action.\n", q.ResultPath)
			continue
		}
		if activity == "reported" {
			if progress, needsRevision := s6AcceptedResultProgress(root, snapshot, q); needsRevision {
				fmt.Fprintf(&b, " %s\n", progress)
			} else if progress != "" {
				fmt.Fprintf(&b, " %s\n", progress)
			} else {
				fmt.Fprintf(&b, " Original Result activity is reported at %s, but its accepted receipt could not be read. Main owns receipt inspection and integration; do not issue another Result.\n", q.ResultPath)
			}
			continue
		}
		if activity == "blocked" {
			if progress, _ := s6AcceptedResultProgress(root, snapshot, q); progress != "" {
				fmt.Fprintf(&b, " %s\n", progress)
			} else {
				fmt.Fprintf(&b, " Responsibility is blocked; follow its recorded blocker and Main's next action. PLAN: %s; Result: %s.\n", q.PlanPath, q.ResultPath)
			}
			continue
		}

		if q.PlanPath != "" && activity != "working" {
			if q.PlanQuestions != nil {
				fmt.Fprintf(&b, " Fill PLAN using plan_questions in %s", q.ContextPath)
			} else {
				b.WriteString(" Fill the registered PLAN")
			}
			fmt.Fprintf(&b, " and save the exact draft %s; the listener submits it to the original begin/activation flow.", q.PlanPath)
		}
		if q.ResultPath != "" {
			if q.ResultQuestions != nil {
				fmt.Fprintf(&b, " After the accepted PLAN and actual TASK work, fill Result using result_questions in %s", q.ContextPath)
			} else {
				b.WriteString(" After the accepted PLAN and actual TASK work, fill the registered Result")
			}
			if q.Workspace {
				fmt.Fprintf(&b, " and save the exact draft %s; the save listener calls the original report adapter automatically.", q.ResultPath)
			} else {
				fmt.Fprintf(&b, " and save the exact draft %s; the save listener calls the original TASK completion adapter automatically.", q.ResultPath)
			}
			if mapping := s6ResultPlanMapping(root, snapshot, q); mapping != "" {
				fmt.Fprintf(&b, " %s", mapping)
			}
		}
		b.WriteByte('\n')
	}
	if strings.TrimSpace(b.String()) == "" {
		if request.EffectiveAgentID() == "" || request.EffectiveAgentID() == "Main" {
			if entry := s6EntryPreparationContext(root, snapshot.State, request); entry != "" {
				return entry
			}
			return "S6 has no current prepared Builder questionnaire awaiting an answer. Main checks `loop-harness s6 status --advice`, selects an eligible TASK and prepares it with `loop-harness s6 prepare --root <Main-root> --task <TASK> --role <Builder-role>`. Preparation performs the original registration and supplies the exact brief for native Agent/worktree dispatch; no separate register-workgroup step is needed. No Builder has been inferred to be running, and no answer path was guessed. Received deliveries retain their original integration step."
		}
		return fmt.Sprintf("S6 has no current registered Builder questionnaire for %s. Main should inspect the original TASK registration and dispatch; no Builder answer path was inferred.", request.EffectiveAgentID())
	}
	return strings.TrimSpace(b.String())
}

func currentS6ResultQuestions(snapshot runtime.Snapshot, owner string) (map[string]string, error) {
	record, found, err := assignmentrecord.ForOwner(snapshot.State, owner)
	if err != nil || !found {
		return nil, fmt.Errorf("current Assignment Record unavailable: %v", err)
	}
	dispatch, err := agentcontext.ApplyRecord(agentcontext.Assignment{}, record)
	if err != nil {
		return nil, err
	}
	_, questions := agentcontext.ResultTemplate(dispatch, record.RuntimeID, time.Now().UTC().Format(time.RFC3339Nano))
	if questions == nil {
		return nil, fmt.Errorf("current Assignment has no canonical Builder Result")
	}
	return questions, nil
}

func s6ResultPlanMapping(root string, snapshot runtime.Snapshot, q s6BuilderQuestionnaire) string {
	record, found, err := assignmentrecord.ForOwner(snapshot.State, q.AgentID)
	if err != nil || !found || record.AssignmentID != q.AssignmentID || record.ClosingContract == nil {
		return ""
	}
	planRef, err := plancheckpoint.ActivatedRef(root, snapshot, q.AgentID)
	if err != nil || planRef == "" {
		return ""
	}
	data, err := workpacket.ReadFile(root, planRef, 1<<20)
	if err != nil {
		return ""
	}
	var plan struct {
		Assertions []assignmentresult.PlanAssertion `json:"assertion_checks"`
	}
	if json.Unmarshal(data, &plan) != nil || len(plan.Assertions) == 0 {
		return ""
	}
	var mapped []string
	for doneWhenIndex, condition := range record.DoneWhen {
		var indexes []string
		for assertionIndex, assertion := range plan.Assertions {
			for _, index := range assertion.DoneWhenIndexes {
				if index == doneWhenIndex {
					indexes = append(indexes, fmt.Sprint(assertionIndex))
					break
				}
			}
		}
		if len(indexes) == 0 {
			return ""
		}
		mapped = append(mapped, fmt.Sprintf("DoneWhen %d (%s) is already mapped to accepted PLAN assertion_checks[%s].", doneWhenIndex, condition, strings.Join(indexes, ",")))
	}
	if len(mapped) == 0 {
		return ""
	}
	return "The Result draft carries these original machine mappings: " + strings.Join(mapped, " ") + " Author the actual disposition, each condition judgment, and rationale; do not copy or change the indexes."
}

func s6AcceptedResultProgress(root string, snapshot runtime.Snapshot, q s6BuilderQuestionnaire) (progress string, needsRevision bool) {
	activity := workstate.AgentActivity(contextAgent(snapshot.State, q.AgentID))
	waiting := fmt.Sprintf("The original Result is recorded at %s. Main owns the original integration, checks and acknowledgement; do not issue another Result unless the original rework flow permits it.", q.ResultPath)
	if !q.CanonicalRecord {
		return waiting, false
	}
	record, found, err := assignmentrecord.ForOwner(snapshot.State, q.AgentID)
	if err != nil || !found || record.AssignmentID != q.AssignmentID {
		return waiting, false
	}
	checkpointPath := assignmentrecord.IntegrationCheckpointPath(root, record.Reference())
	var checkpoint struct {
		State         string `json:"state"`
		FailureReason string `json:"failure_reason"`
	}
	if data, err := workpacket.ReadFile(root, assignmentrecord.IntegrationCheckpointPath("", record.Reference()), assignmentresult.EvidenceReadLimit); err == nil && json.Unmarshal(data, &checkpoint) == nil && checkpoint.FailureReason != "" {
		defer func() {
			progress += fmt.Sprintf(" Main checkpoint %s records %s: %s. Preserve this failure scene; Main owns the original recovery. Do not resubmit the accepted Result. Source fixes require Main's original revision/rework flow; retry only when the original retry conditions permit it.", checkpointPath, checkpoint.State, checkpoint.FailureReason)
		}()
	}
	read := func(ref string) ([]byte, error) {
		return workpacket.ReadFile(root, ref, assignmentresult.EvidenceReadLimit)
	}
	history, err := assignmentresult.AcceptedHistory(snapshot.State, record, read)
	if err != nil || len(history) == 0 {
		return waiting + " The current accepted professional answer is unavailable; Main owns inspection.", false
	}
	accepted := history[len(history)-1]
	if record.ClosingContract == nil || accepted.Answer.ClosingAssessment == nil {
		return fmt.Sprintf("The accepted Result at %s has no recorded professional closing assessment under its original contract. Main owns inspection and the original integration/acknowledgement decision. This reminder does not request new answer fields or another Result.", q.ResultPath), false
	}
	var pending []string
	assessment := accepted.Answer.ClosingAssessment
	if assessment.Disposition != "complete" {
		pending = append(pending, "accepted professional disposition is "+assessment.Disposition)
	} else if !assignmentresult.ClosingAssessmentComplete(accepted.Answer) {
		pending = append(pending, "professional closing conditions are incomplete")
	}
	for _, condition := range assessment.Conditions {
		if condition.Judgment != "satisfied" {
			pending = append(pending, fmt.Sprintf("DoneWhen %d is %s: %s", condition.DoneWhenIndex, condition.Judgment, condition.Rationale))
		}
	}
	for _, risk := range accepted.Answer.RisksBlockers {
		pending = append(pending, "risk/blocker: "+risk)
	}
	for _, deviation := range accepted.Answer.ScopeDeviations {
		pending = append(pending, "scope deviation: "+deviation)
	}
	for _, check := range record.ClosingContract.WorkerChecks {
		status := "missing"
		for _, fact := range accepted.WorkerCheckFacts {
			if fact.Index == check.Index && fact.Command == check.Command {
				status = fact.Status
				break
			}
		}
		if status != "pass" {
			pending = append(pending, fmt.Sprintf("WorkerCheck %d is %s (%s)", check.Index, status, check.Command))
		}
	}
	if len(pending) > 0 {
		return fmt.Sprintf("The accepted Result at %s has recorded unfinished items: %s. Main owns the original source rework/revision decision. Do not continue editing the submitted source; use this exact Result path only for a professional answer correction that the original receiver still permits.", q.ResultPath, strings.Join(pending, "; ")), true
	}
	if activity == "done" {
		return fmt.Sprintf("The accepted Result at %s has no recorded professional or WorkerCheck gaps, and Runtime records Main acknowledgement. No further Result is needed unless Main returns this responsibility through the original rework flow.", q.ResultPath), false
	}
	return fmt.Sprintf("The accepted Result at %s has no recorded professional or WorkerCheck gaps; waiting for Main's original integration, checks and acknowledgement. No further Result is needed unless Main returns this responsibility through the original rework flow.", q.ResultPath), false
}

func stageRepairPlanReportLifecycleContext(ctx context.Context, root string, request policy.Input, service *submission.Service, snapshot runtime.Snapshot, owner string) string {
	return s9PlanQuestionnaireContext(ctx, root, request, service, snapshot, owner)
}

func stageHypothesisResultLifecycleContext(ctx context.Context, root string, request policy.Input, service *submission.Service, snapshot runtime.Snapshot, owner string) string {
	return s8HypothesisQuestionnaireContext(ctx, root, request, service, snapshot, owner)
}

func hypothesisResultLifecyclePaths(ctx context.Context, root string, state map[string]any, packet workpacket.HypothesisResultPacket, cwd, sessionID string) (string, string, error) {
	source, err := questionnaireSourceRoot(root, state, packet.Binding.AssignmentID, packet.Binding.OwnerAgentID, packet.Binding.RuntimeID, packet.Binding.BaselineGeneration)
	if err != nil {
		return "", "", err
	}
	return filepath.Join(source, filepath.FromSlash(packet.ViewPath)), filepath.Join(source, filepath.FromSlash(packet.AnswerPath)), nil
}

func stageReviewResultLifecycleContext(ctx context.Context, root string, request policy.Input, service *submission.Service, snapshot runtime.Snapshot, owner string) string {
	mainSession := owner == "" || owner == "Main"
	if mainSession {
		switch request.Event {
		case "SessionStart", "SubagentStart", "PreCompact", "PreToolUse", "Stop", "SubagentStop", "TeammateIdle":
		default:
			return ""
		}
		if request.CWD == "" {
			return "S7 issuance requires the platform's Main project-root cwd."
		}
		cwd, err := workspace.Canonical(request.CWD)
		mainRoot, rootErr := workspace.Canonical(root)
		if err != nil || rootErr != nil || cwd != mainRoot {
			return "S7 issuance is available only from the registered Main project root."
		}
		if ptr := review.PlanPointerFromState(snapshot.State); ptr == nil {
			return "S7 Main: the current ReviewPlan is not registered yet. Fill and save the exact Main ReviewPlan questionnaire supplied by the listener; Reviewer questions are issued after the original consumer registers it."
		} else if ptr.Status == "stale" {
			planPath := ptr.Path
			if !filepath.IsAbs(planPath) {
				planPath = filepath.Join(mainRoot, filepath.FromSlash(planPath))
			}
			questionnaire := review.PlanQuestionnairePath(mainRoot, snapshot.State)
			journal, _ := snapshot.State["journal"].(map[string]any)
			journalPath := stringValue(journal["path"])
			if journalPath == "" {
				journalPath = ".claude/loop-events.jsonl"
			}
			if !filepath.IsAbs(journalPath) {
				journalPath = filepath.Join(mainRoot, filepath.FromSlash(journalPath))
			}
			last, _ := snapshot.State["last_transition"].(map[string]any)
			eventID := stringValue(last["event_id"])
			feedback := "Runtime records status=stale; original feedback: " + journalPath + " event=" + eventID
			return fmt.Sprintf("S7 Main: original ReviewPlan %s is stale. Main questionnaire: %s. Original system feedback: %s. Main resolves the original baseline/Plan recovery through existing revive/revise; queued dispatch and old answers await a valid plan.", planPath, questionnaire, feedback) + " " + s7PinnedSourceRecovery(root)
		}
		if ptr := review.PlanPointerFromState(snapshot.State); ptr != nil {
			switch ptr.Status {
			case "observation_sealed":
				return "S7 ObservationBatch is sealed. The original handoff continues TR-008 and prepares the S8 intake; Main fills that supplied intake once ready; no further Reviewer dispatch belongs to this round."
			case "paused", "clean":
				return "S7 ReviewPlan is " + ptr.Status + "; Main follows its original pause or stage-exit route. Do not dispatch the old queue."
			}
		}
		for attempt := 0; attempt < 2; attempt++ {
			packets, err := runtime.ProjectCurrentReviewResultPacketIndex(snapshot.State, "")
			if err != nil {
				return "S7 Main ReviewResult lookup deferred: " + err.Error()
			}
			reviewState, _ := snapshot.State["review"].(map[string]any)
			assignments, _ := reviewState["assignments"].(map[string]any)
			seen := map[string]bool{}
			needIssueOwner := ""
			var b strings.Builder
			completed := 0
			fmt.Fprintln(&b, "S7 Main current ReviewResult responsibilities:")
			for _, packet := range packets {
				assignmentID := packet.Binding.AssignmentID
				seen[assignmentID] = true
				row, _ := assignments[assignmentID].(map[string]any)
				activity := workstate.ReviewAssignmentActivity(row)
				if activity == "consumed" {
					completed++
					continue
				}
				if diagnostic := submission.PlanningDiagnostic(root, snapshot.State, nativeS7DiagnosticKind(packet.ID)); diagnostic != "" {
					fmt.Fprintln(&b, nativeS7IndexReturnDiagnostic(root, snapshot.State, packet))
				}
				view, answer, err := reviewResultIndexLifecyclePaths(root, snapshot.State, packet)
				if err != nil {
					fmt.Fprintf(&b, "S7 %s/%s: questionnaire preparation gap: %s\n", packet.Binding.OwnerAgentID, assignmentID, err)
					continue
				}

				if activity != "dispatched" {
					// The shared pending projection below includes blocked owners,
					// their exact original source and recovery entry once.
					continue
				}
				ready := true
				for _, path := range []string{view, answer} {
					info, err := os.Stat(path)
					if err == nil && info.Mode().IsRegular() {
						continue
					}
					ready = false
					if os.IsNotExist(err) {
						if needIssueOwner == "" {
							needIssueOwner = packet.Binding.OwnerAgentID
						}
					}
					fmt.Fprintf(&b, "S7 %s/%s: questionnaire preparation gap at %s (missing or unreadable regular file: %v).\n", packet.Binding.OwnerAgentID, assignmentID, path, err)
				}
				if ready {
					source, err := questionnaireSourceRoot(root, snapshot.State, packet.Binding.AssignmentID, packet.Binding.OwnerAgentID, packet.Binding.RuntimeID, packet.Binding.BaselineGeneration)
					if err != nil {
						fmt.Fprintf(&b, "S7 assignment=%s: Main restores the original source: %s\n", assignmentID, err)
						continue
					}
					fullHandoff := attempt > 0 || request.Event == "SessionStart" || request.Event == "PreCompact" || request.Event == "SubagentStart"
					b.WriteString(s7ReviewerIndexTaskGuidance(mainRoot, source, view, answer, packet, fullHandoff))
				}
			}
			if completed > 0 {
				fmt.Fprintf(&b, "S7 accepted Reviewer responsibilities: %d; no repeat answer is required. Main owns the remaining round/handoff work.\n", completed)
			}
			b.WriteString(s7PendingReviewGuidance(mainRoot, snapshot.State))
			for assignmentID, raw := range assignments {
				row, _ := raw.(map[string]any)
				if !seen[assignmentID] && workstate.ReviewAssignmentActivity(row) == "dispatched" {
					if needIssueOwner == "" {
						needIssueOwner = stringValue(row["agent_id"])
					}
					fmt.Fprintf(&b, "S7 assignment=%s: current dispatched responsibility has no questionnaire; Main must restore its preparation.\n", assignmentID)
				}
			}
			if needIssueOwner == "" || attempt == 1 || request.Event != "PreToolUse" {
				return strings.TrimSpace(b.String())
			}
			if ctx != nil && ctx.Err() != nil {
				return strings.TrimSpace(b.String()) + "\nS7 Main questionnaire preparation deferred for " + needIssueOwner + ": " + ctx.Err().Error()
			}
			if service == nil {
				// Deferred initialization still uses the original issuer and its
				// recovery/validation path; nil is only a read-only projection.
				service, err = submission.New(root)
				if err != nil {
					return strings.TrimSpace(b.String()) + "\nS7 Main questionnaire preparation deferred for " + needIssueOwner + ": " + err.Error()
				}
			}
			// The original issuer already filters to registered, dispatchable
			// responsibilities. Prepare the complete ready set in this one
			// handoff, rather than requiring another tool event per owner.
			if _, err := service.IssueReviewResults(ctx, ""); err != nil {
				return strings.TrimSpace(b.String()) + "\nS7 Main questionnaire preparation deferred for " + needIssueOwner + ": " + err.Error()
			}
			snapshot, err = runtime.NewStore(service.StatePath, service.JournalPath).Snapshot()
			if err != nil {
				return "S7 Main questionnaire preparation needs a current Runtime snapshot: " + err.Error()
			}
		}
		return "S7 Main: no current ReviewResult responsibility is ready."
	}

	loaded, err := hookctx.FromSnapshot(root, owner, snapshot)
	if err != nil || loaded == nil || loaded.PolicyContext.Agent == nil || !loaded.PolicyContext.Agent.ReviewAssignment || loaded.PolicyContext.Agent.ID != owner {
		// S7 is not a general work-packet channel. An unrelated SubagentStart
		// keeps its original lifecycle guidance and receives no Reviewer packet.
		return ""
	}
	assignmentID := loaded.PolicyContext.Agent.AssignmentID
	packets, err := runtime.CurrentReviewResultPacketsContext(runtime.WithPacketBodyRoot(ctx, root), snapshot.State, owner)
	if err != nil {
		return "S7 ReviewResult lookup deferred to Main: " + err.Error()
	}
	var packet *workpacket.ReviewResultPacket
	for i := range packets {
		if packets[i].Binding.AssignmentID == assignmentID && packets[i].Binding.OwnerAgentID == owner {
			copyPacket := packets[i]
			packet = &copyPacket
			break
		}
	}
	if packet == nil {
		reviewState, _ := snapshot.State["review"].(map[string]any)
		assignments, _ := reviewState["assignments"].(map[string]any)
		row, _ := assignments[assignmentID].(map[string]any)
		switch workstate.ReviewAssignmentActivity(row) {
		case "queued", "pending", "planned":
			return fmt.Sprintf("S7 Reviewer assignment=%s is queued. Main resolves dispatch through the original ReviewPlan; no answer packet is issued while queued.", assignmentID)
		case "blocked":
			return fmt.Sprintf("S7 Reviewer assignment=%s is blocked. Main must resolve its recorded blocker before a new answer can be issued.", assignmentID)
		case "dispatched":
			return fmt.Sprintf("S7 Reviewer assignment=%s has no current packet. Main must restore the original dispatched responsibility so the listener can prepare its exact questionnaire path.", assignmentID)
		default:
			return fmt.Sprintf("S7 Reviewer assignment=%s has no current issued packet. Main owns the next dispatch decision.", assignmentID)
		}
	}
	view, answer, err := reviewResultLifecyclePaths(ctx, root, snapshot.State, *packet, request.CWD, request.SessionID)
	if err != nil {
		return "S7 ReviewResult source is unavailable; Main must restore the registered Reviewer execution: " + err.Error()
	}
	// Ordinary listeners read current facts; original consumers and explicit
	// receipt APIs retain full issuance and artifact verification.
	ordinaryReminder := request.Event == "PreToolUse" || request.Event == "SessionStart" || request.Event == "SubagentStart" || request.Event == "PreCompact" || request.Event == "Stop" || request.Event == "SubagentStop" || request.Event == "TeammateIdle"
	if !ordinaryReminder {
		if err := runtime.VerifyReviewResultPacketIssueContext(ctx, root, snapshot.State, *packet); err != nil {
			return "S7 packet evidence is not current; Main must inspect the ReviewPlan issuance: " + err.Error()
		}
	}
	reviewState, _ := snapshot.State["review"].(map[string]any)
	assignments, _ := reviewState["assignments"].(map[string]any)
	row, _ := assignments[assignmentID].(map[string]any)
	if workstate.ReviewAssignmentActivity(row) == "consumed" {
		if ordinaryReminder {
			facts := hook.CurrentResponsibility(root, snapshot, loaded.PolicyContext.Agent, hook.AssignmentForAgent(loaded.Assignments, owner), false)
			if facts.DeliveryRecorded {
				return fmt.Sprintf("S7 Reviewer assignment=%s: Runtime records your current responsibility done for answer %s. Do not repeat the Result. Main owns the next review-control step; you can end this responsibility.", assignmentID, answer)
			}
			return fmt.Sprintf("S7 Reviewer assignment=%s: answer %s has no current completed responsibility fact; Main must recover the original Result record.", assignmentID, answer)
		}
		receipt, accepted, err := review.IssuedResultReceiptContext(ctx, root, snapshot.State, packet.ID, owner)
		if err != nil {
			return "S7 accepted receipt needs Main review: " + err.Error()
		}
		if accepted && receipt.PacketID == packet.ID && receipt.ProducerAgentID == owner {
			return fmt.Sprintf("S7 work_complete=true: the original ReviewResult consumer recorded your exact Assignment receipt for answer %s. Main owns the next review-control step; you can end this responsibility.", answer)
		}
		return "S7 Assignment is marked consumed without a valid exact receipt. Main must recover the original ReviewResult record."
	}
	if workstate.ReviewAssignmentActivity(row) != "dispatched" {
		return fmt.Sprintf("S7 Reviewer assignment=%s is %v; Main must resolve its current disposition before authoring.", assignmentID, row["status"])
	}
	source, err := questionnaireSourceRoot(root, snapshot.State, packet.Binding.AssignmentID, packet.Binding.OwnerAgentID, packet.Binding.RuntimeID, packet.Binding.BaselineGeneration)
	if err != nil {
		return "S7 capture working directory is unavailable; Main must restore the registered Reviewer execution: " + err.Error()
	}
	executable := s7CaptureShellQuote(filepath.Join(root, ".claude/bin/loop-harness"))
	capturePrefix := executable + " capture exec --root " + s7CaptureShellQuote(root) + " --assignment " + s7CaptureShellQuote(assignmentID) + " --cwd " + s7CaptureShellQuote(source) + " --"
	stepPrefix := executable + " capture step --root " + s7CaptureShellQuote(root) + " --assignment " + s7CaptureShellQuote(assignmentID)
	buffer := review.CaptureFile(root, packet.Binding.RuntimeID, packet.Binding.BaselineGeneration, assignmentID)
	return fmt.Sprintf("S7 Reviewer assignment=%s: read the exact issued packet %s, fill the professional ReviewResult answer in %s, and save it. Keep the issued claim scope unchanged. Capture buffer: %s (merged by the original save consumer). Command shape: %s followed by your chosen professional check command and arguments; this prefix alone is incomplete. Observed-step command shape: %s followed by required --action and --observed professional text, and optional --evidence refs, --finding or --claim correlation; this prefix alone is incomplete. Record ordinary Findings and continue safe checks for the current required Claims.", assignmentID, view, answer, buffer, capturePrefix, stepPrefix)
}

func s7CaptureShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func reviewResultLifecyclePaths(ctx context.Context, root string, state map[string]any, packet workpacket.ReviewResultPacket, cwd, sessionID string) (string, string, error) {
	source, err := questionnaireSourceRoot(root, state, packet.Binding.AssignmentID, packet.Binding.OwnerAgentID, packet.Binding.RuntimeID, packet.Binding.BaselineGeneration)
	if err != nil {
		return "", "", err
	}
	return filepath.Join(source, filepath.FromSlash(packet.ViewPath)), filepath.Join(source, filepath.FromSlash(packet.AnswerPath)), nil
}

// Pure display coordinates do not imply a hydrated/verified packet.
func reviewResultIndexLifecyclePaths(root string, state map[string]any, packet runtime.ReviewResultPacketIndex) (string, string, error) {
	source, err := questionnaireSourceRoot(root, state, packet.Binding.AssignmentID, packet.Binding.OwnerAgentID, packet.Binding.RuntimeID, packet.Binding.BaselineGeneration)
	if err != nil {
		return "", "", err
	}
	return filepath.Join(source, filepath.FromSlash(packet.ViewPath)), filepath.Join(source, filepath.FromSlash(packet.AnswerPath)), nil
}

// Questionnaire paths come from registered graph coordinates. Tool/session
// authorization is not part of displaying the work an Agent needs to do.
func questionnaireSourceRoot(root string, state map[string]any, assignment, owner, runtimeID string, generation int) (string, error) {
	registry, err := workspace.Decode(state)
	if err != nil {
		return "", err
	}
	if registry != nil {
		if execution, exists := registry.Executions[assignment]; exists {
			if execution.Status != "ready" || execution.AgentID != owner || execution.RuntimeID != runtimeID || execution.BaselineGeneration != generation {
				return "", fmt.Errorf("questionnaire execution is not current or ready; Main must finish its dispatch")
			}
			return execution.Path, nil
		}
	}
	return root, nil
}

// registeredPlanningRow selects the existing responsibility, without rebuilding
// its packet, checking historical sources or accepting an answer during a read.
func registeredPlanningRow(state map[string]any) (map[string]any, error) {
	life, _ := state["lifecycle"].(map[string]any)
	template := map[string]string{"design": "s2-design-v1", "contracts": "s3-contract-v1", "tasks": "s4-task-planning-v1"}[stringValue(life["phase"])]
	if life["state"] != "planning" || template == "" {
		return nil, nil
	}
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	var current map[string]any
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["template_id"] != template {
			continue
		}
		binding, _ := row["binding"].(map[string]any)
		if binding["runtime_id"] != state["runtime_id"] || tolerantInt(binding["generation"]) != tolerantInt(mapFieldCLI(state, "baseline")["generation"]) || tolerantInt(binding["stage_entry"]) != tolerantInt(life["phase_revision"]) {
			continue
		}
		if current != nil {
			return nil, fmt.Errorf("multiple current questionnaires are registered; Main must resolve the actual entry ambiguity")
		}
		current = row
	}
	return current, nil
}

func stageCurrentPlanningContext(root string, snapshot runtime.Snapshot) string {
	phase := stringValue(mapFieldCLI(snapshot.State, "lifecycle")["phase"])
	stage := map[string]string{"contracts": "S3", "tasks": "S4"}[phase]
	row, err := registeredPlanningRow(snapshot.State)
	if err != nil {
		return stage + " current entry: " + err.Error()
	}
	preparation := submission.PlanningDiagnostic(root, snapshot.State, "preparation")
	if row == nil {
		return stage + " current questionnaire is not prepared by its successful entry outlet. Main completes the actual named materials; a real professional save/Git change can finish preparation. Do not guess a path or resubmit the completed preceding report. Material entry: " + filepath.Join(root, "docs/control/agent-protocol.md#"+strings.ToLower(stage)) + preparation + ". For a technical retry, use " + submission.PlanningRecoveryCommand(root, "issue") + "; this reuses the original entry." + submission.PlanningProfessionalReworkGuidance(root)
	}
	view, answer := stringValue(row["view_path"]), stringValue(row["answer_path"])
	message := fmt.Sprintf("%s current questionnaire: read %s; answer %s. Ordinary Pre/Stop reads existing facts only.", stage, filepath.Join(root, view), filepath.Join(root, answer)) + preparation
	missingFiles := false
	for _, path := range []string{view, answer} {
		if path == "" {
			missingFiles = true
			message += " Current registered questionnaire path is missing; Main must finish entry preparation."
			continue
		}
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			missingFiles = true
			message += " Current material missing: " + filepath.Join(root, path) + " (" + err.Error() + ")."
		}
	}
	if record, found, err := runtime.FindSubmission(snapshot.State, stringValue(row["packet_id"])); err == nil && found {
		if record.ControlStatus == "pending" || record.ControlStatus == "no_transition" {
			message += submission.AcceptedPlanningDeliveryGuidance(root, snapshot.State, record, stage)
			message += " The professional answer is already recorded; do not resubmit it. Main owns the original outlet " + record.FollowupID + ": repair the named material/system gap, then continue using " + submission.PlanningRecoveryCommand(root, "recover") + ". Do not create saves or tools just to trigger progress." + submission.PlanningDiagnostic(root, snapshot.State, "outlet")
		} else {
			message += " This answer is already received; follow the current stage responsibility without resubmitting it."
		}
	} else if err != nil {
		message += " Main must inspect the original reception record: " + err.Error()
	} else {
		if missingFiles {
			message += " Restore original missing questionnaire files with " + submission.PlanningRecoveryCommand(root, "issue") + "; preserve existing answers."
		}
		message += " Complete its original professional questions and save normally." + submission.PlanningDiagnostic(root, snapshot.State, "reception")
		if phase == "contracts" || phase == "tasks" {
			data, decodeErr := json.Marshal(row)
			var packet workpacket.ContractPacket
			var tasks workpacket.TaskPlanningPacket
			if decodeErr == nil && phase == "tasks" {
				decodeErr = json.Unmarshal(data, &tasks)
			}
			if decodeErr == nil && phase == "contracts" {
				decodeErr = json.Unmarshal(data, &packet)
			}
			if decodeErr != nil {
				message += " Original responsibility cannot be read: " + decodeErr.Error()
			} else if phase == "tasks" {
				message += submission.TaskPlanningDeliveryGuidance(root, tasks, nil, snapshot.State)
			} else {
				message += submission.ContractDeliveryGuidance(root, packet, nil, snapshot.State)
			}
		}
	}
	return message
}

// Only a genuine Post save/material change calls this existing preparation
// outlet. Reads do not issue, repair, or reconstruct questionnaires.
func prepareCurrentPlanningAfterChange(root string, request policy.Input) string {
	if !successfulPlanningTool(request) {
		return ""
	}
	switch request.ToolName {
	case "Read", "Glob", "Grep":
		return ""
	}
	snapshot, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil || !runtime.PacketEnabled(snapshot.State) {
		return ""
	}
	life := mapFieldCLI(snapshot.State, "lifecycle")
	if life["state"] != "planning" || (life["phase"] != "design" && life["phase"] != "contracts" && life["phase"] != "tasks") || !mainQuestionnaireViewer(request) {
		return ""
	}
	if (life["phase"] == "design" || life["phase"] == "contracts") && !planningMaterialSave(request, root) {
		return ""
	}
	row, err := registeredPlanningRow(snapshot.State)
	if err != nil {
		return "Current planning preparation: " + err.Error()
	}
	if row != nil {
		_, vErr := os.Stat(filepath.Join(root, stringValue(row["view_path"])))
		_, aErr := os.Stat(filepath.Join(root, stringValue(row["answer_path"])))
		if vErr == nil && aErr == nil {
			return ""
		}
	}
	if life["phase"] == "design" {
		service := &submission.Service{Root: root, StatePath: filepath.Join(root, ".claude/loop-state.json"), JournalPath: filepath.Join(root, ".claude/loop-events.jsonl"), Validator: semantic.RuntimeCandidateValidator{}}
		if _, err := service.IssueDesign(contextForHook(request)); err != nil {
			return "S2 original questionnaire preparation needs repair: " + err.Error()
		}
		current, err := runtime.NewStore(service.StatePath, service.JournalPath).Snapshot()
		if err != nil {
			return "S2 questionnaire preparation finished, but current context could not be read: " + err.Error()
		}
		return stageDesignQuestionnaireContext(root, current)
	}
	service, err := submission.New(root)
	if err != nil {
		submission.RecordPlanningDiagnostic(root, snapshot.State, "preparation", err.Error())
		return "Current planning entry preparation needs: " + err.Error()
	}
	return stagePlanningQuestionnaireLifecycleContext(contextForHook(request), service, stringValue(life["phase"]))
}
