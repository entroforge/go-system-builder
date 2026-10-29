package semantic

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/entroforge/go-system-builder/internal/pathscope"
	"github.com/entroforge/go-system-builder/internal/projectlayout"
	"os"
	"path/filepath"
	"strings"

	"github.com/entroforge/go-system-builder/internal/catalog"
	"github.com/entroforge/go-system-builder/internal/evidence"
	"github.com/entroforge/go-system-builder/internal/fileview"
	"github.com/entroforge/go-system-builder/internal/migration"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/scenario"
	"github.com/entroforge/go-system-builder/internal/schema"
	"github.com/entroforge/go-system-builder/internal/team"
	"github.com/entroforge/go-system-builder/internal/workpacket"
	"github.com/entroforge/go-system-builder/internal/workstate"
)

type stateDefinition struct {
	States map[string]struct {
		PhaseMachine *string `json:"phase_machine"`
	} `json:"states"`
	PhaseMachines map[string]struct {
		Phases map[string]any `json:"phases"`
	} `json:"phase_machines"`
	EntityLifecycles map[string]struct {
		States []string `json:"states"`
	} `json:"entity_lifecycles"`
}

type runtimeState struct {
	SchemaVersion string `json:"schema_version"`
	RuntimeID     string `json:"runtime_id"`
	Definition    struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"definition"`
	HookControl struct {
		PolicyRef struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
		} `json:"policy_ref"`
	} `json:"hook_control"`
	Lifecycle struct {
		State string  `json:"state"`
		Phase *string `json:"phase"`
	} `json:"lifecycle"`
	Entities struct {
		Agents []struct {
			State              string `json:"state"`
			LastLifecycleEvent string `json:"last_lifecycle_event"`
		} `json:"agents"`
		Tasks []struct {
			State              string `json:"state"`
			LastLifecycleEvent string `json:"last_lifecycle_event"`
		} `json:"tasks"`
		Bugs []struct {
			State string `json:"state"`
		} `json:"bugs"`
	} `json:"entities"`
	Review struct {
		Assignments map[string]struct {
			Status             string `json:"status"`
			LastLifecycleEvent string `json:"last_lifecycle_event"`
		} `json:"assignments"`
	} `json:"review"`
}

type reviewManifest struct {
	RuntimeID string `json:"runtime_id"`
	Documents []struct {
		ID     string `json:"id"`
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"documents"`
}

func ValidateRepository(root string) error {
	validator := schema.NewValidator(root)
	// Embedded single-object schema/example pairs.
	embeddedPairs := [][2]string{
		{"loop-state.schema.json", "loop-state.example.json"},
		{"loop-event.schema.json", "loop-event.example.json"},
		{"team-manifest.schema.json", "team-manifest.example.json"},
	}
	for _, pair := range embeddedPairs {
		if err := validator.ValidateEmbedded(pair[0], pair[1]); err != nil {
			return fmt.Errorf("%s: %w", pair[1], err)
		}
	}
	// Embedded array examples (each file is a JSON array of instances).
	for _, pair := range [][2]string{
		{"agent-message.schema.json", "agent-message.examples.json"},
		{"review-evidence.schema.json", "review-evidence.examples.json"},
		{"hook-decision.schema.json", "hook-decision.examples.json"},
	} {
		data, err := schema.ReadAsset(pair[1])
		if err != nil {
			return err
		}
		var items []json.RawMessage
		if err := json.Unmarshal(data, &items); err != nil {
			return fmt.Errorf("decode %s: %w", pair[1], err)
		}
		for index, item := range items {
			if err := validator.ValidateBytes(pair[0], item); err != nil {
				return fmt.Errorf("%s[%d]: %w", pair[1], index, err)
			}
		}
	}
	for _, name := range []string{
		"readback-request.template.json",
		"activation.template.json",
	} {
		if err := validator.ValidateEmbedded("agent-message.schema.json", name); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	// Repository validation checks existing authoring artifacts project-wide.
	// It cannot require an active S2 responsibility to have delivered S3's
	// future batch. Current-REQ completion stays with ContractsCheck and the
	// original S3 consumer; this diagnostic does not certify that delivery.
	contractResult, err := contractsCheck(root, fileview.Disk{Root: root}, workpacket.Reference{}, nil)
	if err != nil {
		return fmt.Errorf("contracts: %w", err)
	}
	if len(contractResult.Problems) > 0 {
		return fmt.Errorf("contracts: %d problem(s): %s", len(contractResult.Problems), strings.Join(contractResult.Problems, "; "))
	}
	// AutoSpecs: once a module's Playwright spec tree exists, every doctor
	// run enforces its browser-case coverage (specs are S6+ artifacts —
	// absent trees are expected earlier and are not failures).
	if _, err := scenario.ValidateAll(root, scenario.ValidateOptions{AutoSpecs: true}); err != nil {
		return fmt.Errorf("scenario packages: %w", err)
	}
	// Runtime authorities on disk (validated against embedded schemas).
	for _, pair := range [][2]string{
		{"loop-definition.schema.json", projectlayout.Definition},
		{"hook-policy.schema.json", projectlayout.Policy},
	} {
		if err := validator.ValidateFile(pair[0], pair[1]); err != nil {
			return fmt.Errorf("%s: %w", pair[1], err)
		}
	}
	if err := catalog.ValidateSkills(root); err != nil {
		return err
	}
	if err := catalog.ValidateAgents(root); err != nil {
		return err
	}
	exampleBytes, err := schema.ReadAsset("team-manifest.example.json")
	if err != nil {
		return err
	}
	if err := team.ValidateBytes(exampleBytes); err != nil {
		return fmt.Errorf("team manifest semantics: %w", err)
	}
	if err := ValidateReviewManifests(root); err != nil {
		return err
	}
	if err := ValidateReadbackTemplates(root); err != nil {
		return err
	}
	if err := ValidateAgentMessages(root); err != nil {
		return err
	}
	if err := migration.ValidateTemplates(root); err != nil {
		return fmt.Errorf("template migration: %w", err)
	}

	stateData, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		return err
	}
	if err := ValidateRuntimeBytes(root, stateData); err != nil {
		return err
	}
	if err := ValidateRuntimeFile(root, ".claude/loop-state.json"); err != nil {
		return err
	}
	if err := ValidateRuntimeReachability(root); err != nil {
		return err
	}
	if err := ValidateEvidenceCatalog(root); err != nil {
		return err
	}
	return nil
}

// ValidateEvidenceCatalog proves every Loop Definition evidence slot has a
// registered persisted kind or an explicit generator. Quality Gate closure is
// checked by qualitygate.ValidateEvidenceCatalog at the doctor boundary.
func ValidateEvidenceCatalog(root string) error {
	definitionData, err := os.ReadFile(filepath.Join(root, projectlayout.Definition))
	if err != nil {
		return fmt.Errorf("evidence catalog: read Loop Definition: %w", err)
	}
	var definition evidenceDefinition
	if err := json.Unmarshal(definitionData, &definition); err != nil {
		return fmt.Errorf("evidence catalog: decode Loop Definition: %w", err)
	}
	catalog := evidence.DefaultCatalog()
	for _, spec := range definition.Transitions {
		if err := catalog.ValidateSlots(spec.RequiredEvidence); err != nil {
			return fmt.Errorf("evidence catalog: transition %s: %w", spec.ID, err)
		}
	}
	for owner, machine := range definition.PhaseMachines {
		for _, spec := range machine.Transitions {
			if err := catalog.ValidateSlots(spec.RequiredEvidence); err != nil {
				return fmt.Errorf("evidence catalog: phase %s transition %s: %w", owner, spec.ID, err)
			}
		}
	}
	for _, spec := range definition.GlobalTransitions {
		if err := catalog.ValidateSlots(spec.RequiredEvidence); err != nil {
			return fmt.Errorf("evidence catalog: global transition %s: %w", spec.ID, err)
		}
	}
	for entity, lifecycle := range definition.EntityLifecycles {
		for _, spec := range lifecycle.Transitions {
			if err := catalog.ValidateSlots(spec.RequiredEvidence); err != nil {
				return fmt.Errorf("evidence catalog: entity %s transition %s: %w", entity, spec.ID, err)
			}
		}
	}
	return nil
}

type evidenceDefinition struct {
	Transitions []struct {
		ID               string   `json:"id"`
		RequiredEvidence []string `json:"required_evidence"`
	} `json:"transitions"`
	PhaseMachines map[string]struct {
		Transitions []struct {
			ID               string   `json:"id"`
			RequiredEvidence []string `json:"required_evidence"`
		} `json:"transitions"`
	} `json:"phase_machines"`
	GlobalTransitions []struct {
		ID               string   `json:"id"`
		RequiredEvidence []string `json:"required_evidence"`
	} `json:"global_transitions"`
	EntityLifecycles map[string]struct {
		Transitions []struct {
			ID               string   `json:"id"`
			RequiredEvidence []string `json:"required_evidence"`
		} `json:"transitions"`
	} `json:"entity_lifecycles"`
}

func ValidateAgentMessages(root string) error {
	refs, err := currentAgentMessageRefs(root)
	if err != nil {
		return err
	}
	validator := schema.NewValidator(root)
	for _, relative := range refs {
		if err := validator.ValidateFile("agent-message.schema.json", relative); err != nil {
			return fmt.Errorf("%s: %w", filepath.ToSlash(relative), err)
		}
		data, err := os.ReadFile(filepath.Join(root, relative))
		if err == nil {
			if warnings := schema.WarnMissingExtensionFields(data); len(warnings) > 0 {
				_ = warnings
			}
		}
	}
	return nil
}

func ValidateReviewManifests(root string) error {
	runtimeData, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		return fmt.Errorf("read runtime for manifest validation: %w", err)
	}
	var state runtimeReachability
	if err := json.Unmarshal(runtimeData, &state); err != nil {
		return fmt.Errorf("decode runtime for manifest validation: %w", err)
	}
	statuses, err := state.s5ManifestStatuses(root)
	if err != nil {
		return err
	}
	refs, err := currentTeamManifestRefs(root)
	if err != nil {
		return err
	}
	for _, relative := range refs {
		if err := team.ValidateFile(root, relative); err != nil {
			return fmt.Errorf("%s: %w", filepath.ToSlash(relative), err)
		}
		status := statuses[normalizeReachabilityReference(relative)]
		if err := validateReviewManifestReferencesWithHistory(root, relative, state.RuntimeID, status.Bound && status.Historical); err != nil {
			return err
		}
	}
	if err := ValidateCrossManifestDependencies(root, crossManifestDependencyRefs(refs, statuses)); err != nil {
		return err
	}
	return nil
}

func currentTeamManifestRefs(root string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		return nil, fmt.Errorf("read runtime for team references: %w", err)
	}
	var state runtimeReachability
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode runtime for team references: %w", err)
	}
	refs := make([]string, 0, len(state.Entities.Teams))
	for _, item := range state.Entities.Teams {
		if ref := referencePath(item.ManifestRef); ref != "" {
			refs = append(refs, ref)
		}
	}
	return refs, nil
}

func currentAgentMessageRefs(root string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		return nil, fmt.Errorf("read runtime for Agent references: %w", err)
	}
	var state runtimeReachability
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("decode runtime for Agent references: %w", err)
	}
	var refs []string
	for _, item := range state.Entities.Agents {
		if ref := referencePath(item.ReadbackRef); ref != "" {
			refs = append(refs, ref)
		}
		// Before activation_sent, plan_checkpoint Agents point activation_ref
		// at a pre-staged capability envelope, not an agent-message. The
		// activation revision is committed with the real activation message;
		// legacy activated-state rows are kept compatible below.
		if item.ActivationRef != "" && hasActivationMessage(state.SchemaVersion, item.State, item.LastLifecycleEvent, item.ActivationRevision) {
			refs = append(refs, referencePath(item.ActivationRef))
		}
	}
	return refs, nil
}

func hasActivationMessage(schemaVersion, state, event string, revision *int) bool {
	if revision != nil {
		return true
	}
	if schemaVersion == "1.3.0" {
		switch event {
		case "activation_sent", "work_started", "completion_reported", "completion_acknowledged", "work_blocked", "blocker_resolved", "integration_rework_requested":
			return true
		default:
			return false
		}
	}
	// Pre-1.3 runtimes used the Agent activity itself to distinguish a staged
	// envelope from the activation message. Keep that compatibility seam for
	// the legacy vocabulary while 1.3 uses its activation revision/event facts.
	switch state {
	case "activated", "working", "reported", "done", "blocked":
		return true
	default:
		return false
	}
}

func referencePath(ref string) string {
	return filepath.ToSlash(strings.SplitN(ref, "#", 2)[0])
}

func currentRuntimeID(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, ".claude/loop-state.json"))
	if err != nil {
		return "", fmt.Errorf("read runtime for manifest validation: %w", err)
	}
	var state runtimeState
	if err := json.Unmarshal(data, &state); err != nil {
		return "", fmt.Errorf("decode runtime for manifest validation: %w", err)
	}
	return state.RuntimeID, nil
}

// ValidateReviewManifestReferences is exported for test use; production callers
// reach it through ValidateReviewManifests.
func ValidateReviewManifestReferences(root, relative, runtimeID string) error {
	return validateReviewManifestReferences(root, relative, runtimeID)
}

func validateReviewManifestReferences(root, relative, runtimeID string) error {
	return validateReviewManifestReferencesWithHistory(root, relative, runtimeID, false)
}

func validateReviewManifestReferencesWithHistory(root, relative, runtimeID string, historicalS5 bool) error {
	data, err := os.ReadFile(filepath.Join(root, relative))
	if err != nil {
		return fmt.Errorf("read %s: %w", filepath.ToSlash(relative), err)
	}
	var manifest reviewManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("decode %s: %w", filepath.ToSlash(relative), err)
	}
	if manifest.RuntimeID != runtimeID {
		return fmt.Errorf("%s: runtime_id=%s does not match current runtime %s",
			filepath.ToSlash(relative), manifest.RuntimeID, runtimeID)
	}
	for _, doc := range manifest.Documents {
		if historicalS5 {
			if _, err := resolveReachablePath(root, filepath.ToSlash(relative)+" document "+doc.ID, doc.Path); err != nil {
				return err
			}
			continue
		}
		actual, err := fileSHA256(filepath.Join(root, doc.Path))
		if err != nil {
			return fmt.Errorf("%s: document %s fingerprint: %w",
				filepath.ToSlash(relative), doc.ID, err)
		}
		if actual != doc.SHA256 {
			return fmt.Errorf("%s: document %s fingerprint mismatch: manifest=%s actual=%s — compare with the current registered source refs at manifest %s; keep any issued S5 manifest bytes unchanged and follow its original rework route",
				filepath.ToSlash(relative), doc.ID, doc.SHA256, actual, filepath.ToSlash(relative))
		}
	}
	return nil
}

func ValidateRuntimeFile(root, runtimePath string) error {
	data, err := os.ReadFile(filepath.Join(root, runtimePath))
	if err != nil {
		return fmt.Errorf("read runtime file: %w", err)
	}
	if err := ValidateRuntimeBytes(root, data); err != nil {
		return err
	}
	var state runtimeState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("decode runtime file: %w", err)
	}
	for _, ref := range []struct {
		name string
		path string
		hash string
	}{
		{"Loop Definition", state.Definition.Path, state.Definition.SHA256},
		{"Hook policy", state.HookControl.PolicyRef.Path, state.HookControl.PolicyRef.SHA256},
	} {
		actual, err := fileSHA256(filepath.Join(root, ref.path))
		if err != nil {
			return fmt.Errorf("%s fingerprint: %w", ref.name, err)
		}
		if actual != ref.hash {
			return fmt.Errorf("%s fingerprint mismatch: runtime=%s actual=%s", ref.name, ref.hash, actual)
		}
	}
	return nil
}

func ValidateRuntimeBytes(root string, data []byte) error {
	validator := schema.NewValidator(root)
	if err := validator.ValidateBytes("loop-state.schema.json", data); err != nil {
		return err
	}
	var rawState map[string]any
	if err := json.Unmarshal(data, &rawState); err != nil {
		return err
	}
	dispatchConfig, err := runtime.DispatchConfig(rawState)
	if err != nil {
		return err
	}
	if _, exists := rawState["dispatch_pressure"]; exists && dispatchConfig == nil {
		return fmt.Errorf("dispatch pressure requires explicit frozen dispatch configuration")
	}

	definitionData, err := os.ReadFile(filepath.Join(root, projectlayout.Definition))
	if err != nil {
		return fmt.Errorf("read definition: %w", err)
	}
	var definition stateDefinition
	if err := json.Unmarshal(definitionData, &definition); err != nil {
		return fmt.Errorf("decode definition: %w", err)
	}
	var state runtimeState
	if err := json.Unmarshal(data, &state); err != nil {
		return fmt.Errorf("decode runtime: %w", err)
	}

	stateSpec, ok := definition.States[state.Lifecycle.State]
	if !ok {
		return fmt.Errorf("unknown lifecycle state %q", state.Lifecycle.State)
	}
	if stateSpec.PhaseMachine == nil {
		if state.Lifecycle.Phase != nil {
			return fmt.Errorf("state %q does not allow a phase", state.Lifecycle.State)
		}
	} else {
		if state.Lifecycle.Phase == nil {
			return fmt.Errorf("state %q requires a phase", state.Lifecycle.State)
		}
		machine, ok := definition.PhaseMachines[*stateSpec.PhaseMachine]
		if !ok {
			return fmt.Errorf("missing phase machine %q", *stateSpec.PhaseMachine)
		}
		if _, ok := machine.Phases[*state.Lifecycle.Phase]; !ok {
			return fmt.Errorf("unknown phase %q for state %q", *state.Lifecycle.Phase, state.Lifecycle.State)
		}
	}

	for _, item := range state.Entities.Agents {
		activity, err := workstate.LegacyAgentActivity(state.SchemaVersion, item.State, item.LastLifecycleEvent)
		if err != nil {
			return fmt.Errorf("invalid Agent lifecycle activity: %w", err)
		}
		if !contains(definition.EntityLifecycles["agent"].States, activity) {
			return fmt.Errorf("unknown Agent state %q", item.State)
		}
	}
	for _, item := range state.Entities.Tasks {
		activity, err := workstate.LegacyTaskActivity(state.SchemaVersion, item.State, item.LastLifecycleEvent)
		if err != nil {
			return fmt.Errorf("invalid TASK lifecycle activity: %w", err)
		}
		if !contains(definition.EntityLifecycles["task"].States, activity) {
			return fmt.Errorf("unknown TASK state %q", item.State)
		}
	}
	for _, item := range state.Entities.Bugs {
		if !contains(definition.EntityLifecycles["bug"].States, item.State) {
			return fmt.Errorf("unknown BUG state %q", item.State)
		}
	}
	if workstate.Unified(state.SchemaVersion) {
		for id, item := range state.Review.Assignments {
			if _, err := workstate.LegacyReviewAssignmentActivity(state.SchemaVersion, item.Status, item.LastLifecycleEvent); err != nil {
				return fmt.Errorf("invalid S7 Assignment %q lifecycle activity: %w", id, err)
			}
		}
	}
	return nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

// ValidateReadbackTemplates validates every readback-request template
// referenced by the runtime against readback-request.schema.json. This is the
// BUG-002 B1 fix: today the schema fires only at team launch time, which
// surprises planners who ran validate --all and saw it pass. Promoting the
// check to validate --all surfaces ordering violations at the documented
// validation step.
func ValidateReadbackTemplates(root string) error {
	refs, err := currentReadbackTemplateRefs(root)
	if err != nil {
		return err
	}
	validator := schema.NewValidator(root)
	for _, relative := range refs {
		if err := validator.ValidateFile("readback-request.schema.json", relative); err != nil {
			return fmt.Errorf("%s: %w", filepath.ToSlash(relative), err)
		}
	}
	return nil
}

// currentReadbackTemplateRefs walks docs/teams/ for readback-request template
// files. Per BUG-002 §8.3 B1 this uses filesystem glob/walk rather than the
// runtime ref because the templates are project artifacts, not runtime
// entities. filepath.Glob does not support ** so we use filepath.Walk for
// recursive discovery.
func currentReadbackTemplateRefs(root string) ([]string, error) {
	baseDir := filepath.Join(root, "docs", "teams")
	info, err := os.Stat(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat readback templates: %w", err)
	}
	if !info.IsDir() {
		return nil, nil
	}
	var refs []string
	walkErr := pathscope.Walk(root, baseDir, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		if fi.Name() != "readback-request.template.json" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		refs = append(refs, filepath.ToSlash(rel))
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk readback templates: %w", walkErr)
	}
	return refs, nil
}

// ValidateCrossManifestDependencies rejects any depends_on target that
// resolves to an assignment in a *different* team manifest. This catches a
// planner typo where the same assignment ID is reused across workgroups and
// accidentally creates a phantom edge that the per-manifest validator cannot
// detect (it only sees one manifest at a time). BUG-001 §8.3 A3.
//
// Exported for test use; production callers reach it through
// ValidateReviewManifests.
func ValidateCrossManifestDependencies(root string, manifests []string) error {
	if len(manifests) == 0 {
		return nil
	}
	type ref struct {
		manifest      string
		workgroupKind string
	}
	owners := make(map[string]ref)
	for _, m := range manifests {
		data, err := os.ReadFile(filepath.Join(root, m))
		if err != nil {
			return fmt.Errorf("read %s: %w", filepath.ToSlash(m), err)
		}
		var value struct {
			WorkgroupKind string `json:"workgroup_kind"`
			Assignments   []struct {
				AssignmentID string   `json:"assignment_id"`
				DependsOn    []string `json:"depends_on"`
			} `json:"assignments"`
		}
		if err := json.Unmarshal(data, &value); err != nil {
			continue // malformed manifests are caught by other validators
		}
		for _, a := range value.Assignments {
			owners[a.AssignmentID] = ref{manifest: m, workgroupKind: value.WorkgroupKind}
		}
	}
	for _, m := range manifests {
		data, err := os.ReadFile(filepath.Join(root, m))
		if err != nil {
			continue
		}
		var value struct {
			Assignments []struct {
				AssignmentID string   `json:"assignment_id"`
				DependsOn    []string `json:"depends_on"`
			} `json:"assignments"`
		}
		if err := json.Unmarshal(data, &value); err != nil {
			continue
		}
		for _, a := range value.Assignments {
			for _, dep := range a.DependsOn {
				owner, ok := owners[dep]
				if !ok {
					continue // unknown deps caught by per-manifest validator
				}
				if owner.manifest != m {
					return fmt.Errorf("%s: assignment %s depends_on %q which lives in a different manifest %s (workgroup_kind=%s) — depends_on is workgroup-internal; encode cross-workgroup waits via separation_edges or runtime scheduling",
						filepath.ToSlash(m), a.AssignmentID, dep, filepath.ToSlash(owner.manifest), owner.workgroupKind)
				}
			}
		}
	}
	return nil
}
