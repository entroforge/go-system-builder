package acceptance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/entroforge/go-system-builder/internal/evidence"
)

// FileReader is shared by all S10 consumers, including deep authority reads.
type FileReader interface{ ReadFile(string) ([]byte, error) }

type diskS10Files struct{ root string }

func (f diskS10Files) ReadFile(path string) ([]byte, error) {
	resolved, err := safeManifestPath(f.root, path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(resolved)
}

type S10Contract struct {
	Kind             string
	Responsibilities []string
	Outcomes         map[string]string // outcome -> optional routing event
}

func Contract(kind string) (S10Contract, error) {
	switch kind {
	case "acceptance", "acceptance_record":
		return S10Contract{"acceptance", []string{"Acceptance", "Orchestrator"}, map[string]string{"pass": "", "review_required": "acceptance_review_required"}}, nil
	case "release_audit", "release_audit_record":
		return S10Contract{"release_audit", []string{"Release Auditor"}, map[string]string{"approved": "", "approved_with_risk": "", "blocked": "release_audit_blocked"}}, nil
	default:
		return S10Contract{}, fmt.Errorf("unknown S10 kind %q", kind)
	}
}

type Subject struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type Envelope struct {
	SchemaVersion          string    `json:"schema_version"`
	EvidenceID             string    `json:"evidence_id"`
	Kind                   string    `json:"kind"`
	RuntimeID              string    `json:"runtime_id"`
	BaselineGeneration     int       `json:"baseline_generation"`
	ReviewRound            int       `json:"review_round"`
	ProducerAgentID        string    `json:"producer_agent_id"`
	ProducerResponsibility string    `json:"producer_responsibility"`
	SubjectRefs            []Subject `json:"subject_refs"`
	Conclusion             string    `json:"conclusion"`
	RequestedEvent         string    `json:"requested_event"`
	InvalidatedBy          string    `json:"invalidated_by"`
	ManifestPath           string    `json:"audit_manifest_path"`
	ManifestSHA            string    `json:"audit_manifest_sha256"`
}

type CandidateInput struct {
	State         map[string]any
	Files         FileReader
	Kind          string
	EvidenceID    string // empty selects the current candidate once
	AffectedPaths []string
}

type Candidate struct {
	EvidenceID       string
	Envelope         Envelope
	Summary          Summary
	Consumed         map[string]string
	ObservedRevision int
	LegacySelection  bool
}

// SelectS10Candidate implements the historical append order within the current
// scope. Selection deliberately precedes content validation: unreadable or
// invalidated current evidence must never expose an older PASS again.
func SelectS10Candidate(state map[string]any, kind string) (map[string]any, error) {
	c, err := Contract(kind)
	if err != nil {
		return nil, err
	}
	rows, _ := state["evidence"].([]any)
	var selected map[string]any
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		actual, err := Contract(stringValue(row["kind"]))
		if err != nil || actual.Kind != c.Kind || intValueS10(row["baseline_generation"]) != nestedS10Int(state, "baseline", "generation") || intValueS10(row["review_round"]) != nestedS10Int(state, "review", "round") {
			continue
		}
		// A foreign responsibility is not admitted into this slot's lineage.
		if !slices.Contains(c.Responsibilities, stringValue(row["responsibility_id"])) {
			continue
		}
		selected = row
	}
	if selected == nil {
		return nil, fmt.Errorf("no current %s candidate with an authorized responsibility", c.Kind)
	}
	return selected, nil
}

type observedFiles struct {
	FileReader
	consumed map[string]string
}

func (f observedFiles) ReadFile(path string) ([]byte, error) {
	data, err := readCandidateFile(f.FileReader, path)
	if err == nil {
		sum := sha256.Sum256(data)
		f.consumed[path] = hex.EncodeToString(sum[:])
	}
	return data, err
}

func readCandidateFile(files FileReader, path string) ([]byte, error) {
	clean := filepath.Clean(path)
	if files == nil || path == "" || filepath.IsAbs(path) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("invalid S10 file view/path %q", path)
	}
	return files.ReadFile(clean)
}

// ValidateS10Candidate validates artifact truth only. CleanRound and the human
// release gate remain separate transition requirements. No state is written.
func ValidateS10Candidate(input CandidateInput) (result Candidate, err error) {
	result.Consumed = map[string]string{}
	result.ObservedRevision = intValueS10(input.State["revision"])
	result.LegacySelection = true
	files := observedFiles{input.Files, result.Consumed}
	contract, err := Contract(input.Kind)
	if err != nil {
		return result, err
	}
	row, err := SelectS10Candidate(input.State, input.Kind)
	if err != nil {
		return result, err
	}
	result.EvidenceID = stringValue(row["id"])
	if input.EvidenceID != "" && input.EvidenceID != result.EvidenceID {
		return result, fmt.Errorf("S10 candidate selection conflict: requested %q, current %q", input.EvidenceID, result.EvidenceID)
	}
	defer func() {
		if err != nil {
			err = fmt.Errorf("S10 evidence %s: %w", result.EvidenceID, err)
		}
	}()
	if stringValue(row["status"]) != "valid" || !s10EvidenceIsLive(row["invalidated_by"]) {
		return result, fmt.Errorf("candidate is invalidated or not valid")
	}
	data, err := readAuthoritativeS10File(files, stringValue(row["path"]), stringValue(row["sha256"]), "envelope")
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal(data, &result.Envelope); err != nil {
		return result, fmt.Errorf("envelope schema: %w", err)
	}
	e := result.Envelope
	actual, kindErr := Contract(e.Kind)
	if e.SchemaVersion != "1.0.0" || e.EvidenceID != result.EvidenceID || kindErr != nil || actual.Kind != contract.Kind || e.Kind != stringValue(row["kind"]) {
		return result, fmt.Errorf("envelope schema/id/kind mismatch")
	}
	if e.RuntimeID != stringValue(input.State["runtime_id"]) || e.BaselineGeneration != nestedS10Int(input.State, "baseline", "generation") || e.ReviewRound < 1 || e.ReviewRound != nestedS10Int(input.State, "review", "round") || e.InvalidatedBy != "" {
		return result, fmt.Errorf("envelope runtime/generation/round binding mismatch")
	}
	producerOK := false
	switch actors := row["produced_by"].(type) {
	case []string:
		producerOK = slices.Contains(actors, e.ProducerAgentID)
	case []any:
		for _, actor := range actors {
			producerOK = producerOK || actor == e.ProducerAgentID
		}
	}
	if e.ProducerAgentID == "" || !producerOK || e.ProducerResponsibility != stringValue(row["responsibility_id"]) || !slices.Contains(contract.Responsibilities, e.ProducerResponsibility) {
		return result, fmt.Errorf("producer/responsibility mismatch")
	}
	event, valid := contract.Outcomes[e.Conclusion]
	if !valid || e.RequestedEvent != event {
		return result, fmt.Errorf("illegal conclusion/requested_event %q/%q for %s", e.Conclusion, e.RequestedEvent, contract.Kind)
	}
	documents, _ := input.State["documents"].([]any)
	for _, subject := range e.SubjectRefs {
		found := false
		for _, raw := range documents {
			doc, _ := raw.(map[string]any)
			if stringValue(doc["path"]) == subject.Path && stringValue(doc["version"]) == subject.Version && stringValue(doc["sha256"]) == subject.SHA256 && intValueS10(doc["generation"]) == e.BaselineGeneration {
				found = true
				break
			}
		}
		if !found {
			return result, fmt.Errorf("subject %q is not a registered current document", subject.Path)
		}
		if _, err = readAuthoritativeS10File(files, subject.Path, subject.SHA256, "subject"); err != nil {
			return result, err
		}
	}
	manifest, err := readAuthoritativeS10File(files, e.ManifestPath, e.ManifestSHA, "manifest")
	if err != nil {
		return result, err
	}
	var binding Manifest
	if err = json.Unmarshal(manifest, &binding); err != nil {
		return result, err
	}
	if binding.RuntimeID != e.RuntimeID || binding.BaselineGeneration != e.BaselineGeneration || binding.ReviewRound != e.ReviewRound {
		return result, fmt.Errorf("manifest binding mismatch")
	}
	baseline, err := BuildS10ExternalBaselineWithFiles(files, input.State, input.AffectedPaths)
	if err != nil {
		return result, err
	}
	authority, err := BuildS10InventoryAuthorityWithFiles(files, input.State, baseline)
	if err != nil {
		return result, err
	}
	result.Summary, err = ValidateForOutcomeWithBaselineAndAuthority(manifest, contract.Kind, e.Conclusion, baseline, authority)
	if err != nil {
		return result, err
	}
	for _, ref := range result.Summary.EvidenceRefs {
		if strings.Contains(ref, "://") || ref == result.EvidenceID {
			return result, fmt.Errorf("evidence_ref %q is not an independent Runtime evidence ID", ref)
		}
		rows, _ := input.State["evidence"].([]any)
		var referenced map[string]any
		for _, raw := range rows {
			r, _ := raw.(map[string]any)
			if r["id"] == ref {
				referenced = r
				break
			}
		}
		if referenced == nil || stringValue(referenced["status"]) != "valid" || !s10EvidenceIsLive(referenced["invalidated_by"]) || intValueS10(referenced["baseline_generation"]) != e.BaselineGeneration || !evidence.DefaultCatalog().IsReferenceableKind(stringValue(referenced["kind"])) {
			return result, fmt.Errorf("evidence_ref %q is not current/referenceable", ref)
		}
		if round := intValueS10(referenced["review_round"]); round != 0 && round != e.ReviewRound {
			return result, fmt.Errorf("evidence_ref %q has stale round", ref)
		}
		if _, err = readAuthoritativeS10File(files, stringValue(referenced["path"]), stringValue(referenced["sha256"]), "evidence_ref "+ref); err != nil {
			return result, err
		}
	}
	return result, nil
}
