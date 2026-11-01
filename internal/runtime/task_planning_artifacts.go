package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/entroforge/go-system-builder/internal/docscope"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/sharedmodel"
	"github.com/entroforge/go-system-builder/internal/workpacket"
)

const (
	taskPlanningDirectory  = "docs/dev/tasks"
	taskContractsDirectory = "docs/dev/contracts"
)

var (
	taskPlanningIDRE       = regexp.MustCompile(`^TASK-[A-Z0-9]+(?:-[A-Z0-9]+)*$`)
	taskPlanningClauseRE   = regexp.MustCompile(`\b((?:FE|BE|SYNC)-[A-Z0-9]+(?:-[A-Z0-9]+)*)\s*§\s*([0-9]+)`)
	taskPlanningREQRE      = regexp.MustCompile(`\bREQ-[A-Za-z0-9]+(?:-[A-Za-z0-9]+)*\b`)
	taskPlanningWaveRE     = regexp.MustCompile(`^## W([1-9][0-9]*)\b`)
	taskPlanningPlanItemRE = regexp.MustCompile(`^- \[([ xX])\] \[([^\]]+)\]\(([^)]+)\)\s*$`)
	taskPlanningFenceRE    = regexp.MustCompile("(?ms)^```.*?^```[^\\n]*$")
)

// CurrentTaskPlanningBinding identifies the real Main responsibility at the
// original planning.tasks cursor. It does not manufacture a new lifecycle
// state or a dispatched worker assignment.
func CurrentTaskPlanningBinding(state map[string]any) (workpacket.PlanningBinding, error) {
	if !PacketEnabled(state) {
		return workpacket.PlanningBinding{}, fmt.Errorf("explicit packet mode is required")
	}
	life, _ := state["lifecycle"].(map[string]any)
	if life["state"] != "planning" || life["phase"] != "tasks" {
		return workpacket.PlanningBinding{}, fmt.Errorf("S4 requires the original planning.tasks cursor")
	}
	bound, _ := state["bound_req"].(map[string]any)
	if bound == nil || bound["status"] != "locked" {
		return workpacket.PlanningBinding{}, fmt.Errorf("S4 requires the authorized locked REQ")
	}
	baseline, _ := state["baseline"].(map[string]any)
	generation, err := integerField(baseline, "generation")
	if err != nil {
		return workpacket.PlanningBinding{}, err
	}
	entry, err := integerField(life, "phase_revision")
	if err != nil {
		return workpacket.PlanningBinding{}, err
	}
	binding := workpacket.PlanningBinding{
		RuntimeID: stringValue(state["runtime_id"]), Generation: generation,
		StageEntry: entry, Stage: "S4", Owner: "Main",
		Requirement: workpacket.Reference{ID: stringValue(bound["id"]), Path: stringValue(bound["path"]), Version: stringValue(bound["version"])},
	}
	if binding.RuntimeID == "" || binding.Requirement.ID == "" || binding.Requirement.Version == "" || binding.Requirement.Path == "" {
		return workpacket.PlanningBinding{}, fmt.Errorf("S4 locked REQ/runtime binding is incomplete")
	}
	return binding, nil
}

// ResolveTaskPlanningInputs derives current contracts from documents[] and
// source closure from the accepted S3 packet/evidence. It never infers those
// historical facts from unregistered files on disk.
func ResolveTaskPlanningInputs(root string, state map[string]any, files fileview.Reader) (workpacket.TaskPlanningInputs, error) {
	return resolveTaskPlanningInputs(root, state, files, nil)
}

func resolveTaskPlanningInputs(root string, state map[string]any, files fileview.Reader, issuedMethods map[string]workpacket.Subject) (workpacket.TaskPlanningInputs, error) {
	if files == nil {
		return workpacket.TaskPlanningInputs{}, fmt.Errorf("S4 requires the original authoritative file view")
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return workpacket.TaskPlanningInputs{}, err
	}
	if err := fileview.ValidateAuthority(absoluteRoot, state); err != nil {
		return workpacket.TaskPlanningInputs{}, err
	}
	binding, err := CurrentTaskPlanningBinding(state)
	if err != nil {
		return workpacket.TaskPlanningInputs{}, err
	}
	contracts, err := currentTaskPlanningContracts(absoluteRoot, state, binding, files)
	if err != nil {
		return workpacket.TaskPlanningInputs{}, err
	}
	s3, answer, s3Sources, err := acceptedS3ForTaskPlanning(absoluteRoot, state, binding, contracts)
	if err != nil {
		return workpacket.TaskPlanningInputs{}, err
	}
	inputs := workpacket.TaskPlanningInputs{
		Contracts: contracts, Sources: []workpacket.Subject{}, Directories: map[string][]string{}, ClauseUniverse: []workpacket.TaskClause{},
	}
	inputs.Sources = append(inputs.Sources, s3Sources...)
	for dir, names := range s3.Inputs.Directories {
		if dir == taskContractsDirectory {
			continue
		}
		inputs.Directories[dir] = append([]string(nil), names...)
	}

	index := contractIndexReference(contracts)
	indexData, err := files.ReadFile(index.Path)
	if err != nil {
		return workpacket.TaskPlanningInputs{}, err
	}
	inputs.ClauseUniverse, err = taskClauseUniverse(string(indexData), contracts)
	if err != nil {
		return workpacket.TaskPlanningInputs{}, err
	}
	for _, path := range []string{"docs/dev/tasks/TASK-template.md", "docs/rules/dispatch-plan.md"} {
		if ref, issued := issuedMethods[path]; issued {
			inputs.Sources = append(inputs.Sources, ref)
			continue
		}
		_, err := readTaskPlanningFrozenFile(absoluteRoot, files, path)
		if err != nil {
			return workpacket.TaskPlanningInputs{}, err
		}
		inputs.Sources = append(inputs.Sources, workpacket.Subject{Path: path})
	}
	if err := validateTaskPlanningFrozenInputs(absoluteRoot, binding, taskPlanningFormalInputs(inputs, issuedMethods), files); err != nil {
		return workpacket.TaskPlanningInputs{}, err
	}
	if verified, ok := files.(interface{ Verify() error }); ok {
		if err := verified.Verify(); err != nil {
			return workpacket.TaskPlanningInputs{}, fmt.Errorf("S4 source view moved while resolving inputs: %w", err)
		}
	}
	// Ensure the answer came from the accepted historical S3 receipt, rather
	// than a newly scanned source set.
	if answer.Conclusion == nil || *answer.Conclusion != "pass" || answer.IndexPath == nil {
		return workpacket.TaskPlanningInputs{}, fmt.Errorf("S4 has no accepted S3 contract answer")
	}
	return inputs, nil
}

// ValidateTaskPlanningPacket reconstructs all locked inputs from original
// state and the upstream source view. Mutable output files are resolved only
// after this frozen binding passes.
func ValidateTaskPlanningPacket(root string, state map[string]any, p workpacket.TaskPlanningPacket, files fileview.Reader) error {
	return validateTaskPlanningPacket(root, state, p, files, false)
}

// ValidateTaskPlanningReceptionSources checks an issued responsibility's formal
// source closure without making historical method text a new reception gate.
// Packet/template identity and every REQ/design/contract source remain exact.
func ValidateTaskPlanningReceptionSources(root string, state map[string]any, p workpacket.TaskPlanningPacket, files fileview.Reader) error {
	return validateTaskPlanningPacket(root, state, p, files, true)
}

func validateTaskPlanningPacket(root string, state map[string]any, p workpacket.TaskPlanningPacket, files fileview.Reader, reception bool) error {
	binding, err := CurrentTaskPlanningBinding(state)
	if err != nil {
		return err
	}
	if !samePlanningResponsibility(binding, p.Binding) {
		return fmt.Errorf("S4 questionnaire does not belong to the current responsibility")
	}
	return nil
}

func TaskPlanningArtifacts(root string, state map[string]any, p workpacket.TaskPlanningPacket, answer workpacket.TaskPlanningAnswer, files fileview.Reader) ([]workpacket.Reference, error) {
	if files == nil {
		return nil, fmt.Errorf("S4 artifact reader unavailable")
	}
	if err := validateStoredTaskPlanningAnswer(answer); err != nil {
		return nil, err
	}
	entries, err := files.ReadDir(taskPlanningDirectory)
	if err != nil {
		return nil, err
	}
	tasks, err := readCurrentTaskBatch(root, state, p, files, entries)
	if err != nil {
		return nil, err
	}
	data, err := files.ReadFile(*answer.IndexPath)
	if err != nil {
		return nil, fmt.Errorf("read actual S4 index: %w", err)
	}
	index, err := taskPlanningArtifactRef(*answer.IndexPath, data)
	if err != nil {
		return nil, err
	}
	refs := make([]workpacket.Reference, 0, 1+len(tasks))
	refs = append(refs, index)
	for _, task := range tasks {
		refs = append(refs, task.ref)
	}
	return refs, nil
}

func reflectTaskPlanningPacketEqual(a, b workpacket.TaskPlanningPacket) bool {
	left, errA := json.Marshal(a)
	right, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(left) == string(right)
}

func currentTaskPlanningContracts(root string, state map[string]any, binding workpacket.PlanningBinding, files fileview.Reader) ([]workpacket.Reference, error) {
	rows, _ := state["documents"].([]any)
	contracts := []workpacket.Reference{}
	seenID, seenPath := map[string]bool{}, map[string]bool{}
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["kind"] != "contract" {
			continue
		}
		generation, err := integerField(row, "generation")
		if err != nil {
			return nil, fmt.Errorf("registered contract generation is invalid")
		}
		if generation != binding.Generation {
			continue
		}
		if row["status"] != "locked" {
			return nil, fmt.Errorf("S4 current contract registration is not locked")
		}
		ref := workpacket.Reference{ID: stringValue(row["id"]), Path: stringValue(row["path"]), Version: stringValue(row["version"])}
		if ref.ID == "" || ref.Path == "" || seenID[ref.ID] || seenPath[ref.Path] {
			return nil, fmt.Errorf("S4 current contract registration has duplicate or empty identity")
		}
		seenID[ref.ID], seenPath[ref.Path] = true, true
		contracts = append(contracts, ref)
	}
	if len(contracts) == 0 {
		return nil, fmt.Errorf("S4 has no current-generation locked contract registrations; return through original planning recovery")
	}
	sort.Slice(contracts, func(i, j int) bool { return contracts[i].Path < contracts[j].Path })
	// The accepted S3 responsibility carries its formal paths into S4. Do not
	// rescan a checkout or manufacture a second source-closure qualification.
	indexPath := ""
	for _, ref := range contracts {
		if strings.HasPrefix(ref.ID, "CONTRACTS-") {
			if indexPath != "" {
				return nil, fmt.Errorf("S4 has multiple current CONTRACTS index registrations")
			}
			indexPath = ref.Path
		}
	}
	if indexPath == "" {
		return nil, fmt.Errorf("S4 current registration omits the CONTRACTS index")
	}
	return contracts, nil
}

func acceptedS3ForTaskPlanning(root string, state map[string]any, binding workpacket.PlanningBinding, contracts []workpacket.Reference) (workpacket.ContractPacket, workpacket.ContractAnswer, []workpacket.Subject, error) {
	entities, _ := state["entities"].(map[string]any)
	packets, _ := entities["work_packets"].([]any)
	evidenceRows, _ := state["evidence"].([]any)
	// Choose the original responsibility before inspecting evidence or source
	// validity. Store/checkpoint validation owns historical structure and the
	// PTR-PLAN-02 journal route; this resolver consumes that original index.
	// S3 issuance requires its current contracts entry, and only its accepted
	// followup can enter tasks. Thus the last S3 before this S4 must have
	// completed that route; a missing/unadvanced association is a gap, not
	// permission to reuse an older receipt. Tasks-only rework adds no S3.
	packetID := ""
	latestEntry := -1
	seenPacketIDs := map[string]bool{}
	entryCounts := map[int]int{}
	for _, raw := range packets {
		row, _ := raw.(map[string]any)
		if row["template_id"] != "s3-contract-v1" {
			continue
		}
		id := stringValue(row["packet_id"])
		if id == "" || seenPacketIDs[id] {
			return workpacket.ContractPacket{}, workpacket.ContractAnswer{}, nil, fmt.Errorf("S4 original S3 packet identity is empty or duplicated")
		}
		seenPacketIDs[id] = true
		data, err := json.Marshal(row["binding"])
		if err != nil {
			return workpacket.ContractPacket{}, workpacket.ContractAnswer{}, nil, fmt.Errorf("S4 cannot decode original S3 source binding")
		}
		var origin workpacket.PlanningBinding
		if json.Unmarshal(data, &origin) != nil {
			return workpacket.ContractPacket{}, workpacket.ContractAnswer{}, nil, fmt.Errorf("S4 cannot decode original S3 source binding")
		}
		if origin.RuntimeID != binding.RuntimeID || origin.Generation != binding.Generation || (origin.Requirement.ID != binding.Requirement.ID || origin.Requirement.Path != binding.Requirement.Path || origin.Requirement.Version != binding.Requirement.Version) || origin.Stage != "S3" || origin.StageEntry >= binding.StageEntry {
			continue
		}
		entryCounts[origin.StageEntry]++
		if origin.StageEntry > latestEntry {
			packetID, latestEntry = id, origin.StageEntry
		}
	}
	// A surviving newer reception must not disappear merely because its packet
	// index is missing or no longer identifies this chain. Such a gap cannot
	// authorize falling back to an older, still-valid receipt.
	submissions, _ := entities["submissions"].([]any)
	for _, raw := range submissions {
		row, _ := raw.(map[string]any)
		entry, err := integerField(row, "origin_entry")
		if row["origin_state"] == "planning" && row["origin_phase"] == "contracts" && err == nil && entry > latestEntry && entry < binding.StageEntry {
			return workpacket.ContractPacket{}, workpacket.ContractAnswer{}, nil, fmt.Errorf("S4 original S3 reception at entry %d lacks its matching source packet", entry)
		}
	}
	if packetID == "" {
		return workpacket.ContractPacket{}, workpacket.ContractAnswer{}, nil, fmt.Errorf("S4 has no original S3 responsibility before this tasks entry; return through original planning recovery")
	}
	if entryCounts[latestEntry] != 1 {
		return workpacket.ContractPacket{}, workpacket.ContractAnswer{}, nil, fmt.Errorf("S4 current original S3 responsibility is duplicated at entry %d", latestEntry)
	}
	p, found, err := FindContractPacket(state, packetID)
	if err != nil || !found {
		return workpacket.ContractPacket{}, workpacket.ContractAnswer{}, nil, fmt.Errorf("S4 cannot decode current original S3 source packet %s", packetID)
	}
	record, found, err := FindSubmission(state, p.ID)
	if err != nil || !found || p.Binding.Owner != "Main" || record.ControlStatus != "advanced" || record.OriginState != "planning" || record.OriginPhase != "contracts" || record.OriginEntry != p.Binding.StageEntry || record.Owner != "Main" || record.PacketID != p.ID || record.TransitionEventID == "" || record.RevisionPacketID != "" || record.SupersededBy != "" {
		return workpacket.ContractPacket{}, workpacket.ContractAnswer{}, nil, fmt.Errorf("S4 current original S3 reception/transition association is incomplete at entry %d", latestEntry)
	}
	if countContractSubmissions(state, p.ID) != 1 {
		return workpacket.ContractPacket{}, workpacket.ContractAnswer{}, nil, fmt.Errorf("S4 original S3 reception identity is duplicated")
	}
	designs, err := currentTaskPlanningDesigns(state, binding)
	if err != nil {
		return workpacket.ContractPacket{}, workpacket.ContractAnswer{}, nil, err
	}
	if !sameReferenceSlice(designs, taskPlanningS2Designs(p.Inputs.Designs)) {
		return workpacket.ContractPacket{}, workpacket.ContractAnswer{}, nil, fmt.Errorf("S4 current original S3 design sources differ from current locked registrations")
	}
	var evidence map[string]any
	for _, raw := range evidenceRows {
		row, _ := raw.(map[string]any)
		if stringValue(row["id"]) == record.EvidenceID {
			if evidence != nil {
				return workpacket.ContractPacket{}, workpacket.ContractAnswer{}, nil, fmt.Errorf("S4 original S3 evidence identity is duplicated")
			}
			evidence = row
		}
	}
	generation, genErr := integerField(evidence, "baseline_generation")
	producers, _ := evidence["produced_by"].([]any)
	if evidence == nil || evidence["status"] != "valid" || evidence["kind"] != "planning_contract" || evidence["responsibility_id"] != "Orchestrator" || genErr != nil || generation != binding.Generation || len(producers) != 1 || producers[0] != "Main" || evidence["invalidated_by"] != nil || evidence["invalidation_rule"] != nil || evidence["invalidation_reason"] != nil {
		return workpacket.ContractPacket{}, workpacket.ContractAnswer{}, nil, fmt.Errorf("S4 original S3 accepted evidence is incomplete")
	}
	evidenceData, err := workpacket.ReadFile(root, stringValue(evidence["path"]), workpacket.ContractEvidenceReadLimit(p))
	if err != nil {
		return workpacket.ContractPacket{}, workpacket.ContractAnswer{}, nil, fmt.Errorf("read S4 original S3 evidence artifact: %w", err)
	}
	answer, err := ContractAnswerFromEvidence(evidenceData)
	if err != nil {
		return workpacket.ContractPacket{}, workpacket.ContractAnswer{}, nil, fmt.Errorf("S4 original S3 answer is unavailable: %w", err)
	}
	sources, matches := s3SourceSubjectsForTaskPlanning(p, answer, evidenceData, contracts)
	if !matches {
		return workpacket.ContractPacket{}, workpacket.ContractAnswer{}, nil, fmt.Errorf("S4 current original S3 contract/source closure differs from current locked registrations")
	}
	return p, answer, sources, nil
}

func s3SourceSubjectsForTaskPlanning(p workpacket.ContractPacket, answer workpacket.ContractAnswer, evidenceData []byte, contracts []workpacket.Reference) ([]workpacket.Subject, bool) {
	var envelope struct {
		SubjectRefs []workpacket.Subject `json:"subject_refs"`
	}
	if json.Unmarshal(evidenceData, &envelope) != nil || answer.IndexPath == nil {
		return nil, false
	}
	refs := map[string]workpacket.Subject{}
	for _, ref := range envelope.SubjectRefs {
		if _, exists := refs[ref.Path]; exists {
			return nil, false
		}
		refs[ref.Path] = ref
	}
	want := map[string]bool{*answer.IndexPath: true}
	for _, path := range answer.ContractPaths {
		want[path] = true
	}
	if len(want) != len(contracts) {
		return nil, false
	}
	for _, contract := range contracts {
		if !want[contract.Path] {
			return nil, false
		}
		ref, ok := refs[contract.Path]
		if !ok || ref.Version != contract.Version {
			return nil, false
		}
	}
	sources := make([]workpacket.Subject, 0, len(envelope.SubjectRefs))
	for _, ref := range envelope.SubjectRefs {
		if ref.Path == p.Binding.Requirement.Path || want[ref.Path] {
			continue
		}
		if !safeTaskPlanningPath(ref.Path) {
			return nil, false
		}
		sources = append(sources, ref)
	}
	return sources, true
}

func countContractSubmissions(state map[string]any, packetID string) int {
	entities, _ := state["entities"].(map[string]any)
	rows, _ := entities["submissions"].([]any)
	count := 0
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if stringValue(row["packet_id"]) == packetID {
			count++
		}
	}
	return count
}

func contractIndexReference(contracts []workpacket.Reference) workpacket.Reference {
	for _, ref := range contracts {
		if strings.HasPrefix(ref.ID, "CONTRACTS-") {
			return ref
		}
	}
	return workpacket.Reference{}
}

func taskClauseUniverse(indexContent string, contracts []workpacket.Reference) ([]workpacket.TaskClause, error) {
	contractIDs := map[string]bool{}
	for _, ref := range contracts {
		if !strings.HasPrefix(ref.ID, "CONTRACTS-") {
			contractIDs[ref.ID] = true
		}
	}
	seen := map[workpacket.TaskClause]bool{}
	clauses := []workpacket.TaskClause{}
	for _, match := range taskPlanningClauseRE.FindAllStringSubmatch(indexContent, -1) {
		section, err := strconv.Atoi(match[2])
		if err != nil || section < 1 {
			return nil, fmt.Errorf("S4 locked CONTRACTS index contains an invalid clause cell")
		}
		if !contractIDs[match[1]] {
			return nil, fmt.Errorf("S4 locked CONTRACTS index contains a clause for unregistered contract %s", match[1])
		}
		clause := workpacket.TaskClause{ContractID: match[1], Section: section}
		if !seen[clause] {
			seen[clause] = true
			clauses = append(clauses, clause)
		}
	}
	if len(clauses) == 0 {
		return nil, fmt.Errorf("S4 locked CONTRACTS index has no parseable clause universe")
	}
	for id := range contractIDs {
		found := false
		for _, clause := range clauses {
			if clause.ContractID == id {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("S4 locked CONTRACTS index has no clause cell for registered contract %s", id)
		}
	}
	sort.Slice(clauses, func(i, j int) bool {
		if clauses[i].ContractID != clauses[j].ContractID {
			return clauses[i].ContractID < clauses[j].ContractID
		}
		return clauses[i].Section < clauses[j].Section
	})
	return clauses, nil
}

func readTaskPlanningFrozenFile(root string, files fileview.Reader, path string) ([]byte, error) {
	if files == nil || !safeTaskPlanningPath(path) {
		return nil, fmt.Errorf("S4 frozen source path is invalid: %s", path)
	}
	view, err := files.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read S4 source %s through original view: %w", path, err)
	}
	if len(view) > 4<<20 {
		return nil, fmt.Errorf("S4 source %s exceeds 4 MiB", path)
	}
	return view, nil
}

// TaskPlanningOutputSourceError identifies an unreceived S4 output that is
// missing from or differs from its bound Git source. It carries the original
// answer path so callers can give a precise correction without reissuing.
type TaskPlanningOutputSourceError struct {
	Path       string
	Branch     string
	AnswerPath string
}

func (e *TaskPlanningOutputSourceError) Error() string {
	return fmt.Sprintf("S4 output %s is missing or differs on bound development branch %s; commit the professional output to that branch, then save the original answer %s", e.Path, e.Branch, e.AnswerPath)
}

func (e *TaskPlanningOutputSourceError) NextAction() string {
	return fmt.Sprintf("将专业产物 %s 提交到绑定开发分支 %s，然后保存原 S4 答案 %s。", e.Path, e.Branch, e.AnswerPath)
}

func readTaskPlanningOutputFile(root string, state map[string]any, p workpacket.TaskPlanningPacket, files fileview.Reader, path string, view []byte) ([]byte, error) {
	if files == nil || !safeTaskPlanningPath(path) {
		return nil, fmt.Errorf("S4 output path is invalid: %s", path)
	}
	if view == nil {
		var err error
		view, err = files.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				if sourceErr := taskPlanningOutputSourceError(root, state, p, files, path); sourceErr != nil {
					return nil, sourceErr
				}
			}
			return nil, fmt.Errorf("read S4 output %s through original source view: %w", path, err)
		}
	}
	if len(view) > 4<<20 {
		return nil, fmt.Errorf("S4 output %s exceeds 4 MiB", path)
	}
	return view, nil
}

func taskPlanningOutputSourceError(root string, state map[string]any, p workpacket.TaskPlanningPacket, files fileview.Reader, path string) error {
	source, ok := files.(interface{ Source(string) (string, error) })
	if !ok {
		return nil
	}
	current, err := CurrentTaskPlanningBinding(state)
	if err != nil || current != p.Binding {
		return nil
	}
	if _, received, err := FindSubmission(state, p.ID); err != nil || received {
		return nil
	}
	kind, err := source.Source(path)
	if err != nil || kind != "git_tree" {
		return nil
	}
	branch, err := fileview.DevelopmentRef(state)
	if err != nil {
		return nil
	}
	return &TaskPlanningOutputSourceError{
		Path: path, Branch: branch, AnswerPath: filepath.Join(root, filepath.FromSlash(p.AnswerPath)),
	}
}

func validateTaskPlanningFrozenInputs(root string, binding workpacket.PlanningBinding, inputs workpacket.TaskPlanningInputs, files fileview.Reader) error {
	// Original input locations are given to Main as professional material.
	// Receipt does not authenticate historical bytes or scan source history.
	return nil
}

func currentTaskPlanningDesigns(state map[string]any, binding workpacket.PlanningBinding) ([]workpacket.Reference, error) {
	rows, _ := state["documents"].([]any)
	designs := []workpacket.Reference{}
	seenID, seenPath := map[string]bool{}, map[string]bool{}
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["kind"] != "design" {
			continue
		}
		if strings.HasPrefix(stringValue(row["id"]), "shared-model:") {
			continue
		}
		generation, err := integerField(row, "generation")
		if err != nil {
			return nil, fmt.Errorf("registered design generation is invalid")
		}
		if generation != binding.Generation {
			continue
		}
		if row["status"] != "locked" {
			return nil, fmt.Errorf("S4 current design registration is not locked")
		}
		ref := workpacket.Reference{ID: stringValue(row["id"]), Path: stringValue(row["path"]), Version: stringValue(row["version"])}
		if ref.ID == "" || ref.Path == "" || seenID[ref.ID] || seenPath[ref.Path] {
			return nil, fmt.Errorf("S4 current design registration has duplicate or empty identity")
		}
		seenID[ref.ID], seenPath[ref.Path] = true, true
		designs = append(designs, ref)
	}
	if len(designs) == 0 {
		return nil, fmt.Errorf("S4 has no current-generation locked design registration")
	}
	sort.Slice(designs, func(i, j int) bool { return designs[i].Path < designs[j].Path })
	return designs, nil
}

func taskPlanningS2Designs(designs []workpacket.Reference) []workpacket.Reference {
	filtered := make([]workpacket.Reference, 0, len(designs))
	for _, design := range designs {
		if strings.HasPrefix(design.ID, "shared-model:") {
			continue
		}
		filtered = append(filtered, design)
	}
	return filtered
}

func sameReferenceSlice(a, b []workpacket.Reference) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Path != b[i].Path || a[i].Version != b[i].Version {
			return false
		}
	}
	return true
}

type taskPlanningArtifact struct {
	ref    workpacket.Reference
	status string
}

func readCurrentTaskBatch(root string, state map[string]any, p workpacket.TaskPlanningPacket, files fileview.Reader, entries []os.DirEntry) ([]taskPlanningArtifact, error) {
	bound, _ := state["bound_req"].(map[string]any)
	reqID := stringValue(bound["id"])
	artifacts := []taskPlanningArtifact{}
	seenID := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		id := strings.TrimSuffix(name, ".md")
		if entry.IsDir() || !strings.HasSuffix(name, ".md") || strings.Contains(strings.ToLower(name), "template") || strings.EqualFold(name, "README.md") || !strings.HasPrefix(id, "TASK-") {
			continue
		}
		path := taskPlanningDirectory + "/" + name
		data, err := files.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read S4 task %s through original source view: %w", path, err)
		}
		if len(data) > 4<<20 {
			return nil, fmt.Errorf("S4 task %s exceeds 4 MiB", path)
		}
		declaredREQs := taskPlanningREQRE.FindAllString(taskPlanningMarkdownField(string(data), "Source REQ refs", "REQ", "需求", "Requirement"), -1)
		if len(declaredREQs) > 0 && !containsTaskPlanningREQ(declaredREQs, reqID) {
			continue
		}
		if !docscope.Belongs(data, reqID) {
			continue
		}
		if !taskPlanningIDRE.MatchString(id) || seenID[id] {
			return nil, fmt.Errorf("S4 scoped TASK batch has invalid or duplicate task identity: %s", id)
		}
		seenID[id] = true
		status := contractHeaderField(string(data), "Status", "状态")
		if status != "complete" && status != "cancelled" {
			return nil, fmt.Errorf("S4 TASK %s status is %q; all batch members must be complete or explicitly cancelled", id, status)
		}
		version := contractHeaderField(string(data), "Version", "版本")
		if version == "" {
			version = "unversioned"
		}
		artifacts = append(artifacts, taskPlanningArtifact{ref: workpacket.Reference{ID: id, Path: path, Version: version}, status: status})
	}
	if len(artifacts) == 0 {
		return nil, fmt.Errorf("S4 has no current REQ TASK batch")
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].ref.Path < artifacts[j].ref.Path })
	return artifacts, nil
}

func validateTaskDispatchIndex(root string, files fileview.Reader, p workpacket.TaskPlanningPacket, path, content string, tasks []taskPlanningArtifact) error {
	if path != taskPlanningDirectory+"/index-"+p.Binding.Requirement.ID+".md" {
		return fmt.Errorf("S4 index path is not the exact bound REQ index")
	}
	if contractHeaderField(content, "REQ", "需求") != p.Binding.Requirement.ID || contractHeaderField(content, "Dispatch policy") != "waves-v1" || contractHeaderField(content, "Status") != "complete" {
		return fmt.Errorf("S4 index must bind the frozen REQ and declare waves-v1 Status complete")
	}
	if revision, err := strconv.Atoi(contractHeaderField(content, "Revision")); err != nil || revision < 1 {
		return fmt.Errorf("S4 index Revision must be a positive integer")
	}
	if source, ok := files.(interface{ Source(string) (string, error) }); ok {
		view, err := source.Source(path)
		if err != nil || view != "git_tree" {
			return fmt.Errorf("S4 dispatch index must use committed git_tree input")
		}
	}
	taskByPath := map[string]taskPlanningArtifact{}
	for _, task := range tasks {
		taskByPath[task.ref.Path] = task
	}
	content = taskPlanningFenceRE.ReplaceAllString(content, "")
	wave, lastWave := 0, 0
	seen := map[string]bool{}
	for _, rawLine := range strings.Split(content, "\n") {
		line := strings.TrimSpace(rawLine)
		if match := taskPlanningWaveRE.FindStringSubmatch(line); match != nil {
			wave, _ = strconv.Atoi(match[1])
			if wave != lastWave+1 {
				return fmt.Errorf("S4 dispatch waves must be contiguous and increasing from W1")
			}
			lastWave = wave
			continue
		}
		if strings.HasPrefix(line, "## ") {
			wave = 0
		}
		if !strings.HasPrefix(line, "- [") {
			continue
		}
		match := taskPlanningPlanItemRE.FindStringSubmatch(line)
		if match == nil || wave == 0 {
			return fmt.Errorf("S4 TASK checklist entries must be file links under a W<n> heading")
		}
		resolved, fragment, err := sharedmodel.Resolve(root, path, match[3])
		if err != nil || fragment != "" {
			return fmt.Errorf("S4 dispatch index has an invalid TASK link: %s", match[3])
		}
		resolved = filepath.ToSlash(resolved)
		task, ok := taskByPath[resolved]
		if !ok || task.status != "complete" {
			return fmt.Errorf("S4 dispatch index contains unknown, cross-REQ or cancelled TASK %s", match[3])
		}
		if match[1] != " " {
			return fmt.Errorf("S4 frozen plan checkboxes must remain unchecked: %s", task.ref.ID)
		}
		if seen[task.ref.ID] {
			return fmt.Errorf("S4 dispatch index repeats TASK %s", task.ref.ID)
		}
		seen[task.ref.ID] = true
	}
	if lastWave == 0 {
		return fmt.Errorf("S4 waves-v1 index has no W1 task wave")
	}
	for _, task := range tasks {
		if task.status == "complete" && !seen[task.ref.ID] {
			return fmt.Errorf("S4 dispatch index omits non-cancelled TASK %s", task.ref.ID)
		}
		if task.status == "cancelled" && seen[task.ref.ID] {
			return fmt.Errorf("S4 cancelled TASK %s remains in the dispatch index", task.ref.ID)
		}
	}
	return nil
}

func taskPlanningArtifactRef(path string, data []byte) (workpacket.Reference, error) {
	base := strings.TrimSuffix(filepath.Base(path), ".md")
	if !safeTaskPlanningPath(path) || !strings.HasSuffix(path, ".md") || base == "" || strings.Contains(strings.ToLower(base), "template") {
		return workpacket.Reference{}, fmt.Errorf("invalid S4 artifact path %s", path)
	}
	version := contractHeaderField(string(data), "Version", "版本")
	if version == "" {
		version = "unversioned"
	}
	return workpacket.Reference{ID: base, Path: path, Version: version}, nil
}

func validateTaskPlanningAnswerProjection(p workpacket.TaskPlanningPacket, answer workpacket.TaskPlanningAnswer) (workpacket.TaskPlanningAnswer, error) {
	draft := p.Draft
	draft.Answer = answer
	data, err := json.Marshal(draft)
	if err != nil {
		return workpacket.TaskPlanningAnswer{}, err
	}
	validated, issues := workpacket.ValidateTaskPlanningAnswer(p, data)
	if len(issues) != 0 {
		return workpacket.TaskPlanningAnswer{}, fmt.Errorf("S4 answer %s: %s", issues[0].Pointer, issues[0].Expected)
	}
	return validated, nil
}

func safeTaskPlanningPath(path string) bool {
	return path != "" && !filepath.IsAbs(path) && filepath.ToSlash(filepath.Clean(path)) == path && !strings.ContainsAny(path, "\\:#") && path != ".." && !strings.HasPrefix(path, "../")
}

func taskPlanningMarkdownField(content string, keys ...string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ">"))
		for _, key := range keys {
			if strings.HasPrefix(line, key+":") {
				return strings.TrimSpace(strings.TrimPrefix(line, key+":"))
			}
		}
	}
	return ""
}

func containsTaskPlanningREQ(values []string, reqID string) bool {
	for _, value := range values {
		if value == reqID {
			return true
		}
	}
	return false
}

func sameStringSlice(a, b []string) bool {
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

func taskPlanningMethodPath(path string) bool {
	return path == "docs/dev/tasks/TASK-template.md" || path == "docs/rules/dispatch-plan.md"
}

func taskPlanningFormalInputs(inputs workpacket.TaskPlanningInputs, methods map[string]workpacket.Subject) workpacket.TaskPlanningInputs {
	if methods == nil {
		return inputs
	}
	sources := make([]workpacket.Subject, 0, len(inputs.Sources))
	for _, ref := range inputs.Sources {
		if _, method := methods[ref.Path]; !method {
			sources = append(sources, ref)
		}
	}
	inputs.Sources = sources
	return inputs
}

func taskPlanningIssuedMethods(p workpacket.TaskPlanningPacket) map[string]workpacket.Subject {
	methods := map[string]workpacket.Subject{}
	for _, ref := range p.Inputs.Sources {
		if taskPlanningMethodPath(ref.Path) {
			methods[ref.Path] = ref
		}
	}
	return methods
}
