package runtime

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

// S5FormalFact is a read-only projection of a source proven by the accepted
// S4 packet. It is deliberately not written to Runtime.documents.
type S5FormalFact struct {
	Reference workpacket.Reference
	Kind      string
	Status    string
}

// S5FormalInputSet contains the exact inputs an S5 packet freezes and the
// additional source facts the quality gate may recognize under the same S4
// proof. Legacy runtimes without a typed S4 packet retain the old document
// projection.
type S5FormalInputSet struct {
	References []workpacket.Reference
	Facts      []S5FormalFact
}

// S5FormalInputs reconstructs the current S5 input closure. For modern S4 it
// derives unregistered source files and S4 task outputs only from the unique
// accepted Main planning_task tuple, after validating that tuple against the
// original journal. It never guesses sources from arbitrary files on disk.
func S5FormalInputs(root string, state map[string]any, files fileview.Reader) (S5FormalInputSet, error) {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	if lifecycle["state"] != "document_verification" || !PacketEnabled(state) {
		return S5FormalInputSet{}, fmt.Errorf("S5 formal inputs require the current packet-enabled document_verification cursor")
	}
	baseline, _ := state["baseline"].(map[string]any)
	generation, err := integerField(baseline, "generation")
	if err != nil {
		return S5FormalInputSet{}, err
	}
	typedS4 := hasTaskPlanningPacket(state)
	refs, err := currentS5RegisteredReferences(state, generation, typedS4)
	if err != nil {
		return S5FormalInputSet{}, err
	}
	if !typedS4 {
		return S5FormalInputSet{References: refs}, nil
	}
	if files == nil {
		return S5FormalInputSet{}, fmt.Errorf("S5 accepted S4 closure requires the original authoritative file view")
	}
	packet, answer, subjects, err := acceptedCurrentS4(root, state, files)
	if err != nil {
		return S5FormalInputSet{}, err
	}
	outputs, err := acceptedTaskPlanningArtifacts(root, state, packet, answer, files)
	if err != nil {
		return S5FormalInputSet{}, fmt.Errorf("revalidate accepted S4 task batch: %w", err)
	}
	// S4 proves the historical method identity; new S5 work freezes the method
	// bytes it actually receives. Existing S5 packets keep their pinned inputs.
	methods := map[string]workpacket.Subject{}
	for path, original := range taskPlanningIssuedMethods(packet) {
		data, err := readTaskPlanningFrozenFile(root, files, path)
		if err != nil {
			return S5FormalInputSet{}, fmt.Errorf("read current S5 planning method: %w", err)
		}
		original.SHA256 = workpacket.Digest(data)
		methods[path] = original
	}
	if verified, ok := files.(interface{ Verify() error }); ok {
		if err := verified.Verify(); err != nil {
			return S5FormalInputSet{}, fmt.Errorf("S5 planning method source moved: %w", err)
		}
	}
	return mergeS5FormalInputs(state, generation, refs, packet, subjects, outputs, methods)
}

func hasTaskPlanningPacket(state map[string]any) bool {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["work_packets"].([]any)
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row != nil && row["template_id"] == "s4-task-planning-v1" {
			return true
		}
	}
	return false
}

func currentS5RegisteredReferences(state map[string]any, generation int, typedS4 bool) ([]workpacket.Reference, error) {
	documents, err := currentRegisteredFormalInputDocuments(state, generation)
	if err != nil {
		return nil, err
	}
	refs := []workpacket.Reference{}
	seenPath := map[string]workpacket.Reference{}
	for _, document := range documents {
		kind := document.Kind
		if typedS4 && (kind == "task" || kind == "dispatch_plan") {
			continue
		}
		ref := document.Reference
		if ref.ID == "" || ref.Path == "" || ref.Version == "" || ref.SHA256 == "" {
			return nil, fmt.Errorf("current S5 registered document has an incomplete identity")
		}
		if old, exists := seenPath[ref.Path]; exists {
			if old != ref {
				return nil, fmt.Errorf("current S5 document path has conflicting registrations: %s", ref.Path)
			}
			return nil, fmt.Errorf("current S5 document path is registered more than once: %s", ref.Path)
		}
		seenPath[ref.Path] = ref
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Path < refs[j].Path })
	return refs, nil
}

func acceptedCurrentS4(root string, state map[string]any, files fileview.Reader) (workpacket.TaskPlanningPacket, workpacket.TaskPlanningAnswer, map[string]workpacket.Subject, error) {
	journal, err := ReadJournalForState(root, state)
	if err != nil {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("read S4 receipt journal: %w", err)
	}
	if _, err := validateTaskPlanningCheckpointIndices(root, state, journal); err != nil {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("validate original S4 receipt checkpoint: %w", err)
	}

	baseline, _ := state["baseline"].(map[string]any)
	generation, err := integerField(baseline, "generation")
	if err != nil {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, err
	}
	bound, _ := state["bound_req"].(map[string]any)
	if bound == nil || bound["status"] != "locked" {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("S5 requires the current locked REQ binding")
	}
	req := workpacket.Reference{ID: stringValue(bound["id"]), Path: stringValue(bound["path"]), Version: stringValue(bound["version"]), SHA256: stringValue(bound["sha256"])}
	entities, _ := state["entities"].(map[string]any)
	packetRows, _ := entities["work_packets"].([]any)
	latestStageEntry := -1
	type candidate struct {
		packet workpacket.TaskPlanningPacket
		record SubmissionRecord
	}
	candidates := []candidate{}
	for _, raw := range packetRows {
		row, _ := raw.(map[string]any)
		if row == nil || row["template_id"] != "s4-task-planning-v1" {
			continue
		}
		id := stringValue(row["packet_id"])
		p, found, err := FindTaskPlanningPacket(state, id)
		if err != nil {
			return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("decode original S4 issuance: %w", err)
		}
		if !found {
			return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("original S4 issuance is unavailable")
		}
		if p.Binding.RuntimeID != stringValue(state["runtime_id"]) || p.Binding.Generation != generation || p.Binding.Requirement != req || p.Binding.Stage != "S4" || p.Binding.Owner != "Main" {
			continue
		}
		if p.Binding.StageEntry > latestStageEntry {
			latestStageEntry = p.Binding.StageEntry
		}
		record, found, err := FindSubmission(state, p.ID)
		if err != nil {
			return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("decode original S4 submission: %w", err)
		}
		if !found || record.ControlStatus != "advanced" || record.OriginState != "planning" || record.OriginPhase != "tasks" || record.OriginEntry != p.Binding.StageEntry || record.TransitionEventID == "" || record.RevisionPacketID != "" || record.SupersededBy != "" {
			continue
		}
		candidates = append(candidates, candidate{packet: p, record: record})
	}
	if len(candidates) == 0 {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("no accepted current-generation S4 tuple for the locked REQ")
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].packet.Binding.StageEntry > candidates[j].packet.Binding.StageEntry
	})
	if len(candidates) > 1 && candidates[0].packet.Binding.StageEntry == candidates[1].packet.Binding.StageEntry {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("accepted S4 tuple for the locked REQ is ambiguous")
	}
	selected := candidates[0]
	if selected.packet.Binding.StageEntry != latestStageEntry {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("latest current-generation S4 packet is not the accepted S4 tuple")
	}
	if countSubmissionRows(state, selected.packet.ID) != 1 {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("accepted S4 submission tuple is duplicated")
	}
	evidence, found, err := currentEvidenceByID(state, selected.record.EvidenceID)
	if err != nil || !found {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("accepted S4 evidence is unavailable")
	}
	if evidence["status"] != "valid" || evidence["kind"] != "planning_task" || evidence["responsibility_id"] != "Orchestrator" || stringValue(evidence["path"]) == "" {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("accepted S4 evidence registration differs from the original Main planning_task")
	}
	gen, err := integerField(evidence, "baseline_generation")
	producers, _ := evidence["produced_by"].([]any)
	invalidationRule, rulePresent := evidence["invalidation_rule"]
	invalidationReason, reasonPresent := evidence["invalidation_reason"]
	reviewRound, roundPresent := evidence["review_round"]
	if err != nil || gen != generation || len(producers) != 1 || producers[0] != "Main" || evidence["invalidated_by"] != nil || !rulePresent || invalidationRule != nil || !reasonPresent || invalidationReason != nil || !roundPresent || reviewRound != nil {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("accepted S4 evidence is invalidated or has a different producer/generation")
	}
	path := stringValue(evidence["path"])
	data, err := files.ReadFile(path)
	if err != nil {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("read accepted S4 envelope %s: %w", path, err)
	}
	if workpacket.Digest(data) != stringValue(evidence["sha256"]) {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("accepted S4 envelope differs from its registered hash")
	}
	disk, err := workpacket.ReadFile(root, path, workpacket.TaskPlanningEvidenceReadLimit(selected.packet))
	if err != nil {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("read accepted S4 envelope checkout %s: %w", path, err)
	}
	if workpacket.Digest(disk) != stringValue(evidence["sha256"]) {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("accepted S4 envelope differs from the checkout")
	}
	var envelope struct {
		SubjectRefs []workpacket.Subject          `json:"subject_refs"`
		Answer      workpacket.TaskPlanningAnswer `json:"task_planning_answer"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("decode accepted S4 envelope: %w", err)
	}
	if err := ValidateTaskPlanningEnvelope(selected.packet, selected.record.EvidenceID, envelope.Answer, data); err != nil {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("validate accepted S4 envelope: %w", err)
	}
	digest, err := workpacket.TaskPlanningAnswerDigest(envelope.Answer)
	if err != nil || digest != selected.record.ContentHash {
		return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("accepted S4 professional answer differs from the submission tuple")
	}
	subjects := map[string]workpacket.Subject{}
	for _, subject := range envelope.SubjectRefs {
		if subject.Path == "" {
			return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("accepted S4 envelope contains an empty subject path")
		}
		if _, duplicate := subjects[subject.Path]; duplicate {
			return workpacket.TaskPlanningPacket{}, workpacket.TaskPlanningAnswer{}, nil, fmt.Errorf("accepted S4 envelope repeats subject %s", subject.Path)
		}
		subjects[subject.Path] = subject
	}
	return selected.packet, envelope.Answer, subjects, nil
}

func mergeS5FormalInputs(state map[string]any, generation int, refs []workpacket.Reference, packet workpacket.TaskPlanningPacket, subjects map[string]workpacket.Subject, outputs []taskPlanningArtifact, methods map[string]workpacket.Subject) (S5FormalInputSet, error) {
	byPath := map[string]workpacket.Reference{}
	for _, ref := range refs {
		byPath[ref.Path] = ref
	}
	facts := []S5FormalFact{}
	for _, source := range packet.Inputs.Sources {
		subject, ok := subjects[source.Path]
		if !ok || subject.SHA256 != source.SHA256 {
			return S5FormalInputSet{}, fmt.Errorf("accepted S4 envelope does not bind frozen source %s", source.Path)
		}
		if method, current := methods[source.Path]; current {
			source = method
			subject = method
		}
		if existing, exists := byPath[source.Path]; exists {
			if existing.SHA256 != source.SHA256 || (source.Version != "" && existing.Version != source.Version) {
				return S5FormalInputSet{}, fmt.Errorf("registered S5 document conflicts with accepted S4 source %s", source.Path)
			}
			continue
		}
		version := subject.Version
		if version == "" {
			version = "unversioned"
		}
		ref := workpacket.Reference{ID: "s4-source:" + source.Path, Path: source.Path, Version: version, SHA256: source.SHA256}
		byPath[ref.Path] = ref
		facts = append(facts, S5FormalFact{Reference: ref, Kind: "source", Status: "frozen"})
	}

	bound, _ := state["bound_req"].(map[string]any)
	reqID := stringValue(bound["id"])
	accepted := map[string]workpacket.Reference{}
	for _, output := range outputs {
		ref := output.ref
		subject, ok := subjects[ref.Path]
		if !ok || subject.SHA256 != ref.SHA256 {
			return S5FormalInputSet{}, fmt.Errorf("accepted S4 envelope does not bind validated output %s", ref.Path)
		}
		accepted[ref.Path] = ref
	}
	index := "docs/dev/tasks/index-" + reqID + ".md"
	for _, raw := range outputs {
		ref := raw.ref
		if ref.Path == index {
			id := "dispatch-plan:" + reqID
			row, err := exactS5OutputDocument(state, generation, id, "dispatch_plan", ref.Path, ref.SHA256, "complete")
			if err != nil {
				return S5FormalInputSet{}, err
			}
			registered := workpacket.Reference{ID: id, Path: ref.Path, Version: stringValue(row["version"]), SHA256: ref.SHA256}
			if registered.Version == "" || registered.Version != ref.Version {
				return S5FormalInputSet{}, fmt.Errorf("accepted dispatch plan revision differs from its registration")
			}
			if err := mergeS5Reference(byPath, registered); err != nil {
				return S5FormalInputSet{}, err
			}
			facts = append(facts, S5FormalFact{Reference: registered, Kind: "dispatch_plan", Status: "complete"})
			continue
		}
		if raw.status != "complete" {
			if _, exists := findS5OutputDocument(state, generation, ref.Path, "task"); exists {
				return S5FormalInputSet{}, fmt.Errorf("cancelled S4 TASK unexpectedly has an S5 document registration: %s", ref.Path)
			}
			continue
		}
		id := ref.ID
		row, err := exactS5OutputDocument(state, generation, id, "task", ref.Path, ref.SHA256, "complete")
		if err != nil {
			return S5FormalInputSet{}, err
		}
		registered := workpacket.Reference{ID: id, Path: ref.Path, Version: stringValue(row["version"]), SHA256: ref.SHA256}
		if registered.Version != ref.Version {
			return S5FormalInputSet{}, fmt.Errorf("accepted S4 TASK version differs from its registration: %s", ref.Path)
		}
		if err := mergeS5Reference(byPath, registered); err != nil {
			return S5FormalInputSet{}, err
		}
		facts = append(facts, S5FormalFact{Reference: registered, Kind: "task", Status: "complete"})
	}
	for _, raw := range stateDocuments(state) {
		if raw["kind"] != "task" && raw["kind"] != "dispatch_plan" {
			continue
		}
		g, err := integerField(raw, "generation")
		if err != nil {
			return S5FormalInputSet{}, err
		}
		if g == generation {
			if _, ok := accepted[stringValue(raw["path"])]; !ok {
				return S5FormalInputSet{}, fmt.Errorf("current S5 TASK or dispatch plan is outside the accepted S4 output set: %s", stringValue(raw["path"]))
			}
		}
	}
	refs = make([]workpacket.Reference, 0, len(byPath))
	for _, ref := range byPath {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].Path < refs[j].Path })
	sort.Slice(facts, func(i, j int) bool { return facts[i].Reference.Path < facts[j].Reference.Path })
	return S5FormalInputSet{References: refs, Facts: facts}, nil
}

func stateDocuments(state map[string]any) []map[string]any {
	raw, _ := state["documents"].([]any)
	rows := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		row, _ := item.(map[string]any)
		if row != nil {
			rows = append(rows, row)
		}
	}
	return rows
}

func exactS5OutputDocument(state map[string]any, generation int, id, kind, path, sha, status string) (map[string]any, error) {
	var found map[string]any
	for _, row := range stateDocuments(state) {
		g, err := integerField(row, "generation")
		if err != nil {
			return nil, err
		}
		if g != generation || (row["id"] != id && row["path"] != path) {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("S4 output has duplicate Runtime document registrations: %s", path)
		}
		if row["id"] != id || row["kind"] != kind || row["path"] != path || row["sha256"] != sha || row["status"] != status {
			return nil, fmt.Errorf("S4 output differs from its Runtime document registration: %s", path)
		}
		found = row
	}
	if found == nil {
		return nil, fmt.Errorf("accepted S4 output is absent from Runtime documents: %s", path)
	}
	return found, nil
}

func findS5OutputDocument(state map[string]any, generation int, path, kind string) (map[string]any, bool) {
	for _, row := range stateDocuments(state) {
		g, _ := integerField(row, "generation")
		if g == generation && row["path"] == path && row["kind"] == kind {
			return row, true
		}
	}
	return nil, false
}

func mergeS5Reference(byPath map[string]workpacket.Reference, ref workpacket.Reference) error {
	if old, exists := byPath[ref.Path]; exists {
		if old != ref {
			return fmt.Errorf("S5 frozen input path has conflicting identities: %s", ref.Path)
		}
		return fmt.Errorf("S5 frozen input path was selected more than once: %s", ref.Path)
	}
	byPath[ref.Path] = ref
	return nil
}

func acceptedTaskPlanningArtifacts(root string, state map[string]any, p workpacket.TaskPlanningPacket, answer workpacket.TaskPlanningAnswer, files fileview.Reader) ([]taskPlanningArtifact, error) {
	if files == nil {
		return nil, fmt.Errorf("S4 output validation requires an authoritative file view")
	}
	if p.Binding.RuntimeID != stringValue(state["runtime_id"]) || p.Binding.Generation != baselineGeneration(state) {
		return nil, fmt.Errorf("S4 packet does not match the current Runtime generation")
	}
	bound, _ := state["bound_req"].(map[string]any)
	if p.Binding.Requirement.ID != stringValue(bound["id"]) || p.Binding.Requirement.Path != stringValue(bound["path"]) || p.Binding.Requirement.Version != stringValue(bound["version"]) || p.Binding.Requirement.SHA256 != stringValue(bound["sha256"]) {
		return nil, fmt.Errorf("S4 packet does not match the current locked REQ")
	}
	expected, err := workpacket.BuildTaskPlanningWithTemplate(p.Binding, p.Inputs, p.TemplateSHA)
	if err != nil || !reflect.DeepEqual(expected, p) {
		return nil, fmt.Errorf("accepted S4 packet differs from its registered template projection")
	}
	projection := make(map[string]any, len(state))
	for key, value := range state {
		projection[key] = value
	}
	life := map[string]any{"state": "planning", "phase": "tasks", "phase_revision": p.Binding.StageEntry}
	projection["lifecycle"] = life
	inputs, err := resolveTaskPlanningInputs(root, projection, files, taskPlanningIssuedMethods(p))
	if err != nil {
		return nil, fmt.Errorf("reconstruct accepted S4 inputs from the current accepted S3 registration: %w", err)
	}
	// The issued questionnaire may retain the old contracts-directory metadata.
	// Actual contract membership remains proven by the accepted S3 refs above.
	if names, ok := p.Inputs.Directories[taskContractsDirectory]; ok {
		inputs.Directories[taskContractsDirectory] = append([]string(nil), names...)
	}
	rebuilt, err := workpacket.BuildTaskPlanningWithTemplate(p.Binding, inputs, p.TemplateSHA)
	if err != nil || !reflect.DeepEqual(rebuilt, p) {
		return nil, fmt.Errorf("accepted S4 packet differs from the reconstructed S3 source closure")
	}
	answer, err = validateTaskPlanningAnswerProjection(p, answer)
	if err != nil {
		return nil, err
	}
	entries, err := files.ReadDir(filepath.Join(root, taskPlanningDirectory))
	if err != nil {
		return nil, fmt.Errorf("read S4 task directory through original source view: %w", err)
	}
	if err := verifyTaskPlanningDirectory(root, files, taskPlanningDirectory, entries); err != nil {
		return nil, err
	}
	actualTasks, err := readCurrentTaskBatch(root, state, p, files, entries)
	if err != nil {
		return nil, err
	}
	// The original batch enumerator supplies complete and cancelled artifacts.
	// TaskPaths names the author's work; it is not a second machine manifest.
	taskByPath := map[string]taskPlanningArtifact{}
	complete := 0
	for _, task := range actualTasks {
		taskByPath[task.ref.Path] = task
		if task.status == "complete" {
			complete++
		}
	}
	if complete == 0 {
		return nil, fmt.Errorf("S4 batch has no complete TASK")
	}
	for _, path := range answer.TaskPaths {
		if _, ok := taskByPath[path]; !ok {
			return nil, fmt.Errorf("S4 answer task set omits or invents %s", path)
		}
	}
	indexPath := *answer.IndexPath
	indexData, err := readTaskPlanningFrozenFile(root, files, indexPath)
	if err != nil {
		return nil, err
	}
	if err := validateTaskDispatchIndex(root, files, p, indexPath, string(indexData), actualTasks); err != nil {
		return nil, err
	}
	indexRef, err := taskPlanningArtifactRef(indexPath, indexData)
	if err != nil {
		return nil, err
	}
	indexRef.Version = contractHeaderField(string(indexData), "Revision")
	if indexRef.Version == "" {
		return nil, fmt.Errorf("S4 dispatch plan has no frozen revision")
	}
	if verified, ok := files.(interface{ Verify() error }); ok {
		if err := verified.Verify(); err != nil {
			return nil, err
		}
	}
	return append([]taskPlanningArtifact{{ref: indexRef, status: "complete"}}, actualTasks...), nil
}

func countSubmissionRows(state map[string]any, packetID string) int {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["submissions"].([]any)
	count := 0
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row != nil && row["packet_id"] == packetID {
			count++
		}
	}
	return count
}

func currentEvidenceByID(state map[string]any, id string) (map[string]any, bool, error) {
	rows, _ := state["evidence"].([]any)
	var found map[string]any
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row == nil || row["id"] != id {
			continue
		}
		if found != nil {
			return nil, false, fmt.Errorf("duplicate evidence id %s", id)
		}
		found = row
	}
	return found, found != nil, nil
}
