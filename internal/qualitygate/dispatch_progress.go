package qualitygate

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path"
	"strings"
	"time"
)

// BuilderProgress is shared by planned S6 gates, the board and dispatch checks.
// Only the TASK's current report and its owner's assignment can prove completion.
type BuilderProgress struct {
	State  string
	Reason string
}

// dispatchCheckpoint is the subset of the Integrator checkpoint consumed by
// dispatch projections.  Keep the report binding and merge receipt together:
// a current Result cannot release a dependent task unless the receipt for the
// same integration is still valid on the bound development branch.
type dispatchCheckpoint struct {
	TaskID       string `json:"task_id"`
	AssignmentID string `json:"assignment_id"`
	Generation   int    `json:"baseline_generation"`
	State        string `json:"state"`
	Updated      string `json:"verified_at"`
	ReportPath   string `json:"completion_report_path"`
	ReportSHA    string `json:"completion_report_sha256"`
	TargetBranch string `json:"target_branch"`
	MergeCommit  string `json:"merge_commit"`
}

func HasDispatchPlan(state map[string]any) bool {
	for _, d := range currentDocuments(state, nestedInt(state, "baseline", "generation")) {
		if d.Kind == "dispatch_plan" {
			return true
		}
	}
	return false
}
func PlannedBuilderProgress(input Input) map[string]BuilderProgress {
	result := map[string]BuilderProgress{}
	entities, _ := input.Snapshot.State["entities"].(map[string]any)
	tasks, _ := entities["tasks"].([]any)
	agents, _ := entities["agents"].([]any)
	generation := nestedInt(input.Snapshot.State, "baseline", "generation")
	runtimeID := stringValue(input.Snapshot.State["runtime_id"])
	evidence, _ := input.Snapshot.State["evidence"].([]any)
	for _, raw := range tasks {
		task, _ := raw.(map[string]any)
		id := stringValue(task["id"])
		state := stringValue(task["state"])
		if state == "candidate" || state == "reviewed" || state == "locked" {
			continue
		}
		progress := BuilderProgress{State: "running", Reason: "registered owner"}
		if state == "blocked" || state == "cancelled" {
			result[id] = BuilderProgress{State: "blocked", Reason: "TASK " + state}
			continue
		}
		report := stringValue(task["completion_report_ref"])
		if report == "" {
			result[id] = progress
			continue
		}
		progress = BuilderProgress{State: "reported", Reason: "current result/integration not verified"}
		var env evidenceEnvelope
		reportSHA := ""
		valid := false
		for _, entry := range evidence {
			e, _ := entry.(map[string]any)
			if e["path"] != report || e["kind"] != "completion_report" || e["status"] != "valid" || e["invalidated_by"] != nil || intValue(e["baseline_generation"]) != generation {
				continue
			}
			b, err := input.Files.ReadFile(report)
			if err != nil || sha256Hex(b) != e["sha256"] || json.Unmarshal(b, &env) != nil {
				continue
			}
			reportSHA = sha256Hex(b)
			valid = env.TaskID == id && env.RuntimeID == runtimeID && env.BaselineGeneration == generation && env.EvidenceID == e["id"] && env.InvalidatedBy == "" && env.Conclusion == "completed" && len(env.Checks) > 0 && len(failingEnvelopeChecks(env)) == 0 && len(env.ScopeDeviations) == 0
		}
		for _, doc := range currentDocuments(input.Snapshot.State, generation) {
			if doc.Kind == "task" && doc.ID == id {
				valid = valid && exactSubjects(env.SubjectRefs, []documentFact{doc})
			}
		}
		if !valid {
			result[id] = progress
			continue
		}
		owner := false
		owners, _ := task["owner_agent_ids"].([]any)
		for _, o := range owners {
			if o == env.ProducerAgentID {
				owner = true
			}
		}
		if !owner {
			result[id] = progress
			continue
		}
		assignment := ""
		for _, rawAgent := range agents {
			a, _ := rawAgent.(map[string]any)
			if a["id"] == env.ProducerAgentID {
				if a["state"] != "reported" && a["state"] != "done" && a["state"] != "stopped" {
					break
				}
				_, assignment, _ = strings.Cut(stringValue(a["prompt_ref"]), "#")
			}
		}
		if assignment == "" || path.Base(assignment) != assignment {
			result[id] = progress
			continue
		}
		b, err := input.Files.ReadFile(path.Join(".claude/evidence", runtimeID, fmt.Sprintf("g%d", generation), "worktree", assignment, "checkpoint.json"))
		var cp dispatchCheckpoint
		if err == nil && json.Unmarshal(b, &cp) == nil && cp.TaskID == id && cp.AssignmentID == assignment && cp.Generation == generation && cp.ReportPath == report && cp.ReportSHA != "" && cp.ReportSHA == reportSHA {
			// A report submitted after this checkpoint needs fresh integration checks.
			var body struct {
				Created string `json:"created_at"`
			}
			rb, _ := input.Files.ReadFile(report)
			_ = json.Unmarshal(rb, &body)
			created, ce := time.Parse(time.RFC3339Nano, body.Created)
			updated, ue := time.Parse(time.RFC3339Nano, cp.Updated)
			if ce == nil && ue == nil && !updated.Before(created) {
				switch cp.State {
				case "verified", "acknowledged", "cleanup_pending", "complete":
					if !checkpointMergeValid(input, cp) {
						break
					}
					progress = BuilderProgress{State: "integrated", Reason: "current result and assignment integration verified"}
				}
			}
		}
		result[id] = progress
	}
	return result
}

// mergeReceiptReachable is a read-only guard for dispatch projections. The
// Integrator remains the authority that writes checkpoints; a projection only
// needs to prove that a recorded merge is still in the bound target history.
// Descendant commits are accepted by --is-ancestor, so ordinary development
// work after delivery does not invalidate the checkpoint.
func mergeReceiptReachable(root, mergeCommit, targetBranch string) bool {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(mergeCommit) == "" || strings.TrimSpace(targetBranch) == "" {
		return false
	}
	return exec.Command("git", "-C", root, "merge-base", "--is-ancestor", mergeCommit, targetBranch).Run() == nil
}

// checkpointMergeValid prevents a durable checkpoint from releasing a
// successor after the target branch was rewritten.  Checkpoints that carry a
// current completion-report binding are new records and must have a complete
// receipt.  A report-bound checkpoint with no receipt is therefore rejected
// even when its state says complete.
//
// Older checkpoints predate the report binding.  They may omit the receipt,
// including the historical target-only shape, and remain readable for the
// legacy recovery path.  Once a merge receipt is present, however, its target
// must be the development branch bound to this REQ and the receipt must still
// be an ancestor of that branch.  This accepts ordinary post-delivery commits
// while rejecting a branch rewrite or a receipt from another target.
func checkpointMergeValid(input Input, cp dispatchCheckpoint) bool {
	reportBound := strings.TrimSpace(cp.ReportPath) != "" || strings.TrimSpace(cp.ReportSHA) != ""
	mergeCommit := strings.TrimSpace(cp.MergeCommit)
	targetBranch := strings.TrimSpace(cp.TargetBranch)
	if mergeCommit == "" {
		// A target-only or entirely unbound checkpoint with no report identity
		// is the explicitly supported legacy shape.  A new report-bound record
		// cannot use this escape hatch.
		return !reportBound
	}
	if targetBranch == "" {
		return false
	}
	if reportBound && (strings.TrimSpace(cp.ReportPath) == "" || strings.TrimSpace(cp.ReportSHA) == "") {
		return false
	}
	boundBranch := boundDevelopmentBranch(input.Snapshot.State)
	if boundBranch == "" || targetBranch != boundBranch {
		return false
	}
	return mergeReceiptReachable(input.Root, mergeCommit, targetBranch)
}

func boundDevelopmentBranch(state map[string]any) string {
	bound, _ := state["bound_req"].(map[string]any)
	workspace, _ := bound["workspace"].(map[string]any)
	return strings.TrimSpace(stringValue(workspace["dev_branch"]))
}
