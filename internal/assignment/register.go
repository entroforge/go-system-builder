package assignment

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/dispatch"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/identity"
	"github.com/entroforge/go-system-builder/internal/integration"
	"github.com/entroforge/go-system-builder/internal/qualitygate"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/team"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

type Request struct {
	Context          context.Context
	ExpectedRevision int
	ManifestPath     string
	TaskID           string
	TaskPath         string
	// The caller's actual per-dispatch selection, when preparation already
	// opened the original source view. These are internal values, not fields
	// authored by an Agent or persisted as new evidence.
	SelectedTaskBytes  []byte
	SelectedBoard      *dispatch.Board
	RepairAssignmentID string
	RepairOwnerAgentID string
	Revision           *RevisionRequest
	OccurredAt         time.Time
}

type manifest struct {
	AssignmentRecordContract   string                 `json:"assignment_record_contract,omitempty"`
	ManifestID                 string                 `json:"manifest_id"`
	PlatformTeamID             string                 `json:"platform_team_id"`
	WorkgroupID                string                 `json:"workgroup_id"`
	WorkgroupKind              string                 `json:"workgroup_kind"`
	ResponsibilityDispositions []responsibilityStatus `json:"responsibility_dispositions"`
	Assignments                []assignment           `json:"assignments"`
}

type responsibilityStatus struct {
	ResponsibilityID string `json:"responsibility_id"`
	Disposition      string `json:"disposition"`
}

type assignment struct {
	CheckProfiles      []checkexec.Profile `json:"check_profiles,omitempty"`
	AssignmentID       string              `json:"assignment_id"`
	ResponsibilityID   string              `json:"responsibility_id"`
	RoleFamily         string              `json:"role_family"`
	AgentID            string              `json:"agent_id"`
	AgentDefinitionRef string              `json:"agent_definition_ref"`
	SkillRefs          []string            `json:"skill_refs"`
	WritePaths         []string            `json:"write_paths"`
	OutputPaths        []string            `json:"output_paths"`
	ClaimIDs           []string            `json:"claim_ids"`
	DispatchMode       string              `json:"dispatch_mode"`
}

// RevisionAcceptanceReceipt projects the original, durably validated
// Assignment revision receipt alongside the Runtime snapshot returned by
// RegisterWithReceipt. Current describes whether that issued Record still
// owns the current Assignment slot; Replayed says this call reused the
// original intent receipt without writing another Runtime commit.
type RevisionAcceptanceReceipt struct {
	loopruntime.AssignmentRevisionReceipt
	Revision int  `json:"revision"`
	Current  bool `json:"current"`
	Replayed bool `json:"replayed"`
}

// Register preserves the original registration API and output behavior.
func Register(root, statePath, journalPath string, request Request) (loopruntime.Snapshot, error) {
	snapshot, err := registerWithReceipt(root, statePath, journalPath, request, nil)
	return snapshot, err
}

// RegisterWithReceipt adds the original durable issuance receipt for
// canonical Assignment revisions. Legacy and first-time registrations return
// a nil receipt, so callers can keep their existing output shape.
func RegisterWithReceipt(root, statePath, journalPath string, request Request) (loopruntime.Snapshot, *RevisionAcceptanceReceipt, error) {
	var receipt *RevisionAcceptanceReceipt
	snapshot, err := registerWithReceipt(root, statePath, journalPath, request, &receipt)
	return snapshot, receipt, err
}

func registerWithReceipt(root, statePath, journalPath string, request Request, receiptOut **RevisionAcceptanceReceipt) (snapshot loopruntime.Snapshot, resultErr error) {
	ctx := request.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = fileview.WithReadCache(ctx)
	if err := ctx.Err(); err != nil {
		return loopruntime.Snapshot{}, err
	}
	wasReplay := false
	defer func() {
		if receiptOut == nil || request.Revision == nil || resultErr != nil {
			return
		}
		receipt, err := revisionAcceptanceReceipt(snapshot, *request.Revision, wasReplay)
		if err != nil {
			resultErr = fmt.Errorf("resolve committed Assignment revision receipt: %w", err)
			return
		}
		*receiptOut = receipt
	}()
	if request.Revision != nil {
		_, unlock, err := integration.LockWorkspace(ctx, root)
		if err != nil {
			return loopruntime.Snapshot{}, err
		}
		defer unlock()
	}
	stateData, err := os.ReadFile(statePath)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("read runtime: %w", err)
	}
	var current map[string]any
	if err := json.Unmarshal(stateData, &current); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("decode runtime: %w", err)
	}
	commitRevision, err := commitRevision(request.ExpectedRevision, current)
	if err != nil {
		return loopruntime.Snapshot{}, err
	}
	lifecycle, ok := current["lifecycle"].(map[string]any)
	if !ok {
		return loopruntime.Snapshot{}, fmt.Errorf("runtime lifecycle must be an object")
	}
	cursor := map[string]any{
		"state": lifecycle["state"],
		"phase": lifecycle["phase"],
	}
	runtimeID, _ := current["runtime_id"].(string)
	registeredBaseline, _ := current["baseline"].(map[string]any)

	manifestData, err := os.ReadFile(request.ManifestPath)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("read team manifest: %w", err)
	}
	if err := schema.NewValidator(root).ValidateBytes(
		"team-manifest.schema.json",
		manifestData,
	); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("team manifest schema: %w%s", err, manifestSchemaHint(err))
	}
	if err := team.ValidateBytes(manifestData); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("team manifest semantics: %w%s", err, manifestSemanticsHint(err))
	}
	if !workstate.SupportsAssignments(current["schema_version"]) {
		var declared manifest
		if err := json.Unmarshal(manifestData, &declared); err != nil {
			return loopruntime.Snapshot{}, err
		}
		for _, row := range declared.Assignments {
			if len(row.CheckProfiles) > 0 {
				return loopruntime.Snapshot{}, fmt.Errorf("legacy Runtime cannot register new CheckProfile authority")
			}
		}
	}

	var value manifest
	if err := json.Unmarshal(manifestData, &value); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("decode team manifest: %w", err)
	}
	recordEnabled, err := ManifestAssignmentRecordEnabled(manifestData)
	if err != nil {
		return loopruntime.Snapshot{}, err
	}
	if recordEnabled && !workstate.SupportsAssignments(current["schema_version"]) {
		return loopruntime.Snapshot{}, fmt.Errorf("canonical Assignment requires an explicitly initialized packet Runtime")
	}
	if request.Revision != nil {
		if !recordEnabled || !workstate.SupportsAssignments(current["schema_version"]) {
			return loopruntime.Snapshot{}, fmt.Errorf("Assignment revision requires an explicitly canonical manifest and packet Runtime")
		}
		if request.RepairAssignmentID != "" || request.RepairOwnerAgentID != "" {
			return loopruntime.Snapshot{}, fmt.Errorf("Assignment revision cannot be combined with a repair binding")
		}
		if len(value.Assignments) != 1 || value.Assignments[0].AssignmentID != request.Revision.AssignmentID {
			return loopruntime.Snapshot{}, fmt.Errorf("Assignment revision manifest must contain exactly the requested Assignment")
		}
		dispatchMode := value.Assignments[0].DispatchMode
		if dispatchMode == "" {
			dispatchMode = "plan_checkpoint"
		}
		if dispatchMode != "plan_checkpoint" {
			return loopruntime.Snapshot{}, fmt.Errorf("Assignment revision currently requires plan_checkpoint; fresh human approval binding is not implemented")
		}
	}
	for _, item := range value.Assignments {
		if err := identity.ValidateAgentID(item.AgentID); err != nil {
			return loopruntime.Snapshot{}, fmt.Errorf("team manifest assignment %s: %w", item.AssignmentID, err)
		}
	}
	manifestRef := repositoryPath(root, request.ManifestPath)
	taskRef := repositoryPath(root, request.TaskPath)
	if request.Revision != nil {
		if err := resolveRevisionRequest(current, request.Revision, manifestRef, value); err != nil {
			return loopruntime.Snapshot{}, err
		}
		if err := validateRevisionRequest(*request.Revision); err != nil {
			return loopruntime.Snapshot{}, err
		}
		journalBytes, err := readRevisionJournal(journalPath)
		if err != nil {
			return loopruntime.Snapshot{}, fmt.Errorf("read Assignment revision issuance journal: %w", err)
		}
		if err := loopruntime.ValidateAssignmentRevisionReceipts(current, journalBytes); err != nil {
			return loopruntime.Snapshot{}, fmt.Errorf("validate prior Assignment revision receipts: %w", err)
		}
		replayed, found, err := findRevisionReplay(root, current, *request.Revision, manifestRef, request.TaskID, taskRef, value.Assignments[0].AgentID, value.WorkgroupID)
		if err != nil {
			return loopruntime.Snapshot{}, err
		}
		if found {
			wasReplay = true
			return replayed, nil
		}
		records, err := assignmentrecord.RecordsFromState(current)
		if err != nil {
			return loopruntime.Snapshot{}, err
		}
		rid, gen, _, err := assignmentrecord.CurrentRuntimeBinding(current)
		if err != nil {
			return loopruntime.Snapshot{}, err
		}
		previous, found, err := assignmentrecord.FindRecord(records, rid, gen, request.Revision.AssignmentID)
		if err != nil || !found {
			return loopruntime.Snapshot{}, fmt.Errorf("Assignment revision requires a current canonical Record: %v", err)
		}
		if request.TaskID != previous.Task.ID {
			return loopruntime.Snapshot{}, fmt.Errorf("Assignment revision must retain the current Assignment TASK id %s", previous.Task.ID)
		}
		if request.Revision.ExpectedRecord != previous.Reference() {
			return loopruntime.Snapshot{}, fmt.Errorf("stale Assignment revision: expected Record %v, current is %v", request.Revision.ExpectedRecord, previous.Reference())
		}
	}
	var selectedFiles fileview.Reader
	var selectedBoard *dispatch.Board
	var taskData []byte
	if lifecycle["state"] == "building" && value.WorkgroupKind == "builder" && qualitygate.HasDispatchPlan(current) {
		view, err := dispatch.View(root, current)
		if err != nil {
			return loopruntime.Snapshot{}, err
		}
		selectedFiles = view
		if request.SelectedBoard != nil {
			selectedBoard = request.SelectedBoard
		} else {
			board, err := dispatch.LoadWithFiles(root, current, view, 1<<20)
			if err != nil {
				return loopruntime.Snapshot{}, err
			}
			selectedBoard = &board
		}
		if request.SelectedTaskBytes != nil {
			taskData = request.SelectedTaskBytes
		} else {
			taskData, err = view.ReadFile(request.TaskPath)
		}
	} else {
		taskData, err = os.ReadFile(request.TaskPath)
	}
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("read task: %w", err)
	}
	occurredAt := request.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	agentIDs := make([]string, 0, len(value.Assignments))
	for _, item := range value.Assignments {
		agentIDs = append(agentIDs, item.AgentID)
	}
	responsibilityIDs := assignedResponsibilities(value)

	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{}).WithContext(ctx)
	// Activation envelopes are staged while the mutation builds its candidate
	// state. Keep their exact bytes so a rejected Apply can remove only
	// the files created by this attempt. Store.Update already protects the
	// cleanup with the same revision/pending-marker checks used by every other
	// staged artifact; if the commit may still be recoverable, the file is
	// intentionally retained.
	stagedActivations := make([]loopruntime.ArtifactCleanupRequest, 0, len(value.Assignments))
	// The identity of a workgroup registration is the registered workgroup and
	// the manifest that declares it. The workgroup id is unique per
	// registration, so no Runtime write counter is needed (G18 §1/§1.3).
	mutationEventID := fmt.Sprintf("evt-register-%s", value.WorkgroupID)
	mutationTransitionID, mutationEvent := "ENTITY-REGISTER", "workgroup_registered"
	mutationIdempotencyKey := fmt.Sprintf("runtime:register:%s:%s", value.ManifestID, value.WorkgroupID)
	mutationMessage := "Registered a validated workgroup, task and phase-one Agents."
	if request.Revision != nil {
		mutationEventID = loopruntime.AssignmentRevisionEventID(request.Revision.ExpectedRecord, request.Revision.IntentKey)
		mutationTransitionID, mutationEvent = "ASSIGNMENT-REVISION", "assignment_revision_issued"
		mutationIdempotencyKey = loopruntime.AssignmentRevisionIdempotencyKey(request.Revision.ExpectedRecord, request.Revision.IntentKey)
		mutationMessage = "Issued a new immutable Assignment revision and invalidated the previous current owner projection."
	}
	// G18 §3.2: the Assignment Record revision issuance is a professional act.
	// It must not be refused because the Runtime moved for an unrelated
	// reason, so the revision path takes the current-value path (-1 is the
	// omission marker) instead of asserting the caller's Runtime write count.
	// The protection is unchanged and lives in the domain facts: the received
	// row (findRevisionReplay and the revision receipt index), the
	// journal-level dedup (the domain idempotency key of this revision
	// intent) and the Assignment Record's own revision (ExpectedRecord
	// against the current Record, checked again inside Apply).
	expectedRevision := request.ExpectedRevision
	if request.Revision != nil {
		expectedRevision = -1
	}
	snapshot, updateErr := updateRuntime(store, expectedRevision, loopruntime.Mutation{
		EventID:            mutationEventID,
		TransitionID:       mutationTransitionID,
		Event:              mutationEvent,
		Actor:              "orchestrator",
		IdempotencyKey:     mutationIdempotencyKey,
		EvidenceIDs:        []string{manifestRef, taskRef},
		Message:            mutationMessage,
		OccurredAt:         occurredAt,
		RuntimeID:          runtimeID,
		BaselineGeneration: dispatch.Number(registeredBaseline["generation"]),
		From:               cursor,
		To:                 cursor,
		Apply: func(state map[string]any) error {
			if value.WorkgroupKind == "builder" {
				if err := dispatch.CheckConfiguredBackpressure(root, state); err != nil {
					return err
				}
			}
			var currentBoard *dispatch.Board
			if selectedBoard != nil {
				board, err := dispatch.ProjectSelectedPlan(root, state, selectedFiles, selectedBoard.Plan, 1<<20)
				if err != nil {
					return err
				}
				currentBoard = &board
			}
			if request.Revision == nil {
				if currentBoard != nil {
					if err := dispatch.CheckBoardRegistration(*currentBoard, request.TaskID); err != nil {
						return err
					}
				} else if err := dispatch.CheckRegistration(root, state, request.TaskID); err != nil {
					return err
				}
			} else if err := checkRevisionDispatchRegistration(root, state, request.TaskID, currentBoard); err != nil {
				return err
			}
			plannedWrites := []string{}
			for _, item := range value.Assignments {
				plannedWrites = append(plannedWrites, effectiveActivationWritePaths(item.WritePaths, item.OutputPaths)...)
			}
			if currentBoard != nil {
				if err := dispatch.ValidateBoardWorkPackage(root, *currentBoard, request.TaskID, request.TaskPath, plannedWrites, len(value.Assignments)); err != nil {
					return err
				}
			} else if err := dispatch.ValidateWorkPackage(root, state, request.TaskID, request.TaskPath, taskData, plannedWrites, len(value.Assignments)); err != nil {
				return err
			}

			_, ok := state["lifecycle"].(map[string]any)
			if !ok {
				return fmt.Errorf("runtime lifecycle must be an object")
			}
			if err := validateWorkgroupState(value.WorkgroupKind, state); err != nil {
				return err
			}
			if request.RepairAssignmentID != "" {
				if value.WorkgroupKind != "builder" || request.RepairOwnerAgentID == "" {
					return fmt.Errorf("repair assignment binding requires a builder workgroup and repair owner agent")
				}
				manifestAssignment := false
				manifestOwner := ""
				for _, item := range value.Assignments {
					if item.AssignmentID == request.RepairAssignmentID || item.AssignmentID == "assignment-s9-"+strings.TrimPrefix(request.RepairAssignmentID, "repair-assignment-") {
						manifestAssignment = true
						manifestOwner = item.AgentID
						break
					}
				}
				if !manifestAssignment {
					return fmt.Errorf("repair assignment %s is not represented by the registered builder manifest", request.RepairAssignmentID)
				}
				if manifestOwner != request.RepairOwnerAgentID {
					return fmt.Errorf("repair assignment %s manifest owner %s does not match requested Agent %s", request.RepairAssignmentID, manifestOwner, request.RepairOwnerAgentID)
				}
				review, _ := state["review"].(map[string]any)
				if review == nil {
					return fmt.Errorf("repair assignment binding requires Runtime review state")
				}
				repairPointer, _ := review["repair"].(map[string]any)
				if repairPointer == nil {
					return fmt.Errorf("repair assignment %s is not bound to an active S9 RepairSession", request.RepairAssignmentID)
				}
				owners, _ := repairPointer["assignment_owners"].(map[string]any)
				if owners == nil {
					owners = map[string]any{}
					repairPointer["assignment_owners"] = owners
				}
				if existing, _ := owners[request.RepairAssignmentID].(string); existing != "" && existing != request.RepairOwnerAgentID {
					return fmt.Errorf("RepairAssignment %s is already owned by Agent %s; do not replace ownership mid-session", request.RepairAssignmentID, existing)
				}
				owners[request.RepairAssignmentID] = request.RepairOwnerAgentID
			}
			// L3-S7: reviewer workgroups bind to the registered ReviewPlan —
			// exact Claim set, lens match, static-before-behavior wave gate.
			if err := bindReviewPlanAssignments(root, state, value); err != nil {
				return err
			}
			entities, ok := state["entities"].(map[string]any)
			if !ok {
				return fmt.Errorf("runtime entities must be an object")
			}
			if request.Revision != nil {
				_, err := applyAssignmentRevision(root, state, value, manifestData, manifestRef, taskRef, request.TaskID, taskData, *request.Revision, currentBoard, mutationEventID, occurredAt, &stagedActivations)
				return err
			}
			if containsEntity(entities["teams"], value.WorkgroupID) {
				return fmt.Errorf("workgroup %s is already registered", value.WorkgroupID)
			}
			life, _ := state["lifecycle"].(map[string]any)
			// S5 reviewers reference a TASK as a document, not as Builder work.
			// Their own Team and Agent entries retain that review responsibility.
			if life["state"] == "document_verification" && value.WorkgroupKind == "document_verifier" {
				// The shared TASK reference is needed for the reviewer's context;
				// it is not an issued Builder assignment or an active owner.
				tasks, _ := entities["tasks"].([]any)
				if !containsEntity(tasks, request.TaskID) {
					task := map[string]any{"id": request.TaskID, "path": taskRef, "owner_agent_ids": []any{}}
					if err := workstate.RecordTaskActivity(state, task, "candidate", "task_created"); err != nil {
						return fmt.Errorf("record review TASK reference: %w", err)
					}
					entities["tasks"] = append(tasks, task)
				}
			} else {
				task := map[string]any{
					"id": request.TaskID,
					// Registering a workgroup in S6 dispatches the TASK: the
					// document is already `complete` and locked, the owner is
					// named, and the Builder is expected to start — landing
					// straight in `in_progress` keeps `runtime task-complete`
					// (which requires that state) reachable without a manual
					// lifecycle hop (L3-S6 complexity pass).
					"state":           "in_progress",
					"path":            taskRef,
					"owner_agent_ids": agentIDs,
				}
				if err := workstate.RecordTaskActivity(state, task, "in_progress", mutationEvent); err != nil {
					return fmt.Errorf("record registered TASK work state: %w", err)
				}
				tasks, _ := entities["tasks"].([]any)
				if !containsEntity(tasks, request.TaskID) {
					entities["tasks"] = append(tasks, task)
				} else if life, _ := state["lifecycle"].(map[string]any); life["state"] == "building" && value.WorkgroupKind == "builder" {
					// A TASK may already exist from the document-review/planning
					// registry. Record the explicitly dispatched Builder owners in
					// this same CAS; keep all existing TASK facts and prior owners.
					for _, raw := range tasks {
						row, _ := raw.(map[string]any)
						if row["id"] != request.TaskID {
							continue
						}
						owners, _ := row["owner_agent_ids"].([]any)
						for _, assignment := range value.Assignments {
							switch assignment.RoleFamily {
							case "backend-builder", "frontend-builder", "test-builder":
								if !stringArrayContains(owners, assignment.AgentID) {
									owners = append(owners, assignment.AgentID)
								}
							}
						}
						row["owner_agent_ids"] = owners
						if err := workstate.RecordTaskActivity(state, row, "in_progress", mutationEvent); err != nil {
							return fmt.Errorf("record registered TASK work state: %w", err)
						}
					}
				}
			}
			teams, _ := entities["teams"].([]any)
			teamRow := map[string]any{
				"id":                 value.WorkgroupID,
				"platform_team_id":   value.PlatformTeamID,
				"kind":               value.WorkgroupKind,
				"status":             "planned",
				"manifest_ref":       manifestRef,
				"responsibility_ids": responsibilityIDs,
				"agent_ids":          agentIDs,
			}
			if loopruntime.PacketEnabled(state) && value.WorkgroupKind == "document_verifier" {
				life, _ := state["lifecycle"].(map[string]any)
				baseline, _ := state["baseline"].(map[string]any)
				if life["state"] != "document_verification" {
					return fmt.Errorf("packet document reviewers must register in the current S5 entry")
				}
				teamRow["packet_stage_entry"] = life["phase_revision"]
				teamRow["packet_generation"] = baseline["generation"]
			}
			entities["teams"] = append(teams, teamRow)
			agents, _ := entities["agents"].([]any)
			for _, item := range value.Assignments {
				if containsEntity(agents, item.AgentID) {
					return fmt.Errorf("Agent %s is already registered", item.AgentID)
				}
				dispatchMode := item.DispatchMode
				if dispatchMode == "" {
					// L4 §3.3: continuous execution is the default; the
					// two-round approval is the exception for high-risk work.
					dispatchMode = "plan_checkpoint"
				}
				// Native registration records a pending engineering responsibility,
				// never proof that Claude Code has started or read anything.
				agentState := "reading"
				if value.AssignmentRecordContract == assignmentrecord.NativeDeliveryContract || reviewAssignmentQueued(state, item.AssignmentID, item.AgentID) {
					agentState = "queued"
				}
				agent := map[string]any{
					"id":                  item.AgentID,
					"role":                item.RoleFamily,
					"state":               agentState,
					"task_ids":            []string{request.TaskID},
					"team_id":             value.WorkgroupID,
					"definition_ref":      item.AgentDefinitionRef,
					"prompt_ref":          manifestRef + "#" + item.AssignmentID,
					"dispatch_mode":       dispatchMode,
					"readback_ref":        nil,
					"activation_ref":      nil,
					"activation_revision": nil,
					"updated_at":          occurredAt.UTC().Format(time.RFC3339Nano),
				}
				if err := workstate.RecordAgentActivity(state, agent, agentState, mutationEvent); err != nil {
					return fmt.Errorf("record registered Agent work state: %w", err)
				}
				agents = append(agents, agent)
			}
			entities["agents"] = agents
			var registeredRecords []Record
			if recordEnabled {
				records, err := buildRegisteredAssignmentRecords(root, state, manifestData, manifestRef, taskRef, request.TaskID, taskData, currentBoard)
				if err != nil {
					return err
				}
				rows, _ := entities["assignments"].([]any)
				existingRecords, err := assignmentrecord.RecordsFromState(state)
				if err != nil {
					return err
				}
				for i, record := range records {
					if _, found, err := assignmentrecord.FindRecord(existingRecords, record.RuntimeID, record.BaselineGeneration, record.AssignmentID); err != nil {
						return err
					} else if found {
						return fmt.Errorf("canonical Assignment already registered: %s", record.AssignmentID)
					}
					record, err = stageRegisteredRecordSources(root, state, record, &stagedActivations)
					if err != nil {
						return err
					}
					records[i] = record
					data, err := json.Marshal(record)
					if err != nil {
						return err
					}
					var row map[string]any
					if err := json.Unmarshal(data, &row); err != nil {
						return err
					}
					rows = append(rows, row)
				}
				entities["assignments"] = rows
				registeredRecords = records
			}
			for _, item := range value.Assignments {
				if value.AssignmentRecordContract == assignmentrecord.NativeDeliveryContract {
					continue // Native delivery records engineering responsibility, never activation.
				}
				dispatchMode := item.DispatchMode
				if dispatchMode == "" {
					dispatchMode = "plan_checkpoint"
				}
				source := ActivationSourceEntry{AgentID: item.AgentID, AgentDefinitionRef: item.AgentDefinitionRef, SkillRefs: item.SkillRefs, WritePaths: item.WritePaths, OutputPaths: item.OutputPaths}
				if recordEnabled {
					found := false
					for _, record := range registeredRecords {
						if record.AssignmentID == item.AssignmentID && record.OwnerAgentID == item.AgentID {
							ref := record.Reference()
							source.AssignmentRecord = &ref
							source.WritePaths = append([]string(nil), record.EffectiveWritePaths...)
							source.OutputPaths = nil
							found = true
							break
						}
					}
					if !found {
						return fmt.Errorf("canonical Assignment missing activation owner %s", item.AgentID)
					}
				}
				// Pre-stage the activation envelope for plan_checkpoint
				// agents (L4 §3.3 auto-activation). The original
				// responsibility and the actual Plan establish the handoff.
				activationRef, envelopeErr := PreStageActivationEnvelope(root, value.WorkgroupID, request.TaskID, item.AgentID, dispatchMode, source)
				if envelopeErr != nil {
					return envelopeErr
				}
				var activationRefValue any
				if activationRef != "" {
					envelope := buildActivationEnvelope(item.AgentID, source)
					activationBytes, marshalErr := json.MarshalIndent(envelope, "", "  ")
					if marshalErr != nil {
						return fmt.Errorf("activation envelope: encode staged bytes: %w", marshalErr)
					}
					stagedActivations = append(stagedActivations, loopruntime.ArtifactCleanupRequest{
						ExpectedRevision: commitRevision,
						ArtifactPath:     activationRef,
						ArtifactBytes:    append(activationBytes, '\n'),
						ReferencedPaths:  stateArtifactPaths(current),
					})
					activationRefValue = activationRef
				}
				for _, raw := range agents {
					row, _ := raw.(map[string]any)
					if row["id"] == item.AgentID {
						row["activation_ref"] = activationRefValue
						break
					}
				}
			}
			state["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
			return nil
		},
	})
	if updateErr != nil {
		for index := len(stagedActivations) - 1; index >= 0; index-- {
			if _, cleanupErr := store.RemoveUnreferencedArtifact(stagedActivations[index]); cleanupErr != nil {
				return snapshot, fmt.Errorf("%w; staged registration artifact cleanup failed: %v", updateErr, cleanupErr)
			}
		}
		if request.Revision != nil {
			latest, readErr := readRuntimeSnapshot(statePath)
			if readErr == nil {
				journalBytes, journalErr := readRevisionJournal(journalPath)
				if journalErr == nil {
					if validateErr := loopruntime.ValidateAssignmentRevisionReceipts(latest.State, journalBytes); validateErr == nil {
						replayed, found, replayErr := findRevisionReplay(root, latest.State, *request.Revision, manifestRef, request.TaskID, taskRef, value.Assignments[0].AgentID, value.WorkgroupID)
						if replayErr == nil && found {
							wasReplay = true
							return replayed, nil
						}
						if replayErr != nil {
							return snapshot, fmt.Errorf("Assignment revision replay after Writer conflict: %w", replayErr)
						}
					}
				}
			}
		}
	}
	if updateErr == nil && request.Revision != nil {
		journalBytes, journalErr := readRevisionJournal(journalPath)
		if journalErr != nil {
			return snapshot, fmt.Errorf("read committed Assignment revision journal: %w", journalErr)
		}
		if err := loopruntime.ValidateAssignmentRevisionReceipts(snapshot.State, journalBytes); err != nil {
			return snapshot, fmt.Errorf("validate committed Assignment revision receipt: %w", err)
		}
	}
	return snapshot, updateErr
}

func reviewAssignmentQueued(state map[string]any, assignmentID, agentID string) bool {
	reviewMap, _ := state["review"].(map[string]any)
	assignments, _ := reviewMap["assignments"].(map[string]any)
	row, _ := assignments[assignmentID].(map[string]any)
	return row != nil && workstate.ReviewAssignmentActivity(row) == "planned" && row["queued_agent_id"] == agentID
}

func assignedResponsibilities(value manifest) []string {
	seen := map[string]bool{}
	var ids []string
	for _, item := range value.ResponsibilityDispositions {
		if item.Disposition != "assigned" || item.ResponsibilityID == "" || seen[item.ResponsibilityID] {
			continue
		}
		seen[item.ResponsibilityID] = true
		ids = append(ids, item.ResponsibilityID)
	}
	if len(ids) > 0 {
		return ids
	}
	for _, item := range value.Assignments {
		if item.ResponsibilityID == "" || seen[item.ResponsibilityID] {
			continue
		}
		seen[item.ResponsibilityID] = true
		ids = append(ids, item.ResponsibilityID)
	}
	return ids
}

func validateWorkgroupState(kind string, runtimeState map[string]any) error {
	lifecycle, _ := runtimeState["lifecycle"].(map[string]any)
	state, _ := lifecycle["state"].(string)
	phase, _ := lifecycle["phase"].(string)
	switch kind {
	case "document_verifier":
		if state != "document_verification" {
			return fmt.Errorf("document verifier workgroup requires document_verification state")
		}
	case "delivery_verifier", "qa", "e2e_browser":
		// L3-S7: reviewers dispatch behind a registered ReviewPlan. The
		// phase machine is a plan-status projection; ordinary findings
		// (cannot_clean / discovery_draining) never stop safe discovery.
		if state != "verification" {
			return fmt.Errorf("%s workgroup requires verification state", kind)
		}
		switch phase {
		case "running", "cannot_clean", "discovery_draining":
		default:
			return fmt.Errorf("%s workgroup requires a registered ReviewPlan (phase running/cannot_clean/discovery_draining, current %s); complete and save the original ReviewPlan questionnaire first", kind, phase)
		}
	case "builder":
		if state != "building" && state != "bug_resolution" {
			return fmt.Errorf("builder workgroup requires building or bug_resolution state")
		}
	case "investigator":
		if state != "bug_resolution" {
			return fmt.Errorf("investigator workgroup requires bug_resolution state (current %s)", state)
		}
		if phase != "investigation" && !investigationReentry(runtimeState) {
			return fmt.Errorf("investigator workgroup requires bug_resolution.investigation (current %s.%s); an investigate_more re-entry keeps the phase at the S9 checkpoint that earlier plan-report submissions advanced — signal the re-entry with `runtime investigation consume --case-id <case>` and inspect `runtime investigation status` before retrying", state, phase)
		}
	default:
		return fmt.Errorf("unsupported workgroup kind %q", kind)
	}
	return nil
}

// investigationReentry reports whether the active InvestigationCase is back in
// S8 investigation after an investigate_more re-entry. Contract approval is
// already phase-agnostic within bug_resolution for the same reason
// (internal/investigation/contract.go: earlier plan-report side effects may
// have advanced the phase past investigation), and every other dispatch guard
// reads the Case pointer — so the pointer is the authority here as well.
func investigationReentry(runtimeState map[string]any) bool {
	review, _ := runtimeState["review"].(map[string]any)
	if review == nil {
		return false
	}
	pointer, _ := review["investigation"].(map[string]any)
	if pointer == nil {
		return false
	}
	status, _ := pointer["status"].(string)
	route, _ := pointer["route"].(string)
	return status == "investigating" && route == "investigate_more"
}

func repositoryPath(root, path string) string {
	if relative, err := filepath.Rel(root, path); err == nil && relative != ".." &&
		!filepath.IsAbs(relative) {
		return filepath.ToSlash(relative)
	}
	return filepath.ToSlash(path)
}

func containsEntity(value any, id string) bool {
	items, _ := value.([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["id"] == id {
			return true
		}
	}
	return false
}

func stateArtifactPaths(state map[string]any) []string {
	seen := map[string]bool{}
	var visit func(any)
	visit = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			for _, child := range typed {
				visit(child)
			}
		case []any:
			for _, child := range typed {
				visit(child)
			}
		case string:
			path := filepath.ToSlash(typed)
			if strings.HasPrefix(path, ".claude/") {
				seen[path] = true
			}
		}
	}
	visit(state)
	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

// =============================================================================
// TASK-015 / BUG-003 §4b.2(b) — RegisterTask runtime operation.
// =============================================================================

// RegisterTaskRequest is the wire shape for the runtime register-task
// operation. It validates that:
//   - Path exists on disk.
//   - OwnerAgentIDs is non-empty and each owner is registered in
//     entities.agents.
//   - SourceContractID is a non-empty primary contract reference.
//   - ID is not already present in entities.tasks (duplicate id rejected).
type RegisterTaskRequest struct {
	ExpectedRevision int
	TaskID           string   // matches ^TASK-[0-9]{3,}(?:-[0-9]{2,})?$
	Path             string   // canonical task spec path
	OwnerAgentIDs    []string // must exist in entities.agents
	SourceContractID string   // primary contract reference (e.g. "BUG-003")
	OccurredAt       time.Time
}

// ErrMissingTaskPath is returned when req.Path is empty or unreadable.
type ErrMissingTaskPath struct {
	Path string
}

func (e *ErrMissingTaskPath) Error() string {
	return fmt.Sprintf("task path %q missing or unreadable", e.Path)
}

// ErrMissingTaskOwner is returned when req.OwnerAgentIDs is empty.
type ErrMissingTaskOwner struct{}

func (e *ErrMissingTaskOwner) Error() string { return "task owner_agent_ids is required" }

// ErrDuplicateTaskID is returned when the supplied task id already exists in
// entities.tasks.
type ErrDuplicateTaskID struct {
	TaskID string
}

func (e *ErrDuplicateTaskID) Error() string {
	return fmt.Sprintf("task %s already registered", e.TaskID)
}

// taskIDPattern matches the canonical TASK id format shared with the S4
// planning check (semantic.taskIDPattern) and the runtime loop-state schema:
// TASK-NNN with an optional numeric sub-suffix (TASK-001, TASK-001-01).
var taskIDPattern = regexp.MustCompile(`^TASK-[0-9]{3,}(?:-[0-9]{2,})?$`)

// manifestHint translates r97-native failure shapes (B/C roads) into the
// concrete missing piece. It only appends guidance; the underlying
// validation judgment is unchanged.
func manifestHint(err error, hints []struct{ needle, hint string }) string {
	msg := err.Error()
	for _, h := range hints {
		if strings.Contains(msg, h.needle) {
			return " — fix: " + h.hint
		}
	}
	return ""
}

func manifestSchemaHint(err error) string {
	return manifestHint(err, []struct{ needle, hint string }{
		{"assignment_record_contract", "S6-only contract fields (s6-v1.2) do not belong in a document-verifier manifest; drop assignment_record_contract/closing_contract s6 values"},
		{"closing_contract", "s6-closing-v1 is Builder-only; a document-verifier assignment carries no closing_contract"},
		{"integration_mode", "document-verifier assignments use dispatch_mode plan_checkpoint, not S6 integration fields"},
		{"na_rationale", "every responsibility_disposition needs explicit na_rationale and evidence_ref (null when assigned)"},
		{"version", "every document reference needs id/path/version; version comes from the Runtime documents registry or the file Version header"},
	})
}

func manifestSemanticsHint(err error) string {
	return manifestHint(err, []struct{ needle, hint string }{
		{"planned_agent_count", "the two reviewer engineering owner IDs must be distinct; Claude Code assigns native platform actor IDs"},
		{"separation edge", "both DV assignments must exist with different agent_ids so the independence edge resolves"},
		{"missing mandatory responsibility", "a document_verifier manifest needs both DV-SPEC-CONSISTENCY and DV-TASK-EXECUTABILITY assignments"},
		{"requires skill", "reviewer skill_refs come from the registered responsibility profile: DV-SPEC-CONSISTENCY needs document-verification/specification-planning/api-contracts, DV-TASK-EXECUTABILITY needs document-verification/specification-planning/testing-strategy"},
		{"max_parallel_agents", "S5 runs both reviewers in parallel: max_parallel_agents=2, planned_agent_count=2"},
		{"does not match ^TASK-", "pass a canonical TASK entity id as --task-id (digits only, e.g. TASK-001-01 mapped from the plan filename); never a REQ id; --task is the actual plan file path"},
	})
}

// RegisterTask adds a canonical TASK entity to state.entities.tasks in the
// "candidate" state. The operation uses CAS-safe append via
// loopruntime.Store.Update.
func RegisterTask(root, statePath, journalPath string, req RegisterTaskRequest) (loopruntime.Snapshot, error) {
	if !taskIDPattern.MatchString(req.TaskID) {
		return loopruntime.Snapshot{}, fmt.Errorf("task id %q does not match ^TASK-[0-9]{3,}(?:-[0-9]{2,})?$ (canonical shape shared with the S4 planning check and the runtime schema)", req.TaskID)
	}
	if req.Path == "" {
		return loopruntime.Snapshot{}, &ErrMissingTaskPath{}
	}
	absPath := filepath.Join(root, req.Path)
	if _, err := os.ReadFile(absPath); err != nil {
		return loopruntime.Snapshot{}, &ErrMissingTaskPath{Path: req.Path}
	}
	if len(req.OwnerAgentIDs) == 0 {
		return loopruntime.Snapshot{}, &ErrMissingTaskOwner{}
	}
	if req.SourceContractID == "" {
		return loopruntime.Snapshot{}, fmt.Errorf("source_contract_id is required")
	}

	stateData, err := os.ReadFile(statePath)
	if err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("read runtime: %w", err)
	}
	var current map[string]any
	if err := json.Unmarshal(stateData, &current); err != nil {
		return loopruntime.Snapshot{}, fmt.Errorf("decode runtime: %w", err)
	}
	if _, err := commitRevision(req.ExpectedRevision, current); err != nil {
		return loopruntime.Snapshot{}, err
	}
	runtimeID, _ := current["runtime_id"].(string)
	lifecycle, _ := current["lifecycle"].(map[string]any)
	cursor := map[string]any{
		"state": lifecycle["state"],
		"phase": lifecycle["phase"],
	}
	taskRef := repositoryPath(root, absPath)
	occurredAt := req.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	store := loopruntime.NewWriter(statePath, journalPath, root, semantic.RuntimeCandidateValidator{})
	return updateRuntime(store, req.ExpectedRevision, loopruntime.Mutation{
		// The identity of a TASK registration is the registered TASK and the
		// canonical task document it registers. The TASK id is unique per
		// registration, so no Runtime write counter is needed (G18 §1/§1.3).
		EventID:        fmt.Sprintf("evt-register-task-%s", req.TaskID),
		TransitionID:   "ENTITY-REGISTER",
		Event:          "task_registered",
		Actor:          "orchestrator",
		IdempotencyKey: fmt.Sprintf("runtime:register:task:%s:%s", req.TaskID, taskRef),
		From:           cursor,
		To:             cursor,
		RuntimeID:      runtimeID,
		Message:        fmt.Sprintf("Registered canonical TASK %s", req.TaskID),
		OccurredAt:     occurredAt,
		EvidenceIDs:    []string{taskRef},
		Apply: func(state map[string]any) error {
			entities, ok := state["entities"].(map[string]any)
			if !ok {
				return fmt.Errorf("entities must be an object")
			}
			tasks, _ := entities["tasks"].([]any)
			for _, raw := range tasks {
				t, _ := raw.(map[string]any)
				id, _ := t["id"].(string)
				if id == req.TaskID {
					return &ErrDuplicateTaskID{TaskID: req.TaskID}
				}
			}
			agents, _ := entities["agents"].([]any)
			known := map[string]bool{}
			for _, raw := range agents {
				agent, _ := raw.(map[string]any)
				id, _ := agent["id"].(string)
				known[id] = true
			}
			for _, owner := range req.OwnerAgentIDs {
				if !known[owner] {
					return fmt.Errorf("task owner %q is not registered", owner)
				}
			}
			owners := make([]any, len(req.OwnerAgentIDs))
			for i, owner := range req.OwnerAgentIDs {
				owners[i] = owner
			}
			newTask := map[string]any{
				"id":              req.TaskID,
				"state":           "candidate",
				"path":            taskRef,
				"owner_agent_ids": owners,
			}
			if err := workstate.RecordTaskActivity(state, newTask, "candidate", "task_registered"); err != nil {
				return fmt.Errorf("record registered TASK work state: %w", err)
			}
			entities["tasks"] = append(tasks, newTask)
			state["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
			return nil
		},
	})
}
