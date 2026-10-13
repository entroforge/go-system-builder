package repair

import (
	"errors"
	"fmt"
	"os"
	"sort"
)

func sessionIntent(session RepairSession) string {
	if session.Intent == "" {
		return "implement"
	}
	return session.Intent
}

// Confirmation inherits a committed handoff's verification surface, never its
// changes. The current approved Contract remains the sole scope authority.
func confirmationSubjects(root string, sources []ArtifactRef, runtimeID, reqID, sessionID string, contract ApprovedContract) ([]ArtifactRef, error) {
	if len(sources) == 0 {
		return nil, errors.New("confirm requires hash-pinned confirmation_sources (prior committed RepairHandoffs)")
	}
	subjects := map[string]ArtifactRef{}
	seen := map[string]bool{}
	for _, ref := range sources {
		if seen[ref.Path] {
			return nil, errors.New("duplicate confirmation source")
		}
		seen[ref.Path] = true
		handoff, err := ValidateRepairHandoff(root, ref)
		if err != nil {
			return nil, fmt.Errorf("confirmation source: %w", err)
		}
		prior, err := ValidateRepairSession(root, handoff.SessionRef)
		if err != nil {
			return nil, err
		}
		if prior.SessionID == sessionID || prior.RuntimeID != runtimeID || prior.ReqID != reqID {
			return nil, errors.New("confirmation source must belong to an earlier Session of the same Runtime and REQ")
		}
		result, err := ValidateRepairResult(root, handoff.ResultRef)
		if err != nil {
			return nil, err
		}
		if result.Result != "pass" {
			return nil, errors.New("confirmation source RepairResult is not pass")
		}
		for _, targetRef := range handoff.TargetedReverificationRefs {
			target, err := ValidateTargetedReverification(root, targetRef)
			if err != nil {
				return nil, err
			}
			if target.Result != "pass" || target.ScopeCompliance != "pass" {
				return nil, errors.New("confirmation source targeted reverification is not pass")
			}
		}
		changeset, err := ValidateChangeset(root, handoff.ChangesetRef)
		if err != nil {
			return nil, err
		}
		surface := append(append([]ArtifactRef{}, changeset.Artifacts...), changeset.VerifiedSubjects...)
		if len(surface) == 0 {
			return nil, errors.New("confirmation source has no verification surface")
		}
		for _, subject := range surface {
			subject.ID = ""
			subject.Path = normalizePath(subject.Path)
			if subject.Status != "deleted" {
				subject.Status = ""
			}
			if err := scopeAllows(subject.Path, contract.ProspectiveScope, contract.ForbiddenScope); err != nil {
				return nil, fmt.Errorf("confirmation subject scope: %w", err)
			}
			if prior, ok := subjects[subject.Path]; ok && prior != subject {
				return nil, fmt.Errorf("conflicting confirmation subjects for %s", subject.Path)
			}
			if err := verifySubjectBytes(root, subject); err != nil {
				return nil, err
			}
			subjects[subject.Path] = subject
		}
	}
	paths := make([]string, 0, len(subjects))
	for path := range subjects {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	result := make([]ArtifactRef, 0, len(paths))
	for _, path := range paths {
		result = append(result, subjects[path])
	}
	return result, nil
}

func verifySubjectBytes(root string, subject ArtifactRef) error {
	path, err := repositoryPath(root, subject.Path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if subject.Status == "deleted" {
		if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
			return fmt.Errorf("confirmation deletion subject %s is present or cannot be inspected", subject.Path)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("confirmation subject %s: %w", subject.Path, err)
	}
	if sha256Bytes(data) != subject.SHA256 {
		return fmt.Errorf("confirmation subject %s bytes changed from predecessor", subject.Path)
	}
	return nil
}

func exactSubjectSet(left, right []ArtifactRef, leftName, rightName string) error {
	project := func(refs []ArtifactRef) (map[string]string, error) {
		m := map[string]string{}
		for _, ref := range refs {
			path := normalizePath(ref.Path)
			if path == "." || path == "" || ref.SHA256 == "" {
				return nil, errors.New("invalid confirmation subject")
			}
			if _, exists := m[path]; exists {
				return nil, fmt.Errorf("duplicate confirmation subject %s", path)
			}
			state := "present"
			if ref.Status == "deleted" {
				state = "deleted"
			}
			m[path] = state + ":" + ref.SHA256
		}
		return m, nil
	}
	a, err := project(left)
	if err != nil {
		return err
	}
	b, err := project(right)
	if err != nil {
		return err
	}
	if len(a) != len(b) {
		return fmt.Errorf("%s does not cover exactly %s", leftName, rightName)
	}
	for path, value := range a {
		if b[path] != value {
			return fmt.Errorf("%s differs from %s at %s", leftName, rightName, path)
		}
	}
	return nil
}

func validateConfirmation(root string, session RepairSession) error {
	if sessionIntent(session) != "confirm" {
		return nil
	}
	contract, err := ValidateApprovedContractRef(root, ContractRef{Path: session.ContractRef, SHA256: session.ContractSHA256})
	if err != nil {
		return err
	}
	subjects, err := confirmationSubjects(root, session.ConfirmationSources, session.RuntimeID, session.ReqID, session.SessionID, contract)
	if err != nil {
		return err
	}
	if err := exactSubjectSet(session.VerifiedSubjects, subjects, "Session verified_subjects", "predecessor surface"); err != nil {
		return err
	}
	actual, err := ComputeSessionChangeset(root, session)
	if err != nil {
		return err
	}
	if len(actual) != 0 {
		return errors.New("confirm requires zero actual Session changes; create an implement Session for implementation work")
	}
	return nil
}

// A free-standing PASS artifact does not prove a committed predecessor. Use
// the existing handoff evidence index, including its historical hash, rather
// than a second registry. Its old verdict is provenance, not current approval.
func committedConfirmationSources(state map[string]any, sources []ArtifactRef) error {
	if len(sources) == 0 {
		return errors.New("confirm requires confirmation_sources")
	}
	evidence, _ := state["evidence"].([]any)
	for _, ref := range sources {
		found := false
		for _, raw := range evidence {
			entry, _ := raw.(map[string]any)
			if entry != nil && stringField(entry["kind"]) == "repair_handoff" && stringField(entry["path"]) == ref.Path && stringField(entry["sha256"]) == ref.SHA256 && stringField(entry["responsibility_id"]) == "S9 Repair" {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("confirmation source %s is not a hash-matching committed RepairHandoff in this Runtime", ref.Path)
		}
	}
	return nil
}

func assignmentConfirmationSubjects(session RepairSession, assignment RepairAssignment) []ArtifactRef {
	result := []ArtifactRef{}
	for _, subject := range session.VerifiedSubjects {
		for _, scope := range assignment.Scope {
			if pathMatches(subject.Path, scope) {
				result = append(result, subject)
				break
			}
		}
	}
	return result
}

func validateConfirmationImpact(root string, state, pointer map[string]any, results []RepairResult, impact ChangeImpact) error {
	ref, err := pointerArtifact(pointer, "path", "sha256", "current Session")
	if err != nil {
		return err
	}
	session, err := ValidateRepairSession(root, ref)
	if err != nil {
		return err
	}
	if sessionIntent(session) != "confirm" {
		if impact.Session != nil || len(impact.VerifiedSubjects) > 0 {
			return errors.New("confirmation impact fields require a confirm Session")
		}
		return nil
	}
	if err := committedConfirmationSources(state, session.ConfirmationSources); err != nil {
		return err
	}
	if err := validateConfirmation(root, session); err != nil {
		return err
	}
	if impact.Session == nil || impact.Session.Path != ref.Path || impact.Session.SHA256 != ref.SHA256 {
		return errors.New("confirmation ChangeImpact must bind the exact current Session")
	}
	if err := exactSubjectSet(impact.VerifiedSubjects, session.VerifiedSubjects, "ChangeImpact verified_subjects", "Session verified_subjects"); err != nil {
		return err
	}
	refs := map[string]ArtifactRef{}
	for _, result := range results {
		if result.Intent != "confirm" || !allChecksPass(result.Checks) {
			return errors.New("confirmation batch requires current passing checks from every Assignment")
		}
		for _, subject := range result.VerifiedSubjects {
			refs[subject.Path] = subject
		}
	}
	batch := []ArtifactRef{}
	for _, ref := range refs {
		batch = append(batch, ref)
	}
	return exactSubjectSet(batch, session.VerifiedSubjects, "confirmation Result batch", "Session verification surface")
}

func validateConfirmationChangeset(root string, pointer map[string]any, changeset Changeset) error {
	ref, err := pointerArtifact(pointer, "path", "sha256", "current Session")
	if err != nil {
		return err
	}
	session, err := ValidateRepairSession(root, ref)
	if err != nil {
		return err
	}
	if sessionIntent(session) != "confirm" {
		if changeset.Intent == "confirm" || len(changeset.VerifiedSubjects) > 0 {
			return errors.New("confirmation Changeset requires a confirm Session")
		}
		actual, err := ComputeSessionChangeset(root, session)
		if err != nil {
			return err
		}
		return exactChangedArtifactSet(changedArtifactProjection(actual), changeset.Artifacts, "actual Session diff", "Changeset")
	}
	if err := validateConfirmation(root, session); err != nil {
		return err
	}
	if changeset.Intent != "confirm" || changeset.SessionID != session.SessionID || len(changeset.Artifacts) != 0 {
		return errors.New("confirmation Changeset must truthfully record an empty actual change set")
	}
	return exactSubjectSet(changeset.VerifiedSubjects, session.VerifiedSubjects, "Changeset verified_subjects", "Session verified_subjects")
}

func changedArtifactProjection(refs []ArtifactRef) []ChangedArtifact {
	result := []ChangedArtifact{}
	for _, r := range refs {
		result = append(result, ChangedArtifact{Path: r.Path, SHA256: r.SHA256, Status: r.Status})
	}
	return result
}

func impactVerificationSubjects(impact ChangeImpact) []ArtifactRef {
	return append(append([]ArtifactRef{}, impact.ChangedArtifacts...), impact.VerifiedSubjects...)
}
