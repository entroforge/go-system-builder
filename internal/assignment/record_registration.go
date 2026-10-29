package assignment

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/entroforge/go-system-builder/internal/agentcontext"
	"github.com/entroforge/go-system-builder/internal/assignmentinputs"
	"github.com/entroforge/go-system-builder/internal/assignmentrecord"
	"github.com/entroforge/go-system-builder/internal/catalog"
	"github.com/entroforge/go-system-builder/internal/checkexec"
	"github.com/entroforge/go-system-builder/internal/dispatch"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/projectlayout"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/semantic"
	"github.com/entroforge/go-system-builder/internal/team"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

const recordRegistrationSourceLimit = 4 << 20

type recordRegistrationManifest struct {
	ManifestID         string                    `json:"manifest_id"`
	Version            string                    `json:"version"`
	REQID              string                    `json:"req_id"`
	RuntimeID          string                    `json:"runtime_id"`
	BaselineGeneration int                       `json:"baseline_generation"`
	WorkgroupID        string                    `json:"workgroup_id"`
	WorkgroupKind      string                    `json:"workgroup_kind"`
	Documents          []team.DocumentReference  `json:"documents"`
	Assignments        []recordRegistrationAgent `json:"assignments"`
}

type recordRegistrationAgent struct {
	AssignmentID       string                            `json:"assignment_id"`
	ResponsibilityID   string                            `json:"responsibility_id"`
	RoleFamily         string                            `json:"role_family"`
	AgentID            string                            `json:"agent_id"`
	AgentDefinitionRef string                            `json:"agent_definition_ref"`
	SkillRefs          []string                          `json:"skill_refs"`
	ReadPaths          []string                          `json:"read_paths"`
	Scope              []string                          `json:"scope"`
	WritePaths         []string                          `json:"write_paths"`
	OutputPaths        []string                          `json:"output_paths"`
	DependsOn          []string                          `json:"depends_on"`
	DoneWhen           []string                          `json:"done_when"`
	RequiredChecks     []string                          `json:"required_checks"`
	CheckProfiles      []checkexec.Profile               `json:"check_profiles"`
	ClosingContract    *assignmentrecord.ClosingContract `json:"closing_contract"`
	DispatchMode       string                            `json:"dispatch_mode"`
}

// buildRegisteredAssignmentRecords constructs v1.2 records for every
// assignment in one already validated Builder manifest. Registration calls it
// from its Writer Apply after the matching Team, Agent, and TASK owner rows
// have been added to the candidate state.
func buildRegisteredAssignmentRecords(root string, state map[string]any, manifestBytes []byte, manifestRef, taskRef, taskID string, taskBytes []byte) ([]Record, error) {
	if !workstate.SupportsAssignments(state["schema_version"]) {
		return nil, fmt.Errorf("Assignment Record registration requires Runtime schema 1.2.0 or 1.3.0")
	}
	lifecycle, _ := state["lifecycle"].(map[string]any)
	if lifecycle == nil || lifecycle["state"] != "building" {
		return nil, fmt.Errorf("Assignment Record registration requires the current building state")
	}
	enabled, err := ManifestAssignmentRecordEnabled(manifestBytes)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, fmt.Errorf("Assignment Record registration requires explicit %s manifest opt-in", AssignmentRecordContract)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("team-manifest.schema.json", manifestBytes); err != nil {
		return nil, fmt.Errorf("S6 assignment manifest schema: %w", err)
	}
	if err := team.ValidateBytes(manifestBytes); err != nil {
		return nil, fmt.Errorf("S6 assignment manifest semantics: %w", err)
	}
	var source recordRegistrationManifest
	if err := json.Unmarshal(manifestBytes, &source); err != nil {
		return nil, fmt.Errorf("decode S6 assignment manifest: %w", err)
	}
	var reqBinding struct {
		REQID string `json:"req_id"`
	}
	if err := json.Unmarshal(manifestBytes, &reqBinding); err != nil {
		return nil, err
	}
	bound, _ := state["bound_req"].(map[string]any)
	if bound == nil || bound["status"] != "locked" || reqBinding.REQID == "" || reqBinding.REQID != recordRegistrationString(bound["id"]) {
		return nil, fmt.Errorf("S6 manifest does not bind the current locked REQ")
	}
	if source.WorkgroupKind != "builder" || len(source.Assignments) == 0 {
		return nil, fmt.Errorf("S6 Assignment Records require a non-empty Builder manifest")
	}
	if err := validateRecordRegistrationRef(manifestRef); err != nil {
		return nil, fmt.Errorf("manifest ref: %w", err)
	}
	if err := validateRecordRegistrationRef(taskRef); err != nil {
		return nil, fmt.Errorf("TASK ref: %w", err)
	}

	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := fileview.ValidateAuthority(root, state); err != nil {
		return nil, err
	}
	files, err := dispatch.View(root, state)
	if err != nil {
		return nil, fmt.Errorf("open original S6 file view: %w", err)
	}
	actualManifest, err := readRecordRegistrationSource(root, files, manifestRef)
	if err != nil {
		return nil, fmt.Errorf("read registered manifest source: %w", err)
	}
	if !bytes.Equal(actualManifest, manifestBytes) {
		return nil, fmt.Errorf("S6 manifest bytes differ from the original file view and checkout")
	}
	actualTask, err := readRecordRegistrationSource(root, files, taskRef)
	if err != nil {
		return nil, fmt.Errorf("read registered TASK source: %w", err)
	}
	if !bytes.Equal(actualTask, taskBytes) {
		return nil, fmt.Errorf("S6 TASK bytes differ from the original file view and checkout")
	}
	if version := semantic.MarkdownField(string(actualTask), "Version"); version == "" {
		return nil, fmt.Errorf("S6 TASK source has no explicit Version field")
	} else {
		registeredVersion := recordRegistrationLockedTaskVersion(state, taskID)
		if registeredVersion == "" || version != registeredVersion {
			return nil, fmt.Errorf("S6 TASK source Version %q differs from current locked TASK version %q", version, registeredVersion)
		}
	}

	var taskDocument *team.DocumentReference
	for i := range source.Documents {
		doc := &source.Documents[i]
		if doc.ID != taskID {
			continue
		}
		if taskDocument != nil {
			return nil, fmt.Errorf("S6 manifest has multiple documents with current TASK id %s", taskID)
		}
		taskDocument = doc
	}
	if taskDocument == nil || taskDocument.Path != taskRef {
		return nil, fmt.Errorf("S6 request TASK differs from the manifest's formal TASK subject")
	}

	board, err := dispatch.LoadWithFiles(root, state, files, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("load original reviewed dispatch plan: %w", err)
	}
	if board.Legacy || board.Plan == nil || board.Plan.Path == "" || board.Plan.REQ != reqBinding.REQID || len(board.Plan.Problems) != 0 {
		return nil, fmt.Errorf("S6 v1.2 Assignment Records require the complete current reviewed dispatch plan")
	}
	var reviewedWrites []string
	rows := 0
	for _, row := range board.Plan.Tasks {
		if row.ID != taskID && row.Path != taskRef {
			continue
		}
		if row.ID != taskID || row.Path != taskRef {
			return nil, fmt.Errorf("reviewed TASK ID/path differs from manifest registration")
		}
		rows++
		reviewedWrites = append([]string(nil), row.Writes...)
	}
	if rows != 1 || len(reviewedWrites) == 0 {
		return nil, fmt.Errorf("current TASK must have exactly one reviewed row with concrete write paths")
	}

	if err := semantic.ValidateAgentDefinitions(root); err != nil {
		return nil, fmt.Errorf("installed Agent definitions: %w", err)
	}
	contextSources, err := recordRegistrationSources(root, files, source)
	if err != nil {
		return nil, err
	}
	records := make([]Record, 0, len(source.Assignments))
	seenAssignments := map[string]bool{}
	for _, assignment := range source.Assignments {
		if assignment.AssignmentID == "" || seenAssignments[assignment.AssignmentID] {
			return nil, fmt.Errorf("S6 manifest assignment identity is empty or duplicated")
		}
		seenAssignments[assignment.AssignmentID] = true
		assignmentSources, err := recordRegistrationAssignmentSources(root, files, assignment)
		if err != nil {
			return nil, fmt.Errorf("S6 assignment %s sources: %w", assignment.AssignmentID, err)
		}
		allSources := append(append([]ContextSource(nil), contextSources...), assignmentSources...)
		record, err := BuildRecord(manifestBytes, manifestRef, state, taskBytes, taskID, assignment.AssignmentID, reviewedWrites, allSources)
		if err != nil {
			return nil, fmt.Errorf("build S6 Assignment Record %s: %w", assignment.AssignmentID, err)
		}
		encoded, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		if err := schema.NewEmbeddedValidator().ValidateBytes("assignment-record.schema.json", encoded); err != nil {
			return nil, fmt.Errorf("S6 Assignment Record %s schema: %w", assignment.AssignmentID, err)
		}
		records = append(records, record)
	}
	if err := rejectRecordRegistrationProtectedScope(state, manifestRef, source.Documents, records); err != nil {
		return nil, err
	}
	finalManifest, err := readRecordRegistrationSource(root, files, manifestRef)
	if err != nil || !bytes.Equal(finalManifest, manifestBytes) {
		return nil, fmt.Errorf("S6 manifest changed while Assignment Records were being constructed")
	}
	finalTask, err := readRecordRegistrationSource(root, files, taskRef)
	if err != nil || !bytes.Equal(finalTask, taskBytes) {
		return nil, fmt.Errorf("S6 TASK changed while Assignment Records were being constructed")
	}
	if err := files.Verify(); err != nil {
		return nil, fmt.Errorf("S6 file view changed during Assignment Record construction: %w", err)
	}
	return records, nil
}

func validateRecordRegistrationRef(path string) error {
	if strings.TrimSpace(path) == "" || filepath.IsAbs(path) || strings.ContainsAny(path, "\\:\x00") {
		return fmt.Errorf("%q is not a safe repository-relative file path", path)
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || clean != path || strings.Contains(path, "#") {
		return fmt.Errorf("%q is not a canonical repository file path", path)
	}
	return nil
}

func readRecordRegistrationSource(root string, files fileview.Reader, path string) ([]byte, error) {
	if err := validateRecordRegistrationRef(path); err != nil {
		return nil, err
	}
	viewBytes, err := files.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read through formal file view %s: %w", path, err)
	}
	if len(viewBytes) > recordRegistrationSourceLimit {
		return nil, fmt.Errorf("source %s exceeds %d bytes", path, recordRegistrationSourceLimit)
	}
	diskBytes, err := workpacket.ReadFile(root, path, recordRegistrationSourceLimit)
	if err != nil {
		return nil, fmt.Errorf("read checkout source %s: %w", path, err)
	}
	if !bytes.Equal(viewBytes, diskBytes) {
		return nil, fmt.Errorf("formal file view and checkout differ for %s", path)
	}
	return viewBytes, nil
}

func recordRegistrationSources(root string, files fileview.Reader, source recordRegistrationManifest) ([]ContextSource, error) {
	refs := []struct {
		path string
		sha  string
	}{}
	for _, doc := range source.Documents {
		if doc.Path == "" || doc.SHA256 == "" {
			return nil, fmt.Errorf("S6 manifest document source is incomplete")
		}
		refs = append(refs, struct {
			path string
			sha  string
		}{doc.Path, doc.SHA256})
	}
	context := []ContextSource{}
	for _, ref := range refs {
		data, err := readRecordRegistrationSource(root, files, ref.path)
		if err != nil {
			return nil, fmt.Errorf("manifest document %s: %w", ref.path, err)
		}
		if sha256Of(data) != ref.sha {
			return nil, fmt.Errorf("manifest document hash differs from file view/checkout: %s", ref.path)
		}
	}

	for _, name := range []string{"team-manifest.schema.json", "check-profile.schema.json", "assignment-record.schema.json", "agent-message.schema.json"} {
		ref := assignmentinputs.SchemaPrefix + name
		data, err := assignmentinputs.Read(root, ref)
		if err != nil {
			return nil, err
		}
		context = append(context, ContextSource{Kind: "embedded_schema", Ref: ref, SHA256: sha256Of(data)})
	}
	profileBytes, err := assignmentinputs.Read(root, assignmentinputs.ContextProfileRef)
	if err != nil {
		return nil, fmt.Errorf("embedded Agent context profile source: %w", err)
	}
	context = append(context, ContextSource{Kind: "embedded_context_profile", Ref: assignmentinputs.ContextProfileRef, SHA256: sha256Of(profileBytes)})
	return context, nil
}

func recordRegistrationAssignmentSources(root string, files fileview.Reader, assignment recordRegistrationAgent) ([]ContextSource, error) {
	if err := assignmentrecord.ValidateClosingContract(assignment.ClosingContract); err != nil {
		return nil, fmt.Errorf("closing contract: %w", err)
	}
	roleBytes, rolePath, err := catalog.ReadAgentAsset(root, assignment.RoleFamily)
	if err != nil {
		return nil, err
	}
	if rolePath != assignment.AgentDefinitionRef {
		return nil, fmt.Errorf("manifest Agent definition ref %q differs from installed role source %q", assignment.AgentDefinitionRef, rolePath)
	}
	viewRole, err := readRecordRegistrationSource(root, files, rolePath)
	if err != nil {
		return nil, fmt.Errorf("Agent definition %s: %w", rolePath, err)
	}
	if !bytes.Equal(roleBytes, viewRole) {
		return nil, fmt.Errorf("installed Agent definition differs from original file view: %s", rolePath)
	}
	sources := []ContextSource{{Kind: "agent_definition", Ref: rolePath, ID: assignment.RoleFamily, SHA256: sha256Of(roleBytes)}}

	selection, err := agentcontext.Select(root, assignment.RoleFamily, []string{assignment.ResponsibilityID}, assignment.SkillRefs)
	if err != nil {
		return nil, fmt.Errorf("resolve registered Agent Skills: %w", err)
	}
	profileData, err := assignmentinputs.Read(root, assignmentinputs.ContextProfileRef)
	if err != nil {
		return nil, fmt.Errorf("embedded Agent context profile source: %w", err)
	}
	if sha256Of(profileData) != selection.ProfileSHA256 {
		return nil, fmt.Errorf("embedded and repository Agent context profiles differ")
	}
	skillNames := map[string]bool{}
	for _, name := range agentcontext.LegacySkills(assignment.RoleFamily) {
		skillNames[name] = true
	}
	for _, skill := range selection.Selected {
		skillNames[skill.Name] = true
	}
	selectedByName := map[string]agentcontext.Skill{}
	for _, skill := range selection.Selected {
		selectedByName[skill.Name] = skill
	}
	names := make([]string, 0, len(skillNames))
	for name := range skillNames {
		names = append(names, name)
	}
	sort.Strings(names)
	specs := map[string]catalog.SkillSpec{}
	for _, spec := range catalog.Skills {
		specs[spec.Name] = spec
	}
	for _, name := range names {
		data, path, err := catalog.ReadSkillAsset(root, name)
		if err != nil {
			return nil, err
		}
		if _, ok := specs[name]; !ok {
			return nil, fmt.Errorf("Skill %s is not in the installed Skill catalog", name)
		}
		if err := catalog.ValidateSkill(root, specs[name]); err != nil {
			return nil, fmt.Errorf("installed Skill %s: %w", name, err)
		}
		viewSkill, err := readRecordRegistrationSource(root, files, path)
		if err != nil {
			return nil, fmt.Errorf("Skill %s: %w", name, err)
		}
		if !bytes.Equal(data, viewSkill) {
			return nil, fmt.Errorf("installed Skill differs from original file view: %s", name)
		}
		if selected, ok := selectedByName[name]; ok && (selected.Source != path || selected.SHA256 != sha256Of(data)) {
			return nil, fmt.Errorf("Agent context Skill binding differs from installed source: %s", name)
		}
		sources = append(sources, ContextSource{Kind: "authorized_skill", Ref: path, ID: name, SHA256: sha256Of(data)})
	}

	for _, profile := range assignment.CheckProfiles {
		for _, dependency := range profile.Dependencies {
			if err := verifyRecordRegistrationProfileFile(root, files, dependency.Path, dependency.SHA256); err != nil {
				return nil, fmt.Errorf("CheckProfile %s dependency: %w", profile.ID, err)
			}
		}
		for _, fixture := range profile.Fixtures {
			if err := verifyRecordRegistrationProfileFile(root, files, fixture.Path, fixture.SHA256); err != nil {
				return nil, fmt.Errorf("CheckProfile %s fixture: %w", profile.ID, err)
			}
		}
	}
	if assignment.ClosingContract != nil {
		for _, check := range assignment.ClosingContract.WorkerChecks {
			for _, dependency := range check.Profile.Dependencies {
				if err := verifyRecordRegistrationProfileFile(root, files, dependency.Path, dependency.SHA256); err != nil {
					return nil, fmt.Errorf("Worker CheckProfile %s dependency: %w", check.Profile.ID, err)
				}
			}
			for _, fixture := range check.Profile.Fixtures {
				if err := verifyRecordRegistrationProfileFile(root, files, fixture.Path, fixture.SHA256); err != nil {
					return nil, fmt.Errorf("Worker CheckProfile %s fixture: %w", check.Profile.ID, err)
				}
			}
		}
	}
	return sources, nil
}

func verifyRecordRegistrationProfileFile(root string, files fileview.Reader, path, expectedSHA string) error {
	data, err := readRecordRegistrationSource(root, files, path)
	if err != nil {
		return err
	}
	if sha256Of(data) != expectedSHA {
		return fmt.Errorf("declared SHA differs from formal file view/checkout: %s", path)
	}
	return nil
}

func rejectRecordRegistrationProtectedScope(state map[string]any, manifestRef string, manifestDocs []team.DocumentReference, records []Record) error {
	protected := map[string]bool{
		".git":                        true,
		".claude/inputs":              true,
		".claude/bin":                 true,
		".claude/agents":              true,
		".claude/skills":              true,
		".claude/runtime-checkpoints": true,
		".claude/settings.json":       true,
		".claude/settings.local.json": true,
		".claude/loop-workspace.json": true,
		".claude/loop-bootstrap.json": true,
		".claude/loop-state.json":     true,
		".claude/loop-events.jsonl":   true,
		projectlayout.Requirements:    true,
		projectlayout.Contracts:       true,
		manifestRef:                   true,
	}
	for _, doc := range manifestDocs {
		protected[doc.Path] = true
	}
	if bound, _ := state["bound_req"].(map[string]any); bound != nil {
		if path := recordRegistrationString(bound["path"]); path != "" {
			protected[path] = true
		}
	}
	if rows, _ := state["documents"].([]any); rows != nil {
		for _, raw := range rows {
			row, _ := raw.(map[string]any)
			if row["status"] == "locked" {
				if path := recordRegistrationString(row["path"]); path != "" {
					protected[path] = true
				}
			}
		}
	}
	for _, record := range records {
		protected[record.Task.Ref] = true
		protected[record.Manifest.Ref] = true
		protected[record.AgentDefinition.Ref] = true
		for _, source := range record.ContextSources {
			if source.Kind == "authorized_skill" || source.Kind == "frozen_input" {
				protected[source.Ref] = true
			}
		}
	}
	protectedPaths := make([]string, 0, len(protected))
	for path := range protected {
		protectedPaths = append(protectedPaths, path)
	}
	sort.Strings(protectedPaths)
	for _, record := range records {
		for _, scope := range record.EffectiveWritePaths {
			for _, path := range protectedPaths {
				if assignmentScopesOverlap(scope, path) {
					return fmt.Errorf("Assignment %s effective scope %q overlaps protected original path %q", record.AssignmentID, scope, path)
				}
			}
		}
	}
	return nil
}

func recordRegistrationString(value any) string {
	text, _ := value.(string)
	return text
}

func recordRegistrationLockedTaskVersion(state map[string]any, taskID string) string {
	baseline, _ := state["baseline"].(map[string]any)
	generation := dispatch.Number(baseline["generation"])
	rows, _ := state["documents"].([]any)
	version := ""
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		if row["kind"] != "task" || row["id"] != taskID || row["status"] != "locked" || dispatch.Number(row["generation"]) != generation {
			continue
		}
		candidate := recordRegistrationString(row["version"])
		if candidate == "" || version != "" {
			return ""
		}
		version = candidate
	}
	return version
}

func assignmentScopesOverlap(left, right string) bool {
	left = strings.TrimSuffix(filepath.ToSlash(filepath.Clean(left)), "/")
	right = strings.TrimSuffix(filepath.ToSlash(filepath.Clean(right)), "/")
	return left == right || strings.HasPrefix(left, right+"/") || strings.HasPrefix(right, left+"/")
}
