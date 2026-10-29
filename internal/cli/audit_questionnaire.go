package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/entroforge/go-system-builder/internal/acceptance"
	"github.com/entroforge/go-system-builder/internal/policy"
	"github.com/entroforge/go-system-builder/internal/runtime"
	"github.com/entroforge/go-system-builder/internal/semantic"
)

var auditPathSegment = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func currentAuditKind(state map[string]any) string {
	lifecycle, _ := state["lifecycle"].(map[string]any)
	switch stringValue(lifecycle["state"]) {
	case acceptance.ManifestAcceptance:
		return acceptance.ManifestAcceptance
	case acceptance.ManifestReleaseAudit:
		return acceptance.ManifestReleaseAudit
	default:
		return ""
	}
}

func stageAuditQuestionnaireContext(root string, request policy.Input, snapshot runtime.Snapshot) string {
	if !mainQuestionnaireViewer(request) {
		return ""
	}
	kind := currentAuditKind(snapshot.State)
	if kind == "" {
		return ""
	}
	path, err := auditQuestionnairePath(root, snapshot.State, kind)
	if err != nil {
		return auditPreparationFailureContext(root, snapshot.State, err)
	}
	absPath, err := absoluteQuestionnairePath(root, path)
	if err != nil {
		return auditPreparationFailureContext(root, snapshot.State, err)
	}
	readmePath, _ := filepath.Abs(filepath.Join(root, "docs", "examples", "s10", "README.md"))
	if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); os.IsNotExist(err) {
		return auditPreparationFailureContext(root, snapshot.State, fmt.Errorf("answer draft is not prepared yet (stage-entry wiring/source gap); this Hook event is read-only"))
	} else if err != nil {
		return auditPreparationFailureContext(root, snapshot.State, err)
	} else if !info.Mode().IsRegular() {
		return auditPreparationFailureContext(root, snapshot.State, fmt.Errorf("answer draft is not a regular file; this Hook event is read-only"))
	}
	if kind == acceptance.ManifestAcceptance && receivedAuditReviewPending(root, snapshot.State, path) {
		plan := mapFieldCLI(mapFieldCLI(snapshot.State, "review"), "plan")
		return fmt.Sprintf("S10 review_required at %s is already received. Main owns its original return to a full S7 review; preserve this report and do not refill or resave it. The original ReviewPlan source is %s; restore missing source material there. Normal Main completion of source recovery resumes the original consumer.", absPath, resolveRootPath(root, stringValue(plan["path"])))
	}
	outcomes := "pass / review_required"
	if kind == acceptance.ManifestReleaseAudit {
		outcomes = "approved / approved_with_risk / blocked"
	}
	method := "Reuse applicable S7 results; disposition is your judgment, oracle may explain observations or unfinished causes, and evidence_refs may point to existing output."
	if kind == acceptance.ManifestReleaseAudit {
		method = "Reference accepted acceptance results; review actual release risks. Reading prompts: state transitions, transactions, concurrency, data/migration, call sites, observability, verification, documentation/release. These are not eight required tasks. If reporting a topic, use audit_areas[].id to name the topic and audit_areas[].conclusion for pass/not_applicable/unknown/fail, with owner/evidence_refs when available; use risks/technical_debt/blocking_findings for actual findings. No separate topic report is required."
	}
	return fmt.Sprintf("S10 %s answer: %s. Main collects actual reviewers' results here, reporting actual results against each commitment in expected and its source material. coverage_inventory[*].expected is the system-prefilled full source commitment (REQ/CONTRACTS/BE/TASK as supplied), not a new author judgment; preserve that baseline. %s disposition: pass / not_applicable / unknown / fail. outcome: %s; empty outcome remains a draft. Save this same file by edits, scripts or MCP. No envelope or second report is required. The same observation can be written once in oracle; counterevidence and risks may stay [] when there is no actual content. Existing object shapes: %s.", kind, absPath, method, outcomes, readmePath)
}

func questionnairePreparationEvent(event string) bool {
	return event == "SessionStart" || event == "PreToolUse"
}

func absoluteQuestionnairePath(root, relative string) (string, error) {
	if strings.TrimSpace(relative) == "" {
		return "", fmt.Errorf("the registered answer path is empty")
	}
	return filepath.Abs(filepath.Join(root, filepath.FromSlash(relative)))
}

func prepareAuditQuestionnaire(root string, state map[string]any, kind string) (string, acceptance.Manifest, error) {
	path, err := auditQuestionnairePath(root, state, kind)
	if err != nil {
		return "", acceptance.Manifest{}, err
	}
	draftPath, err := absoluteQuestionnairePath(root, path)
	if err != nil {
		return "", acceptance.Manifest{}, err
	}
	if info, err := os.Stat(draftPath); err == nil {
		if !info.Mode().IsRegular() {
			return "", acceptance.Manifest{}, fmt.Errorf("existing manifest %s is not a regular file", draftPath)
		}
		// Existing audit observations survive re-entry. Refresh only the
		// system acceptance reference to the latest accepted report.
		if kind == acceptance.ManifestReleaseAudit {
			data, err := os.ReadFile(draftPath)
			if err != nil {
				return "", acceptance.Manifest{}, err
			}
			var existing acceptance.Manifest
			if err := json.Unmarshal(data, &existing); err != nil {
				return "", acceptance.Manifest{}, err
			}
			var latest string
			rows := stateEvidence(state)
			for i := len(rows) - 1; i >= 0; i-- {
				entry, _ := rows[i].(map[string]any)
				if stringValue(entry["kind"]) == acceptance.ManifestAcceptance && stringValue(entry["status"]) == "valid" && integerValue(entry["baseline_generation"]) == integerValue(nestedStateValue(state, "baseline", "generation")) && integerValue(entry["review_round"]) == integerValue(nestedStateValue(state, "review", "round")) {
					latest = stringValue(entry["path"])
					break
				}
			}
			changed := false
			for i := range existing.CoverageInventory {
				item := &existing.CoverageInventory[i]
				if item.ID != "acceptance" || latest == "" {
					continue
				}
				found := false
				kept := item.SourceRefs[:0]
				for _, ref := range item.SourceRefs {
					oldAcceptance := false
					for _, raw := range rows {
						entry, _ := raw.(map[string]any)
						if stringValue(entry["kind"]) == acceptance.ManifestAcceptance && stringValue(entry["path"]) == ref && ref != latest {
							oldAcceptance = true
							break
						}
					}
					if oldAcceptance {
						changed = true
						continue
					}
					kept = append(kept, ref)
				}
				item.SourceRefs = kept
				for _, ref := range item.SourceRefs {
					if ref == latest {
						found = true
					}
				}
				if !found {
					item.SourceRefs = append(item.SourceRefs, latest)
					changed = true
				}
			}
			if changed {
				data, err = json.MarshalIndent(existing, "", "  ")
				if err != nil {
					return "", acceptance.Manifest{}, err
				}
				if err = os.WriteFile(draftPath, append(data, '\n'), 0o644); err != nil {
					return "", acceptance.Manifest{}, err
				}
			}
			return path, existing, nil
		}
		return path, acceptance.Manifest{}, nil
	} else if !os.IsNotExist(err) {
		return "", acceptance.Manifest{}, fmt.Errorf("inspect existing manifest %s: %w", draftPath, err)
	}
	manifest := auditManifestDraft(state, kind, acceptance.InventoryAuthority{})
	// Stage entry reads existing materials; it does not certify their history.
	addMaterial := func(id, category, path, owner string) error {
		if path == "" {
			return fmt.Errorf("%s %s has no material path", category, id)
		}
		if owner == "" {
			owner = "Main"
		}
		material, err := safeS10Path(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(material)
		if err != nil {
			return fmt.Errorf("read %s material %s: %w", category, path, err)
		}
		refs := []string{path}
		if plan := stringValue(mapFieldCLI(mapFieldCLI(state, "review"), "plan")["path"]); plan != "" {
			refs = append(refs, plan)
		}
		manifest.CoverageInventory = append(manifest.CoverageInventory, acceptance.CoverageItem{
			ID: id, Category: category, SourceRefs: refs, Expected: string(data),
			Owner: owner, Disposition: "unknown", EvidenceRefs: []string{},
		})
		return nil
	}
	if kind == acceptance.ManifestAcceptance {
		bound := mapFieldCLI(state, "bound_req")
		if err := addMaterial(stringValue(bound["id"]), "requirement", stringValue(bound["path"]), "Main"); err != nil {
			return "", acceptance.Manifest{}, err
		}
		documents, _ := state["documents"].([]any)
		for _, raw := range documents {
			doc, _ := raw.(map[string]any)
			category := stringValue(doc["kind"])
			if (category != "contract" && category != "task") || integerValue(doc["generation"]) != manifest.BaselineGeneration {
				continue
			}
			if err := addMaterial(stringValue(doc["id"]), category, stringValue(doc["path"]), stringValue(doc["owner"])); err != nil {
				return "", acceptance.Manifest{}, err
			}
		}
	} else {
		accepted, err := auditQuestionnairePath(root, state, acceptance.ManifestAcceptance)
		if err != nil {
			return "", acceptance.Manifest{}, err
		}
		manifest.CoverageInventory = append(manifest.CoverageInventory, acceptance.CoverageItem{
			ID: "acceptance", Category: "requirement", SourceRefs: []string{accepted}, Owner: "Main",
			Expected:    "Review actual release risks using the accepted acceptance result; do not repeat its requirement checks.",
			Disposition: "unknown", EvidenceRefs: []string{},
		})
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", acceptance.Manifest{}, fmt.Errorf("encode current manifest draft: %w", err)
	}
	if err := prepareQuestionnaireText(draftPath, string(data)+"\n"); err != nil {
		return "", acceptance.Manifest{}, fmt.Errorf("prepare current manifest draft %s: %w", draftPath, err)
	}
	return path, manifest, nil
}

// Human rejection returns to the existing report, retaining observations and
// adding the actual decision as material. Its source reference makes replay a no-op.
func prepareAuditReworkQuestionnaires(root string, state map[string]any, transitionID, decisionPath string) ([]string, error) {
	kinds := []string{acceptance.ManifestReleaseAudit}
	if transitionID == "TR-028" {
		kinds = []string{acceptance.ManifestAcceptance, acceptance.ManifestReleaseAudit}
	}
	if decisionPath == "" {
		return nil, fmt.Errorf("the recorded human decision material path is missing")
	}
	decisionData, err := os.ReadFile(decisionPath)
	if err != nil {
		return nil, err
	}
	var decision struct {
		ID     string `json:"decision_id"`
		Reason string `json:"reason"`
	}
	if err = json.Unmarshal(decisionData, &decision); err != nil {
		return nil, err
	}
	var paths []string
	for _, kind := range kinds {
		rel, err := auditQuestionnairePath(root, state, kind)
		if err != nil {
			return paths, err
		}
		path, err := absoluteQuestionnairePath(root, rel)
		if err != nil {
			return paths, err
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if _, _, err = prepareAuditQuestionnaire(root, state, kind); err != nil {
				return paths, err
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return paths, err
		}
		var report acceptance.Manifest
		if err = json.Unmarshal(data, &report); err != nil {
			return paths, err
		}
		already := false
		for _, item := range report.CoverageInventory {
			for _, ref := range item.SourceRefs {
				if ref == decisionPath {
					already = true
				}
			}
		}
		if !already {
			report.Outcome = ""
			if len(report.CoverageInventory) == 0 {
				report.CoverageInventory = append(report.CoverageInventory, acceptance.CoverageItem{ID: decision.ID, Category: "requirement", Owner: "Main", Expected: decision.Reason, SourceRefs: []string{decisionPath}, Disposition: "unknown"})
			} else {
				for i := range report.CoverageInventory {
					report.CoverageInventory[i].SourceRefs = append(report.CoverageInventory[i].SourceRefs, decisionPath)
				}
			}
			data, err = json.MarshalIndent(report, "", "  ")
			if err != nil {
				return paths, err
			}
			if err = os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
				return paths, err
			}
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func auditManifestDraft(state map[string]any, kind string, _ acceptance.InventoryAuthority) acceptance.Manifest {
	manifest := acceptance.Manifest{
		SchemaVersion:      acceptance.SchemaVersionAcceptance,
		ManifestType:       kind,
		RuntimeID:          stringValue(state["runtime_id"]),
		BaselineGeneration: integerValue(nestedStateValue(state, "baseline", "generation")),
		ReviewRound:        integerValue(nestedStateValue(state, "review", "round")),
		CoverageInventory:  make([]acceptance.CoverageItem, 0),
		Counterevidence:    make([]acceptance.CounterevidenceItem, 0),
		Risks:              []acceptance.RiskItem{},
		TechnicalDebt:      []acceptance.DebtItem{},
		BlockingFindings:   []acceptance.BlockingFinding{},
	}
	manifest.Metrics = acceptance.DeriveMetrics(manifest)
	return manifest
}

func auditQuestionnairePath(root string, state map[string]any, kind string) (string, error) {
	runtimeID := strings.TrimSpace(stringValue(state["runtime_id"]))
	if runtimeID == "" {
		return "", fmt.Errorf("current Runtime has no runtime_id")
	}
	generation := integerValue(nestedStateValue(state, "baseline", "generation"))
	round := integerValue(nestedStateValue(state, "review", "round"))
	if generation < 1 || round < 1 {
		return "", fmt.Errorf("current Runtime has no positive baseline generation and review round")
	}
	segment := auditPathSegment.ReplaceAllString(runtimeID, "-")
	if segment == "" || segment == "." || segment == ".." {
		return "", fmt.Errorf("current runtime_id cannot form an in-repository answer path")
	}
	dir := filepath.ToSlash(filepath.Join("s10", segment, fmt.Sprintf("g%d", generation), fmt.Sprintf("r%d", round)))
	return filepath.ToSlash(filepath.Join(dir, kind+"-manifest.json")), nil
}

func runSavedAuditQuestionnaire(root string, request policy.Input, stdout, stderr io.Writer) (bool, int) {
	if !mainQuestionnaireViewer(request) {
		return false, 0
	}
	if failed, _ := request.ToolResponse["is_error"].(bool); failed {
		return false, 0
	}
	actualPath, saved := savedQuestionnairePath(request)
	if !saved && request.Event != "PostToolUse" {
		return false, 0
	}
	if _, err := os.Stat(filepath.Join(root, ".claude/loop-state.json")); err != nil {
		return false, 0
	}
	snapshot, err := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
	if err != nil {
		return false, 0
	}
	kind := currentAuditKind(snapshot.State)
	if kind == "" {
		return false, 0
	}
	path, pathErr := auditQuestionnairePath(root, snapshot.State, kind)
	if pathErr != nil {
		return false, 0
	}
	expected, err := absoluteQuestionnairePath(root, path)
	if err != nil {
		return false, 0
	}
	if !saved || filepath.Clean(actualPath) != filepath.Clean(expected) {
		if request.Event != "PostToolUse" || kind != acceptance.ManifestAcceptance || !receivedAuditReviewPending(root, snapshot.State, path) {
			return false, 0
		}
	}
	artifactPath, err := safeS10Path(root, path)
	if err != nil {
		return true, auditQuestionnaireFeedback(stdout, fmt.Sprintf("S10 %s questionnaire at exact path %s was not registered: %v", kind, expected, err))
	}

	reject := func(err error) (bool, int) {
		return true, auditQuestionnaireFeedback(stdout, fmt.Sprintf("S10 %s questionnaire at exact path %s was saved but not registered: %v. Correct this same draft and save again.", kind, expected, err))
	}
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		return reject(fmt.Errorf("read saved manifest: %w", err))
	}
	readmePath, _ := filepath.Abs(filepath.Join(root, "docs", "examples", "s10", "README.md"))
	var manifest acceptance.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return reject(fmt.Errorf("decode saved manifest: %w; counterevidence and risks entries, when used, are objects, not strings; existing field examples: %s", err, readmePath))
	}
	// The exact current answer path supplies machine association, not the author.
	manifest.SchemaVersion = acceptance.SchemaVersionAcceptance
	manifest.ManifestType = kind
	manifest.RuntimeID = stringValue(snapshot.State["runtime_id"])
	manifest.BaselineGeneration = integerValue(nestedStateValue(snapshot.State, "baseline", "generation"))
	manifest.ReviewRound = integerValue(nestedStateValue(snapshot.State, "review", "round"))
	if strings.TrimSpace(manifest.Outcome) == "" {
		var missing []string
		for _, item := range manifest.CoverageInventory {
			if item.Disposition == "" || item.Disposition == "unknown" || item.Disposition == "fail" {
				missing = append(missing, fmt.Sprintf("%s (disposition=%q, observation/reason=%q)", strings.Join(item.SourceRefs, ", "), item.Disposition, item.Oracle))
			}
		}
		return true, auditQuestionnaireFeedback(stdout, fmt.Sprintf("S10 %s draft remains at %s; final outcome is unanswered. Current unfinished items: %s. Save continued professional work in this same file; no evidence was recorded and no negative route was selected.", kind, expected, strings.Join(missing, "; ")))
	}
	if !validAuditOutcome(kind, manifest.Outcome) {
		allowed := "pass / review_required"
		if kind == acceptance.ManifestReleaseAudit {
			allowed = "approved / approved_with_risk / blocked"
		}
		return reject(fmt.Errorf("outcome for %s must be %s; leave it empty for a draft, never infer PASS", kind, allowed))
	}
	actor := "Main" // The original S10 responsibility, not a Hook identity claim.
	manifest.Metrics = acceptance.DeriveMetrics(manifest)
	data, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return reject(fmt.Errorf("encode manifest with derived metrics: %w", err))
	}
	data = append(data, '\n')
	summary, err := acceptance.ValidateForOutcome(data, kind, manifest.Outcome)
	if err != nil {
		return reject(err)
	}

	manifestSHA := sha256HexForArtifact(data)
	shortSHA := manifestSHA[:12]
	evidenceID := fmt.Sprintf("s10-%s-g%d-r%d-%s-%s", auditPathSegment.ReplaceAllString(manifest.RuntimeID, "-"), manifest.BaselineGeneration, manifest.ReviewRound, kind, shortSHA)
	responsibility := "Acceptance"
	if kind == acceptance.ManifestReleaseAudit {
		responsibility = "Release Auditor"
	}
	envelopePath := strings.TrimSuffix(path, "-manifest.json") + "-evidence-" + shortSHA + ".json"
	envelope := map[string]any{
		"schema_version": "1.0.0", "evidence_id": evidenceID, "kind": kind,
		"runtime_id": manifest.RuntimeID, "baseline_generation": manifest.BaselineGeneration,
		"review_round": manifest.ReviewRound, "producer_agent_id": actor,
		"producer_responsibility": responsibility, "subject_refs": []string{},
		"conclusion": manifest.Outcome, "audit_manifest_path": path,
		"audit_manifest_sha256": manifestSHA,
	}
	envelopeData, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return reject(fmt.Errorf("encode machine evidence envelope: %w", err))
	}
	envelopeData = append(envelopeData, '\n')
	envelopeSHA := sha256HexForArtifact(envelopeData)
	_, recorded := registeredAuditEvidence(snapshot.State, evidenceID, envelopePath, envelopeSHA)
	if !recorded {
		if evidenceHasID(snapshot.State, evidenceID) {
			return reject(fmt.Errorf("evidence id %s already exists with different content; no duplicate was registered", evidenceID))
		}
		if err := os.WriteFile(artifactPath, data, 0o644); err != nil {
			return reject(fmt.Errorf("write machine-derived metrics to the same manifest: %w", err))
		}
		if err := writeAuditArtifactExclusive(root, envelopePath, envelopeData); err != nil {
			return reject(err)
		}
		round := manifest.ReviewRound
		_, err = runtime.RecordEvidence(root, filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl"), runtime.EvidenceRequest{
			ExpectedRevision: -1, ID: evidenceID, Kind: kind, Path: envelopePath, ProducedBy: []string{actor}, ResponsibilityID: responsibility, ReviewRound: &round, Validator: semantic.RuntimeCandidateValidator{},
		})
		if err != nil {
			latest, snapshotErr := runtime.NewStore(filepath.Join(root, ".claude/loop-state.json"), filepath.Join(root, ".claude/loop-events.jsonl")).Snapshot()
			if snapshotErr == nil {
				_, recorded = registeredAuditEvidence(latest.State, evidenceID, envelopePath, envelopeSHA)
				if recorded && currentAuditKind(latest.State) != kind {
					return true, auditQuestionnaireFeedback(stdout, fmt.Sprintf("S10 %s at %s is already received as %s; current cursor is %s. No evidence or completed exit was repeated.", kind, expected, evidenceID, lifecycleState(latest.State)))
				}
			}
			if !recorded {
				return reject(fmt.Errorf("register original S10 evidence: %w", err))
			}
		}
	}
	var unfinished []string
	for _, item := range manifest.CoverageInventory {
		if item.Disposition != "pass" && item.Disposition != "not_applicable" {
			unfinished = append(unfinished, fmt.Sprintf("%s: %s; observation/reason=%q", strings.Join(item.SourceRefs, ", "), item.Disposition, item.Oracle))
		}
	}
	for _, item := range manifest.Counterevidence {
		if item.Outcome != "pass" && item.Outcome != "not_applicable" {
			unfinished = append(unfinished, fmt.Sprintf("%s: %s", item.Question, item.Outcome))
		}
	}
	for _, item := range manifest.AuditAreas {
		if item.Conclusion != "pass" && item.Conclusion != "not_applicable" {
			unfinished = append(unfinished, fmt.Sprintf("audit %s: %s", item.ID, item.Conclusion))
		}
	}
	for _, item := range manifest.BlockingFindings {
		unfinished = append(unfinished, fmt.Sprintf("reported blocker: %+v", item))
	}
	if len(unfinished) == 0 {
		unfinished = append(unfinished, "none explicitly reported in this answer; this is not an independent certification")
	}
	progress := "Recorded only; continue the reported unfinished work in this same current answer."
	negative := manifest.Outcome == "review_required" || manifest.Outcome == "blocked"
	positive := manifest.Outcome == "pass" || manifest.Outcome == "approved" || manifest.Outcome == "approved_with_risk"
	if negative || (positive && summary.UnknownCount == 0 && summary.BlockingFindingCount == 0) {
		if manifest.Outcome == "review_required" {
			err = recordAuditReviewImpact(root, path, evidenceID, envelopePath, envelopeSHA)
			if err != nil {
				return true, auditQuestionnaireFeedback(stdout, fmt.Sprintf("S10 review_required at %s is received as %s. Main owns the original return recovery: %v. Preserve the received answer; no refill, repeated save or new professional report is required.", expected, evidenceID, err))
			}
		}
		// An accepted decision still at this step can finish its original exit.
		// Registration replay is read-only; drafts and unfinished positive reports do not advance.
		result := runControlCycleForHook(root, request)
		if result.QualityGate.TransitionCommitted {
			progress = fmt.Sprintf("Committed %s; current cursor %s.", result.QualityGate.CandidateTransition, result.QualityGate.NextCursor)
			if nextKind := currentAuditKind(result.Snapshot.State); nextKind != "" {
				progress += " " + stageAuditQuestionnaireContext(root, request, result.Snapshot)
			} else if result.QualityGate.CandidateTransition == "TR-017" {
				progress += " " + stageHumanReleaseQuestionnaireContext(root, request, result.Snapshot)
			} else if result.QualityGate.CandidateTransition == "TR-016" {
				progress += " " + stageReviewPlanQuestionnaireContext(root, request, result.Snapshot)
			}
			if positive {
				reportPath := strings.TrimSuffix(artifactPath, ".json") + ".md"
				if err := os.WriteFile(reportPath, []byte(acceptance.RenderMarkdown(manifest, summary)), 0o644); err != nil {
					progress += fmt.Sprintf(" Accepted decision and transition remain recorded, but readable report output %s failed: %v.", reportPath, err)
				} else {
					progress += " Readable report: " + reportPath + "."
				}
			}
		} else {
			progress = fmt.Sprintf("Decision recorded, but no transition committed; missing=%s; conflicts=%s.", strings.Join(result.QualityGate.Missing, "; "), strings.Join(result.QualityGate.Conflicts, "; "))
		}
		if result.Error != "" {
			progress += fmt.Sprintf(" Original Controller error %s: %s.", result.ErrorCode, result.Error)
		}
		if len(result.Warnings) != 0 {
			progress += " System wiring/output gap: " + strings.Join(result.Warnings, "; ") + "."
		}
	}
	return true, auditQuestionnaireFeedback(stdout, fmt.Sprintf("S10 %s recorded at %s as %s; outcome=%s. Current reported unfinished work: %s. %s", kind, expected, evidenceID, manifest.Outcome, strings.Join(unfinished, "; "), progress))
}

func validAuditOutcome(kind, outcome string) bool {
	if kind == acceptance.ManifestAcceptance {
		return outcome == "pass" || outcome == "review_required"
	}
	return kind == acceptance.ManifestReleaseAudit && (outcome == "approved" || outcome == "approved_with_risk" || outcome == "blocked")
}

func registeredAuditEvidence(state map[string]any, id, path, sha string) (string, bool) {
	for _, raw := range stateEvidence(state) {
		entry, _ := raw.(map[string]any)
		if entry == nil || stringValue(entry["id"]) != id {
			continue
		}
		if stringValue(entry["status"]) == "valid" && stringValue(entry["path"]) == path && stringValue(entry["sha256"]) == sha {
			return id, true
		}
		return id, false
	}
	return "", false
}

func evidenceHasID(state map[string]any, id string) bool {
	for _, raw := range stateEvidence(state) {
		entry, _ := raw.(map[string]any)
		if entry != nil && stringValue(entry["id"]) == id {
			return true
		}
	}
	return false
}

func writeAuditArtifactExclusive(root, rel string, data []byte) error {
	path, err := safeS10Path(root, rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create S10 evidence directory for %s: %w", path, err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if os.IsExist(err) {
		existing, readErr := os.ReadFile(path)
		if readErr == nil && bytes.Equal(existing, data) {
			return nil
		}
		return fmt.Errorf("existing evidence path %s has different bytes and was not overwritten", path)
	}
	if err != nil {
		return fmt.Errorf("create S10 evidence envelope %s: %w", path, err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("write S10 evidence envelope %s: %w", path, err)
	}
	return file.Close()
}

func auditQuestionnaireFeedback(stdout io.Writer, message string) int {
	_ = json.NewEncoder(stdout).Encode(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PostToolUse", "additionalContext": message}})
	return 0
}
