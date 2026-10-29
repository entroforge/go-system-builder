package workpacket

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ContractEvidence projects the validated Main planning_contract answer into
// the original evidence envelope. The machine-checked artifact set is carried
// as subjects; contract_answer preserves the professional judgment so a later
// receipt can reconstruct it without trusting a mutable authoring file.
func ContractEvidence(p ContractPacket, id string, artifacts []Reference, answer ContractAnswer, at time.Time) (map[string]any, error) {
	if id == "" || p.ID == "" || p.TemplateID != "s3-contract-v1" || p.Binding.Stage != "S3" || p.Binding.Owner != "Main" || p.Binding.RuntimeID == "" || p.Binding.Generation < 1 {
		return nil, fmt.Errorf("planning_contract evidence requires the original Main S3 packet")
	}
	answer, err := validateContractEvidenceAnswer(p, answer)
	if err != nil {
		return nil, err
	}
	if err := validateContractArtifactProjection(answer, artifacts); err != nil {
		return nil, err
	}
	artifacts = canonicalContractArtifacts(answer, artifacts)

	subjects := make([]Subject, 0, 1+len(p.Inputs.Designs)+len(p.Inputs.Sources)+len(artifacts))
	req := p.Binding.Requirement
	if req.Path == "" || req.Version == "" || !digestPattern.MatchString(req.SHA256) {
		return nil, fmt.Errorf("planning_contract evidence has an invalid frozen REQ subject")
	}
	subjects = append(subjects, Subject{Path: req.Path, Version: req.Version, SHA256: req.SHA256})
	designs := append([]Reference(nil), p.Inputs.Designs...)
	sort.Slice(designs, func(i, j int) bool { return designs[i].Path < designs[j].Path })
	for _, ref := range designs {
		if ref.Path == "" || ref.Version == "" || !digestPattern.MatchString(ref.SHA256) {
			return nil, fmt.Errorf("planning_contract evidence has an invalid frozen design subject")
		}
		subjects = append(subjects, Subject{Path: ref.Path, Version: ref.Version, SHA256: ref.SHA256})
	}
	sources := append([]Subject(nil), p.Inputs.Sources...)
	sort.Slice(sources, func(i, j int) bool { return sources[i].Path < sources[j].Path })
	for _, source := range sources {
		if !safeRelative(source.Path) || !digestPattern.MatchString(source.SHA256) {
			return nil, fmt.Errorf("planning_contract evidence has an invalid frozen conditional source")
		}
		subjects = append(subjects, source)
	}
	for _, artifact := range artifacts {
		subjects = append(subjects, Subject{Path: artifact.Path, Version: artifact.Version, SHA256: artifact.SHA256})
	}
	seenSubjectPaths := map[string]bool{}
	for _, subject := range subjects {
		if seenSubjectPaths[subject.Path] {
			return nil, fmt.Errorf("planning_contract evidence repeats subject path %s", subject.Path)
		}
		seenSubjectPaths[subject.Path] = true
	}

	return map[string]any{
		"schema_version":          "1.0.0",
		"evidence_id":             id,
		"kind":                    "planning_contract",
		"runtime_id":              p.Binding.RuntimeID,
		"baseline_generation":     p.Binding.Generation,
		"producer_agent_id":       p.Binding.Owner,
		"producer_responsibility": "Orchestrator",
		"subject_refs":            subjects,
		"conclusion":              *answer.Conclusion,
		"created_at":              at.UTC().Format(time.RFC3339Nano),
		"contract_answer":         answer,
	}, nil
}

func validateContractEvidenceAnswer(p ContractPacket, answer ContractAnswer) (ContractAnswer, error) {
	draft := p.Draft
	draft.Answer = answer
	data, err := json.Marshal(draft)
	if err != nil {
		return ContractAnswer{}, err
	}
	validated, issues := ValidateContractAnswer(p, data)
	if len(issues) != 0 {
		return ContractAnswer{}, fmt.Errorf("planning_contract answer %s: %s", issues[0].Pointer, issues[0].Expected)
	}
	return validated, nil
}

func validateContractArtifactProjection(answer ContractAnswer, artifacts []Reference) error {
	if answer.IndexPath == nil || len(artifacts) < 1+len(answer.ContractPaths) {
		return fmt.Errorf("planning_contract evidence artifact set differs from professional answer")
	}
	want := map[string]string{*answer.IndexPath: strings.TrimSuffix(filepath.Base(*answer.IndexPath), ".md")}
	for _, path := range answer.ContractPaths {
		want[path] = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	seenPath := map[string]bool{}
	seenName := map[string]bool{}
	for _, artifact := range artifacts {
		if !safeRelative(artifact.Path) || !digestPattern.MatchString(artifact.SHA256) || seenPath[artifact.Path] {
			return fmt.Errorf("planning_contract evidence has an invalid or repeated artifact reference")
		}
		seenPath[artifact.Path] = true
		if name, ok := want[artifact.Path]; ok {
			if filepath.ToSlash(filepath.Dir(artifact.Path)) != "docs/dev/contracts" || !strings.HasSuffix(artifact.Path, ".md") || strings.Contains(strings.ToLower(filepath.Base(artifact.Path)), "template") || artifact.Version == "" {
				return fmt.Errorf("planning_contract evidence has an invalid actual contract reference")
			}
			if seenName[name] {
				return fmt.Errorf("planning_contract evidence repeats artifact identity %s", name)
			}
			seenName[name] = true
			continue
		}
		if !sharedModelEvidencePath(artifact.Path) || artifact.Version != "" {
			return fmt.Errorf("planning_contract evidence has an unsupported shared-model reference: %s", artifact.Path)
		}
	}
	if len(seenName) != len(want) {
		return fmt.Errorf("planning_contract evidence omits an actual contract artifact")
	}
	return nil
}

func sharedModelEvidencePath(path string) bool {
	return safeRelative(path) && strings.HasSuffix(strings.ToLower(path), ".json") && !strings.HasPrefix(path, ".claude/")
}

func canonicalContractArtifacts(answer ContractAnswer, artifacts []Reference) []Reference {
	byPath := make(map[string]Reference, len(artifacts))
	for _, artifact := range artifacts {
		byPath[artifact.Path] = artifact
	}
	ordered := make([]Reference, 0, len(artifacts))
	if ref, ok := byPath[*answer.IndexPath]; ok {
		ordered = append(ordered, ref)
		delete(byPath, *answer.IndexPath)
	}
	contracts := append([]string(nil), answer.ContractPaths...)
	sort.Strings(contracts)
	for _, path := range contracts {
		if ref, ok := byPath[path]; ok {
			ordered = append(ordered, ref)
			delete(byPath, path)
		}
	}
	modelPaths := make([]string, 0, len(byPath))
	for path := range byPath {
		modelPaths = append(modelPaths, path)
	}
	sort.Strings(modelPaths)
	for _, path := range modelPaths {
		ordered = append(ordered, byPath[path])
	}
	return ordered
}
