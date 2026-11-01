package review

import (
	"fmt"
	"github.com/entroforge/go-system-builder/internal/reviewmeta"
	"os"
	"path/filepath"
	"strings"
)

func validateRevisionSource(root string, state map[string]any, sourceRef string, currentRound, generation int) error {
	sourceRef = strings.TrimSpace(sourceRef)
	if strings.HasPrefix(sourceRef, "path:") {
		rel, err := parsePathEvidenceRef(sourceRef)
		if err != nil {
			return revisionSourceDiagnostic(sourceRef, err.Error())
		}
		path, err := repositoryContainedPath(root, rel)
		if err != nil {
			return revisionSourceDiagnostic(sourceRef, err.Error())
		}
		if info, statErr := os.Stat(path); statErr == nil && info.Mode().IsRegular() {
			return nil
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
		if reviewmeta.CompareIdentity(rowReviewIdentity(entry), activeReviewIdentity(state)) == reviewmeta.Different {
			return revisionSourceDiagnostic(sourceRef, "the referenced Runtime evidence belongs to a different review plan")
		}
		return nil
	}
	entities, _ := state["entities"].(map[string]any)
	findings, _ := entities["findings"].([]any)
	for _, raw := range findings {
		finding, _ := raw.(map[string]any)
		if finding != nil && stringField(finding["finding_id"]) == sourceRef {
			if reviewmeta.CompareIdentity(rowReviewIdentity(finding), activeReviewIdentity(state)) == reviewmeta.Different {
				return revisionSourceDiagnostic(sourceRef, "the Finding belongs to a different review plan")
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
		[]string{"use an existing Result/Finding or a path:<repo-relative-path> to the actual material"},
		"update and save the original ReviewPlan questionnaire using the actual source and affected responsibility",
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
