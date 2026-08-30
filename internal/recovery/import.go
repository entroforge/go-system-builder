package recovery

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/entroforge/go-system-builder/internal/evidence"
	"github.com/entroforge/go-system-builder/internal/schema"
)

// ImportCursorInferenceNone states that the importer did not infer a
// lifecycle cursor. Cursor advancement belongs to the production controller
// replay, not to the presence of a file on disk.
const ImportCursorInferenceNone = "none"

// ImportFinding records a rejected or non-authoritative recovery input. A
// finding is deliberately data, not an error: one malformed artifact must not
// prevent the operator from seeing the other trustworthy inputs.
type ImportFinding struct {
	Code   string `json:"code"`
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// ImportResult is the artifact/evidence projection that can be merged into a
// separately constructed Runtime seed. It contains no lifecycle, revision,
// authorization, lease, or active-agent state.
type ImportResult struct {
	Projection      map[string]any              `json:"projection"`
	Documents       []map[string]any            `json:"documents"`
	Evidence        []map[string]any            `json:"evidence"`
	Entities        map[string][]map[string]any `json:"entities"`
	Findings        []ImportFinding             `json:"findings"`
	CursorInference string                      `json:"cursor_inference"`
	TargetCursor    string                      `json:"target_cursor"`
}

const (
	importFindingPath        = "RECOVERY_IMPORT_PATH_INVALID"
	importFindingDocument    = "RECOVERY_IMPORT_DOCUMENT_UNTRUSTED"
	importFindingEvidence    = "RECOVERY_IMPORT_EVIDENCE_UNTRUSTED"
	importFindingDigest      = "RECOVERY_IMPORT_DIGEST_MISMATCH"
	importFindingRuntime     = "RECOVERY_IMPORT_RUNTIME_MISMATCH"
	importFindingBaseline    = "RECOVERY_IMPORT_BASELINE_INVALID"
	importFindingSubject     = "RECOVERY_IMPORT_SUBJECT_UNTRUSTED"
	importFindingSchema      = "RECOVERY_IMPORT_SCHEMA_INVALID"
	importFindingSourceDrift = "RECOVERY_IMPORT_SOURCE_DRIFT"
)

var importDocumentStatus = map[string]struct{}{
	"accepted": {}, "approved": {}, "blocked": {}, "complete": {}, "completed": {},
	"draft": {}, "done": {}, "failed": {}, "in_progress": {}, "locked": {},
	"open": {}, "passed": {}, "pending": {}, "planned": {}, "review": {},
	"reviewed": {}, "valid": {}, "verified": {},
}

const importedEvidenceSchemaVersion = "1.0.0"

var importedEvidenceStatus = map[string]struct{}{
	"valid": {}, "invalid": {}, "superseded": {},
}

// These values are the semantic vocabulary consumed by the registered gate
// contracts. Import must not promote an envelope whose conclusion or
// requested event is merely a syntactically non-empty string.
var importedEvidenceConclusion = map[string]struct{}{
	"accepted": {}, "approved": {}, "approved_with_risk": {}, "blocked": {},
	"blocking": {}, "complete": {}, "completed": {}, "fail": {},
	"fix_required": {}, "incomplete": {}, "no_repair": {}, "pass": {},
	"recorded": {}, "rejected": {}, "release_blocked": {},
	"reported": {}, "req_change_required": {}, "review_required": {},
	"spec_change_required": {}, "stale": {},
}

var importedEvidenceRequestedEvent = map[string]struct{}{
	"acceptance_review_required":       {},
	"blocking_findings_reported":       {},
	"completion_reported":              {},
	"document_fix_required":            {},
	"execution_spec_change_required":   {},
	"finding_req_change_required":      {},
	"finding_spec_change_required":     {},
	"release_audit_blocked":            {},
	"repair_req_change_required":       {},
	"repair_spec_change_required":      {},
	"verification_release_blocked":     {},
	"verification_req_change_required": {},
}

// Producer responsibilities are a controlled vocabulary at the gate
// boundary. Case is presentation-only; the semantic value is compared in
// lower case so existing Architect/architect artifacts remain compatible.
var importedEvidenceResponsibility = map[string]struct{}{
	"acceptance": {}, "architect": {}, "builder": {}, "build-work-package": {},
	"clean round evaluator": {}, "contract planner": {}, "delivery verifier": {},
	"dv-spec-consistency": {}, "dv-task-executability": {}, "e2e browser": {},
	"investigator": {}, "orchestrator": {}, "original finder": {}, "qa": {},
	"release auditor": {}, "task planner": {},
}

// Import accepts an explicit locked REQ, an RR-A Inventory, or an RR-A Plan.
// It only reads the selected source and repository artifacts. In particular,
// it never unmarshals .claude/loop-state.json as a prerequisite.
func Import(root string, source any) (ImportResult, error) {
	resolvedRoot, err := resolveRepositoryRoot(root)
	if err != nil {
		return ImportResult{}, err
	}
	binding, inputs, err := importSource(resolvedRoot, source)
	if err != nil {
		return ImportResult{}, err
	}
	if err := verifyImportInputs(resolvedRoot, inputs); err != nil {
		return ImportResult{}, err
	}
	actualBinding, _, err := validateREQ(resolvedRoot, binding.Path)
	if err != nil {
		return ImportResult{}, fmt.Errorf("validate import req: %w", err)
	}
	if actualBinding != binding {
		return ImportResult{}, &ValidationError{
			Code: ErrInvalidInventory, Field: "req", Path: binding.Path,
			Reason: "selected REQ binding does not match the repository bytes",
		}
	}

	collector := importCollector{root: resolvedRoot, binding: binding}
	if err := collector.scanDocuments(); err != nil {
		return ImportResult{}, err
	}
	if err := collector.scanEvidence(); err != nil {
		return ImportResult{}, err
	}
	collector.finish()
	return collector.result(), nil
}

func importSource(root string, source any) (REQBinding, []InventoryInput, error) {
	switch value := source.(type) {
	case REQBinding:
		return value, nil, nil
	case *REQBinding:
		if value == nil {
			return REQBinding{}, nil, &ValidationError{Code: ErrInvalidREQ, Field: "req", Reason: "req binding is nil"}
		}
		return *value, nil, nil
	case Inventory:
		if err := validateInventory(value); err != nil {
			return REQBinding{}, nil, err
		}
		return value.REQ, append([]InventoryInput(nil), value.Inputs...), nil
	case *Inventory:
		if value == nil {
			return REQBinding{}, nil, &ValidationError{Code: ErrInvalidInventory, Field: "inventory", Reason: "inventory is nil"}
		}
		if err := validateInventory(*value); err != nil {
			return REQBinding{}, nil, err
		}
		return value.REQ, append([]InventoryInput(nil), value.Inputs...), nil
	case Plan:
		inventory := Inventory{SchemaVersion: value.SchemaVersion, REQ: value.REQ, Inputs: value.Inputs}
		if err := validateInventory(inventory); err != nil {
			return REQBinding{}, nil, err
		}
		planned, err := BuildPlan(inventory)
		if err != nil {
			return REQBinding{}, nil, err
		}
		if planned.PlanSHA256 != value.PlanSHA256 {
			return REQBinding{}, nil, &ValidationError{Code: ErrInvalidInventory, Field: "plan_sha256", Reason: "plan hash does not match its content"}
		}
		return value.REQ, append([]InventoryInput(nil), value.Inputs...), nil
	case *Plan:
		if value == nil {
			return REQBinding{}, nil, &ValidationError{Code: ErrInvalidInventory, Field: "plan", Reason: "plan is nil"}
		}
		return importSource(root, *value)
	default:
		return REQBinding{}, nil, &ValidationError{Code: ErrInvalidInventory, Field: "source", Reason: "source must be a REQBinding, Inventory, or Plan"}
	}
}

func verifyImportInputs(root string, inputs []InventoryInput) error {
	for _, input := range inputs {
		if err := validateRelativePath(input.Path); err != nil {
			return &ValidationError{Code: ErrPathOutsideRepository, Field: "input.path", Path: input.Path, Reason: "input path must remain repository-relative", Cause: err}
		}
		fullPath := filepath.Join(root, filepath.FromSlash(input.Path))
		if _, err := ensureImportFileInside(root, fullPath); err != nil {
			return &ValidationError{Code: ErrPathOutsideRepository, Field: "input.path", Path: input.Path, Reason: "resolved input path must remain inside repository", Cause: err}
		}
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return &ValidationError{Code: ErrInvalidInventory, Field: "input", Path: input.Path, Reason: "content-addressed input cannot be read", Cause: err}
		}
		actual := sha256Hex(data)
		if actual != input.SHA256 {
			return &ValidationError{Code: ErrInvalidInventory, Field: "input.sha256", Path: input.Path, Reason: fmt.Sprintf("%s: expected %s, got %s", importFindingSourceDrift, input.SHA256, actual)}
		}
	}
	return nil
}

type importCollector struct {
	root      string
	binding   REQBinding
	documents []map[string]any
	evidence  []map[string]any
	entities  map[string][]map[string]any
	findings  []ImportFinding
	docByPath map[string]map[string]any
	docByID   map[string]map[string]any
}

func (c *importCollector) scanDocuments() error {
	for _, directory := range []string{
		"docs/requirements", "docs/design", "docs/contracts", "docs/tasks",
		"docs/reports", "docs/release_audits",
	} {
		fullDirectory := filepath.Join(c.root, filepath.FromSlash(directory))
		if _, err := os.Stat(fullDirectory); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return fmt.Errorf("inspect import document directory %q: %w", directory, err)
		}
		if err := filepath.WalkDir(fullDirectory, func(filePath string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return fmt.Errorf("walk import document directory %q: %w", directory, walkErr)
			}
			if entry.IsDir() {
				return nil
			}
			relative, err := ensureImportFileInside(c.root, filePath)
			if err != nil {
				c.addFinding(importFindingPath, importPathForFinding(c.root, filePath), "resolved document path escapes repository")
				return nil
			}
			if strings.Contains(strings.ToLower(filepath.Base(relative)), "template") {
				return nil
			}
			kind := importDocumentKind(relative)
			if kind == "" || !isImportDocumentExtension(relative) {
				return nil
			}
			c.importDocument(filePath, relative, kind)
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}

func (c *importCollector) importDocument(fullPath, relative, kind string) {
	data, err := os.ReadFile(fullPath)
	if err != nil {
		c.addFinding(importFindingDocument, relative, fmt.Sprintf("read failed: %v", err))
		return
	}
	digest := sha256Hex(data)
	meta := parseImportMetadata(string(data))
	id := strings.TrimSuffix(filepath.Base(filepath.FromSlash(relative)), filepath.Ext(filepath.FromSlash(relative)))
	if relative == c.binding.Path {
		id = c.binding.ID
		kind = "req"
		meta["status"] = "locked"
		meta["version"] = c.binding.Version
		meta["req"] = c.binding.ID
	}
	if id == "" {
		c.addFinding(importFindingDocument, relative, "document identity is missing")
		return
	}
	status := strings.ToLower(strings.TrimSpace(meta["status"]))
	version := strings.TrimSpace(meta["version"])
	declaredREQ := strings.TrimSpace(meta["req"])
	if status == "" {
		c.addFinding(importFindingDocument, relative, "document status is missing")
		return
	}
	if _, ok := importDocumentStatus[status]; !ok {
		c.addFinding(importFindingDocument, relative, "document status is not recognized: "+status)
		return
	}
	if version == "" {
		c.addFinding(importFindingDocument, relative, "document version is missing")
		return
	}
	if declaredREQ == "" {
		c.addFinding(importFindingDocument, relative, "document REQ ownership is missing")
		return
	}
	if declaredREQ != c.binding.ID {
		c.addFinding(importFindingDocument, relative, "document belongs to "+declaredREQ+", selected REQ is "+c.binding.ID)
		return
	}
	if declaredDigest := firstMetadata(meta, "sha256", "digest", "summary_sha256"); declaredDigest != "" && !strings.EqualFold(declaredDigest, digest) {
		c.addFinding(importFindingDigest, relative, "document declared digest does not match file bytes")
		return
	}
	document := map[string]any{
		"id":         id,
		"kind":       kind,
		"path":       relative,
		"version":    version,
		"sha256":     digest,
		"status":     status,
		"generation": 1,
	}
	if c.docByPath == nil {
		c.docByPath = make(map[string]map[string]any)
		c.docByID = make(map[string]map[string]any)
	}
	if previous := c.docByPath[relative]; previous != nil {
		c.addFinding(importFindingDocument, relative, "duplicate canonical document path")
		return
	}
	if previous := c.docByID[id]; previous != nil && previous["sha256"] != digest {
		c.addFinding(importFindingDocument, relative, "duplicate document identity has conflicting bytes")
		return
	}
	c.docByPath[relative] = document
	c.docByID[id] = document
	c.documents = append(c.documents, document)
}

func (c *importCollector) scanEvidence() error {
	directory := filepath.Join(c.root, ".claude", "evidence")
	if _, err := os.Stat(directory); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect import evidence directory: %w", err)
	}
	return filepath.WalkDir(directory, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk import evidence directory: %w", walkErr)
		}
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".json" {
			return nil
		}
		relative, err := ensureImportFileInside(c.root, filePath)
		if err != nil {
			c.addFinding(importFindingPath, importPathForFinding(c.root, filePath), "resolved evidence path escapes repository")
			return nil
		}
		c.importEvidence(filePath, relative)
		return nil
	})
}

func (c *importCollector) importEvidence(fullPath, relative string) {
	data, err := os.ReadFile(fullPath)
	if err != nil {
		c.addFinding(importFindingEvidence, relative, fmt.Sprintf("read failed: %v", err))
		return
	}
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		c.addFinding(importFindingSchema, relative, "evidence JSON contains a UTF-8 BOM")
		return
	}
	var envelope map[string]any
	if err := json.Unmarshal(data, &envelope); err != nil {
		c.addFinding(importFindingSchema, relative, fmt.Sprintf("evidence JSON is malformed: %v", err))
		return
	}
	if envelope == nil {
		c.addFinding(importFindingSchema, relative, "evidence envelope is null")
		return
	}
	if err := c.validateEvidenceEnvelope(envelope, relative, sha256Hex(data)); err != nil {
		c.addFinding(importFindingSchema, relative, err.Error())
		return
	}

	id := stringImportValue(envelope["evidence_id"])
	kind := stringImportValue(envelope["kind"])
	producerAgent := stringImportValue(envelope["producer_agent_id"])
	producerResponsibility := stringImportValue(envelope["producer_responsibility"])
	status := "valid"
	if value := strings.ToLower(stringImportValue(envelope["status"])); value != "" {
		status = value
	}
	reviewRound, hasReviewRound := integerImportValue(envelope["review_round"])
	var review any
	if hasReviewRound {
		review = reviewRound
	}
	scopeRefs := importSubjectPaths(envelope["subject_refs"])
	evidence := map[string]any{
		"id":                  id,
		"kind":                kind,
		"path":                relative,
		"sha256":              sha256Hex(data),
		"status":              status,
		"baseline_generation": integerImportValueOrZero(envelope["baseline_generation"]),
		"review_round":        review,
		"produced_by":         []any{producerAgent},
		"invalidated_by":      nil,
		"invalidation_rule":   nil,
		"invalidation_reason": nil,
		"responsibility_id":   producerResponsibility,
		"scope_refs":          scopeRefs,
	}
	if value := stringImportValue(envelope["invalidated_by"]); value != "" {
		evidence["invalidated_by"] = value
	}
	if previous := c.evidenceByID(id); previous != nil {
		c.addFinding(importFindingEvidence, relative, "duplicate evidence id")
		return
	}
	c.evidence = append(c.evidence, evidence)
}

func (c *importCollector) validateEvidenceEnvelope(envelope map[string]any, relative, digest string) error {
	schemaVersion, err := requiredEvidenceString(envelope, "schema_version")
	if err != nil {
		return err
	}
	if schemaVersion != importedEvidenceSchemaVersion {
		return fmt.Errorf("schema_version %q is not %q", schemaVersion, importedEvidenceSchemaVersion)
	}
	evidenceID, err := requiredEvidenceString(envelope, "evidence_id")
	if err != nil {
		return err
	}
	kind, err := requiredEvidenceString(envelope, "kind")
	if err != nil {
		return err
	}
	runtimeID, err := requiredEvidenceString(envelope, "runtime_id")
	if err != nil {
		return err
	}
	producerAgentID, err := requiredEvidenceString(envelope, "producer_agent_id")
	if err != nil {
		return err
	}
	producerResponsibility, err := requiredEvidenceString(envelope, "producer_responsibility")
	if err != nil {
		return err
	}
	conclusion, err := requiredEvidenceString(envelope, "conclusion")
	if err != nil {
		return err
	}
	if _, ok := importedEvidenceConclusion[conclusion]; !ok {
		return fmt.Errorf("conclusion %q is not registered in the evidence semantic vocabulary", conclusion)
	}
	if _, ok := importedEvidenceResponsibility[strings.ToLower(producerResponsibility)]; !ok {
		return fmt.Errorf("producer_responsibility %q is not registered in the evidence semantic vocabulary", producerResponsibility)
	}

	if !evidence.DefaultCatalog().IsImportableKind(kind) {
		return fmt.Errorf("evidence kind %q is not registered for recovery import", kind)
	}
	if runtimeID != "loop-"+c.binding.ID {
		return fmt.Errorf("%s: expected loop-%s", importFindingRuntime, c.binding.ID)
	}
	generation, ok := integerImportValue(envelope["baseline_generation"])
	if !ok || generation < 1 {
		return fmt.Errorf("%s: baseline_generation must be a positive integer", importFindingBaseline)
	}
	status, err := importedEvidenceStatusValue(envelope)
	if err != nil {
		return err
	}
	if status == "invalid" || status == "superseded" {
		return fmt.Errorf("evidence status %q is not reusable", status)
	}
	invalidatedBy, err := nullableEvidenceString(envelope, "invalidated_by")
	if err != nil {
		return err
	}
	if invalidatedBy != "" {
		return fmt.Errorf("evidence is invalidated by %s", invalidatedBy)
	}
	if declared := firstEnvelopeDigest(envelope); declared != "" && !strings.EqualFold(declared, digest) {
		return fmt.Errorf("%s: envelope digest does not match file bytes", importFindingDigest)
	}
	if err := validateEnvelopeDigests(envelope); err != nil {
		return err
	}
	if requestedEvent, err := optionalEvidenceString(envelope, "requested_event"); err != nil {
		return err
	} else if requestedEvent != "" {
		if _, ok := importedEvidenceRequestedEvent[requestedEvent]; !ok {
			return fmt.Errorf("requested_event %q is not registered in the evidence semantic vocabulary", requestedEvent)
		}
	}
	if reviewRound, present := envelope["review_round"]; present && reviewRound != nil {
		parsed, valid := integerImportValue(reviewRound)
		if !valid || parsed < 1 {
			return fmt.Errorf("review_round must be a positive integer or null")
		}
	}
	if _, ok := envelope["subject_refs"]; !ok {
		return fmt.Errorf("subject_refs is missing")
	}
	refs, ok := envelope["subject_refs"].([]any)
	if !ok {
		return fmt.Errorf("subject_refs must be an array")
	}
	for index, raw := range refs {
		ref, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: subject_refs[%d] is not an object", importFindingSubject, index)
		}
		refPath, err := requiredEvidenceString(ref, "path")
		if err != nil {
			return fmt.Errorf("%s: subject_refs[%d] %w", importFindingSubject, index, err)
		}
		refVersion, err := requiredEvidenceString(ref, "version")
		if err != nil {
			return fmt.Errorf("%s: subject_refs[%d] %w", importFindingSubject, index, err)
		}
		refDigest, err := requiredEvidenceString(ref, "sha256")
		if err != nil {
			return fmt.Errorf("%s: subject_refs[%d] %w", importFindingSubject, index, err)
		}
		if err := validateRelativePath(refPath); err != nil {
			return fmt.Errorf("%s: subject_refs[%d] path is outside repository", importFindingPath, index)
		}
		if err := validateEnvelopeSHA256(refDigest, "subject_refs["+strconv.Itoa(index)+"].sha256"); err != nil {
			return err
		}
		fullPath := filepath.Join(c.root, filepath.FromSlash(refPath))
		if _, err := ensureImportFileInside(c.root, fullPath); err != nil {
			return fmt.Errorf("%s: subject_refs[%d] resolved path escapes repository", importFindingPath, index)
		}
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return fmt.Errorf("%s: subject_refs[%d] cannot be read", importFindingSubject, index)
		}
		if sha256Hex(data) != refDigest {
			return fmt.Errorf("%s: subject_refs[%d] digest does not match", importFindingDigest, index)
		}
		document, trusted := c.docByPath[refPath]
		if !trusted || stringImportValue(document["sha256"]) != refDigest || stringImportValue(document["version"]) != refVersion {
			return fmt.Errorf("%s: subject_refs[%d] does not reference a trusted current document", importFindingSubject, index)
		}
	}

	return c.validateFormalEvidenceIndex(map[string]any{
		"id":                  evidenceID,
		"kind":                kind,
		"path":                relative,
		"sha256":              digest,
		"status":              status,
		"baseline_generation": generation,
		"review_round":        reviewRoundValue(envelope),
		"produced_by":         []any{producerAgentID},
		"invalidated_by":      nil,
		"invalidation_rule":   nil,
		"invalidation_reason": nil,
		"responsibility_id":   producerResponsibility,
		"scope_refs":          importSubjectPaths(envelope["subject_refs"]),
	})
}

func requiredEvidenceString(object map[string]any, field string) (string, error) {
	raw, ok := object[field]
	if !ok {
		return "", fmt.Errorf("%s is missing", field)
	}
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", field)
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is missing", field)
	}
	return value, nil
}

func optionalEvidenceString(object map[string]any, field string) (string, error) {
	raw, ok := object[field]
	if !ok {
		return "", nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string when present", field)
	}
	return value, nil
}

func nullableEvidenceString(object map[string]any, field string) (string, error) {
	raw, ok := object[field]
	if !ok || raw == nil {
		return "", nil
	}
	return optionalEvidenceString(object, field)
}

func importedEvidenceStatusValue(envelope map[string]any) (string, error) {
	raw, ok := envelope["status"]
	if !ok {
		return "valid", nil
	}
	status, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("status must be a string when present")
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if _, ok := importedEvidenceStatus[status]; !ok {
		return "", fmt.Errorf("status %q is not registered in the Runtime evidence schema", status)
	}
	return status, nil
}

func reviewRoundValue(envelope map[string]any) any {
	value, ok := integerImportValue(envelope["review_round"])
	if !ok {
		return nil
	}
	return value
}

func validateEnvelopeDigests(envelope map[string]any) error {
	for _, field := range []string{"file_sha256", "envelope_sha256", "summary_sha256"} {
		if raw, ok := envelope[field]; ok && raw != nil {
			value, valid := raw.(string)
			if !valid {
				return fmt.Errorf("%s must be a string when present", field)
			}
			if err := validateEnvelopeSHA256(value, field); err != nil {
				return err
			}
		}
	}
	if raw, ok := envelope["index"]; ok && raw != nil {
		index, valid := raw.(map[string]any)
		if !valid {
			return fmt.Errorf("index must be an object when present")
		}
		if rawDigest, present := index["sha256"]; present && rawDigest != nil {
			digest, valid := rawDigest.(string)
			if !valid {
				return fmt.Errorf("index.sha256 must be a string when present")
			}
			if err := validateEnvelopeSHA256(digest, "index.sha256"); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateEnvelopeSHA256(value, field string) error {
	if len(value) != 64 {
		return fmt.Errorf("%s must be a lowercase SHA-256 digest", field)
	}
	if _, err := hex.DecodeString(value); err != nil || value != strings.ToLower(value) {
		return fmt.Errorf("%s must be a lowercase SHA-256 digest", field)
	}
	return nil
}

func (c *importCollector) validateFormalEvidenceIndex(index map[string]any) error {
	// There is no generic embedded evidence-envelope schema today. Reuse the
	// formal Runtime state schema for the persisted evidence[] projection; the
	// envelope-specific allowlists above provide the missing semantic contract.
	example, err := schema.ReadAsset("loop-state.example.json")
	if err != nil {
		return fmt.Errorf("load formal Runtime evidence schema fixture: %w", err)
	}
	var state map[string]any
	if err := json.Unmarshal(example, &state); err != nil {
		return fmt.Errorf("decode formal Runtime evidence schema fixture: %w", err)
	}
	state["runtime_id"] = "loop-" + c.binding.ID
	state["bound_req"] = map[string]any{
		"path":        c.binding.Path,
		"version":     c.binding.Version,
		"sha256":      c.binding.SHA256,
		"id":          c.binding.ID,
		"status":      c.binding.Status,
		"approved_by": "recovery",
		"approved_at": "2026-01-01T00:00:00Z",
	}
	state["evidence"] = []any{index}
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode formal Runtime evidence candidate: %w", err)
	}
	if err := schema.NewEmbeddedValidator().ValidateBytes("loop-state.schema.json", data); err != nil {
		return fmt.Errorf("formal Runtime evidence schema validation failed: %w", err)
	}
	return nil
}

func ensureImportFileInside(root, fullPath string) (string, error) {
	relative, err := repositoryRelativePath(root, fullPath)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(fullPath)
	if err != nil {
		return relative, fmt.Errorf("resolve %s: %w", relative, err)
	}
	if _, err := repositoryRelativePath(root, resolved); err != nil {
		return relative, ErrPathOutsideRepository
	}
	return relative, nil
}

func importPathForFinding(root, fullPath string) string {
	relative, err := filepath.Rel(root, fullPath)
	if err != nil {
		return filepath.Base(fullPath)
	}
	return filepath.ToSlash(relative)
}

func (c *importCollector) finish() {
	sort.Slice(c.documents, func(i, j int) bool {
		return stringImportValue(c.documents[i]["path"]) < stringImportValue(c.documents[j]["path"])
	})
	sort.Slice(c.evidence, func(i, j int) bool {
		return stringImportValue(c.evidence[i]["id"]) < stringImportValue(c.evidence[j]["id"])
	})
	c.entities = map[string][]map[string]any{
		"agents": {},
		"tasks":  {},
		"bugs":   {},
		"teams":  {},
	}
	for _, document := range c.documents {
		kind := stringImportValue(document["kind"])
		id := stringImportValue(document["id"])
		status := stringImportValue(document["status"])
		switch kind {
		case "task":
			c.entities["tasks"] = append(c.entities["tasks"], map[string]any{
				"id": id, "state": importTaskState(status), "path": document["path"],
				"sha256": document["sha256"], "owner_agent_ids": []any{},
			})
		case "bug":
			c.entities["bugs"] = append(c.entities["bugs"], map[string]any{
				"id": id, "state": importBugState(status), "path": document["path"],
				"severity": "P3", "attempt_count": 0, "same_contract_failure_count": 0,
				"original_finder_agent_ids": []any{},
			})
		}
	}
	sort.Slice(c.entities["tasks"], func(i, j int) bool {
		return stringImportValue(c.entities["tasks"][i]["id"]) < stringImportValue(c.entities["tasks"][j]["id"])
	})
	sort.Slice(c.entities["bugs"], func(i, j int) bool {
		return stringImportValue(c.entities["bugs"][i]["id"]) < stringImportValue(c.entities["bugs"][j]["id"])
	})
	sort.Slice(c.findings, func(i, j int) bool {
		if c.findings[i].Code != c.findings[j].Code {
			return c.findings[i].Code < c.findings[j].Code
		}
		if c.findings[i].Path != c.findings[j].Path {
			return c.findings[i].Path < c.findings[j].Path
		}
		return c.findings[i].Reason < c.findings[j].Reason
	})
}

func (c *importCollector) result() ImportResult {
	result := ImportResult{
		Documents:       append([]map[string]any(nil), c.documents...),
		Evidence:        append([]map[string]any(nil), c.evidence...),
		Entities:        c.entities,
		Findings:        append([]ImportFinding(nil), c.findings...),
		CursorInference: ImportCursorInferenceNone,
		TargetCursor:    PlanSeedCursor,
	}
	result.Projection = map[string]any{
		"documents": result.Documents,
		"evidence":  result.Evidence,
		"entities":  result.Entities,
	}
	return result
}

func (c *importCollector) addFinding(code, relative, reason string) {
	c.findings = append(c.findings, ImportFinding{Code: code, Path: filepath.ToSlash(relative), Reason: reason})
}

func (c *importCollector) evidenceByID(id string) map[string]any {
	for _, item := range c.evidence {
		if stringImportValue(item["id"]) == id {
			return item
		}
	}
	return nil
}

func importDocumentKind(relative string) string {
	base := strings.ToUpper(filepath.Base(filepath.FromSlash(relative)))
	switch {
	case strings.HasPrefix(base, "REQ-"):
		return "req"
	case strings.HasPrefix(base, "TASK-"):
		return "task"
	case strings.HasPrefix(base, "BUG-"):
		return "bug"
	case strings.HasPrefix(base, "ACC-"):
		return "acceptance"
	case strings.HasPrefix(base, "REV-"):
		return "review"
	case strings.HasPrefix(base, "QA-"):
		return "qa"
	case strings.HasPrefix(base, "E2E-"):
		return "e2e"
	case strings.Contains(base, "CONTRACT"):
		return "contract"
	case strings.Contains(base, "RELEASE") || strings.Contains(filepath.ToSlash(relative), "/release_audits/"):
		return "release_audit"
	case strings.Contains(filepath.ToSlash(relative), "/design/"):
		return "design"
	default:
		return ""
	}
}

func isImportDocumentExtension(relative string) bool {
	switch strings.ToLower(filepath.Ext(filepath.FromSlash(relative))) {
	case ".md", ".markdown", ".json":
		return true
	default:
		return false
	}
}

func parseImportMetadata(content string) map[string]string {
	metadata := make(map[string]string)
	for _, line := range strings.Split(strings.TrimPrefix(content, "\ufeff"), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ">"))
		separator := strings.Index(line, ":")
		width := 1
		fullWidth := strings.Index(line, "：")
		if separator < 0 || (fullWidth >= 0 && fullWidth < separator) {
			separator = fullWidth
			width = len("：")
		}
		if separator < 0 {
			continue
		}
		key := strings.ToLower(strings.Trim(strings.TrimSpace(line[:separator]), "*_`|"))
		value := strings.Trim(strings.TrimSpace(line[separator+width:]), "*_`| ")
		switch key {
		case "status", "状态":
			metadata["status"] = firstWord(value)
		case "version", "版本":
			metadata["version"] = firstWord(value)
		case "req", "requirement", "req_id", "bound_req", "需求":
			metadata["req"] = firstWord(value)
		case "sha256", "sha-256", "digest", "summary_sha256":
			metadata[key] = firstWord(value)
		}
	}
	return metadata
}

func firstMetadata(metadata map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(metadata[key]); value != "" {
			return value
		}
	}
	return ""
}

func firstEnvelopeDigest(envelope map[string]any) string {
	for _, key := range []string{"file_sha256", "envelope_sha256", "summary_sha256"} {
		if value := stringImportValue(envelope[key]); value != "" {
			return value
		}
	}
	if index, ok := envelope["index"].(map[string]any); ok {
		return stringImportValue(index["sha256"])
	}
	return ""
}

func importSubjectPaths(value any) []any {
	refs, ok := value.([]any)
	if !ok {
		return []any{}
	}
	paths := make([]string, 0, len(refs))
	for _, raw := range refs {
		if ref, ok := raw.(map[string]any); ok {
			if item := stringImportValue(ref["path"]); item != "" {
				paths = append(paths, item)
			}
		}
	}
	sort.Strings(paths)
	result := make([]any, len(paths))
	for index, item := range paths {
		result[index] = item
	}
	return result
}

func integerImportValue(value any) (int, bool) {
	switch item := value.(type) {
	case int:
		return item, true
	case int64:
		return int(item), true
	case float64:
		if item != float64(int(item)) {
			return 0, false
		}
		return int(item), true
	case json.Number:
		parsed, err := strconv.Atoi(string(item))
		return parsed, err == nil
	default:
		return 0, false
	}
}

func integerImportValueOrZero(value any) int {
	parsed, _ := integerImportValue(value)
	return parsed
}

func stringImportValue(value any) string {
	item, _ := value.(string)
	return item
}

func firstWord(value string) string {
	if fields := strings.Fields(value); len(fields) > 0 {
		return strings.Trim(fields[0], "`*_|")
	}
	return ""
}

func importTaskState(status string) string {
	switch status {
	case "candidate", "reviewed", "locked", "in_progress", "review", "blocked", "done", "cancelled":
		return status
	case "complete", "completed", "passed", "verified":
		return "done"
	default:
		return "candidate"
	}
}

func importBugState(status string) string {
	valid := map[string]struct{}{
		"draft": {}, "investigating": {}, "pending_approval": {}, "accepted": {},
		"repair_readback": {}, "fixing": {}, "targeted_reverification": {},
		"ready_for_full_review": {}, "closed": {}, "rejected": {},
	}
	if _, ok := valid[status]; ok {
		return status
	}
	if status == "complete" || status == "completed" || status == "passed" {
		return "closed"
	}
	return "draft"
}
