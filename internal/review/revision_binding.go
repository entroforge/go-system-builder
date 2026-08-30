package review

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func validateRevisionSource(root string, state map[string]any, sourceRef string, currentRound, generation int) error {
	sourceRef = strings.TrimSpace(sourceRef)
	if strings.HasPrefix(sourceRef, "path:") {
		rel, wantDigest, err := parsePathEvidenceRef(sourceRef)
		if err != nil {
			return revisionSourceDiagnostic(sourceRef, err.Error())
		}
		if wantDigest == "" {
			return revisionSourceDiagnostic(sourceRef, "a local path source_ref must carry an explicit #sha256=<64 hex> content digest")
		}
		path, err := repositoryContainedPath(root, rel)
		if err == nil {
			if info, statErr := os.Stat(path); statErr == nil && info.Mode().IsRegular() {
				data, readErr := os.ReadFile(path)
				if readErr == nil {
					if got := sha256Of(data); got == wantDigest {
						return nil
					}
					return revisionSourceDiagnostic(sourceRef, fmt.Sprintf("the referenced local evidence path has digest %s, want %s", sha256Of(data), wantDigest))
				}
				return revisionSourceDiagnostic(sourceRef, fmt.Sprintf("the referenced local evidence path cannot be read: %v", readErr))
			}
		}
		return revisionSourceDiagnostic(sourceRef, "the referenced local evidence path is missing or not a regular repository file")
	}
	for _, raw := range evidenceEntries(state) {
		entry, _ := raw.(map[string]any)
		if entry == nil || stringField(entry["id"]) != sourceRef {
			continue
		}
		if status := stringField(entry["status"]); status != "valid" {
			return revisionSourceDiagnostic(sourceRef, fmt.Sprintf("the referenced Runtime evidence has status %q, want valid", status))
		}
		if got := intField(entry["baseline_generation"]); got != generation {
			return revisionSourceDiagnostic(sourceRef, fmt.Sprintf("the referenced Runtime evidence belongs to baseline_generation %d, current generation is %d", got, generation))
		}
		if got := intField(entry["review_round"]); got != currentRound {
			return revisionSourceDiagnostic(sourceRef, fmt.Sprintf("the referenced Runtime evidence belongs to review_round %d, current round is %d", got, currentRound))
		}
		return nil
	}
	entities, _ := state["entities"].(map[string]any)
	findings, _ := entities["findings"].([]any)
	for _, raw := range findings {
		finding, _ := raw.(map[string]any)
		if finding != nil && stringField(finding["finding_id"]) == sourceRef {
			if got := intField(finding["review_round"]); got != currentRound {
				return revisionSourceDiagnostic(sourceRef, fmt.Sprintf("the Finding belongs to review_round %d, current round is %d", got, currentRound))
			}
			if rawGeneration, ok := finding["baseline_generation"]; ok && intField(rawGeneration) != generation {
				return revisionSourceDiagnostic(sourceRef, fmt.Sprintf("the Finding belongs to baseline_generation %d, current generation is %d", intField(rawGeneration), generation))
			}
			return nil
		}
	}
	return revisionSourceDiagnostic(sourceRef, "the source_ref is not a current Runtime evidence id, Finding id, or repository path")
}

func revisionSourceDiagnostic(sourceRef, reason string) error {
	return s7GateError(
		"S7_REVISION_SOURCE",
		fmt.Sprintf("revision source_ref %q is not usable", sourceRef),
		[]string{reason},
		[]string{"use the canonical Result/Finding evidence id from the current round or a path:<repo-relative-path>#sha256=<64 hex> artifact"},
		"runtime review-plan revise --file plan-v2.json --source-ref <current-result-or-finding> --affected-surface <surface>",
	)
}

func surfaceMatches(target, surface string) bool {
	target = normalizeSurface(target)
	surface = normalizeSurface(surface)
	if target == "" || surface == "" {
		return false
	}
	return target == surface || strings.HasPrefix(target, surface+"/")
}

func normalizeSurface(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.TrimPrefix(value, "./")
	if value == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(value))
}
