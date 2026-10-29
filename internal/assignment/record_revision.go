package assignment

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/entroforge/go-system-builder/internal/assignmentinputs"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/dispatch"
	"github.com/entroforge/go-system-builder/internal/identity"
	loopruntime "github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/team"
	"github.com/entroforge/go-system-builder/internal/transition"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workspace"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

var revisionIntentKeyRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)

// RevisionRequest identifies an explicit request to issue the next immutable
// revision of one Assignment. The Writer chooses the next revision number.
type RevisionRequest struct {
	AssignmentID         string
	ExpectedRecord       assignmentrecord.RecordReference
	ExpectedRecordSHA256 string
	IntentKey            string
}

func validateRevisionRequest(request RevisionRequest) error {
	if len(request.IntentKey) > 1024 || !revisionIntentKeyRE.MatchString(request.IntentKey) {
		return fmt.Errorf("Assignment revision intent key must be a stable non-empty token of at most 1024 characters")
	}
	if request.AssignmentID == "" {
		return fmt.Errorf("Assignment revision requires an Assignment id")
	}
	if zeroRecordReference(request.ExpectedRecord) != (request.ExpectedRecordSHA256 == "") {
		return fmt.Errorf("expected Assignment Record reference and SHA-256 must be supplied together")
	}
	if request.ExpectedRecordSHA256 != "" && !validRevisionSHA(request.ExpectedRecordSHA256) {
		return fmt.Errorf("expected Assignment Record SHA-256 is invalid")
	}
	return nil
}

func zeroRecordReference(ref assignmentrecord.RecordReference) bool {
	return ref.RuntimeID == "" && ref.BaselineGeneration == 0 && ref.AssignmentID == "" && ref.Revision == 0
}

type assignmentRevisionRequestIdentity struct {
	Contract       string                           `json:"contract"`
	RuntimeID      string                           `json:"runtime_id"`
	Generation     int                              `json:"baseline_generation"`
	AssignmentID   string                           `json:"assignment_id"`
	IntentKey      string                           `json:"intent_key"`
	Previous       assignmentrecord.RecordReference `json:"previous_record"`
	PreviousSHA256 string                           `json:"previous_record_sha256"`
	ManifestRef    string                           `json:"manifest_ref"`
	ManifestSHA256 string                           `json:"manifest_sha256"`
	TaskID         string                           `json:"task_id"`
	TaskRef        string                           `json:"task_ref"`
	TaskSHA256     string                           `json:"task_sha256"`
	ContextSources []assignmentrecord.ContextSource `json:"context_sources"`
}

func revisionContextSources(root string, state map[string]any, manifestBytes []byte, manifestRef, taskRef, taskID string, taskBytes []byte) ([]assignmentrecord.ContextSource, error) {
	files, err := dispatch.View(root, state)
	if err != nil {
		return nil, fmt.Errorf("open current Assignment revision source view: %w", err)
	}
	actualManifest, err := readRecordRegistrationSource(root, files, manifestRef)
	if err != nil || !bytes.Equal(actualManifest, manifestBytes) {
		return nil, fmt.Errorf("revision manifest bytes differ from the original source view and checkout")
	}
	actualTask, err := readRecordRegistrationSource(root, files, taskRef)
	if err != nil || !bytes.Equal(actualTask, taskBytes) {
		return nil, fmt.Errorf("revision TASK bytes differ from the original source view and checkout")
	}
	var source recordRegistrationManifest
	if err := json.Unmarshal(manifestBytes, &source); err != nil {
		return nil, fmt.Errorf("decode revision manifest sources: %w", err)
	}
	if source.WorkgroupKind != "builder" || source.ManifestID == "" || source.Version == "" || len(source.Assignments) != 1 || source.Assignments[0].AssignmentID == "" {
		return nil, fmt.Errorf("revision manifest source identity is incomplete")
	}
	var manifestTask *team.DocumentReference
	for i := range source.Documents {
		if source.Documents[i].ID == taskID {
			if manifestTask != nil {
				return nil, fmt.Errorf("revision manifest duplicates TASK source %s", taskID)
			}
			manifestTask = &source.Documents[i]
		}
	}
	if manifestTask == nil || manifestTask.Path != taskRef || manifestTask.SHA256 != sha256Of(taskBytes) || manifestTask.Version != recordRegistrationLockedTaskVersion(state, taskID) {
		return nil, fmt.Errorf("revision manifest TASK source differs from the current locked TASK")
	}
	common, err := recordRegistrationSources(root, files, source)
	if err != nil {
		return nil, fmt.Errorf("resolve revision manifest sources: %w", err)
	}
	assignmentSources, err := recordRegistrationAssignmentSources(root, files, source.Assignments[0])
	if err != nil {
		return nil, fmt.Errorf("resolve revision Assignment sources: %w", err)
	}
	context := []assignmentrecord.ContextSource{
		{Kind: "manifest", Ref: manifestRef, ID: source.ManifestID, Version: source.Version, SHA256: sha256Of(manifestBytes)},
		{Kind: "task", Ref: taskRef, ID: taskID, Version: manifestTask.Version, SHA256: manifestTask.SHA256},
	}
	for _, doc := range source.Documents {
		context = append(context, assignmentrecord.ContextSource{Kind: "manifest_document", Ref: doc.Path, ID: doc.ID, Version: doc.Version, SHA256: doc.SHA256})
	}
	context = append(context, common...)
	context = append(context, assignmentSources...)
	item := source.Assignments[0]
	for _, profile := range item.CheckProfiles {
		context = append(context, assignmentrecord.ContextSource{Kind: "check_profile", Ref: manifestRef + "#assignments/" + item.AssignmentID + "/check_profiles/" + profile.ID, ID: profile.ID, Version: profile.Version, SHA256: profile.Digest()})
		for _, dependency := range profile.Dependencies {
			context = append(context, assignmentrecord.ContextSource{Kind: "check_profile_dependency", Ref: dependency.Path, SHA256: dependency.SHA256})
		}
		for _, fixture := range profile.Fixtures {
			context = append(context, assignmentrecord.ContextSource{Kind: "check_profile_fixture", Ref: fixture.Path, SHA256: fixture.SHA256})
		}
	}
	if item.ClosingContract != nil {
		for _, check := range item.ClosingContract.WorkerChecks {
			for _, dependency := range check.Profile.Dependencies {
				context = append(context, assignmentrecord.ContextSource{Kind: "worker_check_profile_dependency", Ref: dependency.Path, ID: check.Profile.ID, SHA256: dependency.SHA256})
			}
			for _, fixture := range check.Profile.Fixtures {
				context = append(context, assignmentrecord.ContextSource{Kind: "worker_check_profile_fixture", Ref: fixture.Path, ID: check.Profile.ID, SHA256: fixture.SHA256})
			}
		}
	}
	sort.Slice(context, func(i, j int) bool {
		if context[i].Kind != context[j].Kind {
			return context[i].Kind < context[j].Kind
		}
		if context[i].Ref != context[j].Ref {
			return context[i].Ref < context[j].Ref
		}
		return context[i].ID < context[j].ID
	})
	for i := 1; i < len(context); i++ {
		if context[i-1].Kind == context[i].Kind && context[i-1].Ref == context[i].Ref && context[i-1].ID == context[i].ID {
			return nil, fmt.Errorf("revision source closure duplicates %s", context[i].Ref)
		}
	}
	if err := files.Verify(); err != nil {
		return nil, fmt.Errorf("revision source view changed during closure: %w", err)
	}
	return context, nil
}

func assignmentRevisionRequestDigest(runtimeID string, generation int, request RevisionRequest, manifestRef string, manifestBytes []byte, taskID, taskRef string, taskBytes []byte, contextSources []assignmentrecord.ContextSource) (string, error) {
	identity := assignmentRevisionRequestIdentity{
		Contract: "s6-assignment-revision-request-v1", RuntimeID: runtimeID,
		Generation: generation, AssignmentID: request.AssignmentID,
		IntentKey: request.IntentKey, Previous: request.ExpectedRecord,
		PreviousSHA256: request.ExpectedRecordSHA256, ManifestRef: manifestRef,
		ManifestSHA256: sha256Of(manifestBytes), TaskID: taskID,
		TaskRef: taskRef, TaskSHA256: sha256Of(taskBytes), ContextSources: contextSources,
	}
	data, err := json.Marshal(identity)
	if err != nil {
		return "", err
	}
	return sha256Of(data), nil
}

func generationFromState(state map[string]any) int {
	baseline, _ := state["baseline"].(map[string]any)
	return dispatch.Number(baseline["generation"])
}

func readRuntimeSnapshot(path string) (loopruntime.Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return loopruntime.Snapshot{}, err
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		return loopruntime.Snapshot{}, err
	}
	return loopruntime.Snapshot{Revision: dispatch.Number(state["revision"]), State: state}, nil
}

func readRevisionJournal(path string) ([]byte, error) {
	paths, err := loopruntime.JournalSegmentPaths(path)
	if err != nil {
		return nil, err
	}
	var out []byte
	for _, segment := range paths {
		part, err := os.ReadFile(segment)
		if err != nil {
			return nil, err
		}
		out = append(out, part...)
	}
	active, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return append(out, active...), nil
}

func findRevisionReplay(root string, state map[string]any, request RevisionRequest, manifestRef, taskID, taskRef string, manifestBytes, taskBytes []byte, contextSources []assignmentrecord.ContextSource) (loopruntime.Snapshot, bool, error) {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["assignment_revision_receipts"].([]any)
	runtimeID, generation, _, err := assignmentrecord.CurrentRuntimeBinding(state)
	if err != nil {
		return loopruntime.Snapshot{}, false, err
	}
	for _, raw := range rows {
		data, err := json.Marshal(raw)
		if err != nil {
			return loopruntime.Snapshot{}, false, err
		}
		var receipt loopruntime.AssignmentRevisionReceipt
		if err := json.Unmarshal(data, &receipt); err != nil {
			return loopruntime.Snapshot{}, false, fmt.Errorf("decode Assignment revision receipt: %w", err)
		}
		if receipt.IntentKey != request.IntentKey || receipt.AssignmentRecord.RuntimeID != runtimeID || receipt.AssignmentRecord.BaselineGeneration != generation || receipt.AssignmentRecord.AssignmentID != request.AssignmentID {
			continue
		}
		if !zeroRecordReference(request.ExpectedRecord) && (request.ExpectedRecord != receipt.PreviousRecord || request.ExpectedRecordSHA256 != receipt.PreviousRecordSHA256) {
			return loopruntime.Snapshot{}, false, fmt.Errorf("Assignment revision intent key was already issued from a different expected Record")
		}
		digest, err := assignmentRevisionRequestDigest(runtimeID, generation, RevisionRequest{AssignmentID: request.AssignmentID, IntentKey: request.IntentKey, ExpectedRecord: receipt.PreviousRecord, ExpectedRecordSHA256: receipt.PreviousRecordSHA256}, manifestRef, manifestBytes, taskID, taskRef, taskBytes, contextSources)
		if err != nil {
			return loopruntime.Snapshot{}, false, err
		}
		if digest != receipt.RequestDigest {
			return loopruntime.Snapshot{}, false, fmt.Errorf("Assignment revision intent key %q was already issued for different input bytes", request.IntentKey)
		}
		records, err := assignmentrecord.RecordsFromState(state)
		if err != nil {
			return loopruntime.Snapshot{}, false, err
		}
		record, found, err := assignmentrecord.FindRecordRevision(records, receipt.AssignmentRecord)
		if err != nil || !found {
			return loopruntime.Snapshot{}, false, fmt.Errorf("replayed Assignment revision Record is missing: %v", err)
		}
		recordSHA, err := record.Digest()
		if err != nil || recordSHA != receipt.AssignmentRecordSHA256 || record.Manifest.Ref != manifestRef || record.Task.ID != taskID || record.Task.Ref != taskRef || record.Task.SHA256 != sha256Of(taskBytes) {
			return loopruntime.Snapshot{}, false, fmt.Errorf("replayed Assignment revision no longer matches its immutable Record")
		}
		return loopruntime.Snapshot{Revision: dispatch.Number(state["revision"]), State: state}, true, nil
	}
	return loopruntime.Snapshot{}, false, nil
}

func revisionAcceptanceReceipt(snapshot loopruntime.Snapshot, request RevisionRequest, replayed bool) (*RevisionAcceptanceReceipt, error) {
	runtimeID, generation, _, err := assignmentrecord.CurrentRuntimeBinding(snapshot.State)
	if err != nil {
		return nil, err
	}
	entities, _ := snapshot.State["entities"].(map[string]any)
	rawReceipts, _ := entities["assignment_revision_receipts"]
	encoded, err := json.Marshal(rawReceipts)
	if err != nil {
		return nil, err
	}
	var receipts []loopruntime.AssignmentRevisionReceipt
	if err := json.Unmarshal(encoded, &receipts); err != nil || receipts == nil {
		return nil, fmt.Errorf("Runtime revision receipt index is invalid")
	}
	var matched *loopruntime.AssignmentRevisionReceipt
	for i := range receipts {
		row := &receipts[i]
		if row.IntentKey != request.IntentKey || row.AssignmentRecord.RuntimeID != runtimeID ||
			row.AssignmentRecord.BaselineGeneration != generation || row.AssignmentRecord.AssignmentID != request.AssignmentID {
			continue
		}
		if !zeroRecordReference(request.ExpectedRecord) &&
			(row.PreviousRecord != request.ExpectedRecord || row.PreviousRecordSHA256 != request.ExpectedRecordSHA256) {
			continue
		}
		if matched != nil {
			return nil, fmt.Errorf("revision intent %q has more than one durable receipt", request.IntentKey)
		}
		matched = row
	}
	if matched == nil {
		return nil, fmt.Errorf("revision intent %q has no durable receipt", request.IntentKey)
	}
	current := false
	records, err := assignmentrecord.RecordsFromState(snapshot.State)
	if err != nil {
		return nil, err
	}
	currentRecord, found, err := assignmentrecord.FindRecord(records, runtimeID, generation, request.AssignmentID)
	if err != nil {
		return nil, err
	}
	if found && currentRecord.Reference() == matched.AssignmentRecord {
		currentSHA, digestErr := currentRecord.Digest()
		if digestErr != nil {
			return nil, digestErr
		}
		current = currentSHA == matched.AssignmentRecordSHA256
	}
	return &RevisionAcceptanceReceipt{
		AssignmentRevisionReceipt: *matched,
		Revision:                  matched.AssignmentRecord.Revision,
		Current:                   current,
		Replayed:                  replayed,
	}, nil
}

func checkRevisionDispatchRegistration(root string, state map[string]any, taskID string) error {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	if lifecycle == nil || lifecycle["state"] != "building" {
		return fmt.Errorf("Assignment revision requires the current building stage")
	}
	board, err := dispatch.Load(root, state, 1<<20)
	if err != nil {
		return err
	}
	if board.Legacy || board.Plan == nil || len(board.Plan.Problems) > 0 {
		return fmt.Errorf("Assignment revision requires the original registered dispatch plan")
	}
	found := false
	integrated := map[string]bool{}
	for _, row := range board.Rows {
		integrated[row.Task.ID] = row.Integrated && row.State == workstate.Done
	}
	for _, row := range board.Rows {
		if row.Task.ID != taskID {
			continue
		}
		if found {
			return fmt.Errorf("dispatch plan duplicates revision TASK %s", taskID)
		}
		found = true
		for _, dep := range append(append([]string(nil), row.Task.Dependencies...), row.Task.Before...) {
			if !integrated[dep] {
				return fmt.Errorf("TASK %s awaits verified integration of %s", taskID, dep)
			}
		}
		for _, other := range board.Rows {
			if other.Task.ID != taskID && (other.State == workstate.Active || other.State == workstate.Blocked) && semantic.DispatchConflict(row.Task, other.Task) {
				return fmt.Errorf("TASK %s conflicts with active %s", taskID, other.Task.ID)
			}
		}
	}
	if !found {
		return fmt.Errorf("TASK %s is outside the current approved dispatch plan", taskID)
	}
	return nil
}

func applyAssignmentRevision(root string, state map[string]any, value manifest, manifestBytes []byte, manifestRef, taskRef, taskID string, taskBytes []byte, request RevisionRequest, expectedDigest string, expectedSources []assignmentrecord.ContextSource, eventID string, occurredAt time.Time, staged *[]loopruntime.ArtifactCleanupRequest) (Record, error) {
	if !workstate.SupportsAssignments(state["schema_version"]) || value.WorkgroupKind != "builder" || len(value.Assignments) != 1 {
		return Record{}, fmt.Errorf("Assignment revision requires one canonical S6 Builder Assignment")
	}
	runtimeID, generation, _, err := assignmentrecord.CurrentRuntimeBinding(state)
	if err != nil {
		return Record{}, err
	}
	if err := checkRevisionDispatchRegistration(root, state, taskID); err != nil {
		return Record{}, err
	}
	records, err := assignmentrecord.RecordsFromState(state)
	if err != nil {
		return Record{}, err
	}
	previous, found, err := assignmentrecord.FindRecord(records, runtimeID, generation, request.AssignmentID)
	if err != nil || !found {
		return Record{}, fmt.Errorf("Assignment revision requires one current prior Record: %v", err)
	}
	previousSHA, err := previous.Digest()
	if err != nil {
		return Record{}, err
	}
	if request.ExpectedRecord != previous.Reference() || request.ExpectedRecordSHA256 != previousSHA {
		return Record{}, fmt.Errorf("stale Assignment revision: current Record changed before Writer apply")
	}
	if taskID != previous.Task.ID || taskRef != previous.Task.Ref || sha256Of(taskBytes) != previous.Task.SHA256 {
		return Record{}, fmt.Errorf("Assignment revision must retain the exact current locked TASK source %s (%s). Main must route formal TASK changes through original planning/document review; do not refresh fingerprints or repeat this revision to replace the specification", previous.Task.ID, previous.Task.Ref)
	}
	if value.WorkgroupID == previous.TeamID || containsEntity(entityRows(state, "teams"), value.WorkgroupID) {
		return Record{}, fmt.Errorf("Assignment revision requires a fresh Team/workgroup id")
	}
	item := value.Assignments[0]
	if item.AssignmentID != previous.AssignmentID || item.AgentID == previous.OwnerAgentID {
		return Record{}, fmt.Errorf("Assignment revision must retain the Assignment id and issue a fresh owner Agent")
	}
	if err := identity.ValidateAgentID(item.AgentID); err != nil {
		return Record{}, err
	}
	if containsEntity(entityRows(state, "agents"), item.AgentID) {
		return Record{}, fmt.Errorf("Assignment revision owner Agent %s must be new", item.AgentID)
	}
	mode := item.DispatchMode
	if mode == "" {
		mode = "plan_checkpoint"
	}
	if mode != "plan_checkpoint" {
		return Record{}, fmt.Errorf("Assignment revision currently requires plan_checkpoint; fresh human approval binding is not implemented")
	}
	if previous.DispatchMode == "plan_approval_required" {
		return Record{}, fmt.Errorf("Assignment revision cannot inherit old approval; fresh human approval binding is not implemented")
	}
	if err := verifyRevisionManifestIdentity(value, state, runtimeID, generation, taskID, previous); err != nil {
		return Record{}, err
	}
	if err := verifyRevisionTaskEntity(state, previous); err != nil {
		return Record{}, err
	}
	oldAgent, err := findRevisionAgent(state, previous.OwnerAgentID, previous)
	if err != nil {
		return Record{}, err
	}
	oldResultRef, _ := oldAgent["completion_reported_ref"].(string)
	oldTeam, err := findRevisionTeam(state, previous.TeamID, previous)
	if err != nil {
		return Record{}, err
	}
	previousSnapshots, previousSnapshotArtifacts, err := capturePreviousRecordSources(root, state, previous)
	if err != nil {
		return Record{}, err
	}
	if err := stageRevisionArtifacts(root, state, previousSnapshotArtifacts, staged); err != nil {
		return Record{}, err
	}
	requestDigest, err := assignmentRevisionRequestDigest(runtimeID, generation, request, manifestRef, manifestBytes, taskID, taskRef, taskBytes, expectedSources)
	if err != nil {
		return Record{}, err
	}
	newRef := assignmentrecord.RecordReference{RuntimeID: runtimeID, BaselineGeneration: generation, AssignmentID: request.AssignmentID, Revision: previous.Revision + 1}
	if eventID != loopruntime.AssignmentRevisionEventID(previous.Reference(), request.IntentKey) {
		return Record{}, fmt.Errorf("Assignment revision event id differs from its bound prior Record and intent")
	}

	registry, err := workspace.Decode(state)
	if err != nil {
		return Record{}, fmt.Errorf("validate workspace execution before Assignment revision: %w", err)
	}
	if registry != nil {
		if current, exists := registry.Executions[request.AssignmentID]; exists {
			if current.RuntimeID != runtimeID || current.BaselineGeneration != generation || current.AssignmentID != request.AssignmentID || current.AgentID != previous.OwnerAgentID || current.AssignmentRecord == nil || *current.AssignmentRecord != previous.Reference() || current.AssignmentRecordSHA256 != previousSHA {
				return Record{}, fmt.Errorf("current workspace execution does not bind the prior Assignment revision")
			}
			current.Status = "retired"
			registry.History = append(registry.History, current)
			delete(registry.Executions, request.AssignmentID)
			state["workspace"] = workspace.Encode(registry)
		}
	}

	entities, ok := state["entities"].(map[string]any)
	if !ok {
		return Record{}, fmt.Errorf("Runtime entities are missing")
	}
	catalog, err := transition.LoadCatalog(root)
	if err != nil {
		return Record{}, err
	}
	task, owners, err := replaceRevisionTaskOwner(state, previous, item.AgentID, taskEntityTransitions(catalog), occurredAt)
	if err != nil {
		return Record{}, err
	}
	if err := supersedePreviousResultEvidence(state, oldResultRef, previous.OwnerAgentID, generation, eventID); err != nil {
		return Record{}, err
	}
	_ = task
	_ = owners

	teams := entityRows(state, "teams")
	teamRow := map[string]any{
		"id": value.WorkgroupID, "platform_team_id": value.PlatformTeamID,
		"kind": value.WorkgroupKind, "status": "planned", "manifest_ref": manifestRef,
		"responsibility_ids": assignedResponsibilities(value), "agent_ids": []string{item.AgentID},
		"review_round": value.ReviewRound,
	}
	entities["teams"] = append(teams, teamRow)

	agents := entityRows(state, "agents")
	for _, agent := range agents {
		row, _ := agent.(map[string]any)
		if row["id"] != oldAgent["id"] {
			continue
		}
		if err := stopRevisionOwner(root, state, row, occurredAt); err != nil {
			return Record{}, err
		}
	}
	newAgent := map[string]any{
		"id": item.AgentID, "role": item.RoleFamily, "state": "reading",
		"task_ids": []string{taskID}, "team_id": value.WorkgroupID,
		"definition_ref": item.AgentDefinitionRef, "prompt_ref": manifestRef + "#" + item.AssignmentID,
		"dispatch_mode": mode, "readback_ref": nil, "activation_ref": nil,
		"activation_revision": nil, "updated_at": occurredAt.UTC().Format(time.RFC3339Nano),
	}
	activity := "reading"
	if value.AssignmentRecordContract == assignmentrecord.NativeDeliveryContract {
		activity = "queued"
	}
	if err := workstate.RecordAgentActivity(state, newAgent, activity, "workgroup_registered"); err != nil {
		return Record{}, fmt.Errorf("record revised owner Agent work state: %w", err)
	}
	entities["agents"] = append(agents, newAgent)
	if err := retirePreviousTeamIfUnused(state, records, oldTeam, previous, item.AgentID); err != nil {
		return Record{}, err
	}

	newRecords, err := buildRegisteredAssignmentRecords(root, state, manifestBytes, manifestRef, taskRef, taskID, taskBytes)
	if err != nil {
		return Record{}, fmt.Errorf("build revised Assignment Record: %w", err)
	}
	if len(newRecords) != 1 {
		return Record{}, fmt.Errorf("Assignment revision must construct exactly one new Record")
	}
	record := newRecords[0]
	if !reflect.DeepEqual(record.ContextSources, expectedSources) {
		return Record{}, fmt.Errorf("Assignment revision context sources changed between preflight and the original Writer")
	}
	record.Revision = newRef.Revision
	requestDigest, err = assignmentRevisionRequestDigest(runtimeID, generation, request, manifestRef, manifestBytes, taskID, taskRef, taskBytes, record.ContextSources)
	if err != nil {
		return Record{}, err
	}
	if requestDigest != expectedDigest {
		return Record{}, fmt.Errorf("Assignment revision request source identity changed before Writer apply")
	}
	record.RevisionIssuance = &assignmentrecord.RevisionIssuance{
		IntentKey: request.IntentKey, RequestDigest: requestDigest,
		PreviousRecord: previous.Reference(), PreviousRecordSHA256: previousSHA,
	}
	if err := assignmentrecord.ValidateRecord(record); err != nil {
		return Record{}, fmt.Errorf("validate revised Assignment Record: %w", err)
	}
	record, err = stageRegisteredRecordSources(root, state, record, staged)
	if err != nil {
		return Record{}, err
	}
	rows, err := assignmentrecord.RecordsFromState(state)
	if err != nil {
		return Record{}, err
	}
	for _, existing := range rows {
		if existing.Reference() == record.Reference() {
			return Record{}, fmt.Errorf("Assignment revision Record already exists")
		}
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return Record{}, err
	}
	var recordRow map[string]any
	if err := json.Unmarshal(encoded, &recordRow); err != nil {
		return Record{}, err
	}
	assignmentRows, _ := entities["assignments"].([]any)
	entities["assignments"] = append(assignmentRows, recordRow)

	if err := preStageRevisionActivation(root, state, value, taskID, item, record, newAgent, occurredAt, staged); err != nil {
		return Record{}, err
	}
	newSHA, err := record.Digest()
	if err != nil {
		return Record{}, err
	}
	commitRevision, err := commitRevision(-1, state)
	if err != nil {
		return Record{}, err
	}
	receipt := loopruntime.AssignmentRevisionReceipt{
		IntentKey: request.IntentKey, RequestDigest: requestDigest,
		AssignmentRecord: record.Reference(), AssignmentRecordSHA256: newSHA,
		PreviousRecord: previous.Reference(), PreviousRecordSHA256: previousSHA,
		RuntimeEventID: eventID, RuntimeCommitRevision: commitRevision + 1,
		PreviousSourceSnapshots: previousSnapshots,
	}
	receiptBytes, err := json.Marshal(receipt)
	if err != nil {
		return Record{}, err
	}
	var receiptRow map[string]any
	if err := json.Unmarshal(receiptBytes, &receiptRow); err != nil {
		return Record{}, err
	}
	receipts, _ := entities["assignment_revision_receipts"].([]any)
	if len(receipts) >= 1024 {
		return Record{}, fmt.Errorf("Assignment revision receipt index is full")
	}
	entities["assignment_revision_receipts"] = append(receipts, receiptRow)
	state["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
	return record, nil
}

func supersedePreviousResultEvidence(state map[string]any, resultRef, owner string, generation int, eventID string) error {
	if resultRef == "" {
		return nil
	}
	rows, _ := state["evidence"].([]any)
	matched := 0
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row == nil || row["kind"] != "completion_report" || row["path"] != resultRef {
			continue
		}
		if row["status"] != "valid" || row["invalidated_by"] != nil || dispatch.Number(row["baseline_generation"]) != generation {
			return fmt.Errorf("prior Result evidence %s is no longer the current valid evidence", resultRef)
		}
		producers, _ := row["produced_by"].([]any)
		if !stringArrayContains(producers, owner) {
			return fmt.Errorf("prior Result evidence %s is not owned by previous Agent %s", resultRef, owner)
		}
		row["status"] = "superseded"
		row["invalidated_by"] = eventID
		row["invalidation_rule"] = "assignment_revision_issued"
		row["invalidation_reason"] = "superseded by the next immutable Assignment revision"
		matched++
	}
	if matched > 1 {
		return fmt.Errorf("prior Result evidence %s is ambiguous", resultRef)
	}
	return nil
}

func verifyRevisionManifestIdentity(value manifest, state map[string]any, runtimeID string, generation int, taskID string, previous Record) error {
	if value.WorkgroupKind != "builder" || value.WorkgroupID == "" || value.ManifestID == "" || len(value.Assignments) != 1 || value.Assignments[0].AssignmentID != previous.AssignmentID {
		return fmt.Errorf("revision manifest must represent exactly the current Assignment in a fresh Builder workgroup")
	}
	if runtimeID == "" || generation < 1 || taskID != previous.Task.ID {
		return fmt.Errorf("revision manifest Runtime/TASK binding is invalid")
	}
	bound, _ := state["bound_req"].(map[string]any)
	if bound == nil || bound["status"] != "locked" || bound["id"] == nil {
		return fmt.Errorf("Assignment revision requires the current locked REQ")
	}
	return nil
}

func verifyRevisionTaskEntity(state map[string]any, previous Record) error {
	version := fmt.Sprint(state["schema_version"])
	for _, raw := range entityRows(state, "tasks") {
		row, _ := raw.(map[string]any)
		if row["id"] != previous.Task.ID {
			continue
		}
		if row["path"] != previous.Task.Ref || row["sha256"] != previous.Task.SHA256 {
			return fmt.Errorf("Assignment revision TASK entity differs from its immutable locked source")
		}
		projection, err := workstate.TaskProjection(row, version)
		if err != nil {
			return fmt.Errorf("Assignment revision TASK lifecycle: %w", err)
		}
		switch projection.Activity {
		case "locked", "in_progress", "review", "blocked", "done":
			return nil
		case "cancelled":
			return fmt.Errorf("cancelled TASK %s cannot be revised", previous.Task.ID)
		default:
			return fmt.Errorf("TASK %s is not in a revision-eligible state", previous.Task.ID)
		}
	}
	return fmt.Errorf("Assignment revision TASK entity is missing")
}

func findRevisionAgent(state map[string]any, owner string, record Record) (map[string]any, error) {
	var found map[string]any
	for _, raw := range entityRows(state, "agents") {
		row, _ := raw.(map[string]any)
		if row["id"] != owner {
			continue
		}
		if found != nil || row["team_id"] != record.TeamID || row["role"] != record.RoleFamily || row["definition_ref"] != record.AgentDefinition.Ref || row["dispatch_mode"] != record.DispatchMode || !containsStringValue(anyStrings(row["task_ids"]), record.Task.ID) {
			return nil, fmt.Errorf("previous Assignment owner Agent differs from its immutable Record")
		}
		found = row
	}
	if found == nil {
		return nil, fmt.Errorf("previous Assignment owner Agent is missing")
	}
	return found, nil
}

func findRevisionTeam(state map[string]any, teamID string, record Record) (map[string]any, error) {
	var found map[string]any
	for _, raw := range entityRows(state, "teams") {
		row, _ := raw.(map[string]any)
		if row["id"] != teamID {
			continue
		}
		if found != nil || row["manifest_ref"] != record.Manifest.Ref || row["kind"] != "builder" {
			return nil, fmt.Errorf("previous Assignment Team differs from its immutable Record")
		}
		found = row
	}
	if found == nil {
		return nil, fmt.Errorf("previous Assignment Team is missing")
	}
	return found, nil
}

func replaceRevisionTaskOwner(state map[string]any, previous Record, newOwner string, transitions taskEntityTransitionsResolver, at time.Time) (map[string]any, []string, error) {
	for _, raw := range entityRows(state, "tasks") {
		row, _ := raw.(map[string]any)
		if row["id"] != previous.Task.ID {
			continue
		}
		projection, err := workstate.TaskProjection(row, fmt.Sprint(state["schema_version"]))
		if err != nil {
			return nil, nil, fmt.Errorf("resolve TASK authorization activity: %w", err)
		}
		currentState := projection.Activity
		resolvedState, found := transitions.resolve(currentState, "assignment_revision_issued")
		if !found || resolvedState != "in_progress" {
			return nil, nil, fmt.Errorf("TASK state %q has no assignment_revision_issued transition to in_progress", currentState)
		}
		owners := anyStrings(row["owner_agent_ids"])
		if owners == nil || !containsStringValue(owners, previous.OwnerAgentID) {
			return nil, nil, fmt.Errorf("current TASK does not name the previous Assignment owner")
		}
		next := make([]string, 0, len(owners))
		for _, owner := range owners {
			if owner == previous.OwnerAgentID || owner == newOwner {
				continue
			}
			next = append(next, owner)
		}
		next = append(next, newOwner)
		sort.Strings(next)
		row["owner_agent_ids"] = next
		if err := workstate.RecordTaskActivity(state, row, resolvedState, "assignment_revision_issued"); err != nil {
			return nil, nil, fmt.Errorf("record revised TASK work state: %w", err)
		}
		for _, key := range []string{"activation_ref", "completion_report_ref", "verification_evidence", "blocker_evidence"} {
			delete(row, key)
		}
		row["updated_at"] = at.UTC().Format(time.RFC3339Nano)
		return row, next, nil
	}
	return nil, nil, fmt.Errorf("Assignment revision TASK is missing")
}

func stopRevisionOwner(root string, state, row map[string]any, occurredAt time.Time) error {
	version := fmt.Sprint(state["schema_version"])
	projection, err := workstate.AgentProjection(row, version)
	if err != nil {
		return fmt.Errorf("resolve prior owner Agent activity: %w", err)
	}
	status := projection.Activity
	for _, key := range []string{"readback_ref", "activation_ref", "activation_revision", "plan_reported_ref", "work_started_ref", "completion_reported_ref", "completion_acknowledged_ref", "work_blocked_ref", "blocker_resolved_ref", "shutdown_approved_ref"} {
		row[key] = nil
	}
	delete(row, "completion_acknowledged_reports")
	if workstate.Unified(version) {
		if status != "stopped" {
			// Assignment revision is the original new-attempt boundary. The
			// prior owner becomes cancelled even if its previous attempt was done.
			if status != "done" {
				catalog, err := transition.LoadCatalog(root)
				if err != nil {
					return err
				}
				to, ok := ResolveAgentTransition(catalog, status, "assignment_revision_issued")
				if !ok || to != "stopped" {
					return fmt.Errorf("prior owner Agent activity %q has no assignment_revision_issued transition to stopped", status)
				}
			}
			if err := workstate.RecordAgentActivity(state, row, "stopped", "assignment_revision_issued"); err != nil {
				return fmt.Errorf("record cancelled prior owner Agent: %w", err)
			}
		}
	} else if status != "done" && status != "stopped" {
		catalog, err := transition.LoadCatalog(root)
		if err != nil {
			return err
		}
		to, ok := ResolveAgentTransition(catalog, status, "assignment_revision_issued")
		if !ok || to != "stopped" {
			return fmt.Errorf("prior owner Agent state %q has no assignment_revision_issued transition to stopped", status)
		}
		row["state"] = to
	}
	row["updated_at"] = occurredAt.UTC().Format(time.RFC3339Nano)
	return nil
}

func retirePreviousTeamIfUnused(state map[string]any, records []Record, oldTeam map[string]any, previous Record, newOwner string) error {
	current := map[string]Record{}
	for _, record := range records {
		if record.RuntimeID != previous.RuntimeID || record.BaselineGeneration != previous.BaselineGeneration {
			continue
		}
		old, ok := current[record.AssignmentID]
		if !ok || record.Revision > old.Revision {
			current[record.AssignmentID] = record
		}
	}
	for _, record := range current {
		if record.AssignmentID != previous.AssignmentID && record.TeamID == previous.TeamID {
			return nil
		}
	}
	for _, raw := range entityRows(state, "agents") {
		row, _ := raw.(map[string]any)
		if row["id"] == previous.OwnerAgentID || row["id"] == newOwner || row["team_id"] != previous.TeamID {
			continue
		}
		status := workstate.AgentActivity(row)
		if status != "done" && status != "stopped" {
			return nil
		}
	}
	oldTeam["status"] = "retired"
	return nil
}

func capturePreviousRecordSources(root string, state map[string]any, previous Record) ([]assignmentrecord.SourceSnapshot, map[string][]byte, error) {
	if len(previous.SourceSnapshots) > 0 {
		if err := assignmentrecord.ValidateRecord(previous); err != nil {
			return nil, nil, err
		}
		for _, row := range previous.SourceSnapshots {
			data, err := workpacket.ReadFile(root, row.Path, 16<<20)
			if err != nil || sha256Of(data) != row.SHA256 {
				return nil, nil, fmt.Errorf("previous Assignment source snapshot is unverifiable: %s", row.Path)
			}
		}
		return append([]assignmentrecord.SourceSnapshot(nil), previous.SourceSnapshots...), map[string][]byte{}, nil
	}
	files, err := dispatch.View(root, state)
	if err != nil {
		return nil, nil, fmt.Errorf("open previous Assignment source view: %w", err)
	}
	captured, artifacts, err := assignmentrecord.CaptureSourceSnapshots(previous, func(ref string) ([]byte, error) {
		if strings.HasPrefix(ref, "gsb-embedded/") {
			return assignmentinputs.Read(root, ref)
		}
		return readRecordRegistrationSource(root, files, ref)
	})
	if err != nil {
		return nil, nil, fmt.Errorf("capture previous Assignment source audit: %w", err)
	}
	if err := files.Verify(); err != nil {
		return nil, nil, fmt.Errorf("previous Assignment source view changed during audit: %w", err)
	}
	return captured.SourceSnapshots, artifacts, nil
}

func stageRevisionArtifacts(root string, state map[string]any, artifacts map[string][]byte, staged *[]loopruntime.ArtifactCleanupRequest) error {
	revision, err := commitRevision(-1, state)
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(artifacts))
	for path := range artifacts {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	referenced := stateArtifactPaths(state)
	for _, path := range paths {
		body := artifacts[path]
		// Publish may have linked final bytes before a sync/readback error.
		// Track the cleanup intent first; the original Writer cleanup still
		// checks the actual digest and every live reference before removing it.
		*staged = append(*staged, loopruntime.ArtifactCleanupRequest{ExpectedRevision: revision, ArtifactPath: path, ArtifactSHA256: sha256Of(body), ReferencedPaths: referenced})
		published, err := workpacket.PublishBounded(root, path, body, 16<<20)
		if err != nil {
			return fmt.Errorf("publish prior Assignment source audit: %w", err)
		}
		if !bytes.Equal(body, published) {
			return fmt.Errorf("prior Assignment source snapshot conflicts with its immutable bytes")
		}
	}
	return nil
}

func preStageRevisionActivation(root string, state map[string]any, value manifest, taskID string, item assignment, record Record, agent map[string]any, occurredAt time.Time, staged *[]loopruntime.ArtifactCleanupRequest) error {
	if record.Contract == assignmentrecord.NativeDeliveryContract {
		return nil
	}
	ref := record.Reference()
	sha, err := record.Digest()
	if err != nil {
		return err
	}
	source := ActivationSourceEntry{AgentID: item.AgentID, AgentDefinitionRef: item.AgentDefinitionRef, SkillRefs: item.SkillRefs, WritePaths: append([]string(nil), record.EffectiveWritePaths...), AssignmentRecord: &ref, AssignmentRecordSHA256: sha}
	activationRef, err := PreStageActivationEnvelope(root, value.WorkgroupID, taskID, item.AgentID, "plan_checkpoint", source)
	if err != nil {
		return err
	}
	if activationRef == "" {
		return nil
	}
	envelope := buildActivationEnvelope(item.AgentID, source)
	activationBytes, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return err
	}
	*staged = append(*staged, loopruntime.ArtifactCleanupRequest{ExpectedRevision: dispatch.Number(state["revision"]), ArtifactPath: activationRef, ArtifactSHA256: sha256Of(append(activationBytes, '\n')), ReferencedPaths: stateArtifactPaths(state)})
	agent["activation_ref"] = activationRef
	_ = occurredAt
	return nil
}

func entityRows(state map[string]any, name string) []any {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities[name].([]any)
	return rows
}

func anyStrings(value any) []string {
	items, _ := value.([]any)
	if items == nil {
		if rows, ok := value.([]string); ok {
			return append([]string(nil), rows...)
		}
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if value, ok := item.(string); ok {
			out = append(out, value)
		}
	}
	return out
}

func containsStringValue(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func validRevisionSHA(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
